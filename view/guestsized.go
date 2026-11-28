package view

import (
	"image/color"
	"math"
	"slices"
	"strings"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/look"
)

// A terminal whose size a guest's page sets shows the program at that
// size, from its top left, and hatches the room the program does not
// have, with a note of whose size it is. One larger than the pane is
// drawn smaller to fit, as a size another kakel sets is.

// beyondInk is the hatching, beyondEvery how far apart its stripes are and
// beyondWide how wide each is, and sizedNoteRoom how far the note stands
// from the pane's corner.
var (
	beyondInk     = theme.Color("kakel.term.beyond", color.NRGBA{R: 0x80, G: 0x80, B: 0x80, A: 0x2a})
	beyondEvery   = theme.Length("kakel.term.beyond.every", 10)
	beyondWide    = theme.Length("kakel.term.beyond.wide", 4)
	sizedNoteRoom = theme.Length("kakel.term.sized.room", 6)
)

// guestSized is what a terminal shows of a guest's page setting its size.
type guestSized struct {
	// beyond is how far the hatching has come, and note says whose size it
	// is.
	beyond *anim.Float
	note   *widget.Badge
}

func newGuestSized() guestSized {
	n := widget.NewBadge("")
	n.Ink = look.Faint
	n.SetHidden(true, nil)
	return guestSized{beyond: anim.NewFloat(0), note: n}
}

// show shows that guests' pages set the size, whose they are in note, or
// that none do, for "".
func (g *guestSized) show(note string, u *gunim.UI) {
	on := note != ""
	if on {
		// Kept as it fades away.
		g.note.Text = note
	}
	g.note.SetHidden(!on, u)
	if on != (g.beyond.Target() == 1) {
		g.beyond.Animate(anim.OnOff(on), widget.Settle.Get(u.Theme()))
	}
}

// sizedNote says whose pages set pane id's size, in the window st is of,
// "" for nobody's.
func sizedNote(st app.State, id string) string {
	var names []string
	for _, l := range st.Links {
		if l.Window == st.Window && l.ForGuest && slices.Contains(l.Sizes, id) {
			names = append(names, guestCalled(l)+"'s")
		}
	}
	switch len(names) {
	case 0:
		return ""
	case 1:
		return "Sized for " + names[0] + " page"
	}
	n := len(names)
	return "Sized for " + strings.Join(names[:n-1], ", ") + " and " + names[n-1] + " pages"
}

// Children implements [gunim.Composite]: the terminal's cells, and the
// note.
func (t *term) Children() []gunim.Node {
	return append(t.Term.Children(), t.sized.note)
}

// Layout implements [gunim.Node]: the terminal, and the note in the
// corner.
func (t *term) Layout(c gunim.Constraints, f gunim.Frame, kids gunim.Children) geom.Size {
	size := t.Term.Layout(c, f, kids)
	note := kids.At(1)
	s := note.Layout(gunim.Constraints{Max: size})
	room := sizedNoteRoom.Get(f.Theme)
	note.Place(geom.Pt(max(size.W-s.W-room, 0), max(size.H-s.H-room, 0)))
	return size
}

// screenRect is where the program's screen is drawn in the terminal.
func (t *term) screenRect() geom.Rect {
	at, cell := t.Drawn()
	cols, rows := t.Cells().GridSize()
	return geom.Rect{Min: at, Max: at.Add(geom.Pt(float32(cols)*cell.W, float32(rows)*cell.H))}
}

// paintBeyond hatches the room in box the program's screen does not take,
// while a guest's page sets its size.
func (t *term) paintBeyond(p *paint.Painter, f gunim.Frame, box geom.Size) {
	k := min(max(t.sized.beyond.Value(), 0), 1)
	if k < 0.001 {
		return
	}
	scr := t.screenRect()
	all := geom.Rect{Max: box.Point()}
	c := beyondInk.Get(f.Theme)
	c.A = uint8(float32(c.A) * k)
	every, wide := beyondEvery.Get(f.Theme), beyondWide.Get(f.Theme)
	for _, r := range beside(all, scr) {
		hatch(p, r, c, every, wide)
	}
}

// beside are the parts of all that inner leaves: above it, under it, and
// on either side.
func beside(all, inner geom.Rect) []geom.Rect {
	in := geom.Rect{
		Min: geom.Pt(min(max(inner.Min.X, all.Min.X), all.Max.X), min(max(inner.Min.Y, all.Min.Y), all.Max.Y)),
		Max: geom.Pt(min(max(inner.Max.X, all.Min.X), all.Max.X), min(max(inner.Max.Y, all.Min.Y), all.Max.Y)),
	}
	out := []geom.Rect{
		{Min: all.Min, Max: geom.Pt(all.Max.X, in.Min.Y)},
		{Min: geom.Pt(all.Min.X, in.Max.Y), Max: all.Max},
		{Min: geom.Pt(all.Min.X, in.Min.Y), Max: geom.Pt(in.Min.X, in.Max.Y)},
		{Min: geom.Pt(in.Max.X, in.Min.Y), Max: geom.Pt(all.Max.X, in.Max.Y)},
	}
	// Less than a pixel is a sum that did not come out even.
	return slices.DeleteFunc(out, func(r geom.Rect) bool { return r.Size().W < 1 || r.Size().H < 1 })
}

// hatch fills r with diagonal stripes of c, wide wide and every apart,
// which run on from one part to the next as one pattern.
func hatch(p *paint.Painter, r geom.Rect, c color.NRGBA, every, wide float32) {
	if every <= 0 {
		return
	}
	defer p.Layer(paint.LayerOpts{Bounds: r, Opacity: 1, Clip: true})()
	// Drawn turned a quarter of the way round, about a fixed point, so
	// the parts line up: what of the turned space r's corners reach, in
	// whichever way the turn goes.
	const k = math.Sqrt2 / 2
	lo, hi := geom.Pt(math.MaxFloat32, math.MaxFloat32), geom.Pt(-math.MaxFloat32, -math.MaxFloat32)
	for _, at := range []geom.Point{r.Min, r.Max, geom.Pt(r.Min.X, r.Max.Y), geom.Pt(r.Max.X, r.Min.Y)} {
		for _, sign := range []float32{-1, 1} {
			x, y := k*(at.X+sign*at.Y), k*(at.Y-sign*at.X)
			lo = geom.Pt(min(lo.X, x), min(lo.Y, y))
			hi = geom.Pt(max(hi.X, x), max(hi.Y, y))
		}
	}
	defer p.Push(paint.Rotate(-math.Pi/4, geom.Point{}))()
	for x := float32(math.Floor(float64(lo.X/every))) * every; x < hi.X; x += every {
		p.RRect(geom.Rc(x, lo.Y, wide, hi.Y-lo.Y), 0, paint.Solid(c))
	}
}
