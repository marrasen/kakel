package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The status line says where the folder of the pane with the keyboard
// stands in git, once its shell has said which folder it is in.
func TestTheStatusLineSaysTheGitStatus(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil || runtime.GOOS == "windows" {
		t.Skip("no git here, or a path a shell would say differently")
	}
	root := t.TempDir()
	for _, args := range [][]string{{"init", "-q", "-b", "trunk"}} {
		if out, err := exec.Command(git, append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, _ := localPane(t, "\x1b]7;file://localhost"+root+"\x07", "/bin/sh")
	a.focus("p1")
	waitFor(t, a, "the shell's folder", func() bool { dir, _ := a.terminal("p1").Dir(); return dir == root })
	a.lookAtGit(true)
	waitFor(t, a, "the git status", func() bool { return strings.Contains(a.st.Status, "Git: trunk") })
	if !strings.Contains(a.st.Status, "1 new") {
		t.Fatalf("the status line says %q", a.st.Status)
	}
}
