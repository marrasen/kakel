// Package gitstat says where a folder stands in git: its branch, how
// far it is ahead of and behind its upstream, and how many files have
// changed, for the status line.
//
// A repository's own settings can name programs git runs while it
// reads the work tree: a filter that cleans files, or a monitor that
// watches it. A repository can come from anywhere, as from an archive
// unpacked, so a folder is never a reason to run what it names. The
// branch is read from the repository's files, with no program run; git
// status runs only where the repository names no such program, with
// its file monitor off and asking nothing of anyone.
package gitstat

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/marrasen/kakel/internal/quiet"
)

// Repo is a repository a folder is in: the top of its work tree, the
// folder its git files are in, and the one shared with the work trees
// beside it.
type Repo struct {
	Root, GitDir, Common string
}

// Find returns the repository dir is in, looking up from it, and false
// where it is in none.
func Find(dir string) (Repo, bool) {
	dir = filepath.Clean(dir)
	for {
		at := filepath.Join(dir, ".git")
		if info, err := os.Stat(at); err == nil {
			r := Repo{Root: dir, GitDir: at, Common: at}
			if !info.IsDir() {
				// A work tree of its own, or a submodule: the file says
				// where its git files are.
				b, err := os.ReadFile(at)
				if err != nil {
					return Repo{}, false
				}
				gd, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir:")
				if !ok {
					return Repo{}, false
				}
				gd = strings.TrimSpace(gd)
				if !filepath.IsAbs(gd) {
					gd = filepath.Join(dir, gd)
				}
				r.GitDir, r.Common = gd, gd
			}
			if b, err := os.ReadFile(filepath.Join(r.GitDir, "commondir")); err == nil {
				c := strings.TrimSpace(string(b))
				if !filepath.IsAbs(c) {
					c = filepath.Join(r.GitDir, c)
				}
				r.Common = filepath.Clean(c)
			}
			return r, true
		}
		up := filepath.Dir(dir)
		if up == dir {
			return Repo{}, false
		}
		dir = up
	}
}

// Head reads the branch checked out, or for a detached head the start
// of its commit, with detached set.
func (r Repo) Head() (branch string, detached bool, err error) {
	b, err := os.ReadFile(filepath.Join(r.GitDir, "HEAD"))
	if err != nil {
		return "", false, err
	}
	head := strings.TrimSpace(string(b))
	if ref, ok := strings.CutPrefix(head, "ref:"); ok {
		ref = strings.TrimSpace(ref)
		return strings.TrimPrefix(ref, "refs/heads/"), false, nil
	}
	if len(head) >= 7 {
		return head[:7], true, nil
	}
	return "", false, errors.New("HEAD names nothing")
}

// risky are what in a repository's settings would have git status run
// a program, or read settings from elsewhere that could.
var risky = []string{"fsmonitor", "[filter", "[include", "[includeif"}

// Risky reports whether the repository's own settings name a program
// git status would run, or settings kept elsewhere: either way, status
// is not run there. Settings that cannot be read count as risky.
func (r Repo) Risky() bool {
	files := []string{filepath.Join(r.Common, "config"), filepath.Join(r.GitDir, "config.worktree")}
	if r.GitDir != r.Common {
		files = append(files, filepath.Join(r.GitDir, "config"))
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return true
		}
		low := bytes.ToLower(b)
		// [filter "lfs"] and the like, whatever spaces sit between.
		low = bytes.ReplaceAll(low, []byte("[ "), []byte("["))
		for _, word := range risky {
			if bytes.Contains(low, []byte(word)) {
				return true
			}
		}
	}
	return false
}

// Status is where a work tree stands.
type Status struct {
	Branch   string
	Detached bool
	// Counted says the counts below are known: git status ran.
	Counted bool
	// Ahead and Behind are the commits it has that its upstream lacks,
	// and the other way, with Upstream set when it has one.
	Ahead, Behind int
	Upstream      bool
	// Changed counts the files changed, staged or not, Untracked the
	// files git does not track, and Conflicts those left in a merge.
	Changed, Untracked, Conflicts int
}

// String says the status as the status line does.
func (s Status) String() string {
	if s.Branch == "" {
		return ""
	}
	parts := []string{s.Branch}
	if s.Detached {
		parts[0] = "detached at " + s.Branch
	}
	if s.Upstream && (s.Ahead > 0 || s.Behind > 0) {
		ab := ""
		if s.Ahead > 0 {
			ab += "↑" + strconv.Itoa(s.Ahead)
		}
		if s.Behind > 0 {
			ab += "↓" + strconv.Itoa(s.Behind)
		}
		parts[0] += " " + ab
	}
	if s.Counted {
		var more []string
		if s.Conflicts > 0 {
			more = append(more, count(s.Conflicts, "conflict"))
		}
		if s.Changed > 0 {
			more = append(more, strconv.Itoa(s.Changed)+" changed")
		}
		if s.Untracked > 0 {
			more = append(more, strconv.Itoa(s.Untracked)+" new")
		}
		if len(more) == 0 {
			more = []string{"clean"}
		}
		parts = append(parts, strings.Join(more, ", "))
	}
	return "Git: " + strings.Join(parts, " · ")
}

// count says n of a thing, with an s for more than one.
func count(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return strconv.Itoa(n) + " " + what + "s"
}

// git is where the git program is, found once; "" where there is none.
var git = sync.OnceValue(func() string {
	p, err := exec.LookPath("git")
	if err != nil {
		return ""
	}
	return p
})

// Read says where the work tree dir is in stands: the branch always,
// and the counts where git status may run and does within ctx.
func Read(ctx context.Context, dir string) (Status, bool) {
	r, ok := Find(dir)
	if !ok {
		return Status{}, false
	}
	branch, detached, err := r.Head()
	if err != nil {
		return Status{}, false
	}
	s := Status{Branch: branch, Detached: detached}
	if git() == "" || r.Risky() {
		return s, true
	}
	out, err := run(ctx, r.Root)
	if err != nil {
		return s, true
	}
	parse(out, &s)
	s.Counted = true
	return s, true
}

// most is how long git status may take.
const most = 3 * time.Second

// run runs git status in root: its own settings for file monitors and
// submodules turned off, no prompt, no lock taken, and nothing of git's
// from kakel's own surroundings.
func run(ctx context.Context, root string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, most)
	defer cancel()
	cmd := exec.CommandContext(ctx, git(), "--no-optional-locks",
		"-c", "core.fsmonitor=false", "-c", "core.untrackedCache=false", "-c", "submodule.recurse=false",
		"-C", root, "status", "--porcelain=v2", "--branch", "--untracked-files=normal", "--ignore-submodules=all")
	cmd.Env = environ()
	cmd.Stdin = nil
	quiet.Hide(cmd)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git status: %w", err)
	}
	return out, nil
}

// environ is kakel's surroundings without git's own, which could point
// it at another repository, with no prompt and no lock taken.
func environ() []string {
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(strings.ToUpper(kv), "GIT_") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
}

// parse reads git status --porcelain=v2 --branch into s.
func parse(out []byte, s *Status) {
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "# branch.upstream "):
			s.Upstream = true
		case strings.HasPrefix(line, "# branch.ab "):
			for _, f := range strings.Fields(strings.TrimPrefix(line, "# branch.ab ")) {
				n, err := strconv.Atoi(f[1:])
				if err != nil {
					continue
				}
				if f[0] == '+' {
					s.Ahead = n
				} else {
					s.Behind = n
				}
			}
		case strings.HasPrefix(line, "1 "), strings.HasPrefix(line, "2 "):
			s.Changed++
		case strings.HasPrefix(line, "u "):
			s.Conflicts++
		case strings.HasPrefix(line, "? "):
			s.Untracked++
		}
	}
}
