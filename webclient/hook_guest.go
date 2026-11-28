//go:build js && wasm && demo

package main

import (
	"context"
	"sync"
	"syscall/js"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/access"

	"github.com/marrasen/kakel/guestview"
	"github.com/marrasen/kakel/screen"
)

// A build for the tests has window.kakelGuest on the page, to ask what it
// shows. It tells nothing the page does not show.
func init() {
	shells = screen.NewShells()
	var mu sync.Mutex
	var now guestview.State
	told = func(st guestview.State) {
		mu.Lock()
		defer mu.Unlock()
		now = st
	}
	var c gunim.Client
	opens = func(w *gunim.Window) { c = w.Client() }
	js.Global().Set("kakelGuest", js.ValueOf(map[string]any{
		// state is the page, and what it says.
		"state": js.FuncOf(func(js.Value, []js.Value) any {
			mu.Lock()
			defer mu.Unlock()
			panes := []any{}
			for _, p := range now.Panes {
				panes = append(panes, p.ID)
			}
			shown := []any{}
			for _, id := range now.Layout.Shown(0) {
				shown = append(shown, id)
			}
			return map[string]any{
				"page": int(now.Page), "lost": now.Lost, "busy": now.Busy, "over": now.Over.Title, "trouble": now.Trouble,
				"host": now.HostName, "canType": now.CanType, "panes": panes, "shown": shown,
				"theme": now.Theme.Name, "why": now.Why,
			}
		}),
		// screen is what is on a pane's screen, a line each.
		"screen": js.FuncOf(func(_ js.Value, a []js.Value) any {
			sh := shells.Get(a[0].String())
			if sh == nil {
				return nil
			}
			s, ok := screenText(sh)
			if !ok {
				return nil
			}
			return s
		}),
		// at is a promise of where the part a screen reader calls name is,
		// its middle in CSS pixels, or of null where there is none.
		"at": js.FuncOf(func(_ js.Value, a []js.Value) any {
			name := a[0].String()
			return promise(func() (any, error) {
				tree, err := c.Access(context.Background())
				if err != nil {
					return nil, err
				}
				if n := named(tree.Root, name); n != nil {
					at := n.Bounds.Center()
					return []any{at.X, at.Y}, nil
				}
				return nil, nil
			})
		}),
		// size is a pane's columns and rows.
		"size": js.FuncOf(func(_ js.Value, a []js.Value) any {
			sh := shells.Get(a[0].String())
			if sh == nil {
				return nil
			}
			size := sh.T.Size()
			return []any{size.Cols, size.Rows}
		}),
	}))
}

// named is the first part under n with a name, and some room.
func named(n *access.Node, name string) *access.Node {
	if n.Name == name && n.Bounds.Size().W > 1 {
		return n
	}
	for _, k := range n.Children {
		if got := named(k, name); got != nil {
			return got
		}
	}
	return nil
}
