package app

import (
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marrasen/kakel/internal/sshtest"
	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/vfs"

	"github.com/marrasen/gunim/filemanager"
)

// copyOn copies a file onto name on m as the file manager's copy does:
// a part file made new, written, given mode and the time when, and
// renamed onto the name. hold is how long the part file stays open
// for writing, as for a big file.
func copyOn(t *testing.T, m *fmFS, name string, mode fs.FileMode, when time.Time, hold time.Duration) {
	t.Helper()
	var st filemanager.Stamper = m
	part := name + ".part"
	w, err := m.Create(part)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, "the body"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(hold)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := st.Chmod(part, mode); err != nil {
		t.Fatalf("setting the mode stopped the copy: %v", err)
	}
	if err := st.Chtimes(part, when); err != nil {
		t.Fatalf("setting the time stopped the copy: %v", err)
	}
	if err := m.Rename(part, name); err != nil {
		t.Fatal(err)
	}
	r, err := m.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(r)
	_ = r.Close()
	if string(got) != "the body" {
		t.Fatalf("%s holds %q", name, got)
	}
}

// shortKeptWait makes a copy count as over after a short quiet, for
// the test.
func shortKeptWait(t *testing.T) {
	was := keptWait
	keptWait = 50 * time.Millisecond
	t.Cleanup(func() { keptWait = was })
}

// A copy in the file manager on a server that doesn't store
// permissions lands whole at each name, with its time, and the user is
// told once, when the copy is over, with every file counted once. The
// copy takes many times keptWait: a file stays open for writing that
// long, and the window's operation runs on after the last file.
func TestAServerWithoutPermissionsTakesACopy(t *testing.T) {
	shortKeptWait(t)
	srv := sshtest.NewNoModes(t)
	m := newFMFS(serverFS+"s1", vfs.NewSFTP("plain", nil, srv.Client, nil))
	told := make(chan keptNote, 8)
	m.told = func(k keptNote) { told <- k }
	var busy atomic.Bool
	busy.Store(true)
	m.busy = busy.Load

	when := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	copyOn(t, m, "/a.txt", 0o644, when, 0)
	copyOn(t, m, "/big.bin", 0o644, when, 8*keptWait)
	copyOn(t, m, "/key", 0o600, when, 0)
	for _, name := range []string{"/a.txt", "/big.bin", "/key"} {
		if at, ok := srv.Time(name); !ok || !at.Equal(when) {
			t.Errorf("%s has the time %v, %v", name, at, ok)
		}
	}
	select {
	case k := <-told:
		t.Fatalf("the user was told while the operation still ran: %+v", k)
	case <-time.After(6 * keptWait):
	}
	busy.Store(false)

	var k keptNote
	select {
	case k = <-told:
	case <-time.After(5 * time.Second):
		t.Fatal("the user was never told")
	}
	if k.Items != 3 || !k.Perms || k.Times || k.Private != 1 || !vfs.Refused(k.First) {
		t.Fatalf("the user was told %+v, want 3 items, permissions only, 1 private", k)
	}
	if title, body := keptWords(k); !strings.Contains(title, "3") || !strings.Contains(body, "Others") {
		t.Fatalf("the notice reads %q, %q", title, body)
	}
	select {
	case k = <-told:
		t.Fatalf("the user was told twice: %+v", k)
	case <-time.After(4 * keptWait):
	}
}

// A server that refuses times as well as permissions has each file
// counted once, and the user is told of both.
func TestAServerWithoutTimesCountsEachFileOnce(t *testing.T) {
	shortKeptWait(t)
	srv := sshtest.NewNoModes(t)
	srv.RefuseTimes()
	m := newFMFS(serverFS+"s1", vfs.NewSFTP("plain", nil, srv.Client, nil))
	told := make(chan keptNote, 8)
	m.told = func(k keptNote) { told <- k }

	when := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	copyOn(t, m, "/a.txt", 0o644, when, 0)
	copyOn(t, m, "/b.txt", 0o644, when, 0)
	var k keptNote
	select {
	case k = <-told:
	case <-time.After(5 * time.Second):
		t.Fatal("the user was never told")
	}
	if k.Items != 2 || !k.Perms || !k.Times || k.Private != 0 {
		t.Fatalf("the user was told %+v, want 2 items, permissions and times", k)
	}
	if title, body := keptWords(k); !strings.Contains(title, "2") || strings.Contains(body, "Others") {
		t.Fatalf("the notice reads %q, %q", title, body)
	}
	select {
	case k = <-told:
		t.Fatalf("the user was told twice: %+v", k)
	case <-time.After(4 * keptWait):
	}
}

// A file manager pane on the server shows what it would not set, and
// kakel's own window shows nothing of it.
func TestWhatAServerKeptIsToldInItsPane(t *testing.T) {
	a, _ := agentApp(t)
	a.settings = mustSettings(t)
	a.files = newFakeFiles(t)
	a.handle(OpenFilesOn{})
	id := a.st.Panes[len(a.st.Panes)-1].ID
	// The pane says which folder it shows, and then is taken to show
	// one on the server.
	waitFor(t, a, "the pane to show a folder", func() bool {
		a.fmTitleMu.Lock()
		defer a.fmTitleMu.Unlock()
		_, ok := a.fmFolders[id]
		return ok
	})
	a.fmTitleMu.Lock()
	a.fmFolders[id] = [2]string{serverFS + "s1", "/"}
	a.fmTitleMu.Unlock()

	var logged syncBuilder
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	before := len(a.st.Notices)
	k := keptNote{Server: "plain", Items: 3, Perms: true}
	title, _ := keptWords(k)
	a.tellKept(serverFS+"s1", k)
	waitFor(t, a, "the pane to tell of it", func() bool { return strings.Contains(logged.String(), "files: "+title) })
	if len(a.st.Notices) != before {
		t.Fatalf("kakel's window told of it too: %+v", a.st.Notices[before:])
	}
}

// syncBuilder is a strings.Builder that many goroutines write.
type syncBuilder struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuilder) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuilder) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// With no file manager pane on the server, kakel's own window tells of
// what the server would not set.
func TestWhatAServerKeptIsToldWithNoPaneOnIt(t *testing.T) {
	a, _ := agentApp(t)
	before := len(a.st.Notices)
	a.tellKept(serverFS+"s1", keptNote{Server: "plain", Items: 3, Perms: true})
	if len(a.st.Notices) != before+1 {
		t.Fatalf("the notices are %+v", a.st.Notices[before:])
	}
}

// A file manager transfer to a server that doesn't store permissions
// runs as a job for each folder the items are in. Each finishes, with
// its file there, and the user is warned once for the whole transfer.
func TestATransferToAServerWithoutPermissionsFinishes(t *testing.T) {
	a, _ := agentApp(t)
	a.st.Saved = []remote.Host{{ID: "s1", Name: "plain", Address: "plain.example"}}
	a.st.Connected = []machines.ID{"s1"}
	srv := sshtest.NewNoModes(t)
	to := vfs.NewSFTP("plain", nil, srv.Client, nil)
	a.machines.At("s1").Files = to
	path := filepath.Join(t.TempDir(), "a.txt")
	other := filepath.Join(t.TempDir(), "b.txt")
	for _, p := range []string{path, other} {
		if err := os.WriteFile(p, []byte("the body"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	warned := func() []Notice {
		var w []Notice
		for _, n := range a.st.Notices {
			if strings.HasPrefix(n.Title, "Wrote ") {
				w = append(w, n)
			}
		}
		return w
	}
	if err := transferNow(t, a, filemanager.Transfer{FromFS: "", Paths: []string{path, other}, ToFS: serverFS + "s1", Into: "/"}); err != nil {
		t.Fatalf("the transfer: %v", err)
	}
	waitFor(t, a, "the warning", func() bool { return len(warned()) > 0 })
	waitFor(t, a, "the events to settle", func() bool { return len(a.events) == 0 })
	if w := warned(); len(w) != 1 || !strings.Contains(w[0].Title, "2") {
		t.Fatalf("a transfer from two folders warned %+v, want once, of 2 items", w)
	}
	r, err := to.Open("/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(r)
	_ = r.Close()
	if string(got) != "the body" {
		t.Fatalf("the server holds %q", got)
	}
	if kept := a.running[len(a.running)-1].job.Progress().Kept; len(kept) != 1 {
		t.Fatalf("the last job kept %v", kept)
	}
}

// A file open for writing holds back the warning by itself, with no
// operation said to run: a big file that takes many times keptWait to
// write is still part of the copy.
func TestAFileOpenForWritingHoldsTheWarning(t *testing.T) {
	shortKeptWait(t)
	srv := sshtest.NewNoModes(t)
	m := newFMFS(serverFS+"s1", vfs.NewSFTP("plain", nil, srv.Client, nil))
	told := make(chan keptNote, 8)
	m.told = func(k keptNote) { told <- k }

	when := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	copyOn(t, m, "/a.txt", 0o644, when, 0)
	copyOn(t, m, "/big.bin", 0o644, when, 8*keptWait)
	var k keptNote
	select {
	case k = <-told:
	case <-time.After(5 * time.Second):
		t.Fatal("the user was never told")
	}
	if k.Items != 2 {
		t.Fatalf("the user was told of %d items, want 2 at once", k.Items)
	}
	select {
	case k = <-told:
		t.Fatalf("the user was told twice: %+v", k)
	case <-time.After(4 * keptWait):
	}
}

// A file whose rename into place fails is not counted: the copy of it
// fails, and gunim takes the part file away.
func TestAFileThatFailsToLandIsNotCounted(t *testing.T) {
	shortKeptWait(t)
	srv := sshtest.NewNoModes(t)
	m := newFMFS(serverFS+"s1", vfs.NewSFTP("plain", nil, srv.Client, nil))
	told := make(chan keptNote, 8)
	m.told = func(k keptNote) { told <- k }
	var busy atomic.Bool
	busy.Store(true)
	m.busy = busy.Load

	when := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	copyOn(t, m, "/a.txt", 0o644, when, 0)
	srv.DenyRenames()
	var st filemanager.Stamper = m
	w, err := m.Create("/b.txt.part")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, "the body"); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := st.Chmod("/b.txt.part", 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.Rename("/b.txt.part", "/b.txt"); err == nil {
		t.Fatal("a rename the server denied worked")
	}
	if err := m.Remove("/b.txt.part"); err != nil {
		t.Fatal(err)
	}
	busy.Store(false)

	var k keptNote
	select {
	case k = <-told:
	case <-time.After(5 * time.Second):
		t.Fatal("the user was never told")
	}
	if k.Items != 1 || k.Private != 0 {
		t.Fatalf("the user was told %+v, want the one file that landed, and none private", k)
	}
}

// A mode of 0 is what a server that sent no permissions gives: it says
// nothing, and the file is not counted as private.
func TestAModeOfZeroIsNotPrivate(t *testing.T) {
	shortKeptWait(t)
	srv := sshtest.NewNoModes(t)
	m := newFMFS(serverFS+"s1", vfs.NewSFTP("plain", nil, srv.Client, nil))
	told := make(chan keptNote, 8)
	m.told = func(k keptNote) { told <- k }

	when := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	copyOn(t, m, "/a.txt", 0, when, 0)
	copyOn(t, m, "/key", 0o600, when, 0)
	var k keptNote
	select {
	case k = <-told:
	case <-time.After(5 * time.Second):
		t.Fatal("the user was never told")
	}
	if k.Items != 2 || k.Private != 1 {
		t.Fatalf("the user was told %+v, want 2 items, 1 private", k)
	}
}

// fakeRunner is a file manager window for fmBusy: one that runs n
// operations, or that takes too long to say.
type fakeRunner struct {
	n    atomic.Int32
	slow atomic.Bool
	done chan struct{}
}

func (f *fakeRunner) Running() int {
	if f.slow.Load() {
		select {
		case <-f.done:
		case <-time.After(3 * runningWait):
		}
		return 0
	}
	return int(f.n.Load())
}

func (f *fakeRunner) Done() <-chan struct{} { return f.done }

// A window that takes too long to say how many operations it runs
// counts as running one, unless it has closed.
func TestAWindowThatDoesNotAnswerIsBusy(t *testing.T) {
	was := runningWait
	runningWait = 20 * time.Millisecond
	t.Cleanup(func() { runningWait = was })
	f := &fakeRunner{done: make(chan struct{})}
	f.slow.Store(true)
	if !anyRunning([]fmRunner{f}) {
		t.Fatal("a window that did not answer counts as idle")
	}
	close(f.done)
	if anyRunning([]fmRunner{f}) {
		t.Fatal("a closed window counts as running")
	}
}

// The files of a server the app opens hold back the warning while a
// Files window runs an operation, or doesn't say whether it does, and
// tell it once it is done.
func TestTheAppsServerFilesWaitForItsWindows(t *testing.T) {
	shortKeptWait(t)
	was := runningWait
	runningWait = 20 * time.Millisecond
	t.Cleanup(func() { runningWait = was })
	a, _ := agentApp(t)
	f := &fakeRunner{done: make(chan struct{})}
	f.n.Store(1)
	a.fmRunners = func() []fmRunner { return []fmRunner{f} }
	srv := sshtest.NewNoModes(t)
	m := a.fmFor("s1", vfs.NewSFTP("plain", nil, srv.Client, nil))
	warned := func() []Notice {
		var w []Notice
		for _, n := range a.st.Notices {
			if strings.HasPrefix(n.Title, "Wrote ") {
				w = append(w, n)
			}
		}
		return w
	}
	pump := func(d time.Duration) {
		end := time.After(d)
		for {
			select {
			case fn := <-a.events:
				fn()
			case <-end:
				return
			}
		}
	}

	when := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	copyOn(t, m, "/a.txt", 0o644, when, 0)
	pump(6 * keptWait)
	if w := warned(); len(w) != 0 {
		t.Fatalf("warned while the window ran an operation: %+v", w)
	}
	// The window is too busy to answer, in the middle of the copy.
	f.slow.Store(true)
	pump(6 * keptWait)
	if w := warned(); len(w) != 0 {
		t.Fatalf("warned while the window did not answer: %+v", w)
	}
	f.slow.Store(false)
	f.n.Store(1)
	copyOn(t, m, "/b.txt", 0o644, when, 0)
	f.n.Store(0)
	waitFor(t, a, "the warning", func() bool { return len(warned()) > 0 })
	pump(4 * keptWait)
	if w := warned(); len(w) != 1 || !strings.Contains(w[0].Title, "2") {
		t.Fatalf("the copy warned %+v, want once, of 2 items", w)
	}
}

// A transfer a Files window asks for warns in that window, once, and
// not in kakel's own.
func TestATransferWarnsInItsWindow(t *testing.T) {
	a, _ := agentApp(t)
	a.settings = mustSettings(t)
	a.files = newFakeFiles(t)
	a.handle(OpenFilesOn{})
	id := a.st.Panes[len(a.st.Panes)-1].ID
	fp := a.fmPanes[id]
	if fp == nil || fp.w == nil {
		t.Fatal("no Files window opened")
	}
	a.st.Saved = []remote.Host{{ID: "s1", Name: "plain", Address: "plain.example"}}
	a.st.Connected = []machines.ID{"s1"}
	srv := sshtest.NewNoModes(t)
	a.machines.At("s1").Files = vfs.NewSFTP("plain", nil, srv.Client, nil)
	here := t.TempDir()
	var paths []string
	for _, name := range []string{"a.txt", "b.txt"} {
		p := filepath.Join(here, name)
		if err := os.WriteFile(p, []byte("the body"), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}

	var logged syncBuilder
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	before := len(a.st.Notices)
	done := make(chan error, 1)
	go func() {
		done <- a.transferFiles(t.Context(), fp.w, filemanager.Transfer{FromFS: "", Paths: paths, ToFS: serverFS + "s1", Into: "/"}, nil)
	}()
	var err error
	waitFor(t, a, "the transfer", func() bool {
		select {
		case err = <-done:
			return true
		default:
			a.showJobs()
			return false
		}
	})
	if err != nil {
		t.Fatalf("the transfer: %v", err)
	}
	title, _ := keptWords(keptNote{Server: "plain", Items: 2, Perms: true})
	waitFor(t, a, "the window to warn", func() bool { return strings.Contains(logged.String(), "files: "+title) })
	waitFor(t, a, "the events to settle", func() bool { return len(a.events) == 0 })
	time.Sleep(100 * time.Millisecond)
	if n := strings.Count(logged.String(), "files: Wrote "); n != 1 {
		t.Fatalf("the window warned %d times:\n%s", n, logged.String())
	}
	for _, n := range a.st.Notices[before:] {
		if strings.HasPrefix(n.Title, "Wrote ") {
			t.Fatalf("kakel's own window warned too: %+v", n)
		}
	}
}
