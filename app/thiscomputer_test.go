package app

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// A shell started from a folder nobody starts kakel in to work there,
// as a shortcut gives it, starts in its profile's folder, or else at
// home; any other folder stays. A folder is ~ for home, and one that
// isn't there is refused.
func TestAShellStartsInTheStartFolder(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the installed folder is the user's own here")
	}
	a := updatesApp(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	bin := filepath.Join(home, ".local", "bin")
	installed := filepath.Join(home, ".local", "share", "kakel")
	work, elsewhere := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, from := range []string{"", home, bin, installed} {
		if got := a.startDir(from, ""); got != home {
			t.Fatalf("from %q with no start folder, a shell starts in %s, want home", from, got)
		}
	}
	for _, from := range []string{"", home, bin} {
		if got := a.startDir(from, work); got != work {
			t.Fatalf("from %q, a shell starts in %s, want %s", from, got, work)
		}
	}
	if got := a.startDir(elsewhere, work); got != elsewhere {
		t.Fatalf("from %s, a shell starts in %s", elsewhere, got)
	}
	if got, err := FolderHere("~/.local"); err != nil || got != filepath.Join(home, ".local") {
		t.Fatalf("~/.local is %q, %v", got, err)
	}
	if _, err := FolderHere(filepath.Join(home, "nowhere")); err == nil {
		t.Fatal("a folder that isn't there was taken")
	}
}
