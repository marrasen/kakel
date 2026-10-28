package app

import (
	"slices"
	"testing"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/filemanager"

	"github.com/marrasen/kakel/settings"
	shellfind "github.com/marrasen/kakel/shells"
)

// foundApp is an app whose shells here are Command Prompt and
// PowerShell, found.
func foundApp(t *testing.T) *app {
	t.Helper()
	was := findShells
	findShells = func() ([]shellfind.Shell, error) {
		return []shellfind.Shell{
			{ID: "cmd", Title: "Command Prompt", Path: `C:\Windows\System32\cmd.exe`},
			{ID: "powershell", Title: "Windows PowerShell", Path: `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`},
		}, nil
	}
	t.Cleanup(func() { findShells = was })
	a := fontApp(t)
	a.scanShells()
	waitFor(t, a, "the shells", func() bool { return len(a.st.Shells) == 2 })
	return a
}

// shellTitles are the IDs and names of the shells offered.
func shellTitles(a *app) []string {
	var out []string
	for _, s := range a.st.Shells {
		out = append(out, s.ID+" "+s.Title)
	}
	return out
}

// A shell found here can be renamed, and hidden: then it is offered
// nowhere, though it is still known for a pane that runs it, and a new
// terminal that started it starts the default shell. Shown again, it
// is offered again, by the name it was given.
func TestAFoundShellCanBeRenamedAndHidden(t *testing.T) {
	a := foundApp(t)
	a.handle(SaveFound{Found: settings.FoundProfile{ID: "powershell", Name: "PS 5"}})
	a.handle(PickShell{ID: "powershell"})
	if got, want := shellTitles(a), []string{"cmd Command Prompt", "powershell PS 5"}; !slices.Equal(got, want) {
		t.Fatalf("renamed, the shells offered are %q, want %q", got, want)
	}
	if got := a.st.FoundShells; len(got) != 2 || got[1].Title != "Windows PowerShell" {
		t.Fatalf("renamed, the shells found are %+v, by the names found", got)
	}
	a.handle(HideFound{ID: "powershell"})
	if got, want := shellTitles(a), []string{"cmd Command Prompt"}; !slices.Equal(got, want) {
		t.Fatalf("hidden, the shells offered are %q, want %q", got, want)
	}
	if _, ok := shellfind.Lookup(a.found, "powershell"); !ok {
		t.Fatal("hidden, the shell is no longer known for the panes that run it")
	}
	if a.st.ChosenShell != "" || a.localShell() != nil {
		t.Fatalf("the kept shell hidden, new terminals start %q, kept %q", a.localShell(), a.st.ChosenShell)
	}
	ms := a.launchMachines()
	for _, x := range ms[0].Actions {
		if x.ID == "shell:powershell" {
			t.Fatal("hidden, the shell is still in the launcher")
		}
	}
	a.handle(HideFound{ID: "powershell", Show: true})
	if got, want := shellTitles(a), []string{"cmd Command Prompt", "powershell PS 5"}; !slices.Equal(got, want) {
		t.Fatalf("shown again, the shells offered are %q, want %q", got, want)
	}
	if len(a.st.Notices) != 0 {
		t.Fatalf("hiding and showing a shell said %+v", a.st.Notices)
	}
}

// The user's own shell hidden, a new terminal starts the first shell
// shown in its place.
func TestTheUsersOwnShellHiddenStartsTheFirstShown(t *testing.T) {
	a := foundApp(t)
	a.ownShell = "cmd"
	a.handle(HideFound{ID: "cmd"})
	if got := a.defaultShell(); got != "powershell" {
		t.Fatalf("the user's own shell hidden, the default is %q", got)
	}
	if got := a.localShell(); len(got) == 0 || got[0] != `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe` {
		t.Fatalf("the user's own shell hidden, a new terminal starts %q", got)
	}
}

// A shell found here starts in the folder, and with the variables,
// the user gave it, as a profile does.
func TestAFoundShellHasTheFolderAndVariablesGivenIt(t *testing.T) {
	a := foundApp(t)
	dir := t.TempDir()
	a.handle(SaveFound{Found: settings.FoundProfile{ID: "cmd", Folder: dir, Env: []string{"FOO=bar"}}})
	folder, env := a.foundRun(a.shellCommand("cmd"))
	if folder != dir || !slices.Equal(env, []string{"FOO=bar"}) {
		t.Fatalf("Command Prompt starts in %q with %q", folder, env)
	}
	if folder, env := a.foundRun(a.shellCommand("powershell")); folder != "" || env != nil {
		t.Fatalf("PowerShell, unchanged, starts in %q with %q", folder, env)
	}
	// Changed back to how it was found, nothing is kept.
	a.handle(SaveFound{Found: settings.FoundProfile{ID: "cmd"}})
	if len(a.st.Found) != 0 {
		t.Fatalf("changed back, kept are %+v", a.st.Found)
	}
}

// A server whose own shell and home are hidden, with no profile or
// favourite shown, is used only to reach others: the launcher, the
// tray, the file manager's places leave it out. While one of its
// profiles is shown, it offers that alone, and New Terminal runs it.
func TestAServerWithEverythingHiddenIsLeftOut(t *testing.T) {
	a, _ := serverWithProfiles(t)
	a.handle(HideFound{HostID: "srv", ID: LoginShell})
	a.handle(HideFound{HostID: "srv", ID: FoundHome})
	if got := a.farRun("srv"); got != "profile:tmux" {
		t.Fatalf("its own shell hidden, the server's new terminal runs %q, want the first profile", got)
	}
	ms := a.launchMachines()
	i := slices.IndexFunc(ms, func(m LaunchMachine) bool { return m.ID == "srv" })
	if i < 0 {
		t.Fatal("with profiles shown, the server is not in the launcher")
	}
	var actions []string
	for _, x := range ms[i].Actions {
		actions = append(actions, x.ID)
	}
	if want := []string{"terminal", "shell:profile:tmux", "shell:profile:htop"}; !slices.Equal(actions, want) {
		t.Fatalf("with its shell and home hidden, the launcher offers %v, want %v", actions, want)
	}
	a.handle(RemoveProfile{ID: "profile:tmux"})
	a.handle(RemoveProfile{ID: "profile:htop"})
	if !a.offersNothing("srv") {
		t.Fatal("with everything hidden, the server still offers something")
	}
	if slices.ContainsFunc(a.launchMachines(), func(m LaunchMachine) bool { return m.ID == "srv" }) {
		t.Fatal("with everything hidden, the server is in the launcher")
	}
	items, _ := a.trayMenu(1)
	if slices.ContainsFunc(items, func(it gunim.TrayItem) bool { return it.Title == "srv" }) {
		t.Fatal("with everything hidden, the server is in the tray")
	}
	a.notePlaces()
	if places := *a.serverPlaces.Load(); slices.ContainsFunc(places, func(p filemanager.Place) bool { return p.FS == serverFS+"srv" }) {
		t.Fatal("with its home hidden, the server is among the file manager's places")
	}
	if err := a.removeServer("srv"); err != nil {
		t.Fatal(err)
	}
	if len(a.st.Found) != 0 {
		t.Fatalf("the server removed, kept of its found profiles are %+v", a.st.Found)
	}
}

// A favourite added in Settings is kept, renamed keeps its colour, and
// one already there is refused.
func TestAFavouriteIsKeptFromSettings(t *testing.T) {
	a := fontApp(t)
	dir, other := t.TempDir(), t.TempDir()
	a.handle(SaveFavourite{Path: dir, Name: "Work"})
	if err := a.saveFavourite(SaveFavourite{Path: dir}); err == nil {
		t.Fatal("the same folder was kept twice")
	}
	kept, _ := a.settings.Favourites()
	kept[len(kept)-1].Color = "green"
	if err := a.settings.PutFavourites(kept); err != nil {
		t.Fatal(err)
	}
	a.handle(SaveFavourite{Was: dir, Path: other, Name: "Projects"})
	kept, _ = a.settings.Favourites()
	if f := kept[len(kept)-1]; f.Path != other || f.Name != "Projects" || f.Color != "green" {
		t.Fatalf("changed, the favourite is %+v", f)
	}
	a.handle(RemoveFavourite{Path: other})
	kept, _ = a.settings.Favourites()
	if slices.ContainsFunc(kept, func(f settings.Favourite) bool { return f.Path == other }) || len(a.st.Notices) != 0 {
		t.Fatalf("removed, the favourites are %+v; notices %+v", kept, a.st.Notices)
	}
}
