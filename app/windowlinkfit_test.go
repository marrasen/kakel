package app

import (
	"slices"
	"strings"
	"testing"

	"github.com/marrasen/kakel/guest"
	"github.com/marrasen/kakel/termview"
	"github.com/marrasen/kakel/ui"
)

// laid gives pane id's terminal the room the window has for it.
func (r *linkRig) laid(id string, cols, rows int) {
	r.t.Helper()
	t := r.a.terminal(id)
	if t == nil {
		r.t.Fatalf("no terminal %s", id)
	}
	t.Layout(ui.Size{Cols: cols, Rows: rows})
}

// sizeOf is pane id's terminal's size, and whether somebody else has it.
func (r *linkRig) sizeOf(id string) (ui.Size, bool) {
	t := r.a.terminal(id)
	return t.Size(), t.Held()
}

// at waits for pane id to be cols by rows, held or not.
func (r *linkRig) at(id string, cols, rows int, held bool) {
	r.t.Helper()
	r.until("the size of "+id, func() bool {
		size, h := r.sizeOf(id)
		return size == ui.Size{Cols: cols, Rows: rows} && h == held
	})
}

// The guest's page sets a terminal's size only while the share says so,
// and the window's size is back as it stops.
func TestAGuestsPageSetsTheSizeOnlyWhileItMay(t *testing.T) {
	r := newLinkRig(t)
	r.laid("p1", 90, 25)
	l := r.share(true)
	v := r.visit(l.Link)
	v.in(l.ID, true)
	v.state("the window", func(st guest.State) bool { return len(st.Panes) == 3 && !st.GuestSizes })
	p, err := v.open("p1")
	if err != nil {
		t.Fatal(err)
	}
	v.s.Shown([]string{"p1"})
	p.Fit(100, 30)
	// Typed after it: once that is there, the size has been looked at.
	_, _ = p.Write([]byte("after"))
	r.until("what was typed after", func() bool { return strings.Contains(r.progs["p1"].sent(), "after") })
	if size, held := r.sizeOf("p1"); held || size != (ui.Size{Cols: 90, Rows: 25}) {
		t.Fatalf("sized for the window, the terminal is %v, held %v", size, held)
	}

	r.do(SetLinkSize{ID: l.ID, ForGuest: true})
	v.state("the guest sets the size", func(st guest.State) bool { return st.GuestSizes })
	p.Fit(100, 30)
	r.at("p1", 100, 30, true)
	if got := r.only().Sizes; !slices.Equal(got, []string{"p1"}) {
		t.Errorf("the share says it sizes %v", got)
	}
	v.state("the pane's new size", func(st guest.State) bool {
		return slices.ContainsFunc(st.Panes, func(p guest.Pane) bool { return p.ID == "p1" && p.Cols == 100 && p.Rows == 30 })
	})
	if size, held := r.sizeOf("p2"); held && size == (ui.Size{Cols: 100, Rows: 30}) {
		t.Error("a pane the guest did not size was sized")
	}

	r.do(SetLinkSize{ID: l.ID})
	if size, held := r.sizeOf("p1"); held || size != (ui.Size{Cols: 90, Rows: 25}) {
		t.Errorf("back to the window's size, the terminal is %v, held %v", size, held)
	}
	if got := r.only().Sizes; len(got) != 0 {
		t.Errorf("the share says it sizes %v", got)
	}

	// A size of a cell is no smaller than the window gives one.
	r.do(SetLinkSize{ID: l.ID, ForGuest: true})
	v.state("the guest sets the size again", func(st guest.State) bool { return st.GuestSizes })
	p.Fit(1, 1)
	r.at("p1", termview.LeastCols, termview.LeastRows, true)
}

// A guest that says no size, leaves, or whose share is stopped, gives the
// terminals the window's sizes again.
func TestTheWindowsSizesComeBack(t *testing.T) {
	for _, how := range []string{"no size", "leaves", "stopped"} {
		t.Run(how, func(t *testing.T) {
			r := newLinkRig(t)
			r.laid("p1", 90, 25)
			r.do(ShareWindow{Window: r.one.id, Name: "Anna", CanType: true, ForGuest: true, Relay: r.url})
			r.until("the link", func() bool { return len(r.a.windowLinks()) == 1 && r.only().Stands == LinkWaiting })
			l := r.only()
			v := r.visit(l.Link)
			v.in(l.ID, true)
			v.state("the guest sets the size", func(st guest.State) bool { return st.GuestSizes })
			p, err := v.open("p1")
			if err != nil {
				t.Fatal(err)
			}
			v.s.Shown([]string{"p1"})
			p.Fit(110, 35)
			r.at("p1", 110, 35, true)
			switch how {
			case "no size":
				p.Fit(0, 0)
			case "leaves":
				go v.s.Leave()
			case "stopped":
				r.do(StopLink{ID: l.ID})
			}
			r.at("p1", 90, 25, false)
		})
	}
}

// Two guests who size one terminal have it the smaller of the two, each
// way, and the other's alone once one stops.
func TestTwoGuestsHaveTheSmallerSize(t *testing.T) {
	r := newLinkRig(t)
	r.laid("p1", 90, 25)
	first, second := r.share(true), r.share(true)
	a, b := r.visit(first.Link), r.visit(second.Link)
	a.in(first.ID, true)
	b.in(second.ID, true)
	for _, l := range []WindowLink{first, second} {
		r.do(SetLinkSize{ID: l.ID, ForGuest: true})
	}
	a.state("the first sets the size", func(st guest.State) bool { return st.GuestSizes })
	b.state("the second sets the size", func(st guest.State) bool { return st.GuestSizes })
	pa, err := a.open("p1")
	if err != nil {
		t.Fatal(err)
	}
	pb, err := b.open("p1")
	if err != nil {
		t.Fatal(err)
	}
	a.s.Shown([]string{"p1"})
	b.s.Shown([]string{"p1"})
	pa.Fit(120, 30)
	pb.Fit(80, 40)
	r.at("p1", 80, 30, true)
	r.do(StopLink{ID: second.ID})
	r.at("p1", 120, 30, true)
}

// Whose size the terminals have is offered next as it was chosen last.
func TestTheNextShareIsSizedAsTheLast(t *testing.T) {
	r := newLinkRig(t)
	if r.a.linkSetup().ForGuest {
		t.Fatal("at first the guest sets the size")
	}
	r.do(ShareWindow{Window: r.one.id, Name: "Anna", CanType: true, ForGuest: true, Relay: r.url})
	if !r.a.linkSetup().ForGuest {
		t.Error("a share sized for the guest is not offered again")
	}
	if err := r.a.setLinkSetup(SetLinkSetup{Name: "Anna", Relay: r.url}); err != nil || r.a.linkSetup().ForGuest {
		t.Errorf("set back, it is %+v: %v", r.a.linkSetup(), err)
	}
}

// A terminal another kakel watches has that kakel's size, the guest is told
// so, and the guest's size comes back as it stops watching.
func TestAWatchedTerminalKeepsItsWatchersSize(t *testing.T) {
	r := newLinkRig(t)
	r.laid("p1", 90, 25)
	r.do(ShareWindow{Window: r.one.id, Name: "Anna", CanType: true, ForGuest: true, Relay: r.url})
	r.until("the link", func() bool { return len(r.a.windowLinks()) == 1 && r.only().Stands == LinkWaiting })
	l := r.only()
	v := r.visit(l.Link)
	v.in(l.ID, true)
	p, err := v.open("p1")
	if err != nil {
		t.Fatal(err)
	}
	v.s.Shown([]string{"p1"})
	p.Fit(110, 35)
	r.at("p1", 110, 35, true)
	hostSized := func(st guest.State) bool {
		i := slices.IndexFunc(st.Panes, func(p guest.Pane) bool { return p.ID == "p1" })
		return i >= 0 && st.Panes[i].HostSized
	}
	if hostSized(v.s.View().State) {
		t.Error("a terminal at the guest's size is said to be at the host's")
	}
	w, err := r.a.watchPane(r.a.terminal("p1"), 60, 20)
	if err != nil {
		t.Fatal(err)
	}
	// As a watcher comes from the program's own goroutine.
	r.a.publish()
	r.at("p1", 60, 20, true)
	v.state("the guest told it has the host's size", hostSized)
	if got := r.only().Sizes; len(got) != 0 {
		t.Errorf("the share says it sizes %v", got)
	}
	_ = w.Close()
	r.at("p1", 110, 35, true)
	v.state("the guest told it has its size again", func(st guest.State) bool { return !hostSized(st) })
}
