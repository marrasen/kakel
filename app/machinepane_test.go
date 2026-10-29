package app

import (
	"path/filepath"
	"testing"

	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/settings"
)

// panesOf are the machine panes open.
func panesOf(a *app) []Pane {
	var out []Pane
	for _, p := range a.st.Panes {
		if p.Kind == KindMachine {
			out = append(out, p)
		}
	}
	return out
}

// A machine's pane opens once: asked for again, it is the one there. A
// new server's pane becomes the server's as it is saved, named by it,
// and goes as the server is removed.
func TestAMachinesPaneOpensOnceAndFollowsItsServer(t *testing.T) {
	a, _ := dialApp(t)
	a.handle(ShowMachine{})
	a.handle(ShowMachine{})
	if ps := panesOf(a); len(ps) != 1 || ps[0].Machine != machines.Local || ps[0].Title != "This computer" {
		t.Fatalf("asked twice, the machine panes are %+v", ps)
	}
	a.handle(AddServer{})
	a.handle(AddServer{})
	ps := panesOf(a)
	if len(ps) != 2 || !ps[1].Draft || ps[1].Title != "New server" {
		t.Fatalf("a new server asked twice, the machine panes are %+v", ps)
	}
	a.handle(SaveServer{Host: remote.Host{Name: "box", Address: "box.example"}, Pane: ps[1].ID})
	h, ok := a.book.Lookup("box")
	if !ok {
		t.Fatalf("not saved; notices %+v", a.st.Notices)
	}
	ps = panesOf(a)
	if ps[1].Draft || ps[1].Machine != machines.ID(h.ID) || ps[1].Title != "box" {
		t.Fatalf("saved, the new server's pane is %+v", ps[1])
	}
	a.handle(ShowMachine{Machine: machines.ID(h.ID)})
	if len(panesOf(a)) != 2 {
		t.Fatal("the saved server's pane was opened a second time")
	}
	if err := a.removeServer(machines.ID(h.ID)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, a, "the pane to close", func() bool { return len(panesOf(a)) == 1 })
}

// A new server's pane with something typed in it asks before it
// closes, and closes once the answer is yes; one with nothing typed
// closes at once.
func TestANewServersPaneAsksBeforeItLosesWhatWasTyped(t *testing.T) {
	a := fontApp(t)
	a.handle(AddServer{})
	id := panesOf(a)[0].ID
	a.handle(DraftTyped{Pane: id, Typed: true})
	a.handle(ClosePane{Pane: id})
	if len(a.st.Asks) != 1 || len(panesOf(a)) != 1 {
		t.Fatalf("closed with something typed, the asks are %+v", a.st.Asks)
	}
	a.handle(AskAnswered{ID: a.st.Asks[0].ID, Yes: true})
	waitFor(t, a, "the pane to close", func() bool { return len(panesOf(a)) == 0 })
	a.handle(AddServer{})
	a.handle(ClosePane{Pane: panesOf(a)[0].ID})
	waitFor(t, a, "the empty pane to close", func() bool { return len(panesOf(a)) == 0 })
	if len(a.st.Asks) != 0 {
		t.Fatalf("closed empty, the asks are %+v", a.st.Asks)
	}
}

// The default profile on a server is what its New Terminal runs, and
// the login shell made the default again is no profile.
func TestTheDefaultProfileIsWhatANewTerminalRuns(t *testing.T) {
	a, _ := serverWithProfiles(t)
	a.handle(SetDefaultProfile{Machine: "srv", ID: "profile:htop"})
	if got := a.farRun("srv"); got != "profile:htop" {
		t.Fatalf("htop the default, a new terminal runs %q", got)
	}
	a.handle(SetDefaultProfile{Machine: "srv", ID: LoginShell})
	if got := a.farRun("srv"); got != "" {
		t.Fatalf("the login shell the default, a new terminal runs %q", got)
	}
}

// A shell here is taught as its profile says, and as Shell Setup does
// when it does not say.
func TestAShellHereIsTaughtAsItsProfileSays(t *testing.T) {
	a := foundApp(t)
	a.st.ShellSetup = true
	cmd := a.shellCommand("cmd")
	if !a.teaches(cmd) {
		t.Fatal("unsaid, Command Prompt is not taught though Shell Setup is on")
	}
	off := false
	a.handle(SaveFound{Found: settings.FoundProfile{ID: "cmd", Setup: &off}})
	if a.teaches(cmd) {
		t.Fatal("its tick off, Command Prompt is taught")
	}
	if !a.teaches(a.shellCommand("powershell")) {
		t.Fatal("PowerShell, unsaid, is no longer taught")
	}
}

// The start folder This Computer kept before profiles becomes the
// folder of each profile here that has none, once, so each shell starts
// where it did; one with a folder of its own keeps it.
func TestTheStartFolderMovesToEachProfile(t *testing.T) {
	a := foundApp(t)
	dir, own := t.TempDir(), t.TempDir()
	a.handle(SaveFound{Found: settings.FoundProfile{ID: "cmd", Folder: own}})
	if err := a.settings.PutLocal(dir); err != nil {
		t.Fatal(err)
	}
	a.moveStartFolder()
	if got := a.foundEdit("", "powershell").Folder; got != dir {
		t.Fatalf("moved, PowerShell starts in %q, want %s", got, dir)
	}
	if got := a.foundEdit("", "cmd").Folder; got != own {
		t.Fatalf("moved, Command Prompt starts in %q, want its own %s", got, own)
	}
	if a.settings.Local() != "" {
		t.Fatal("moved, the start folder is still kept")
	}
	if folder, _ := a.foundRun(a.shellCommand("powershell")); folder != dir {
		t.Fatalf("a new PowerShell starts in %q", folder)
	}
}

// A new server with something typed in it is something to lose, which
// closing its window or quitting asks about.
func TestATypedNewServerIsAskedAboutOnQuitting(t *testing.T) {
	a := fontApp(t)
	a.handle(AddServer{})
	id := panesOf(a)[0].ID
	if len(a.whatIsOpen()) != 0 {
		t.Fatalf("an empty new server counts as %q", a.whatIsOpen())
	}
	a.handle(DraftTyped{Pane: id, Typed: true})
	if len(a.whatIsOpen()) == 0 {
		t.Fatal("a new server with something typed counts for nothing")
	}
	a.handle(ClosePane{Pane: id})
	a.handle(ClosePane{Pane: id})
	if len(a.st.Asks) != 1 {
		t.Fatalf("closed twice, it asked %d times", len(a.st.Asks))
	}
}

// Save and Connect saves the server and opens a terminal on it.
func TestSaveAndConnectOpensATerminal(t *testing.T) {
	a, answering := dialApp(t)
	h, ok := a.book.Lookup("srv")
	if !ok {
		t.Fatal("no srv")
	}
	a.handle(SaveServer{Host: h, Under: "srv", Connect: true})
	waitFor(t, a, "a shell on the server", func() bool { answering(); return oneShell(a) })
}

// A server's profile with no tick of its own is taught as its server
// says; its own tick says otherwise.
func TestAServerProfileFallsBackToItsServersTick(t *testing.T) {
	a, _ := serverWithProfiles(t)
	h, _ := a.book.Lookup("srv")
	h.Setup = true
	if err := a.book.Put(h, "srv"); err != nil {
		t.Fatal(err)
	}
	a.st.Saved = a.book.Hosts()
	bash := settings.Profile{ID: "profile:bash", HostID: "srv", Command: []string{"bash"}}
	if !a.teachesFar("srv", bash) || !a.teachesFar("srv", settings.Profile{}) {
		t.Fatal("unsaid, the server's shells are not taught though it says so")
	}
	off := false
	bash.Setup = &off
	if a.teachesFar("srv", bash) {
		t.Fatal("its tick off, the profile is taught")
	}
	a.handle(SaveFound{Found: settings.FoundProfile{HostID: "srv", ID: LoginShell, Setup: &off}})
	if a.teachesFar("srv", settings.Profile{}) {
		t.Fatal("its tick off, the login shell is taught")
	}
}

// A favourite with no name of its own goes by its folder's, as the file
// manager's sidebar calls it; a root goes by itself.
func TestAFavouriteGoesByItsFoldersName(t *testing.T) {
	for _, c := range []struct{ path, want string }{
		{"/home/me/src", "src"},
		{"/home/me/src/", "src"},
		{`C:\Users\me\Projects`, "Projects"},
		{`C:\`, `C:\`},
		{"/C:/", `C:\`},
		{`\\wsl$\Ubuntu`, "Ubuntu"},
		{`\\server\share\`, "share"},
		{"/", "/"},
	} {
		if got := (Favourite{Path: c.path}).Label(); got != c.want {
			t.Errorf("%q is called %q, want %q", c.path, got, c.want)
		}
	}
	if got := (Favourite{Path: "/srv", Name: "Sites"}).Label(); got != "Sites" {
		t.Errorf("named, it is called %q", got)
	}
}

// A favourite moved to another folder keeps the colour it is given; on a server,
// a slash at the end makes no second one; renamed, a local one whose
// folder has gone is kept.
func TestAFavouriteMovedOrRenamedStaysOne(t *testing.T) {
	a := fontApp(t)
	dir, other := t.TempDir(), t.TempDir()
	if err := a.settings.PutFavourites([]settings.Favourite{{Path: dir, Color: "green"}, {Machine: "s1", Path: "/srv/www"}}); err != nil {
		t.Fatal(err)
	}
	a.showFavourites()
	a.handle(SaveFavourite{Was: dir, Path: other, Color: "green"})
	if err := a.saveFavourite(SaveFavourite{Machine: "s1", Path: "/srv/www/"}); err == nil {
		t.Fatal("/srv/www/ was kept beside /srv/www")
	}
	gone := filepath.Join(other, "gone")
	if err := a.settings.PutFavourites(append(a.settingsFavs(), settings.Favourite{Path: gone})); err != nil {
		t.Fatal(err)
	}
	a.handle(SaveFavourite{Was: gone, Path: gone, Name: "Old"})
	kept := a.settingsFavs()
	if kept[0].Path != other || kept[0].Color != "green" || kept[len(kept)-1].Name != "Old" || len(a.st.Notices) != 0 {
		t.Fatalf("the favourites are %+v; notices %+v", kept, a.st.Notices)
	}
}

// settingsFavs are the favourites kept.
func (a *app) settingsFavs() []settings.Favourite {
	kept, _ := a.settings.Favourites()
	return kept
}

// A command or a tunnel kept is taken off the list from its pane.
func TestAKeptCommandOrTunnelIsForgotten(t *testing.T) {
	a := fontApp(t)
	tun := settings.SavedTunnel{Host: "srv", HostID: "srv", Kind: "local", Listen: "localhost:5432", Target: "db:5432"}
	if err := a.settings.KeepCommand(settings.SavedCommand{Line: "make test"}, 10); err != nil {
		t.Fatal(err)
	}
	if err := a.settings.KeepTunnel(tun, 10); err != nil {
		t.Fatal(err)
	}
	a.handle(ForgetCommand{Saved: settings.SavedCommand{Line: "make test"}})
	a.handle(ForgetTunnel{Saved: tun})
	if len(a.settings.Commands()) != 0 || len(a.settings.Tunnels()) != 0 || len(a.st.Notices) != 0 {
		t.Fatalf("forgotten, kept are %+v and %+v; notices %+v", a.settings.Commands(), a.settings.Tunnels(), a.st.Notices)
	}
}
