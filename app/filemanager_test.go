package app

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/marrasen/kakel/single"

	"github.com/marrasen/kakel/remote"

	"github.com/marrasen/gunim/filemanager"
)

// fakeFiles stands in for the file manager's hub: it records what it is
// asked to open, and opens nothing.
type fakeFiles struct {
	opened    []filemanager.Options
	refreshed int
}

var errNotOpened = errors.New("not opened in a test")

func (f *fakeFiles) Open(o filemanager.Options) (*filemanager.Window, error) {
	f.opened = append(f.opened, o)
	return nil, errNotOpened
}

func (f *fakeFiles) Refresh() { f.refreshed++ }

// Files open where the user last chose: a window once one was asked
// for, a pane once a pane was, and the servers are the file manager's
// places, under Servers, with how they are doing.
func TestFilesOpenWhereLastChosen(t *testing.T) {
	a, _ := agentApp(t)
	a.settings = mustSettings(t)
	files := &fakeFiles{}
	a.files = files
	a.st.Saved = []remote.Host{{ID: "s1", Name: "web", Address: "web.example"}}

	panes := len(a.st.Panes)
	a.handle(OpenFilesOn{})
	if len(files.opened) != 0 || len(a.st.Panes) != panes+1 {
		t.Fatalf("by default, files opened %d windows and %d panes", len(files.opened), len(a.st.Panes)-panes)
	}
	a.handle(OpenFileManager{})
	if len(files.opened) != 1 || !a.settings.FilesInWindow() || files.opened[0].FS == nil {
		t.Fatalf("asked for a window, opened %+v, kept %v", files.opened, a.settings.FilesInWindow())
	}
	a.handle(OpenFilesOn{})
	if len(files.opened) != 2 {
		t.Fatal("once a window was chosen, files didn't open in one")
	}
	places, _ := files.opened[0].Places()
	i := slices.IndexFunc(places, func(p filemanager.Place) bool { return p.Group == "Servers" })
	if i < 0 || places[i].Name != "web" || places[i].Note != "Not connected" || places[i].FS != serverFS+"s1" {
		t.Fatalf("the places are %+v", places)
	}
	a.handle(FilesInPane{})
	if a.settings.FilesInWindow() {
		t.Fatal("a pane chosen, files still open in windows")
	}
}

// -files opens a folder in a file manager window, as Windows asks once
// kakel opens folders: in the kakel running, handed over, whatever files
// opened in last, and a drive's root read through the quote Windows'
// "%V\." keeps whole.
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
		if !set || dir != c.want || !o.StartsHidden() || o.StartsInTray() {
			t.Errorf("%q opens %q (%v), hidden %v, in the tray %v", c.args, dir, set, o.StartsHidden(), o.StartsInTray())
		}
	}
	if _, set := (Options{}).OpensFolder(); set {
		t.Fatal("no -files opens a folder")
	}

	a, _ := agentApp(t)
	a.settings = mustSettings(t)
	files := &fakeFiles{}
	a.files = files
	panes := len(a.st.Panes)
	a.handover(single.Handover{Args: []string{"-files", "/srv/data/."}})
	if len(files.opened) != 1 || files.opened[0].Dir != filepath.FromSlash("/srv/data") || len(a.st.Panes) != panes {
		t.Fatalf("handed a folder, kakel opened %+v and %d panes", files.opened, len(a.st.Panes)-panes)
	}
}
