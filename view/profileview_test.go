package view

import (
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/icon"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/logs"
	"github.com/marrasen/kakel/look"
	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/settings"
)

// machinePaneOf opens the pane of machine m, or a draft one, in a
// window with st, and is it.
func machinePaneOf(t *testing.T, st app.State, m machines.ID, draft bool) (*Window, *machinePane, func(app.State)) {
	t.Helper()
	win, _, publish := windowStage(t)
	st.Panes = append(st.Panes, app.Pane{ID: "mp", Title: "machine", Kind: app.KindMachine, Machine: m, Draft: draft})
	st.Stage, st.Focus = &app.Box{Pane: "mp"}, "mp"
	publish(st)
	mp := win.machinePanes["mp"]
	if mp == nil {
		t.Fatal("no machine pane")
	}
	again := func(next app.State) {
		next.Panes, next.Stage, next.Focus = st.Panes, st.Stage, st.Focus
		publish(next)
	}
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	return win, mp, again
}

// titlesOf are the titles of the machine pane's rows.
func titlesOf(mp *machinePane) []string {
	var out []string
	for _, it := range mp.items {
		out = append(out, it.title)
	}
	return out
}

// rowOf is the machine pane's sidebar row with key k.
func rowOf(t *testing.T, mp *machinePane, k string) *mpRow {
	t.Helper()
	r, ok := widget.RowOf[*mpRow](mp.list, widget.Key(k))
	if !ok {
		t.Fatalf("no row %q among %q", k, titlesOf(mp))
	}
	return r
}

// This computer's pane lists the machine, then its profiles: the shells
// found and those made here, as found and made, none of a server's. The
// star makes one the default, the eye hides or shows one found, and a
// hidden one stays listed, faint.
func TestThisComputersPaneListsItsProfiles(t *testing.T) {
	st := app.State{
		FoundShells:  []app.ShellChoice{{ID: "cmd", Title: "Command Prompt"}, {ID: "pwsh", Title: "PowerShell"}},
		DefaultShell: "pwsh",
		Profiles: []settings.Profile{
			{ID: "profile:nu", Name: "Nushell", Command: []string{"nu"}},
			{ID: "profile:tmux", HostID: "s1", Name: "tmux", Command: []string{"tmux"}},
		},
		Found: []settings.FoundProfile{{ID: "cmd", Hidden: true}},
		Saved: []remote.Host{{ID: "s1", Name: "prod", Address: "prod.example"}},
	}
	_, mp, _ := machinePaneOf(t, st, "", false)
	want := []string{"This computer", "Profiles", "Command Prompt", "PowerShell", "Nushell", "Add Profile", "Files", "Home", "Add Folder"}
	if got := titlesOf(mp); !slices.Equal(got, want) {
		t.Fatalf("the rows are %q, want %q", got, want)
	}
	cmd := rowOf(t, mp, mpProfile+"cmd")
	if cmd.title.Color.Key() != look.Faint.Key() || cmd.eye.Icon != icon.EyeOff {
		t.Fatal("hidden, Command Prompt's row is not faint with its eye shut")
	}
	if in, ok := cmd.eye.OnClick(lastUI).(app.HideFound); !ok || in.ID != "cmd" || !in.Show {
		t.Fatalf("the eye on hidden Command Prompt sent %#v", in)
	}
	if in := rowOf(t, mp, mpProfile+"profile:nu").star.OnClick(lastUI); in != (app.SetDefaultProfile{ID: "profile:nu"}) {
		t.Fatalf("Nushell's star sends %#v", in)
	}
	if r := rowOf(t, mp, mpProfile+"profile:nu"); slices.Contains(r.inner.Children(), gunim.Node(r.eye)) {
		t.Fatal("a profile made here has an eye")
	}
}

// A profile picked shows its settings: saved untouched, it is the
// profile it was, its own shell tick left unset; with the tick changed,
// that is kept.
func TestAProfileSavedFromItsPaneIsAsItWas(t *testing.T) {
	nu := settings.Profile{ID: "profile:nu", Name: "Nushell", Command: []string{"sh", "-l"}, Env: []string{"FOO=bar"}}
	_, mp, _ := machinePaneOf(t, app.State{Profiles: []settings.Profile{nu}, ShellSetup: true}, "", false)
	mp.pick(mpProfile+nu.ID, lastUI)
	save, teach := findButton(t, mp.right, "Save"), findCheckbox(t, mp.right)
	in, ok := save.OnClick(lastUI).(app.SaveProfile)
	if !ok || in.Profile.ID != nu.ID || in.Profile.Name != nu.Name || !slices.Equal(in.Profile.Command, nu.Command) ||
		!slices.Equal(in.Profile.Env, nu.Env) || in.Profile.Setup != nil {
		t.Fatalf("saved untouched, the profile is %+v", in.Profile)
	}
	teach.SetChecked(false, lastUI)
	if in, _ := save.OnClick(lastUI).(app.SaveProfile); in.Profile.Setup == nil || *in.Profile.Setup {
		t.Fatalf("with its tick off, the profile is %+v", in.Profile)
	}
}

// A server's pane lists its own shell first, then its profiles; one
// added there is the server's, its folder a path there.
func TestAProfileIsAddedOnItsServer(t *testing.T) {
	st := app.State{Saved: []remote.Host{{ID: "s1", Name: "prod", Address: "prod.example"}},
		Profiles:     []settings.Profile{{ID: "profile:htop", HostID: "s1", Name: "htop", Command: []string{"htop"}}},
		ServerShells: map[string]string{"s1": "profile:htop"}}
	_, mp, _ := machinePaneOf(t, st, "s1", false)
	if got, want := titlesOf(mp), []string{"prod", "Connection log", "Profiles", "Login Shell", "htop", "Add Profile", "Files", "Home", "Add Folder", "Tunnels", "SOCKS proxies"}; !slices.Equal(got, want) {
		t.Fatalf("the rows are %q, want %q", got, want)
	}
	mp.pick(mpAdd, lastUI)
	fields := fieldsIn(mp.right)
	if len(fields) < 3 {
		t.Fatalf("adding, the fields are %d", len(fields))
	}
	name, line, dir := fields[0], fields[1], fields[2]
	name.SetText("tmux", nil)
	line.SetText("kakel-test-not-here new -A -s main", nil)
	dir.SetText("src", nil)
	save := findButton(t, mp.right, "Save")
	if _, ok := save.OnClick(lastUI).(app.SaveProfile); ok {
		t.Fatal("a folder on a server not from / or ~ was taken")
	}
	dir.SetText("~/src", nil)
	in, ok := save.OnClick(lastUI).(app.SaveProfile)
	if !ok || in.Profile.HostID != "s1" || in.Profile.Folder != "~/src" || in.Profile.Command[0] != "kakel-test-not-here" {
		t.Fatalf("the profile saved is %+v", in.Profile)
	}
}

// A server whose own shell is hidden stars its first profile, which is
// what its New Terminal runs, and a hidden row's star takes no click.
func TestAHiddenLoginShellPassesItsStar(t *testing.T) {
	st := app.State{Saved: []remote.Host{{ID: "s1", Name: "prod", Address: "prod.example"}},
		Profiles: []settings.Profile{{ID: "profile:htop", HostID: "s1", Name: "htop", Command: []string{"htop"}}},
		Found:    []settings.FoundProfile{{HostID: "s1", ID: app.LoginShell, Hidden: true}}}
	_, mp, _ := machinePaneOf(t, st, "s1", false)
	if !rowOf(t, mp, mpProfile+"profile:htop").it.profile.def {
		t.Fatal("its own shell hidden, the server's first profile has no star")
	}
	if login := rowOf(t, mp, mpProfile+app.LoginShell); login.it.profile.def || !login.star.Disabled {
		t.Fatalf("the hidden login shell's star is %+v", login.it.profile)
	}
}

// The Files section lists the home folder and the drives, which kakel
// finds, a hidden one faint with its eye shut, then the favourites by
// their folder's name; a favourite is saved, renamed, from its row.
func TestThisComputersFilesAreListedUnderItsProfiles(t *testing.T) {
	dir := t.TempDir()
	st := app.State{
		Drives:     []app.FoundDrive{{ID: "drive:/", Name: "Computer", Path: "/"}},
		Found:      []settings.FoundProfile{{ID: "drive:/", Hidden: true}},
		Favourites: []app.Favourite{{Path: dir}, {Machine: "s1", Path: "/srv"}},
	}
	_, mp, _ := machinePaneOf(t, st, "", false)
	got := titlesOf(mp)
	want := []string{"Files", "Home", "Computer", filepath.Base(dir), "Add Folder"}
	if len(got) < len(want) || !slices.Equal(got[len(got)-len(want):], want) {
		t.Fatalf("the rows are %q, want them to end %q", got, want)
	}
	drive := rowOf(t, mp, mpFolder+"found:drive:/")
	if drive.eye.Icon != icon.EyeOff || drive.title.Color.Key() != look.Faint.Key() {
		t.Fatal("hidden, the drive's row is not faint with its eye shut")
	}
	if in, ok := drive.eye.OnClick(lastUI).(app.HideFound); !ok || in.ID != "drive:/" || !in.Show {
		t.Fatalf("the hidden drive's eye sent %#v", in)
	}
	mp.pick(mpFolder+"fav:"+dir, lastUI)
	fields := fieldsIn(mp.right)
	save := findButton(t, mp.right, "Save")
	if !save.Disabled {
		t.Fatal("untouched, the favourite can be saved")
	}
	fields[0].SetText("Work", nil)
	fields[0].OnChange("Work", lastUI)
	in, ok := save.OnClick(lastUI).(app.SaveFavourite)
	if !ok || in.Was != dir || in.Path != dir || in.Name != "Work" || in.Machine != "" {
		t.Fatalf("renamed, the favourite saved is %#v", in)
	}
}

// A folder added is picked once it is saved, so its form is the saved
// one, not the one it was typed in.
func TestAFolderAddedIsPickedOnceSaved(t *testing.T) {
	dir := t.TempDir()
	_, mp, again := machinePaneOf(t, app.State{}, "", false)
	mp.pick(mpAddDir, lastUI)
	fields := fieldsIn(mp.right)
	fields[1].SetText(dir, nil)
	in, ok := findButton(t, mp.right, "Save").OnClick(lastUI).(app.SaveFavourite)
	if !ok || in.Path != dir || in.Was != "" {
		t.Fatalf("Save sent %#v", in)
	}
	again(app.State{Favourites: []app.Favourite{{Path: dir}}})
	if mp.sel != mpFolder+"fav:"+dir {
		t.Fatalf("saved, the row picked is %q", mp.sel)
	}
}

// A server's pane lists what is kept for it, each kind under its
// heading: the commands run on it, the copies to or from it, and the
// tunnels through it, and none of another machine's. Each runs again
// from its row.
func TestAServersKeptThingsAreListedInItsPane(t *testing.T) {
	cmd := settings.SavedCommand{Line: "make test", Dir: "/src", Host: "prod", HostID: "s1"}
	cp := settings.SavedCopy{To: "prod", ToID: "s1", At: "/tmp", Into: "/srv", Names: []string{"site.zip"}}
	tun := settings.SavedTunnel{Host: "prod", HostID: "s1", Kind: "local", Listen: "localhost:5432", Target: "db:5432"}
	st := app.State{Saved: []remote.Host{{ID: "s1", Name: "prod", Address: "prod.example"}},
		SavedCommands: []settings.SavedCommand{cmd, {Line: "ls"}, {Line: "uptime", Host: "prod\x00far", HostID: "s1"}},
		SavedCopies:   []settings.SavedCopy{cp, {From: "prod", At: "/a", Into: "/b", Names: []string{"old.txt"}}},
		SavedTunnels:  []settings.SavedTunnel{tun}}
	_, mp, _ := machinePaneOf(t, st, "s1", false)
	got := titlesOf(mp)
	// A command beyond prod, kept under prod's ID through it, is not
	// prod's; a copy kept before IDs is, by its name.
	want := []string{"Tunnels", "localhost:5432 → db:5432", "SOCKS proxies", "Commands", "make test", "Copies", "Copy site.zip", "Copy old.txt"}
	if len(got) < len(want) || !slices.Equal(got[len(got)-len(want):], want) {
		t.Fatalf("the rows are %q, want them to end %q", got, want)
	}
	for k, wantIn := range map[string]gunim.Intent{
		mpSaved + fmt.Sprintf("cmd:%#v", cmd):  app.RunSavedCommand{Saved: cmd},
		mpTunnelKept + fmt.Sprintf("%#v", tun): app.OpenSavedTunnel{Saved: tun},
	} {
		mp.pick(k, lastUI)
		label := "Run"
		if strings.HasPrefix(k, mpTunnelKept) {
			label = "Open"
		}
		if in := findButton(t, mp.right, label).OnClick(lastUI); !reflect.DeepEqual(in, wantIn) {
			t.Fatalf("%s sent %#v, want %#v", label, in, wantIn)
		}
	}
	mp.pick(mpSaved+fmt.Sprintf("copy:%#v", cp), lastUI)
	if in, ok := findButton(t, mp.right, "Run").OnClick(lastUI).(app.RunSavedCopy); !ok || in.Saved.Into != "/srv" {
		t.Fatalf("Run on the copy sent %#v", in)
	}
}

// A kakel window's pane has its settings and its files, no profiles; a new
// server's has its settings alone, says what is typed, and is saved
// from the pane it was typed in.
func TestANewServersPaneSavesIntoItself(t *testing.T) {
	_, mp, _ := machinePaneOf(t, app.State{Saved: []remote.Host{{ID: "w1", Name: "desk", Address: "desk.example", Window: true}}}, "w1", false)
	if got := titlesOf(mp); !slices.Equal(got, []string{"desk", "Connection log", "Files", "Home", "Add Folder", "Tunnels", "SOCKS proxies"}) {
		t.Fatalf("a window's rows are %q", got)
	}
	_, mp, _ = machinePaneOf(t, app.State{}, "", true)
	if got := titlesOf(mp); !slices.Equal(got, []string{"New server"}) {
		t.Fatalf("a new server's rows are %q", got)
	}
	fields := fieldsIn(mp.right)
	fields[0].SetText("box", nil)
	fields[0].OnChange("box", lastUI)
	if in, ok := nextIntent(t).(app.DraftTyped); !ok || in.Pane != "mp" || !in.Typed {
		t.Fatalf("typing a name sent %#v", in)
	}
	fields[1].SetText("box.example", nil)
	in, ok := findButton(t, mp.right, "Save").OnClick(lastUI).(app.SaveServer)
	if !ok || in.Pane != "mp" || in.Host.Name != "box" || in.Host.Address != "box.example" || in.Connect {
		t.Fatalf("Save sent %#v", in)
	}
	if in, _ := findButton(t, mp.right, "Save and Connect").OnClick(lastUI).(app.SaveServer); !in.Connect {
		t.Fatalf("Save and Connect sent %#v", in)
	}
}

// findButton is the button labelled label under n.
func findButton(t *testing.T, n gunim.Node, label string) *widget.Button {
	t.Helper()
	var found *widget.Button
	walkNodes(n, func(n gunim.Node) {
		if b, ok := n.(*widget.Button); ok && b.Label == label && found == nil {
			found = b
		}
	})
	if found == nil {
		t.Fatalf("no button %q", label)
	}
	return found
}

// findCheckbox is the first tick box under n.
func findCheckbox(t *testing.T, n gunim.Node) *widget.Checkbox {
	t.Helper()
	var found *widget.Checkbox
	walkNodes(n, func(n gunim.Node) {
		if c, ok := n.(*widget.Checkbox); ok && found == nil {
			found = c
		}
	})
	if found == nil {
		t.Fatal("no tick box")
	}
	return found
}

// fieldsIn are the text fields under n, in order.
func fieldsIn(n gunim.Node) []*widget.TextField {
	var out []*widget.TextField
	walkNodes(n, func(n gunim.Node) {
		if f, ok := n.(*widget.TextField); ok {
			out = append(out, f)
		}
	})
	return out
}

// walkNodes calls do with n and every node under it.
func walkNodes(n gunim.Node, do func(gunim.Node)) {
	if n == nil {
		return
	}
	do(n)
	if c, ok := n.(gunim.Composite); ok {
		for _, k := range c.Children() {
			walkNodes(k, do)
		}
	}
}

// A server's profiles are chips on its palette line after its
// terminal, the one it runs first, and lines of their own; either
// opens the profile.
func TestAServersProfilesOpenFromThePalette(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(app.State{Saved: []remote.Host{{ID: "s1", Name: "prod", Address: "prod.example"}},
		Machines: []machines.Info{{ID: "s1", Name: "prod"}},
		Profiles: []settings.Profile{
			{ID: "profile:tmux", HostID: "s1", Name: "tmux", Command: []string{"tmux"}},
			{ID: "profile:htop", HostID: "s1", Name: "htop", Command: []string{"htop"}},
		},
		ServerShells: map[string]string{"s1": "profile:htop"}})
	at := slices.Index(win.paletteIDs, "machine:s1")
	if at < 0 || !slices.Equal(win.palette.Items[at].Chips, []string{"Terminal", "tmux", "htop", "Files", "Connect", "Settings", "Remove"}) {
		t.Fatalf("prod's line is %+v", win.palette.Items[max(at, 0)])
	}
	line := slices.IndexFunc(win.palette.Items, func(it widget.PaletteItem) bool { return it.Title == "tmux" })
	if line < 0 || win.palette.Items[line].Detail != "prod" {
		t.Fatal("the palette has no line for tmux on prod")
	}
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	win.palette.OnChip(at, 1, lastUI)
	if in, ok := nextIntent(t).(app.OpenShellNamed); !ok || in.ID != "profile:tmux" {
		t.Fatalf("prod's tmux chip sent %#v", in)
	}
	if !win.runItem(win.paletteIDs[line], lastUI) {
		t.Fatalf("tmux's line names %q, which runs nothing", win.paletteIDs[line])
	}
	if in, ok := nextIntent(t).(app.OpenShellNamed); !ok || in.ID != "profile:tmux" {
		t.Fatalf("tmux's line sent %#v", in)
	}
}

// A server's pane lists the tunnels saved for it, by its ID or, saved
// before servers had IDs, by its name; another server's are not there.
// One open is listed once, as open, saved or not.
func TestAServersTunnelsAreListedInItsPane(t *testing.T) {
	byName := settings.SavedTunnel{Host: "prod", Kind: "remote", Listen: ":8080", Target: "127.0.0.1:80"}
	open := settings.SavedTunnel{Host: "prod", HostID: "s1", Kind: "local", Listen: ":9000", Target: "db:5432"}
	st := app.State{Saved: []remote.Host{{ID: "s1", Name: "prod", Address: "prod.example"}},
		Machines: []machines.Info{{ID: "s1", Name: "prod"}},
		SavedTunnels: []settings.SavedTunnel{byName, open,
			{Host: "other", Kind: "local", Listen: ":9001", Target: "db:5432"},
			{Host: "prod", HostID: "s1", Kind: "socks", Listen: ":1080"}},
		Tunnels: []app.Tunnel{{ID: "t1", Machine: "s1", Live: true, As: open}}}
	_, mp, _ := machinePaneOf(t, st, "s1", false)
	got := titlesOf(mp)
	want := []string{"Tunnels", ":9000 → db:5432", ":8080 → 127.0.0.1:80", "SOCKS proxies", ":1080"}
	if !slices.Equal(got[len(got)-len(want):], want) {
		t.Fatalf("the rows are %q, want them to end %q", got, want)
	}
	it, ok := mp.item(mpTunnelOpen + "t1")
	if !ok || it.tunnel.saved == nil || !it.tunnel.live() {
		t.Fatalf("the open tunnel's row is %+v", it)
	}
}

// The + under Tunnels shows the fields, and Open opens the tunnel over
// the machine, saved unless the tick is taken off.
func TestATunnelIsAddedFromThePlus(t *testing.T) {
	st := app.State{Saved: []remote.Host{{ID: "s1", Name: "prod", Address: "prod.example"}},
		Machines: []machines.Info{{ID: "s1", Name: "prod"}}, Connected: []machines.ID{"s1"}}
	_, mp, _ := machinePaneOf(t, st, "s1", false)
	mp.pick(mpAddTunnel, lastUI)
	fields := fieldsIn(mp.right)
	fields[0].SetText("8080", lastUI)
	fields[1].SetText("localhost:80", lastUI)
	in := findButton(t, mp.right, "Open").OnClick(lastUI)
	want := app.OpenTunnel{Machine: "s1", Tunnel: remote.Tunnel{Kind: remote.LocalForward, Listen: ":8080", Target: "localhost:80"}, Keep: true}
	if !reflect.DeepEqual(in, want) {
		t.Fatalf("Open sent %#v, want %#v", in, want)
	}
}

// The log's row counts the problems in it not looked at yet; looking
// clears the count.
func TestTheLogsRowCountsProblemsNotSeen(t *testing.T) {
	l := logs.New(0, nil)
	_, _ = l.Write([]byte("09:00:00  \x1b[31mcould not connect\x1b[0m\n"))
	st := app.State{Saved: []remote.Host{{ID: "s1", Name: "prod", Address: "prod.example"}},
		Machines: []machines.Info{{ID: "s1", Name: "prod"}}, Logs: map[machines.ID]*logs.Lines{"s1": l}}
	_, mp, again := machinePaneOf(t, st, "s1", false)
	if b := rowOf(t, mp, mpLog).badge; b.Hidden() || b.Text != "1 problem" {
		t.Fatalf("the log's row says %q, hidden %v", b.Text, b.Hidden())
	}
	// Shown to its newest line, the log is read.
	mp.pick(mpLog, lastUI)
	settle()
	again(st)
	if b := rowOf(t, mp, mpLog).badge; !b.Hidden() {
		t.Fatalf("looked at, the log's row still says %q", b.Text)
	}
}

// A tunnel's page says what it is doing in place: traffic changing its
// note does not build the page again.
func TestATunnelsPageStaysAsItsTrafficChanges(t *testing.T) {
	as := settings.SavedTunnel{Host: "prod", HostID: "s1", Kind: "local", Listen: ":9000", Target: "db:5432"}
	st := app.State{Saved: []remote.Host{{ID: "s1", Name: "prod", Address: "prod.example"}},
		Machines: []machines.Info{{ID: "s1", Name: "prod"}}, Connected: []machines.ID{"s1"},
		Tunnels: []app.Tunnel{{ID: "t1", Machine: "s1", Live: true, As: as, Note: "1 stream"}}}
	_, mp, again := machinePaneOf(t, st, "s1", false)
	mp.pick(mpTunnelOpen+"t1", lastUI)
	page := mp.right.Page()
	st.Tunnels = []app.Tunnel{{ID: "t1", Machine: "s1", Live: true, As: as, Note: "2 streams, 4 KB"}}
	again(st)
	if mp.right.Page() != page {
		t.Fatal("the tunnel's page was built again as its traffic changed")
	}
	if mp.tunnelNow.Text != "Open · 2 streams, 4 KB" {
		t.Fatalf("the page says it is %q", mp.tunnelNow.Text)
	}
}

// A tunnel's page built again, as an open tunnel taken off the saved
// list is, fades in over the one it replaces: each says what the
// tunnel is doing on a line of its own, as a line can be on one page
// only.
func TestAnOpenTunnelTakenOffTheListKeepsBothPagesWhole(t *testing.T) {
	as := settings.SavedTunnel{Host: "prod", HostID: "s1", Kind: "local", Listen: ":9000", Target: "db:5432"}
	st := app.State{Saved: []remote.Host{{ID: "s1", Name: "prod", Address: "prod.example"}},
		Machines: []machines.Info{{ID: "s1", Name: "prod"}}, Connected: []machines.ID{"s1"},
		Tunnels:      []app.Tunnel{{ID: "t1", Machine: "s1", Live: true, As: as, Note: "1 stream"}},
		SavedTunnels: []settings.SavedTunnel{as}}
	_, mp, again := machinePaneOf(t, st, "s1", false)
	mp.pick(mpTunnelOpen+"t1", lastUI)
	// Shown in full, so it has a fade to go out with.
	frames := func() {
		for range 60 {
			lastWindow.Frame(time.Second / 60)
		}
	}
	frames()
	page := mp.right.Page()
	st.SavedTunnels = nil
	again(st)
	if mp.right.Page() == page {
		t.Fatal("taken off the list, the tunnel's page was not built again")
	}
	// Drawn through the fade, with both pages laid out.
	frames()
	if mp.tunnelNow.Text != "Open · 1 stream" {
		t.Fatalf("the new page says it is %q", mp.tunnelNow.Text)
	}
}

// Beyond a kakel window that is connected, a machine takes tunnels:
// the + form's Open is there to press.
func TestAMachineBeyondAWindowTakesTunnels(t *testing.T) {
	far := machines.FarID("desk", "k1")
	st := app.State{Windows: []app.RemoteWindow{{Name: "desk"}},
		Machines: []machines.Info{{ID: "desk", Name: "desk"}, {ID: far, Name: "db"}}}
	_, mp, _ := machinePaneOf(t, st, far, false)
	mp.pick(mpAddTunnel, lastUI)
	fields := fieldsIn(mp.right)
	for i, s := range []string{"8080", "localhost:80"} {
		// As typed: each change checks the fields again.
		fields[i].SetText(s, lastUI)
		fields[i].OnChange(s, lastUI)
	}
	if findButton(t, mp.right, "Open").Disabled {
		t.Fatalf("beyond a connected window, Open is greyed out; card %+v, problem in %T", mp.card, mp.right.Page())
	}
}

// A profile's row moves among the profiles, by Alt and an arrow as by a
// drag, and the order goes to the program; the star stays where it is.
func TestAProfileIsMovedAmongTheProfiles(t *testing.T) {
	st := app.State{Saved: []remote.Host{{ID: "s1", Name: "prod", Address: "prod.example"}},
		Profiles: []settings.Profile{
			{ID: "profile:tmux", HostID: "s1", Name: "tmux", Command: []string{"tmux"}},
			{ID: "profile:htop", HostID: "s1", Name: "htop", Command: []string{"htop"}},
		},
		ServerShells: map[string]string{"s1": "profile:htop"}}
	_, mp, _ := machinePaneOf(t, st, "s1", false)
	if got := titlesOf(mp)[3:6]; !slices.Equal(got, []string{"Login Shell", "tmux", "htop"}) {
		t.Fatalf("the profiles stand as %q, the default not first", got)
	}
	lastUI.Focus(mp.list)
	frames(1)
	box, _ := lastUI.Bounds(rowOf(t, mp, mpProfile+"profile:tmux"))
	lastWindow.Input(gi.PointerDown{Pos: box.Center(), Button: gi.ButtonPrimary})
	lastWindow.Input(gi.PointerUp{Pos: box.Center(), Button: gi.ButtonPrimary})
	frames(2)
	drainIntents()
	lastWindow.Input(gi.KeyPress{Key: gi.KeyDown, Mods: gi.ModAlt})
	frames(1)
	in, ok := nextIntent(t).(app.ReorderProfiles)
	if !ok || in.Machine != "s1" || !slices.Equal(in.IDs, []string{app.LoginShell, "profile:htop", "profile:tmux"}) {
		t.Fatalf("Alt+Down on tmux sent %#v", in)
	}
	// Past the last profile, it goes no further: Add Profile stays put.
	lastWindow.Input(gi.KeyPress{Key: gi.KeyDown, Mods: gi.ModAlt})
	frames(1)
	if len(lastWindow.Client().Intents()) > 0 {
		t.Fatalf("Alt+Down past the last profile sent %#v", nextIntent(t))
	}
}

// drainIntents empties what the window has sent so far.
func drainIntents() {
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
}
