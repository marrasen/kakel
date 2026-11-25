package view

import (
	"slices"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/look"
	"github.com/marrasen/kakel/termview"
)

// What a pane shows of the guests of its window: a ring round each pane
// they see, bright round the one a guest is in, and the guest's name at
// the pane's corner as it comes to the pane, and while it types there.
// The name goes a few seconds after, so it covers nothing for long, and
// moves to the corner below while the cursor is under it.

// tagFor is how long a guest's name stands after it came, or typed: long
// enough to read, and no longer, as it covers the end of a line.
const tagFor = 2 * time.Second

// tagAgain is how soon a name that found no room is tried again.
const tagAgain = 100 * time.Millisecond

// mostTags is how many guests a pane names at once.
const mostTags = 3

// tagMargin is the room between the names and the pane's edges, and
// tagGap between two.
var (
	tagMargin = theme.Length("kakel.guest.tag.margin", 7)
	tagGap    = theme.Length("kakel.guest.tag.gap", 4)
)

// guestTags is what a terminal shows of the guests of its window.
type guestTags struct {
	// sees says a guest sees the pane, and here that one is in it.
	sees, here bool
	// ring is how far the ring has come, and near how bright it is for a
	// guest in the pane.
	ring, near *anim.Float
	// badges are the names, by whose they are, "" for none, and low how
	// far they stand at the corner below.
	badges [mostTags]*widget.Badge
	whose  [mostTags]string
	low    *anim.Float
	// at is where each name is.
	at [mostTags]geom.Rect
}

// tagged is a guest's name at a pane.
type tagged struct{ link, name string }

func newGuestTags() guestTags {
	g := guestTags{ring: anim.NewFloat(0), near: anim.NewFloat(0), low: anim.NewFloat(0)}
	for i := range g.badges {
		b := widget.NewBadge("")
		b.Ink = look.Guest
		b.SetHidden(true, nil)
		g.badges[i] = b
	}
	return g
}

// show shows that a guest sees the pane, that one is in it, and the
// names. It reports whether a name found no room, as those going have not
// gone yet.
func (g *guestTags) show(sees, here bool, names []tagged, u *gunim.UI) (later bool) {
	th := u.Theme()
	if sees != g.sees {
		g.sees = sees
		g.ring.Animate(anim.OnOff(sees), widget.Settle.Get(th))
	}
	if here != g.here {
		g.here = here
		g.near.Animate(anim.OnOff(here), widget.Settle.Get(th))
	}
	for i, b := range g.badges {
		if g.whose[i] != "" && !slices.ContainsFunc(names, func(t tagged) bool { return t.link == g.whose[i] }) {
			b.SetHidden(true, u)
			g.whose[i] = ""
		}
	}
	for _, t := range names {
		i := slices.Index(g.whose[:], t.link)
		if i < 0 {
			// Not one that still shows the name before.
			for k, whose := range g.whose {
				if whose == "" && g.at[k].Size().W == 0 {
					i = k
					break
				}
			}
		}
		if i < 0 {
			later = later || slices.Contains(g.whose[:], "")
			continue
		}
		g.whose[i], g.badges[i].Text = t.link, t.name
		g.badges[i].SetHidden(false, u)
	}
	return later
}

// ringed reports whether there is a ring to draw.
func (g *guestTags) ringed() bool { return g.sees || g.ring.Value() > 0.01 }

// ringAlpha is how strong the ring is drawn, of one that glows at a.
func (g *guestTags) ringAlpha(a uint8) uint8 {
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

// Children implements [gunim.Composite]: the cells, and over them the
// guests' names.
func (t *term) Children() []gunim.Node {
	out := t.Term.Children()
	for _, b := range t.guests.badges {
		out = append(out, b)
	}
	return out
}

// Layout implements [gunim.Node]: the terminal, and the names in its
// corner.
func (t *term) Layout(c gunim.Constraints, f gunim.Frame, kids gunim.Children) geom.Size {
	size := t.Term.Layout(c, f, kids)
	margin, gap := tagMargin.Get(f.Theme), tagGap.Get(f.Theme)
	var sizes [mostTags]geom.Size
	var all geom.Size
	for i := range sizes {
		s := kids.At(1 + i).Layout(gunim.Constraints{Max: geom.Sz(max(size.W/3, 0), 0)})
		sizes[i] = s
		if s.W > 0 {
			all = geom.Sz(all.W+s.W+gap, max(all.H, s.H))
		}
	}
	t.guests.at = [mostTags]geom.Rect{}
	if all.W == 0 {
		return size
	}
	// Clear of the cursor: at the corner below while it is at this one.
	caret := t.TextCaret()
	under := t.CursorShown() && caret.Max.X > size.W-margin-all.W-gap && caret.Min.Y < all.H+2*margin
	if to := anim.OnOff(under); t.guests.low.Target() != to {
		t.guests.low.Animate(to, widget.Settle.Get(f.Theme))
		f.RedrawAt(f.Now)
	}
	y := margin + (size.H-2*margin-all.H)*unit(t.guests.low.Value())
	x := size.W - margin
	for i, s := range sizes {
		x -= s.W
		kids.At(1 + i).Place(geom.Pt(x, y))
		t.guests.at[i] = geom.Rc(x, y, s.W, s.H)
		if s.W > 0 {
			x -= gap
		}
	}
	return size
}

// paintTags draws the guests' names, each on the pane's own ground, so
// what is under them does not show through.
func (t *term) paintTags(p *paint.Painter, f gunim.Frame, kids gunim.Children) {
	for i := range mostTags {
		r := t.guests.at[i]
		if r.Size().W < 1 {
			continue
		}
		p.RRect(r, r.Size().H/2, paint.Solid(look.TermBackground.Get(f.Theme)))
		kids.At(1 + i).Paint(p)
	}
}

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

// cameTo is the pane a guest was last in, and when it came there.
type cameTo struct {
	pane string
	at   time.Time
}

// showGuests has each terminal show the guests who see it, and wakes the
// window for the next name to go.
func (w *Window) showGuests(st app.State, u *gunim.UI) {
	now := time.Now()
	if w.guestAt == nil {
		w.guestAt = map[string]cameTo{}
	}
	for _, l := range st.Links {
		if was := w.guestAt[l.ID]; was.pane != l.In {
			w.guestAt[l.ID] = cameTo{l.In, now}
		}
	}
	next := time.Duration(0)
	for _, p := range st.Panes {
		t, ok := w.terms[p.ID]
		if !ok {
			continue
		}
		var names []tagged
		here := false
		for _, l := range st.Links {
			if l.Window != st.Window || l.Stands != app.LinkIn {
				continue
			}
			left := time.Duration(0)
			if l.In == p.ID {
				here = true
				left = tagFor - now.Sub(w.guestAt[l.ID].at)
			}
			if l.TypedIn == p.ID {
				left = max(left, tagFor-now.Sub(l.TypedAt))
			}
			if left <= 0 {
				continue
			}
			names = append(names, tagged{l.ID, guestCalled(l)})
			if next == 0 || left < next {
				next = left
			}
		}
		if t.guests.show(guestSees(st, p), here, names, u) && (next == 0 || next > tagAgain) {
			next = tagAgain
		}
	}
	if w.stopTags != nil {
		w.stopTags()
		w.stopTags = nil
	}
	if next > 0 {
		w.stopTags = u.After(next, func(u *gunim.UI) {
			w.stopTags = nil
			w.showGuests(w.lastChips, u)
			u.Invalidate()
		})
	}
}

// guestCalled is what a share's guest is called, where it has no name.
func guestCalled(l app.WindowLink) string {
	if l.Guest == "" {
		return "A guest"
	}
	return l.Guest
}
