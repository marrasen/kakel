package view

import (
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
	gi "github.com/marrasen/gunim/input"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/logs"
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
	st.Tunnels[0].As = settings.SavedTunnel{Host: "web", HostID: "s1", Kind: "local", Listen: ":8080", Target: "localhost:80"}
	st.Tunnels[1].As = settings.SavedTunnel{Host: "db-primary", HostID: "s2", Kind: "socks", Listen: ":1080"}
	st.SavedTunnels = []settings.SavedTunnel{
		{Host: "web", HostID: "s1", Kind: "local", Listen: ":8080", Target: "localhost:80"},
		{Host: "web", HostID: "s1", Kind: "local", Listen: ":5432", Target: "db.internal:5432"},
		{Host: "web", HostID: "s1", Kind: "socks", Listen: ":1080"},
	}
	l := logs.New(0, nil)
	for _, line := range []string{
		"09:12:03  connecting to web",
		"09:12:03  trying the key ~/.ssh/id_ed25519",
		"09:12:04  \x1b[32mconnected in 410ms\x1b[0m",
		"10:41:57  \x1b[31mping unanswered for 15 s\x1b[0m",
		"10:42:02  answering again, 31 ms",
	} {
		_, _ = l.Write([]byte(line + "\n"))
	}
	st.Accounts = []machines.ID{"s1"}
	st.Logs = map[machines.ID]*logs.Lines{"s1": l}
	st.LogEnds = map[machines.ID]int64{"s1": l.End()}
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
			box, ok := lastUI.Bounds(win.cards.cards[m].head.conn.sw)
			if !ok {
				t.Fatalf("no chip on %s", m)
			}
			lastWindow.Input(gi.PointerMove{Pos: box.Center()})
			settle()
			shot("chip-" + string(m))
		}
		// Disconnect asks, with web's terminals and pi through it.
		lastWindow.Input(gi.PointerMove{Pos: geom.Pt(5, 5)})
		box, _ := lastUI.Bounds(win.cards.cards["s1"].head.conn.sw)
		lastWindow.Input(gi.PointerDown{Pos: box.Center(), Button: gi.ButtonPrimary})
		lastWindow.Input(gi.PointerUp{Pos: box.Center(), Button: gi.ButtonPrimary})
		settle()
		shot("disconnect-ask")
	}
	for _, c := range []struct {
		m        machines.ID
		row, out string
	}{
		{"s1", "", "pane-s1"},
		{"", "", "pane-local"},
		{"s3", "", "pane-s3"},
		{"s1", app.MachineRowLog, "pane-s1-log"},
		{"s1", app.MachineRowAddTunnel, "pane-s1-addtunnel"},
		{"s1", tunnelKey("local", ":8080", "localhost:80"), "pane-s1-tunnel"},
		{"s1", app.MachineRowAddSOCKS, "pane-s1-addsocks"},
		{"s1", app.MachineRowLog, "pane-s1-log-narrow"},
	} {
		size := geom.Sz(1300, 860)
		if strings.HasSuffix(c.out, "-narrow") {
			size = geom.Sz(700, 420)
		}
		win, _, publish := windowStageOf(t, size)
		st := shotsState()
		st.Panes = append(st.Panes, app.Pane{ID: "mp", Title: "machine", Kind: app.KindMachine, Machine: c.m, Row: c.row, RowAt: 1})
		st.Stage, st.Focus = &app.Box{Pane: "mp"}, "mp"
		publish(st)
		settle()
		shot(c.out)
		if c.out != "pane-s1" {
			continue
		}
		// Picking a row further down: the selection glides down, and
		// the page comes up from below as the one before fades.
		mp := win.machinePanes["mp"]
		mp.pick(tunnelKey("local", ":8080", "localhost:80"), lastUI)
		for i := 1; i <= 4; i++ {
			frames(3)
			shot("motion-" + strconv.Itoa(i))
		}
	}
}

// TestShotsTabSwitch writes frames of a switch from the Machines pane to
// a machine's, to see the one go and the other come.
func TestShotsTabSwitch(t *testing.T) {
	dir := os.Getenv("KAKEL_SHOTS")
	if dir == "" {
		t.Skip("KAKEL_SHOTS names no folder to write the screenshots to")
	}
	_, _, publish := windowStageOf(t, geom.Sz(1000, 700))
	st := shotsState()
	publish(st)
	settle()
	next := st
	next.Panes = append(slices.Clone(st.Panes), app.Pane{ID: "mp", Title: "web", Kind: app.KindMachine, Machine: "s1"})
	next.Stage, next.Focus = &app.Box{Pane: "mp"}, "mp"
	publish(next)
	for i := 1; i <= 4; i++ {
		frames(2)
		gunimtest.WritePNG(t, filepath.Join(dir, "switch-"+strconv.Itoa(i)+".png"), gunimtest.Pixels(t, lastWindow))
	}
}

// TestShotsTunnelTraffic writes an open tunnel's page as bytes go
// through it, to see its graph.
func TestShotsTunnelTraffic(t *testing.T) {
	dir := os.Getenv("KAKEL_SHOTS")
	if dir == "" {
		t.Skip("KAKEL_SHOTS names no folder to write the screenshots to")
	}
	_, _, publish := windowStageOf(t, geom.Sz(1300, 860))
	st := shotsState()
	m := st.Tunnels[0].Meter
	st.SavedTunnels[0].Auto = true
	st.Panes = append(st.Panes, app.Pane{ID: "mp", Title: "machine", Kind: app.KindMachine, Machine: "s1",
		Row: tunnelKey("local", ":8080", "localhost:80"), RowAt: 1})
	st.Stage, st.Focus = &app.Box{Pane: "mp"}, "mp"
	publish(st)
	// Five seconds of traffic, rising and falling, as frames go by.
	for i := range 300 {
		m.Moved(2000+int(math.Abs(math.Sin(float64(i)/20))*40000), 800, time.Now())
		lastWindow.Frame(time.Second / 60)
		time.Sleep(time.Second / 60)
	}
	shot := func(name string) {
		gunimtest.WritePNG(t, filepath.Join(dir, name+".png"), gunimtest.Pixels(t, lastWindow))
	}
	shot("pane-s1-tunnel-traffic")
}
