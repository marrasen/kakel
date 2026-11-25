package view

import (
	"image/color"
	"slices"
	"strconv"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/look"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"
)

// The chips at the end of the menu bar: what the window is doing for
// someone else, a click away from its dialog. "Shared" while this window
// is shared with a link, or who is in it by one.
// "Agent Share" while panes are shared with an agent, and
// "Remote Access" while it is on, with how many kakels are connected.

// chip is one chip: what it says, its colour, and what a click does.
type chip struct {
	text   string
	colour color.NRGBA
	do     func(*gunim.UI)
	// off, when set, puts an × on the chip that turns off what it says
	// is on. accent colours it in the theme's accent, in place of colour,
	// and token in that of the theme's.
	off    func(*gunim.UI)
	accent bool
	token  theme.Token[color.NRGBA]
	// must keeps the chip on a bar with no room for them all, cut to
	// what there is, where the rest make way: one that waits for the
	// user.
	must bool
}

// chipSlot is a chip on the bar, which grows in as it comes, and shrinks
// out as it goes. One that says something else goes, and what it says
// now comes in its place.
type chipSlot struct {
	chip
	label *widget.Label
	// used says the slot has a chip, which has come as far as on says;
	// gone that it is on its way out.
	used, gone bool
	on         *anim.Float
	// box is where it is drawn.
	box geom.Rect
}

// chipBar shows the chips.
type chipBar struct {
	anim.Group
	slots []*chipSlot
	// order are the slots in use, as they stand.
	order []*chipSlot
	// limited says the bar has only room wide to stand in, as on a narrow
	// tab bar at the end of the title row, and fades away where it needs
	// more, but for the chips that must stay; on is how far it shows, and
	// wide how wide it is with all of that showing.
	limited bool
	room    float32
	on      *anim.Float
	wide    float32
}

// tabTop is how far below the top of the title bar a tab starts.
const tabTop = 4

// mostChips is how many chips the bar holds, those on their way out
// among them. Its labels are made with it, since the window takes a
// node's children as it is mounted.
const mostChips = 8

func newChipBar() *chipBar {
	b := &chipBar{on: anim.NewFloat(1)}
	b.Add(b.on)
	for range mostChips {
		l := widget.NewLabel("")
		l.Size, l.MaxLines = smallText, 1
		s := &chipSlot{label: l, on: anim.NewFloat(0)}
		b.Add(s.on)
		b.slots = append(b.slots, s)
	}
	return b
}

// show puts chips on the bar. A chip that was there with the same words
// stays, and takes on what it does now.
func (b *chipBar) show(chips []chip) {
	var order []*chipSlot
	for _, c := range chips {
		i := slices.IndexFunc(b.order, func(s *chipSlot) bool { return s.text == c.text && !s.gone && !slices.Contains(order, s) })
		var s *chipSlot
		switch {
		case i >= 0:
			s = b.order[i]
		default:
			s = b.freeSlot()
			s.used, s.label.Text = true, c.text
			s.on.Jump(0)
		}
		s.chip = c
		order = append(order, s)
	}
	// Those going stand where they stood, after what they came after.
	for i, s := range b.order {
		if slices.Contains(order, s) {
			continue
		}
		s.gone = true
		at := 0
		if i > 0 {
			at = slices.Index(order, b.order[i-1]) + 1
		}
		order = slices.Insert(order, at, s)
	}
	b.order = order
}

// freeSlot is a slot with no chip, or else the one whose chip has been on
// its way out the longest, which has gone as of now: one that comes is
// never left out for one that goes.
func (b *chipBar) freeSlot() *chipSlot {
	if i := slices.IndexFunc(b.slots, func(s *chipSlot) bool { return !s.used }); i >= 0 {
		return b.slots[i]
	}
	old := b.slots[0]
	for _, s := range b.order {
		if s.gone && (!old.gone || s.on.Value() < old.on.Value()) {
			old = s
		}
	}
	b.order = slices.DeleteFunc(b.order, func(s *chipSlot) bool { return s == old })
	*old = chipSlot{label: old.label, on: old.on}
	return old
}

// shown are the chips on the bar, but for those on their way out.
func (b *chipBar) shown() []chip {
	var out []chip
	for _, s := range b.order {
		if !s.gone {
			out = append(out, s.chip)
		}
	}
	return out
}

// Children implements [gunim.Composite].
func (b *chipBar) Children() []gunim.Node {
	out := make([]gunim.Node, len(b.slots))
	for i, s := range b.slots {
		out[i] = s.label
	}
	return out
}

// chipPad and chipGap are a chip's room, and chipCross the room its ×
// takes. chipMost is the most its words take, and chipLeast what a chip
// that must stay has, however little there is.
const (
	chipPad, chipGap, chipCross = 10, 6, 14
	chipMost, chipLeast         = 300, 88
)

// beside is what a chip is wide besides its words.
func (s *chipSlot) beside() float32 {
	if s.off != nil {
		return 2*chipPad + chipCross
	}
	return 2 * chipPad
}

// Layout implements [gunim.Node]: the chips side by side, as wide as
// they need, and as far as they have come.
func (b *chipBar) Layout(c gunim.Constraints, f gunim.Frame, kids gunim.Children) geom.Size {
	// As tall as the tab bar beside it, so the two share a middle.
	h := widget.MenubarHeight.Get(f.Theme)
	if c.Max.H > 0 {
		h = min(h, c.Max.H)
	}
	// How wide the chips would be, to see whether they have the room.
	need, must := float32(chipGap), 0
	for _, s := range b.order {
		if s.gone {
			continue
		}
		size := kids.At(slices.Index(b.slots, s)).Layout(gunim.Constraints{Max: geom.Sz(chipMost, h)})
		need += size.W + s.beside() + chipGap
		if s.must {
			must++
		}
	}
	tight := b.limited && need > b.room
	order := b.order[:0]
	x := float32(0)
	for _, s := range b.order {
		stays := !s.gone && (!tight || s.must || must == 0)
		if to := anim.OnOff(stays); s.on.Target() != to {
			s.on.Animate(to, widget.Quick.Get(f.Theme))
			f.RedrawAt(f.Now)
		}
		if s.gone && !s.on.Active() {
			*s = chipSlot{label: s.label, on: s.on}
			continue
		}
		order = append(order, s)
		k := kids.At(slices.Index(b.slots, s))
		most := float32(chipMost)
		if tight && s.must {
			most = max(b.room/float32(must)-2*chipGap, chipLeast) - s.beside()
		}
		size := k.Layout(gunim.Constraints{Max: geom.Sz(most, h)})
		w := size.W + s.beside()
		// The pill and its words share one middle: the tabs', which
		// start a little below the top of the bar, so the chip's words
		// sit on the line the tabs' titles do.
		bh := min(size.H+8, h-tabTop)
		mid := (h + tabTop) / 2
		come := unit(s.on.Value())
		s.box = geom.Rc(x, mid-bh/2, w*come, bh)
		k.Place(geom.Pt(x+chipPad, mid-size.H/2))
		x += (w + chipGap) * come
	}
	clear(b.order[len(order):])
	b.order = order
	if x > 0 {
		x += chipGap
	}
	b.wide = x
	want := anim.OnOff(!tight || must > 0)
	if b.on.Target() != want {
		b.on.Animate(want, widget.Quick.Get(f.Theme))
		f.RedrawAt(f.Now)
	}
	return c.Constrain(geom.Sz(x*min(max(b.on.Value(), 0), 1), h))
}

// Paint implements [gunim.Node]: on the title bar's own colour, as the
// tabs beside it are, not on the window's darker one beneath.
func (b *chipBar) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	if len(b.order) > 0 {
		p.RRect(geom.Rect{Max: box.Point()}, 0, paint.Solid(widget.MenubarFill.Get(f.Theme)))
	}
	if on := min(max(b.on.Value(), 0), 1); on < 1 {
		defer p.Layer(paint.LayerOpts{Bounds: geom.Rect{Max: box.Point()}, Opacity: on, Clip: true})()
	}
	for _, s := range b.order {
		b.paintChip(p, f, s, kids.At(slices.Index(b.slots, s)))
	}
}

// paintChip draws a chip as far as it has come.
func (b *chipBar) paintChip(p *paint.Painter, f gunim.Frame, s *chipSlot, label gunim.Child) {
	if come := unit(s.on.Value()); come < 0.999 {
		defer p.Layer(paint.LayerOpts{Bounds: s.box, Opacity: come, Clip: true, Radius: s.box.Size().H / 2})()
	}
	// A pill in the chip's colour, as a server card's state is: a
	// faint tint of it and a thin ring of it, rather than a dark
	// patch.
	c := s.colour
	switch {
	case s.accent:
		c = widget.Accent.Get(f.Theme)
	case s.token.Key() != "":
		c = s.token.Get(f.Theme)
	}
	fill, ring := c, c
	fill.A, ring.A = 0x22, 0x66
	p.RRectStroke(s.box, s.box.Size().H/2, paint.Solid(fill), paint.Stroke{Width: 1, Color: ring})
	label.Paint(p)
	if s.off != nil {
		paintCross(p, s.cross(), widget.Ink.Get(f.Theme))
	}
}

// cross is where a chip's × is.
func (s *chipSlot) cross() geom.Rect {
	const size = 9
	return geom.Rc(s.box.Max.X-chipPad-size+2, s.box.Min.Y+(s.box.Size().H-size)/2, size, size)
}

// Handle implements [gunim.Handler]: a click on a chip does what it
// offers.
func (b *chipBar) Handle(e gi.Event, u *gunim.UI) bool {
	down, ok := e.(gi.PointerDown)
	if !ok {
		return false
	}
	for _, s := range b.order {
		if s.gone {
			continue
		}
		if s.off != nil && s.cross().Inset(geom.Uniform(-4)).Contains(down.Pos) {
			s.off(u)
			return true
		}
		if s.box.Contains(down.Pos) && s.do != nil {
			s.do(u)
			return true
		}
	}
	return false
}

// showChips puts on the bar what the window is doing for others.
func (w *Window) showChips(st app.State) {
	var chips []chip
	if text, waits := linkChip(w.liveLinks()); text != "" {
		chips = append(chips, chip{text: text, token: look.Guest, must: waits, do: func(u *gunim.UI) { w.linkDialog(u) }})
	}
	if st.Share.Code != "" && len(st.Share.Panes) > 0 {
		chips = append(chips, chip{text: "Agent Share", colour: st.Marks.Agent, do: func(u *gunim.UI) { w.shareDialog(w.share, u) }})
	}
	if w.typeAll {
		// Said plainly while it lasts, as a key typed where it was not
		// meant is typed in every pane.
		chips = append(chips, chip{text: "Typing in All Panes", accent: true, off: func(u *gunim.UI) { w.setTypeAll(false, u) }})
	}
	if st.Serving.On {
		text := "Remote Access"
		if n := len(st.Serving.Clients); n > 0 {
			text += " · " + strconv.Itoa(n)
		}
		chips = append(chips, chip{text: text, colour: st.Marks.Watched, do: func(u *gunim.UI) { w.servingDialog(w.serving, u) }})
	}
	w.chips.show(chips)
}
