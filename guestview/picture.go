package guestview

import (
	"image/color"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"
	"github.com/marrasen/gunim/widget/dock"

	"github.com/marrasen/kakel/look"
)

// How the host's window is drawn as a picture: the room round it, how
// much of the room past the host's own size it grows into, its corners,
// its shadow, the line round each of its panes, the guest's ring round
// the pane it is in, and the host's line round the host's.
var (
	pictureMargin = theme.Length("kakel.page.picture.margin", 16)
	pictureGrow   = theme.Number("kakel.page.picture.grow", 0.5)
	pictureRadius = theme.Length("kakel.page.picture.radius", 10)
	pictureShadow = theme.Length("kakel.page.picture.shadow", 24)
	paneEdge      = theme.Length("kakel.page.pane.edge", 1)
	guestRing     = theme.Length("kakel.page.ring", 2)
	hostLine      = theme.Length("kakel.page.host.line", 1)
	// pictureLeast is the least a terminal's cell is drawn tall in the
	// picture, under which the page shows one pane at a time: 9 pixels
	// is text of about 7, the smallest whose letters do not run together
	// on a screen of ordinary density.
	pictureLeast = theme.Length("kakel.page.picture.least", 9)
	// fingerLeast is the least a bar of tabs is drawn tall on a page a
	// finger is at, under which it shows one pane at a time: 28 pixels is
	// 7.4 mm, about the 7.5 mm a fingertip needs to hit what it aims at.
	// The picture's bars are the host's, never drawn taller than they are.
	fingerLeast = theme.Length("kakel.page.picture.finger", 28)
	// hostInk is the host's line, and its name on the tab of the pane it
	// is in.
	hostInk = theme.Color("kakel.page.host", color.NRGBA{R: 0x5e, G: 0x9c, B: 0xff, A: 0xff})
)

// picture is the host's window, whole: its panes laid out as large as the
// host has them, and drawn as one picture scaled to fit the page, in the
// middle, in a frame. Every terminal fills its pane, and all have the one
// size of text. A pointer on it reaches what is under it in the picture,
// gunim taking it back through the scale it was drawn with.
//
// Where the host has said no size, the page sets the sizes, or the panes
// stand one at a time, they fill the page as they are: with their edges
// and rings, but where they stand one at a time. Between the two, and as
// the host resizes its window, it glides.
type picture struct {
	anim.Group
	w *windowPage
	// on is how far the panes are the host's picture, rather than filling
	// the page, and framed what it is on its way to. room is the host's,
	// gliding as it changes, and sized says it has been said.
	on     *anim.Float
	framed bool
	room   *anim.Size
	sized  bool
	// drawn says the picture has been drawn, so a change from now on
	// glides.
	drawn bool
	// laid is the size the panes were last laid out at, and scale and at
	// how they are drawn: scaled by scale, their corner at at.
	laid  geom.Size
	scale float32
	at    geom.Point
	// guest and host ring the pane each is in, and together how far the
	// host's stands inside the guest's, both being round the one pane.
	// lines is how far the panes' edges and the rings show, and lined
	// what it is on its way to.
	guest, host mark
	together    *anim.Float
	lines       *anim.Float
	lined       bool
}

// mark rings a pane of the picture: where it is, in the panes' own space,
// gliding to a pane come to, and how lit it is.
type mark struct {
	id  string
	at  *anim.Rect
	lit *anim.Float
	// moved says it is gliding from one pane to another.
	moved bool
}

func newPicture(w *windowPage) *picture {
	pc := &picture{w: w, on: anim.NewFloat(0), room: anim.NewSize(geom.Size{}), scale: 1, together: anim.NewFloat(0), lines: anim.NewFloat(0)}
	pc.guest = mark{at: anim.NewRect(geom.Rect{}), lit: anim.NewFloat(0)}
	pc.host = mark{at: anim.NewRect(geom.Rect{}), lit: anim.NewFloat(0)}
	pc.Add(pc.on, pc.room, pc.guest.at, pc.guest.lit, pc.host.at, pc.host.lit, pc.together, pc.lines)
	return pc
}

// hostRoom is the host's room, where it has said one.
func (pc *picture) hostRoom() (geom.Size, bool) {
	r := pc.w.st.Room
	return r, roomy(r)
}

// roomy reports whether a room the host says is one to lay panes out in:
// a pixel or more each way.
func roomy(r geom.Size) bool { return r.W >= 1 && r.H >= 1 }

// readable reports whether the picture, on a page of size page, is drawn
// large enough to be read, and, on a page a finger is at, to be touched.
// Where the host has said no room it is not drawn at all, and is.
func (pc *picture) readable(page geom.Size, th *theme.Live) bool {
	room, said := pc.hostRoom()
	if !said || pc.w.st.GuestSizes {
		// Drawn as large as the page has it.
		return true
	}
	k := pc.fit(page, room, th)
	if cell := pc.w.st.Cell; cell.H > 0 && k*cell.H < pictureLeast.Get(th) {
		return false
	}
	return !pc.w.p.touch || k*dock.BarHeight.In(pc.w.st.Theme.Theme) >= fingerLeast.Get(th)
}

// set has the picture go to what the state and the page say: framed at
// the host's size, or filling the page.
func (pc *picture) set(u *gunim.UI) {
	room, said := pc.hostRoom()
	framed := said && !pc.w.narrow && !pc.w.st.GuestSizes
	lined := (said || pc.w.st.GuestSizes) && !pc.w.narrow
	m := widget.Settle.Get(u.Theme())
	if said && room != pc.room.Target() {
		if pc.sized && pc.drawn {
			pc.room.Animate(room, m)
		} else {
			pc.room.Jump(room)
		}
		pc.sized = true
	}
	if framed != pc.framed {
		pc.framed = framed
		if pc.drawn {
			pc.on.Animate(anim.OnOff(framed), m)
		} else {
			pc.on.Jump(anim.OnOff(framed))
		}
	}
	if lined != pc.lined {
		pc.lined = lined
		if pc.drawn {
			pc.lines.Animate(anim.OnOff(lined), m)
		} else {
			pc.lines.Jump(anim.OnOff(lined))
		}
	}
	u.Invalidate()
}

// Children implements [gunim.Composite].
func (pc *picture) Children() []gunim.Node { return []gunim.Node{pc.w.tabs} }

// Layout implements [gunim.Node]: the panes at a size between the page's
// and the host's, as far as the picture is on, and scaled as far.
func (pc *picture) Layout(c gunim.Constraints, f gunim.Frame, kids gunim.Children) geom.Size {
	page := c.Max
	if _, said := pc.hostRoom(); said && !pc.drawn {
		// Not drawn yet, it is shown as the page will have it, which the
		// page works out once this is laid out.
		wide := !pc.w.narrowIn(pc.w.room, f.Theme)
		pc.on.Jump(anim.OnOff(wide && !pc.w.st.GuestSizes))
		pc.lines.Jump(anim.OnOff(wide))
	}
	on := min(max(pc.on.Value(), 0), 1)
	host := pc.room.Value()
	if !roomy(host) {
		on, host = 0, page
	}
	// Laid out between the page's size and the host's, and drawn between
	// the page and the picture's place on it, so that it stays on the page
	// all the way.
	k := pc.fit(page, host, f.Theme)
	pc.laid = geom.Sz(page.W+(host.W-page.W)*on, page.H+(host.H-page.H)*on)
	drawn := geom.Sz(page.W+(host.W*k-page.W)*on, page.H+(host.H*k-page.H)*on)
	pc.scale = min(drawn.W/pc.laid.W, drawn.H/pc.laid.H)
	pc.at = geom.Pt((page.W-pc.laid.W*pc.scale)/2, (page.H-pc.laid.H*pc.scale)/2)
	kid := kids.At(0)
	kid.Layout(gunim.Tight(pc.laid))
	kid.Place(geom.Point{})
	pc.follow(&pc.guest, pc.w.dock.Active(), f)
	pc.follow(&pc.host, pc.w.hostIn, f)
	if together := pc.guest.id == pc.host.id && pc.guest.lit.Target() == 1; together != (pc.together.Target() == 1) {
		pc.together.Animate(anim.OnOff(together), widget.Settle.Get(f.Theme))
	}
	return page
}

// fit is how much the host's window is scaled on a page: to fit within
// it, and past its own size only by a share of the room left.
func (pc *picture) fit(page, host geom.Size, th *theme.Live) float32 {
	pad := pictureMargin.Get(th)
	k := min(max(page.W-2*pad, 1)/host.W, max(page.H-2*pad, 1)/host.H)
	if k > 1 {
		k = 1 + (k-1)*min(max(pictureGrow.Get(th), 0), 1)
	}
	return k
}

// follow has m ring pane id, where it shows, beside another: gliding to it
// from the pane it rang before, and with it as the panes move.
func (pc *picture) follow(m *mark, id string, f gunim.Frame) {
	d := pc.w.dock
	r, shown := d.Rect(id)
	lit := shown && len(d.Shown()) > 1 && pc.lined
	if lit != (m.lit.Target() == 1) {
		m.lit.Animate(anim.OnOff(lit), widget.Settle.Get(f.Theme))
	}
	if !shown {
		return
	}
	switch {
	case m.lit.Value() < 0.01:
		// Out of sight: where it will light.
		m.at.Jump(r)
		m.moved = false
	case id != m.id || m.moved && m.at.Active():
		m.at.Animate(r, widget.Settle.Get(f.Theme))
		m.moved = true
	case r != m.at.Target():
		m.at.Jump(r)
		m.moved = false
	}
	m.id = id
}

// Paint implements [gunim.Node]: the frame and its shadow, the panes in
// it, scaled, the line round each pane, and the rings, which keep their
// widths whatever the scale.
func (pc *picture) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	pc.drawn = true
	th := f.Theme
	on := min(max(pc.on.Value(), 0), 1)
	lines := min(max(pc.lines.Value(), 0), 1)
	kid := kids.At(0)
	if on < 0.001 {
		kid.Paint(p)
		if lines > 0.001 {
			pc.paintLines(p, th, lines)
		}
		return
	}
	r := geom.Rect{Min: pc.at, Max: pc.at.Add(geom.Pt(pc.laid.W*pc.scale, pc.laid.H*pc.scale))}
	radius := pictureRadius.Get(th) * on
	shadow := widget.DialogShadow.Get(th)
	shadow.A = uint8(float32(shadow.A) * on)
	blur := pictureShadow.Get(th)
	p.DrawRRect(paint.RRectOp{
		Rect: r, Radius: radius, Fill: paint.Solid(widget.Background.In(pc.w.st.Theme.Theme)),
		Shadow: paint.Shadow{Offset: geom.Pt(0, blur/4), Blur: blur * on, Color: shadow},
	})
	func() {
		defer p.Layer(paint.LayerOpts{Bounds: r, Opacity: 1, Clip: true, Radius: radius})()
		func() {
			defer p.Push(paint.Translate(pc.at))()
			defer p.Push(paint.Scale(pc.scale, geom.Point{}))()
			kid.Paint(p)
		}()
		pc.paintLines(p, th, lines)
	}()
	edge := widget.DialogBorder.Get(th)
	edge.A = uint8(float32(edge.A) * on)
	p.RRectStroke(r.Inset(geom.Uniform(0.5)), radius, paint.Fill{}, paint.Stroke{Width: 1, Color: edge})
}

// paintLines draws the line round each pane, and the rings round the
// guest's pane and the host's, as strong as on.
func (pc *picture) paintLines(p *paint.Painter, th *theme.Live, on float32) {
	pc.paintEdges(p, th, on)
	// The host's inside the guest's, so both show round one pane.
	ring := guestRing.Get(th)
	pc.paintMark(p, &pc.host, hostInk.Get(th), hostLine.Get(th), ring*min(max(pc.together.Value(), 0), 1), on)
	pc.paintMark(p, &pc.guest, look.Guest.Get(th), ring, 0, on)
}

// inPage is r, in the panes' own space, where the page shows it.
func (pc *picture) inPage(r geom.Rect) geom.Rect {
	return geom.Rect{
		Min: pc.at.Add(geom.Pt(r.Min.X*pc.scale, r.Min.Y*pc.scale)),
		Max: pc.at.Add(geom.Pt(r.Max.X*pc.scale, r.Max.Y*pc.scale)),
	}
}

// paintEdges draws a line round each pane that shows, so where one ends
// is plain whatever its colours and the window's.
func (pc *picture) paintEdges(p *paint.Painter, th *theme.Live, on float32) {
	d := pc.w.dock
	c := widget.DialogBorder.Get(th)
	c.A = uint8(float32(c.A) * on)
	w := paneEdge.Get(th)
	for _, id := range d.Shown() {
		if r, ok := d.Rect(id); ok {
			p.RRectStroke(pc.inPage(r).Inset(geom.Uniform(w/2)), 0, paint.Fill{}, paint.Stroke{Width: w, Color: c})
		}
	}
}

// paintMark draws m's ring in c, w wide, in from its pane's edge.
func (pc *picture) paintMark(p *paint.Painter, m *mark, c color.NRGBA, w, in, on float32) {
	lit := min(max(m.lit.Value(), 0), 1) * on
	if lit < 0.01 {
		return
	}
	r := pc.inPage(m.at.Value()).Inset(geom.Uniform(in + w/2))
	if r.Empty() {
		return
	}
	c.A = uint8(float32(c.A) * lit)
	p.RRectStroke(r, 0, paint.Fill{}, paint.Stroke{Width: w, Color: c})
}
