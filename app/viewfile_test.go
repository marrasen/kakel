package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/vfs"
)

// A file is read for the viewer, through a link to it too; a pipe is
// not, as it could make the read wait for ever; a large file is cut.
func TestTheViewerReadsFilesAlone(t *testing.T) {
	dir := t.TempDir()
	f := vfs.NewLocal()
	file := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(file, []byte("# Notes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if data, cut, why := readView(f, file); why != "" || cut || string(data) != "# Notes\n" {
		t.Fatalf("the file read %q, cut %v, %s", data, cut, why)
	}
	big := filepath.Join(dir, "big.log")
	if err := os.WriteFile(big, []byte(strings.Repeat("x", mostView+10)), 0o600); err != nil {
		t.Fatal(err)
	}
	if data, cut, why := readView(f, big); why != "" || !cut || len(data) != mostView {
		t.Fatalf("a large file read %d bytes, cut %v, %s", len(data), cut, why)
	}
	if runtime.GOOS == "windows" {
		return
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink("notes.md", link); err != nil {
		t.Fatal(err)
	}
	if data, _, why := readView(f, link); why != "" || string(data) != "# Notes\n" {
		t.Fatalf("through a link, the file read %q, %s", data, why)
	}
	loop := filepath.Join(dir, "loop")
	if err := os.Symlink("loop", loop); err != nil {
		t.Fatal(err)
	}
	if _, _, why := readView(f, loop); why == "" {
		t.Fatal("a link to itself was read")
	}
}

// A file shown goes to the window in front, once; one that cannot be
// read is said in a notice instead.
func TestAFileShownGoesToTheWindowInFront(t *testing.T) {
	a, one, _ := twoWindowApp(t)
	a.front(one)
	file := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(file, []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.viewFile(machines.Local, vfs.NewLocal(), file, 3)
	waitFor(t, a, "the file is read", func() bool { return len(a.st.Views) == 1 })
	v := a.st.Views[0]
	if v.Name != "main.go" || v.Line != 3 || string(v.Data) != "package main\n" {
		t.Fatalf("the viewer shows %+v", v)
	}
	if st := a.stateFor(one, a.st); len(st.Views) != 1 {
		t.Fatal("the window in front was not given the file")
	}
	for _, w := range a.liveWins() {
		if w != one && len(a.stateFor(w, a.st).Views) != 0 {
			t.Fatal("another window was given the file")
		}
	}
	a.viewFile(machines.Local, vfs.NewLocal(), filepath.Join(t.TempDir(), "gone.txt"), 0)
	waitFor(t, a, "the failure is said", func() bool { return len(a.st.Notices) > 0 })
	if n := a.st.Notices[len(a.st.Notices)-1]; !strings.HasPrefix(n.Title, "Couldn't show gone.txt") {
		t.Fatalf("the notice is %+v", n)
	}
}
