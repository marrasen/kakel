package app

import (
	"strconv"

	"github.com/marrasen/gunim/geom"
)

// All Panes shows every window's panes at once: a card for each of
// kakel's windows, holding its tabs, each split as it stands. A pane is
// picked there, or dragged onto another window's card, beside another
// pane, or out to a window of its own.

// OverWindow is one of kakel's windows as All Panes shows it.
type OverWindow struct {
	ID int
	// Tabs are its tabs, in order, and Front the group of the one in
	// front.
	Tabs  []OverTab
	Front int
	// Stage is the size of its stage, as it last said, in logical
	// pixels; zero before it has.
	Stage geom.Size
}

// OverTab is one tab of a window, how its panes are arranged, and Pane
// the one of them its tab names.
type OverTab struct {
	Group int
	Box   *Box
	Pane  string
}

// Intents for All Panes.
type (
	// StageSized says how large the stage of the window it came from
	// is, for All Panes in the others to draw its tabs that shape.
	StageSized struct{ Size geom.Size }
	// OverviewShown says All Panes opened, or closed with On false, in
	// the window it came from. While it is open the window hears of
	// output from every window's panes, not only its own.
	OverviewShown struct{ On bool }
	// PaneToTab moves a pane onto a tab of its own in the window
	// numbered Window. Asked of the window it is in, it takes the pane
	// out of its split onto a tab of its own there.
	PaneToTab struct {
		Pane   string
		Window int
	}
	// DockPane takes a pane into a split beside the pane Beside, in
	// whichever window that is: to its right, or below it with
	// Vertical, or to its left or above it with First.
	DockPane struct {
		Pane, Beside    string
		Vertical, First bool
	}
)

// overviewIn takes the intents about All Panes that leave the window
// in front as it is: a window behind says what it says without coming
// forward. It reports whether in was one.
func (a *app) overviewIn(w *ownWin, in any) bool {
	switch in := in.(type) {
	case StageSized:
		if w.stage == in.Size {
			a.quiet = true
			return true
		}
		w.stage = in.Size
		// Worth showing only where All Panes is open.
		a.quiet = !a.overviewOpen()
	case OverviewShown:
		w.overview = in.On
	default:
		return false
	}
	return true
}

// handleOverview carries out an intent of All Panes, and reports
// whether it was one.
func (a *app) handleOverview(in any) bool {
	switch in := in.(type) {
	case PaneToTab:
		a.paneToTab(in)
	case DockPane:
		a.dockPane(in)
	default:
		return false
	}
	return true
}

// overviewOpen reports whether All Panes is open in some window.
func (a *app) overviewOpen() bool {
	for _, w := range a.liveWins() {
		if w.overview {
			return true
		}
	}
	return false
}

// overview returns every window as All Panes shows it, oldest first.
func (a *app) overview() []OverWindow {
	var out []OverWindow
	for _, w := range a.liveWins() {
		ow := OverWindow{ID: w.id, Stage: w.stage}
		if g, ok := a.groupOf[a.focusIn(w)]; ok {
			ow.Front = g
		}
		for _, t := range a.tabsOf(w) {
			ow.Tabs = append(ow.Tabs, OverTab{Group: t.Group, Box: a.groups[t.Group].clone(), Pane: t.Pane})
		}
		out = append(out, ow)
	}
	return out
}

// paneToTab carries out in: into another window as a tab of its own,
// or out of its split onto one in its own window.
func (a *app) paneToTab(in PaneToTab) {
	w := a.winByID(in.Window)
	from := a.ownerOf(in.Pane)
	if w == nil || w.gone || from == nil || a.closing[in.Pane] {
		return
	}
	if from != w {
		a.moveToWindow(in.Pane, w)
		return
	}
	a.ownTab(in.Pane)
}

// ownTab takes pane id out of its split onto a tab of its own, in its
// window, and reports the group it is in now. A pane alone stays where
// it is.
func (a *app) ownTab(id string) int {
	g, ok := a.groupOf[id]
	if !ok || a.groups[g] == nil || a.groups[g].Pane == id {
		return g
	}
	w := a.ownerOf(id)
	next := a.take(id)
	a.nextGroup++
	a.groups[a.nextGroup] = &Box{Pane: id}
	a.groupOf[id] = a.nextGroup
	if w != nil && a.focusIn(w) == id && next != "" {
		// The tab it left keeps the pane beside it in front.
		a.groupFocus[g] = next
	}
	a.sortTabs()
	return a.nextGroup
}

// dockPane carries out in: the pane leaves its split, and joins the
// pane beside it in one.
func (a *app) dockPane(in DockPane) {
	to := a.ownerOf(in.Beside)
	if in.Pane == in.Beside || to == nil || to.gone || a.ownerOf(in.Pane) == nil || a.closing[in.Pane] || a.closing[in.Beside] {
		return
	}
	g := a.ownTab(in.Pane)
	a.dockGroup(g, in.Beside, in.Vertical, in.First, to)
}

// dockGroup takes group g's panes into a split beside the pane beside,
// in window to, on the side vertical and first say, and gives the pane
// that was in front in g the keyboard.
func (a *app) dockGroup(g int, beside string, vertical, first bool, to *ownWin) {
	into, ok := a.groupOf[beside]
	if !ok || a.groups[g] == nil || into == g || !a.movable(g) || a.closing[beside] || a.ownerOf(beside) != to {
		return
	}
	a.groupToWindow(g, to)
	moved := a.groups[g]
	a.splits++
	box := &Box{
		ID: "s" + strconv.Itoa(a.splits), Vertical: vertical, Share: 0.5, Opening: true,
		A: &Box{Pane: beside}, B: moved,
	}
	if first {
		box.A, box.B = moved, box.A
	}
	a.groups[into] = a.groups[into].replace(beside, box)
	pane := a.tabPane(g)
	for _, id := range moved.leaves(nil) {
		a.groupOf[id] = into
	}
	delete(a.groups, g)
	delete(a.groupFocus, g)
	a.sortTabs()
	if to == a.cur {
		a.focus(pane)
		return
	}
	a.setFocusIn(to, pane)
}
