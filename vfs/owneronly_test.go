package vfs

import (
	"io/fs"
	"testing"
)

// A mode only its owner may use is private. One with no permission
// bits says nothing, as a server that sent none gives it.
func TestOwnerOnly(t *testing.T) {
	for mode, want := range map[fs.FileMode]bool{
		0o600: true, 0o400: true, 0o700: true,
		0o644: false, 0o640: false, 0o604: false,
		0: false, fs.ModeDir: false,
	} {
		if got := OwnerOnly(mode); got != want {
			t.Errorf("OwnerOnly(%v) = %v, want %v", mode, got, want)
		}
	}
}
