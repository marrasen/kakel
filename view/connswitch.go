package view

import (
	"image/color"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/look"
	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/meter"
)

// A machine's status switch is gunim's StatusSwitch: its words say how
// the connection is, and its switch changes it. On connects, opening
// nothing; off disconnects, asking first while something is open; while
// it connects, off cancels; once lost, on connects again. The machine's
// pane has one with the last seconds of the connection's traffic beside
// its switch.

// connSwitch keeps a status switch saying a machine's state.
type connSwitch struct {
	sw *widget.StatusSwitch
	w  *Window
	// of is the machine and what its card says of it.
	of func() (machines.ID, *cardInfo)
	// short leaves out how long it has been connected, where room is
	// short; acted is when it last did what a click does, so a second
	// click does not undo it at once.
	short bool
	acted time.Time
	// graph is the traffic beside the pane's switch, nil on a card's;
	// rate turns rated's totals into speeds.
	graph *widget.LiveGraph
	rate  meter.Rate
	rated *meter.Meter
}

// trafficFloor is the speed, in bytes a second, the traffic graph's
// scale reaches up to at least: the pings an idle connection answers
// draw low, not full height.
const trafficFloor = 4 << 10

func newConnSwitch(w *Window, of func() (machines.ID, *cardInfo), graph bool) *connSwitch {
	c := &connSwitch{sw: widget.NewStatusSwitch(""), w: w, of: of}
	c.sw.OnToggle = func(_ bool, u *gunim.UI) gunim.Intent { return c.act(u) }
	if graph {
		c.graph = widget.NewLiveGraph(500*time.Millisecond, 30)
		c.graph.Compact, c.graph.Width, c.graph.Floor = true, 34, trafficFloor
		c.graph.Sample = c.sample
		c.sw.Extra = c.graph
	}
	return c
}

// stateInk is the colour of a state.
func stateInk(c cardInfo) theme.Token[color.NRGBA] {
	switch {
	case c.state == cardConnected && c.silent, c.state == cardDialing:
		return widget.ToastWarningInk
	case c.state == cardConnected, c.state == cardLocal:
		return widget.ToastSuccessInk
	case c.state == cardLost:
		return widget.DialogDangerInk
	}
	return look.Faint
}

// sync makes the switch say the machine's state at now.
func (c *connSwitch) sync(now time.Time) {
	_, card := c.of()
	if card == nil {
		return
	}
	info := *card
	words, _ := pillFor(info, now, nil)
	if c.short && info.state == cardConnected && !info.silent && info.rtt > 0 {
		words = roundTrip(info.rtt)
	}
	s := c.sw
	s.Label, s.Ink = words, stateInk(info)
	s.Checked = info.state == cardConnected || info.state == cardDialing
	s.Busy = info.state == cardDialing
	switch info.state {
	case cardOff:
		s.Tooltip = "Not connected. Click to connect."
	case cardDialing:
		s.Tooltip = "Connecting. Click to cancel."
	case cardConnected:
		switch {
		case info.silent:
			s.Tooltip = "Not answering. Click to disconnect."
		case info.rtt > 0 && !info.since.IsZero():
			s.Tooltip = "Connected for " + connectedFor(now.Sub(info.since)) + ", round trip " + roundTrip(info.rtt) + ". Click to disconnect."
		default:
			s.Tooltip = "Connected. Click to disconnect."
		}
	case cardLost:
		s.Tooltip = "Connection lost. Click to connect again."
		if _, _, far := c.machine().Far(); far {
			s.Tooltip = "Connection lost. Click to clear it."
		}
	}
	if c.graph != nil {
		c.graph.Ink = s.Ink
		c.graph.SetRunning(info.state == cardConnected && info.traffic != nil)
		if info.traffic != c.rated {
			// A new connection counts from nothing.
			c.rate, c.rated = meter.Rate{}, info.traffic
		}
	}
}

// machine is the machine the switch is for.
func (c *connSwitch) machine() machines.ID {
	m, _ := c.of()
	return m
}

// actGap is how soon after acting the switch acts again: a double click
// would connect and then cancel.
const actGap = 600 * time.Millisecond

// act does what flipping the switch does in the machine's state.
func (c *connSwitch) act(u *gunim.UI) gunim.Intent {
	m, card := c.of()
	if card == nil || time.Since(c.acted) < actGap {
		return nil
	}
	c.acted = time.Now()
	switch card.state {
	case cardOff:
		return app.ConnectMachine{Machine: m}
	case cardDialing:
		return app.Disconnect{Machine: m}
	case cardConnected:
		c.w.disconnect(m, u)
	case cardLost:
		if _, _, far := m.Far(); far {
			return app.ClearMachine{ID: m}
		}
		return app.ConnectMachine{Machine: m}
	}
	return nil
}

// sample is the connection's speed now, in bytes a second, for the
// graph.
func (c *connSwitch) sample() float64 {
	if c.rated == nil {
		return 0
	}
	c.rate.Sample(c.rated, time.Now())
	past := c.rate.Past()
	if len(past) == 0 {
		return 0
	}
	return float64(past[len(past)-1])
}
