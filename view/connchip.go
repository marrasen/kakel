package view

import (
	"image/color"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/icon"
	"github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/text"
	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/machines"
)

// A card's status chip says how its machine's connection is doing, and
// is the button that changes that: Connect while it is not connected,
// Cancel while it connects, Disconnect while it is, Reconnect once it
// is lost. At rest it says the state; with the pointer or the keyboard
// on it, the words turn over to what a click does.

// connChip is a card's status chip.
type connChip struct {
	anim.Group
	r *sideRow
	// hover runs to 1 as the pointer comes over it, and ring as the
	// keyboard's ring grows round it.
	hover, ring *anim.Float
	tip         widget.PartTip
	held        bool
	size        geom.Size
	// compact shows only an icon, for a card on a line; short leaves
	// out how long it has been connected, where room is short.
	compact, short bool
}

func newConnChip(r *sideRow) *connChip {
	c := &connChip{r: r, hover: anim.NewFloat(0), ring: anim.NewFloat(0)}
	c.Add(c.hover, c.ring)
	return c
}

// chipLook is what the chip says and does in its card's state.
type chipLook struct {
	// rest is what it says at rest, in ink, with a dot of its colour, or
	// restIcon in place of the dot; act what it turns over to, with its
	// icon, in actInk.
	rest            string
	ink, actInk     color.NRGBA
	restIcon        *icon.Icon
	act             string
	actIcon         *icon.Icon
	outlined        bool
	tip             string
	do              func(u *gunim.UI)
	dotted, showAct bool
}

// look is what the chip says and does now.
func (c *connChip) look(now time.Time, th *theme.Live) chipLook {
	r := c.r
	info := *r.card
	w, m := r.w, r.machine
	words, ink := pillFor(info, now, th)
	if c.short && info.state == cardConnected && !info.silent && info.rtt > 0 {
		words = roundTrip(info.rtt)
	}
	l := chipLook{rest: words, ink: ink, actInk: ink, dotted: true}
	name := info.name
	switch info.state {
	case cardLocal:
		l.tip = ""
	case cardOff:
		l.rest, l.restIcon, l.dotted, l.outlined = "Connect", icon.Plug, false, true
		l.ink, l.actInk = widget.Ink.Get(th), widget.Ink.Get(th)
		l.act, l.actIcon = "Connect", icon.Plug
		l.tip = "Connect to " + name + " without opening anything"
		l.do = func(u *gunim.UI) { u.Send(r, w.connectInPlace(m)) }
	case cardDialing:
		l.act, l.actIcon, l.actInk, l.showAct = "Cancel", icon.X, widget.Ink.Get(th), true
		l.tip = "Connecting to " + name + ". Click to give up."
		l.do = func(u *gunim.UI) { u.Send(r, app.Disconnect{Machine: m}) }
	case cardConnected:
		l.act, l.actIcon, l.actInk, l.showAct = "Disconnect", icon.Unplug, widget.DialogDangerInk.Get(th), true
		switch {
		case info.silent:
			l.tip = name + " has stopped answering. Click to disconnect."
		case info.rtt > 0 && !info.since.IsZero():
			l.tip = "Connected for " + connectedFor(now.Sub(info.since)) + ", round trip " + roundTrip(info.rtt) + ". Click to disconnect."
		default:
			l.tip = "Connected. Click to disconnect."
		}
		l.do = func(u *gunim.UI) { w.disconnect(m, u) }
	case cardLost:
		if _, _, far := m.Far(); far {
			l.act, l.actIcon, l.actInk, l.showAct = "Clear", icon.X, widget.Ink.Get(th), true
			l.tip = "The connection to " + name + " was lost. Click to clear it."
			l.do = func(u *gunim.UI) { u.Send(r, app.ClearMachine{ID: m}) }
			break
		}
		l.act, l.actIcon, l.actInk, l.showAct = "Reconnect", icon.RotateCw, widget.Ink.Get(th), true
		l.tip = "The connection to " + name + " was lost. Click to connect again."
		l.do = func(u *gunim.UI) { u.Send(r, w.connectInPlace(m)) }
	}
	return l
}

// Focusable implements [gunim.Focusable]: the chip takes the keyboard
// where it does something.
func (c *connChip) Focusable() bool { return c.r.card != nil && c.r.card.state != cardLocal }

// connHeight is the chip's height, and connPad the room at its ends.
const (
	connHeight = 28
	connPad    = 9
)

// widths are how wide the chip is at rest and with its words turned
// over to what a click does.
func (c *connChip) widths(now time.Time, th *theme.Live) (rest, act float32) {
	if c.compact {
		return cardChipHeight, cardChipHeight
	}
	l := c.look(now, th)
	size := smallText.Get(th)
	rest = connPad + leadOf(l.restIcon) + 5 + text.Default().Shape(l.rest, size).Advance + connPad
	act = rest
	if l.showAct {
		act = connPad + leadOf(l.actIcon) + 5 + text.Default().Shape(l.act, size).Advance + connPad
	}
	return rest, act
}

// leadOf is the room before the words: an icon's, or a dot's.
func leadOf(ic *icon.Icon) float32 {
	if ic != nil {
		return 14
	}
	return 6
}

// width is the room the chip takes: its wider words', so it can grow to
// them under the pointer without moving what is beside it.
func (c *connChip) width(now time.Time, th *theme.Live) float32 {
	rest, act := c.widths(now, th)
	return max(rest, act)
}

// Layout implements [gunim.Node].
func (c *connChip) Layout(cs gunim.Constraints, f gunim.Frame, _ gunim.Children) geom.Size {
	if c.r.machine == machines.Local {
		// This computer has no connection to make.
		c.size = geom.Size{}
		return c.size
	}
	h := float32(connHeight)
	if c.compact {
		h = cardChipHeight
	}
	c.size = cs.Constrain(geom.Sz(c.width(f.Now, f.Theme), h))
	return c.size
}

// Paint implements [gunim.Node]: the pill, its dot or icon and its
// words, turning over to what a click does as the pointer comes.
func (c *connChip) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, _ gunim.Children) {
	if box.W <= 0 {
		// This computer's card has none.
		return
	}
	th := f.Theme
	l := c.look(f.Now, th)
	if c.r.card.state == cardConnected && !c.r.card.since.IsZero() {
		f.RedrawAt(f.Now.Add(time.Minute))
	}
	t := float32(0)
	if l.do != nil {
		t = min(max(c.hover.Value(), c.ring.Value()), 1)
	}
	if !l.showAct && l.do != nil {
		// Its words already say what it does: it lights instead.
		t = 0
	}
	if c.compact {
		c.paintIcon(p, l, box, th)
		return
	}
	ink := anim.Mix(anim.ColorCodec, l.ink, l.actInk, t)
	restW, actW := c.widths(f.Now, th)
	pillW := restW + (actW-restW)*t
	x0 := box.W - pillW
	r := geom.Rect{Min: geom.Pt(x0, 0), Max: box.Point()}
	radius := box.H / 2
	fill := ink
	fill.A = 0x22
	if l.outlined {
		fill = widget.CardFill.Get(th)
		if l.do != nil {
			fill = anim.Mix(anim.ColorCodec, fill, widget.ChipHover.Get(th), min(max(c.hover.Value(), c.ring.Value()), 1))
		}
	}
	if c.held {
		fill.A = min(255, fill.A+0x22)
	}
	p.RRect(r, radius, paint.Solid(fill))
	if l.outlined || t > 0 {
		edge := widget.DialogBorder.Get(th)
		if !l.outlined {
			edge = ink
			edge.A = uint8(float32(0x66) * t)
		}
		p.RRectStroke(r.Inset(geom.Uniform(0.5)), radius, paint.Fill{}, paint.Stroke{Width: 1, Color: edge})
	}
	if v := c.ring.Value(); v > 0.01 {
		ring := widget.Accent.Get(th)
		ring.A = uint8(float32(ring.A) * min(v, 1))
		p.RRectStroke(r.Inset(geom.Uniform(-2)), radius+2, paint.Fill{}, paint.Stroke{Width: 1.5, Color: ring})
	}
	size := smallText.Get(th)
	cy := box.H / 2
	// The words at rest fade as the action's come up over them, each
	// after its icon or dot.
	fade := func(c color.NRGBA, a float32) color.NRGBA {
		c.A = uint8(float32(c.A) * min(max(a, 0), 1))
		return c
	}
	say := func(words string, ic *icon.Icon, dot bool, a float32) {
		x := x0 + connPad
		switch {
		case ic != nil:
			drawIcon(p, ic, geom.Rc(x, cy-7, 14, 14), fade(ink, a), 1.6)
		case dot:
			p.RRect(geom.Rc(x, cy-3, 6, 6), 3, paint.Solid(fade(ink, a)))
		}
		run := text.Default().Shape(words, size)
		run.Paint(p, geom.Pt(x+leadOf(ic)+5, cy-run.Height()/2), fade(ink, a))
	}
	if rest := 1 - t; rest > 0.01 {
		say(l.rest, l.restIcon, l.dotted, rest)
	}
	if t > 0.01 && l.showAct {
		say(l.act, l.actIcon, false, t)
	}
}

// paintIcon draws a compact card's chip as its other buttons are drawn,
// clear until the pointer comes, with the icon of what a click does: the
// dot on the card's badge says the state.
func (c *connChip) paintIcon(p *paint.Painter, l chipLook, box geom.Size, th *theme.Live) {
	ic := l.actIcon
	if ic == nil {
		ic = l.restIcon
	}
	if ic == nil {
		return
	}
	t := min(max(c.hover.Value(), c.ring.Value()), 1)
	r := geom.Rect{Max: box.Point()}
	if t > 0.01 || c.held {
		fill := widget.ChipHover.Get(th)
		fill.A = uint8(float32(fill.A) * t)
		p.RRect(r, 6, paint.Solid(fill))
	}
	if v := c.ring.Value(); v > 0.01 {
		ring := widget.Accent.Get(th)
		ring.A = uint8(float32(ring.A) * min(v, 1))
		p.RRectStroke(r.Inset(geom.Uniform(-1)), 7, paint.Fill{}, paint.Stroke{Width: 1.5, Color: ring})
	}
	ink := anim.Mix(anim.ColorCodec, widget.Ink.Get(th), l.actInk, t)
	drawIcon(p, ic, geom.Rc(box.W/2-8, box.H/2-8, 16, 16), ink, 1.6)
}

// Handle implements [gunim.Handler]: a click, or Space or Enter with
// the keyboard on it, does what it says.
func (c *connChip) Handle(e input.Event, u *gunim.UI) bool {
	if c.r.card == nil {
		return false
	}
	th := u.Theme()
	l := c.look(time.Now(), th)
	switch e.(type) {
	case input.PointerEnter, input.PointerMove, input.PointerLeave, input.PointerDown:
		c.tip.Handle(e, u, c, l.tip)
	}
	switch e := e.(type) {
	case input.PointerEnter:
		c.hover.Animate(1, widget.Quick.Get(th))
	case input.PointerLeave:
		c.hover.Animate(0, widget.Settle.Get(th))
		c.held = false
	case input.PointerDown:
		if e.Button != input.ButtonPrimary || l.do == nil {
			return false
		}
		c.held = true
		c.tip.Hide(u)
	case input.PointerUp:
		if !c.held {
			return false
		}
		c.held = false
		if (geom.Rect{Max: c.size.Point()}).Contains(e.Pos) {
			l.do(u)
		}
	case input.KeyPress:
		if l.do == nil || e.Mods != 0 || e.Key != input.KeySpace && e.Key != input.KeyEnter && e.Key != input.KeyKPEnter {
			return false
		}
		l.do(u)
	case input.FocusRing:
		to := float32(0)
		if e.On {
			to = 1
		}
		c.ring.Animate(to, widget.Quick.Get(th))
	case input.FocusLost:
		c.ring.Animate(0, widget.Settle.Get(th))
		return false
	default:
		return false
	}
	u.Invalidate()
	return true
}
