package app

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gunim/driver"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
	"github.com/pkg/sftp"

	"github.com/marrasen/kakel/guest"
)

// shareWith shares window one as the dialog does, with files and a
// folder, and returns the share once its link can be opened.
func (r *linkRig) shareWith(files guest.FilesMay, folder string) WindowLink {
	r.t.Helper()
	r.do(ShareWindow{Window: r.one.id, Name: "Anna", CanType: true, Relay: r.url, Files: files, Folder: folder})
	r.until("the link", func() bool {
		all := r.a.windowLinks()
		return len(all) > 0 && all[len(all)-1].Stands == LinkWaiting
	})
	all := r.a.windowLinks()
	return all[len(all)-1]
}

// files opens the share's folder as the guest.
func (v *visitor) files() *sftp.Client {
	v.r.t.Helper()
	c, err := v.s.Files(v.r.t.Context())
	if err != nil {
		v.r.t.Fatal(err)
	}
	v.r.t.Cleanup(func() { _ = c.Close() })
	return c
}

// upload writes a file as the guest.
func upload(c *sftp.Client, name, text string) error {
	w, err := c.Create(name)
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(text)); err != nil {
		_ = w.Close()
		return err
	}
	return w.Close()
}

// showFolder has the Files pane p3 show dir, as its file manager says.
func (r *linkRig) showFolder(dir string) {
	r.a.fmTitleMu.Lock()
	defer r.a.fmTitleMu.Unlock()
	r.a.fmFolders = map[string][2]string{"p3": {"", dir}}
}

// The folder a Files pane shows is offered for the next share of its
// window, and what the guest is let do there it can do, and no more, as
// that changes.
func TestAGuestReachesTheSharedFolder(t *testing.T) {
	r := newLinkRig(t)
	top := t.TempDir()
	dir := filepath.Join(top, "Reports")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plan.txt"), []byte("THE CONTENTS"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := r.a.linkSetup().Folders; got[r.one.id] != "" {
		t.Fatalf("with no folder shown, the next share starts with %v", got)
	}
	r.showFolder(dir)
	if got := r.a.linkSetup().Folders; got[r.one.id] != dir || got[r.two.id] != "" {
		t.Fatalf("the next shares start with %v", got)
	}

	l := r.shareWith(guest.FilesLook, dir)
	if l.Files != guest.FilesLook || l.Folder != dir {
		t.Errorf("the share reads %+v", l)
	}
	v := r.visit(l.Link)
	v.in(l.ID, true)
	v.state("the folder", func(st guest.State) bool { return st.Files == guest.FilesLook && st.Folder == "Reports" })
	if strings.Contains(v.everything(), top) {
		t.Error("the guest was told the folder's path")
	}
	c := v.files()
	f, err := c.Open("/plan.txt")
	if err != nil {
		t.Fatal(err)
	}
	if b, err := io.ReadAll(f); err != nil || string(b) != "THE CONTENTS" {
		t.Errorf("read %q, %v", b, err)
	}
	_ = f.Close()
	if err := upload(c, "/new.txt", "NEW CONTENTS"); !errors.Is(err, fs.ErrPermission) {
		t.Errorf("uploaded with Look: %v", err)
	}

	r.do(SetLinkFiles{ID: l.ID, Files: guest.FilesChange})
	v.state("change", func(st guest.State) bool { return st.Files == guest.FilesChange })
	for i := range 3 {
		if err := upload(c, "/up"+itoa(i)+".txt", "NEW CONTENTS"); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Mkdir("/made"); err != nil {
		t.Fatal(err)
	}
	if got := r.only(); got.Files != guest.FilesChange {
		t.Errorf("the share reads %+v", got)
	}

	r.do(SetLinkFiles{ID: l.ID, Files: guest.FilesNone})
	v.state("none", func(st guest.State) bool { return st.Files == guest.FilesNone && st.Folder == "" })
	if _, err := c.ReadDir("/"); !errors.Is(err, fs.ErrPermission) {
		t.Errorf("listed with None: %v", err)
	}

	r.a.linkByID(l.ID).filesLog.flush()
	logged := r.logged.String()
	for _, want := range []string{`downloaded "Reports/plan.txt"`, `uploaded 3 files to "Reports"`, `created the folder "Reports/made"`, "may change the files", "may use no files"} {
		if !strings.Contains(logged, want) {
			t.Errorf("the Window Log has no %q", want)
		}
	}
	if strings.Contains(logged, "CONTENTS") {
		t.Error("the Window Log has what is in a file")
	}
}

// With no folder, files can't be turned on, and the folder chosen is
// shared from then on.
func TestFilesWaitForAFolder(t *testing.T) {
	r := newLinkRig(t)
	gw := gunimtest.New(t, geom.Sz(400, 300), nil)
	r.one.c = gw.Client()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	asked := make(chan driver.ChooseOptions, 4)
	gw.Offscreen().SetChooser(func(o driver.ChooseOptions) ([]string, error) {
		asked <- o
		return []string{dir}, nil
	})

	l := r.shareWith(guest.FilesLook, "")
	if l.Files != guest.FilesNone || l.Folder != "" {
		t.Fatalf("with no folder, the share reads %+v", l)
	}
	v := r.visit(l.Link)
	v.in(l.ID, true)
	if _, err := v.s.Files(t.Context()); !errors.Is(err, guest.ErrNoFiles) {
		t.Errorf("with no folder, the guest opened files: %v", err)
	}
	notices := len(r.a.st.Notices)
	r.do(SetLinkFiles{ID: l.ID, Files: guest.FilesLook})
	if got := r.only(); got.Files != guest.FilesNone || len(r.a.st.Notices) == notices {
		t.Errorf("with no folder, files were turned on: %+v", got)
	}

	r.do(ChooseLinkFolder{Window: r.one.id, ID: l.ID})
	r.until("the folder", func() bool { return r.only().Folder == dir })
	if o := <-asked; !o.Folders {
		t.Errorf("the dialog asked %+v", o)
	}
	r.do(SetLinkFiles{ID: l.ID, Files: guest.FilesLook})
	v.state("the folder", func(st guest.State) bool { return st.Files == guest.FilesLook && st.Folder == filepath.Base(dir) })
	c := v.files()
	if infos, err := c.ReadDir("/"); err != nil || len(infos) != 1 {
		t.Errorf("listed %v, %v", infos, err)
	}

	// Another folder: what was open in the one before is closed, and the
	// guest sees the new one.
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	open, err := c.Open("/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	gw.Offscreen().SetChooser(func(driver.ChooseOptions) ([]string, error) { return []string{other}, nil })
	r.do(ChooseLinkFolder{Window: r.one.id, ID: l.ID})
	r.until("the other folder", func() bool { return r.only().Folder == other })
	if _, err := io.ReadAll(open); err == nil {
		t.Error("a file in the folder before was read")
	}
	infos, err := c.ReadDir("/")
	if err != nil || len(infos) != 1 || infos[0].Name() != "b.txt" {
		t.Errorf("in the other folder, listed %v, %v", infos, err)
	}

	// For the next share, the folder chosen is offered.
	r.do(ChooseLinkFolder{Window: r.one.id})
	r.until("the next share's folder", func() bool { return r.a.linkSetup().Folders[r.one.id] == other })
}

// A folder that can't be shared shares no files, and says so.
func TestAFolderThatCantBeShared(t *testing.T) {
	r := newLinkRig(t)
	gone := filepath.Join(t.TempDir(), "gone")
	l := r.shareWith(guest.FilesChange, gone)
	if got := r.only(); got.Files != guest.FilesNone {
		t.Errorf("the share reads %+v", got)
	}
	if !slices.ContainsFunc(r.a.st.Notices, func(n Notice) bool { return n.Title == couldntShareFolder }) {
		t.Errorf("nothing was said: %+v", r.a.st.Notices)
	}
	_ = l
}

// What a guest does in the folder is run together in the Window Log, a
// line for each run of the same in one folder.
func TestTheFilesLogRunsThingsTogether(t *testing.T) {
	var lines []string
	f := &filesLog{write: func(s string) { lines = append(lines, s) }}
	f.setWho("Bob")
	f.setFolder("Reports")
	for _, e := range []guest.FileEvent{
		{Op: guest.FileUploaded, Path: "/a.txt", Bytes: 5},
		{Op: guest.FileUploaded, Path: "/b.txt", Bytes: 5},
		{Op: guest.FileUploaded, Path: "/sub/c.txt", Bytes: 5},
		{Op: guest.FileRenamed, Path: "/sub/c.txt", To: "/sub/d.txt"},
		{Op: guest.FileRemoved, Path: "/a.txt"},
		{Op: guest.FileRemoved, Path: "/b.txt"},
		{Op: guest.FileDownloaded, Path: "/x\n\x1b[31m.txt", Bytes: 1},
	} {
		f.add(e)
	}
	f.flush()
	want := []string{
		`the guest "Bob" uploaded 2 files to "Reports" in a shared window`,
		`the guest "Bob" uploaded "Reports/sub/c.txt" (5 B) to a shared window`,
		`the guest "Bob" renamed "Reports/sub/c.txt" to "Reports/sub/d.txt" in a shared window`,
		`the guest "Bob" removed 2 items from "Reports" in a shared window`,
		`the guest "Bob" downloaded "Reports/x\n\x1b[31m.txt" (1 B) from a shared window`,
	}
	if !slices.Equal(lines, want) {
		t.Errorf("the lines are\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	// Quiet for a moment, a run is written by itself.
	lines = nil
	f.add(guest.FileEvent{Op: guest.FolderCreated, Path: "/new"})
	time.Sleep(filesQuiet + 500*time.Millisecond)
	f.mu.Lock()
	n := len(lines)
	f.mu.Unlock()
	if n != 1 {
		t.Errorf("after a quiet moment, %d lines", n)
	}
}

// A guest that does much in a minute has its lines cut off there, and
// what was not written counted in one line, so the Window Log keeps what
// came before.
func TestTheFilesLogIsBounded(t *testing.T) {
	var lines []string
	now := time.Unix(1e9, 0)
	f := &filesLog{write: func(s string) { lines = append(lines, s) }, now: func() time.Time { return now }}
	f.setWho("Bob")
	f.setFolder("Reports")
	// Each in another folder, so none runs together.
	for i := range 500 {
		f.add(guest.FileEvent{Op: guest.FolderCreated, Path: "/d" + itoa(i) + "/x"})
	}
	f.flush()
	if len(lines) != filesLines {
		t.Fatalf("%d lines in a minute", len(lines))
	}
	now = now.Add(filesEvery)
	f.add(guest.FileEvent{Op: guest.FileRemoved, Path: "/gone"})
	f.end()
	want := `the guest "Bob" did 480 more things in a shared window`
	if len(lines) != filesLines+2 || lines[filesLines] != want || !strings.Contains(lines[filesLines+1], "removed") {
		t.Errorf("after the minute: %q", lines[filesLines:])
	}
}
