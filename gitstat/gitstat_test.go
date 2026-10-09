package gitstat

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestARepositoryIsFoundFromAFolderInIt(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	r, ok := Find(sub)
	if !ok || r.Root != root {
		t.Fatalf("found %+v, %v", r, ok)
	}
	if b, detached, err := r.Head(); err != nil || b != "main" || detached {
		t.Fatalf("the head is %q, detached %v, %v", b, detached, err)
	}
	if _, ok := Find(t.TempDir()); ok {
		t.Fatal("a folder in no repository was found in one")
	}
}

// A work tree of its own names its git files in a .git file, and shares
// its settings with the repository it came from.
func TestAWorkTreeIsFoundThroughItsGitFile(t *testing.T) {
	main := t.TempDir()
	gd := filepath.Join(main, ".git", "worktrees", "wt")
	write(t, filepath.Join(gd, "HEAD"), "0123456789abcdef0123456789abcdef01234567\n")
	write(t, filepath.Join(gd, "commondir"), "../..\n")
	wt := t.TempDir()
	write(t, filepath.Join(wt, ".git"), "gitdir: "+gd+"\n")
	r, ok := Find(wt)
	if !ok || r.GitDir != gd || r.Common != filepath.Join(main, ".git") {
		t.Fatalf("found %+v, %v", r, ok)
	}
	if b, detached, _ := r.Head(); b != "0123456" || !detached {
		t.Fatalf("the head is %q, detached %v", b, detached)
	}
}

// Settings that would have git status run a program, or read settings
// from elsewhere, keep it from running.
func TestRiskySettingsAreSeen(t *testing.T) {
	for body, want := range map[string]bool{
		"[core]\n\tbare = false\n":                       false,
		"[core]\n\tfsmonitor = ./watch.sh\n":             true,
		"[filter \"x\"]\n\tclean = ./run.sh\n":           true,
		"[ FILTER \"x\" ]\n\tclean = ./run.sh\n":         true,
		"[include]\n\tpath = ../evil\n":                  true,
		"[includeIf \"gitdir:/\"]\n\tpath = ../evil\n":   true,
		"[remote \"origin\"]\n\turl = https://x/y.git\n": false,
	} {
		root := t.TempDir()
		write(t, filepath.Join(root, ".git", "config"), body)
		r := Repo{Root: root, GitDir: filepath.Join(root, ".git"), Common: filepath.Join(root, ".git")}
		if got := r.Risky(); got != want {
			t.Errorf("settings %q: risky %v, want %v", body, got, want)
		}
	}
}

func TestStatusIsReadAndSaid(t *testing.T) {
	var s Status
	parse([]byte("# branch.oid abc\n# branch.head main\n# branch.upstream origin/main\n# branch.ab +2 -1\n"+
		"1 .M N... 100644 100644 100644 a b x.go\n2 R. N... 100644 100644 100644 a b R100 y.go\tz.go\n"+
		"u UU N... 1 2 3 4 a b c c.go\n? new.txt\n? other.txt\n"), &s)
	s.Branch, s.Counted = "main", true
	if got := s.String(); got != "Git: main ↑2↓1 · 1 conflict, 2 changed, 2 new" {
		t.Fatalf("said %q", got)
	}
	if got := (Status{Branch: "main", Counted: true}).String(); got != "Git: main · clean" {
		t.Fatalf("a clean tree said %q", got)
	}
	if got := (Status{Branch: "abc1234", Detached: true}).String(); got != "Git: detached at abc1234" {
		t.Fatalf("a detached head said %q", got)
	}
}

// git runs in a repository with no programs in its settings, and counts;
// in one whose filter names a program, it does not run, and the branch
// is said alone: the program never runs.
func TestGitStatusRunsOnlyWhereItRunsNothing(t *testing.T) {
	if git() == "" {
		t.Skip("no git here")
	}
	root := t.TempDir()
	gitIn := func(args ...string) {
		t.Helper()
		cmd := exec.Command(git(), append([]string{"-C", root}, args...)...)
		cmd.Env = append(environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	gitIn("init", "-q", "-b", "main")
	write(t, filepath.Join(root, "a.txt"), "one\n")
	gitIn("add", "a.txt")
	gitIn("commit", "-q", "-m", "one")
	write(t, filepath.Join(root, "a.txt"), "two\n")
	write(t, filepath.Join(root, "b.txt"), "new\n")
	s, ok := Read(context.Background(), filepath.Join(root))
	if !ok || !s.Counted || s.Branch != "main" || s.Changed != 1 || s.Untracked != 1 {
		t.Fatalf("read %+v, %v", s, ok)
	}

	if runtime.GOOS == "windows" {
		return
	}
	marker := filepath.Join(t.TempDir(), "ran")
	script := filepath.Join(t.TempDir(), "clean.sh")
	write(t, script, "#!/bin/sh\ntouch "+marker+"\ncat\n")
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn("config", "filter.evil.clean", script)
	write(t, filepath.Join(root, ".gitattributes"), "*.txt filter=evil\n")
	s, ok = Read(context.Background(), root)
	if !ok || s.Counted || s.Branch != "main" {
		t.Fatalf("with a filter, read %+v, %v", s, ok)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the filter's program ran")
	}
	if !strings.HasPrefix(s.String(), "Git: main") {
		t.Fatalf("said %q", s.String())
	}
}
