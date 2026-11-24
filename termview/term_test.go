package termview

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"

	"github.com/marrasen/kakel/input"
	"github.com/marrasen/kakel/internal/sessiontest"
	"github.com/marrasen/kakel/screen"
	"github.com/marrasen/kakel/session"
	"github.com/marrasen/kakel/ui"
	"github.com/marrasen/kakel/vt"
)

// rgba is c as a grid's colours are.
func rgba(c color.NRGBA) color.RGBA { return color.RGBA{R: c.R, G: c.G, B: c.B, A: 0xff} }

// stage is a window's only node: one terminal filling it, as a program
// puts one on screen.
type stage struct {
	term   *Term
	output Output
	u      *gunim.UI
}

func (s *stage) Children() []gunim.Node { return []gunim.Node{s.term} }

func (s *stage) Layout(c gunim.Constraints, f gunim.Frame, kids gunim.Children) geom.Size {
	s.output.Copy(func(yield func(*Term) bool) { yield(s.term) })
	k := kids.At(0)
	k.Layout(gunim.Tight(c.Max))
	k.Place(geom.Point{})
	return c.Max
}

func (s *stage) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	kids.At(0).Paint(p)
}

// Update takes the window's UI, which the tests ask and tell things of.
func (s *stage) Update(u *gunim.UI) {
	if s.u == nil {
		s.u = u
		u.Focus(s.term)
	}
}

// testTerm is a terminal in a window of its own.
type testTerm struct {
	*Term
	stage *stage
	win   *gunim.Window
}

// onStage puts a terminal on sess in a window of size, with the
// keyboard.
func onStage(t *testing.T, size geom.Size, sess session.Session, hooks screen.Hooks) *testTerm {
	t.Helper()
	hooks.Output, hooks.Title = func() {}, func(string) {}
	hooks.Exit, hooks.Clipboard = func() {}, func(string) {}
	sh := screen.Open(sess, vt.DefaultPalette(), hooks)
	t.Cleanup(func() { _ = sh.T.Close() })
	tm := &testTerm{stage: &stage{term: New(sh, ui.NewKeymap())}}
	tm.Term = tm.stage.term
	// Painted each frame: the tests ask where a node stands.
	tm.win = gunimtest.New(t, size, nil)
	tm.win.PaintWhenLooked(false)
	gunim.RegisterView(tm.win, "stage", func(struct{}) *stage { return tm.stage },
		func(s *stage, _ struct{}, u *gunim.UI) { s.Update(u) })
	if err := tm.win.Client().Mount(gunim.Root, "stage", "stage", struct{}{}); err != nil {
		t.Fatal(err)
	}
	tm.frames(3)
	return tm
}

// frames draws n frames.
func (tm *testTerm) frames(n int) {
	for range n {
		tm.win.Frame(time.Second / 60)
	}
}

// await draws frames until ok, which waits on what a goroutine of the
// terminal's own does.
func (tm *testTerm) await(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); !ok(); {
		if time.Now().After(end) {
			t.Fatal(what)
		}
		time.Sleep(5 * time.Millisecond)
		tm.frames(1)
	}
	tm.Sync()
	tm.frames(1)
}

// The click that gives a pane the keyboard only does that; the next
// one reaches the terminal.
func TestTheClickThatFocusesAPaneOnlyFocusesIt(t *testing.T) {
	tm := onStage(t, geom.Sz(900, 600), sessiontest.New(), screen.Hooks{})
	box, _ := tm.stage.u.Bounds(tm.Term)
	tm.stage.u.Focus(nil)
	tm.frames(1)
	click := func() input.MouseButton {
		tm.win.Input(gi.PointerDown{Pos: box.Center(), Button: gi.ButtonPrimary, Clicks: 1})
		held := tm.held
		tm.win.Input(gi.PointerUp{Pos: box.Center(), Button: gi.ButtonPrimary})
		tm.frames(1)
		return held
	}
	if held := click(); held != input.MouseNone || !tm.focused {
		t.Fatalf("the focusing click held %v, focused %v", held, tm.focused)
	}
	if held := click(); held != input.MouseLeft {
		t.Fatalf("the next click held %v, want the left button", held)
	}
}

// A pane without the keyboard draws no cursor; with it, one, lit again
// by each key.
func TestOnlyThePaneWithTheKeyboardDrawsACursor(t *testing.T) {
	tm := onStage(t, geom.Sz(900, 600), sessiontest.New(), screen.Hooks{})
	if !tm.cursorShown {
		t.Fatal("the pane with the keyboard shows no cursor")
	}
	tm.blinkOff = true
	tm.win.Input(gi.TextInput{Text: "x"})
	// Lit by the key, for a whole half of the blink.
	for i := range 29 {
		tm.frames(1)
		if tm.blinkOff {
			t.Fatalf("the cursor went off %d frames after a key", i+1)
		}
	}
	tm.stage.u.Focus(nil)
	tm.frames(1)
	if tm.cursorShown {
		t.Fatal("the pane without the keyboard shows a cursor")
	}
}

// A pane too small for a usable screen still resizes the shell, once it
// has stayed so a moment.
func TestATinyPaneResizesItsShellOnceSettled(t *testing.T) {
	tm := onStage(t, geom.Sz(900, 40), sessiontest.New(), screen.Hooks{})
	if size := tm.sh.T.Size(); size.Cols != screen.Cols || size.Rows != screen.Rows {
		t.Fatalf("the shell was given %dx%d at once", size.Cols, size.Rows)
	}
	tm.frames(30)
	cols, rows := tm.cells.Fit()
	if cols >= LeastCols && rows >= LeastRows {
		t.Fatalf("the pane fits %dx%d, which is not small", cols, rows)
	}
	if size := tm.sh.T.Size(); size.Cols != cols || size.Rows != rows {
		t.Fatalf("settled, the shell is %dx%d, want %dx%d", size.Cols, size.Rows, cols, rows)
	}
}

// The pointer over a terminal is the I-beam, and a hand over a link
// while Ctrl is down.
func TestTheHandShowsOverALinkWithCtrl(t *testing.T) {
	tm := onStage(t, geom.Sz(900, 600), sessiontest.NewPrinted([]byte("https://example.com/a")), screen.Hooks{Link: func(string) {}})
	// The link is printed on a goroutine of its own: waited for, not
	// given a number of frames, which a slow machine may not print in.
	tm.await(t, "the link was never printed", func() bool { return strings.Contains(tm.sh.T.AllText(), "https://example.com/a") })
	tm.frames(30)
	box, _ := tm.stage.u.Bounds(tm.cells)
	cell := tm.cells.CellSize()
	at := box.Min.Add(geom.Pt(cell.W*3, cell.H/2))
	tm.win.Input(gi.PointerMove{Pos: at})
	tm.frames(2)
	if got := tm.win.Offscreen().Cursor(); got != gi.CursorText {
		t.Fatalf("over the text the pointer is %v, want the I-beam", got)
	}
	tm.win.Input(gi.KeyPress{Key: gi.KeyLeftControl})
	tm.frames(2)
	if got := tm.win.Offscreen().Cursor(); got != gi.CursorHand {
		t.Fatalf("over a link with Ctrl down the pointer is %v, want the hand", got)
	}
}

// The shell's output moving the cursor lights it, and the blink goes on
// after: it once stopped there, lit, until the window lost the keyboard
// and got it back.
func TestTheBlinkGoesOnAfterOutputMovesTheCursor(t *testing.T) {
	tm := onStage(t, geom.Sz(900, 600), sessiontest.New(), screen.Hooks{})
	toggles := func() int {
		n, was := 0, tm.blinkOff
		for range 120 {
			tm.frames(1)
			if tm.blinkOff != was {
				n, was = n+1, tm.blinkOff
			}
		}
		return n
	}
	if n := toggles(); n < 2 {
		t.Fatalf("the cursor blinked %d times in two seconds, before any output", n)
	}
	// The pane loses the keyboard and gets it back between two steps of
	// the blink: the cursor is lit for a whole half from there, not put
	// out by the step still to come.
	for lit := false; !lit; {
		was := tm.blinkOff
		tm.frames(1)
		lit = was && !tm.blinkOff
	}
	tm.frames(20)
	tm.stage.u.Focus(nil)
	tm.stage.u.Focus(tm.Term)
	for i := range 29 {
		tm.frames(1)
		if tm.blinkOff {
			t.Fatalf("the cursor went off %d frames after the pane got the keyboard back", i+1)
		}
	}
	for i := range 3 {
		tm.sh.T.Say("output")
		tm.stage.output.Arrived(tm.stage.u)
		tm.frames(1)
		if tm.blinkOff {
			t.Fatalf("output %d left the cursor in the off half", i)
		}
		if n := toggles(); n < 2 {
			t.Fatalf("after output %d the cursor blinked %d times in two seconds", i, n)
		}
	}
}

// A screen somebody watching has sized bigger than the pane is drawn
// whole, shrunk to fit and centred, and a click lands on the cell drawn
// under it.
func TestAHeldScreenBiggerThanThePaneIsDrawnToFit(t *testing.T) {
	tm := onStage(t, geom.Sz(900, 600), sessiontest.New(), screen.Hooks{})
	tm.sh.T.Hold(200, 60)
	tm.Sync()
	tm.stage.u.Invalidate()
	tm.frames(3)
	if tm.scale >= 1 || tm.scale <= 0 {
		t.Fatalf("a 200 by 60 screen in a %v pane is drawn at scale %v", tm.win.Offscreen().Size(), tm.scale)
	}
	if cols, rows := tm.cells.GridSize(); cols != 200 || rows != 60 {
		t.Fatalf("the cells are %d by %d, want the whole screen", cols, rows)
	}
	box, _ := tm.stage.u.Bounds(tm.Term)
	cell := tm.cells.CellSize()
	// The last cell of the screen, drawn shrunk, is where a click on it
	// lands.
	at := geom.Pt(tm.offset.X+(199.5*cell.W)*tm.scale, tm.offset.Y+(59.5*cell.H)*tm.scale)
	if got := tm.cellAt(at); got.X != 199 || got.Y != 59 {
		t.Fatalf("a click on the last cell, at %v in %v, lands on %v", at, box, got)
	}
	tm.sh.T.Release()
	tm.Sync()
	tm.stage.u.Invalidate()
	tm.frames(3)
	if tm.scale != 1 {
		t.Fatalf("let go, the screen is still drawn at %v", tm.scale)
	}
}

func TestAnImageInTheOutputIsDrawnOverItsCells(t *testing.T) {
	var file bytes.Buffer
	if err := png.Encode(&file, image.NewRGBA(image.Rect(0, 0, 40, 20))); err != nil {
		t.Fatal(err)
	}
	out := "before\r\n\x1b]1337;File=inline=1;width=4;height=2:" + base64.StdEncoding.EncodeToString(file.Bytes()) + "\x07after\r\n"
	tm := onStage(t, geom.Sz(900, 600), sessiontest.NewPrinted([]byte(out)), screen.Hooks{})
	tm.await(t, "the terminal never took the image", func() bool { return len(tm.sh.T.Images()) > 0 })
	at := tm.sh.T.Images()[0]
	var drawn *paint.ImageOp
	for _, op := range tm.win.Offscreen().Ops() {
		if im, ok := op.(*paint.ImageOp); ok {
			drawn = im
		}
	}
	if drawn == nil {
		t.Fatal("the image was never painted")
	}
	if w, h := drawn.Image.Size(); w != 40 || h != 20 {
		t.Fatalf("painted a %dx%d image, want the 40x20 one", w, h)
	}
	cell := tm.cells.CellSize()
	want := geom.Rc(float32(at.Col)*cell.W, float32(at.Top)*cell.H, 4*cell.W, 2*cell.H)
	if drawn.Rect != want || at.Top != 1 {
		t.Fatalf("painted in %v, want the four by two cells under the first line, %v", drawn.Rect, want)
	}
}
