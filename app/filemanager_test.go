package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/single"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/filemanager"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
)

// fakeFiles stands in for the file manager's hub: it records the
// options of each pane it is asked for, and runs the pane on a hub of its
// own, with its settings in the test's folder.
type fakeFiles struct {
	t         *testing.T
	hub       *filemanager.Hub
	opened    []filemanager.Options
	refreshed int
}

func newFakeFiles(t *testing.T) *fakeFiles {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &fakeFiles{t: t, hub: filemanager.NewHub(ctx, nil)}
}

func (f *fakeFiles) NewPane(o filemanager.Options, host filemanager.PaneHost) (*filemanager.Window, error) {
	f.opened = append(f.opened, o)
	o.PrefsPath, o.Poll = filepath.Join(f.t.TempDir(), "files.json"), -1
	w, err := f.hub.NewPane(o, host)
	if err == nil {
		// Stopped before its folder is taken away, which Windows refuses
		// while it has a file there open.
		f.t.Cleanup(func() {
			w.Stop()
			select {
			case <-w.Done():
			case <-time.After(5 * time.Second):
			}
		})
	}
	return w, err
}

func (f *fakeFiles) Refresh() { f.refreshed++ }

// Files open in a file manager pane, beside the one in front, and the
// servers are the file manager's places, under Machines, with how they
// are doing.
func TestFilesOpenInAFileManagerPane(t *testing.T) {
	a, _ := agentApp(t)
	a.settings = mustSettings(t)
	files := newFakeFiles(t)
	a.files = files
	a.st.Saved = []remote.Host{{ID: "s1", Name: "web", Address: "web.example"}}

	panes := len(a.st.Panes)
	a.handle(OpenFilesOn{})
	if len(files.opened) != 1 || len(a.st.Panes) != panes+1 || files.opened[0].FS == nil {
		t.Fatalf("files opened %d file managers and %d panes", len(files.opened), len(a.st.Panes)-panes)
	}
	first := a.st.Panes[len(a.st.Panes)-1]
	if first.Kind != KindFileManager || a.st.Focus != first.ID || a.fmPanes[first.ID] == nil {
		t.Fatalf("the pane opened is %+v, with the keyboard on %q", first, a.st.Focus)
	}
	a.handle(OpenFilesOn{})
	second := a.st.Panes[len(a.st.Panes)-1]
	if a.groupOf[second.ID] != a.groupOf[first.ID] {
		t.Fatal("a second file manager pane didn't open beside the one in front")
	}
	places, _ := files.opened[0].Places()
	i := slices.IndexFunc(places, func(p filemanager.Place) bool { return p.Group == "Machines" })
	if i < 0 || places[i].Name != "web" || places[i].Note != "Not connected" || places[i].FS != serverFS+"s1" {
		t.Fatalf("the places are %+v", places)
	}
	// Closed, its file manager stops, and the pane goes.
	a.handle(ClosePane{Pane: second.ID})
	fw := a.fmPanes[second.ID].w
	select {
	case <-fw.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("closing the pane left its file manager running")
	}
	waitFor(t, a, "the pane goes", func() bool { return !a.has(second.ID) })
	if _, ok := a.fmPanes[second.ID]; ok {
		t.Fatal("a closed file manager pane is still kept")
	}
}

// -files opens a folder in a window holding a file manager pane, as
// Windows asks once kakel opens folders: in the kakel running, handed
// over, and a drive's root read through the quote Windows' "%V\." keeps
// whole.
func TestAFolderOpenedAnywhereOpensInTheFileManager(t *testing.T) {
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"-files", "/home/me/."}, filepath.FromSlash("/home/me")},
		{[]string{"-files", ""}, ""},
		{[]string{"-files", `/mnt/"`}, filepath.FromSlash("/mnt")},
	} {
		o, err := ParseOptions(c.args)
		if err != nil {
			t.Fatal(err)
		}
		dir, set := o.OpensFolder()
		if !set || dir != c.want || o.StartsHidden() || o.StartsInTray() {
			t.Errorf("%q opens %q (%v), hidden %v, in the tray %v", c.args, dir, set, o.StartsHidden(), o.StartsInTray())
		}
	}
	if _, set := (Options{}).OpensFolder(); set {
		t.Fatal("no -files opens a folder")
	}

	a, _ := agentApp(t)
	a.settings = mustSettings(t)
	files := newFakeFiles(t)
	a.files = files
	var windows int
	a.openWindow = func(*gunim.Window, geom.Point, geom.Size) (gunim.Client, *gunim.Window, error) {
		windows++
		w := gunimtest.New(t, geom.Sz(400, 300), nil)
		return w.Client(), w, nil
	}
	a.handover(single.Handover{Args: []string{"-files", "/srv/data/."}})
	waitFor(t, a, "a window opens", func() bool { return windows == 1 && len(files.opened) == 1 })
	p := a.st.Panes[len(a.st.Panes)-1]
	if files.opened[0].Dir != filepath.FromSlash("/srv/data") || p.Kind != KindFileManager || a.ownerOf(p.ID) != a.cur || len(a.panesIn(a.cur)) != 1 {
		t.Fatalf("handed a folder, kakel opened %+v in a window of %d panes", files.opened, len(a.panesIn(a.cur)))
	}
}

// View in Reader on a file in a file manager pane opens the file in a
// reader beside the pane.
func TestAFileManagerPaneOpensAFileInAReader(t *testing.T) {
	a, _ := agentApp(t)
	a.settings = mustSettings(t)
	a.files = newFakeFiles(t)
	if err := a.filesOn(machines.Local, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	pane := a.st.Focus
	file := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(file, []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.fileAction(a.fmPanes[pane].w, "", []string{file}, "view")
	waitFor(t, a, "a reader opens", func() bool { return a.kindOfPane(a.st.Focus) == KindReader })
	if a.groupOf[a.st.Focus] != a.groupOf[pane] {
		t.Fatal("the reader didn't open beside the file manager pane")
	}
}

// A file manager pane moved to another window shows there once the
// windows are told, and the intents of its views go to it, not to
// kakel.
func TestAFileManagerPaneFollowsItsPaneToAnotherWindow(t *testing.T) {
	a, one, two := twoWindowApp(t)
	a.files = newFakeFiles(t)
	a.front(one)
	if err := a.filesOn(machines.Local, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	id := a.st.Focus
	a.publish()
	if fp := a.fmPanes[id]; fp == nil || fp.win != one {
		t.Fatal("the file manager pane isn't shown in its window")
	}
	a.moveToWindow(id, two)
	a.publish()
	if a.fmPanes[id].win != two {
		t.Fatal("the file manager didn't follow its pane to the other window")
	}
	from := gunim.ID(FilePaneViews(id) + "/browser")
	if !a.toFilePane(gunim.Envelope{From: from, Intent: filemanager.Command{Name: filemanager.CmdRefresh}}) {
		t.Fatal("an intent of the file manager's views went to kakel")
	}
	if a.toFilePane(gunim.Envelope{From: "window", Intent: NewTerminal{}}) {
		t.Fatal("an intent of kakel's window went to the file manager")
	}
}

// New Tab, as the tab bar's + asks, opens a file manager at the folder
// of the file manager in front, in a tab of its own, and a terminal
// where a terminal is in front.
func TestNewTabIsLikeTheTabInFront(t *testing.T) {
	a, _ := agentApp(t)
	a.settings = mustSettings(t)
	files := newFakeFiles(t)
	a.files = files
	term := a.st.Focus
	dir := t.TempDir()
	if err := a.filesOn(machines.Local, dir); err != nil {
		t.Fatal(err)
	}
	first := a.st.Focus
	a.handle(NewTab{})
	waitFor(t, a, "a second file manager opens", func() bool { return len(a.fmPanes) == 2 })
	second := a.st.Focus
	if second == first || a.kindOfPane(second) != KindFileManager || a.groupOf[second] == a.groupOf[first] {
		t.Fatalf("New Tab opened %q, beside %q", second, first)
	}
	if got := files.opened[len(files.opened)-1].Dir; got != dir {
		t.Fatalf("the new file manager opened at %q, want %q", got, dir)
	}
	a.focus(term)
	panes := len(a.st.Panes)
	a.handle(NewTab{})
	if len(a.st.Panes) != panes+1 || a.kindOfPane(a.st.Focus) != KindTerminal {
		t.Fatal("New Tab on a terminal didn't open a terminal")
	}
}
