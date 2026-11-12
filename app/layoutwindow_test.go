package app

import (
	"net"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/marrasen/kakel/internal/sessiontest"
	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/relay"
	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/screen"
	"github.com/marrasen/kakel/serve"
	"github.com/marrasen/kakel/settings"
	shellfind "github.com/marrasen/kakel/shells"
)

// windowLayoutApps is a window served, a, and another, b, that opens
// layouts as openingApp's does, with a saved in b's server list as the
// kakel window desk, by an id that is its name, with the key a allows.
func windowLayoutApps(t *testing.T) (a, b *app, opened *atomic.Int32) {
	t.Helper()
	a, _ = agentApp(t)
	dir := t.TempDir()
	a.serving.hostKey, a.serving.allowed = filepath.Join(dir, "host_key"), filepath.Join(dir, "authorized_keys")
	b, opened = openingApp(t)
	keyFile := filepath.Join(t.TempDir(), "id_ed25519")
	writeKey(t, keyFile)
	pub, err := os.ReadFile(keyFile + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.serving.allowed, append(pub[:len(pub)-1], []byte(" laptop\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	a.handle(StartServing{Port: "0"})
	if !a.st.Serving.On {
		t.Fatalf("asked to serve, the window says %+v, notices %+v", a.st.Serving, a.st.Notices)
	}
	t.Cleanup(func() { _ = a.stopServing() })
	t.Cleanup(func() {
		for _, name := range b.machines.Windows() {
			_ = b.disconnectWindow(name)
		}
	})
	host, port, err := net.SplitHostPort(a.st.Serving.Addr)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.book.Put(remote.Host{Name: "desk", Address: host, Port: atoi(t, port), Window: true, Identities: []string{keyFile}}, ""); err != nil {
		t.Fatal(err)
	}
	idsAsNames(t, b.book)
	b.st.Saved = b.book.Hosts()
	return a, b, opened
}

// atoi is s as a number.
func atoi(t *testing.T, s string) int {
	t.Helper()
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			t.Fatalf("%q is no number", s)
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// throughDesk is p through the saved window desk.
func throughDesk(p settings.SavedPane) settings.SavedPane {
	p.Host, p.HostID, p.Window = "desk", "desk", true
	return p
}

// servedPath is folder dir as a served window's files name it: as it
// is, and on Windows, as SFTP spells a drive, /C:/Users/me.
func servedPath(dir string) string {
	if runtime.GOOS == "windows" {
		return "/" + filepath.ToSlash(dir)
	}
	return dir
}

// shownIn waits, running both programs, for pane id of b to show text,
// read across the lines a long one wraps onto.
func shownIn(t *testing.T, a, b *app, id, text string) {
	t.Helper()
	pumpBoth(t, a, b, "the pane to show "+text, func() bool {
		tm := b.terminal(id)
		return tm != nil && strings.Contains(strings.ReplaceAll(tm.AllText(), "\n", ""), text)
	})
}

// A pane through a kakel window is kept by the window, by its name and
// id as a server is, and by the window's key for the machine beyond it
// that the pane runs on and what the window calls it; with its command
// and folder, a terminal's folder as its shell said it there, but for
// one beyond the window, a file manager's, and a name the user gave it. What the window's shell ran is the window's, and is not
// kept. A window connected to by address alone is kept by its address,
// and one removed from the list is left out, and said.
func TestPanesThroughAWindowAreSaved(t *testing.T) {
	a, _ := layoutApp(t)
	desk := deskOf(t, a)
	if err := a.book.Put(remote.Host{Name: "old", Address: "old.example", Window: true}, ""); err != nil {
		t.Fatal(err)
	}
	old, _ := a.book.Lookup("old")
	quick := a.machines.NewQuick("laptop.example:7022", true)
	a.machines.NameFar(desk, "k-box", "box")
	quiet := screen.Hooks{Output: func() {}, Title: func(string) {}, Exit: func() {}, Clipboard: func(string) {}}
	a.addPane(Pane{ID: "t1", Machine: desk}, screen.Open(sessiontest.NewPrinted([]byte(osc7("deskbox", "/home/me"))), a.palette, quiet), Placement{})
	t.Cleanup(func() { a.remove("t1") })
	a.commands["t2"] = command{argv: []string{"top", "-d", "2"}, dir: "/var"}
	a.addPane(Pane{ID: "t2", Machine: desk, On: "k-box", Command: true}, nil, Placement{})
	a.fmFolders = map[string][2]string{"t3": {fsOf(desk), "/srv"}}
	a.addPane(Pane{ID: "t3", Machine: desk, Kind: KindFileManager}, nil, Placement{})
	a.addPane(Pane{ID: "t4", Machine: desk, Title: "mine", Named: true}, nil, Placement{})
	a.addPane(Pane{ID: "t5", Machine: quick}, nil, Placement{})
	a.addPane(Pane{ID: "t6", Machine: machines.ID(old.ID), On: "k-db"}, nil, Placement{})
	a.addPane(Pane{ID: "t7", Machine: desk, On: "k-box"}, screen.Open(sessiontest.NewPrinted([]byte(osc7("box", "/data"))), a.palette, quiet), Placement{})
	t.Cleanup(func() { a.remove("t7") })
	if err := a.book.RemoveID(old.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, a, "the shells to say where they are", func() bool {
		d1, _ := a.terminal("t1").Dir()
		d7, _ := a.terminal("t7").Dir()
		return d1 != "" && d7 != ""
	})

	got, skipped, _, err := a.saveLayout(SaveLayout{Name: "Desk"})
	if err != nil {
		t.Fatal(err)
	}

	if skipped != (leftOut{through: 1}) || skipped.said() != "1 pane through a removed window" {
		t.Errorf("left out %+v, said %q", skipped, skipped.said())
	}
	want := map[string]settings.SavedPane{
		"p1": {Kind: settings.LayoutTerminal, Host: "desk", HostID: string(desk), Window: true, Folder: "/home/me"},
		"p2": {Kind: settings.LayoutCommand, Host: "desk", HostID: string(desk), Window: true, On: "box", OnKey: "k-box", Line: "top -d 2", Folder: "/var"},
		"p3": {Kind: settings.LayoutFiles, Host: "desk", HostID: string(desk), Window: true, Folder: "/srv"},
		"p4": {Kind: settings.LayoutTerminal, Host: "desk", HostID: string(desk), Window: true, Title: "mine"},
		"p5": {Kind: settings.LayoutTerminal, Host: "laptop.example:7022", Window: true},
		// Beyond the window its folder is not kept: it could not be
		// restored there.
		"p6": {Kind: settings.LayoutTerminal, Host: "desk", HostID: string(desk), Window: true, On: "box", OnKey: "k-box"},
	}
	if !reflect.DeepEqual(got.Panes, want) {
		t.Fatalf("the panes are\n%+v\nwant\n%+v", got.Panes, want)
	}
	if h := LayoutHosts(got, a.serverNamed); !slices.Equal(h, []LayoutHost{{Name: "desk", Window: true}, {Name: "box through desk"}, {Name: "laptop.example:7022", Window: true}}) {
		t.Fatalf("the layout opens on %+v", h)
	}
	if m, err := a.slotMachine(got.Panes["p5"]); err != nil || m != quick {
		t.Fatalf("the window at an address is found as %q, %v, want %q", m, err, quick)
	}
}

// A window reached through a relay is named on a layout's card by its
// code and the relay's machine, never by the relay's secret.
func TestALayoutNamesARelayedWindowWithoutItsSecret(t *testing.T) {
	code := relay.NewCode()
	l := alone("Relayed", settings.SavedPane{Kind: settings.LayoutTerminal, Host: "https://relay.example/k/s3cret#" + code, Window: true})
	h := LayoutHosts(l, nil)
	if len(h) != 1 || strings.Contains(h[0].Name, "s3cret") || h[0].Name != code+" on relay.example" || !h[0].Window {
		t.Fatalf("the layout opens on %+v", h)
	}
}

// A layout of panes through a kakel window: the window is connected to
// once for them all, its host key asked once, and each pane is asked of
// it anew: a terminal on its machine, a command in its folder, a file
// manager at its folder, and a terminal on a server it reaches. Each
// opens in a pane of its own there, which a pane here watches; nothing
// open there is attached to. Saved again, it is the same layout.
func TestALayoutThroughAWindowOpensAndSavesTheSame(t *testing.T) {
	_, conn, _ := tunnelApp(t)
	a, b, opened := windowLayoutApps(t)
	a.machines.At("srv").Conn = conn
	b.files = newFakeFiles(t)
	dir := t.TempDir()
	kept := keep(t, b, alone("Desk",
		throughDesk(settings.SavedPane{Kind: settings.LayoutTerminal, Title: "named"}),
		throughDesk(settings.SavedPane{Kind: settings.LayoutCommand, Line: waitCommand(60), Folder: dir}),
		throughDesk(settings.SavedPane{Kind: settings.LayoutFiles, Folder: servedPath(dir)}),
		throughDesk(settings.SavedPane{Kind: settings.LayoutTerminal, On: "srv", OnKey: "srv"}),
	))
	pumpBoth(t, a, b, "srv listed", func() bool { a.publish(); return true })
	had := len(a.st.Panes)

	w := opens(t, b, opened, kept.ID)
	asked := 0
	pumpBoth(t, a, b, "the panes to start", func() bool { answering(b, &asked); return len(waitingIn(b, w)) == 0 })
	pumpBoth(t, a, b, "the files to show their folder", func() bool {
		return slices.ContainsFunc(b.panesIn(w), func(p Pane) bool {
			return p.Kind == KindFileManager && b.filesFolder(p.ID, p.Machine) != ""
		})
	})

	if asked != 1 || len(b.machines.Windows()) != 1 || len(a.st.Serving.Clients) != 1 {
		t.Fatalf("asked %d questions, connected to %v, served %d windows", asked, b.machines.Windows(), len(a.st.Serving.Clients))
	}
	// The terminal, the command and the server's terminal: new panes there.
	if len(a.st.Panes) != had+3 {
		t.Fatalf("the window has %d panes, want %d", len(a.st.Panes), had+3)
	}
	for _, p := range b.panesIn(w) {
		if p.Machine != "desk" {
			t.Fatalf("%+v is not through desk", p)
		}
	}
	if len(b.st.Notices) != 0 {
		t.Fatalf("it said %+v", b.st.Notices)
	}
	far := b.panesIn(w)[3]
	shownIn(t, a, b, far.ID, "READY")

	again, _, _, err := b.saveLayout(SaveLayout{Window: w.id, Name: "Again"})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		// The Command Prompt there says its folder, which a shell of /
		// here does not: kept, it is where the window started it.
		p := again.Panes["p1"]
		p.Folder = ""
		again.Panes["p1"] = p
	}
	if !reflect.DeepEqual(again.Panes, kept.Panes) || !reflect.DeepEqual(again.Root, kept.Root) {
		t.Fatalf("saved again, the panes are\n%+v\nwant\n%+v", again.Panes, kept.Panes)
	}
}

// A terminal through a window starts in its folder on the window's own
// machine; one whose folder is not there starts in the default folder,
// which is said. On a machine beyond the window it starts where that
// machine starts it, which is said too. A command whose folder is not
// there runs in the default folder, said, and is kept so.
func TestATerminalThroughAWindowStartsInItsFolder(t *testing.T) {
	_, conn, _ := tunnelApp(t)
	a, b, opened := windowLayoutApps(t)
	a.machines.At("srv").Conn = conn
	dir := t.TempDir()
	kept := keep(t, b, alone("Folders",
		throughDesk(settings.SavedPane{Kind: settings.LayoutTerminal, Folder: dir}),
		throughDesk(settings.SavedPane{Kind: settings.LayoutTerminal, Folder: filepath.Join(dir, "gone")}),
		throughDesk(settings.SavedPane{Kind: settings.LayoutTerminal, On: "srv", Folder: "/srv"}),
		throughDesk(settings.SavedPane{Kind: settings.LayoutCommand, Line: waitCommand(60), Folder: filepath.Join(dir, "gone")}),
	))
	pumpBoth(t, a, b, "srv listed", func() bool { a.publish(); return true })

	w := opens(t, b, opened, kept.ID)
	asked := 0
	pumpBoth(t, a, b, "the panes to start", func() bool { answering(b, &asked); return len(waitingIn(b, w)) == 0 })
	in := b.panesIn(w)[0].ID
	b.terminal(in).Paste(printDir() + "\r")
	shownIn(t, a, b, in, dir)

	said := map[string]int{}
	for _, n := range b.st.Notices {
		said[n.Title]++
	}
	if said["Folder not found"] != 2 || said["Couldn't restore the folder"] != 1 || len(b.st.Notices) != 3 {
		t.Fatalf("it said %+v", b.st.Notices)
	}
	// The command ran in the default folder, and is kept so: its folder
	// gone is not said again at the next open.
	again, _, _, err := b.saveLayout(SaveLayout{Window: w.id, Name: "Again"})
	if err != nil {
		t.Fatal(err)
	}
	if f := again.Panes["p4"].Folder; f != "" {
		t.Fatalf("saved again, the command keeps the folder %q", f)
	}
}

// A pane on a machine the window does not reach fails alone, saying
// why, and nothing is left open on the window for it.
func TestAMachineTheWindowDoesNotReachFailsItsPane(t *testing.T) {
	a, b, opened := windowLayoutApps(t)
	kept := keep(t, b, alone("Nowhere",
		throughDesk(settings.SavedPane{Kind: settings.LayoutTerminal}),
		throughDesk(settings.SavedPane{Kind: settings.LayoutTerminal, On: "nowhere"}),
	))
	had := len(a.st.Panes)

	w := opens(t, b, opened, kept.ID)
	asked := 0
	pumpBoth(t, a, b, "the panes to start or fail", func() bool {
		answering(b, &asked)
		return len(waitingIn(b, w)) == 1 && b.slots[waitingIn(b, w)[0].ID].failed
	})

	st := b.st.Waits[waitingIn(b, w)[0].ID]
	if st.Failed != "Couldn't open a shell on nowhere through desk" || !strings.Contains(st.Why, "nowhere") {
		t.Fatalf("the pane says %+v", st)
	}
	if len(a.st.Panes) != had+1 {
		t.Fatalf("the window has %d panes, want %d", len(a.st.Panes), had+1)
	}
}

// A window that cannot be reached fails only its own panes, a pane here
// starting all the same; Retry, once it can be, connects to it once for
// all of them.
func TestAWindowThatRefusesFailsOnlyItsPanes(t *testing.T) {
	a, b, opened := windowLayoutApps(t)
	_, port, _ := net.SplitHostPort(a.st.Serving.Addr)
	if err := a.stopServing(); err != nil {
		t.Fatal(err)
	}
	kept := keep(t, b, alone("Refused",
		settings.SavedPane{Kind: settings.LayoutTerminal, Command: plainShell()},
		throughDesk(settings.SavedPane{Kind: settings.LayoutTerminal}),
		throughDesk(settings.SavedPane{Kind: settings.LayoutCommand, Line: waitCommand(60)}),
	))

	w := opens(t, b, opened, kept.ID)
	pumpBoth(t, a, b, "the window's panes to fail", func() bool {
		ps := waitingIn(b, w)
		return len(ps) == 2 && b.slots[ps[0].ID].failed && b.slots[ps[1].ID].failed
	})
	ps := waitingIn(b, w)
	if st := b.st.Waits[ps[0].ID]; st.Failed != "Couldn't connect to desk" || st.Why == "" {
		t.Fatalf("the pane says %+v", st)
	}
	if b.panesIn(w)[0].Kind != KindTerminal {
		t.Fatalf("the pane here did not start: %+v", b.panesIn(w))
	}

	a.handle(StartServing{Port: port})
	if !a.st.Serving.On {
		t.Fatalf("served again, the window says %+v", a.st.Serving)
	}
	had := len(a.st.Panes)
	b.handle(RetryWaiting{Pane: ps[1].ID})
	asked := 0
	pumpBoth(t, a, b, "the panes to start", func() bool { answering(b, &asked); return len(waitingIn(b, w)) == 0 })
	if asked != 1 || len(a.st.Panes) != had+2 {
		t.Fatalf("retried, %d questions were asked, and the window has %d panes, want %d", asked, len(a.st.Panes), had+2)
	}
}

// A window removed from the list since fails its panes, saying so.
func TestAWindowRemovedFailsItsPanes(t *testing.T) {
	_, b, opened := windowLayoutApps(t)
	kept := keep(t, b, alone("Gone", throughDesk(settings.SavedPane{Kind: settings.LayoutTerminal})))
	if err := b.book.RemoveID("desk"); err != nil {
		t.Fatal(err)
	}

	w := opens(t, b, opened, kept.ID)

	ps := waitingIn(b, w)
	if len(ps) != 1 || b.st.Waits[ps[0].ID].Failed != "Couldn't connect to desk" || !strings.Contains(b.st.Waits[ps[0].ID].Why, "removed") {
		t.Fatalf("the window holds %+v, saying %+v", b.panesIn(w), b.st.Waits)
	}
	if len(b.machines.Dialing()) != 0 || len(b.machines.Windows()) != 0 {
		t.Fatal("a window removed was connected to")
	}
}

// A pane through a window closed before the window answers takes the
// connection with it, question and all, and nothing opens on the window;
// the same for the layout's window closed then.
func TestClosingBeforeTheWindowAnswersLeavesNothingThere(t *testing.T) {
	for _, closing := range []string{"the pane", "the layout's window"} {
		t.Run(closing, func(t *testing.T) {
			a, b, opened := windowLayoutApps(t)
			kept := keep(t, b, alone("Desk",
				throughDesk(settings.SavedPane{Kind: settings.LayoutTerminal}),
				throughDesk(settings.SavedPane{Kind: settings.LayoutFiles}),
			))
			b.files = newFakeFiles(t)
			had := len(a.st.Panes)

			w := opens(t, b, opened, kept.ID)
			pumpBoth(t, a, b, "the host key question", func() bool { return len(b.st.Asks) > 0 })
			if closing == "the pane" {
				for _, p := range waitingIn(b, w) {
					b.handle(ClosePane{Pane: p.ID})
				}
			} else {
				b.closeWindow(w)
			}
			pumpBoth(t, a, b, "the connection to be given up", func() bool { return len(b.machines.Dialing()) == 0 && len(b.st.Asks) == 0 })
			settleEvents(b)
			settleEvents(a)

			if len(b.st.Panes) != 0 || len(b.slots) != 0 || len(b.machines.Windows()) != 0 || len(a.st.Panes) != had {
				t.Fatalf("closed, b holds %+v and %d slots, connected to %v; a has %d panes, want %d",
					b.st.Panes, len(b.slots), b.machines.Windows(), len(a.st.Panes), had)
			}
		})
	}
}

// A pane the window opened for a waiting pane closed meanwhile is closed
// there too, rather than left open on the window with nobody watching.
func TestAPaneOpenedForAClosedSlotIsClosedThere(t *testing.T) {
	a, b, opened := windowLayoutApps(t)
	kept := keep(t, b, alone("Desk", throughDesk(settings.SavedPane{Kind: settings.LayoutTerminal})))
	w := opens(t, b, opened, kept.ID)
	asked := 0
	pumpBoth(t, a, b, "the pane to start", func() bool { answering(b, &asked); return len(waitingIn(b, w)) == 0 })
	had, next := len(a.st.Panes), a.next

	b.next++
	err := b.openThroughHow("desk", "", command{}, "p"+itoa(b.next), "gone", Placement{},
		throughHow{in: true, still: func() bool { return false }}, func(string, error) { t.Error("told of a pane no longer wanted") })
	if err != nil {
		t.Fatal(err)
	}
	pumpBoth(t, a, b, "the start to land", func() bool { return b.starting == 0 && a.next > next })
	pumpBoth(t, a, b, "it to be closed there", func() bool { return len(a.st.Panes) == had })
	if len(b.panesIn(w)) != 1 {
		t.Fatalf("b holds %+v", b.panesIn(w))
	}
}

// farNas saves, on served window a, a server called nas whose id is not
// its name, connected over conn, and returns its id: the key a window
// connected to a knows it by.
func farNas(t *testing.T, a *app, conn *remote.Conn) machines.ID {
	t.Helper()
	book, err := remote.LoadBook(filepath.Join(t.TempDir(), "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := book.Put(remote.Host{Name: "nas", Address: "nas.example"}, ""); err != nil {
		t.Fatal(err)
	}
	a.book = book
	h, _ := book.Lookup("nas")
	if h.ID == "nas" {
		t.Fatal("nas was given its name as its id")
	}
	a.machines.At(machines.ID(h.ID)).Conn = conn
	return machines.ID(h.ID)
}

// A machine beyond a window, opened before the window is connected to,
// is found by the window's key for it, as the window lists it, and not
// by its name, which would make it a second machine there: by the key
// kept, after a rename there, and by the name kept where the key kept is
// one the window no longer knows, as a quick connection's from another
// run.
func TestAMachineBeyondAWindowIsFoundByItsKey(t *testing.T) {
	_, conn, _ := tunnelApp(t)
	a, b, opened := windowLayoutApps(t)
	nas := farNas(t, a, conn)
	pumpBoth(t, a, b, "nas listed", func() bool { a.publish(); return true })
	kept := keep(t, b, alone("Beyond",
		throughDesk(settings.SavedPane{Kind: settings.LayoutTerminal, On: "nas"}),
		throughDesk(settings.SavedPane{Kind: settings.LayoutTerminal, On: "nas", OnKey: "from-another-run"}),
		throughDesk(settings.SavedPane{Kind: settings.LayoutTerminal, On: "nas before its rename", OnKey: string(nas)}),
	))

	w := opens(t, b, opened, kept.ID)
	asked := 0
	pumpBoth(t, a, b, "the panes to start", func() bool { answering(b, &asked); return len(waitingIn(b, w)) == 0 })

	for _, p := range b.panesIn(w) {
		if p.On != string(nas) {
			t.Fatalf("%+v is on %q, want the window's key %q", p, p.On, nas)
		}
	}
	if got := b.machines.Name(machines.FarID("desk", string(nas))); got != "nas through desk" {
		t.Fatalf("the machine is called %q", got)
	}
}

// A window of a kakel from before a pane could be asked for in a folder:
// each pane opens once all the same, a terminal in the window's default
// folder, which is said once, and a command in its folder.
func TestAnOlderWindowOpensThePanesWithoutTheFolder(t *testing.T) {
	a, b, opened := windowLayoutApps(t)
	a.serving.older = true
	if err := a.stopServing(); err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(b.st.Saved[0].ServeAddr())
	a.handle(StartServing{Port: port})
	dir := t.TempDir()
	line := "pwd"
	if runtime.GOOS == "windows" {
		line = "cmd /c cd"
	}
	kept := keep(t, b, alone("Older",
		throughDesk(settings.SavedPane{Kind: settings.LayoutTerminal, Folder: dir}),
		throughDesk(settings.SavedPane{Kind: settings.LayoutCommand, Line: line, Folder: dir}),
	))
	had := len(a.st.Panes)

	w := opens(t, b, opened, kept.ID)
	asked := 0
	pumpBoth(t, a, b, "the panes to start", func() bool { answering(b, &asked); return len(waitingIn(b, w)) == 0 })
	shownIn(t, a, b, b.panesIn(w)[1].ID, dir)

	if len(a.st.Panes) != had+2 {
		t.Fatalf("the window has %d panes, want %d", len(a.st.Panes), had+2)
	}
	said := 0
	for _, n := range b.st.Notices {
		if n.Title == "Couldn't restore the folder" {
			said++
		}
	}
	if said != 1 || len(b.st.Notices) != 1 {
		t.Fatalf("it said %+v", b.st.Notices)
	}
}

// A window connected to by address alone, never saved, is opened by its
// address, with the usual keys.
func TestAWindowByItsAddressOpensAgain(t *testing.T) {
	a, b, opened := windowLayoutApps(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(home, ".ssh", "id_ed25519")
	writeKey(t, key)
	pub, err := os.ReadFile(key + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	allowed, err := os.ReadFile(a.serving.allowed)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.serving.allowed, append(allowed, pub...), 0o600); err != nil {
		t.Fatal(err)
	}
	// Serving reads the keys allowed as it starts.
	if err := a.stopServing(); err != nil {
		t.Fatal(err)
	}
	a.handle(StartServing{Port: "0"})
	kept := keep(t, b, alone("Quick", settings.SavedPane{Kind: settings.LayoutTerminal, Host: a.st.Serving.Addr, Window: true}))
	had := len(a.st.Panes)

	w := opens(t, b, opened, kept.ID)
	asked := 0
	pumpBoth(t, a, b, "the pane to start", func() bool { answering(b, &asked); return len(waitingIn(b, w)) == 0 })

	p := b.panesIn(w)[0]
	if target, window, quick := b.machines.Quick(p.Machine); !quick || !window || target != a.st.Serving.Addr || len(a.st.Panes) != had+1 {
		t.Fatalf("the pane is on %q (%q, a window %v, quick %v); the window has %d panes", p.Machine, target, window, quick, len(a.st.Panes))
	}
	again, _, _, err := b.saveLayout(SaveLayout{Window: w.id, Name: "Again"})
	if err != nil {
		t.Fatal(err)
	}
	if got := again.Panes["p1"]; got.Host != a.st.Serving.Addr || got.HostID != "" || !got.Window {
		t.Fatalf("saved again, the pane is %+v", got)
	}
}

// What a window opened for another is closed for it when asked, and
// nothing else: not a pane its own user opened.
func TestOnlyWhatWasOpenedForAWindowIsClosedForIt(t *testing.T) {
	a, _ := agentApp(t)
	own := a.st.Panes[0].ID
	var err error
	asAgent(t, a, func() { err = a.closeOpenedFor(serve.Attached{ID: own}) })
	if err == nil || !a.has(own) {
		t.Fatalf("asked to close the user's own pane, it gave %v and the pane is there: %v", err, a.has(own))
	}
}

// A served window asked for a terminal in a folder reads the folder as
// the shell its new terminals start says one, and starts that shell
// there: the Command Prompt's and PowerShell's as Windows paths, Git
// Bash's /c/Users/me as C:\Users\me, and a WSL shell's /home/me/src as
// its distribution's, told to it on its command line in its own words.
// Read as Windows reads them on every system.
func TestAWindowReadsAFolderAsItsNewShellSaysIt(t *testing.T) {
	a, _ := layoutApp(t)
	cmd := shellfind.Shell{ID: "cmd", Path: `C:\Windows\system32\cmd.exe`}
	pwsh := shellfind.Shell{ID: "pwsh", Path: `C:\Program Files\PowerShell\7\pwsh.exe`}
	git := shellfind.Shell{ID: "gitbash", Path: `C:\Program Files\Git\bin\bash.exe`, Args: []string{"--login", "-i"}, MSYS: true}
	wsl := shellfind.Shell{ID: "wsl:Ubuntu", Path: `C:\Windows\system32\wsl.exe`, Args: []string{"-d", "Ubuntu"}, Distro: "Ubuntu"}
	a.found, a.scanned = []shellfind.Shell{cmd, pwsh, git, wsl}, true
	for _, c := range []struct {
		shell, said, want string
	}{
		{"cmd", `C:\Users\me\src`, `C:\Users\me\src`},
		{"pwsh", "C:/Users/me/src", `C:\Users\me\src`},
		{"gitbash", "/c/Users/me/src", `C:\Users\me\src`},
		{"wsl:Ubuntu", "/home/me/src", `\\wsl.localhost\Ubuntu\home\me\src`},
		{"wsl:Ubuntu", "/mnt/d/src", `D:\src`},
	} {
		a.handle(PickShell{ID: c.shell})
		if got := a.newShellPath(c.said, true); got != c.want {
			t.Errorf("with %s new, %q is read as %q, want %q", c.shell, c.said, got, c.want)
		}
	}
	a.handle(PickShell{ID: "wsl:Ubuntu"})
	if got, want := a.slotArgv(settings.SavedPane{}, a.newShellPath("/home/me/src", true)), []string{wsl.Path, "-d", "Ubuntu", "--cd", "/home/me/src"}; !slices.Equal(got, want) {
		t.Fatalf("WSL starts %q, want %q", got, want)
	}
	if len(a.st.Notices) != 0 {
		t.Fatalf("it said %+v", a.st.Notices)
	}
}
