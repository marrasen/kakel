package app

import (
	"slices"

	"github.com/marrasen/gunim/widget/dock"
)

// Panes stand in a layout, which gunim's dock shows: every window is a
// tree of splits, each part of it a group of tabs, each tab a pane, as
// an editor has. The tab in front of each group shows. The pane with
// the keyboard is always in front of its group. A tab dragged along a
// bar moves along it, onto another bar joins that group, onto the edge
// of a pane splits it, into another window moves there, and let go
// outside every window opens one of its own.

// Intents for tabs.
type (
	// NextTab puts the tab after the one in front of the group with the
	// keyboard in front, or the one before with Back, going round.
	NextTab struct{ Back bool }
	// ShiftTab moves the tab in front one place later along its bar, or
	// one earlier with Back.
	ShiftTab struct{ Back bool }
	// MoveTab moves pane Pane to where To says, and gives it the
	// keyboard. To.Win is the window it goes to, or in another window,
	// To.At says which.
	MoveTab struct {
		Pane string
		To   dock.Target
	}
)

// handleTab carries out an intent about tabs, and reports whether it
// was one.
func (a *app) handleTab(in any) bool {
	switch in := in.(type) {
	case NextTab:
		a.nextTab(in.Back)
	case ShiftTab:
		a.shiftTab(in.Back)
	case MoveTab:
		a.moveTab(in.Pane, in.To)
	default:
		return false
	}
	return true
}

// paneWin returns the number of the window pane id stands in, and
// whether it stands in one.
func (a *app) paneWin(id string) (int, bool) { return a.layout.Where(id) }

// keepWindow has the layout hold window win.
func (a *app) keepWindow(win int) {
	if a.layout.Window(win) == nil {
		a.layout.Windows = append(a.layout.Windows, dock.Window{ID: win, Keep: true})
	}
}

// frontOf is the pane n shows first: the pane itself, the one in front
// of a group, or the first of a split's. "" for none.
func frontOf(n *dock.Node) string {
	switch {
	case n == nil:
		return ""
	case n.Kind == dock.Item:
		return n.ID
	case n.Kind == dock.Group:
		for _, k := range n.Kids {
			if k.ID == n.Front {
				return frontOf(k)
			}
		}
		return ""
	}
	for _, k := range n.Kids {
		if id := frontOf(k); id != "" {
			return id
		}
	}
	return ""
}

// beside is the pane that takes the keyboard as pane id leaves: the tab
// beside it, the one after, or the one before where it was last; or
// alone in its group, the pane nearest on the other side of the split
// it stands in; "" for none.
func (a *app) beside(id string) string {
	g := a.layout.GroupOf(id)
	if g == nil {
		return ""
	}
	if len(g.Kids) >= 2 {
		i := slices.IndexFunc(g.Kids, func(k *dock.Node) bool { return k.ID == id })
		if i+1 < len(g.Kids) {
			return g.Kids[i+1].ID
		}
		return g.Kids[i-1].ID
	}
	p := a.layout.Parent(g.ID)
	if p == nil {
		return ""
	}
	if p.Kids[0].ID == g.ID {
		return frontOf(p.Kids[1])
	}
	// The side before it: the last of what that shows, the nearest.
	n := p.Kids[0]
	for n.Kind == dock.Row || n.Kind == dock.Column {
		n = n.Kids[1]
	}
	return frontOf(n)
}

// keysGroup is the group a new tab in window win joins: that of the
// pane with the keyboard there, or a group of its panes for one with
// none, or the Machines pane's only where it holds nothing else. nil
// for a window with no group yet.
func (a *app) keysGroup(win int) *dock.Node {
	w := a.layout.Window(win)
	if w == nil || w.Root == nil {
		return nil
	}
	focus := ""
	if o := a.winByID(win); o != nil {
		focus = a.focusIn(o)
	}
	if g := a.layout.GroupOf(focus); g != nil && !a.isServers(focus) {
		return g
	}
	var first, other *dock.Node
	var walk func(n *dock.Node)
	walk = func(n *dock.Node) {
		switch n.Kind {
		case dock.Group:
			if first == nil {
				first = n
			}
			if other == nil && !a.isServers(frontOf(n)) {
				other = n
			}
		case dock.Row, dock.Column:
			walk(n.Kids[0])
			walk(n.Kids[1])
		}
	}
	walk(w.Root)
	if other != nil {
		return other
	}
	return first
}

// isServers reports whether pane id is the Machines pane.
func (a *app) isServers(id string) bool {
	return slices.ContainsFunc(a.st.Panes, func(p Pane) bool { return p.ID == id && p.Kind == KindServers })
}

// keepFocusShown has the pane with the keyboard, in each window, in
// front of its group.
func (a *app) keepFocusShown() {
	for _, w := range a.liveWins() {
		if id := a.focusIn(w); id != "" {
			if g := a.layout.GroupOf(id); g != nil && g.Front != id {
				a.layout.Show(id)
			}
		}
	}
}

func (a *app) nextTab(back bool) {
	g := a.layout.GroupOf(a.st.Focus)
	if g == nil || len(g.Kids) < 2 {
		return
	}
	i := slices.IndexFunc(g.Kids, func(k *dock.Node) bool { return k.ID == a.st.Focus })
	step := 1
	if back {
		step = len(g.Kids) - 1
	}
	a.focus(g.Kids[(i+step)%len(g.Kids)].ID)
}

func (a *app) shiftTab(back bool) {
	id := a.st.Focus
	g := a.layout.GroupOf(id)
	if g == nil {
		return
	}
	i := slices.IndexFunc(g.Kids, func(k *dock.Node) bool { return k.ID == id })
	switch {
	case back && i > 0:
		a.layout.Move(id, dock.Target{At: g.ID, Side: dock.Into, Index: i - 1})
	case !back && i+1 < len(g.Kids):
		a.layout.Move(id, dock.Target{At: g.ID, Side: dock.Into, Index: i + 2})
	}
}

// moveTab moves pane id where to says, and gives it the keyboard. The
// window it left gives the keyboard to what stood beside it.
func (a *app) moveTab(id string, to dock.Target) {
	from := a.ownerOf(id)
	if from == nil {
		return
	}
	i := slices.IndexFunc(a.st.Panes, func(p Pane) bool { return p.ID == id })
	next := a.beside(id)
	if to.At == "" {
		a.keepWindow(to.Win)
	}
	if !a.layout.Move(id, to) {
		// Nowhere new: in front, as a tab dropped is.
		a.focus(id)
		return
	}
	w := a.ownerOf(id)
	if w != from {
		a.refocus(from, id, next, i)
	}
	if w == a.cur || w == nil {
		a.focus(id)
		return
	}
	a.setFocusIn(w, id)
}

// boxOf is how node n shows on a stage: its splits, and in each part
// the pane in front. nil for a node showing nothing.
func boxOf(n *dock.Node) *Box {
	switch {
	case n == nil:
		return nil
	case n.Kind == dock.Item:
		return &Box{Pane: n.ID}
	case n.Kind == dock.Group:
		for _, k := range n.Kids {
			if k.ID == n.Front {
				return boxOf(k)
			}
		}
		return nil
	}
	x, y := boxOf(n.Kids[0]), boxOf(n.Kids[1])
	switch {
	case x == nil:
		return y
	case y == nil:
		return x
	}
	share := n.Share
	if share <= 0 || share >= 1 {
		share = 0.5
	}
	return &Box{ID: n.ID, Vertical: n.Kind == dock.Column, Share: share, A: x, B: y}
}

// behindOf adds the panes n holds that do not show to out.
func behindOf(n *dock.Node, out []string) []string {
	switch {
	case n == nil || n.Kind == dock.Item:
	case n.Kind == dock.Group:
		for _, k := range n.Kids {
			if k.ID != n.Front {
				out = append(out, k.Items()...)
			} else {
				out = behindOf(k, out)
			}
		}
	default:
		for _, k := range n.Kids {
			out = behindOf(k, out)
		}
	}
	return out
}

// OverTabs is window win of l as All Panes shows it: first what shows
// now, each split as it stands, then each pane behind another. The first
// is in front.
func OverTabs(l *dock.Layout, win int) []OverTab {
	w := l.Window(win)
	if w == nil || w.Root == nil {
		return nil
	}
	var out []OverTab
	add := func(b *Box, pane string) {
		out = append(out, OverTab{Group: len(out) + 1, Box: b, Pane: pane})
	}
	if b := boxOf(w.Root); b != nil {
		add(b, frontOf(w.Root))
	}
	for _, id := range behindOf(w.Root, nil) {
		add(&Box{Pane: id}, id)
	}
	return out
}
