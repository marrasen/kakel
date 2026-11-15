package app

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/kakel/internal/sshtest"
	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/vfs"

	"github.com/marrasen/gunim/filemanager"
)

// A file the file manager copies on a server that doesn't store
// permissions lands whole at its name, with its time, and the user is
// told the server kept its own permissions, once for the whole copy.
//
// The calls are the ones the file manager's copy makes: a part file
// made new, written, given the original's mode and time, and renamed
// onto the name.
func TestAServerWithoutPermissionsTakesACopy(t *testing.T) {
	srv := sshtest.NewNoModes(t)
	m := newFMFS(serverFS+"s1", vfs.NewSFTP("plain", nil, srv.Client, nil))
	told := make(chan keptNote, 4)
	m.told = func(k keptNote) { told <- k }
	var st filemanager.Stamper = m

	when := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	for _, name := range []string{"/a.txt", "/b.txt"} {
		part := name + ".part"
		w, err := m.Create(part)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, "the body"); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		if err := st.Chmod(part, 0o644); err != nil {
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
		if at, ok := srv.Time(name); !ok || !at.Equal(when) {
			t.Errorf("%s has the time %v, %v", name, at, ok)
		}
	}

	var k keptNote
	select {
	case k = <-told:
	case <-time.After(5 * time.Second):
		t.Fatal("the user was never told")
	}
	if k.Items != 2 || !k.Perms || k.Times || !vfs.Refused(k.First) {
		t.Fatalf("the user was told %+v", k)
	}
	if title, body := keptWords(k); !strings.Contains(title, "2") || body == "" {
		t.Fatalf("the notice reads %q, %q", title, body)
	}
	select {
	case k = <-told:
		t.Fatalf("the user was told twice: %+v", k)
	case <-time.After(2 * keptWait):
	}
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
// runs as a job that finishes, with the file there.
func TestATransferToAServerWithoutPermissionsFinishes(t *testing.T) {
	a, _ := agentApp(t)
	a.st.Saved = []remote.Host{{ID: "s1", Name: "plain", Address: "plain.example"}}
	a.st.Connected = []machines.ID{"s1"}
	srv := sshtest.NewNoModes(t)
	to := vfs.NewSFTP("plain", nil, srv.Client, nil)
	a.machines.At("s1").Files = to
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("the body"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := transferNow(t, a, filemanager.Transfer{FromFS: "", Paths: []string{path}, ToFS: serverFS + "s1", Into: "/"}); err != nil {
		t.Fatalf("the transfer: %v", err)
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
		t.Fatalf("the job kept %v", kept)
	}
}
