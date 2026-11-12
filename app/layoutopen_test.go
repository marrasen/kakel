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
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/driver"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
	"github.com/marrasen/gunim/single"

	"github.com/marrasen/kakel/internal/sshtest"
	"github.com/marrasen/kakel/internal/testhome"
	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/screen"
	"github.com/marrasen/kakel/settings"
	shellfind "github.com/marrasen/kakel/shells"
	"github.com/marrasen/kakel/vfs"
)

// openingApp is a program with settings, an empty server list, its one
// window at a known place, and new windows opened as test windows,
// counted. Shells here run this machine's own shell, as plainShell
// gives it.
func openingApp(t *testing.T) (*app, *atomic.Int32) {
	t.Helper()
	testhome.New(t)
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv("SSH_AUTH_SOCK", "")
	a := newApp(gunimtest.New(t, geom.Sz(400, 300), nil).Client(), screen.NewShells())
	a.ctx = t.Context()
	a.settings = mustSettings(t)
	book, err := remote.LoadBook(filepath.Join(t.TempDir(), "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	a.book = book
	a.placed = func(*ownWin) (driver.Placement, bool) {
		return driver.Placement{Bounds: geom.Rc(10, 20, 800, 600)}, true
	}
	var opened atomic.Int32
	a.openWindow = func(*gunim.Window, geom.Point, geom.Size, *driver.Placement) (gunim.Client, *gunim.Window, error) {
		opened.Add(1)
		w := gunimtest.New(t, geom.Sz(400, 300), nil)
		return w.Client(), w, nil
	}
	t.Cleanup(func() {
		for len(a.st.Panes) > 0 {
			a.remove(a.st.Panes[0].ID)
		}
		for _, id := range a.machines.Connected() {
			_ = a.machines.Get(id).Conn.Close()
		}
	})
	return a, &opened
}

// servers saves a test SSH server under each name, by an id that is
// its name, and returns them by name.
func servers(t *testing.T, a *app, names ...string) map[string]*sshtest.Server {
	t.Helper()
	out := map[string]*sshtest.Server{}
	for _, name := range names {
		s := sshtest.New(t)
		host, port := s.Host()
		if err := a.book.Put(remote.Host{Name: name, Address: host, Port: port, User: "tester"}, ""); err != nil {
			t.Fatal(err)
		}
		out[name] = s
	}
	idsAsNames(t, a.book)
	a.st.Saved = a.book.Hosts()
	return out
}

// refused saves a server under name that refuses every connection.
func refused(t *testing.T, a *app, name string) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	if err := a.book.Put(remote.Host{Name: name, Address: "127.0.0.1", Port: port, User: "tester"}, ""); err != nil {
		t.Fatal(err)
	}
	idsAsNames(t, a.book)
	a.st.Saved = a.book.Hosts()
}

// answering answers every question asked: yes, with the test server's
// password, and counts the questions.
func answering(a *app, asked *int) {
	for _, q := range a.st.Asks {
		*asked++
		ans := AskAnswered{ID: q.ID, Yes: true}
		if len(q.Prompts) > 0 {
			ans.Answers = []string{sshtest.Password}
		}
		a.handle(ans)
	}
}

// keep saves l, returning it as kept.
func keep(t *testing.T, a *app, l settings.SavedLayout) settings.SavedLayout {
	t.Helper()
	kept, _, err := a.settings.SaveLayout(l, false)
	if err != nil {
		t.Fatal(err)
	}
	a.st.Layouts = a.settings.Layouts()
	return kept
}

// item, group and split build a layout's tree.
func item(id string) *settings.LayoutNode { return &settings.LayoutNode{ID: id, Kind: "item"} }

func group(id, front string, tabs ...*settings.LayoutNode) *settings.LayoutNode {
	return &settings.LayoutNode{ID: id, Kind: "group", Front: front, Kids: tabs}
}

func split(id, kind string, share float32, a, b *settings.LayoutNode) *settings.LayoutNode {
	return &settings.LayoutNode{ID: id, Kind: kind, Share: share, Kids: []*settings.LayoutNode{a, b}}
}

// alone is a layout of one group of the panes, in slots p1 and on.
func alone(name string, panes ...settings.SavedPane) settings.SavedLayout {
	l := settings.SavedLayout{Name: name, Panes: map[string]settings.SavedPane{}}
	g := group("#1", "p1")
	for i, p := range panes {
		id := "p" + itoa(i+1)
		l.Panes[id] = p
		g.Kids = append(g.Kids, item(id))
	}
	l.Root = g
	return l
}

// layoutWindow is the window last opened.
func layoutWindow(a *app) *ownWin { return a.wins[len(a.wins)-1] }

// waitingIn are the panes of w still waiting.
func waitingIn(a *app, w *ownWin) []Pane {
	return slices.DeleteFunc(a.panesIn(w), func(p Pane) bool { return p.Kind != KindWaiting })
}

// opens opens layout id and waits for its window, returning it.
func opens(t *testing.T, a *app, opened *atomic.Int32, id string) *ownWin {
	t.Helper()
	before := opened.Load()
	a.handle(OpenLayout{ID: id})
	if len(a.st.Notices) > 0 {
		t.Fatalf("opening said %+v", a.st.Notices)
	}
	waitFor(t, a, "the layout's window", func() bool {
		return opened.Load() == before+1 && len(a.wins) > 0 && len(a.panesIn(layoutWindow(a))) > 0
	})
	return layoutWindow(a)
}

// noticed reports whether a notice titled title was posted.
func noticed(a *app, title string) bool {
	return slices.ContainsFunc(a.st.Notices, func(n Notice) bool { return n.Title == title })
}

// A layout opens in a window of its own, the window in front left as it
// was, in the tree it was saved with: its splits where they were, the
// tabs in front that were. Every slot has its pane, the pane named by
// the user called so, and the keyboard is on the first group's pane in
// front. On this computer nothing is dialled. Saved again, it is the
// same layout.
func TestALayoutOpensAsANewWindowAndSavesTheSame(t *testing.T) {
	a, opened := openingApp(t)
	a.files = newFakeFiles(t)
	a.addPane(Pane{ID: "p0", Title: "here", Kind: KindFileManager}, nil, Placement{})
	first := a.cur
	dir := t.TempDir()
	kept := keep(t, a, settings.SavedLayout{
		Name: "Work",
		Root: split("#1", "row", 0.3,
			group("#2", "p2", item("p1"), item("p2")),
			split("#3", "column", 0.6, group("#4", "p3", item("p3")), group("#5", "p4", item("p4")))),
		Panes: map[string]settings.SavedPane{
			"p1": {Kind: settings.LayoutTerminal, Command: plainShell()},
			"p2": {Kind: settings.LayoutCommand, Line: waitCommand(60), Folder: dir},
			"p3": {Kind: settings.LayoutTerminal, Command: plainShell(), Title: "named"},
			"p4": {Kind: settings.LayoutFiles, Folder: dir},
		},
		Place: &settings.WindowPlace{X: 10, Y: 20, W: 800, H: 600},
	})

	w := opens(t, a, opened, kept.ID)
	if len(a.machines.Dialing()) != 0 {
		t.Fatalf("a layout of this computer dials %v", a.machines.Dialing())
	}
	waitFor(t, a, "every pane to start", func() bool { return len(waitingIn(a, w)) == 0 })
	waitFor(t, a, "the files to show their folder", func() bool {
		for _, p := range a.panesIn(w) {
			if p.Kind == KindFileManager && a.filesFolder(p.ID, machines.Local) == "" {
				return false
			}
		}
		return true
	})

	if w == first || len(a.panesIn(first)) != 1 {
		t.Fatalf("the layout opened in the first window, which holds %+v", a.panesIn(first))
	}
	root := a.layout.Window(w.id).Root
	if root.Kind != "row" || root.Share != 0.3 || root.Kids[1].Kind != "column" || root.Kids[1].Share != 0.6 {
		t.Fatalf("the tree is %+v", root)
	}
	left := root.Kids[0]
	if len(left.Kids) != 2 || left.Front != left.Kids[1].ID || a.kindOfPane(left.Front) != KindTerminal || a.commands[left.Front].argv == nil {
		t.Fatalf("the left group is %+v, its front a %q", left, a.kindOfPane(left.Front))
	}
	if i := slices.IndexFunc(a.st.Panes, func(p Pane) bool { return p.Title == "named" }); i < 0 || !a.st.Panes[i].Named {
		t.Fatalf("the pane named is not called so: %+v", a.panesIn(w))
	}
	if a.focusIn(w) != frontOf(left) || a.cur != w {
		t.Fatalf("the keyboard is on %q in window %d, want %q", a.focusIn(w), a.cur.id, frontOf(left))
	}

	again, _, _, err := a.saveLayout(SaveLayout{Window: w.id, Name: "Again"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again.Root, kept.Root) || !reflect.DeepEqual(again.Panes, kept.Panes) || !reflect.DeepEqual(again.Place, kept.Place) {
		t.Fatalf("saved again, the layout is\n%+v %+v\nwant\n%+v %+v", again.Root, again.Panes, kept.Root, kept.Panes)
	}
}

// Two servers, three panes: each server is dialled once, and asked
// about once, however many of its panes wait. A server that cannot be
// reached fails only its own panes, which say why, and Retry connects
// them once it can be reached.
func TestEachServerIsDialledOnceAndFailsAlone(t *testing.T) {
	a, opened := openingApp(t)
	srv := servers(t, a, "one", "two")
	refused(t, a, "bad")
	kept := keep(t, a, alone("Servers",
		settings.SavedPane{Kind: settings.LayoutTerminal, Host: "one", HostID: "one", Profile: LoginShell},
		settings.SavedPane{Kind: settings.LayoutCommand, Host: "one", HostID: "one", Line: "sleep 60"},
		settings.SavedPane{Kind: settings.LayoutTerminal, Host: "two", HostID: "two", Profile: LoginShell},
		settings.SavedPane{Kind: settings.LayoutTerminal, Host: "bad", HostID: "bad", Profile: LoginShell},
		settings.SavedPane{Kind: settings.LayoutFiles, Host: "bad", HostID: "bad"},
	))
	a.files = newFakeFiles(t)

	w := opens(t, a, opened, kept.ID)
	asked := 0
	waitFor(t, a, "the servers' panes to start, and bad's to fail", func() bool {
		answering(a, &asked)
		left := waitingIn(a, w)
		return len(left) == 2 && a.st.Waits[left[0].ID].Failed != "" && a.st.Waits[left[1].ID].Failed != ""
	})

	if srv["one"].Conns() != 1 || srv["two"].Conns() != 1 {
		t.Fatalf("one was dialled %d times and two %d", srv["one"].Conns(), srv["two"].Conns())
	}
	// A host key and a password for each server reached.
	if asked != 4 {
		t.Fatalf("%d questions asked for two servers", asked)
	}
	bad := waitingIn(a, w)[0]
	if st := a.st.Waits[bad.ID]; st.Failed != "Couldn't connect to bad" || st.Why == "" {
		t.Fatalf("bad's pane says %+v", st)
	}
	if len(a.panesIn(w)) != 5 || slices.ContainsFunc(a.panesIn(w)[:3], func(p Pane) bool { return p.Kind != KindTerminal }) {
		t.Fatalf("the window holds %+v", a.panesIn(w))
	}

	// bad comes up, where two is.
	host, port := srv["two"].Host()
	if err := a.book.Put(remote.Host{Name: "bad", Address: host, Port: port, User: "tester"}, "bad"); err != nil {
		t.Fatal(err)
	}
	// Retry on one retries both, which wait on one connection.
	a.handle(RetryWaiting{Pane: bad.ID})
	for _, p := range waitingIn(a, w) {
		if st := a.st.Waits[p.ID]; st.Failed != "" || st.Doing == "" {
			t.Fatalf("retried, a pane says %+v", st)
		}
	}
	waitFor(t, a, "bad's pane to start", func() bool { answering(a, &asked); return len(waitingIn(a, w)) == 0 })
	if srv["one"].Conns() != 1 || srv["two"].Conns() != 2 {
		t.Fatalf("retried, one was dialled %d times and two %d", srv["one"].Conns(), srv["two"].Conns())
	}
}

// A server removed since the layout was saved fails its pane, saying
// so, and Retry looks for it again.
func TestAServerRemovedFailsItsPane(t *testing.T) {
	a, opened := openingApp(t)
	kept := keep(t, a, alone("Gone", settings.SavedPane{Kind: settings.LayoutTerminal, Host: "old", HostID: "s9", Profile: LoginShell}))

	w := opens(t, a, opened, kept.ID)

	p := waitingIn(a, w)
	if len(p) != 1 || a.st.Waits[p[0].ID].Failed != "Couldn't connect to old" || !noticed(a, "Couldn't connect to old") {
		t.Fatalf("the pane says %+v; notices %+v", a.st.Waits, a.st.Notices)
	}
	a.handle(RetryWaiting{Pane: p[0].ID})
	if st := a.st.Waits[p[0].ID]; st.Failed != "Couldn't connect to old" || len(a.machines.Dialing()) != 0 {
		t.Fatalf("retried, it says %+v and dials %v", st, a.machines.Dialing())
	}
}

// A profile no longer saved starts the default shell, here or on a
// server, and says so; a folder no longer there starts in the default
// folder, and says so. The panes start all the same.
func TestWhatIsGoneFallsBackAndSaysSo(t *testing.T) {
	a, opened := openingApp(t)
	servers(t, a, "one")
	kept := keep(t, a, alone("Fallen",
		settings.SavedPane{Kind: settings.LayoutTerminal, Profile: "profile:gone"},
		settings.SavedPane{Kind: settings.LayoutTerminal, Host: "one", HostID: "one", Profile: "profile:also-gone"},
		settings.SavedPane{Kind: settings.LayoutTerminal, Command: shellSayingDir(), Folder: filepath.Join(t.TempDir(), "no-such-folder")},
	))

	w := opens(t, a, opened, kept.ID)
	asked := 0
	waitFor(t, a, "the panes to start", func() bool { answering(a, &asked); return len(waitingIn(a, w)) == 0 })

	profiles := 0
	for _, n := range a.st.Notices {
		if n.Title == "Profile not found" {
			profiles++
		}
	}
	if profiles != 2 || !noticed(a, "Folder not found") {
		t.Fatalf("it said %+v", a.st.Notices)
	}
	ps := a.panesIn(w)
	if len(ps) != 3 || slices.ContainsFunc(ps, func(p Pane) bool { return p.Kind != KindTerminal }) {
		t.Fatalf("the window holds %+v", ps)
	}
	// Here, the default shell, which is started with nothing named.
	if argv := a.argvs[ps[0].ID]; argv != nil {
		t.Fatalf("the profile gone started %q", argv)
	}
	// There, the server's own shell, which runs no profile.
	if run := a.farRuns[ps[1].ID]; run != "" || !textIn(t, a, ps[1].ID, "READY") {
		t.Fatalf("the profile gone on one started %q", run)
	}
	// The folder gone: the default, which is where kakel was started.
	wd, _ := os.Getwd()
	if !textIn(t, a, ps[2].ID, wd) {
		t.Fatalf("the folder gone opened the shell elsewhere than %s", wd)
	}
}

// textIn waits for pane id's terminal to show text, and reports whether
// it did.
func textIn(t *testing.T, a *app, id, text string) bool {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		// Read across the lines a long one wraps onto.
		if tm := a.terminal(id); tm != nil && strings.Contains(strings.ReplaceAll(tm.AllText(), "\n", ""), text) {
			return true
		}
		settleEvents(a)
	}
	if tm := a.terminal(id); tm != nil {
		t.Logf("pane %s shows %q", id, tm.AllText())
	}
	return false
}

// A waiting pane closed before its server is reached takes the
// connection with it, question and all, and nothing opens for it later,
// here or in another window.
func TestClosingAWaitingPaneCancelsItsStart(t *testing.T) {
	a, opened := openingApp(t)
	srv := servers(t, a, "one")
	kept := keep(t, a, alone("One", settings.SavedPane{Kind: settings.LayoutTerminal, Host: "one", HostID: "one", Profile: LoginShell}))

	w := opens(t, a, opened, kept.ID)
	waitFor(t, a, "the host key question", func() bool { return len(a.st.Asks) > 0 })
	a.handle(ClosePane{Pane: waitingIn(a, w)[0].ID})
	waitFor(t, a, "the connection to be given up", func() bool { return len(a.machines.Dialing()) == 0 && len(a.st.Asks) == 0 })
	settleEvents(a)

	if len(a.st.Panes) != 0 || a.machines.Get("one").Conn != nil || srv["one"].Sessions() != 0 {
		t.Fatalf("closed, there are panes %+v, a connection %v, %d sessions", a.st.Panes, a.machines.Get("one").Conn != nil, srv["one"].Sessions())
	}
	if len(a.slots) != 0 || len(a.st.Waits) != 0 {
		t.Fatalf("closed, it keeps %v and %v", a.slots, a.st.Waits)
	}
}

// The window closed while its server is being reached: no pane opens
// in another window, and nothing is left waiting.
func TestClosingTheWindowMidConnect(t *testing.T) {
	a, opened := openingApp(t)
	srv := servers(t, a, "one")
	kept := keep(t, a, alone("One",
		settings.SavedPane{Kind: settings.LayoutTerminal, Host: "one", HostID: "one", Profile: LoginShell},
		settings.SavedPane{Kind: settings.LayoutFiles, Host: "one", HostID: "one"},
	))

	w := opens(t, a, opened, kept.ID)
	waitFor(t, a, "the host key question", func() bool { return len(a.st.Asks) > 0 })
	a.closeWindow(w)
	waitFor(t, a, "the connection to be given up", func() bool { return len(a.machines.Dialing()) == 0 && len(a.st.Asks) == 0 })
	settleEvents(a)

	if !w.gone || len(a.st.Panes) != 0 || srv["one"].Sessions() != 0 || len(a.slots) != 0 {
		t.Fatalf("closed, the window is gone %v, panes %+v, %d sessions, %d slots", w.gone, a.st.Panes, srv["one"].Sessions(), len(a.slots))
	}
}

// settleEvents runs what the program is told for a while, for anything
// late to land.
func settleEvents(a *app) {
	deadline := time.After(300 * time.Millisecond)
	for {
		select {
		case f := <-a.events:
			f()
		case <-deadline:
			return
		}
	}
}

// Started plainly, kakel opens each layout kept to open at start in a
// window of its own, in place of the window with a terminal, and the
// first window, never shown, goes. The others stay closed.
func TestLayoutsOpenAtStart(t *testing.T) {
	a, opened := openingApp(t)
	first := a.cur
	shell := settings.SavedPane{Kind: settings.LayoutTerminal, Command: plainShell()}
	for _, name := range []string{"Work", "Play", "Later"} {
		l := keep(t, a, alone(name, shell, shell))
		if name != "Later" {
			if err := a.settings.PutLayoutAtStart(l.ID, true); err != nil {
				t.Fatal(err)
			}
		}
	}

	a.startInTray()
	waitFor(t, a, "the two layouts' windows", func() bool { return opened.Load() == 2 && len(a.liveWins()) == 2 })

	if !first.gone {
		t.Fatal("the first window stays")
	}
	for _, w := range a.liveWins() {
		if ps := a.panesIn(w); len(ps) != 2 || len(waitingIn(a, w)) != 0 {
			t.Fatalf("a layout's window holds %+v", ps)
		}
	}
	if len(a.st.Panes) != 4 {
		t.Fatalf("kakel opened %+v", a.st.Panes)
	}
}

// A layout kept to open at start that cannot be opened leaves the start
// as it is with none: a window with a terminal, which says why.
func TestALayoutThatCannotOpenAtStartLeavesTheTerminal(t *testing.T) {
	a, opened := openingApp(t)
	l := keep(t, a, alone("Odd", settings.SavedPane{Kind: "telnet"}))
	if err := a.settings.PutLayoutAtStart(l.ID, true); err != nil {
		t.Fatal(err)
	}

	a.startInTray()
	waitFor(t, a, "a window", func() bool { return opened.Load() == 1 && len(a.st.Panes) == 1 })

	w := layoutWindow(a)
	if p := a.panesIn(w); len(p) != 1 || p[0].Kind != KindTerminal {
		t.Fatalf("the window holds %+v", p)
	}
	if i := slices.IndexFunc(a.st.Notices, func(n Notice) bool { return n.Title == "Couldn't open the layout" }); i < 0 || a.st.Notices[i].win != w.id {
		t.Fatalf("it said %+v, in window %d", a.st.Notices, w.id)
	}
}

// kakel started again while it runs hands its command line over, and
// opens a terminal: the layouts open only as kakel starts.
func TestAKakelStartedAgainOpensNoLayout(t *testing.T) {
	a, opened := openingApp(t)
	l := keep(t, a, alone("Work", settings.SavedPane{Kind: settings.LayoutTerminal, Command: plainShell()}))
	if err := a.settings.PutLayoutAtStart(l.ID, true); err != nil {
		t.Fatal(err)
	}

	a.handover(single.Handover{Dir: t.TempDir()})
	waitFor(t, a, "a window", func() bool { return opened.Load() == 1 && len(a.st.Panes) == 1 })

	if p := a.st.Panes[0]; p.Kind != KindTerminal || len(a.slots) != 0 {
		t.Fatalf("started again, kakel opened %+v", a.st.Panes)
	}
}

// A pane that starts after the user went to another window leaves that
// window in front, and has the keyboard in its own where the waiting
// pane had it.
func TestALatePaneLeavesTheWindowInFront(t *testing.T) {
	a, opened := openingApp(t)
	servers(t, a, "one")
	first := a.cur
	a.addPane(Pane{ID: "p0", Title: "here", Kind: KindFileManager}, nil, Placement{})
	kept := keep(t, a, alone("One", settings.SavedPane{Kind: settings.LayoutTerminal, Host: "one", HostID: "one", Profile: LoginShell}))

	w := opens(t, a, opened, kept.ID)
	waiting := waitingIn(a, w)[0].ID
	if a.focusIn(w) != waiting {
		t.Fatalf("the keyboard is on %q, want the waiting pane", a.focusIn(w))
	}
	a.front(first)
	asked := 0
	waitFor(t, a, "the pane to start", func() bool { answering(a, &asked); return len(waitingIn(a, w)) == 0 })

	if a.cur != first || a.st.Focus != "p0" {
		t.Fatalf("the window in front is %d, with the keyboard on %q", a.cur.id, a.st.Focus)
	}
	if got := a.focusIn(w); got == waiting || a.kindOfPane(got) != KindTerminal {
		t.Fatalf("in the layout's window the keyboard is on %q", got)
	}
}

// On a server, a terminal and a command start in the folder kept: the
// account's own shell by /bin/sh changing there first, and a command
// with cd. A folder no longer there, looked for over the server's files,
// starts each in the default folder, and the files at theirs, each said.
//
// The test server's files are this machine's, so on Windows it is a
// server with no /bin/sh, as a Windows server is: nothing can change to
// the folder before its own shell starts, so the shell starts where the
// login lands, and says it could not restore the folder.
func TestServerPanesStartInTheirFolder(t *testing.T) {
	a, opened := openingApp(t)
	files := newFakeFiles(t)
	a.files = files
	servers(t, a, "one")
	// The test server's files are this machine's.
	dir := t.TempDir()
	gone := filepath.Join(dir, "gone")
	on := func(p settings.SavedPane) settings.SavedPane { p.Host, p.HostID = "one", "one"; return p }
	kept := keep(t, a, alone("Folders",
		on(settings.SavedPane{Kind: settings.LayoutTerminal, Profile: LoginShell, Folder: dir}),
		on(settings.SavedPane{Kind: settings.LayoutCommand, Line: "make it", Folder: dir}),
		on(settings.SavedPane{Kind: settings.LayoutTerminal, Profile: LoginShell, Folder: gone}),
		on(settings.SavedPane{Kind: settings.LayoutCommand, Line: "make it", Folder: gone}),
		on(settings.SavedPane{Kind: settings.LayoutFiles, Folder: gone}),
	))

	w := opens(t, a, opened, kept.ID)
	asked := 0
	waitFor(t, a, "the panes to start", func() bool { answering(a, &asked); return len(waitingIn(a, w)) == 0 })

	ps := a.panesIn(w)
	if runtime.GOOS == "windows" {
		if !textIn(t, a, ps[0].ID, "READY") || !noticed(a, "Couldn't restore the folder") {
			t.Fatalf("with no /bin/sh, the shell did not start where the login lands, saying so: %+v", a.st.Notices)
		}
	} else if !textIn(t, a, ps[0].ID, `RAN '/bin/sh' '-c' 'cd -- "$1"; exec "${SHELL:-/bin/sh}" -l' 'sh' '`+dir+`'`) {
		t.Fatal("the shell was not started in its folder")
	}
	if !textIn(t, a, ps[1].ID, `RAN cd -- '`+dir+`' && 'make' 'it'`) {
		t.Fatal("the command was not run in its folder")
	}
	if !textIn(t, a, ps[2].ID, "READY") {
		t.Fatal("with its folder gone, the shell was not started where the login lands")
	}
	if !textIn(t, a, ps[3].ID, `RAN 'make' 'it'`) {
		t.Fatal("with its folder gone, the command was not run in the default folder")
	}
	if len(files.opened) != 1 || files.opened[0].Dir == gone {
		t.Fatalf("with its folder gone, the files opened at %+v", files.opened)
	}
	said := 0
	for _, n := range a.st.Notices {
		if n.Title == "Folder not found" {
			said++
		}
	}
	if said != 3 {
		t.Fatalf("it said %+v", a.st.Notices)
	}
}

// Two servers behind one jump host: the jump host is signed in to once,
// and both are reached through it.
func TestAJumpHostSharedIsDialledOnce(t *testing.T) {
	a, opened := openingApp(t)
	srv := servers(t, a, "jump")
	for _, name := range []string{"b", "c"} {
		s := sshtest.New(t)
		host, port := s.Host()
		if err := a.book.Put(remote.Host{Name: name, Address: host, Port: port, User: "tester", Via: "jump"}, ""); err != nil {
			t.Fatal(err)
		}
		srv[name] = s
	}
	idsAsNames(t, a.book)
	a.st.Saved = a.book.Hosts()
	kept := keep(t, a, alone("Behind",
		settings.SavedPane{Kind: settings.LayoutTerminal, Host: "b", HostID: "b", Profile: LoginShell},
		settings.SavedPane{Kind: settings.LayoutTerminal, Host: "c", HostID: "c", Profile: LoginShell},
	))

	w := opens(t, a, opened, kept.ID)
	asked := 0
	waitFor(t, a, "the panes to start", func() bool { answering(a, &asked); return len(waitingIn(a, w)) == 0 })

	// A host key and a password for each of the three.
	if srv["jump"].Conns() != 1 || asked != 6 {
		t.Fatalf("the jump host was dialled %d times, and %d questions asked", srv["jump"].Conns(), asked)
	}
}

// A waiting pane closed while its dial has someone else waiting on it,
// as files opened there meanwhile, leaves the dial going.
func TestClosingAWaitingPaneLeavesADialOthersWaitOn(t *testing.T) {
	a, opened := openingApp(t)
	servers(t, a, "one")
	kept := keep(t, a, alone("One", settings.SavedPane{Kind: settings.LayoutTerminal, Host: "one", HostID: "one", Profile: LoginShell}))

	w := opens(t, a, opened, kept.ID)
	waitFor(t, a, "the host key question", func() bool { return len(a.st.Asks) > 0 })
	got, failed := false, error(nil)
	if err := a.withFilesHow("one", func(vfs.FS) { got = true }, func(err error) { failed = err }, true); err != nil {
		t.Fatal(err)
	}
	a.handle(ClosePane{Pane: waitingIn(a, w)[0].ID})
	asked := 0
	waitFor(t, a, "the files", func() bool { answering(a, &asked); return got || failed != nil })

	if failed != nil {
		t.Fatalf("the files failed: %v", failed)
	}
}

// A name given to a waiting pane is the pane's once it starts.
func TestANameGivenWhileWaitingStays(t *testing.T) {
	a, opened := openingApp(t)
	servers(t, a, "one")
	kept := keep(t, a, alone("One", settings.SavedPane{Kind: settings.LayoutTerminal, Host: "one", HostID: "one", Profile: LoginShell}))

	w := opens(t, a, opened, kept.ID)
	a.handle(RenamePane{Pane: waitingIn(a, w)[0].ID, Title: "mine"})
	asked := 0
	waitFor(t, a, "the pane to start", func() bool { answering(a, &asked); return len(waitingIn(a, w)) == 0 })

	if p := a.panesIn(w)[0]; p.Title != "mine" || !p.Named {
		t.Fatalf("started, the pane is %+v", p)
	}
}

// Exit asks nothing for a layout's pane still waiting, as closing its
// window does not.
func TestExitAsksNothingForAWaitingPane(t *testing.T) {
	a, opened := openingApp(t)
	servers(t, a, "one")
	kept := keep(t, a, alone("One", settings.SavedPane{Kind: settings.LayoutTerminal, Host: "one", HostID: "one", Profile: LoginShell}))

	opens(t, a, opened, kept.ID)
	waitFor(t, a, "the host key question", func() bool { return len(a.st.Asks) > 0 })

	if open := a.whatIsOpen(); len(open) != 0 {
		t.Fatalf("Exit would ask about %v", open)
	}
}

// With no file manager to open, a files pane says so, in the Window Log
// too.
func TestFilesWithNoFileManagerSaySo(t *testing.T) {
	a, opened := openingApp(t)
	kept := keep(t, a, alone("Files", settings.SavedPane{Kind: settings.LayoutFiles}))

	w := opens(t, a, opened, kept.ID)

	p := waitingIn(a, w)
	if len(p) != 1 || a.st.Waits[p[0].ID].Why != reason(errNoFileManager) || !noticed(a, "Couldn't open the files") {
		t.Fatalf("it says %+v; notices %+v", a.st.Waits, a.st.Notices)
	}
}

// A shell or a command that arrives for a window closed while it was
// being started is closed, and nothing opens.
func TestAStartArrivingForAClosedWindowIsClosed(t *testing.T) {
	for _, p := range []settings.SavedPane{
		{Kind: settings.LayoutTerminal, Profile: LoginShell},
		{Kind: settings.LayoutCommand, Line: "make it"},
	} {
		a, opened := openingApp(t)
		srv := servers(t, a, "one")
		p.Host, p.HostID = "one", "one"
		kept := keep(t, a, alone("One", p))
		srv["one"].StallChannels()

		w := opens(t, a, opened, kept.ID)
		asked := 0
		waitFor(t, a, "the start on its way", func() bool { answering(a, &asked); return a.starting > 0 })
		a.closeWindow(w)
		srv["one"].AnswerChannels()
		waitFor(t, a, "the start to land", func() bool { return a.starting == 0 })
		settleEvents(a)

		if len(a.st.Panes) != 0 {
			t.Fatalf("%s: closed, kakel opened %+v", p.Kind, a.st.Panes)
		}
		waitFor(t, a, "the session to close", func() bool { return srv["one"].Sessions() == 0 })
	}
}

// A folder looked for for a pane no longer waiting says nothing, and
// starts nothing.
func TestAFolderFoundTooLateIsLetGo(t *testing.T) {
	a, _ := openingApp(t)
	called := false
	a.folderThere(filepath.Join(t.TempDir(), "gone"), "The terminal starts in the default folder.", folderHere, func() bool { return false }, func(string) { called = true })
	settleEvents(a)

	if called || len(a.st.Notices) != 0 {
		t.Fatalf("too late, it started %v and said %+v", called, a.st.Notices)
	}
}

// Started into the tray with a layout kept to open that cannot be opened,
// kakel says so from the tray, as no window opens to say it in.
func TestALayoutThatCannotOpenIsSaidFromTheTray(t *testing.T) {
	a, opened := openingApp(t)
	var popped []string
	a.traySet = Tray{
		Set:      func(gunim.Tray) error { return nil },
		StayOpen: func(bool) {},
		Notify:   func(title, body string) error { popped = append(popped, title+": "+body); return nil },
	}
	l := keep(t, a, alone("Odd", settings.SavedPane{Kind: "telnet"}))
	if err := a.settings.PutLayoutAtStart(l.ID, true); err != nil {
		t.Fatal(err)
	}

	a.startInTray()
	settleEvents(a)

	if opened.Load() != 0 || len(popped) != 1 || !strings.HasPrefix(popped[0], "Couldn't open the layout: Odd") {
		t.Fatalf("%d windows opened; the tray said %q", opened.Load(), popped)
	}
}

// A local terminal's folder is kept as Windows names it, whichever
// shell said it: the Command Prompt's as it is, PowerShell's C:/ as
// C:\, WSL's and Git Bash's own paths as the Windows ones. Away from
// Windows a path is kept as the shell said it. Windows is passed in,
// so this runs on every system.
func TestALocalFolderIsKeptAsThisComputerNamesIt(t *testing.T) {
	wsl := shellfind.Shell{ID: "wsl:Ubuntu", Path: `C:\Windows\system32\wsl.exe`, Args: []string{"-d", "Ubuntu"}, Distro: "Ubuntu"}
	git := shellfind.Shell{ID: "gitbash", Path: `C:\Program Files\Git\bin\bash.exe`, Args: []string{"--login", "-i"}, MSYS: true}
	cmd := shellfind.Shell{ID: "cmd", Path: `C:\Windows\system32\cmd.exe`}
	for _, c := range []struct {
		sh       shellfind.Shell
		said     string
		windows  bool
		want     string
		whatSaid string
	}{
		{cmd, `C:\Users\me\src`, true, `C:\Users\me\src`, "the Command Prompt"},
		{cmd, `\\nas\share\src`, true, `\\nas\share\src`, "the Command Prompt on a share"},
		{shellfind.Shell{}, "C:/Users/me/src", true, `C:\Users\me\src`, "PowerShell"},
		{wsl, "/home/me/src", true, `\\wsl.localhost\Ubuntu\home\me\src`, "WSL in its own files"},
		{wsl, "/mnt/d/src", true, `D:\src`, "WSL on a drive"},
		{git, "/c/Users/me/src", true, `C:\Users\me\src`, "Git Bash on a drive"},
		{git, "/usr/bin", true, `C:\Program Files\Git\usr\bin`, "Git Bash in its own files"},
		{git, "/tmp", true, "", "Git Bash in its /tmp"},
		{shellfind.Shell{}, "/mnt/d/src", true, `D:\src`, "another shell on a WSL drive"},
		{shellfind.Shell{}, "/home/me", true, "", "another shell of /"},
		{shellfind.Shell{}, "/home/me/src", false, "/home/me/src", "a shell away from Windows"},
	} {
		if got := localPathOf(c.sh, c.said, c.windows); got != c.want {
			t.Errorf("%s saying %q is kept as %q, want %q", c.whatSaid, c.said, got, c.want)
		}
	}
}

// A layout's terminal here starts as it was kept, told its folder as it
// takes it: a WSL shell on its command line, in its own words, whether
// it is the shell kept or the one new terminals start; any other shell
// as the folder it starts in, so its command line has none.
func TestALayoutsShellIsToldItsFolder(t *testing.T) {
	a, _ := layoutApp(t)
	wsl := shellfind.Shell{ID: "wsl:Ubuntu", Path: `C:\Windows\system32\wsl.exe`, Args: []string{"-d", "Ubuntu"}, Distro: "Ubuntu"}
	git := shellfind.Shell{ID: "gitbash", Path: `C:\Program Files\Git\bin\bash.exe`, Args: []string{"--login", "-i"}, MSYS: true}
	a.found, a.scanned = []shellfind.Shell{wsl, git}, true
	kept := func(id string) settings.SavedPane {
		return settings.SavedPane{Kind: settings.LayoutTerminal, Profile: id}
	}
	for _, c := range []struct {
		r    settings.SavedPane
		dir  string
		want []string
	}{
		{kept("wsl:Ubuntu"), `\\wsl.localhost\Ubuntu\home\me`, []string{wsl.Path, "-d", "Ubuntu", "--cd", "/home/me"}},
		{kept("wsl:Ubuntu"), `D:\src`, []string{wsl.Path, "-d", "Ubuntu", "--cd", "/mnt/d/src"}},
		{kept("wsl:Ubuntu"), "", []string{wsl.Path, "-d", "Ubuntu"}},
		{kept("gitbash"), `C:\src`, []string{git.Path, "--login", "-i"}},
		{settings.SavedPane{Kind: settings.LayoutTerminal, Command: []string{"/bin/odd", "x"}}, `C:\src`, []string{"/bin/odd", "x"}},
	} {
		if got := a.slotArgv(c.r, c.dir); !slices.Equal(got, c.want) {
			t.Errorf("%+v in %q starts %q, want %q", c.r, c.dir, got, c.want)
		}
	}

	a.handle(PickShell{ID: "wsl:Ubuntu"})
	if got, want := a.slotArgv(kept(""), `D:\src`), []string{wsl.Path, "-d", "Ubuntu", "--cd", "/mnt/d/src"}; !slices.Equal(got, want) {
		t.Fatalf("the default shell, WSL, in D:\\src starts %q, want %q", got, want)
	}
	if len(a.st.Notices) != 0 {
		t.Fatalf("it said %+v", a.st.Notices)
	}
}

// A terminal here that cannot be started, as a shell no longer there,
// turns its waiting pane into one that says so and why, with Retry,
// which tries it again.
func TestALocalShellThatCannotStartSaysWhy(t *testing.T) {
	a, opened := openingApp(t)
	kept := keep(t, a, alone("Gone", settings.SavedPane{Kind: settings.LayoutTerminal, Command: []string{filepath.Join(t.TempDir(), "no-such-shell")}}))

	w := opens(t, a, opened, kept.ID)
	settleEvents(a)

	p := waitingIn(a, w)
	if len(p) != 1 {
		t.Fatalf("the window holds %+v", a.panesIn(w))
	}
	if st := a.st.Waits[p[0].ID]; st.Failed != "Couldn't open a terminal" || st.Why == "" || st.Doing != "" {
		t.Fatalf("the pane says %+v", st)
	}
	said := func() (n int) {
		for _, x := range a.st.Notices {
			if x.Title == "Couldn't open a terminal" {
				n++
			}
		}
		return n
	}
	if said() != 1 {
		t.Fatalf("it said %+v", a.st.Notices)
	}
	a.handle(RetryWaiting{Pane: p[0].ID})
	settleEvents(a)
	if st := a.st.Waits[p[0].ID]; st.Failed != "Couldn't open a terminal" || said() != 2 {
		t.Fatalf("retried, the pane says %+v; notices %+v", st, a.st.Notices)
	}
}

// A files pane on a server is called by its folder's last part, a
// Windows server's folder written with either separator.
func TestAFarFilesPaneIsCalledByItsFolder(t *testing.T) {
	a, _ := layoutApp(t)
	for folder, want := range map[string]string{
		"/srv/app/":         "app",
		`C:\Users\me\src`:   "src",
		"/C:/Users/me/src/": "src",
		"/":                 "Files",
	} {
		r := settings.SavedPane{Kind: settings.LayoutFiles, Host: "web", Folder: folder}
		if got := a.waitTitle(r, machines.Local); got != want {
			t.Errorf("files at %q are called %q, want %q", folder, got, want)
		}
	}
}
