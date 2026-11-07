package app

import (
	"slices"
	"testing"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/driver"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
	"github.com/marrasen/gunim/widget/dock"
)

// tabsWith returns the panes in the group of pane id, in order.
func tabsWith(a *app, id string) []string {
	g := a.layout.GroupOf(id)
	if g == nil {
		return nil
	}
	var out []string
	for _, k := range g.Kids {
		out = append(out, k.ID)
	}
	return out
}

// sameTabs reports whether panes x and y are tabs of one group.
func sameTabs(a *app, x, y string) bool {
	g := a.layout.GroupOf(x)
	return g != nil && g == a.layout.GroupOf(y)
}

// inSplit reports whether panes x and y stand in one window, each in a
// group of its own.
func inSplit(a *app, x, y string) bool {
	wx, okx := a.layout.Where(x)
	wy, oky := a.layout.Where(y)
	return okx && oky && wx == wy && !sameTabs(a, x, y)
}

// shown returns the panes window w shows.
func shown(a *app, w *ownWin) []string {
	a.keepFocusShown()
	return a.layout.Shown(w.id)
}

// New panes in a window are tabs of the group with the keyboard, each
// in front as it comes, and each window has its own.
func TestNewPanesAreTabsOfTheGroupWithTheKeyboard(t *testing.T) {
	a, one, two := twoWindowApp(t)
	if got := tabsWith(a, "p1"); !slices.Equal(got, []string{"p1", "p2"}) {
		t.Fatalf("the first window's tabs are %v", got)
	}
	if got := shown(a, one); !slices.Equal(got, []string{"p2"}) {
		t.Fatalf("the first window shows %v", got)
	}
	if got := shown(a, two); !slices.Equal(got, []string{"p3"}) {
		t.Fatalf("the second window shows %v", got)
	}
}

// The new tab joins the group with the keyboard, in a split: not the
// other part of it.
func TestANewTabJoinsThePartOfTheSplitWithTheKeyboard(t *testing.T) {
	a, one, _ := twoWindowApp(t)
	a.front(one)
	a.handle(MovePane{Pane: "p2", Beside: "p1"})
	a.focus("p1")
	a.addPane(Pane{ID: "p4", Kind: KindFileManager}, nil, Placement{})
	if !sameTabs(a, "p4", "p1") || sameTabs(a, "p4", "p2") {
		t.Fatalf("p4 stands with %v", tabsWith(a, "p4"))
	}
}

// With the keyboard in the Machines pane, a new tab goes beside it.
func TestANewTabStaysOutOfTheMachinesPane(t *testing.T) {
	a, one, _ := twoWindowApp(t)
	a.front(one)
	a.addPane(Pane{ID: "m", Kind: KindServers}, nil, Placement{Beside: "p1"})
	a.focus("m")
	a.addPane(Pane{ID: "p4", Kind: KindFileManager}, nil, Placement{})
	if sameTabs(a, "p4", "m") || !sameTabs(a, "p4", "p1") {
		t.Fatalf("p4 stands with %v", tabsWith(a, "p4"))
	}
}

// A pane with the keyboard stands in front of its group.
func TestThePaneWithTheKeyboardIsInFront(t *testing.T) {
	a, one, _ := twoWindowApp(t)
	a.front(one)
	a.focus("p1")
	if got := shown(a, one); !slices.Equal(got, []string{"p1"}) {
		t.Fatalf("the window shows %v", got)
	}
	st := a.stateFor(one, a.st)
	if st.Focus != "p1" {
		t.Fatalf("the focus is on %q", st.Focus)
	}
}

// Next Tab goes round the tabs of the group with the keyboard, and Back
// the other way.
func TestNextTabGoesRound(t *testing.T) {
	a, one, _ := twoWindowApp(t)
	a.front(one)
	a.focus("p1")
	a.handle(NextTab{})
	if a.st.Focus != "p2" {
		t.Fatalf("next: the focus is on %q", a.st.Focus)
	}
	a.handle(NextTab{})
	if a.st.Focus != "p1" {
		t.Fatalf("next, round: the focus is on %q", a.st.Focus)
	}
	a.handle(NextTab{Back: true})
	if a.st.Focus != "p2" {
		t.Fatalf("back: the focus is on %q", a.st.Focus)
	}
}

// Moving a tab goes along its bar, stopping at the ends.
func TestShiftTabMovesAlongTheBar(t *testing.T) {
	a, one, _ := twoWindowApp(t)
	a.front(one)
	a.focus("p1")
	a.handle(ShiftTab{})
	if got := tabsWith(a, "p1"); !slices.Equal(got, []string{"p2", "p1"}) {
		t.Fatalf("right: the tabs are %v", got)
	}
	a.handle(ShiftTab{})
	if got := tabsWith(a, "p1"); !slices.Equal(got, []string{"p2", "p1"}) {
		t.Fatalf("right again, past the end: the tabs are %v", got)
	}
	a.handle(ShiftTab{Back: true})
	if got := tabsWith(a, "p1"); !slices.Equal(got, []string{"p1", "p2"}) {
		t.Fatalf("left: the tabs are %v", got)
	}
}

// A tab dropped along its own bar lands where it was let go.
func TestATabDraggedAlongTheBarLandsThere(t *testing.T) {
	a, one, _ := twoWindowApp(t)
	a.front(one)
	g := a.layout.GroupOf("p1").ID
	a.handle(MoveTab{Pane: "p1", To: dock.Target{At: g, Index: 2}})
	if got := tabsWith(a, "p1"); !slices.Equal(got, []string{"p2", "p1"}) || a.st.Focus != "p1" {
		t.Fatalf("the tabs are %v, the focus on %q", got, a.st.Focus)
	}
}

// A tab dropped on another window's bar moves there, and the window it
// left gives the keyboard to the tab beside.
func TestATabMovesToAnotherWindow(t *testing.T) {
	a, one, two := twoWindowApp(t)
	a.front(two)
	g := a.layout.GroupOf("p3").ID
	a.front(one)
	a.focus("p2")
	a.handle(MoveTab{Pane: "p2", To: dock.Target{At: g, Index: 0}})
	if a.ownerOf("p2") != two || !slices.Equal(tabsWith(a, "p2"), []string{"p2", "p3"}) {
		t.Fatalf("p2 is in window %d with %v", a.ownerOf("p2").id, tabsWith(a, "p2"))
	}
	if a.focusIn(one) != "p1" {
		t.Fatalf("the window it left has %q in front", a.focusIn(one))
	}
}

// A tab dropped at a pane's edge splits it, on that side, wherever the
// tab came from.
func TestATabDroppedAtAPanesEdgeSplitsIt(t *testing.T) {
	a, one, _ := twoWindowApp(t)
	a.front(one)
	a.handle(MoveTab{Pane: "p2", To: dock.Target{At: "p1", Side: dock.Bottom}})
	if !inSplit(a, "p1", "p2") {
		t.Fatalf("p1 stands with %v, p2 with %v", tabsWith(a, "p1"), tabsWith(a, "p2"))
	}
	root := a.layout.Window(one.id).Root
	if root.Kind != dock.Column || frontOf(root.Kids[0]) != "p1" || frontOf(root.Kids[1]) != "p2" {
		t.Fatalf("the window's root is %+v", root)
	}
	if a.st.Focus != "p2" {
		t.Fatalf("the focus is on %q", a.st.Focus)
	}
}

// A split's divider keeps where it was let go.
func TestASplitKeepsItsShare(t *testing.T) {
	a, one, _ := twoWindowApp(t)
	a.front(one)
	a.handle(MovePane{Pane: "p2", Beside: "p1"})
	split := a.layout.Window(one.id).Root
	a.handle(SplitMoved{Split: split.ID, Share: 0.3})
	if split = a.layout.Window(one.id).Root; split.Share != 0.3 {
		t.Fatalf("the share is %v", split.Share)
	}
}

// Closing a pane shows the tab beside it: the one after, or the last's
// the one before.
func TestClosingATabShowsTheOneBeside(t *testing.T) {
	a, one, _ := twoWindowApp(t)
	a.front(one)
	a.addPane(Pane{ID: "p4", Kind: KindFileManager}, nil, Placement{})
	a.focus("p2")
	a.handle(ClosePane{Pane: "p2"})
	if a.st.Focus != "p4" {
		t.Fatalf("closed, the focus is on %q, want p4, after it", a.st.Focus)
	}
	a.handle(ClosePane{Pane: "p4"})
	if a.st.Focus != "p1" {
		t.Fatalf("closed, the focus is on %q, want p1, before it", a.st.Focus)
	}
}

// Closing a pane in a split gives the keyboard to the pane nearest on
// the other side, showing the tab it has in front.
func TestClosingAPaneInASplitGivesTheKeysAcross(t *testing.T) {
	a, one, _ := twoWindowApp(t)
	a.front(one)
	a.handle(MovePane{Pane: "p2", Beside: "p1"})
	a.handle(ClosePane{Pane: "p2"})
	if a.has("p2") || a.st.Focus != "p1" {
		t.Fatalf("closed, p2 stays %v, the focus on %q", a.has("p2"), a.st.Focus)
	}
	if root := a.layout.Window(one.id).Root; root.Kind != dock.Group {
		t.Fatalf("the split stays: %+v", root)
	}
}

// A tab let go outside every window opens one of its own; a window's
// only tab stays.
func TestATabLetGoOutsideOpensAWindow(t *testing.T) {
	a, one, two := twoWindowApp(t)
	opened := 0
	a.openWindow = func(_ *gunim.Window, _ geom.Point, s geom.Size, _ *driver.Placement) (gunim.Client, *gunim.Window, error) {
		opened++
		return gunimtest.New(t, s, nil).Client(), nil, nil
	}
	a.front(two)
	a.handle(PaneToNewWindow{Pane: "p3"})
	a.front(one)
	a.handle(PaneToNewWindow{Pane: "p2"})
	select {
	case f := <-a.events:
		f()
	case <-time.After(time.Second):
		t.Fatal("no window opened")
	}
	if opened != 1 || len(a.wins) != 3 || winNum(a, "p2") != a.cur.id || a.st.Focus != "p2" {
		t.Fatalf("%d opened, %d windows, p2 in window %d, the focus on %q", opened, len(a.wins), winNum(a, "p2"), a.st.Focus)
	}
}

// winNum is the window pane id stands in.
func winNum(a *app, id string) int {
	n, _ := a.paneWin(id)
	return n
}

// All Panes sees what a window shows, then each pane behind.
func TestAllPanesSeesWhatShowsAndWhatIsBehind(t *testing.T) {
	a, one, _ := twoWindowApp(t)
	a.front(one)
	a.addPane(Pane{ID: "p4", Kind: KindFileManager}, nil, Placement{Beside: "p2", Vertical: true})
	a.focus("p2")
	a.keepFocusShown()
	tabs := OverTabs(&a.layout, one.id)
	if len(tabs) != 2 {
		t.Fatalf("the tabs are %+v", tabs)
	}
	if b := tabs[0].Box; !b.Vertical || b.A.Pane != "p2" || b.B.Pane != "p4" {
		t.Fatalf("what shows is %+v", b)
	}
	if b := tabs[1].Box; b.Pane != "p1" {
		t.Fatalf("what is behind is %+v", b)
	}
}

// A + on a bar puts the tab in that group, the Machines pane's too.
func TestAPlusPutsTheTabInItsGroup(t *testing.T) {
	a, one, _ := twoWindowApp(t)
	a.front(one)
	a.addPane(Pane{ID: "m", Kind: KindServers}, nil, Placement{Beside: "p1"})
	g := a.layout.GroupOf("m").ID
	a.focus("m")
	a.addPane(Pane{ID: "p4", Kind: KindFileManager}, nil, Placement{Group: g})
	if !sameTabs(a, "p4", "m") {
		t.Fatalf("p4 stands with %v", tabsWith(a, "p4"))
	}
}

// Closing a tab behind another, with nothing to ask about, leaves the
// keyboard where it is.
func TestClosingATabBehindLeavesTheKeyboard(t *testing.T) {
	a, one, _ := twoWindowApp(t)
	a.front(one)
	a.focus("p2")
	a.handle(ClosePane{Pane: "p1"})
	waitFor(t, a, "p1 to close", func() bool { return !a.has("p1") })
	if a.st.Focus != "p2" {
		t.Fatalf("the focus went to %q", a.st.Focus)
	}
}
