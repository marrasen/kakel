package jobs

import (
	"io/fs"
	"testing"

	"github.com/marrasen/kakel/vfs"
)

// sepFS is a file system that is a Windows one where sep is '\'.
type sepFS struct {
	vfs.FS
	sep byte
}

func (s sepFS) Sep() byte { return s.sep }

// A copy from a Windows machine to one that is not gets modes of its
// own: a folder 0755, though Windows read it as 0555 or 0777, and a
// file 0644, or 0444 where it was read-only. Anywhere else, the modes
// are the source's.
func TestACopyFromWindowsGetsModesOfItsOwn(t *testing.T) {
	linux := sepFS{sep: '/'}
	local := sepFS{sep: '\\'}
	dir := func(m fs.FileMode) vfs.Entry { return vfs.Entry{Name: "d", Mode: fs.ModeDir | m} }
	file := func(m fs.FileMode) vfs.Entry { return vfs.Entry{Name: "f", Mode: m} }
	cases := []struct {
		name     string
		op       Op
		e        vfs.Entry
		want     fs.FileMode
		windowsy bool
	}{
		{"a read-only folder from this Windows computer", Op{From: local, At: `C:\Users\me`, To: linux, Into: "/home/me"}, dir(0o555), fs.ModeDir | 0o755, true},
		{"a folder from this Windows computer", Op{From: local, At: `C:\Users\me`, To: linux, Into: "/home/me"}, dir(0o777), fs.ModeDir | 0o755, true},
		{"a file from a Windows machine over SFTP", Op{From: linux, At: "/C:/Users/me", To: linux, Into: "/home/me"}, file(0o666), 0o644, true},
		{"a read-only file from a Windows machine", Op{From: linux, At: "/D:/work", To: linux, Into: "/srv"}, file(0o444), 0o444, true},
		{"a folder between Linux machines", Op{From: linux, At: "/home/a", To: linux, Into: "/home/b"}, dir(0o555), fs.ModeDir | 0o555, false},
		{"a file between Windows machines", Op{From: local, At: `C:\a`, To: linux, Into: "/C:/b"}, file(0o666), 0o666, false},
		{"a file from Linux to Windows", Op{From: linux, At: "/home/a", To: local, Into: `C:\b`}, file(0o600), 0o600, false},
	}
	for _, c := range cases {
		j := &Job{op: c.op}
		if got := j.fromWindows(); got != c.windowsy {
			t.Errorf("%s: from Windows %v, want %v", c.name, got, c.windowsy)
		}
		if got := j.modeFor(c.e); got != c.want {
			t.Errorf("%s: copied as %v, want %v", c.name, got, c.want)
		}
	}
}
