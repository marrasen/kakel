package app

import (
	"os"
	"time"

	"github.com/marrasen/kakel/gitstat"
)

// gitEvery is how often a file manager pane's git status is read again,
// at most: a repository whose status takes long is read less often.
const gitEvery = 3 * time.Second

// gitWatch is the git status on a file manager pane's status bar: the
// folder it was last read for, what was last said and of which folder,
// whether a read is out, and when the next may go.
type gitWatch struct {
	dir          string
	saidOn, said string
	asking       bool
	next         time.Time
}

// startGit reads the git status of the folder each file manager pane
// shows, and again every gitEvery while kakel runs.
func (a *app) startGit() {
	time.AfterFunc(gitEvery, func() {
		a.events <- func() {
			if a.gone {
				return
			}
			for id := range a.fmPanes {
				a.lookAtGit(id, false)
			}
			a.startGit()
			// Asking changes nothing kakel draws: the file manager draws
			// the answer.
			a.quiet = true
		}
	})
}

// filePaneFolder reads the git status at once for file manager pane id,
// which has turned to another folder.
func (a *app) filePaneFolder(id string) {
	a.lookAtGit(id, true)
	a.quiet = true
}

// gitDir is the folder file manager pane id shows, when it is one of this
// computer's: not a server's.
func (a *app) gitDir(id string) string {
	a.fmTitleMu.Lock()
	f, ok := a.fmFolders[id]
	a.fmTitleMu.Unlock()
	if !ok || f[0] != "" {
		return ""
	}
	return f[1]
}

// readGit reads the git status of folder dir, which is nothing for one
// that is not a folder on disk, as one inside an archive. It may take a
// while, on a share that has gone, so never on the program's goroutine.
func (a *app) readGit(dir string) string {
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return ""
	}
	s, ok := gitstat.Read(a.ctx, dir)
	if !ok {
		return ""
	}
	return s.String()
}

// lookAtGit reads the git status of the folder file manager pane id
// shows, on a goroutine, unless one is out or it is not yet time: now
// asks at once.
func (a *app) lookAtGit(id string, now bool) {
	fp := a.fmPanes[id]
	if fp == nil || fp.done {
		return
	}
	g := &fp.git
	dir := a.gitDir(id)
	if dir == "" {
		g.dir = ""
		return
	}
	if g.asking || !now && dir == g.dir && time.Now().Before(g.next) {
		return
	}
	g.dir, g.asking = dir, true
	go func() {
		start := time.Now()
		text := a.readGit(dir)
		took := time.Since(start)
		a.events <- func() {
			// Nothing kakel draws changes: the file manager draws it.
			a.quiet = true
			fp := a.fmPanes[id]
			if fp == nil {
				return // the pane has gone
			}
			g := &fp.git
			g.asking = false
			g.next = time.Now().Add(max(gitEvery, 10*took))
			if g.dir != dir || a.gitDir(id) != dir {
				// The pane turned to another folder meanwhile.
				a.lookAtGit(id, true)
				return
			}
			a.showGit(fp, dir, text)
		}
	}()
}

// showGit puts text on the status bar of file manager pane fp, as the git
// status of folder dir, unless it says that already.
func (a *app) showGit(fp *fmPane, dir, text string) {
	g := &fp.git
	if dir == g.saidOn && text == g.said {
		return
	}
	g.saidOn, g.said = dir, text
	// Never waiting for the file manager, which may be waiting for kakel.
	go fp.w.SetNote("", dir, text)
}
