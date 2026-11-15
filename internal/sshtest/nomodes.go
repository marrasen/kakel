package sshtest

import (
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pkg/sftp"
)

// NoModes is an SFTP server that keeps its files in memory and doesn't
// store permissions: every request to set them is answered "operation
// unsupported", as many servers answer it. It does set the time a file
// last changed, and remembers it, and a rename replaces what is there.
type NoModes struct {
	// Client is a session on it.
	Client *sftp.Client

	mu          sync.Mutex
	times       map[string]time.Time
	modes       atomic.Int32
	writes      atomic.Pointer[error]
	refuseTimes atomic.Bool
}

// NewNoModes starts a server that doesn't store permissions, and a
// session on it, for as long as t runs.
func NewNoModes(t testing.TB) *NoModes {
	t.Helper()
	n := &NoModes{times: map[string]time.Time{}}
	handlers := sftp.InMemHandler()
	handlers.FileCmd = noModesCmd{handlers.FileCmd, n}
	handlers.FilePut = noModesPut{handlers.FilePut, n}
	here, there := net.Pipe()
	server := sftp.NewRequestServer(there, handlers)
	go func() { _ = server.Serve() }()
	t.Cleanup(func() { _ = server.Close() })
	client, err := sftp.NewClientPipe(here, here)
	if err != nil {
		t.Fatalf("start the client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	n.Client = client
	return n
}

// Time is the time the file at path was last set to have changed.
func (n *NoModes) Time(path string) (time.Time, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	at, ok := n.times[path]
	return at, ok
}

// Refused is how many requests to set permissions it has refused.
func (n *NoModes) Refused() int { return int(n.modes.Load()) }

// RefuseWrites makes every write fail from now on, as a full disk does.
func (n *NoModes) RefuseWrites() { n.writes.Store(&errDiskFull) }

// DenyWrites makes every write fail from now on with "permission
// denied", the answer a refused chmod may also get.
func (n *NoModes) DenyWrites() {
	err := error(sftp.ErrSSHFxPermissionDenied)
	n.writes.Store(&err)
}

// RefuseTimes makes it refuse to set times too from now on, with
// "operation unsupported", as some servers do.
func (n *NoModes) RefuseTimes() { n.refuseTimes.Store(true) }

// noModesCmd refuses permissions, keeps times unless told not to, and renames over a name
// that is taken.
type noModesCmd struct {
	sftp.FileCmder
	n *NoModes
}

func (c noModesCmd) Filecmd(r *sftp.Request) error {
	switch r.Method {
	case "Setstat":
		flags := r.AttrFlags()
		if flags.Permissions || flags.UidGid {
			c.n.modes.Add(1)
			return sftp.ErrSSHFxOpUnsupported
		}
		if flags.Acmodtime && c.n.refuseTimes.Load() {
			return sftp.ErrSSHFxOpUnsupported
		}
		if flags.Acmodtime {
			c.n.mu.Lock()
			c.n.times[r.Filepath] = time.Unix(int64(r.Attributes().Mtime), 0)
			c.n.mu.Unlock()
		}
	case "Rename":
		_ = c.FileCmder.Filecmd(&sftp.Request{Method: "Remove", Filepath: r.Target})
		if err := c.FileCmder.Filecmd(r); err != nil {
			return err
		}
		c.n.mu.Lock()
		if at, ok := c.n.times[r.Filepath]; ok {
			c.n.times[r.Target] = at
			delete(c.n.times, r.Filepath)
		}
		c.n.mu.Unlock()
		return nil
	}
	return c.FileCmder.Filecmd(r)
}

// PosixRename is OpenSSH's rename that replaces what is there.
func (c noModesCmd) PosixRename(r *sftp.Request) error {
	return c.Filecmd(&sftp.Request{Method: "Rename", Filepath: r.Filepath, Target: r.Target})
}

// noModesPut fails every write once writes are refused.
type noModesPut struct {
	sftp.FileWriter
	n *NoModes
}

func (p noModesPut) Filewrite(r *sftp.Request) (io.WriterAt, error) {
	w, err := p.FileWriter.Filewrite(r)
	if err != nil {
		return nil, err
	}
	return refusingWriter{w, p.n}, nil
}

// errDiskFull is the failure of a write once writes are refused.
var errDiskFull = errors.New("the disk is full")

type refusingWriter struct {
	io.WriterAt
	n *NoModes
}

func (w refusingWriter) WriteAt(b []byte, off int64) (int, error) {
	if err := w.n.writes.Load(); err != nil {
		return 0, *err
	}
	return w.WriterAt.WriteAt(b, off)
}
