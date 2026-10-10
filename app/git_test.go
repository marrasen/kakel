package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A file manager pane's status bar says where the folder it shows stands
// in git, and says it again of the next folder it turns to. The status
// line of the window says nothing of git.
func TestAFilePaneSaysTheGitStatusOfItsFolder(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("no git here")
	}
	root := t.TempDir()
	if out, err := exec.Command(git, "-C", root, "init", "-q", "-b", "trunk").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	plain := t.TempDir()

	a, _ := agentApp(t)
	a.settings = mustSettings(t)
	a.files = newFakeFiles(t)
	a.handle(OpenFilesOn{Path: root})
	id := a.st.Panes[len(a.st.Panes)-1].ID
	fp := a.fmPanes[id]
	waitFor(t, a, "the git status", func() bool { return fp.git.saidOn == root && strings.Contains(fp.git.said, "Git: trunk") })
	// The counts, where git status answered in time: on a busy machine,
	// as a test runner can be, it may not, and the branch alone is said.
	if said := fp.git.said; said != "Git: trunk" && !strings.Contains(said, "1 new") {
		t.Fatalf("the status bar says %q", said)
	}
	if strings.Contains(a.st.Status, "Git") {
		t.Fatalf("the status line says %q", a.st.Status)
	}

	// In a folder outside any repository, there is nothing to say.
	go fp.w.Show(nil, plain)
	waitFor(t, a, "the folder outside git read", func() bool { return fp.git.saidOn == plain })
	if fp.git.said != "" {
		t.Fatalf("a folder outside git says %q", fp.git.said)
	}
}

// A file manager pane that turns to another folder while the git status
// of the last is being read has the new one read once that read is back.
func TestAFilePaneTurnedDuringAGitReadIsReadAgain(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("no git here")
	}
	root := t.TempDir()
	if out, err := exec.Command(git, "-C", root, "init", "-q", "-b", "trunk").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	plain := t.TempDir()

	a, _ := agentApp(t)
	a.settings = mustSettings(t)
	a.files = newFakeFiles(t)
	a.handle(OpenFilesOn{Path: root})
	id := a.st.Panes[len(a.st.Panes)-1].ID
	fp := a.fmPanes[id]
	waitFor(t, a, "the git status", func() bool { return fp.git.saidOn == root })

	a.lookAtGit(id, true)
	// Turned while that read is out, as the file manager says.
	a.fmTitleMu.Lock()
	a.fmFolders[id] = [2]string{"", plain}
	a.fmTitleMu.Unlock()
	a.filePaneFolder(id)
	waitFor(t, a, "the new folder read", func() bool { return fp.git.saidOn == plain })
	if fp.git.said != "" {
		t.Fatalf("a folder outside git says %q", fp.git.said)
	}
}
