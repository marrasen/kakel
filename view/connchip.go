package view

import (
	"image/color"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/text"
	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/look"
	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/meter"
)

// A card's status chip says how its machine's connection is, and holds
// a switch that changes it: on connects, off disconnects. While it
// connects the switch is on, its knob breathing, and turning it off
// cancels; once lost it is off, in red, and turning it on connects
// again. Its words say only the state, the same whatever a click does.

// connChip is a card's status chip.
type connChip struct {
	anim.Group
	w *Window
	// of is the machine and what its card says of it, nil where there
	// is none.
	of func() (machines.ID, *cardInfo)
	// hover runs to 1 as the pointer comes over it, ring as the
	// keyboard's ring grows round it, and knob from off to on.
	hover, ring, knob *anim.Float
	tip               widget.PartTip
	held              bool
	size              geom.Size
	// compact and bare show only the switch: on a card on a line, or
	// where its words leave the buttons beside it too little room; short
	// leaves out how long it has been connected.
	compact, bare, short bool
	// graph draws the last seconds of the connection's traffic before
	// the switch, from rate.
	graph bool
	rate  meter.Rate
	rated *meter.Meter
	// acted is when it last did what a click does, so a second click,
	// or a key held down, does not undo it at once.
	acted time.Time
}

func newConnChip(w *Window, of func() (machines.ID, *cardInfo)) *connChip {
	c := &connChip{w: w, of: of, hover: anim.NewFloat(0), ring: anim.NewFloat(0), knob: anim.NewFloat(-1)}
	c.Add(c.hover, c.ring, c.knob)
	return c
}

// chipState is what the chip shows and does in its card's state.
type chipState struct {
	words string
	ink   color.NRGBA
	// on says the switch is on, and busy that its knob breathes, as a
	// connection is on its way.
	on, busy bool
	tip      string
	do       func(u *gunim.UI)
}

// state is what the chip shows and does now.
func (c *connChip) state(now time.Time, th *theme.Live) chipState {
	m, card := c.of()
	info := *card
	w, r := c.w, gunim.Node(c)
	words, ink := pillFor(info, now, th)
	if c.short && info.state == cardConnected && !info.silent && info.rtt > 0 {
		words = roundTrip(info.rtt)
	}
	s := chipState{words: words, ink: ink}
	switch info.state {
	case cardOff:
		s.tip = "Not connected. Click to connect."
		s.do = func(u *gunim.UI) { u.Send(r, app.ConnectMachine{Machine: m}) }
	case cardDialing:
		s.on, s.busy = true, true
		s.tip = "Connecting. Click to cancel."
		s.do = func(u *gunim.UI) { u.Send(r, app.Disconnect{Machine: m}) }
	case cardConnected:
		s.on = true
		switch {
		case info.silent:
			s.tip = "Not answering. Click to disconnect."
		case info.rtt > 0 && !info.since.IsZero():
			s.tip = "Connected for " + connectedFor(now.Sub(info.since)) + ", round trip " + roundTrip(info.rtt) + ". Click to disconnect."
		default:
			s.tip = "Connected. Click to disconnect."
		}
		s.do = func(u *gunim.UI) { w.disconnect(m, u) }
	case cardLost:
		if _, _, far := m.Far(); far {
			s.tip = "Connection lost. Click to clear it."
			s.do = func(u *gunim.UI) { u.Send(r, app.ClearMachine{ID: m}) }
			break
		}
		s.tip = "Connection lost. Click to connect again."
		s.do = func(u *gunim.UI) { u.Send(r, app.ConnectMachine{Machine: m}) }
	}
	return s
}

// Focusable implements [gunim.Focusable]: the chip takes the keyboard
// where it does something.
func (c *connChip) Focusable() bool {
	_, card := c.of()
	return card != nil && card.state != cardLocal
}

// The chip's measures: its height, the room at its ends, and its
// switch's track.
const (
	connHeight = 28
	connPad    = 10
	trackW     = 24
	trackH     = 14
)

// width is how wide the chip is: its words, then the switch.
func (c *connChip) width(now time.Time, th *theme.Live) float32 {
	if c.compact {
		return cardChipHeight + 4
	}
	if c.bare {
		return connPad + trackW + connPad
	}
	words := text.Default().Shape(c.state(now, th).words, smallText.Get(th)).Advance
	return connPad + words + 8 + trackW + connPad
}

// Layout implements [gunim.Node].
func (c *connChip) Layout(cs gunim.Constraints, f gunim.Frame, _ gunim.Children) geom.Size {
	if m, card := c.of(); m == machines.Local || card == nil {
		// This computer has no connection to make.
		c.size = geom.Size{}
		return c.size
	}
	// The knob slides to where the state puts it; at first it is there.
	to := float32(0)
	if c.state(f.Now, f.Theme).on {
		to = 1
	}
	if c.knob.Value() < 0 {
		c.knob.Jump(to)
	} else if c.knob.Target() != to {
		c.knob.Animate(to, widget.Settle.Get(f.Theme))
	}
	h := float32(connHeight)
	if c.compact {
		h = cardChipHeight
	}
	c.size = cs.Constrain(geom.Sz(c.width(f.Now, f.Theme), h))
	return c.size
}

// Paint implements [gunim.Node]: the pill in the state's colour, its
// words, and the switch.
func (c *connChip) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, _ gunim.Children) {
	if box.W <= 0 {
		// This computer's card has none.
		return
	}
	th := f.Theme
	s := c.state(f.Now, th)
	if _, card := c.of(); card.state == cardConnected && !card.since.IsZero() {
		f.RedrawAt(f.Now.Add(time.Minute))
	}
	lit := min(max(c.hover.Value(), c.ring.Value()), 1)
	r := geom.Rect{Max: box.Point()}
	radius := box.H / 2
	if c.compact {
		radius = 6
	}
	fill := s.ink
	fill.A = 0x1c
	if c.compact {
		fill.A = 0
	}
	fill = anim.Mix(anim.ColorCodec, fill, widget.ChipHover.Get(th), lit*0.6)
	if c.held {
		fill = widget.ChipHover.Get(th)
	}
	p.RRect(r, radius, paint.Solid(fill))
	if v := c.ring.Value(); v > 0.01 {
		ring := widget.Accent.Get(th)
		ring.A = uint8(float32(ring.A) * min(v, 1))
		p.RRectStroke(r.Inset(geom.Uniform(-2)), radius+2, paint.Fill{}, paint.Stroke{Width: 1.5, Color: ring})
	}
	x := box.W - connPad - trackW
	if c.compact || c.bare {
		x = (box.W - trackW) / 2
	} else {
		run := text.Default().Shape(s.words, smallText.Get(th))
		run.Paint(p, geom.Pt(connPad, box.H/2-run.Height()/2), s.ink)
		if _, card := c.of(); c.graph && card.traffic != nil && card.state == cardConnected {
			c.paintTraffic(p, f, geom.Rc(x-10-graphRoom-6, box.H/2-6, graphRoom+6, 12), card.traffic, s.ink)
		}
	}
	c.paintSwitch(p, f, geom.Rc(x, box.H/2-trackH/2, trackW, trackH), s)
}

// trafficFloor is the speed, in bytes a second, that fills the graph at
// least: the pings an idle connection answers draw low, not full height.
const trafficFloor = 4 << 10

// paintTraffic draws the last seconds of what went over the connection
// as bars in r, when anything has moved.
func (c *connChip) paintTraffic(p *paint.Painter, f gunim.Frame, r geom.Rect, m *meter.Meter, ink color.NRGBA) {
	if c.rated != m {
		// A new connection counts from nothing.
		c.rate, c.rated = meter.Rate{}, m
	}
	// The speeds change once a second.
	f.RedrawAt(f.Now.Add(500 * time.Millisecond))
	c.rate.Sample(m, f.Now)
	past := c.rate.Past()
	peak := uint64(trafficFloor)
	for _, s := range past {
		peak = max(peak, s)
	}
	ink.A = 0xc0
	w := r.Size().W / float32(meter.Samples)
	for i, s := range past {
		if s == 0 {
			continue
		}
		h := max(r.Size().H*float32(s)/float32(peak), 1)
		x := r.Max.X - float32(len(past)-i)*w
		p.RRect(geom.Rc(x, r.Max.Y-h, max(w-1, 1), h), 0, paint.Solid(ink))
	}
}

// paintSwitch draws the switch in track: filled in the state's colour
// while on, an outline while off, its knob breathing while busy.
func (c *connChip) paintSwitch(p *paint.Painter, f gunim.Frame, track geom.Rect, s chipState) {
	th := f.Theme
	t := min(max(c.knob.Value(), 0), 1)
	off := look.Faint.Get(th)
	if _, card := c.of(); card.state == cardLost {
		off = s.ink
	}
	on := s.ink
	edge := anim.Mix(anim.ColorCodec, off, on, t)
	fill := on
	fill.A = uint8(float32(0xd0) * t)
	p.RRect(track, trackH/2, paint.Solid(fill))
	p.RRectStroke(track.Inset(geom.Uniform(0.5)), trackH/2, paint.Fill{}, paint.Stroke{Width: 1, Color: edge})
	knob := float32(trackH - 4)
	kx := track.Min.X + 2 + (track.Size().W-4-knob)*t
	kc := anim.Mix(anim.ColorCodec, off, widget.CardFill.Get(th), t)
	if s.busy {
		// Breathing, a second each way, while it connects.
		phase := float64(f.Now.UnixMilli()%2000) / 1000
		if phase > 1 {
			phase = 2 - phase
		}
		kc.A = uint8(110 + 145*phase)
		f.RedrawAt(f.Now.Add(time.Second / 30))
	}
	p.RRect(geom.Rc(kx, track.Min.Y+2, knob, knob), knob/2, paint.Solid(kc))
}

// actGap is how soon after acting the chip acts again: a double click
// or a key held down would connect and then cancel.
const actGap = 600 * time.Millisecond

// act does what a click does now, unless it has just done so.
func (c *connChip) act(s chipState, u *gunim.UI) {
	if s.do == nil || time.Since(c.acted) < actGap {
		return
	}
	c.acted = time.Now()
	s.do(u)
}

// Handle implements [gunim.Handler]: a click, or Space or Enter with
// the keyboard on it, flips the switch.
func (c *connChip) Handle(e input.Event, u *gunim.UI) bool {
	if _, card := c.of(); card == nil {
		return false
	}
	th := u.Theme()
	s := c.state(time.Now(), th)
	switch e.(type) {
	case input.PointerEnter, input.PointerMove, input.PointerLeave, input.PointerDown:
		c.tip.Handle(e, u, c, s.tip)
	}
	switch e := e.(type) {
	case input.PointerEnter:
		c.hover.Animate(1, widget.Quick.Get(th))
	case input.PointerLeave:
		c.hover.Animate(0, widget.Settle.Get(th))
		c.held = false
	case input.PointerDown:
		if e.Button != input.ButtonPrimary || s.do == nil {
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
			c.act(s, u)
		}
	case input.KeyPress:
		if s.do == nil || e.Mods != 0 || e.Repeat || e.Key != input.KeySpace && e.Key != input.KeyEnter && e.Key != input.KeyKPEnter {
			return false
		}
		c.act(s, u)
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
