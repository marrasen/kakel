package view

import (
	"github.com/marrasen/gunim/widget/dock"

	"github.com/marrasen/kakel/app"
)

// boxLayout is a layout of window 0 showing b: a group of one tab at
// each pane, and the split IDs b gives. Behind, in the group of the
// first pane, stand the panes behind.
func boxLayout(b *app.Box, behind ...string) dock.Layout {
	l := dock.Layout{Grouped: true}
	w := dock.Window{ID: 0, Keep: true}
	first := true
	var walk func(b *app.Box) *dock.Node
	walk = func(b *app.Box) *dock.Node {
		switch {
		case b == nil:
			return nil
		case b.Pane != "":
			g := &dock.Node{ID: "#g" + b.Pane, Kind: dock.Group, Front: b.Pane, Kids: []*dock.Node{{ID: b.Pane, Kind: dock.Item}}}
			if first {
				first = false
				for _, id := range behind {
					g.Kids = append(g.Kids, &dock.Node{ID: id, Kind: dock.Item})
				}
			}
			return g
		}
		k := dock.Row
		if b.Vertical {
			k = dock.Column
		}
		x := walk(b.A)
		y := walk(b.B)
		return &dock.Node{ID: "#" + b.ID, Kind: k, Share: b.Share, Kids: []*dock.Node{x, y}}
	}
	w.Root = walk(b)
	l.Windows = []dock.Window{w}
	return l
}

// tabsLayout is a layout of window 0 with panes ids as tabs of one
// group, front in front.
func tabsLayout(front string, ids ...string) dock.Layout {
	l := dock.NewLayout(true, ids...)
	l.Show(front)
	return l
}

// inWin is l with its one window numbered win, for a state of that
// window.
func inWin(l dock.Layout, win int) dock.Layout {
	if win != 0 && len(l.Windows) == 1 && l.Windows[0].ID == 0 {
		l = l.Clone()
		l.Windows[0].ID = win
	}
	return l
}

// withAllPanes is l with st's panes that stand nowhere yet as tabs of its
// first group, behind what shows, as a window's panes all stand somewhere.
func withAllPanes(l dock.Layout, st app.State) dock.Layout {
	w := l.Window(st.Window)
	if w == nil || w.Root == nil {
		return l
	}
	l = l.Clone()
	w = l.Window(st.Window)
	g := w.Root
	for g.Kind != dock.Group {
		g = g.Kids[0]
	}
	for _, p := range st.Panes {
		if l.Node(p.ID) == nil {
			g.Kids = append(g.Kids, &dock.Node{ID: p.ID, Kind: dock.Item})
		}
	}
	return l
}
