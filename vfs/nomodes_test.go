package vfs

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"testing"

	"github.com/marrasen/kakel/internal/sshtest"
	"github.com/pkg/sftp"
)

// A server that doesn't store permissions still takes a new file and a
// new directory. The file says the server refused its mode, and the
// bytes written to it all arrive.
func TestAServerWithoutPermissionsTakesFiles(t *testing.T) {
	srv := sshtest.NewNoModes(t)
	f := NewSFTP("plain", nil, srv.Client, nil)

	for _, create := range []struct {
		name string
		make func(string, fs.FileMode) (io.WriteCloser, error)
	}{{"Create", f.Create}, {"CreateNew", f.CreateNew}} {
		path := "/" + create.name + ".txt"
		w, err := create.make(path, 0o640)
		if err != nil {
			t.Fatalf("%s: %v", create.name, err)
		}
		if refused := ModeRefused(w); refused == nil || !Refused(refused) {
			t.Errorf("%s: the refused mode reads %v", create.name, refused)
		}
		if _, err := io.WriteString(w, "the body"); err != nil {
			t.Fatalf("%s: write: %v", create.name, err)
		}
		if err := w.Close(); err != nil {
			t.Fatalf("%s: close: %v", create.name, err)
		}
		if got := read(t, f, path); got != "the body" {
			t.Errorf("%s: the file holds %q", create.name, got)
		}
	}
	if err := f.Mkdir("/dir", 0o711); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if info, err := f.Stat("/dir"); err != nil || !info.IsDir() {
		t.Fatalf("the directory is %+v, %v", info, err)
	}
	if srv.Refused() == 0 {
		t.Fatal("nothing asked the server for permissions")
	}
}

// Only a server declining to set something counts as a refusal: a
// failure of any other kind stays one.
func TestWhatCountsAsRefused(t *testing.T) {
	for _, c := range []struct {
		err  error
		want bool
	}{
		{&sftp.StatusError{Code: 8}, true},
		{os.ErrPermission, true},
		{&fs.PathError{Op: "chmod", Path: "/x", Err: fs.ErrPermission}, true},
		{&sftp.StatusError{Code: 4}, false},
		{os.ErrNotExist, false},
		{errors.New("the connection ended"), false},
		{nil, false},
	} {
		if got := Refused(c.err); got != c.want {
			t.Errorf("Refused(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}
