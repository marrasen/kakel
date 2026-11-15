package jobs

import (
	"io"
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
		t.Errorf("the job kept %v, want a refusal for each file", p.Kept)
	}
	for _, err := range p.Kept {
		if !vfs.Refused(err) {
			t.Errorf("kept %v, which is not a refusal", err)
		}
	}
}

// A copy whose bytes don't arrive still fails, on a server that
// doesn't store permissions as on any other.
func TestACopyThatCannotWriteStillFails(t *testing.T) {
	from := local(t)
	write(t, from.real, "one.txt", "the body")
	srv := sshtest.NewNoModes(t)
	srv.RefuseWrites()
	to := vfs.NewSFTP("plain", nil, srv.Client, nil)

	j := New(1).Start(t.Context(), Op{
		Kind: Copy, From: from.fs, At: from.at, Names: []string{"one.txt"},
		To: to, Into: "/",
	}, Options{})
	if err := ends(t, j); err == nil {
		t.Fatal("a copy whose bytes never arrived finished")
	}
	if _, err := to.Stat("/one.txt"); err == nil {
		t.Fatal("a copy that failed left a file at the name")
	}
}
