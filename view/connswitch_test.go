package view

import (
	"slices"
	"strings"
	"testing"

	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/remote"
)

// chipStage is the Machines pane with st, and a click on m's chip.
func chipStage(t *testing.T, st app.State) (*Window, func(m machines.ID)) {
	t.Helper()
	win, _, publish := windowStageOf(t, geom.Sz(1400, 900))
	publish(st)
	settle()
	drain()
	click := func(m machines.ID) {
		t.Helper()
		c, ok := win.cards.cards[m]
		if !ok {
			t.Fatalf("no card for %s", m)
		}
		box, ok := lastUI.Bounds(c.head.conn.sw)
		if !ok {
			t.Fatalf("%s's chip is not laid out", m)
		}
		lastWindow.Input(gi.PointerMove{Pos: box.Center()})
		lastWindow.Input(gi.PointerDown{Pos: box.Center(), Button: gi.ButtonPrimary})
		lastWindow.Input(gi.PointerUp{Pos: box.Center(), Button: gi.ButtonPrimary})
		frames(2)
	}
	return win, click
}

// The chip does what its state asks: Connect connects and opens
// nothing, Cancel gives a connection on its way up, Reconnect connects
// again, all without a pane of the log.
func TestTheChipConnectsWithoutOpeningAnything(t *testing.T) {
	_, click := chipStage(t, shotsState())
	for _, c := range []struct {
		m    machines.ID
		want any
	}{
		{"s5", app.ConnectMachine{Machine: "s5"}},
		{"s4", app.ConnectMachine{Machine: "s4"}},
		{"s3", app.Disconnect{Machine: "s3"}},
	} {
		click(c.m)
		if in := nextIntentPast(t); in != c.want {
			t.Fatalf("%s's chip sent %#v, not %#v", c.m, in, c.want)
		}
	}
}

// Disconnecting a machine with something open asks first, saying what
// closes, the servers reached through it too; ticking Don't ask again
// keeps that for the machine.
func TestDisconnectAsksWhileSomethingIsOpen(t *testing.T) {
	st := shotsState()
	st.Connected = append(st.Connected, "s5")
	st.Panes = append(st.Panes, app.Pane{ID: "t5", Title: "pi", Machine: "s5"})
	win, click := chipStage(t, st)
	click("s1")
	d := win.dialog
	if d == nil {
		t.Fatal("disconnecting web, with terminals open, asked nothing")
	}
	body := d.Body.(*widget.Flex).Children()
	said := body[0].(*widget.Label).Text
	for _, want := range []string{"This closes 2 terminals and 1 tunnel on web, and disconnects pi (1 terminal), which uses web as a jump host."} {
		if !strings.Contains(said, want) {
			t.Fatalf("the question says %q, without %q", said, want)
		}
	}
	body[1].(*widget.Checkbox).SetChecked(true, lastUI)
	if in := d.OnAccept(lastUI); in != (app.Disconnect{Machine: "s1"}) {
		t.Fatalf("Disconnect sent %#v", in)
	}
	if in := nextIntentPast(t); in != (app.SetDisconnectAsk{Machine: "s1", Ask: false}) {
		t.Fatalf("Don't ask again sent %#v", in)
	}
}

// A machine the user said not to ask about, or one with nothing open,
// disconnects at once.
func TestDisconnectGoesAtOnceWhenNotToAsk(t *testing.T) {
	st := shotsState()
	st.NoDisconnectAsk = []machines.ID{"s1"}
	st.Panes = slices.DeleteFunc(st.Panes, func(p app.Pane) bool { return p.Machine == "s2" })
	st.Tunnels = slices.DeleteFunc(st.Tunnels, func(t app.Tunnel) bool { return t.Machine == "s2" })
	win, click := chipStage(t, st)
	for _, m := range []machines.ID{"s1", "s2"} {
		click(m)
		if win.dialog != nil {
			t.Fatalf("disconnecting %s asked first", m)
		}
		if in := nextIntentPast(t); in != (app.Disconnect{Machine: m}) {
			t.Fatalf("%s's chip sent %#v", m, in)
		}
	}
}

// The machine's menu keeps what opens there: its settings and
// Disconnect are on the card, and Remove in its settings.
func TestTheMachineMenuLeavesTheCardsButtonsOut(t *testing.T) {
	win, _ := chipStage(t, shotsState())
	head := win.cards.cards["s1"].head
	win.openMachineMenu(head, lastUI)
	frames(2)
	var labels []string
	for _, it := range head.menu.Items() {
		labels = append(labels, it.Label)
	}
	for _, gone := range []string{"Connection Log", "Disconnect", "Settings…", "Remove This Server…"} {
		if slices.Contains(labels, gone) {
			t.Fatalf("the menu still has %q: %q", gone, labels)
		}
	}
	if !slices.Contains(labels, "New Terminal") || !slices.Contains(labels, "Tunnel…") {
		t.Fatalf("the menu lost what opens there: %q", labels)
	}
}

// A row the program asks a machine's pane for is shown once, and the
// pane says so, so a pane moved to another window does not show it
// again.
func TestARowAskedForIsShownOnce(t *testing.T) {
	st := app.State{Saved: []remote.Host{{ID: "s1", Name: "prod", Address: "prod.example"}}}
	win, _, publish := windowStage(t)
	st.Panes = []app.Pane{{ID: "mp", Kind: app.KindMachine, Machine: "s1", Row: app.MachineRowLog, RowAt: 1}}
	st.Stage, st.Focus = &app.Box{Pane: "mp"}, "mp"
	publish(st)
	frames(2)
	if mp := win.machinePanes["mp"]; mp == nil || mp.sel != app.MachineRowLog {
		t.Fatalf("the pane shows %q", win.machinePanes["mp"].sel)
	}
	for {
		in := nextIntent(t)
		if in == (app.MachineRowShown{Pane: "mp"}) {
			return
		}
	}
}

// The palette has no Connection Log of its own, which had nothing to
// show on this computer: a server's log is its pane's log row.
func TestThePaletteHasNoBareConnectionLog(t *testing.T) {
	win, _ := chipStage(t, shotsState())
	for i, it := range win.palette.Items {
		if it.Title == "Connection Log" {
			t.Fatalf("the palette offers %q, as %q", it.Title, win.paletteIDs[i])
		}
	}
	if win.run("conn.log", lastUI) {
		t.Fatal("conn.log is still a command")
	}
}
