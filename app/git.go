package app

import (
	"time"

	"github.com/marrasen/kakel/gitstat"
)

// gitEvery is how often the status line's git status is read again, at
// most: a repository whose status takes long is read less often.
const gitEvery = 3 * time.Second

// gitWatch is the status line's git status: the pane and folder it was
// last read for, what it said, whether a read is out, and when the next
// may go.
type gitWatch struct {
	pane, dir string
	said      string
	asking    bool
	next      time.Time
}

// startGit reads the git status of the pane with the keyboard now, and
// again every gitEvery while kakel runs.
func (a *app) startGit() {
	time.AfterFunc(gitEvery, func() {
		a.events <- func() {
			if a.gone {
				return
			}
			changed := a.lookAtGit(false)
			a.startGit()
			// Asking changes nothing shown: an answer may.
			a.quiet = !changed
		}
	})
}

// gitFocusMoved reads the git status again at once when the keyboard has
// gone to another pane, rather than at the next look.
func (a *app) gitFocusMoved() {
	if a.st.Focus != a.git.pane {
		a.lookAtGit(true)
	}
}

// lookAtGit reads the git status of the folder of the terminal pane with
// the keyboard, on this computer, on a goroutine, unless one is out or
// it is not yet time: now asks at once. It reports whether the status
// line changed, as it does at once for a pane in no folder known.
func (a *app) lookAtGit(now bool) bool {
	pane := a.st.Focus
	a.git.pane = pane
	dir := ""
	if a.kindOfPane(pane) == KindTerminal {
		dir = a.dirHere()
	}
	if dir == "" {
		a.git.dir = ""
		return a.showGit("")
	}
	if a.git.asking || !now && dir == a.git.dir && time.Now().Before(a.git.next) {
		return false
	}
	a.git.dir, a.git.asking = dir, true
	go func() {
		start := time.Now()
		s, ok := gitstat.Read(a.ctx, dir)
		took := time.Since(start)
		a.events <- func() {
			a.git.asking = false
			a.git.next = time.Now().Add(max(gitEvery, 10*took))
			if a.git.dir != dir {
				return // the keyboard moved on meanwhile
			}
			text := ""
			if ok {
				text = s.String()
			}
			a.quiet = !a.showGit(text)
		}
	}()
	return false
}

// showGit puts text on the status line as the git status, and reports
// whether that changed it.
func (a *app) showGit(text string) bool {
	if text == a.git.said {
		return false
	}
	a.git.said = text
	a.say("git", text)
	return true
}
