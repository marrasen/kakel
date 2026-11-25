//go:build js && wasm

package main

import (
	"context"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/internal/build"
	"github.com/marrasen/kakel/look"
)

// nameState is what the page with the name shows: always the same.
type nameState struct{}

// namePage is the page with kakel's name on it.
type namePage struct{ body gunim.Node }

// showName shows kakel's name in the middle of the page, until the
// window closes.
func showName(ctx context.Context, w *gunim.Window, _ look.Themed) error {
	gunim.RegisterView(w, "name", func(nameState) *namePage {
		col := widget.Column(widget.NewLabel(build.Name))
		col.Justify, col.Cross = widget.JustifyCenter, widget.CrossCenter
		return &namePage{body: col}
	}, (*namePage).Update)
	c := w.Client()
	if err := c.Mount(gunim.Root, "name", "name", nameState{}); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case _, ok := <-c.Intents():
			if !ok {
				return nil
			}
		}
	}
}

// Update shows st.
func (p *namePage) Update(nameState, *gunim.UI) {}

// Children implements [gunim.Composite].
func (p *namePage) Children() []gunim.Node { return []gunim.Node{p.body} }

// Layout implements [gunim.Node].
func (p *namePage) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	k := kids.At(0)
	k.Layout(gunim.Tight(c.Max))
	k.Place(geom.Point{})
	return c.Max
}

// Paint implements [gunim.Node].
func (p *namePage) Paint(pt *paint.Painter, _ gunim.Frame, _ geom.Size, kids gunim.Children) {
	kids.At(0).Paint(pt)
	drawn()
}
