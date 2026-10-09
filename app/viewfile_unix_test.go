//go:build unix

package app

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/marrasen/kakel/vfs"
)

// A pipe is not read for the viewer: it could make the read wait for
// ever.
func TestTheViewerReadsNoPipe(t *testing.T) {
	pipe := filepath.Join(t.TempDir(), "pipe")
	if err := syscall.Mkfifo(pipe, 0o600); err != nil {
		t.Skip("no pipes here:", err)
	}
	if _, _, why := readView(vfs.NewLocal(), pipe); !strings.Contains(why, "not a file") {
		t.Fatalf("a pipe was read: %q", why)
	}
}
