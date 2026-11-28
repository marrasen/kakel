package guestview

import (
	"testing"
	"time"

	"github.com/marrasen/gunim/geom"
)

// fitsOf are the sizes the host was asked for pane id, over every time it
// was open.
func (s *fakeSession) fitsOf(id string) [][2]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out [][2]int
	for _, p := range s.panes {
		if p.id == id {
			p.mu.Lock()
			out = append(out, p.fits...)
			p.mu.Unlock()
		}
	}
	return out
}

// lastFit is the size the host was last asked for pane id.
func (s *fakeSession) lastFit(id string) ([2]int, bool) {
	all := s.fitsOf(id)
	if len(all) == 0 {
		return [2]int{}, false
	}
	return all[len(all)-1], true
}

// The page tells the host what fits a pane only where the share has the
// guest set the size, and only of the panes on the screen: one that goes
// off it has the host's size again.
func TestThePageSaysWhatFitsOnlyWhereItSetsTheSize(t *testing.T) {
	quick(t)
	s := newFake(in("p1", "p2"))
	c := newCourse(t, true, nil, s)
	c.on(PageWindow)
	c.do(Shown{[]string{"p1", "p2"}})
	c.until("both open", func() bool { return len(s.opened()) == 2 })
	c.do(Fits{Pane: "p1", Cols: 100, Rows: 30})
	c.do(Fits{Pane: "p2", Cols: 50, Rows: 20})
	time.Sleep(20 * time.Millisecond)
	if a, b := s.fitsOf("p1"), s.fitsOf("p2"); len(a)+len(b) > 0 {
		t.Fatalf("while the host sets the size, the page asked for %v and %v", a, b)
	}

	v := in("p1", "p2")
	v.State.GuestSizes = true
	s.say(v)
	c.until("both asked for", func() bool {
		a, _ := s.lastFit("p1")
		b, _ := s.lastFit("p2")
		return a == [2]int{100, 30} && b == [2]int{50, 20}
	})
	c.do(Shown{[]string{"p1"}})
	c.until("p2 has the host's size", func() bool { b, _ := s.lastFit("p2"); return b == [2]int{} })
	c.do(Fits{Pane: "p2", Cols: 60, Rows: 20})
	c.do(Fits{Pane: "p1", Cols: 90, Rows: 25})
	c.until("p1 asked for again", func() bool { a, _ := s.lastFit("p1"); return a == [2]int{90, 25} })
	if b, _ := s.lastFit("p2"); b != [2]int{} {
		t.Errorf("a pane off the screen was asked for at %v", b)
	}

	// Back to the host's sizes, nothing more is asked.
	s.say(in("p1", "p2"))
	c.until("the host sets the size", func() bool { return !c.state().GuestSizes })
	n := len(s.fitsOf("p1"))
	c.do(Fits{Pane: "p1", Cols: 80, Rows: 20})
	time.Sleep(20 * time.Millisecond)
	if got := s.fitsOf("p1"); len(got) != n {
		t.Errorf("with the host setting the size the page asked for %v", got[n:])
	}
}

// However often the page's size changes, a pane is asked for no more often
// than paces.fits, and at the latest.
func TestThePageAsksForSizesAtAPace(t *testing.T) {
	quick(t)
	paces.fits = 50 * time.Millisecond
	v := in("p1")
	v.State.GuestSizes = true
	s := newFake(v)
	c := newCourse(t, true, nil, s)
	c.on(PageWindow)
	c.do(Shown{[]string{"p1"}})
	c.until("open", func() bool { return len(s.opened()) == 1 })
	start := time.Now()
	for i := range 200 {
		c.do(Fits{Pane: "p1", Cols: 40 + i, Rows: 10})
	}
	c.until("the latest", func() bool { a, _ := s.lastFit("p1"); return a == [2]int{239, 10} })
	if n, most := len(s.fitsOf("p1")), int(time.Since(start)/paces.fits)+2; n > most {
		t.Errorf("asked %d times in %v, more than %d", n, time.Since(start), most)
	}
}

// Where the page sets the size, the panes glide to fill the page, keep
// their edges and rings, their text the page's own size, and each shown
// terminal says what it has room for; back to the host's size, the host's
// window comes whole again.
func TestThePageSetsTheSizeOfItsPanes(t *testing.T) {
	s, _ := hostStage(t, geom.Sz(1100, 640), geom.Sz(1600, 900))
	w := s.page.win
	pc := w.picture
	s.intents()
	s.show(func(st *State) { st.GuestSizes = true })
	for i := range 3 {
		s.frames(3)
		s.shot("guest-sizing-" + string(rune('1'+i)))
	}
	if on := pc.on.Value(); on <= 0.01 || on >= 0.99 {
		t.Errorf("the picture is %v of the way at once", on)
	}
	s.settle()
	s.shot("guest-sized")
	page, _ := s.u.Bounds(pc)
	if pc.on.Value() > 0.01 || pc.lines.Value() < 0.99 || pc.laid != page.Size() || pc.scale != 1 {
		t.Errorf("the panes are laid out at %v, scaled %v, framed %v, lined %v", pc.laid, pc.scale, pc.on.Value(), pc.lines.Value())
	}
	if pc.guest.lit.Value() < 0.99 {
		t.Error("the guest's pane has no ring")
	}
	fits := map[string]Fits{}
	said := map[string][]Fits{}
	for _, in := range s.intents() {
		if f, ok := in.(Fits); ok {
			fits[f.Pane] = f
			said[f.Pane] = append(said[f.Pane], f)
		}
	}
	for id, all := range said {
		if len(all) != 1 {
			t.Errorf("%s said what fits it %d times on the way: %+v", id, len(all), all)
		}
	}
	for _, id := range []string{"p1", "p3"} {
		tm := w.panes[id].term
		if tm.Fitted() {
			t.Errorf("%s's text is fitted to the host's screen", id)
		}
		box, _ := s.u.Bounds(tm)
		cell := tm.Cells().CellSize()
		cols, rows := int(box.Size().W/cell.W), int(box.Size().H/cell.H)
		if f := fits[id]; f.Cols != cols || f.Rows != rows || cols < 1 {
			t.Errorf("%s has room for %dx%d, and said %+v", id, cols, rows, f)
		}
	}
	if _, said := fits["p2"]; said {
		t.Error("a tab behind another said what fits it")
	}
	// The host takes the size: the screen fills the pane, from its top left.
	for _, id := range []string{"p1", "p3"} {
		tm := w.panes[id].term
		f := fits[id]
		tm.Shell().T.Hold(f.Cols, f.Rows)
		tm.Sync()
	}
	s.u.Invalidate()
	s.settle()
	s.shot("guest-sized-held")
	if at, cell := w.panes["p1"].term.Drawn(); at != (geom.Point{}) || cell != w.panes["p1"].term.Cells().CellSize() {
		t.Errorf("the screen is drawn at %v, with cells of %v", at, cell)
	}

	s.show(func(st *State) { st.GuestSizes = false })
	s.settle()
	if pc.on.Value() < 0.99 || !w.panes["p1"].term.Fitted() {
		t.Errorf("back to the host's size, the picture is %v of the way", pc.on.Value())
	}
}

// On a narrow page the one pane shown fills it, and says what fits it.
func TestANarrowPageSetsTheSizeOfThePaneShown(t *testing.T) {
	st := inWindow("p1", "p2")
	st.GuestSizes = true
	s := newStage(t, geom.Sz(420, 760), st)
	s.settle()
	s.shot("guest-sized-narrow")
	w := s.page.win
	if !w.narrow || len(w.dock.Shown()) != 1 {
		t.Fatalf("the page shows %v", w.dock.Shown())
	}
	var fits []Fits
	for _, in := range s.intents() {
		if f, ok := in.(Fits); ok {
			fits = append(fits, f)
		}
	}
	shown := w.dock.Shown()[0]
	tm := w.panes[shown].term
	box, _ := s.u.Bounds(tm)
	cell := tm.Cells().CellSize()
	if len(fits) == 0 || fits[len(fits)-1].Pane != shown || fits[len(fits)-1].Cols != int(box.Size().W/cell.W) {
		t.Errorf("the pane of %v said %+v", box.Size(), fits)
	}
	for _, f := range fits {
		if f.Pane != shown {
			t.Errorf("%s, not shown, said %+v", f.Pane, f)
		}
	}
}

// A terminal the host keeps at its own size, where the page sets the
// others', is drawn as the host's, fitted to its pane, and says so.
func TestATerminalAtTheHostsSizeSaysSo(t *testing.T) {
	s, _ := hostStage(t, geom.Sz(1100, 640), geom.Sz(1600, 900))
	w := s.page.win
	s.show(func(st *State) {
		st.GuestSizes = true
		st.Panes[0].HostSized = true
	})
	s.settle()
	s.shot("guest-sized-host")
	p1, p3 := w.panes["p1"], w.panes["p3"]
	if !p1.term.Fitted() || p1.hostSized.Hidden() {
		t.Errorf("at the host's size, p1 is fitted %v, its note hidden %v", p1.term.Fitted(), p1.hostSized.Hidden())
	}
	if p3.term.Fitted() || !p3.hostSized.Hidden() {
		t.Error("p3, at the page's size, is fitted or says it is the host's")
	}
	for _, in := range s.intents() {
		if f, ok := in.(Fits); ok && f.Pane == "p1" {
			t.Errorf("p1 said what fits it: %+v", f)
		}
	}
	s.show(func(st *State) { st.Panes[0].HostSized = false })
	s.settle()
	if p1.term.Fitted() || !p1.hostSized.Hidden() {
		t.Error("back at the page's size, p1 is still the host's")
	}
}
