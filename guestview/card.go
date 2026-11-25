package guestview

import (
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/text"
	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/appicon"
)

// The pages with something to say stand as the one the client loads on
// does, so that one fades into the other with nothing moving: the icon,
// a heading, a sentence, and what there is to do about it.

// The measures of a card: the widest it is, the room at its sides, the
// icon's side, and the room over its parts.
var (
	cardWide  = theme.Length("kakel.page.card.wide", 416)
	cardSide  = theme.Length("kakel.page.card.side", 24)
	cardIcon  = theme.Length("kakel.page.card.icon", 72)
	cardTitle = theme.FontSize("kakel.page.card.title", 21.6)
	cardText  = theme.FontSize("kakel.page.card.text", 15)
	// cardPart is how wide what there is to do is.
	cardPart = theme.Length("kakel.page.card.part", 300)
	cardBar  = theme.Length("kakel.page.card.bar", 224)
	// cardOver and cardUp are where the icon's top stands: so far over
	// the middle, and never nearer the top than cardUp.
	cardOver = theme.Length("kakel.page.card.over", 130)
	cardUp   = theme.Length("kakel.page.card.up", 24)
	// cardWhyRoom and cardBusyRoom are the room over what went wrong, and
	// over the bar of a join on its way.
	cardWhyRoom  = theme.Insets("kakel.page.card.why.room", geom.Insets{Top: 10})
	cardBusyRoom = theme.Insets("kakel.page.card.busy.room", geom.Insets{Top: 16})
)

// breath is how long the icon takes to grow while the page waits, and to
// go back, and breathes how much it grows.
const (
	breath   = 2400 * time.Millisecond
	breathes = 0.06
)

// appIcon is kakel's icon, large enough for a sharp screen.
var appIcon = sync.OnceValue(func() *paint.Image { return paint.NewImage(appicon.Draw(216)) })

// iconNode draws kakel's icon, which breathes while the page waits.
type iconNode struct {
	anim.Group
	waits bool
	out   *anim.Float
}

func newIconNode(waits bool) *iconNode {
	n := &iconNode{waits: waits, out: anim.NewFloat(0)}
	n.Add(n.out)
	return n
}

// Layout implements [gunim.Node].
func (n *iconNode) Layout(_ gunim.Constraints, f gunim.Frame, _ gunim.Children) geom.Size {
	if n.waits && !n.out.Active() && theme.Scale.Get(f.Theme) > 0 {
		n.out.Animate(1-n.out.Target(), anim.Tween{Duration: breath, Ease: anim.EaseInOut})
	}
	s := cardIcon.Get(f.Theme)
	return geom.Sz(s, s)
}

// Paint implements [gunim.Node].
func (n *iconNode) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, _ gunim.Children) {
	if k := 1 + breathes*n.out.Value(); k != 1 {
		defer p.Push(paint.Scale(k, geom.Pt(box.W/2, box.H/2)))()
	}
	p.Image(appIcon(), geom.Rect{Max: box.Point()}, paint.ImageOpts{Opacity: 1})
}

// cardItem is one part of a card, over the next: how far under the one
// before it stands, and how wide it is, 0 for as wide as the card.
type cardItem struct {
	node gunim.Node
	gap  float32
	wide *theme.Token[float32]
}

// card is one of the pages with something to say.
type card struct {
	scroll *widget.Scroll
	body   *cardBody
	title  *widget.Label
	text   *widget.Label
	// first takes the keyboard.
	first gunim.Node
	// show shows a state, of the same page.
	show func(st State, u *gunim.UI)
}

// cardBody lays a card's parts out, one under another.
type cardBody struct {
	items []cardItem
	// room is how tall the page is.
	room float32
}

func newCard(title, says string, waits bool, items ...cardItem) *card {
	c := &card{title: widget.NewLabel(title), text: widget.NewLabel(says)}
	c.title.Face, c.title.Size, c.title.Align = widget.BoldFont, cardTitle, text.AlignCenter
	c.text.Size, c.text.Color, c.text.Align = cardText, quietInk, text.AlignCenter
	parts := []cardItem{{node: newIconNode(waits)}, {node: c.title, gap: 20}}
	if says != "" {
		parts = append(parts, cardItem{node: c.text, gap: 8})
	}
	c.body = &cardBody{items: append(parts, items...)}
	c.scroll = widget.NewScroll(c.body)
	return c
}

// says is what the card says, in one go.
func (c *card) says() string {
	if c.text.Text == "" {
		return c.title.Text + "."
	}
	return c.title.Text + ". " + c.text.Text
}

// update shows st, a state of the page it is already.
func (c *card) update(st State, u *gunim.UI) {
	if c.show != nil {
		c.show(st, u)
	}
}

// Children implements [gunim.Composite].
func (c *card) Children() []gunim.Node { return []gunim.Node{c.scroll} }

// Layout implements [gunim.Node].
func (c *card) Layout(cs gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	c.body.room = cs.Max.H
	k := kids.At(0)
	k.Layout(gunim.Tight(cs.Max))
	k.Place(geom.Point{})
	return cs.Max
}

// Paint implements [gunim.Node].
func (c *card) Paint(p *paint.Painter, _ gunim.Frame, _ geom.Size, kids gunim.Children) {
	kids.At(0).Paint(p)
}

// Children implements [gunim.Composite].
func (b *cardBody) Children() []gunim.Node {
	out := make([]gunim.Node, len(b.items))
	for i, it := range b.items {
		out[i] = it.node
	}
	return out
}

// Layout implements [gunim.Node].
func (b *cardBody) Layout(c gunim.Constraints, f gunim.Frame, kids gunim.Children) geom.Size {
	th := f.Theme
	side := cardSide.Get(th)
	wide := max(min(c.Max.W, cardWide.Get(th))-2*side, 0)
	y := max(cardUp.Get(th), b.room/2-cardOver.Get(th))
	for i, it := range b.items {
		w := wide
		if it.wide != nil {
			w = min(w, it.wide.Get(th))
		}
		y += it.gap
		k := kids.At(i)
		cs := gunim.Constraints{Max: geom.Sz(w, 0)}
		if it.wide != nil {
			cs.Min.W = w
		}
		s := k.Layout(cs)
		k.Place(geom.Pt((c.Max.W-s.W)/2, y))
		y += s.H
	}
	return geom.Sz(c.Max.W, y+side)
}

// Paint implements [gunim.Node].
func (b *cardBody) Paint(p *paint.Painter, _ gunim.Frame, _ geom.Size, kids gunim.Children) {
	for k := range kids.All {
		k.Paint(p)
	}
}

// busyBar is a bar for something on its way.
func busyBar() *widget.ProgressBar {
	b := widget.NewProgressBar()
	b.Indeterminate = true
	return b
}

// joinCard asks for the guest's name, and joins.
func joinCard(p *page, st State) *card {
	name := widget.NewTextField()
	name.SetText(st.Name, nil)
	// The one from last time goes as another is typed.
	name.Select(0, utf8.RuneCountInString(st.Name))
	join := widget.NewButton("Join")
	join.Kind = widget.ButtonPrimary
	why := widget.NewLabel(st.Trouble)
	why.Color, why.Align = widget.DialogProblem, text.AlignCenter
	trouble := widget.NewFold(padded(why, cardWhyRoom), st.Trouble != "")
	busy := widget.NewFold(padded(busyBar(), cardBusyRoom), st.Busy)

	c := newCard(joinTitle, joinSays, false,
		cardItem{node: widget.NewForm().Add("Name", name), gap: 24, wide: &cardPart},
		cardItem{node: trouble, wide: &cardPart},
		cardItem{node: join, gap: 14, wide: &cardPart},
		cardItem{node: busy, wide: &cardBar},
	)
	c.first = name
	say := func(words string, u *gunim.UI) {
		if words != "" {
			why.Text = words
		}
		trouble.SetOpen(words != "", u)
		u.Invalidate()
	}
	do := func(u *gunim.UI) gunim.Intent {
		if join.Disabled {
			return nil
		}
		called := strings.TrimSpace(name.Text())
		if called == "" {
			say("Enter a name", u)
			u.Focus(name)
			return nil
		}
		return Join{Name: called}
	}
	join.OnClick = do
	name.OnCommit = func(_ string, u *gunim.UI) gunim.Intent { return do(u) }
	name.OnChange = func(_ string, u *gunim.UI) gunim.Intent {
		say("", u)
		return nil
	}
	c.show = func(st State, u *gunim.UI) {
		say(st.Trouble, u)
		busy.SetOpen(st.Busy, u)
		join.Disabled, name.Disabled = st.Busy, st.Busy
	}
	return c
}

// waitingCard waits for the host's answer, or for the connection. Its
// button has no keyboard until it is asked for: an Enter too many on the
// page before would cancel, and the link is used up.
func waitingCard(st State) *card {
	title, says := waitTitle, waitSays
	if st.Lost {
		title, says = backTitle, backSays
	}
	stop := widget.NewButton("Cancel")
	stop.OnClick = widget.Sends(Leave{})
	return newCard(title, says, true,
		cardItem{node: busyBar(), gap: 20, wide: &cardBar},
		cardItem{node: stop, gap: 24},
	)
}

// overCard says how it ended.
func overCard(o Over) *card {
	var items []cardItem
	var act *widget.Button
	if o.Act != ActNone {
		act = widget.NewButton(o.Word)
		act.Kind = widget.ButtonPrimary
		act.OnClick = widget.Sends(Do{o.Act})
		items = append(items, cardItem{node: act, gap: 24})
	}
	c := newCard(o.Title, o.Text, false, items...)
	if act != nil {
		c.first = act
	}
	return c
}

// padded is child with room over it.
func padded(child gunim.Node, room theme.Token[geom.Insets]) gunim.Node {
	p := widget.NewPad(child)
	p.Padding = room
	return p
}
