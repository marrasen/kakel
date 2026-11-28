package guestview

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sync"
	"time"

	"github.com/marrasen/gunim/filemanager"
	"github.com/pkg/sftp"

	"github.com/marrasen/kakel/vfs"
)

// sharedFS is the share's folder as the file manager reads it: kakel's
// SFTP files over the guest's files channel, whose top is the folder's.
// The channel is the connection's, so it is handed over again as the
// connection is made again, and between the two every call fails, as
// the file manager shows a server's failures.
//
// The host refuses what the share does not allow, and keeps neither
// links nor permissions: a file the guest writes has the permissions the
// host gives it, so Chmod asks nothing.
type sharedFS struct {
	id   string
	name string

	mu  sync.Mutex
	cur *vfs.SFTP
	// err is why the channel could not be had, and waiting closes once
	// it is had, or err is known, while it is asked for.
	err     error
	waiting chan struct{}
}

var (
	_ filemanager.FS            = (*sharedFS)(nil)
	_ filemanager.Stamper       = (*sharedFS)(nil)
	_ filemanager.SpaceReporter = (*sharedFS)(nil)
)

// errNoFolder is a call while the folder is not reached.
var errNoFolder = errors.New("the shared folder is not reached now")

// reachWait is how long a call waits for the channel being asked for.
const reachWait = 30 * time.Second

// newSharedFS is a folder called name, as the file system of ID id.
func newSharedFS(id, name string) *sharedFS { return &sharedFS{id: id, name: name} }

// asking says the channel is being asked for: calls wait for it.
func (s *sharedFS) asking() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cur == nil && s.waiting == nil {
		s.waiting = make(chan struct{})
	}
}

// fail says the channel could not be had, and why: calls fail so.
func (s *sharedFS) fail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = fmt.Errorf("couldn't reach the shared folder: %w", err)
	s.wake()
}

// wake lets the calls that wait go on. Of s.mu.
func (s *sharedFS) wake() {
	if s.waiting != nil {
		close(s.waiting)
		s.waiting = nil
	}
}

// set hands over the folder's channel, or none while there is none. The
// one before is closed.
func (s *sharedFS) set(c *sftp.Client) {
	var next *vfs.SFTP
	if c != nil {
		next = vfs.NewSFTP(s.name, nil, c, c.Close)
	}
	s.mu.Lock()
	was := s.cur
	s.cur, s.err = next, nil
	if next != nil {
		s.wake()
	}
	s.mu.Unlock()
	if was != nil {
		_ = was.Close()
	}
}

// files is the folder, once it is reached, or why it cannot be read.
func (s *sharedFS) files() (*vfs.SFTP, error) {
	for end := time.Now().Add(reachWait); ; {
		s.mu.Lock()
		cur, err, waiting := s.cur, s.err, s.waiting
		s.mu.Unlock()
		switch {
		case cur != nil:
			return cur, nil
		case waiting == nil && err != nil:
			return nil, err
		case waiting == nil:
			return nil, errNoFolder
		}
		select {
		case <-waiting:
		case <-time.After(time.Until(end)):
			return nil, errNoFolder
		}
		if time.Now().After(end) {
			return nil, errNoFolder
		}
	}
}

// ID implements [filemanager.FS].
func (s *sharedFS) ID() string { return s.id }

// Paths implements [filemanager.FS]: SFTP's, with slashes.
func (*sharedFS) Paths() filemanager.PathStyle { return filemanager.SlashPaths }

// Home implements [filemanager.FS]: the folder's top.
func (*sharedFS) Home() (string, error) { return "/", nil }

// ReadDir implements [filemanager.FS]. A folder is read whole, so a ctx
// that ends lets the read finish unheard.
func (s *sharedFS) ReadDir(ctx context.Context, dir string) ([]fs.DirEntry, error) {
	f, err := s.files()
	if err != nil {
		return nil, err
	}
	type read struct {
		entries []vfs.Entry
		err     error
	}
	got := make(chan read, 1)
	go func() {
		entries, err := f.ReadDir(dir)
		got <- read{entries, err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-got:
		if r.err != nil {
			return nil, r.err
		}
		out := make([]fs.DirEntry, len(r.entries))
		for i, e := range r.entries {
			out[i] = entryOf{e}
		}
		return out, nil
	}
}

// Stat implements [filemanager.FS], following a link at the end of path.
func (s *sharedFS) Stat(path string) (fs.FileInfo, error) {
	f, err := s.files()
	if err != nil {
		return nil, err
	}
	return f.Follow(path)
}

// Lstat implements [filemanager.FS].
func (s *sharedFS) Lstat(path string) (fs.FileInfo, error) {
	f, err := s.files()
	if err != nil {
		return nil, err
	}
	e, err := f.Stat(path)
	if err != nil {
		return nil, err
	}
	return infoOf{e}, nil
}

// Open implements [filemanager.FS].
func (s *sharedFS) Open(path string) (io.ReadSeekCloser, error) {
	f, err := s.files()
	if err != nil {
		return nil, err
	}
	r, err := f.Open(path)
	if err != nil {
		return nil, err
	}
	if rs, ok := r.(io.ReadSeekCloser); ok {
		return rs, nil
	}
	return nil, errors.Join(errors.New("the file cannot be read here"), r.Close())
}

// Create implements [filemanager.FS]: a new file, and never one there.
func (s *sharedFS) Create(path string) (io.WriteCloser, error) {
	f, err := s.files()
	if err != nil {
		return nil, err
	}
	return f.CreateNew(path, 0o644)
}

// Mkdir implements [filemanager.FS].
func (s *sharedFS) Mkdir(path string, perm fs.FileMode) error {
	f, err := s.files()
	if err != nil {
		return err
	}
	return f.Mkdir(path, perm)
}

// Rename implements [filemanager.FS], over a file at to.
func (s *sharedFS) Rename(from, to string) error {
	f, err := s.files()
	if err != nil {
		return err
	}
	return f.Rename(from, to)
}

// Remove implements [filemanager.FS].
func (s *sharedFS) Remove(path string) error {
	f, err := s.files()
	if err != nil {
		return err
	}
	return f.Remove(path)
}

// Chmod implements [filemanager.Stamper]: the host keeps no permissions
// a guest asks for, so there is nothing to ask.
func (*sharedFS) Chmod(string, fs.FileMode) error { return nil }

// Chtimes implements [filemanager.Stamper].
func (s *sharedFS) Chtimes(path string, mod time.Time) error {
	f, err := s.files()
	if err != nil {
		return err
	}
	return f.Chtimes(path, mod)
}

// Space implements [filemanager.SpaceReporter], where the host says.
func (s *sharedFS) Space(path string) (free, total uint64, err error) {
	f, err := s.files()
	if err != nil {
		return 0, 0, errors.ErrUnsupported
	}
	return f.Space(path)
}

// entryOf is an item of a folder, as the file manager reads it.
type entryOf struct{ e vfs.Entry }

func (d entryOf) Name() string               { return d.e.Name }
func (d entryOf) IsDir() bool                { return d.e.IsDir() }
func (d entryOf) Type() fs.FileMode          { return d.e.Mode.Type() }
func (d entryOf) Info() (fs.FileInfo, error) { return infoOf(d), nil }

// infoOf is what the file manager reads of an item.
type infoOf struct{ e vfs.Entry }

func (i infoOf) Name() string       { return i.e.Name }
func (i infoOf) Size() int64        { return i.e.Size }
func (i infoOf) Mode() fs.FileMode  { return i.e.Mode }
func (i infoOf) ModTime() time.Time { return i.e.Mod }
func (i infoOf) IsDir() bool        { return i.e.IsDir() }
func (i infoOf) Sys() any           { return nil }
