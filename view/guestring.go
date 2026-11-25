package view

import (
	"slices"
	"strings"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/look"
	"github.com/marrasen/kakel/termview"
)

// What a window shows of its guests: a ring round each pane they see,
// bright round the one a guest is in, and the guest's name on that pane's
// tab, which covers no text.

// guestRing is what a terminal shows of the guests of its window.
type guestRing struct {
	// sees says a guest sees the pane, and here that one is in it.
	sees, here bool
	// ring is how far the ring has come, and near how bright it is for a
	// guest in the pane.
	ring, near *anim.Float
}

func newGuestRing() guestRing { return guestRing{ring: anim.NewFloat(0), near: anim.NewFloat(0)} }

// show shows that a guest sees the pane, and that one is in it.
func (g *guestRing) show(sees, here bool, u *gunim.UI) {
	th := u.Theme()
	if sees != g.sees {
		g.sees = sees
		g.ring.Animate(anim.OnOff(sees), widget.Settle.Get(th))
	}
	if here != g.here {
		g.here = here
		g.near.Animate(anim.OnOff(here), widget.Settle.Get(th))
	}
}

// ringed reports whether there is a ring to draw.
func (g *guestRing) ringed() bool { return g.sees || g.ring.Value() > 0.01 }

// ringAlpha is how strong the ring is drawn, of one that glows at a.
func (g *guestRing) ringAlpha(a uint8) uint8 {
	const faint, full = 0.8, 1.7
	k := faint + (full-faint)*unit(g.near.Value())
	return uint8(min(float32(a)*k*unit(g.ring.Value()), 0xff))
}

// paintGuestRing draws the ring round a pane a guest sees, in the room
// between the cells and the pane's edge, which has no text in it, where
// there is that room.
func (t *term) paintGuestRing(p *paint.Painter, f gunim.Frame, box geom.Size) {
	if !t.guests.ringed() {
		return
	}
	c := look.Guest.Get(f.Theme)
	c.A = t.guests.ringAlpha(ringGlow(f.Now))
	corner := min(t.Cells().CellSize().W, t.Cells().CellSize().H)
	out := termview.Padding.Get(f.Theme) / 2
	if out < markWidth/2.0 {
		out = -markWidth / 2.0
	}
	r := geom.Rect{Max: box.Point()}.Inset(geom.Uniform(-out))
	if r.Empty() {
		return
	}
	p.RRectStroke(r, max(corner+out, 0), paint.Fill{}, paint.Stroke{Width: markWidth, Color: c})
}

// unit is v held between 0 and 1.
func unit(v float32) float32 { return min(max(v, 0), 1) }

// guestSees reports whether a guest of the window pane p is in sees it.
func guestSees(st app.State, p app.Pane) bool {
	win := p.Window
	if win == 0 {
		win = st.Window
	}
	return p.Kind == app.KindTerminal && slices.ContainsFunc(st.Links, func(l app.WindowLink) bool {
		return l.Window == win && l.GuestIn()
	})
}

// showGuests has each terminal show the guests who see it.
func (w *Window) showGuests(st app.State, u *gunim.UI) {
	for _, p := range st.Panes {
		if t, ok := w.terms[p.ID]; ok {
			t.guests.show(guestSees(st, p), guestsIn(st, p.ID) != "", u)
		}
	}
}

// guestsIn are the names of the window's guests in pane id, "" for none.
func guestsIn(st app.State, id string) string {
	var names []string
	for _, l := range st.Links {
		if l.Window == st.Window && l.Stands == app.LinkIn && l.In == id {
			names = append(names, guestCalled(l))
		}
	}
	return strings.Join(names, ", ")
}

// guestCalled is what a share's guest is called, where it has no name.
func guestCalled(l app.WindowLink) string {
	if l.Guest == "" {
		return "A guest"
	}
	return l.Guest
}

// guestMarked is that where kakel's own words stand too: what a guest calls
// itself is said to be a guest's name.
func guestMarked(l app.WindowLink) string {
	if l.Guest == "" {
		return guestCalled(l)
	}
	return "Guest: " + l.Guest
}
