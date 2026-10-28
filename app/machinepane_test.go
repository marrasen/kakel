package app

import (
	"slices"
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
// folder of the shell new terminals start, once.
func TestTheStartFolderMovesToTheDefaultProfile(t *testing.T) {
	a := foundApp(t)
	dir := t.TempDir()
	if err := a.settings.PutLocal(dir); err != nil {
		t.Fatal(err)
	}
	a.handle(PickShell{ID: "powershell"})
	a.moveStartFolder()
	if got := a.foundEdit("", "powershell").Folder; got != dir {
		t.Fatalf("moved, PowerShell starts in %q, want %s", got, dir)
	}
	if a.settings.Local() != "" {
		t.Fatal("moved, the start folder is still kept")
	}
	if folder, _ := a.foundRun(a.shellCommand("powershell")); folder != dir {
		t.Fatalf("a new PowerShell starts in %q", folder)
	}
	if !slices.ContainsFunc(a.st.Found, func(f settings.FoundProfile) bool { return f.ID == "powershell" && f.Folder == dir }) {
		t.Fatalf("the found profiles are %+v", a.st.Found)
	}
}
