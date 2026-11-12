package app

import (
	"errors"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/marrasen/gunim/driver"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
	"github.com/marrasen/kakel/internal/sessiontest"
	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/screen"
	"github.com/marrasen/kakel/settings"
	shellfind "github.com/marrasen/kakel/shells"
)

// layoutApp is a program with settings and one saved server, web, whose
// id it returns, and a window at a known place on the screen.
func layoutApp(t *testing.T) (*app, machines.ID) {
	t.Helper()
	a := newApp(gunimtest.New(t, geom.Sz(400, 300), nil).Client(), screen.NewShells())
	a.ctx = t.Context()
	dir := t.TempDir()
	set, err := settings.Load(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	a.settings = set
	book, err := remote.LoadBook(filepath.Join(dir, "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := book.Put(remote.Host{Name: "web", Address: "web.example"}, ""); err != nil {
		t.Fatal(err)
	}
	a.book = book
	a.placed = func(*ownWin) (driver.Placement, bool) {
		return driver.Placement{Bounds: geom.Rc(10, 20, 800, 600)}, true
	}
	return a, machines.ID(book.Hosts()[0].ID)
}

// workWindow fills window one with two tabs, a shell here and one on
// web, beside a command and a file manager in tabs, with Settings split
// off below them, a pane through another window among the tabs and a
// split's chooser beside the shells. The split between the two sides is
// moved off the middle. A second window, in front, holds a shell of its
// own.
func workWindow(t *testing.T, a *app, web machines.ID) (one *ownWin) {
	one = a.cur
	a.argvs["p1"] = []string{"/bin/frobsh", "-l"}
	a.addPane(Pane{ID: "p1", Title: "frobsh"}, nil, Placement{})
	a.farRuns["p2"] = "profile:tmux"
	a.addPane(Pane{ID: "p2", Title: "tmux on web", Machine: web}, nil, Placement{})
	a.commands["p3"] = command{argv: []string{"make", "watch it"}, dir: "/src"}
	a.addPane(Pane{ID: "p3", Title: "build", Named: true, Command: true}, nil, Placement{Beside: "p1"})
	a.addPane(Pane{ID: "p4", Title: "Settings", Kind: KindSettings}, nil, Placement{Beside: "p3", Vertical: true})
	a.fmFolders = map[string][2]string{"p5": {"", "/home/me"}}
	a.addPane(Pane{ID: "p5", Title: "me", Kind: KindFileManager}, nil, Placement{Group: a.layout.GroupOf("p3").ID})
	a.addPane(Pane{ID: "p6", Title: "far", Machine: "win", On: "box"}, nil, Placement{Group: a.layout.GroupOf("p3").ID})
	a.addPane(Pane{ID: "p7", Kind: KindChooser}, nil, Placement{Group: a.layout.GroupOf("p1").ID})
	a.layout.SetShare(a.layout.Parent(a.layout.GroupOf("p1").ID).ID, 0.3)
	two := a.addWindow(gunimtest.New(t, geom.Sz(400, 300), nil).Client(), nil)
	a.front(two)
	a.addPane(Pane{ID: "p8", Title: "elsewhere"}, nil, Placement{})
	return one
}

// A window is saved as the same tree of splits and tabs, its panes in
// slots p1, p2 and on, with what each runs: the profile a shell on a
// saved server runs, by the server's name and id, the program a shell
// here runs, a command's line and folder, a file manager's folder. A
// Settings tab, a split's chooser and a pane through another window are
// left out, the split Settings stood in closing up; only the pane
// through another window is counted, as the tools open again from their
// own commands. The window saved need not be the one in front.
func TestAWindowIsSavedAsALayout(t *testing.T) {
	a, web := layoutApp(t)
	one := workWindow(t, a, web)

	got, skipped, _, err := a.saveLayout(SaveLayout{Window: one.id, Name: "Work"})
	if err != nil {
		t.Fatal(err)
	}

	if skipped != (leftOut{through: 1}) {
		t.Errorf("left out %+v, want the pane through another window", skipped)
	}
	root := got.Root
	if root == nil || root.Kind != "row" || len(root.Kids) != 2 || root.Share != 0.3 {
		t.Fatalf("the tree is %+v", root)
	}
	left, right := root.Kids[0], root.Kids[1]
	slots := func(n *settings.LayoutNode) (out []string) {
		for _, k := range n.Kids {
			out = append(out, k.ID)
		}
		return out
	}
	if left.Kind != "group" || !reflect.DeepEqual(slots(left), []string{"p1", "p2"}) || left.Front != "p2" {
		t.Errorf("the left is %+v", left)
	}
	if right.Kind != "group" || !reflect.DeepEqual(slots(right), []string{"p3", "p4"}) || right.Front != "p4" {
		t.Errorf("the right, Settings split off it, is %+v", right)
	}
	want := map[string]settings.SavedPane{
		"p1": {Kind: settings.LayoutTerminal, Command: []string{"/bin/frobsh", "-l"}},
		"p2": {Kind: settings.LayoutTerminal, Host: "web", HostID: string(web), Profile: "profile:tmux"},
		"p3": {Kind: settings.LayoutCommand, Line: `make "watch it"`, Folder: "/src", Title: "build"},
		"p4": {Kind: settings.LayoutFiles, Folder: "/home/me"},
	}
	if !reflect.DeepEqual(got.Panes, want) {
		t.Errorf("the panes are\n%+v\nwant\n%+v", got.Panes, want)
	}
	if got.Place == nil || *got.Place != (settings.WindowPlace{X: 10, Y: 20, W: 800, H: 600}) {
		t.Errorf("the place is %+v", got.Place)
	}
	if kept := a.settings.Layouts(); len(kept) != 1 || !reflect.DeepEqual(kept[0], got) || len(a.st.Layouts) != 1 {
		t.Errorf("kept %+v, want %+v", kept, got)
	}
}

// A shell here is kept by the profile it runs, one of the user's or one
// kakel found, and a program that is neither by its command line. One
// started with nothing named runs the default shell, and keeps neither.
func TestWhatAShellHereRunsIsKept(t *testing.T) {
	a, _ := layoutApp(t)
	a.st.Profiles = []settings.Profile{{ID: "profile:dev", Name: "Dev", Command: []string{"/bin/devsh", "-i"}}}
	a.found = []shellfind.Shell{{ID: "fish", Title: "fish", Path: "/usr/bin/fish"}}
	runs := map[string][]string{"p1": {"/bin/devsh", "-i"}, "p2": {"/usr/bin/fish"}, "p3": {"/bin/odd", "x"}, "p4": nil}
	for _, id := range []string{"p1", "p2", "p3", "p4"} {
		a.argvs[id] = runs[id]
		a.addPane(Pane{ID: id}, nil, Placement{})
	}

	got, _, _, err := a.saveLayout(SaveLayout{Name: "Here"})
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]settings.SavedPane{
		"p1": {Kind: settings.LayoutTerminal, Profile: "profile:dev"},
		"p2": {Kind: settings.LayoutTerminal, Profile: "fish"},
		"p3": {Kind: settings.LayoutTerminal, Command: []string{"/bin/odd", "x"}},
		"p4": {Kind: settings.LayoutTerminal},
	}
	if !reflect.DeepEqual(got.Panes, want) {
		t.Fatalf("the panes are\n%+v\nwant\n%+v", got.Panes, want)
	}
}

// termPane opens terminal pane id on machine, whose program prints out.
func termPane(t *testing.T, a *app, id string, machine machines.ID, out string) {
	t.Helper()
	quiet := screen.Hooks{Output: func() {}, Title: func(string) {}, Exit: func() {}, Clipboard: func(string) {}}
	a.addPane(Pane{ID: id, Machine: machine}, screen.Open(sessiontest.NewPrinted([]byte(out)), a.palette, quiet), Placement{})
	t.Cleanup(func() { a.remove(id) })
}

// osc7 is what a shell on host prints to say it is in dir.
func osc7(host, dir string) string { return "\x1b]7;file://" + host + dir + "\x07" }

// A shell's folder is kept as it last said: here, as this computer
// names it, whether or not it is there now; on a server, as the server
// names it, while it is the server's own. After an ssh typed in the
// pane, the folder is another machine's, and none is kept.
//
// The shell here is this machine's own. On Windows that is no shell of
// /, and it says a Windows path, as PowerShell spells one in a file URL.
func TestAShellsFolderIsKept(t *testing.T) {
	a, web := layoutApp(t)
	said, here := "/no/such/folder", "/no/such/folder"
	if runtime.GOOS == "windows" {
		said, here = "/C:/no/such/folder", `C:\no\such\folder`
	}
	termPane(t, a, "p1", machines.Local, osc7("", said))
	termPane(t, a, "p2", web, osc7("web01", "/srv")+osc7("web01.example.com", "/srv/app"))
	termPane(t, a, "p3", web, osc7("web01", "/srv")+osc7("db", "/var/lib"))
	waitFor(t, a, "the shells to say where they are", func() bool {
		d1, _ := a.terminal("p1").Dir()
		_, h2 := a.terminal("p2").Dir()
		_, h3 := a.terminal("p3").Dir()
		return d1 != "" && h2 == "web01.example.com" && h3 == "db"
	})

	got, _, _, err := a.saveLayout(SaveLayout{Name: "Folders"})
	if err != nil {
		t.Fatal(err)
	}

	for slot, want := range map[string]string{"p1": here, "p2": "/srv/app", "p3": ""} {
		if f := got.Panes[slot].Folder; f != want {
			t.Errorf("%s keeps the folder %q, want %q", slot, f, want)
		}
	}
}

// A pane on a server removed from the list while the pane stayed open
// is left out and counted: by name alone, it would be found as another
// server, or dialled as an address. A quick connection, which was never
// on the list, is kept by its address.
func TestAPaneOnAServerRemovedIsLeftOut(t *testing.T) {
	a, web := layoutApp(t)
	quick := a.machines.NewQuick("box.example", false)
	a.addPane(Pane{ID: "p1", Machine: web}, nil, Placement{})
	a.addPane(Pane{ID: "p2", Machine: quick}, nil, Placement{})
	if err := a.book.RemoveID(string(web)); err != nil {
		t.Fatal(err)
	}

	got, skipped, _, err := a.saveLayout(SaveLayout{Name: "Left"})
	if err != nil {
		t.Fatal(err)
	}

	if skipped != (leftOut{removed: 1}) || len(got.Panes) != 1 {
		t.Fatalf("%+v left out, the panes are %+v", skipped, got.Panes)
	}
	if p := got.Panes["p1"]; p.HostID != "" || p.Host != a.machines.Name(quick) || p.Host == "" {
		t.Fatalf("the quick connection's pane is %+v", p)
	}
}

// A shell on a server running no profile runs the account's own, and is
// kept as LoginShell: left empty, it would open whatever the server
// runs by then.
func TestAServersLoginShellIsKept(t *testing.T) {
	a, web := layoutApp(t)
	a.addPane(Pane{ID: "p1", Machine: web}, nil, Placement{})

	got, _, _, err := a.saveLayout(SaveLayout{Name: "Web"})
	if err != nil {
		t.Fatal(err)
	}

	if p := got.Panes["p1"]; p.Profile != LoginShell || p.HostID != string(web) {
		t.Fatalf("the pane is %+v", p)
	}
}

// A window holding nothing a layout can open again is refused, and
// nothing is kept. Its tools are not counted as left out.
func TestAWindowOfToolsIsNotSaved(t *testing.T) {
	a, _ := layoutApp(t)
	a.addPane(Pane{ID: "p1", Title: "Settings", Kind: KindSettings}, nil, Placement{})
	a.addPane(Pane{ID: "p2", Title: "Jobs", Kind: KindJobs}, nil, Placement{})

	_, skipped, _, err := a.saveLayout(SaveLayout{Name: "Tools"})

	if !errors.Is(err, ErrNothingToSave) || skipped.total() != 0 {
		t.Fatalf("saving gave %v, %+v left out", err, skipped)
	}
	if kept := a.settings.Layouts(); len(kept) != 0 {
		t.Fatalf("kept %+v", kept)
	}
}

// The layouts are saved, renamed, set to open at start and removed as
// the window asks. A name in use is refused, so the window can offer to
// replace that layout; replaced, it keeps its ID and whether it opens
// at start, and what is handed back is what the file holds.
func TestLayoutsAreSavedAndChangedAsAsked(t *testing.T) {
	a, _ := layoutApp(t)
	a.addPane(Pane{ID: "p1"}, nil, Placement{})
	a.handle(SaveLayout{Name: "Work"})
	a.handle(RenameLayout{ID: "work", Name: "Office"})
	a.handle(SetLayoutAtStart{ID: "work", On: true})
	if kept := a.st.Layouts; len(kept) != 1 || kept[0].Name != "Office" || !kept[0].AtStart || len(a.st.Notices) != 1 {
		t.Fatalf("the layouts are %+v; notices %+v", kept, a.st.Notices)
	}
	a.addPane(Pane{ID: "p2", Kind: KindFileManager}, nil, Placement{})

	a.handle(SaveLayout{Name: "office"})
	if len(a.st.Notices) != 2 || a.st.Notices[1].Title != "Couldn't save the layout" {
		t.Fatalf("saving over Office unasked said %+v", a.st.Notices)
	}
	again, _, _, err := a.saveLayout(SaveLayout{Name: "  office ", Replace: true})
	if err != nil {
		t.Fatal(err)
	}

	kept := a.settings.Layouts()
	if len(kept) != 1 || !reflect.DeepEqual(kept[0], again) || again.ID != "work" || again.Name != "office" || !again.AtStart || len(again.Panes) != 2 {
		t.Fatalf("handed back %+v, kept %+v", again, kept)
	}
	a.handle(RemoveLayout{ID: "work"})
	if len(a.settings.Layouts()) != 0 || len(a.st.Layouts) != 0 || len(a.st.Notices) != 3 {
		t.Fatalf("removed, the layouts are %+v; notices %+v", a.st.Layouts, a.st.Notices)
	}
}
