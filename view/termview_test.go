package view

import (
	"strings"
	"testing"
	"time"

	"github.com/marrasen/kakel/app"

	"github.com/marrasen/kakel/internal/sessiontest"
	"github.com/marrasen/kakel/screen"
	"github.com/marrasen/kakel/termview"
	"github.com/marrasen/kakel/ui"
	"github.com/marrasen/kakel/winkeys"

	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"

	"github.com/marrasen/kakel/input"
	"github.com/marrasen/kakel/vt"
)

// termStage puts one terminal pane on stage, with the keyboard, in a
// window of size.
func termStage(t *testing.T, size geom.Size) (*Window, *term) {
	t.Helper()
	win, sh, publish := windowStageOf(t, size)
	quiet := screen.Hooks{Output: func() {}, Title: func(string) {}, Exit: func() {}, Clipboard: func(string) {}}
	sh.Set("p1", screen.Open(sessiontest.New(), vt.DefaultPalette(), quiet))
	t.Cleanup(func() { _ = sh.Get("p1").T.Close() })
	publish(app.State{Panes: []app.Pane{{ID: "p1", Title: "Terminal 1"}}, Layout: boxLayout(&app.Box{Pane: "p1"}), Focus: "p1"})
	return win, win.terms["p1"]
}

// hoverMods are the modifiers the pointer is over tm with.
func hoverMods(tm *term) input.Mods {
	mods, _ := tm.Hover()
	return mods
}

func frames(n int) {
	for range n {
		lastWindow.Frame(time.Second / 60)
	}
}

// Ctrl going down over a still pointer is heard, as a move would be.
func TestCtrlOverAStillPointerIsHeard(t *testing.T) {
	win, tm := termStage(t, geom.Sz(900, 600))
	box, _ := lastUI.Bounds(tm)
	lastWindow.Input(gi.PointerMove{Pos: box.Center()})
	frames(1)
	lastWindow.Input(gi.KeyPress{Key: gi.KeyLeftControl})
	if hoverMods(tm) != input.ModCtrl {
		t.Fatalf("with Ctrl down, the hover's modifiers are %v", hoverMods(tm))
	}
	lastWindow.Input(gi.KeyRelease{Key: gi.KeyLeftControl, Mods: gi.ModControl})
	if hoverMods(tm) != 0 {
		t.Fatalf("with Ctrl up, the hover's modifiers are %v", hoverMods(tm))
	}
	// With the keyboard elsewhere, the pane under the pointer still
	// hears it.
	st := withServers(app.State{Panes: []app.Pane{{ID: "p1", Title: "Terminal 1"}}, Layout: boxLayout(&app.Box{Pane: "p1"}), Focus: "p1"})
	if err := lastWindow.Client().Publish(app.WindowTopic, st); err != nil {
		t.Fatal(err)
	}
	frames(60)
	box, _ = lastUI.Bounds(tm)
	lastWindow.Input(gi.PointerMove{Pos: box.Center()})
	lastUI.Focus(win.serversRow(lastUI))
	frames(1)
	if tm.Focused() {
		t.Fatal("the terminal kept the keyboard")
	}
	lastWindow.Input(gi.KeyPress{Key: gi.KeyLeftControl})
	if hoverMods(tm) != input.ModCtrl {
		t.Fatalf("with the keyboard elsewhere and Ctrl down, the hover's modifiers are %v", hoverMods(tm))
	}
	lastWindow.Input(gi.KeyRelease{Key: gi.KeyLeftControl, Mods: gi.ModControl})
	// Once the pointer has gone, Ctrl leaves the pane alone.
	lastWindow.Input(gi.PointerMove{Pos: geom.Pt(20, 300)})
	frames(1)
	lastWindow.Input(gi.KeyPress{Key: gi.KeyLeftControl})
	if _, over := tm.Hover(); over || hoverMods(tm) != 0 {
		t.Fatalf("with the pointer gone, the pane is over it %v with modifiers %v", over, hoverMods(tm))
	}
}

// A click outside the palette closes it, and the keyboard goes where
// the click landed.
func TestAClickOutsideThePaletteClosesIt(t *testing.T) {
	win, tm := termStage(t, geom.Sz(900, 600))
	win.run("palette.open", lastUI)
	frames(20)
	if !win.palette.IsOpen() {
		t.Fatal("the palette did not open")
	}
	box, _ := lastUI.Bounds(tm)
	lastWindow.Input(gi.PointerDown{Pos: box.Center(), Button: gi.ButtonPrimary, Clicks: 1})
	lastWindow.Input(gi.PointerUp{Pos: box.Center(), Button: gi.ButtonPrimary})
	frames(20)
	if win.palette.IsOpen() {
		t.Fatal("a click on the terminal left the palette open")
	}
	if !tm.Focused() {
		t.Fatal("after the click, the terminal has no keyboard")
	}
}

// The theme picker closes on a click outside it, as the palette does.
func TestAClickOutsideTheThemePickerClosesIt(t *testing.T) {
	win, tm := termStage(t, geom.Sz(900, 600))
	win.themes = []string{"one", "two"}
	win.pickTheme(lastUI)
	frames(20)
	if !win.themePicker.IsOpen() {
		t.Fatal("the theme picker did not open")
	}
	box, _ := lastUI.Bounds(tm)
	lastWindow.Input(gi.PointerDown{Pos: box.Center(), Button: gi.ButtonPrimary, Clicks: 1})
	lastWindow.Input(gi.PointerUp{Pos: box.Center(), Button: gi.ButtonPrimary})
	frames(20)
	if win.themePicker.IsOpen() || !tm.Focused() {
		t.Fatalf("after a click on the terminal, the picker is open %v, the terminal has the keyboard %v", win.themePicker.IsOpen(), tm.Focused())
	}
}

// On a Swedish keyboard the key marked + sits where a US one has -.
// Ctrl and that key makes the font bigger, as the key says.
func TestPunctuationIsTheKeyItTypes(t *testing.T) {
	for _, c := range []struct {
		press gi.KeyPress
		want  input.Key
	}{
		{gi.KeyPress{Key: gi.KeyMinus, Char: '+'}, input.KeyPlus},
		{gi.KeyPress{Key: gi.KeySlash, Char: '-'}, input.KeyMinus},
		{gi.KeyPress{Key: gi.KeyMinus, Char: '-'}, input.KeyMinus},
		{gi.KeyPress{Key: gi.KeyMinus}, input.KeyMinus},
		{gi.KeyPress{Key: gi.KeyA, Char: 'a'}, input.KeyA},
	} {
		ev, ok := winkeys.Event(c.press)
		if !ok || ev.Key != c.want {
			t.Errorf("%+v reads as %v, %v; want %v", c.press, ev.Key, ok, c.want)
		}
	}
	ev, _ := winkeys.Event(gi.KeyPress{Key: gi.KeyMinus, Char: '+', Mods: gi.ModControl})
	if id, _ := Shortcuts().Lookup(ui.ChordOf(ev)); id != "font.increase" {
		t.Fatalf("Ctrl and the key marked + runs %q", id)
	}
}

// A press that another program took the release of, as a browser a
// Ctrl+click opened does, ends as the window loses the keyboard, and no
// link stays lit or named.
func TestLosingTheKeyboardEndsAPressAndTheLinksLight(t *testing.T) {
	_, tm := termStage(t, geom.Sz(900, 600))
	box, _ := lastUI.Bounds(tm)
	lastWindow.Input(gi.PointerMove{Pos: box.Center()})
	lastWindow.Input(gi.KeyPress{Key: gi.KeyLeftControl})
	lastWindow.Input(gi.PointerDown{Pos: box.Center(), Button: gi.ButtonPrimary, Clicks: 1, Mods: gi.ModControl})
	frames(1)
	if tm.Pressed() == input.MouseNone || hoverMods(tm) == 0 {
		t.Fatalf("pressed with Ctrl, held %v, modifiers %v", tm.Pressed(), hoverMods(tm))
	}
	lastWindow.Input(gi.WindowFocusLost{})
	frames(1)
	if tm.Pressed() != input.MouseNone || hoverMods(tm) != 0 || tm.Shell().T.HoveredLink() != "" {
		t.Fatalf("away, held %v, modifiers %v, link %q", tm.Pressed(), hoverMods(tm), tm.Shell().T.HoveredLink())
	}
}

// A terminal pane whose shell has gone by the time the window shows it,
// as a connection's closed when the connection is given up, is drawn
// blank rather than crashing the window.
func TestAPaneWhoseShellHasGoneIsBlank(t *testing.T) {
	win, _, publish := windowStageOf(t, geom.Sz(900, 600))
	publish(app.State{Panes: []app.Pane{{ID: "p9", Title: "gone"}}, Layout: boxLayout(&app.Box{Pane: "p9"}), Focus: "p9"})
	frames(2)
	if _, ok := win.terms["p9"]; ok {
		t.Fatal("a terminal was made for a pane with no shell")
	}
}

// A pane too small for a usable screen still resizes the shell, once it
// has stayed so a moment.
func TestATinyPaneResizesItsShellOnceSettled(t *testing.T) {
	_, tm := termStage(t, geom.Sz(900, 75))
	// Until the sidebar has slid into place.
	frames(90)
	cols, rows := tm.Cells().Fit()
	if cols >= termview.LeastCols && rows >= termview.LeastRows {
		t.Fatalf("the pane fits %dx%d, which is not small", cols, rows)
	}
	if size := tm.Shell().T.Size(); size.Cols != cols || size.Rows != rows {
		t.Fatalf("settled, the shell is %dx%d, want %dx%d", size.Cols, size.Rows, cols, rows)
	}
}

// The pointer over a terminal is the I-beam, and a hand over a link
// while Ctrl is down.
func TestTheHandShowsOverALinkWithCtrl(t *testing.T) {
	win, sh, publish := windowStageOf(t, geom.Sz(900, 600))
	linked := screen.Hooks{Output: func() {}, Title: func(string) {}, Exit: func() {}, Clipboard: func(string) {}, Link: func(string) {}}
	sh.Set("p1", screen.Open(sessiontest.NewPrinted([]byte("https://example.com/a")), vt.DefaultPalette(), linked))
	t.Cleanup(func() { _ = sh.Get("p1").T.Close() })
	publish(app.State{Panes: []app.Pane{{ID: "p1", Title: "Terminal 1"}}, Layout: boxLayout(&app.Box{Pane: "p1"}), Focus: "p1"})
	tm := win.terms["p1"]
	// The link is printed on a goroutine of its own: waited for, not
	// given a number of frames, which a slow machine may not print in.
	for i := 0; !strings.Contains(sh.Get("p1").T.AllText(), "https://example.com/a"); i++ {
		if i > 500 {
			t.Fatal("the link was never printed")
		}
		frames(1)
		time.Sleep(5 * time.Millisecond)
	}
	frames(30)
	box, _ := lastUI.Bounds(tm.Cells())
	cell := tm.Cells().CellSize()
	at := box.Min.Add(geom.Pt(cell.W*3, cell.H/2))
	lastWindow.Input(gi.PointerMove{Pos: at})
	frames(2)
	if got := lastWindow.Offscreen().Cursor(); got != gi.CursorText {
		t.Fatalf("over the text the pointer is %v, want the I-beam", got)
	}
	lastWindow.Input(gi.KeyPress{Key: gi.KeyLeftControl})
	frames(2)
	if got := lastWindow.Offscreen().Cursor(); got != gi.CursorHand {
		t.Fatalf("over a link with Ctrl down the pointer is %v, want the hand", got)
	}
}

// A shell's output reaches its pane through the window, and moving the
// cursor lights it, with the blink going on after.
func TestTheWindowsOutputLightsTheCursorAndTheBlinkGoesOn(t *testing.T) {
	win, tm := termStage(t, geom.Sz(900, 600))
	toggles := func() int {
		n, was := 0, tm.CursorLit()
		for range 120 {
			frames(1)
			if tm.CursorLit() != was {
				n, was = n+1, tm.CursorLit()
			}
		}
		return n
	}
	for i := range 3 {
		// Out, so what lights it is the output.
		for j := 0; tm.CursorLit(); j++ {
			if j > 120 {
				t.Fatal("the cursor never blinks")
			}
			frames(1)
		}
		tm.Shell().T.Say("output")
		win.OutputArrived(lastUI)
		frames(1)
		if !tm.CursorLit() {
			t.Fatalf("output %d left the cursor in the off half", i)
		}
		if n := toggles(); n < 2 {
			t.Fatalf("after output %d the cursor blinked %d times in two seconds", i, n)
		}
	}
}
