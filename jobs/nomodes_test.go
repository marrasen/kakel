package jobs

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/marrasen/kakel/internal/sshtest"
	"github.com/marrasen/kakel/vfs"
)

// A copy to a server that doesn't store permissions finishes, with
// every file whole, and says which permissions the server would not
// take.
func TestACopyToAServerWithoutPermissionsFinishes(t *testing.T) {
	from := local(t)
	write(t, from.real, "one.txt", "the first")
	write(t, from.real, "dir/two.txt", "the second")
	unix := runtime.GOOS != "windows"
	if unix {
		// A private file, and a folder the job makes wide and then
		// narrows, which the server refuses too.
		if err := os.Chmod(filepath.Join(from.real, "one.txt"), 0o600); err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(from.real, "dir")
		if err := os.Chmod(dir, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	}
	srv := sshtest.NewNoModes(t)
	to := vfs.NewSFTP("plain", nil, srv.Client, nil)

	j := New(1).Start(t.Context(), Op{
		Kind: Copy, From: from.fs, At: from.at, Names: []string{"one.txt", "dir"},
		To: to, Into: "/",
	}, Options{})
	if err := ends(t, j); err != nil {
		t.Fatalf("the job: %v", err)
	}
	for path, want := range map[string]string{"/one.txt": "the first", "/dir/two.txt": "the second"} {
		r, err := to.Open(path)
		if err != nil {
			t.Fatalf("open %s: %v", path, err)
		}
		got, _ := io.ReadAll(r)
		_ = r.Close()
		if string(got) != want {
			t.Errorf("%s holds %q", path, got)
		}
	}
	names, err := to.ReadDir("/")
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 {
		t.Errorf("the server holds %v, and no part file should be left", names)
	}
	p := j.Progress()
	if p.FilesDone != 2 {
		t.Errorf("%d files done", p.FilesDone)
	}
	if len(p.Kept) != 2 {
		t.Errorf("the job kept %v, want a refusal for each file and none for the folder", p.Kept)
	}
	if unix && p.KeptPrivate != 1 {
		t.Errorf("%d private files kept the server's permissions, want 1", p.KeptPrivate)
	}
	for _, err := range p.Kept {
		if !vfs.Refused(err) {
			t.Errorf("kept %v, which is not a refusal", err)
		}
	}
}

// A copy whose bytes don't arrive still fails, on a server that
// doesn't store permissions as on any other, and leaves no part file.
// A write the server answers with "permission denied" fails too: only
// a refused chmod is let through.
func TestACopyThatCannotWriteStillFails(t *testing.T) {
	for name, refuse := range map[string]func(*sshtest.NoModes){
		"a full disk":       (*sshtest.NoModes).RefuseWrites,
		"permission denied": (*sshtest.NoModes).DenyWrites,
		"a rename denied":   (*sshtest.NoModes).DenyRenames,
	} {
		t.Run(name, func(t *testing.T) {
			from := local(t)
			write(t, from.real, "one.txt", "the body")
			srv := sshtest.NewNoModes(t)
			refuse(srv)
			to := vfs.NewSFTP("plain", nil, srv.Client, nil)

			j := New(1).Start(t.Context(), Op{
				Kind: Copy, From: from.fs, At: from.at, Names: []string{"one.txt"},
				To: to, Into: "/",
			}, Options{})
			if err := ends(t, j); err == nil {
				t.Fatal("a copy whose bytes never arrived finished")
			}
			names, err := to.ReadDir("/")
			if err != nil {
				t.Fatal(err)
			}
			if len(names) != 0 {
				t.Fatalf("a copy that failed left %v", names)
			}
			if kept := j.Progress().Kept; len(kept) != 0 {
				t.Fatalf("a file that was never put in place is counted: %v", kept)
			}
		})
	}
}

// A copy over a file already there is counted as private by the mode
// of the file it copies, not the one that was there.
func TestACopyOverAFileCountsTheSourcesMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a Windows file has no mode private to its owner")
	}
	from := local(t)
	write(t, from.real, "key", "the key")
	if err := os.Chmod(filepath.Join(from.real, "key"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := sshtest.NewNoModes(t)
	to := vfs.NewSFTP("plain", nil, srv.Client, nil)
	w, err := to.Create("/key", 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if e, err := to.Stat("/key"); err != nil || vfs.OwnerOnly(e.Mode) {
		t.Fatalf("the file there reads %+v, %v; the test wants one that isn't private", e, err)
	}

	j := New(1).Start(t.Context(), Op{
		Kind: Copy, From: from.fs, At: from.at, Names: []string{"key"},
		To: to, Into: "/",
	}, Options{Ask: Always{What: Replace}})
	if err := ends(t, j); err != nil {
		t.Fatalf("the job: %v", err)
	}
	if p := j.Progress(); len(p.Kept) != 1 || p.KeptPrivate != 1 {
		t.Fatalf("the job kept %v with %d private, want the private key counted", p.Kept, p.KeptPrivate)
	}
}
