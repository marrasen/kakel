package guestview

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"

	"github.com/marrasen/kakel/termview"
)

// hostStage is inSplit on a page of a size, the host's panes laid out in
// room, its cells as large as the page's own at some size of text, and
// each terminal as many cells as its pane has room for at the host.
func hostStage(t *testing.T, page, room geom.Size) (*stage, geom.Size) {
	t.Helper()
	s := newStage(t, page, inSplit())
	s.settle()
	// A cell the page's terminals have, as the host's is at its size of
	// text with the same font.
	cell := s.page.win.panes["p1"].term.Cells().CellSize()
	s.show(func(st *State) { st.Room, st.Cell = room, cell })
	s.settle()
	s.hostSizes(cell)
	return s, cell
}

// hostSizes gives each terminal shown the columns and rows its pane has
// room for, at the host, with cells of cell.
func (s *stage) hostSizes(cell geom.Size) {
	s.t.Helper()
	w := s.page.win
	pad := termview.Padding.Get(s.u.Theme())
	for id, v := range w.panes {
		r, ok := w.dock.FinalRect(id)
		if v.term == nil || !ok {
			continue
		}
		cols := int((r.Size().W - 2*pad) / cell.W)
		rows := int((r.Size().H - 2*pad) / cell.H)
		v.term.Shell().T.Hold(cols, rows)
		v.term.Sync()
	}
	s.u.Invalidate()
	s.settle()
}

// The host's window is drawn whole, scaled as one picture to fit the page
// in its middle: every terminal fills its pane, with text of the one size,
// the host's.
func TestTheHostsWindowIsDrawnWholeAtOneSize(t *testing.T) {
	s, cell := hostStage(t, geom.Sz(1100, 640), geom.Sz(1600, 900))
	s.shot("picture")
	w := s.page.win
	pc := w.picture
	if pc.laid != geom.Sz(1600, 900) {
		t.Fatalf("the panes are laid out at %v", pc.laid)
	}
	page, _ := s.u.Bounds(pc)
	if want := min((page.Size().W-32)/1600, (page.Size().H-32)/900); math.Abs(float64(pc.scale-want)) > 0.001 {
		t.Errorf("the picture is scaled by %v, want %v", pc.scale, want)
	}
	drawn, _ := s.u.Bounds(w.tabs)
	if left, right := drawn.Min.X-page.Min.X, page.Max.X-drawn.Max.X; math.Abs(float64(left-right)) > 1 {
		t.Errorf("the picture has %v to its left and %v to its right", left, right)
	}
	if math.Abs(float64(drawn.Size().W-1600*pc.scale)) > 1 {
		t.Errorf("the picture is drawn %v wide", drawn.Size().W)
	}
	pad := termview.Padding.Get(s.u.Theme())
	sizes := map[float32]bool{}
	for _, id := range []string{"p1", "p3"} {
		tm := w.panes[id].term
		sizes[tm.Cells().Size] = true
		if got := tm.Cells().CellSize(); math.Abs(float64(got.W-cell.W)) > 0.01 || math.Abs(float64(got.H-cell.H)) > 0.01 {
			t.Errorf("%s has cells of %v, the host's are %v", id, got, cell)
		}
		r, _ := w.dock.FinalRect(id)
		box := geom.Sz(r.Size().W-2*pad, r.Size().H-2*pad)
		cols, rows := tm.Cells().GridSize()
		at, c := tm.Drawn()
		screen := geom.Sz(float32(cols)*c.W, float32(rows)*c.H)
		if screen.W > box.W+0.5 || screen.H > box.H+0.5 || box.W-screen.W >= c.W || box.H-screen.H >= c.H || at.X < 0 || at.Y < 0 {
			t.Errorf("%s's screen of %v, at %v, does not fill its pane of %v", id, screen, at, box)
		}
	}
	if len(sizes) != 1 {
		t.Errorf("the terminals' text has the sizes %v", sizes)
	}
	// The guest's ring is round the pane it is in, the host's round the
	// host's, and each pane has an edge.
	if pc.guest.id != "p1" || pc.guest.lit.Value() < 0.99 || pc.host.id != "p1" || pc.host.lit.Value() < 0.99 {
		t.Errorf("the guest's ring is round %q, lit %v, the host's round %q, lit %v", pc.guest.id, pc.guest.lit.Value(), pc.host.id, pc.host.lit.Value())
	}
	l := w.dock.Arrangement()
	if f := w.dock.Face(l.Node("p1")); f.Badge != "Anna" || f.BadgeInk != &hostInk {
		t.Errorf("the host's tab says %+v", f)
	}
	if f := w.dock.Face(l.Node("p3")); f.Badge != "" {
		t.Errorf("another tab says %q", f.Badge)
	}
}

// A pointer reaches what is under it in the picture, through its scale:
// a tab, and a cell of a terminal.
func TestAPointerReachesThroughThePicture(t *testing.T) {
	s, _ := hostStage(t, geom.Sz(1100, 640), geom.Sz(1600, 900))
	w := s.page.win
	pc := w.picture
	dockAt, _ := s.u.Bounds(w.dock)
	toPage := func(r geom.Rect) geom.Rect {
		return geom.Rect{Min: dockAt.Min.Add(geom.Pt(r.Min.X*pc.scale, r.Min.Y*pc.scale)), Max: dockAt.Min.Add(geom.Pt(r.Max.X*pc.scale, r.Max.Y*pc.scale))}
	}
	tab, ok := w.dock.TabRect("p2")
	if !ok {
		t.Fatal("p2 has no tab")
	}
	s.clickAt(toPage(tab).Center())
	s.settle()
	if w.dock.Active() != "p2" {
		t.Fatalf("a click on p2's tab has the guest in %q", w.dock.Active())
	}
	if pc.guest.id != "p2" {
		t.Errorf("the guest's ring is round %q", pc.guest.id)
	}
	// A double click on the word "p3" of the line "this is p3" picks it.
	v := w.panes["p3"]
	termAt, _ := s.u.Bounds(v.term)
	at, cell := v.term.Drawn()
	word := termAt.Min.Add(geom.Pt((at.X+8.5*cell.W)*pc.scale, (at.Y+0.5*cell.H)*pc.scale))
	for i := 1; i <= 2; i++ {
		s.win.Input(gi.PointerDown{Pos: word, Button: gi.ButtonPrimary, Clicks: i, Time: time.Now()})
		s.frames(1)
		s.win.Input(gi.PointerUp{Pos: word, Button: gi.ButtonPrimary, Time: time.Now()})
		s.frames(1)
	}
	s.settle()
	s.shot("picture-apart")
	if got := v.term.Shell().T.SelectionText(); got != "p3" {
		t.Errorf("a double click on p3 picked %q", got)
	}
	if w.dock.Active() != "p3" {
		t.Errorf("a click in p3 has the guest in %q", w.dock.Active())
	}
}

// As the host resizes its window the picture glides to its new size,
// every frame a little nearer, and the terminals' text keeps its size.
func TestThePictureFollowsTheHostsSize(t *testing.T) {
	s, cell := hostStage(t, geom.Sz(1100, 640), geom.Sz(1600, 900))
	w := s.page.win
	pc := w.picture
	size := w.panes["p1"].term.Cells().Size
	s.show(func(st *State) { st.Room = geom.Sz(1000, 900) })
	last := pc.laid.W
	for i := range 40 {
		if i%4 == 0 {
			s.shot(fmt.Sprintf("picture-resize-%02d", i))
		}
		if pc.laid.W > last+0.01 || last-pc.laid.W > 200 {
			t.Fatalf("frame %d: the panes went from %v wide to %v", i, last, pc.laid.W)
		}
		last = pc.laid.W
		s.frames(1)
	}
	s.settle()
	s.hostSizes(cell)
	if pc.laid != geom.Sz(1000, 900) {
		t.Errorf("at rest the panes are laid out at %v", pc.laid)
	}
	if got := w.panes["p1"].term.Cells().Size; got != size {
		t.Errorf("the text went from %v to %v", size, got)
	}
}

// A narrow page shows one pane at a time, as before, its terminal as
// large as the page's width has room for; with the room it is the host's
// picture again. Both ways it glides.
func TestANarrowPageShowsOnePaneOfThePicture(t *testing.T) {
	s, cell := hostStage(t, geom.Sz(1100, 640), geom.Sz(1600, 900))
	w := s.page.win
	pc := w.picture
	s.win.Offscreen().Resize(geom.Sz(390, 700))
	var ons []float32
	for i := range 30 {
		s.frames(1)
		if i%3 == 0 {
			s.shot(fmt.Sprintf("picture-narrow-%02d", i))
		}
		ons = append(ons, pc.on.Value())
	}
	s.settle()
	if ons[0] < 0.5 || pc.on.Value() != 0 || pc.scale != 1 {
		t.Fatalf("on the way the picture was %v, at rest %v, scaled by %v", ons, pc.on.Value(), pc.scale)
	}
	if got := w.dock.Shown(); len(got) != 1 {
		t.Fatalf("a narrow page shows %v", got)
	}
	id := w.dock.Shown()[0]
	tm := w.panes[id].term
	r, _ := w.dock.FinalRect(id)
	cols, _ := tm.Cells().GridSize()
	box := r.Size().W - 2*termview.Padding.Get(s.u.Theme())
	if c := tm.Cells().CellSize(); float32(cols)*c.W > box+0.5 || box-float32(cols)*c.W > float32(cols) {
		t.Errorf("%d columns of %v fill %v of the page's %v", cols, c, float32(cols)*c.W, box)
	}
	s.win.Offscreen().Resize(geom.Sz(1100, 640))
	s.frames(1)
	if on := pc.on.Value(); on > 0.5 {
		t.Errorf("at the first frame back the picture is %v", on)
	}
	s.settle()
	if pc.on.Value() != 1 || w.panes["p1"].term.Cells().CellSize() != cell {
		t.Errorf("back, the picture is %v, its cells %v", pc.on.Value(), w.panes["p1"].term.Cells().CellSize())
	}
}
