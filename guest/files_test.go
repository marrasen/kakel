package guest

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// folders is a folder to share, beside one that is not, each with a file.
type folders struct {
	share, outside string
}

func newFolders(t *testing.T) folders {
	t.Helper()
	top := t.TempDir()
	f := folders{share: filepath.Join(top, "share"), outside: filepath.Join(top, "outside")}
	put(t, filepath.Join(f.share, "a.txt"), "inside")
	put(t, filepath.Join(f.share, "sub", "b.txt"), "below")
	put(t, filepath.Join(f.outside, "secret.txt"), "SECRET")
	return f
}

func put(t *testing.T, name, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// link makes a symbolic link, or skips a test on a machine that makes
// none for this user.
func link(t *testing.T, to, name string) {
	t.Helper()
	if err := os.Symlink(to, name); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("no links here: %v", err)
		}
		t.Fatal(err)
	}
}

// filesOn is a guest let in, the host sharing dir for may, and the
// guest's client on it.
func filesOn(t *testing.T, dir string, may FilesMay) (*rig, *Session, *sftp.Client) {
	t.Helper()
	r := newRig(t, false)
	s := r.in(key(t))
	if err := r.host.SetFiles(dir, may); err != nil {
		t.Fatal(err)
	}
	c, err := s.Files(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return r, s, c
}

func readAll(c *sftp.Client, name string) (string, error) {
	f, err := c.Open(name)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(f)
	return string(b), err
}

// refused reports whether err is a request refused for what the guest
// may do, or one there is no such thing as.
func refused(err error) bool {
	var se *sftp.StatusError
	return errors.Is(err, fs.ErrPermission) ||
		errors.As(err, &se) && (se.FxCode() == sftp.ErrSSHFxOpUnsupported || se.FxCode() == sftp.ErrSSHFxPermissionDenied)
}

// No name the guest gives leads above the folder: not "..", not the host's
// own path, not a link that leads out, and not a link's words.
func TestNothingAboveTheFolderIsReached(t *testing.T) {
	f := newFolders(t)
	link(t, "../outside", filepath.Join(f.share, "out"))
	link(t, filepath.Join(f.outside, "secret.txt"), filepath.Join(f.share, "abs"))
	link(t, "../outside/secret.txt", filepath.Join(f.share, "outfile"))
	link(t, "sub/b.txt", filepath.Join(f.share, "in"))
	link(t, "../outside/new.txt", filepath.Join(f.share, "dangling"))
	_, _, c := filesOn(t, f.share, FilesChange)

	if text, err := readAll(c, "/a.txt"); err != nil || text != "inside" {
		t.Fatalf("a.txt read %q, %v", text, err)
	}
	if text, err := readAll(c, "/in"); err != nil || text != "below" {
		t.Errorf("a link inside read %q, %v", text, err)
	}
	for _, name := range []string{
		"/../outside/secret.txt", "../../outside/secret.txt", "sub/../../outside/secret.txt",
		filepath.ToSlash(filepath.Join(f.outside, "secret.txt")),
		"/out/secret.txt", "/outfile", "/abs",
	} {
		if text, err := readAll(c, name); err == nil || strings.Contains(text, "SECRET") {
			t.Errorf("%s read %q, %v", name, text, err)
		}
	}
	if _, err := c.ReadDir("/out"); err == nil {
		t.Error("a link out is listed")
	}
	if _, err := c.Stat("/out"); err == nil {
		t.Error("a link out says what it leads to")
	}
	// The host's real path is a name inside like any other.
	if _, err := c.Stat(filepath.ToSlash(f.share) + "/a.txt"); err == nil {
		t.Errorf("the host's path is found: %v", err)
	}
	if to, err := c.ReadLink("/in"); err != nil || to != "sub/b.txt" {
		t.Errorf("the link inside reads %q, %v", to, err)
	}
	for _, name := range []string{"/out", "/abs", "/outfile"} {
		if to, err := c.ReadLink(name); err == nil || strings.Contains(to, "outside") {
			t.Errorf("the link %s reads %q, %v", name, to, err)
		}
	}

	// Nothing is made or moved out.
	for _, name := range []string{"/out/new.txt", "/dangling"} {
		if w, err := c.Create(name); err == nil {
			_ = w.Close()
			t.Errorf("a file was created through a link out, as %s", name)
		}
	}
	if err := c.Mkdir("/out/made"); err == nil {
		t.Error("a folder was made through a link out")
	}
	if err := c.Rename("/a.txt", "/out/a.txt"); err == nil {
		t.Error("a file was moved out through a link")
	}
	if err := c.PosixRename("/a.txt", "/../a.txt"); err != nil {
		t.Errorf("a rename onto itself: %v", err)
	}
	if err := c.Remove("/out/secret.txt"); err == nil {
		t.Error("a file outside was removed")
	}
	if err := c.Chtimes("/outfile", time.Unix(1, 0), time.Unix(1, 0)); err == nil {
		t.Error("a file outside had its times set")
	}
	// A link inside is removed itself.
	if err := c.Remove("/outfile"); err != nil {
		t.Errorf("removing a link: %v", err)
	}
	for _, name := range []string{"/", "."} {
		if err := c.RemoveDirectory(name); err == nil {
			t.Errorf("the folder itself was removed as %q", name)
		}
	}
	if err := c.Rename("/", "/moved"); err == nil {
		t.Error("the folder itself was renamed")
	}
	got, err := os.ReadDir(f.outside)
	if err != nil || len(got) != 1 || got[0].Name() != "secret.txt" {
		t.Errorf("outside the folder is %v, %v", got, err)
	}
	if b, err := os.ReadFile(filepath.Join(f.outside, "secret.txt")); err != nil || string(b) != "SECRET" {
		t.Errorf("the file outside is %q, %v", b, err)
	}
	// No links are made, of either kind.
	if err := c.Symlink(filepath.Join(f.outside, "secret.txt"), "/made"); !refused(err) {
		t.Errorf("a link was made: %v", err)
	}
	if err := c.Link("/a.txt", "/hard"); !refused(err) {
		t.Errorf("a hard link was made: %v", err)
	}
}

// A folder swapped for a link out while the guest reads through it is
// never read through: each name is looked up anew, and none of it may be a
// link out.
func TestALinkSwappedInIsNotFollowed(t *testing.T) {
	f := newFolders(t)
	put(t, filepath.Join(f.share, "sw", "secret.txt"), "the one inside")
	_, _, c := filesOn(t, f.share, FilesLook)

	sw, real := filepath.Join(f.share, "sw"), filepath.Join(f.share, "real")
	if err := os.Rename(sw, real); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	var swapping sync.WaitGroup
	swapping.Go(func() {
		// sw is the folder, then a link out, and over again.
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			_ = os.Remove(sw)
			if i%2 == 0 {
				_ = os.Symlink(f.outside, sw)
			} else {
				_ = os.Symlink("real", sw)
			}
		}
	})
	defer func() {
		close(stop)
		swapping.Wait()
	}()
	read := 0
	for end := time.Now().Add(time.Second); time.Now().Before(end); {
		text, err := readAll(c, "/sw/secret.txt")
		if strings.Contains(text, "SECRET") {
			t.Fatal("read through a link out swapped in")
		}
		if err == nil {
			read++
		}
		if infos, err := c.ReadDir("/sw"); err == nil {
			for _, info := range infos {
				if info.Size() == int64(len("SECRET")) {
					t.Fatal("listed through a link out swapped in")
				}
			}
		}
	}
	if read == 0 {
		t.Log("the link never stood inside while it was read")
	}
}

// op is one thing a guest asks of the folder.
type op struct {
	name string
	// need is the least that allows it, and none says nothing does.
	need FilesMay
	none bool
	do   func(c *sftp.Client) error
}

// ops are all that a guest can ask, each on its own names.
func ops(i int) []op {
	n := func(s string) string { return "/" + s + strconv.Itoa(i) }
	return []op{
		{name: "list", need: FilesLook, do: func(c *sftp.Client) error { _, err := c.ReadDir("/"); return err }},
		{name: "stat", need: FilesLook, do: func(c *sftp.Client) error { _, err := c.Stat("/a.txt"); return err }},
		{name: "lstat", need: FilesLook, do: func(c *sftp.Client) error { _, err := c.Lstat("/a.txt"); return err }},
		{name: "read", need: FilesLook, do: func(c *sftp.Client) error { _, err := readAll(c, "/a.txt"); return err }},
		{name: "create", need: FilesChange, do: func(c *sftp.Client) error {
			w, err := c.Create(n("new"))
			if err != nil {
				return err
			}
			if _, err := w.Write([]byte("new")); err != nil {
				return err
			}
			return w.Close()
		}},
		{name: "write", need: FilesChange, do: func(c *sftp.Client) error {
			w, err := c.OpenFile("/a.txt", os.O_WRONLY)
			if err != nil {
				return err
			}
			defer func() { _ = w.Close() }()
			_, err = w.WriteAt([]byte("I"), 0)
			return err
		}},
		{name: "read and write", need: FilesChange, do: func(c *sftp.Client) error {
			w, err := c.OpenFile("/a.txt", os.O_RDWR)
			if err != nil {
				return err
			}
			return w.Close()
		}},
		{name: "mkdir", need: FilesChange, do: func(c *sftp.Client) error { return c.Mkdir(n("dir")) }},
		{name: "rmdir", need: FilesChange, do: func(c *sftp.Client) error { return c.RemoveDirectory(n("empty")) }},
		{name: "remove", need: FilesChange, do: func(c *sftp.Client) error { return c.Remove(n("gone")) }},
		{name: "rename", need: FilesChange, do: func(c *sftp.Client) error { return c.Rename(n("from"), n("to")) }},
		{name: "posix rename", need: FilesChange, do: func(c *sftp.Client) error { return c.PosixRename(n("pfrom"), n("pto")) }},
		{name: "times", need: FilesChange, do: func(c *sftp.Client) error {
			return c.Chtimes("/a.txt", time.Unix(1e9, 0), time.Unix(1e9, 0))
		}},
		{name: "permissions", none: true, do: func(c *sftp.Client) error { return c.Chmod("/a.txt", 0o777) }},
		{name: "owner", none: true, do: func(c *sftp.Client) error { return c.Chown("/a.txt", 0, 0) }},
		{name: "size", none: true, do: func(c *sftp.Client) error { return c.Truncate("/a.txt", 1) }},
		{name: "symlink", none: true, do: func(c *sftp.Client) error { return c.Symlink("/a.txt", n("sym")) }},
		{name: "hard link", none: true, do: func(c *sftp.Client) error { return c.Link("/a.txt", n("hard")) }},
		{name: "statvfs", none: true, do: func(c *sftp.Client) error { _, err := c.StatVFS("/"); return err }},
	}
}

// Each thing a guest may ask is allowed by what the host lets it do, as
// that stands as it is asked, and by nothing less.
func TestEachRequestIsAllowedByWhatTheGuestMayDo(t *testing.T) {
	f := newFolders(t)
	r, _, c := filesOn(t, f.share, FilesChange)
	for i, may := range []FilesMay{FilesChange, FilesLook, FilesNone, FilesChange} {
		for _, name := range []string{"empty", "gone", "from", "pfrom"} {
			if name == "empty" {
				_ = os.Mkdir(filepath.Join(f.share, name+strconv.Itoa(i)), 0o755)
			} else {
				put(t, filepath.Join(f.share, name+strconv.Itoa(i)), "x")
			}
		}
		// On the channel opened before, as the permission changes.
		if err := r.host.SetFiles(f.share, may); err != nil {
			t.Fatal(err)
		}
		for _, o := range ops(i) {
			err := o.do(c)
			allowed := !o.none && may >= o.need
			switch {
			case allowed && err != nil:
				t.Errorf("%d: %s, which %d allows, failed: %v", may, o.name, may, err)
			case !allowed && err == nil:
				t.Errorf("%d: %s was done", may, o.name)
			case !allowed && !refused(err):
				t.Errorf("%d: %s failed, but not as refused: %v", may, o.name, err)
			}
		}
	}
	if info, err := os.Stat(filepath.Join(f.share, "a.txt")); err != nil || info.Mode().Perm()&0o111 != 0 {
		t.Errorf("a.txt is %v, %v", info.Mode(), err)
	}
}

// With no files shared, or none yet, there is no channel to open.
func TestNoFilesNoChannel(t *testing.T) {
	f := newFolders(t)
	r := newRig(t, false)
	waits := r.joined(key(t))
	if err := r.host.SetFiles(f.share, FilesChange); err != nil {
		t.Fatal(err)
	}
	if _, err := waits.Files(t.Context()); !errors.Is(err, ErrNoFiles) {
		t.Errorf("a guest not let in opened the folder: %v", err)
	}
	r.answer <- true
	r.until("the guest is let in", func() bool { return waits.View().Phase == PhaseIn })
	for _, set := range []struct {
		dir string
		may FilesMay
	}{{f.share, FilesNone}, {"", FilesChange}} {
		if err := r.host.SetFiles(set.dir, set.may); err != nil {
			t.Fatal(err)
		}
		if _, err := waits.Files(t.Context()); !errors.Is(err, ErrNoFiles) {
			t.Errorf("%q at %d opened: %v", set.dir, set.may, err)
		}
	}
	if err := r.host.SetFiles(filepath.Join(f.share, "not there"), FilesLook); err == nil {
		t.Error("a folder not there was shared")
	}
	if _, err := waits.Files(t.Context()); !errors.Is(err, ErrNoFiles) {
		t.Errorf("a folder not there opened: %v", err)
	}
}

// Down to Look, a file open to write is closed under the guest; down to
// None, so is one being read, and the guest is told it failed. Up again,
// the same channel works.
func TestGoingDownClosesWhatWasOpenForMore(t *testing.T) {
	f := newFolders(t)
	// Big enough to be read for a while, 16 MB a second at most, and
	// sparse, so it takes no room.
	const big = 256 << 20
	sparse(t, filepath.Join(f.share, "big"), big)
	r, _, c := filesOn(t, f.share, FilesChange)

	w, err := c.Create("/up")
	if err != nil {
		t.Fatal(err)
	}
	// Written as fast as it goes, 64 KB at a time, until it fails.
	var wrote atomic.Int64
	writing := make(chan error, 1)
	go func() {
		chunk := make([]byte, 64<<10)
		for {
			n, err := w.Write(chunk)
			wrote.Add(int64(n))
			if err != nil {
				writing <- err
				return
			}
		}
	}()
	reading, err := c.Open("/big")
	if err != nil {
		t.Fatal(err)
	}
	var read atomic.Int64
	got := make(chan error, 1)
	go func() {
		_, err := io.Copy(counter{&read}, reading)
		got <- err
	}()
	r.until("a part is read and written", func() bool { return read.Load() > 1<<20 && wrote.Load() > 1<<20 })

	if err := r.host.SetFiles(f.share, FilesLook); err != nil {
		t.Fatal(err)
	}
	select {
	case <-writing:
	case <-time.After(10 * time.Second):
		t.Fatal("the write went on under Look")
	}
	if info, err := os.Stat(filepath.Join(f.share, "up")); err != nil || info.Size() > wrote.Load()+64<<10 {
		t.Errorf("the file written is %v, of %d sent, %v", info.Size(), wrote.Load(), err)
	}
	if read.Load() >= big {
		t.Error("the big read was over before it could be cut")
	}
	if err := r.host.SetFiles(f.share, FilesNone); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-got:
		if err == nil || read.Load() >= big {
			t.Errorf("the big read went on to %d bytes, %v", read.Load(), err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the big read went on under None")
	}
	_ = reading.Close()
	_ = w.Close()

	if err := r.host.SetFiles(f.share, FilesLook); err != nil {
		t.Fatal(err)
	}
	if text, err := readAll(c, "/a.txt"); err != nil || text != "inside" {
		t.Errorf("up again, a.txt read %q, %v", text, err)
	}
}

// sparse is a file of size that takes no room on disk, where the disk can.
func sparse(t *testing.T, name string, size int64) {
	t.Helper()
	f, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

type counter struct{ n *atomic.Int64 }

func (c counter) Write(p []byte) (int, error) { c.n.Add(int64(len(p))); return len(p), nil }

// Another folder closes all that was open in the one before, and names are
// found in the new one from then on.
func TestAnotherFolderClosesWhatWasOpen(t *testing.T) {
	f := newFolders(t)
	put(t, filepath.Join(f.outside, "a.txt"), "other")
	r, _, c := filesOn(t, f.share, FilesLook)
	open, err := c.Open("/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.host.SetFiles(f.outside, FilesLook); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(open); err == nil {
		t.Error("a file in the folder before was read")
	}
	_ = open.Close()
	if text, err := readAll(c, "/a.txt"); err != nil || text != "other" {
		t.Errorf("in the other folder, a.txt read %q, %v", text, err)
	}
}

// A guest can have so many files open, and so many channels, and no more.
func TestFilesLimits(t *testing.T) {
	f := newFolders(t)
	_, s, c := filesOn(t, f.share, FilesLook)
	var open []*sftp.File
	for range mostHandles {
		h, err := c.Open("/a.txt")
		if err != nil {
			t.Fatalf("file %d: %v", len(open), err)
		}
		open = append(open, h)
	}
	if h, err := c.Open("/a.txt"); err == nil {
		_ = h.Close()
		t.Error("one more file was opened")
	}
	if _, err := c.ReadDir("/"); err == nil {
		t.Error("one more folder was opened")
	}
	_ = open[0].Close()
	if _, err := c.ReadDir("/"); err != nil {
		t.Errorf("with room again: %v", err)
	}
	for _, h := range open[1:] {
		_ = h.Close()
	}

	more, err := s.Files(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = more.Close() }()
	if extra, err := s.Files(t.Context()); err == nil {
		_ = extra.Close()
		t.Error("a third channel opened")
	}
}

// A guest that asks without end and reads no answer is read no further:
// the host holds no more than its limits, whatever is sent.
func TestAFloodOfRequestsIsHeldBack(t *testing.T) {
	f := newFolders(t)
	// A file the answers are of: 32 KB a read.
	if err := os.WriteFile(filepath.Join(f.share, "big"), make([]byte, 1<<20), 0o644); err != nil {
		t.Fatal(err)
	}
	_, s, _ := filesOn(t, f.share, FilesLook)
	ch, reqs, err := s.conn.OpenChannel(chanFiles, nil)
	if err != nil {
		t.Fatal(err)
	}
	go ssh.DiscardRequests(reqs)
	// Hello, then open, by hand: the answers are read so far.
	hello := []byte{0, 0, 0, 5, 1, 0, 0, 0, 3}
	if _, err := ch.Write(hello); err != nil {
		t.Fatal(err)
	}
	openReq := sftpPacket(3, 1, []byte("\x00\x00\x00\x04/big"), []byte{0, 0, 0, 1, 0, 0, 0, 0})
	if _, err := ch.Write(openReq); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		// The version, and the handle.
		if _, err := readPacket(ch); err != nil {
			t.Fatal(err)
		}
	}
	handle := []byte("\x00\x00\x00\x011")

	var before runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	// Reads of 32 KB, sent and never read: 40 000 of them would be 1.2 GB
	// of answers.
	go func() {
		for i := uint32(2); i < 40000; i++ {
			off := make([]byte, 8)
			req := sftpPacket(5, i, handle, off, []byte{0, 0, 0x80, 0})
			if _, err := ch.Write(req); err != nil {
				return
			}
		}
	}()
	time.Sleep(3 * time.Second)
	var after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&after)
	grew := int64(after.HeapInuse) - int64(before.HeapInuse)
	if grew > 64<<20 {
		t.Errorf("the heap grew %d MB", grew>>20)
	}
	_ = ch.Close()
}

// readPacket reads one SFTP packet, whole.
func readPacket(r io.Reader) ([]byte, error) {
	var head [4]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return nil, err
	}
	body := make([]byte, int(head[0])<<24|int(head[1])<<16|int(head[2])<<8|int(head[3]))
	_, err := io.ReadFull(r, body)
	return body, err
}

// sftpPacket is an SFTP request of kind, its id and its parts.
func sftpPacket(kind byte, id uint32, parts ...[]byte) []byte {
	body := []byte{kind, byte(id >> 24), byte(id >> 16), byte(id >> 8), byte(id)}
	for _, p := range parts {
		body = append(body, p...)
	}
	n := len(body)
	return append([]byte{byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}, body...)
}

// What the guest does is told to the host: every upload, download, rename,
// removal and new folder, and nothing of what is in the files.
func TestWhatTheGuestDoesIsTold(t *testing.T) {
	f := newFolders(t)
	var mu sync.Mutex
	var told []FileEvent
	r := newRig(t, false)
	r.host.cfg.OnFiles = func(e FileEvent) {
		mu.Lock()
		defer mu.Unlock()
		told = append(told, e)
	}
	s := r.in(key(t))
	if err := r.host.SetFiles(f.share, FilesChange); err != nil {
		t.Fatal(err)
	}
	c, err := s.Files(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	if _, err := readAll(c, "/a.txt"); err != nil {
		t.Fatal(err)
	}
	w, err := c.Create("/sub/up.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	steps := []error{c.Mkdir("/made"), c.Rename("/sub/up.txt", "/made/up.txt"), c.Remove("/made/up.txt"), c.RemoveDirectory("/made")}
	for i, err := range steps {
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	// Opened and closed with nothing read or written is nothing done.
	if h, err := c.Open("/a.txt"); err == nil {
		_ = h.Close()
	}
	want := []FileEvent{
		{Op: FileDownloaded, Path: "/a.txt", Bytes: 6},
		{Op: FileUploaded, Path: "/sub/up.txt", Bytes: 5},
		{Op: FolderCreated, Path: "/made"},
		{Op: FileRenamed, Path: "/sub/up.txt", To: "/made/up.txt"},
		{Op: FileRemoved, Path: "/made/up.txt"},
		{Op: FileRemoved, Path: "/made", Folder: true},
	}
	r.until("all is told", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(told) >= len(want)
	})
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(told, want) {
		t.Errorf("told %+v\nwant %+v", told, want)
	}
}

// What the guest is told of the files comes as it was sent, and only what
// it can be.
func TestTheFilesInTheState(t *testing.T) {
	for _, c := range []struct {
		may        FilesMay
		folder     string
		wantMay    FilesMay
		wantFolder string
	}{
		{FilesChange, "Reports", FilesChange, "Reports"},
		{FilesLook, "Reports", FilesLook, "Reports"},
		{FilesLook, "", FilesNone, ""},
		{9, "Reports", FilesNone, "Reports"},
		{FilesLook, "Re\x1bports\u202e", FilesLook, "Reports"},
	} {
		frame, err := encodeState(State{HostName: "anna", Files: c.may, Folder: c.folder})
		if err != nil {
			t.Fatal(err)
		}
		st, err := decodeState(frame[headerLen:])
		if err != nil {
			t.Fatal(err)
		}
		if st.Files != c.wantMay || st.Folder != c.wantFolder {
			t.Errorf("%d %q came as %d %q", c.may, c.folder, st.Files, st.Folder)
		}
	}
	long := State{Files: FilesLook, Folder: strings.Repeat("x", 500)}.least()
	if len(long.Folder) > mostName || long.Files != FilesLook {
		t.Errorf("the least of a long name is %d %q", long.Files, long.Folder)
	}
}

// A guest's files are shut when its connection ends, and open again as it
// comes back.
func TestFilesAfterAConnectionIsLost(t *testing.T) {
	f := newFolders(t)
	r, s, c := filesOn(t, f.share, FilesLook)
	open, err := c.Open("/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	k := s.key
	_ = s.Close()
	r.until("the guest is away", func() bool { return !r.host.Status().Here })
	if _, err := io.ReadAll(open); err == nil {
		t.Error("read after the connection was lost")
	}
	r.host.mu.Lock()
	left := len(r.host.files.open)
	r.host.mu.Unlock()
	if left != 0 {
		t.Errorf("%d files are open still", left)
	}
	back, err := r.guest(k)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = back.Close() }()
	c2, err := back.Files(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c2.Close() }()
	if text, err := readAll(c2, "/a.txt"); err != nil || text != "inside" {
		t.Errorf("back, a.txt read %q, %v", text, err)
	}
}
