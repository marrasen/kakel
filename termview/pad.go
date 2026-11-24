package termview

import (
	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/theme"

	"github.com/marrasen/kakel/look"
)

// Padding is the room between a terminal's cells and its pane's
// edges, in the terminal's background.
var Padding = theme.Length("kakel.term.padding", 6)

// Pad holds a terminal a little in from its pane's edges, the room
// filled with the terminal's background, so the text is clear of the
// window's edge.
type Pad struct {
	// Term is the terminal, or a program's own node for it.
	Term gunim.Node
}

// Children implements [gunim.Composite].
func (p *Pad) Children() []gunim.Node { return []gunim.Node{p.Term} }

// Layout implements [gunim.Node].
func (p *Pad) Layout(c gunim.Constraints, f gunim.Frame, kids gunim.Children) geom.Size {
	pad := Padding.Get(f.Theme)
	k := kids.At(0)
	k.Layout(gunim.Tight(geom.Sz(max(0, c.Max.W-2*pad), max(0, c.Max.H-2*pad))))
	k.Place(geom.Pt(pad, pad))
	return c.Max
}

// Paint implements [gunim.Node].
func (p *Pad) Paint(pt *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	pt.RRect(geom.Rect{Max: box.Point()}, 0, paint.Solid(look.TermBackground.Get(f.Theme)))
	kids.At(0).Paint(pt)
}
