package view

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/icon"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/app"
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
// found and those made here, the default first, none of a server's. The
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
	want := []string{"This computer", "Profiles", "PowerShell", "Command Prompt", "Nushell", "Add Profile", "Files", "Home", "Add Folder"}
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
	if got, want := titlesOf(mp), []string{"prod", "Profiles", "htop", "Login Shell", "Add Profile", "Files", "Home", "Add Folder"}; !slices.Equal(got, want) {
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

// A kakel window's pane has its settings and its files, no profiles; a new
// server's has its settings alone, says what is typed, and is saved
// from the pane it was typed in.
func TestANewServersPaneSavesIntoItself(t *testing.T) {
	_, mp, _ := machinePaneOf(t, app.State{Saved: []remote.Host{{ID: "w1", Name: "desk", Address: "desk.example", Window: true}}}, "w1", false)
	if got := titlesOf(mp); !slices.Equal(got, []string{"desk", "Files", "Home", "Add Folder"}) {
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
	if at < 0 || !slices.Equal(win.palette.Items[at].Chips, []string{"Terminal", "htop", "tmux", "Files", "Connect", "Settings", "Remove"}) {
		t.Fatalf("prod's line is %+v", win.palette.Items[max(at, 0)])
	}
	line := slices.IndexFunc(win.palette.Items, func(it widget.PaletteItem) bool { return it.Title == "tmux" })
	if line < 0 || win.palette.Items[line].Detail != "prod" {
		t.Fatal("the palette has no line for tmux on prod")
	}
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	win.palette.OnChip(at, 2, lastUI)
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
