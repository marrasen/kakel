package termview

import (
	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/text"
	"github.com/marrasen/gunim/widget"
)

// fit is a fitted screen's text, as large as the whole screen has room
// for, and the glide of any screen that goes to another place: a fitted
// one that changes its size, and one that becomes fitted or stops.
type fit struct {
	// on says the screen is fitted.
	on bool
	// probe measures a cell, in place of the cells, which would forget
	// their glyphs at each size tried.
	probe *widget.CellGrid
	// want is what the size of the text was last worked out from, and
	// size what it came to. own is the size the cells had, and have
	// again once the screen is no longer fitted.
	want      fitKey
	size, own float32
	// cell is how large a cell the far end has, where that is said: the
	// text is no larger than one whose cell fits in it.
	cell geom.Size

	// cellH is the height the cells are drawn at, and at where the
	// screen's corner is.
	cellH *anim.Float
	at    *anim.Point
	// laid says the screen has stood somewhere, so it has a place to
	// glide from, and due that it is to glide to its next.
	laid, due bool
}

// fitKey is what the size of the text depends on.
type fitKey struct {
	box, cell  geom.Size
	cols, rows int
	scale      float32
	faces      [4]*text.Face
}

// The sizes of text a fitted screen can have, counted in fitStep, so a
// pane being sized does not draw every letter anew at every pixel.
const (
	fitStep            = 0.25
	fitLeast, fitMost  = 2, 200
	fitFirst, fitAfter = int(fitLeast / fitStep), int(fitMost / fitStep)
)

func (t *fit) init(g *anim.Group) {
	t.probe = widget.NewCellGrid()
	t.probe.Resize(1, 1)
	t.cellH, t.at = anim.NewFloat(0), anim.NewPoint(geom.Point{})
	g.Add(t.cellH, t.at)
}

// set has the screen fitted or not, and cells at the size of text they
// had before once it is not.
func (t *fit) set(on bool, cells *widget.CellGrid) {
	if on == t.on {
		return
	}
	t.on, t.due = on, t.laid
	if !on && t.want != (fitKey{}) && cells.Size == t.size {
		cells.Size = t.own
	}
	t.want = fitKey{}
}

// layout lays the cells of a fitted screen out in box, and returns where
// they stand.
func (t *fit) layout(k gunim.Child, box geom.Size, cells *widget.CellGrid, f gunim.Frame) geom.Point {
	cols, rows := cells.GridSize()
	want := fitKey{box: box, cell: t.cell, cols: cols, rows: rows, scale: f.Scale, faces: cells.Faces}
	if cells.Size != t.size || t.want == (fitKey{}) {
		// The size the program gave it, now or while it was fitted.
		t.own = cells.Size
	}
	if was := t.want; want != was {
		t.want, t.size = want, t.largest(want, f)
		if c := want.cell; c.W > 0 && c.H > 0 {
			// One size for every screen with the same cell, wherever
			// that is smaller than the whole screen would be.
			t.size = min(t.size, t.largest(fitKey{box: c, cols: 1, rows: 1, scale: want.scale, faces: want.faces}, f))
		}
		t.due = t.due || was.cols != 0 && (was.cols != cols || was.rows != rows)
	}
	cells.Size = t.size
	k.Layout(gunim.Tight(box))
	cell := cells.CellSize()
	need := geom.Sz(float32(cols)*cell.W, float32(rows)*cell.H)
	k.Layout(gunim.Tight(need))
	k.Place(geom.Point{})
	return geom.Pt(max(0, (box.W-need.W)/2), max(0, (box.H-need.H)/2))
}

// glide is how a screen that stands at at, scaled by scale, with cells
// cellH tall before that, is drawn now: there, or on its way there. A
// pane being sized, and a screen just shown, are there at once.
func (t *fit) glide(scale float32, at geom.Point, cellH float32, f gunim.Frame) (float32, geom.Point) {
	to := cellH * scale
	moved := t.cellH.Target() != to || t.at.Target() != at
	switch {
	case t.due || t.gliding() && moved:
		m := widget.Settle.Get(f.Theme)
		t.cellH.Animate(to, m)
		t.at.Animate(at, m)
	case moved:
		t.cellH.Jump(to)
		t.at.Jump(at)
	}
	t.laid, t.due = true, false
	if cellH <= 0 || !t.gliding() {
		return scale, at
	}
	return t.cellH.Value() / cellH, t.at.Value()
}

// gliding reports whether the screen is on its way to another place.
func (t *fit) gliding() bool { return t.cellH.Active() || t.at.Active() }

// largest is the largest size of text the whole screen has room for in
// k's box.
func (t *fit) largest(k fitKey, f gunim.Frame) float32 {
	t.probe.Faces = k.faces
	fits := func(n int) bool {
		t.probe.Size = float32(n) * fitStep
		t.probe.Layout(gunim.Constraints{}, f, gunim.Children{})
		cell := t.probe.CellSize()
		// A hair past the box is a sum that did not come out even.
		const hair = 0.01
		return float32(k.cols)*cell.W <= k.box.W+hair && float32(k.rows)*cell.H <= k.box.H+hair
	}
	lo, hi := fitFirst, fitAfter
	for hi-lo > 1 {
		if mid := (lo + hi) / 2; fits(mid) {
			lo = mid
		} else {
			hi = mid
		}
	}
	return float32(lo) * fitStep
}
