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

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/guest"
	"github.com/marrasen/kakel/input"
	"github.com/marrasen/kakel/internal/sessiontest"
	"github.com/marrasen/kakel/keys"
	"github.com/marrasen/kakel/logs"
	"github.com/marrasen/kakel/look"
	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/meter"
	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/screen"
	"github.com/marrasen/kakel/serve"
	"github.com/marrasen/kakel/settings"
	"github.com/marrasen/kakel/themes"
	"github.com/marrasen/kakel/ui"
	"github.com/marrasen/kakel/vt"
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
	st = withDefaults(st)
	// What opens by default, as the user named and picked it.
	st.Defaults[""] = app.MachineDefaults{Shell: "PowerShell", Files: "Home", FilesKey: app.FilesFound + app.FoundHome,
		Shells: []app.ShellEntry{{Action: "terminal", Name: "PowerShell", Default: true}}}
	st.Defaults["s1"] = app.MachineDefaults{Shell: "tmux", Files: "www", FilesKey: app.FilesFavourite + "/srv/www",
		Shells: []app.ShellEntry{{Action: "login", Name: "Login Shell"}, {Action: "shell:profile:tmux", Name: "tmux", Default: true}}}
	st.Favourites = []app.Favourite{{Machine: "s1", Path: "/srv/www"}, {Path: "/home/me/Documents", Color: "blue", Icon: "file-text"},
		{Machine: "s1", Path: "/var/log", Name: "Logs", Color: "orange", Icon: "archive"}}
	st.Locations = []app.Favourite{{Path: "/home/me/src", Name: "Source", Color: "green", Icon: "code"},
		{Machine: "s1", Path: "/etc/nginx", Name: "nginx", Color: "teal", Icon: "server"}}
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
		{"", mpFolder + app.FilesLocation + "/home/me/src", "pane-local-location"},
		{"s1", mpFolder + app.FilesFavourite + "/var/log", "pane-s1-favourite"},
		{"s1", mpAddLoc, "pane-s1-addlocation"},
	} {
		size := geom.Sz(1300, 860)
		if strings.HasSuffix(c.out, "-narrow") {
			size = geom.Sz(700, 420)
		}
		win, _, publish := windowStageOf(t, size)
		st := shotsState()
		st.Panes = append(st.Panes, app.Pane{ID: "mp", Title: "machine", Kind: app.KindMachine, Machine: c.m, Row: c.row, RowAt: 1})
		st.Layout, st.Focus = boxLayout(&app.Box{Pane: "mp"}), "mp"
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
	next.Layout, next.Focus = boxLayout(&app.Box{Pane: "mp"}), "mp"
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
	account := logs.New(0, nil)
	for _, line := range []string{"opened :8080 → localhost:80 over web", "watching what goes through it",
		"→ 78 bytes", "GET / HTTP/1.1", "Host: localhost:8080", "User-Agent: curl/8.5.0", "Accept: */*", "",
		"← 120 bytes", "HTTP/1.1 200 OK", "Content-Type: text/html", "Content-Length: 44", "", "<html><body>Hello from web</body></html>"} {
		_, _ = account.Write([]byte(line + "\n"))
	}
	st.Tunnels[0].Account, st.Tunnels[0].Watching = account, true
	st.Panes = append(st.Panes, app.Pane{ID: "mp", Title: "machine", Kind: app.KindMachine, Machine: "s1",
		Row: tunnelKey("local", ":8080", "localhost:80"), RowAt: 1})
	st.Layout, st.Focus = boxLayout(&app.Box{Pane: "mp"}), "mp"
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

// TestShotsShortcuts writes the shortcut editor at rest, listening,
// asking before it takes a key, and part way through a key flying from
// one row to another.
func TestShotsShortcuts(t *testing.T) {
	dir := os.Getenv("KAKEL_SHOTS")
	if dir == "" {
		t.Skip("KAKEL_SHOTS names no folder to write the screenshots to")
	}
	shot := func(name string) {
		gunimtest.WritePNG(t, filepath.Join(dir, "shortcuts-"+name+".png"), gunimtest.Pixels(t, lastWindow))
	}
	win, _, publish := windowStageOf(t, geom.Sz(1100, 760))
	st := app.State{Panes: []app.Pane{{ID: "p1", Kind: app.KindSettings, Title: "Settings"}}, Layout: boxLayout(&app.Box{Pane: "p1"}), Focus: "p1", SettingsTab: app.SettingsTabShortcuts, SettingsAsked: 1}
	publish(st)
	settle()
	shot("rest")
	// The tab sliding in, from the one before it.
	win.settings.tabs.SetSelected(0, lastUI)
	settle()
	win.settings.tabs.SetSelected(3, lastUI)
	lastWindow.Frame(time.Second / 60)
	lastWindow.Frame(time.Second / 30)
	shot("tab-sliding")
	settle()
	p := win.shortcuts
	p.side.OnActivate(p.groupItems()[2].key, lastUI)
	settle()
	shot("group")
	p.side.OnActivate(groupAll, lastUI)
	settle()
	row := func(id string) *shortcutRow {
		for _, l := range p.lines {
			if l.id == id {
				n, ok := p.list.Row(l.key)
				if !ok {
					t.Fatalf("%s isn't built", id)
				}
				return n.(*shortcutRow)
			}
		}
		t.Fatalf("no line for %s", id)
		return nil
	}
	first := row("conn.terminal")
	lastUI.FocusFirst(first.add)
	settle()
	lastWindow.Input(gi.KeyPress{Key: gi.KeyDown})
	frames(3)
	shot("selection-gliding")
	settle()
	shot("selected")
	second, ok := p.list.Row(p.under.selected)
	if !ok {
		t.Fatal("nothing is lit")
	}
	r := second.(*shortcutRow)
	lastWindow.Input(gi.KeyPress{Key: gi.KeyEnter})
	frames(3)
	shot("listen-opening")
	settle()
	shot("listening")
	lastWindow.Input(gi.KeyPress{Key: gi.KeyLeftControl, Mods: gi.ModControl})
	lastWindow.Input(gi.KeyPress{Key: gi.KeyLeftShift, Mods: gi.ModControl | gi.ModShift})
	settle()
	shot("holding")
	lastWindow.Input(gi.KeyRelease{Key: gi.KeyLeftShift})
	lastWindow.Input(gi.KeyRelease{Key: gi.KeyLeftControl})
	lastWindow.Input(gi.KeyPress{Key: gi.KeyQ, Typed: true})
	settle()
	shot("refused")
	lastWindow.Input(gi.KeyRelease{Key: gi.KeyQ})
	lastWindow.Input(gi.KeyPress{Key: gi.KeyT, Mods: gi.ModControl | gi.ModShift})
	settle()
	shot("asking")
	lastWindow.Input(gi.PointerMove{Pos: geom.Pt(300, 420)})
	settle()
	shot("hover")
	// Replace: as the program would, the file changes and comes back.
	v := first.chips[ui.Chord{Key: input.KeyT, Mods: input.ModCtrl | input.ModShift}]
	if v == nil {
		t.Fatal("no chip to take")
	}
	chord := ui.Chord{Key: input.KeyT, Mods: input.ModCtrl | input.ModShift}
	r.act.OnClick(lastUI)
	st.Shortcuts = []keys.Change{{Chord: chord, Command: r.line.id, Written: chord.String()}}
	st.ShortcutsRead++
	publish(st)
	shot("flying-1")
	frames(4)
	shot("flying-2")
	frames(4)
	shot("flying-3")
	settle()
	shot("replaced")
	_, _, publish = windowStageOf(t, geom.Sz(640, 500))
	publish(st)
	settle()
	shot("narrow")
}

// TestShotsProfileEnded writes a profile's page, and frames of its When it ends menu opening.
func TestShotsProfileEnded(t *testing.T) {
	dir := os.Getenv("KAKEL_SHOTS")
	if dir == "" {
		t.Skip("KAKEL_SHOTS names no folder to write the screenshots to")
	}
	shot := func(name string) {
		gunimtest.WritePNG(t, filepath.Join(dir, name+".png"), gunimtest.Pixels(t, lastWindow))
	}
	win, _, publish := windowStageOf(t, geom.Sz(1300, 860))
	st := shotsState()
	st.Profiles = []settings.Profile{{ID: "profile:fish", Name: "Fish", Command: []string{"/usr/bin/fish", "-l"}, Ended: settings.EndAskFailed}}
	st.Panes = append(st.Panes, app.Pane{ID: "mp", Title: "machine", Kind: app.KindMachine, Machine: "", Row: mpProfile + "profile:fish", RowAt: 1})
	st.Layout, st.Focus = boxLayout(&app.Box{Pane: "mp"}), "mp"
	publish(st)
	settle()
	shot("profile-ended")
	mp := win.machinePanes["mp"]
	var menu *widget.Dropdown
	walkNodes(mp.right, func(n gunim.Node) {
		if d, ok := n.(*widget.Dropdown); ok && d.Label == "When it ends" {
			menu = d
		}
	})
	if menu == nil {
		t.Fatal("no When it ends menu on the profile's page")
	}
	box, ok := lastUI.Bounds(menu)
	if !ok {
		t.Fatal("the When it ends menu is not on screen")
	}
	lastWindow.Input(gi.PointerDown{Pos: box.Center(), Button: gi.ButtonPrimary})
	lastWindow.Input(gi.PointerUp{Pos: box.Center(), Button: gi.ButtonPrimary})
	for i := 1; i <= 3; i++ {
		frames(3)
		shot("profile-ended-open-" + strconv.Itoa(i))
	}
	settle()
	shot("profile-ended-open")
}

func TestShotsWordStops(t *testing.T) {
	dir := os.Getenv("KAKEL_SHOTS")
	if dir == "" {
		t.Skip("KAKEL_SHOTS names no folder to write the screenshots to")
	}
	shot := func(name string) {
		gunimtest.WritePNG(t, filepath.Join(dir, "wordstops-"+name+".png"), gunimtest.Pixels(t, lastWindow))
	}
	win, _, publish := windowStageOf(t, geom.Sz(1100, 1300))
	st := app.State{Panes: []app.Pane{{ID: "p1", Kind: app.KindSettings, Title: "Settings"}}, Layout: boxLayout(&app.Box{Pane: "p1"}), Focus: "p1", SettingsTab: "General", SettingsAsked: 1, WordStops: widget.WordStops.Default()}
	publish(st)
	settle()
	shot("rest")
	f := win.settings.wordStops.field
	lastUI.Focus(f)
	f.SetText("/home/me/my_file.txt", lastUI)
	settle()
	lastWindow.Input(gi.KeyPress{Key: gi.KeyEnd})
	settle()
	lastWindow.Input(gi.KeyPress{Key: gi.KeyLeft, Mods: gi.ModControl})
	frames(2)
	shot("caret-gliding")
	settle()
	shot("caret-stopped")
	st.WordStops = "/"
	publish(st)
	settle()
	shot("saved")
}

// TestShotsRemoteAccess writes the Remote Access dialog off and on,
// Connect to Another kakel, Tunnels with ssh, Settings › Sharing and the
// cards that name another kakel, wide and narrow.
func TestShotsRemoteAccess(t *testing.T) {
	dir := os.Getenv("KAKEL_SHOTS")
	if dir == "" {
		t.Skip("KAKEL_SHOTS names no folder to write the screenshots to")
	}
	off := app.Serving{Port: 2222, Host: "desk", RelayURL: "https://relay.example.com/k/0123456789abcdef", AtStart: settings.ServeAsk,
		Keys: []serve.AllowedKey{{Name: "marcus@laptop", Type: "ssh-ed25519", Fingerprint: "SHA256:0Yx3c8V1Qm5pW2aT7rK9"}}, Allowed: []string{"marcus@laptop"}}
	on := off
	on.On, on.Addr, on.Fingerprint = true, "0.0.0.0:2222", "SHA256:q8mB1cYz7WJf0aK3xN5tR2vL9dPhG4sE6uT1oIyXwZc"
	on.Relayed, on.Reached, on.RelayOn, on.RelayHost, on.Code = true, true, true, "relay.example.com", "7KQ2-M9XD"
	on.Clients = []app.ServedClient{{Name: "marcus@laptop", From: "10.0.0.7:51322"}}
	for _, size := range []geom.Size{geom.Sz(1100, 1000), geom.Sz(480, 900)} {
		shot := func(name string) {
			gunimtest.WritePNG(t, filepath.Join(dir, "remoteaccess-"+strconv.Itoa(int(size.W))+"-"+name+".png"), gunimtest.Pixels(t, lastWindow))
		}
		win, _, publish := windowStageOf(t, size)
		st := app.State{Panes: []app.Pane{{ID: "p1", Kind: app.KindSettings, Title: "Settings"}}, Layout: boxLayout(&app.Box{Pane: "p1"}), Focus: "p1",
			SettingsTab: "Sharing", SettingsAsked: 1, Serving: off}
		publish(st)
		settle()
		shot("settings-off")
		win.servingDialog(win.serving, lastUI)
		settle()
		shot("dialog-off")
		win.dialog.Close(lastUI)
		settle()
		win.connectWindowDialog(lastUI)
		settle()
		shot("connect")
		win.dialog.Close(lastUI)
		settle()
		st.Serving = on
		publish(st)
		settle()
		shot("settings-on")
		win.servingDialog(win.serving, lastUI)
		frames(4)
		shot("dialog-on-opening")
		settle()
		shot("dialog-on")
		win.dialog.Close(lastUI)
		settle()
		win.sshDialog(win.serving, lastUI)
		settle()
		shot("ssh")
		// The cards and rows that name another kakel.
		cards := shotsState()
		cards.Saved = append(cards.Saved, remote.Host{ID: "k1", Name: "office", Address: "office.local", Window: true})
		cards.Serving = on
		publish(cards)
		win.dialog.Close(lastUI)
		settle()
		shot("cards")
	}
}

// TestShotsWindowLink writes the Share Window with Link dialog making a
// link and listing them, the dialog that asks about a guest, the chip,
// the rings and the names, the Machines pane and Settings › Sharing, wide
// and narrow, and frames of the dialog turning and of a name coming.
func TestShotsWindowLink(t *testing.T) {
	dir := os.Getenv("KAKEL_SHOTS")
	if dir == "" {
		t.Skip("KAKEL_SHOTS names no folder to write the screenshots to")
	}
	now := time.Now()
	link := "https://relay.example.com/s/kaestp3m5q7w2x4y6z8abcdefg/#Zm9vYmFyYmF6cXV4Zm9vYmFyYmF6cXV4Zm9vYmFyYg"
	waiting := app.WindowLink{ID: "l1", Link: link, CanType: true, Stands: app.LinkWaiting, Until: now.Add(14*time.Minute + 32*time.Second), Relay: "relay.example.com"}
	asking := app.WindowLink{ID: "l2", CanType: true, Stands: app.LinkAsking, Until: now.Add(9*time.Minute + 41*time.Second), Guest: "Bob", From: "203.0.113.7", About: "Firefox on Linux", Relay: "relay.example.com"}
	in := asking
	in.Stands, in.Until, in.Since, in.In = app.LinkIn, time.Time{}, now.Add(-3*time.Minute), "p1"
	away := app.WindowLink{ID: "l3", Stands: app.LinkAway, Guest: "Maria Alexandra Fernández", From: "2001:db8:85a3::8a2e:370:7334", About: "Safari on macOS", Relay: "relay.example.com", Until: now.Add(28 * time.Minute)}
	over := app.WindowLink{ID: "l4", Stands: app.LinkEnded, Over: "expired", Relay: "relay.example.com"}
	lost := waiting
	lost.ID, lost.Trouble = "l5", "connection refused"
	reaching := app.WindowLink{ID: "l1", CanType: true, Stands: app.LinkReaching, Relay: "relay.example.com"}
	sending := reaching
	sending.Stands = app.LinkSending
	for _, size := range []geom.Size{geom.Sz(1100, 760), geom.Sz(480, 760)} {
		shot := func(name string) {
			gunimtest.WritePNG(t, filepath.Join(dir, "windowlink-"+strconv.Itoa(int(size.W))+"-"+name+".png"), gunimtest.Pixels(t, lastWindow))
		}
		win, sh, publish := windowStageOf(t, size)
		quiet := screen.Hooks{Output: func() {}, Title: func(string) {}, Exit: func() {}, Clipboard: func(string) {}}
		for id, text := range map[string]string{"p1": "anna@desk:~/src$ make test\r\nok   app    4.2s\r\nok   view   8.1s\r\nanna@desk:~/src$ ", "p2": "anna@desk:~$ tail -f log\r\n10:04 up\r\n10:05 ready\r\n"} {
			sh.Set(id, screen.Open(sessiontest.NewPrinted([]byte(text)), vt.DefaultPalette(), quiet))
			t.Cleanup(func() { _ = sh.Get(id).T.Close() })
		}
		st := app.State{Panes: []app.Pane{{ID: "p1", Title: "make test"}, {ID: "p2", Title: "log"}}, Focus: "p1", Marks: look.MarksOf(vt.DefaultPalette()),
			Layout:    boxLayout(&app.Box{ID: "s", Share: 0.5, A: &app.Box{Pane: "p1"}, B: &app.Box{Pane: "p2"}}),
			LinkSetup: app.LinkSetup{Name: "Anna", Relay: "https://relay.example.com/k/0123456789abcdef"}}
		publish(st)
		settle()
		win.linkDialog(lastUI)
		frames(5)
		shot("dialog-opening")
		settle()
		shot("dialog")
		for i, to := range []gi.Key{gi.KeyRight, gi.KeyLeft} {
			lastUI.Focus(win.linkBox.may)
			lastWindow.Input(gi.KeyPress{Key: to})
			frames(6)
			shot("may-" + strconv.Itoa(i+1))
			settle()
		}
		win.linkBox.relay.SetText("", lastUI)
		win.linkBox.make(lastUI)
		settle()
		shot("dialog-wrong")
		win.linkBox.relay.SetText(st.LinkSetup.Relay, lastUI)
		win.linkBox.may.SetSelected(1, lastUI)
		win.linkBox.make(lastUI)
		st.Links, st.LinkSetup.Tries = []app.WindowLink{reaching}, 1
		publish(st)
		for i := 1; i <= 8; i++ {
			frames(2)
			shot("turning-" + strconv.Itoa(i))
		}
		settle()
		shot("reaching")
		st.Links = []app.WindowLink{sending}
		publish(st)
		settle()
		shot("sending")
		st.Links = []app.WindowLink{waiting}
		publish(st)
		for i := 1; i <= 4; i++ {
			frames(3)
			shot("link-coming-" + strconv.Itoa(i))
		}
		settle()
		shot("link")
		st.Links = []app.WindowLink{lost, in, away, over}
		publish(st)
		frames(8)
		shot("list-changing")
		settle()
		shot("list")
		win.dialog.Close(lastUI)
		settle()

		// A guest asks, over the terminals.
		st.Links = []app.WindowLink{asking}
		publish(st)
		frames(6)
		shot("asking-opening")
		frames(30)
		shot("asking")
		for range 3 {
			lastWindow.Frame(time.Second / 2)
		}
		settle()
		shot("asking-ready")
		win.guestAsk.d.Close(lastUI)
		win.answered["l2"] = true
		settle()
		shot("chip-asking")

		// In: the chip, the rings and the name on the tab.
		in.TypedIn, in.TypedAt = "p1", time.Now()
		st.Links = []app.WindowLink{in}
		publish(st)
		for i := 1; i <= 5; i++ {
			frames(2)
			shot("badge-coming-" + strconv.Itoa(i))
		}
		settle()
		shot("in")
		other := in
		other.ID, other.Guest, other.In = "l9", "Maria", "p1"
		st.Links = []app.WindowLink{in, other}
		publish(st)
		frames(5)
		shot("two-in-coming")
		settle()
		shot("two-in")
		in.In, in.TypedIn = "p2", ""
		st.Links = []app.WindowLink{in}
		publish(st)
		frames(6)
		shot("moved-panes")
		settle()
		shot("moved")

		// On a light ground.
		light, ok := themes.Named(themes.Built(), "Paper")
		if !ok {
			t.Fatal("there is no light theme")
		}
		made, err := look.Of(light)
		if err != nil {
			t.Fatal(err)
		}
		lastWindow.RegisterTheme(made.Theme)
		if err := lastWindow.Client().SetTheme(made.Name); err != nil {
			t.Fatal(err)
		}
		win.onStage.Use(made.Content)
		for _, id := range []string{"p1", "p2"} {
			sh.Get(id).SetPalette(made.Palette)
		}
		publish(st)
		settle()
		shot("light")
		lastWindow.RegisterTheme(theme.Make("plain"))
		if err := lastWindow.Client().SetTheme("plain"); err != nil {
			t.Fatal(err)
		}
		win.onStage.Use(widget.Dark())
		for _, id := range []string{"p1", "p2"} {
			sh.Get(id).SetPalette(vt.DefaultPalette())
		}

		// With an agent in it too, two rings and no text under either.
		st.Share = app.Share{Panes: []app.SharedPane{{Pane: "p1"}, {Pane: "p2"}}}
		publish(st)
		settle()
		shot("with-agent")
		st.Share = app.Share{}
		publish(st)

		// The Machines pane lists the guest.
		st = withServers(st)
		st.Panes = append(st.Panes, app.Pane{ID: "ps", Title: "Machines", Kind: app.KindServers})
		st.AllPanes = st.Panes
		st.Layout, st.Focus = boxLayout(&app.Box{Pane: "ps"}), "ps"
		publish(st)
		settle()
		shot("machines")
		st.Panes = append(st.Panes, app.Pane{ID: "p9", Kind: app.KindSettings, Title: "Settings"})
		st.Layout, st.Focus, st.SettingsTab, st.SettingsAsked = boxLayout(&app.Box{Pane: "p9"}), "p9", "Sharing", 1
		publish(st)
		settle()
		shot("settings")
		st.Links, st.LinkSetup.NoClient = nil, true
		publish(st)
		win.linkDialog(lastUI)
		settle()
		shot("no-client")
	}
}

// TestShotsWindowLinkFiles writes a share's files: the dialog offering a
// folder, and a card choosing one and then what the guest may do with it,
// frames of each change.
func TestShotsWindowLinkFiles(t *testing.T) {
	dir := os.Getenv("KAKEL_SHOTS")
	if dir == "" {
		t.Skip("KAKEL_SHOTS names no folder to write the screenshots to")
	}
	shot := func(name string) {
		gunimtest.WritePNG(t, filepath.Join(dir, "linkfiles-"+name+".png"), gunimtest.Pixels(t, lastWindow))
	}
	in := app.WindowLink{ID: "l1", CanType: true, Stands: app.LinkIn, Since: time.Now().Add(-3 * time.Minute), In: "p1",
		Guest: "Bob", From: "203.0.113.7", About: "Firefox on Linux", Relay: "relay.example.com"}
	win, st, publish := linkStage(t)
	st.LinkSetup.Folders = map[int]string{win.winID: "/home/anna/src/Reports"}
	publish(st)
	settle()
	win.linkDialog(lastUI)
	settle()
	shot("dialog")
	lastUI.Focus(win.linkBox.files.may)
	lastWindow.Input(gi.KeyPress{Key: gi.KeyRight})
	frames(4)
	shot("dialog-look-moving")
	settle()
	shot("dialog-look")
	win.dialog.Close(lastUI)
	settle()

	st.Links = []app.WindowLink{in}
	publish(st)
	settle()
	win.linkDialog(lastUI)
	settle()
	shot("card-no-folder")
	in.Folder = "/home/anna/src/Reports"
	st.Links = []app.WindowLink{in}
	publish(st)
	for i := 1; i <= 4; i++ {
		frames(3)
		shot("card-folder-coming-" + strconv.Itoa(i))
	}
	settle()
	shot("card-folder")
	in.Files = guest.FilesChange
	st.Links = []app.WindowLink{in}
	publish(st)
	frames(4)
	shot("card-change-moving")
	settle()
	shot("card-change")
}
