package view

import (
	"slices"
	"strings"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/words"
)

// Disconnecting asks first when anything is open on the machine: what
// closes is said in words, the servers reached through it too, and a
// tick has it not ask about that machine again. Another window's
// connection is always asked about, as others may be working there.

// closing is what disconnecting a machine closes.
type closing struct {
	terminals, files, other, tunnels int
	// through are the machines reached through it, which go with it,
	// and beyond what is open on them.
	through []machines.ID
	beyond  *closing
}

// empty reports whether nothing is open.
func (c closing) empty() bool {
	return c.open() == "" && len(c.through) == 0
}

// paneOn reports whether the pane p runs on m: m's own, or, for a kakel
// window m, one on a machine it reaches.
func paneOn(p app.Pane, m machines.ID) bool {
	if p.On != "" && machines.FarID(p.Machine, p.On) == m {
		return true
	}
	return p.Machine == m && m != ""
}

// closingOn is what disconnecting m would close: its panes, its tunnels
// open, and what is open on the machines reached through it.
func (w *Window) closingOn(m machines.ID) closing {
	var c closing
	w.countOn(m, &c)
	c.beyond = &closing{}
	// A server saved as reached through m, connected now, goes with it,
	// and so on along the chain.
	seen := map[machines.ID]bool{m: true}
	queue := []machines.ID{m}
	for len(queue) > 0 {
		at := queue[0]
		queue = queue[1:]
		for _, h := range w.saved {
			id := machines.ID(h.ID)
			if machines.ID(h.Via) != at || seen[id] || !w.isConnected(id) {
				continue
			}
			seen[id] = true
			c.through = append(c.through, id)
			w.countOn(id, c.beyond)
			queue = append(queue, id)
		}
	}
	// A window's own connections to the servers beyond it.
	for _, id := range w.connected {
		if win, _, far := id.Far(); far && win == m && !seen[id] {
			seen[id] = true
			c.through = append(c.through, id)
		}
	}
	return c
}

// countOn adds what is open on m itself to c.
func (w *Window) countOn(m machines.ID, c *closing) {
	for _, p := range w.panes {
		if !paneOn(p, m) {
			continue
		}
		switch p.Kind {
		case app.KindTerminal:
			c.terminals++
		case app.KindFileManager:
			c.files++
		case app.KindLog, app.KindMachine, app.KindTunnel:
			// Its log and settings stay; a tunnel is counted as one.
		default:
			c.other++
		}
	}
	for _, t := range w.tunnels {
		if t.Machine == m && t.Live {
			c.tunnels++
		}
	}
}

// isConnected reports whether m is connected, or a window reached.
func (w *Window) isConnected(m machines.ID) bool {
	return slices.Contains(w.connected, m) || slices.ContainsFunc(w.remoteWindows, func(rw app.RemoteWindow) bool { return rw.Name == m })
}

// open says what c closes on the machine itself, as a list in a
// sentence, or "".
func (c closing) open() string {
	var parts []string
	if c.terminals > 0 {
		parts = append(parts, words.Count(c.terminals, "terminal"))
	}
	if c.files > 0 {
		parts = append(parts, words.Count(c.files, "file manager"))
	}
	if c.other > 0 {
		parts = append(parts, words.Count(c.other, "other pane"))
	}
	if c.tunnels > 0 {
		parts = append(parts, words.Count(c.tunnels, "tunnel"))
	}
	return joinAnd(parts)
}

// says is what closing c on the machine called name closes, as
// sentences, or "" when nothing does.
func (c closing) says(name string, nameOf func(machines.ID) string) string {
	said := ""
	if open := c.open(); open != "" {
		said = "This closes " + open + " on " + name + "."
	}
	if len(c.through) > 0 {
		var names []string
		for _, id := range c.through {
			names = append(names, nameOf(id))
		}
		if said != "" {
			said += " "
		}
		if len(names) == 1 {
			said += names[0] + " is reached through " + name + ", so it disconnects too"
		} else {
			said += joinAnd(names) + " are reached through " + name + ", so they disconnect too"
		}
		if c.beyond != nil {
			if open := c.beyond.open(); open != "" {
				said += ", with " + open
			}
		}
		said += "."
	}
	return said
}

// joinAnd joins parts as a list in a sentence: "a, b and c".
func joinAnd(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	}
	return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
}

// asksBeforeDisconnect reports whether Disconnect on m asks first, as
// it does unless the user said not to: another window's connection is
// always asked about.
func (w *Window) asksBeforeDisconnect(m machines.ID) bool {
	if _, _, far := m.Far(); far {
		return true
	}
	return !slices.Contains(w.noDisconnectAsk, m)
}

// disconnect disconnects m, asking first while anything is open there
// and the user has not said to stop asking.
func (w *Window) disconnect(m machines.ID, u *gunim.UI) {
	if _, _, far := m.Far(); far {
		w.confirmFarDisconnect(m, u)
		return
	}
	c := w.closingOn(m)
	if c.empty() || !w.asksBeforeDisconnect(m) {
		u.Send(w, app.Disconnect{Machine: m})
		return
	}
	name := w.nameOf(m)
	d := widget.NewDialog("Disconnect " + name + "?")
	stop := widget.NewCheckbox("Don't ask again for " + name)
	body := widget.Column(widget.NewLabel(c.says(name, w.nameOf)), stop)
	body.Cross, body.Gap = widget.CrossStretch, mpGap
	d.Body = body
	d.SetButtons("Disconnect", "Cancel")
	d.Danger = true
	d.OnAccept = func(u *gunim.UI) gunim.Intent {
		if stop.Checked() {
			u.Send(w, app.SetDisconnectAsk{Machine: m, Ask: false})
		}
		return app.Disconnect{Machine: m}
	}
	d.OnDismiss = widget.Sends(app.DialogClosed{})
	w.openDialog(d, u)
}

// confirmFarDisconnect asks before closing another window's connection
// to the machine m it reaches: whoever uses that window loses what is
// open there too.
func (w *Window) confirmFarDisconnect(m machines.ID, u *gunim.UI) {
	window, _, _ := m.Far()
	d := widget.NewDialog("Disconnect " + w.farHostName(m) + " on " + w.nameOf(window) + "?")
	d.Body = widget.NewLabel("This closes " + w.nameOf(window) + "'s own connection to " + w.farHostName(m) +
		", and everything open on it there and on any server reached through it, for anyone working in that window too.")
	d.SetButtons("Disconnect", "Cancel")
	d.Danger = true
	d.OnAccept = widget.Sends(app.Disconnect{Machine: m})
	d.OnDismiss = widget.Sends(app.DialogClosed{})
	w.openDialog(d, u)
}

// connectInPlace is the intent that connects to m and opens nothing,
// its card or pane saying how it goes: a saved server by its ID, one
// typed in by where it was.
func (w *Window) connectInPlace(m machines.ID) gunim.Intent {
	for _, info := range w.machineList {
		if info.ID == m && info.Quick {
			return app.ConnectTo{Target: info.Target, As: m, Only: true, InPlace: true}
		}
	}
	return app.ConnectTo{Server: m, Only: true, InPlace: true}
}
