package termview

import (
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"

	"github.com/marrasen/kakel/grid"
	"github.com/marrasen/kakel/internal/sessiontest"
	"github.com/marrasen/kakel/screen"
	"github.com/marrasen/kakel/vt"
)

// finger is a finger at a terminal, at cells of it.
type finger struct {
	t  *testing.T
	tm *testTerm
}

func fingerAt(t *testing.T, printed string) finger {
	t.Helper()
	tm := onStage(t, geom.Sz(700, 240), sessiontest.NewPrinted([]byte(printed)), screen.Hooks{})
	tm.win.TimeByFrames(true)
	tm.await(t, "the program's output never arrived", func() bool { return strings.TrimSpace(topRowOf(tm.sh)) != "" })
	return finger{t, tm}
}

// cell is the middle of a cell, in the window.
func (f finger) cell(col, row int) geom.Point {
	box, _ := f.tm.stage.u.Bounds(f.tm.Term)
	at, cell := f.tm.Drawn()
	return box.Min.Add(at).Add(geom.Pt((float32(col)+0.5)*cell.W, (float32(row)+0.5)*cell.H))
}

func (f finger) down(p geom.Point, clicks int) {
	f.tm.win.Input(gi.PointerDown{Pos: p, Button: gi.ButtonPrimary, Clicks: clicks, Touch: true, Time: time.Now()})
	f.tm.frames(1)
}

func (f finger) move(p geom.Point) {
	f.tm.win.Input(gi.PointerMove{Pos: p, Touch: true, Time: time.Now()})
	f.tm.frames(1)
}

func (f finger) up(p geom.Point) {
	f.tm.win.Input(gi.PointerUp{Pos: p, Button: gi.ButtonPrimary, Touch: true, Time: time.Now()})
	f.tm.frames(1)
}

// ground reports whether a cell is outside the selection, as it shows.
func (f finger) ground(col, row int) (out bool) {
	f.tm.sh.Drawn(func(g *grid.Grid) { out = g.BGOf(col, row) != vt.DefaultPalette().Selection })
	return out
}

// wait lets a finger that is down be held.
func (f finger) wait() { f.tm.frames(40) }

func (f finger) selected(when, want string) {
	f.t.Helper()
	if got := f.tm.sh.T.SelectionText(); got != want {
		f.t.Fatalf("%s %q is selected, want %q", when, got, want)
	}
}

// A finger's press does what it does as it lifts from a tap. One that
// scrolls leaves what is selected.
func TestAFingersPressWaitsForItsTap(t *testing.T) {
	f := fingerAt(t, "one two three\r\n"+strings.Repeat("more\r\n", 40)+"four five six")
	f.tm.await(t, "the end never came", func() bool { return strings.Contains(f.tm.sh.T.Text(), "six") })
	_, rows := f.tm.cells.GridSize()
	word := f.cell(6, rows-1)
	tap := func(clicks int) {
		f.down(word, clicks)
		f.up(word)
	}
	tap(1)
	f.down(word, 2)
	f.selected("with the finger down", "")
	f.up(word)
	f.selected("once a double tap has lifted", "five")

	f.down(f.cell(1, 2), 1)
	for i := range 10 {
		f.move(f.cell(1, 2).Add(geom.Pt(0, float32(i)*12)))
		f.selected("as a finger scrolls", "five")
	}
	f.up(f.cell(1, 2).Add(geom.Pt(0, 120)))
	f.selected("once it has scrolled", "five")
	if !f.ground(6, rows-1) {
		t.Error("it did not scroll")
	}
	f.tm.frames(120)
	f.down(f.cell(1, 1), 1)
	f.up(f.cell(1, 1))
	f.selected("after a tap elsewhere", "")

	// A tap that brings the keyboard does nothing else, as a click.
	f.tm.stage.u.Focus(nil)
	f.tm.TextInput = true
	f.tm.frames(1)
	f.down(f.cell(1, 1), 2)
	f.up(f.cell(1, 1))
	f.selected("after a tap that brought the keyboard", "")
	if !f.tm.focused {
		t.Error("a tap brought no keyboard")
	}
}

// A finger held selects the word under it, or keeps the selection it is
// in, and what it is dragged over; its menu comes as it lifts.
func TestAFingerHeldSelects(t *testing.T) {
	f := fingerAt(t, "one two three four")
	var menus []geom.Point
	hold := func(col int) {
		f.down(f.cell(col, 0), 1)
		f.wait()
	}
	// With no menu it is the window's press.
	hold(5)
	f.up(f.cell(5, 0))
	f.selected("with no menu", "")

	box, _ := f.tm.stage.u.Bounds(f.tm.Term)
	f.tm.OnHeld = func(at geom.Point, _ *gunim.UI) { menus = append(menus, box.Min.Add(at)) }
	hold(5)
	f.selected("held", "two")
	if len(menus) != 0 {
		t.Fatal("the menu came before the finger lifted")
	}
	f.move(f.cell(10, 0))
	f.selected("dragged on", "two three")
	f.up(f.cell(10, 0))
	if len(menus) != 1 || menus[0] != f.cell(10, 0) {
		t.Fatalf("the menus are at %v, want one at %v", menus, f.cell(10, 0))
	}
	f.wait()

	// In the selection, which it keeps whichever way it is dragged.
	hold(5)
	f.selected("held in the selection", "two three")
	f.move(f.cell(6, 0))
	f.selected("dragged in it", "two three")
	f.move(f.cell(16, 0))
	f.selected("dragged on from it", "two three four")
	f.move(f.cell(0, 0))
	f.selected("dragged back over it", "one two three")
	f.up(f.cell(0, 0))
	if len(menus) != 2 {
		t.Fatalf("%d menus came, want 2", len(menus))
	}
	// The touch is over: a pointer over it selects nothing.
	f.tm.win.Input(gi.PointerMove{Pos: f.cell(17, 0), Time: time.Now()})
	f.selected("after it", "one two three")
}

// A program with the mouse hears a finger's tap as a click, and one held
// as a click of the right button.
func TestAProgramWithTheMouseHearsAFinger(t *testing.T) {
	sess := sessiontest.NewPrinted([]byte("\x1b[?1000h\x1b[?1006hready"))
	tm := onStage(t, geom.Sz(700, 240), sess, screen.Hooks{})
	tm.win.TimeByFrames(true)
	f := finger{t, tm}
	tm.OnHeld = func(geom.Point, *gunim.UI) { t.Error("a menu came over a program with the mouse") }
	tm.await(t, "the program never asked for the mouse", func() bool { return tm.sh.T.MouseTaken(0) })
	f.down(f.cell(2, 0), 1)
	if got := sess.Sent(); got != "" {
		t.Errorf("with the finger down it heard %q", got)
	}
	f.up(f.cell(2, 0))
	const tap = "\x1b[<0;3;1M\x1b[<0;3;1m"
	tm.await(t, "it never heard the tap", func() bool { return sess.Sent() == tap })
	f.down(f.cell(4, 0), 1)
	f.wait()
	f.up(f.cell(4, 0))
	tm.await(t, "it never heard the finger held", func() bool { return sess.Sent() == tap+"\x1b[<2;5;1M\x1b[<2;5;1m" })
}
