package termview

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
	gi "github.com/marrasen/gunim/input"

	"github.com/marrasen/kakel/internal/sessiontest"
	"github.com/marrasen/kakel/screen"
)

// fitted puts a fitted terminal of cols by rows in a window of size.
func fitted(t *testing.T, size geom.Size, cols, rows int) *testTerm {
	t.Helper()
	tm := onStage(t, size, sessiontest.New(), screen.Hooks{})
	tm.SetFitted(true, tm.stage.u)
	tm.become(cols, rows)
	tm.frames(120)
	return tm
}

// become gives the screen the size the far end sets.
func (tm *testTerm) become(cols, rows int) {
	tm.sh.T.Hold(cols, rows)
	tm.Sync()
	tm.stage.u.Invalidate()
}

// shown is where the screen is drawn in the window.
func (tm *testTerm) shown() geom.Rect {
	cols, rows := tm.cells.GridSize()
	cell := tm.cells.CellSize()
	return geom.Rc(tm.offset.X, tm.offset.Y, float32(cols)*cell.W*tm.scale, float32(rows)*cell.H*tm.scale)
}

// A fitted screen keeps its size, whole in the middle of the pane, with
// its text as large as there is room for.
func TestAFittedScreenFillsThePaneAsFarAsItCan(t *testing.T) {
	for _, c := range []struct {
		size       geom.Size
		cols, rows int
	}{
		{geom.Sz(900, 600), 200, 60},
		{geom.Sz(1600, 900), 80, 24},
		{geom.Sz(320, 500), 120, 40},
		{geom.Sz(900, 600), 10, 60},
	} {
		tm := fitted(t, c.size, c.cols, c.rows)
		if size := tm.sh.T.Size(); size.Cols != c.cols || size.Rows != c.rows {
			t.Fatalf("a %dx%d screen became %dx%d", c.cols, c.rows, size.Cols, size.Rows)
		}
		if tm.scale != 1 {
			t.Errorf("at rest in %v, %dx%d is drawn scaled by %v", c.size, c.cols, c.rows, tm.scale)
		}
		at := tm.shown()
		if at.Min.X < 0 || at.Min.Y < 0 || at.Max.X > c.size.W+0.5 || at.Max.Y > c.size.H+0.5 {
			t.Errorf("in %v, %dx%d is drawn in %v, outside the pane", c.size, c.cols, c.rows, at)
		}
		if left, right := at.Min.X, c.size.W-at.Max.X; left-right > 1 || right-left > 1 {
			t.Errorf("in %v, %dx%d has %v to its left and %v to its right", c.size, c.cols, c.rows, left, right)
		}
		// A pixel more in each cell, one way or the other, is past the
		// pane: cells are whole pixels.
		cell := tm.cells.CellSize()
		if at.Size().W+float32(c.cols) <= c.size.W && at.Size().H+float32(c.rows) <= c.size.H && cell.H < 2*cell.W {
			t.Errorf("in %v, %dx%d is drawn in %v, with room for larger cells than %v", c.size, c.cols, c.rows, at, cell)
		}
	}
}

// A fitted screen given another size glides from where it stood to its
// new place, and its first frame is not there already. With KAKEL_SHOTS
// set the frames are written there too, to see it move.
func TestAFittedScreenGlidesToItsNewSize(t *testing.T) {
	dir := os.Getenv("KAKEL_SHOTS")
	tm := onStage(t, geom.Sz(900, 600), sessiontest.NewPrinted([]byte("$ ls\r\nAUTHORS  go.mod  main.go  README.md\r\n$ ")), screen.Hooks{})
	tm.await(t, "the program's output never arrived", func() bool { return strings.Contains(tm.sh.T.AllText(), "README") })
	tm.SetFitted(true, tm.stage.u)
	tm.become(80, 24)
	tm.frames(120)
	was := tm.shown()
	wasCell := tm.cells.CellSize().H
	tm.become(160, 48)
	tm.frames(1)
	if h := tm.cells.CellSize().H * tm.scale; h < wasCell*0.8 {
		t.Fatalf("at the first frame the cells went from %v tall to %v", wasCell, h)
	}
	// The cells shrink at every frame, to rest whole in the pane.
	last := wasCell
	for i := range 120 {
		h := tm.cells.CellSize().H * tm.scale
		if h > last+0.01 {
			t.Fatalf("frame %d: the cells grew from %v to %v on the way down", i, last, h)
		}
		last = h
		if dir != "" && i%3 == 0 && i < 30 {
			gunimtest.WritePNG(t, filepath.Join(dir, "fit-"+strconv.Itoa(i)+".png"), gunimtest.Pixels(t, tm.win))
		}
		tm.frames(1)
	}
	at := tm.shown()
	if tm.scale != 1 || at.Max.X > 900.5 || at.Max.Y > 600.5 || at == was {
		t.Fatalf("at rest it is drawn in %v, scaled by %v", at, tm.scale)
	}
}

// A screen glides from its own size to a fitted one, and back to the
// size of text it had, taking the cells that fit again.
func TestBecomingFittedAndBackGlides(t *testing.T) {
	tm := onStage(t, geom.Sz(900, 600), sessiontest.New(), screen.Hooks{})
	tm.frames(30)
	ownSize, own := tm.sh.T.Size(), tm.shown()
	ownCell, ownText := tm.cells.CellSize().H, tm.cells.Size
	// drawn is how tall the cells are drawn, and settles that they end
	// up at to, on the way there at every frame till then.
	drawn := func() float32 { return tm.cells.CellSize().H * tm.scale }
	settles := func(what string, from, to float32) {
		t.Helper()
		last := drawn()
		if d := (last - from) / (to - from); d > 0.5 {
			t.Fatalf("%s: at the first frame the cells are %v tall, of %v to %v", what, last, from, to)
		}
		between := 0
		for i := range 120 {
			tm.frames(1)
			h := drawn()
			if (h-last)*(to-from) < -0.01 {
				t.Fatalf("%s: frame %d went from %v to %v, on the way from %v to %v", what, i, last, h, from, to)
			}
			if h != last {
				between++
			}
			last = h
		}
		if between < 5 || tm.scale != 1 || drawn() != to {
			t.Fatalf("%s: after %d frames moving it is %v tall, scaled by %v, want %v", what, between, drawn(), tm.scale, to)
		}
	}

	tm.SetFitted(true, tm.stage.u)
	tm.become(200, 60)
	tm.frames(1)
	// What it comes to at rest, worked out at once.
	fittedCell := tm.cells.CellSize().H
	if fittedCell >= ownCell {
		t.Fatalf("fitted, the cells are %v tall, from %v", fittedCell, ownCell)
	}
	settles("on", ownCell, fittedCell)

	// The program sizes the text meanwhile: it shows once it is its own.
	tm.cells.Size = ownText + 3
	tm.frames(2)
	if h := drawn(); h != fittedCell {
		t.Fatalf("sized while fitted, the cells are %v tall, want %v still", h, fittedCell)
	}
	tm.cells.Size = ownText
	tm.frames(2)

	tm.SetFitted(false, tm.stage.u)
	tm.sh.T.Release()
	tm.Sync()
	tm.frames(1)
	settles("off", fittedCell, ownCell)
	tm.Sync()
	tm.frames(1)
	if got := tm.sh.T.Size(); got != ownSize || tm.shown() != own || tm.cells.Size != ownText {
		t.Fatalf("no longer fitted, it is %v in %v with text of %v, want %v in %v with %v", got, tm.shown(), tm.cells.Size, ownSize, own, ownText)
	}
}

// A click on a fitted screen lands on the cell drawn under it.
func TestAClickOnAFittedScreenLandsOnItsCell(t *testing.T) {
	tm := fitted(t, geom.Sz(900, 600), 60, 40)
	at := tm.shown()
	cell := tm.cells.CellSize()
	p := at.Min.Add(geom.Pt(59.5*cell.W, 39.5*cell.H))
	if got := tm.cellAt(p); got.X != 59 || got.Y != 39 {
		t.Fatalf("a click on the last cell lands on %v", got)
	}
	tm.win.Input(gi.PointerMove{Pos: p})
	tm.frames(1)
	if tm.at.X != 59 || tm.at.Y != 39 {
		t.Fatalf("the pointer is over cell %v", tm.at)
	}
}
