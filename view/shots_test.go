package view

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
	gi "github.com/marrasen/gunim/input"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/meter"
	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/settings"
)

// Screenshots of the Machines cards and a machine's pane, for looking
// at the pixels, which no test can check: run with KAKEL_SHOTS set to a
// folder, and DISPLAY to a private X server, as
//
//	DISPLAY=:57 KAKEL_SHOTS=/tmp/shots go test ./view -run TestShots

// shotsState is a window holding machines in each state: connected,
// connecting, lost, not connected, one with a long name, one through
// another, with terminals and tunnels open.
func shotsState() app.State {
	st := withServers(app.State{Focus: "ps"})
	st.Saved = []remote.Host{
		{ID: "s1", Name: "web", Address: "web.example.com", User: "deploy"},
		{ID: "s2", Name: "db-primary", Address: "10.0.0.12", User: "postgres", Port: 2222},
		{ID: "s3", Name: "build server in the basement", Address: "buildbox-01.internal.corp.example.net", User: "jenkins-runner"},
		{ID: "s4", Name: "nas", Address: "nas.local", User: "admin"},
		{ID: "s5", Name: "pi", Address: "raspberrypi", User: "pi", Via: "s1"},
	}
	now := time.Now()
	st.Machines = []machines.Info{
		{ID: "s1", Name: "web", Since: now.Add(-95 * time.Minute), RTT: 23 * time.Millisecond},
		{ID: "s2", Name: "db-primary", Since: now.Add(-3 * time.Minute), RTT: 140 * time.Millisecond},
		{ID: "s3", Name: "build server in the basement"},
		{ID: "s4", Name: "nas"},
		{ID: "s5", Name: "pi"},
	}
	st.Connected = []machines.ID{"s1", "s2"}
	st.Dialing = []machines.ID{"s3"}
	st.Dropped = []machines.ID{"s4"}
	st.Panes = append(st.Panes,
		app.Pane{ID: "t1", Title: "deploy@web: ~/app", Machine: "s1"},
		app.Pane{ID: "t2", Title: "htop", Machine: "s1"},
		app.Pane{ID: "t3", Title: "psql", Machine: "s2"},
		app.Pane{ID: "t4", Title: "~/src/kakel"},
	)
	m := meter.New()
	m.Moved(30000, 4000, now)
	st.Tunnels = []app.Tunnel{
		{ID: "u1", Machine: "s1", Label: ":8080 → localhost:80", Note: "2 streams", Live: true, Meter: m},
		{ID: "u2", Machine: "s2", Label: "SOCKS :1080", Note: "idle", Live: true, Meter: meter.New()},
	}
	st.SavedTunnels = []settings.SavedTunnel{{HostID: "s1", Kind: "local", Listen: ":8080", Target: "localhost:80"}}
	return st
}

func TestShots(t *testing.T) {
	dir := os.Getenv("KAKEL_SHOTS")
	if dir == "" {
		t.Skip("KAKEL_SHOTS names no folder to write the screenshots to")
	}
	shot := func(name string) {
		gunimtest.WritePNG(t, filepath.Join(dir, name+".png"), gunimtest.Pixels(t, lastWindow))
	}
	for _, width := range []float32{1400, 1000, 760, 400} {
		win, _, publish := windowStageOf(t, geom.Sz(width, 900))
		publish(shotsState())
		settle()
		w := strconv.Itoa(int(width))
		shot("cards-" + w)
		if width != 1000 {
			continue
		}
		// The pointer on each card's chip: what a click would do.
		for _, m := range []machines.ID{"s1", "s3", "s4", "s5"} {
			box, ok := lastUI.Bounds(win.cards.cards[m].head.conn)
			if !ok {
				t.Fatalf("no chip on %s", m)
			}
			lastWindow.Input(gi.PointerMove{Pos: box.Center()})
			settle()
			shot("chip-" + string(m))
		}
		// Disconnect asks, with web's terminals and pi through it.
		lastWindow.Input(gi.PointerMove{Pos: geom.Pt(5, 5)})
		box, _ := lastUI.Bounds(win.cards.cards["s1"].head.conn)
		lastWindow.Input(gi.PointerDown{Pos: box.Center(), Button: gi.ButtonPrimary})
		lastWindow.Input(gi.PointerUp{Pos: box.Center(), Button: gi.ButtonPrimary})
		settle()
		shot("disconnect-ask")
	}
	for _, m := range []machines.ID{"s1", "", "s3"} {
		_, _, publish := windowStageOf(t, geom.Sz(1300, 860))
		st := shotsState()
		st.Panes = append(st.Panes, app.Pane{ID: "mp", Title: "machine", Kind: app.KindMachine, Machine: m})
		st.Stage, st.Focus = &app.Box{Pane: "mp"}, "mp"
		publish(st)
		settle()
		name := string(m)
		if name == "" {
			name = "local"
		}
		shot("pane-" + name)
	}
}
