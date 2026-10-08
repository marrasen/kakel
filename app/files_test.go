package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/marrasen/kakel/screen"
	"github.com/marrasen/kakel/settings"

	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"

	"github.com/marrasen/kakel/internal/testhome"
)

func TestAReaderIsToldHowItsSaveWent(t *testing.T) {
	home := testhome.New(t)
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	a := newApp(w.Client(), screen.NewShells())
	a.setReader("p1", Reader{Path: "/x/notes.txt", Seq: 1})

	a.handle(SaveLines{Pane: "p1", Path: "~/kept.txt", Lines: []string{"one", "two"}})
	if got, err := os.ReadFile(filepath.Join(home, "kept.txt")); err != nil || string(got) != "one\ntwo\n" {
		t.Fatalf("saved %q, %v", got, err)
	}
	if r := a.st.Readers["p1"]; r.Saves != 1 || r.SaveErr != "" {
		t.Fatalf("saved, the reader reads %+v", r)
	}

	// Saved again under the same name, the file there is left alone.
	a.handle(SaveLines{Pane: "p1", Path: "~/kept.txt", Lines: []string{"three"}})
	if got, _ := os.ReadFile(filepath.Join(home, "kept.txt")); string(got) != "one\ntwo\n" {
		t.Fatalf("saved over a file that was there: it holds %q", got)
	}
	if r := a.st.Readers["p1"]; r.Saves != 2 || !strings.HasPrefix(r.SaveErr, "already there") {
		t.Fatalf("saved over a file that was there, the reader reads %+v", r)
	}
	// The name offered next is one nothing has, so saving again saves.
	if r := a.st.Readers["p1"]; r.SaveAs != "~/kept 2.txt" {
		t.Fatalf("after the refusal, the name offered is %q", r.SaveAs)
	}

	a.handle(SaveLines{Pane: "p1", Path: filepath.Join(home, "missing", "kept.txt"), Lines: []string{"one"}})
	if r := a.st.Readers["p1"]; r.Saves != 3 || r.SaveErr == "" {
		t.Fatalf("saved into a missing folder, the reader reads %+v", r)
	}
	if len(a.st.Notices) != 0 {
		t.Fatalf("the reader says how its save went, and notices say it again: %+v", a.st.Notices)
	}
}

// Files on a server with one favourite open at that folder, however
// they are asked for; with more than one, or none, at home.
func TestFilesOnAServerWithOneSavedFolderOpenThere(t *testing.T) {
	a, answering := dialApp(t)
	there := t.TempDir()
	h, _ := a.book.Lookup("srv")
	a.settings = mustSettings(t)
	if err := a.settings.PutFavourites([]settings.Favourite{{Machine: h.ID, Path: there}}); err != nil {
		t.Fatal(err)
	}
	a.showFavourites()
	a.handle(OpenOn{Machine: "srv"})
	waitFor(t, a, "a shell on the server", func() bool { answering(); return oneShell(a) })
	files := newFakeFiles(t)
	a.files = files
	a.handle(OpenFiles{})
	waitFor(t, a, "the files", func() bool {
		return len(a.st.Panes) == 2 && a.st.Panes[1].Kind == KindFileManager
	})
	if o := files.opened[0]; o.Dir != there || o.FS.ID() != serverFS+h.ID {
		t.Fatalf("the files opened at %q on %q, want the saved folder %q on the server", o.Dir, o.FS.ID(), there)
	}
}

// onServer is a path of this machine as the test server's SFTP spells
// it: the same on Linux, and /C:/Users/... on Windows.
func onServer(path string) string {
	if runtime.GOOS == "windows" {
		return "/" + filepath.ToSlash(path)
	}
	return path
}
