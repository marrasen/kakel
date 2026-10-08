package app

import (
	"testing"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/driver"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
)

// All Panes is shown every window, with its tabs and the size of its
// stage.
func TestTheOverviewShowsEveryWindow(t *testing.T) {
	a, one, two := twoWindowApp(t)
	a.overviewIn(one, StageSized{At: geom.Rc(0, 40, 640, 400), Window: geom.Sz(640, 460)})
	ov := a.overview()
	if len(ov) != 2 || ov[0].ID != one.id || ov[1].ID != two.id {
		t.Fatalf("the overview shows %+v", ov)
	}
	if len(ov[0].Tabs) != 2 || len(ov[1].Tabs) != 1 || ov[1].Tabs[0].Box.Pane != "p3" || ov[1].Tabs[0].Pane != "p3" {
		t.Fatalf("the windows' tabs are %+v and %+v", ov[0].Tabs, ov[1].Tabs)
	}
	if ov[0].Stage != geom.Sz(640, 400) || ov[0].StageAt != geom.Rc(0, 40, 640, 400) || ov[0].Size != geom.Sz(640, 460) || ov[0].Front != a.groupOf["p2"] {
		t.Fatalf("the first window's stage is %v, with group %d in front", ov[0].Stage, ov[0].Front)
	}
}

// A window behind saying its size, or that All Panes opened in it,
// stays behind.
func TestAWindowBehindSayingItsSizeStaysBehind(t *testing.T) {
	a, one, two := twoWindowApp(t)
	if !a.overviewIn(one, StageSized{At: geom.Rc(0, 0, 1, 2)}) || !a.overviewIn(one, OverviewShown{On: true}) {
		t.Fatal("the intents were not taken")
	}
	if a.cur != two || !one.overview || !a.overviewOpen() {
		t.Fatalf("the window in front is %d, All Panes open %v", a.cur.id, one.overview)
	}
}

// A pane dragged onto another window's card moves there, onto a tab of
// its own.
func TestAPaneDraggedOntoAWindowsCardMovesThere(t *testing.T) {
	a, one, two := twoWindowApp(t)
	a.handle(PaneToTab{Pane: "p1", Window: two.id})
	if a.winOf["p1"] != two.id || a.groupOf["p1"] == a.groupOf["p3"] {
		t.Fatalf("p1 is in window %d, group %d", a.winOf["p1"], a.groupOf["p1"])
	}
	if one.focus != "p2" && a.focusIn(one) != "p2" {
		t.Fatalf("the window it left has %q in front", a.focusIn(one))
	}
}

// A pane in a split, dragged onto its own window's card, leaves the
// split for a tab of its own.
func TestAPaneDraggedOntoItsOwnCardLeavesItsSplit(t *testing.T) {
	a, one, _ := twoWindowApp(t)
	a.front(one)
	a.handle(MovePane{Pane: "p2", Beside: "p1"})
	if a.groupOf["p1"] != a.groupOf["p2"] {
		t.Fatal("the split was not made")
	}
	a.handle(PaneToTab{Pane: "p2", Window: one.id})
	if a.groupOf["p1"] == a.groupOf["p2"] || a.winOf["p2"] != one.id {
		t.Fatal("p2 is still in p1's split")
	}
	if a.groups[a.groupOf["p1"]].Pane != "p1" || a.groups[a.groupOf["p2"]].Pane != "p2" {
		t.Fatal("the tabs are not one pane each")
	}
}

// A pane dropped on a pane of another window joins it in a split there,
// on the side it was dropped on.
func TestAPaneDockedBesideAnotherWindowsPaneJoinsItsSplit(t *testing.T) {
	a, one, two := twoWindowApp(t)
	a.front(one)
	a.handle(DockPane{Pane: "p1", Beside: "p3", Vertical: true, First: true})
	if a.winOf["p1"] != two.id || a.groupOf["p1"] != a.groupOf["p3"] {
		t.Fatalf("p1 is in window %d, group %d; p3 in group %d", a.winOf["p1"], a.groupOf["p1"], a.groupOf["p3"])
	}
	b := a.groups[a.groupOf["p3"]]
	if !b.Vertical || b.A == nil || b.A.Pane != "p1" || b.B == nil || b.B.Pane != "p3" {
		t.Fatalf("the split is %+v", b)
	}
	if got := ids(a.panesIn(one)); len(got) != 1 || got[0] != "p2" {
		t.Fatalf("the first window keeps %v", got)
	}
}

// A pane docked beside its own split's neighbour changes sides there.
func TestAPaneDockedInItsOwnSplitChangesSides(t *testing.T) {
	a, one, _ := twoWindowApp(t)
	a.front(one)
	a.handle(MovePane{Pane: "p2", Beside: "p1"})
	a.handle(DockPane{Pane: "p2", Beside: "p1", Vertical: true})
	b := a.groups[a.groupOf["p1"]]
	if a.groupOf["p2"] != a.groupOf["p1"] || !b.Vertical || b.A.Pane != "p1" || b.B.Pane != "p2" {
		t.Fatalf("the split is %+v", b)
	}
}

// A pane docked beside itself stays where it is.
func TestAPaneDockedBesideItselfStays(t *testing.T) {
	a, _, _ := twoWindowApp(t)
	g := a.groupOf["p3"]
	a.handle(DockPane{Pane: "p3", Beside: "p3"})
	if a.groupOf["p3"] != g || a.groups[g].Pane != "p3" {
		t.Fatal("p3 moved")
	}
}

// A maximized window stands over its monitor's work area, and a window
// stands in the overview's space as far from the monitor's corner as it
// is, in logical pixels.
func TestAWindowStandsInTheOverviewWhereItIsOnTheScreen(t *testing.T) {
	mons := []driver.Monitor{
		{Bounds: geom.Rc(0, 0, 1920, 1080), WorkArea: geom.Rc(0, 0, 1920, 1040), CoordsPerLogical: 1, Primary: true},
		{Bounds: geom.Rc(1920, 0, 2560, 1440), WorkArea: geom.Rc(1920, 0, 2560, 1400), CoordsPerLogical: 2},
	}
	if got := screenRect(driver.Placement{Bounds: geom.Rc(2000, 100, 800, 600), Maximized: true}, mons); got != mons[1].WorkArea {
		t.Fatalf("maximized on the second monitor, it stands at %v", got)
	}
	if got := screenRect(driver.Placement{Bounds: geom.Rc(10, 20, 300, 200)}, mons); got != geom.Rc(10, 20, 300, 200) {
		t.Fatalf("it stands at %v", got)
	}
	if got := inSpaceOf(geom.Rc(2120, 100, 400, 200), mons[1]); got != geom.Rc(100, 50, 200, 100) {
		t.Fatalf("on the second monitor, it is at %v in the overview", got)
	}
	if got := monitorAt(geom.Pt(5000, 5000), mons); got.Bounds != mons[1].Bounds {
		t.Fatalf("a point off every monitor is on %v", got.Bounds)
	}
}

// All Panes opens in a window of its own over the monitor the window in
// front is on; a pane picked there gets the keyboard, and its window
// comes to the front once All Panes has gone.
func TestAllPanesOpensInAWindowOfItsOwn(t *testing.T) {
	a, one, two := twoWindowApp(t)
	two.gw = gunimtest.New(t, geom.Sz(400, 300), nil)
	mon := driver.Monitor{Bounds: geom.Rc(0, 0, 1600, 1000), CoordsPerLogical: 1, Primary: true}
	a.monitors = func() []driver.Monitor { return []driver.Monitor{mon} }
	ow := gunimtest.New(t, geom.Sz(1600, 1000), nil)
	opened := make(chan driver.Monitor, 1)
	a.openOverview = func(m driver.Monitor) (gunim.Client, *gunim.Window, error) {
		opened <- m
		return ow.Client(), ow, nil
	}
	if !a.overviewAlone() {
		t.Fatal("All Panes would not open in a window of its own")
	}
	a.handle(ToggleOverview{})
	select {
	case f := <-a.events:
		f()
	case <-time.After(5 * time.Second):
		t.Fatal("the window did not open")
	}
	if got := <-opened; got.Bounds != mon.Bounds {
		t.Fatalf("it opened over %v", got.Bounds)
	}
	if a.over.c == nil || !a.overviewOpen() {
		t.Fatal("All Panes is not open")
	}
	a.handleOverviewWin(FocusPane{Pane: "p1"})
	if a.cur != one || a.st.Focus != "p1" || a.over.raise != one {
		t.Fatalf("picked, window %d is in front with %q", a.cur.id, a.st.Focus)
	}
	a.handleOverviewWin(OverviewDone{})
	if a.over.c != nil || a.overviewOpen() {
		t.Fatal("All Panes is still open")
	}
}

// Asked again while it is open, All Panes is told to close.
func TestAllPanesAskedAgainCloses(t *testing.T) {
	a, _, _ := twoWindowApp(t)
	ow := gunimtest.New(t, geom.Sz(1600, 1000), nil)
	c := ow.Client()
	a.over = overState{c: &c, gw: ow}
	a.handle(ToggleOverview{})
	if !a.over.closing {
		t.Fatal("All Panes was not told to close")
	}
}
