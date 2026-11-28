package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/marrasen/gunim/driver"

	"github.com/marrasen/kakel/guest"
	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/words"
)

// A shared window's files: one folder of this computer, which the guest
// reaches with everything under it and nothing above it, to look at or
// to change as the share's Files says. Files starts at None, and can be
// changed at any time, as can the folder. What the guest does there goes
// into the Window Log.

// Intents for a shared window's files.
type (
	// SetLinkFiles sets what a share's guest may do with its folder.
	SetLinkFiles struct {
		ID    string
		Files guest.FilesMay
	}
	// ChooseLinkFolder asks the user for the folder of a share, or, with
	// no ID, for the next share of Window.
	ChooseLinkFolder struct {
		Window int
		ID     string
	}
)

// couldntShareFolder heads what is said when a folder could not be
// shared.
const couldntShareFolder = "Couldn't share the folder"

// errNoFolder refuses files with no folder to share.
var errNoFolder = errors.New("choose a folder first")

// linkFolder is the folder the next share of window win starts with: the
// one chosen for it, or else the one a Files pane in it shows on this
// computer, the one with the keyboard first.
func (a *app) linkFolder(win int) string {
	if dir := a.links.picked[win]; dir != "" {
		return dir
	}
	w := a.winByID(win)
	if w == nil || w.gone {
		return ""
	}
	panes := a.panesIn(w)
	focus := a.focusIn(w)
	best := ""
	for _, p := range panes {
		if p.Kind != KindFileManager || p.Machine != machines.Local || p.On != "" {
			continue
		}
		dir := a.filesFolder(p.ID, machines.Local)
		if dir == "" {
			continue
		}
		if p.ID == focus {
			return dir
		}
		if best == "" {
			best = dir
		}
	}
	return best
}

// linkFolders are the folders the next share of each window starts with.
func (a *app) linkFolders() map[int]string {
	out := map[int]string{}
	for _, w := range a.wins {
		if dir := a.linkFolder(w.id); dir != "" {
			out[w.id] = dir
		}
	}
	return out
}

// filesWith is what a guest may do with folder: nothing with none.
func filesWith(may guest.FilesMay, folder string) guest.FilesMay {
	if folder == "" {
		return guest.FilesNone
	}
	return may
}

// shareFiles has the host share what l says of its folder, and says so
// where it could not.
func (a *app) shareFiles(l *windowLink) {
	l.files = filesWith(l.files, l.folder)
	if l.host == nil {
		return
	}
	l.filesLog.setFolder(folderName(l.folder))
	if err := l.host.SetFiles(l.folder, l.files); err != nil {
		l.files = guest.FilesNone
		log.Printf("couldn't share the folder %s with a shared window: %v", l.folder, err)
		a.failed(couldntShareFolder, reason(err))
		a.problem()
	}
}

// setLinkFiles sets what a share's guest may do with its folder.
func (a *app) setLinkFiles(in SetLinkFiles) error {
	l := a.linkByID(in.ID)
	if l == nil || l.stands == LinkEnded || l.files == in.Files {
		return nil
	}
	if in.Files != guest.FilesNone && l.folder == "" {
		return errNoFolder
	}
	l.files = in.Files
	a.shareFiles(l)
	a.tellLink(l)
	a.sayFiles(l)
	return nil
}

// sayFiles writes in the Window Log what a share's guest may do with its
// folder now.
func (a *app) sayFiles(l *windowLink) {
	who := "the guest of a shared window"
	if l.status.Phase == guest.PhaseIn {
		who = guestForLog(l.status.Guest)
	}
	switch l.files {
	case guest.FilesNone:
		log.Printf("%s may use no files now", who)
	case guest.FilesLook:
		log.Printf("%s may look at the files in %s now", who, l.folder)
	case guest.FilesChange:
		log.Printf("%s may change the files in %s now", who, l.folder)
	}
}

// chooseLinkFolder asks the user for a folder, in the system's dialog, on
// a goroutine of its own, and shares it once it is chosen.
func (a *app) chooseLinkFolder(in ChooseLinkFolder) error {
	w := a.winByID(in.Window)
	if w == nil || w.gone {
		return errors.New("the window is closed")
	}
	start := a.linkFolder(in.Window)
	if l := a.linkByID(in.ID); l != nil {
		start = l.folder
	}
	c := w.c
	go func() {
		chosen, err := c.ChooseFiles(a.ctx, driver.ChooseOptions{Title: "Choose a Folder to Share", Folders: true, Folder: start})
		post(a, func() {
			switch {
			case errors.Is(err, driver.ErrNoChooser):
				a.failed("Couldn't choose a folder", "This system has no folder dialog.")
			case err != nil && !errors.Is(err, context.Canceled):
				log.Printf("couldn't choose a folder to share: %v", err)
				a.failed("Couldn't choose a folder", reason(err))
			case len(chosen) > 0:
				a.setLinkFolder(in, chosen[0])
			}
		})
	}()
	return nil
}

// setLinkFolder makes dir the folder of a share, or of the next share of
// a window.
func (a *app) setLinkFolder(in ChooseLinkFolder, dir string) {
	if in.ID == "" {
		if a.links.picked == nil {
			a.links.picked = map[int]string{}
		}
		a.links.picked[in.Window] = dir
		return
	}
	l := a.linkByID(in.ID)
	if l == nil || l.stands == LinkEnded || l.folder == dir {
		return
	}
	l.folder = dir
	a.shareFiles(l)
	a.tellLink(l)
	log.Printf("the folder of a shared window is %s now", dir)
}

// filesQuiet is how long nothing more is done before what a guest did is
// written, and filesMost how long a run of it is at most.
const (
	filesQuiet = 2 * time.Second
	filesMost  = 30 * time.Second

	// filesLines is how many lines a share writes in filesEvery, so a
	// guest that does much cannot push what came before it out of the
	// Window Log, which keeps its latest lines. What is past it is counted,
	// and said in a line once filesEvery is over.
	filesLines = 20
	filesEvery = time.Minute
)

// filesLog writes what a guest does in a share's folder in the Window
// Log, a line for each run of the same thing in the same folder: "uploaded
// 12 files to Reports". Never anything that is in a file. The share's
// goroutines tell it, and it writes from its own.
type filesLog struct {
	mu sync.Mutex
	// who is the guest, and folder what the folder is called.
	who, folder string
	run         *filesRun
	quiet       *time.Timer
	// write writes a line, log.Printf but in a test, and now is the time.
	write func(string)
	now   func() time.Time
	// lines are those written since since, and held the things done since
	// then that are not, which roll says once it is time.
	lines int
	since time.Time
	held  int
	roll  *time.Timer
}

// filesRun is the same thing done, one after the other, in one folder.
type filesRun struct {
	op    guest.FileOp
	dir   string
	n     int
	first guest.FileEvent
	since time.Time
}

func newFilesLog() *filesLog {
	return &filesLog{write: func(s string) { log.Print(s) }, now: time.Now}
}

// setWho says who the guest is, and setFolder what the folder is called.
// What was done before is written first, as it was.
func (f *filesLog) setWho(who string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if who != f.who {
		f.flushLocked()
		f.who = who
	}
}

func (f *filesLog) setFolder(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if name != f.folder {
		f.flushLocked()
		f.folder = name
	}
}

// add notes one thing done.
func (f *filesLog) add(e guest.FileEvent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	dir := path.Dir(e.Path)
	if r := f.run; r != nil && (r.op != e.Op || r.dir != dir || time.Since(r.since) > filesMost) {
		f.flushLocked()
	}
	if f.run == nil {
		f.run = &filesRun{op: e.Op, dir: dir, first: e, since: time.Now()}
	}
	f.run.n++
	if f.quiet != nil {
		f.quiet.Stop()
	}
	f.quiet = time.AfterFunc(filesQuiet, f.flush)
}

// flush writes what was done, and not yet written.
func (f *filesLog) flush() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.flushLocked()
}

func (f *filesLog) flushLocked() {
	if f.quiet != nil {
		f.quiet.Stop()
		f.quiet = nil
	}
	r := f.run
	if r == nil {
		return
	}
	f.run = nil
	f.writeLocked(f.line(r), r.n)
}

// writeLocked writes line, of n things done, unless filesLines have been
// written in this filesEvery.
func (f *filesLog) writeLocked(line string, n int) {
	now := f.clock()
	if now.Sub(f.since) >= filesEvery {
		f.sayHeldLocked()
		f.since = now
	}
	if f.lines < filesLines {
		f.lines++
		f.write(line)
		return
	}
	f.held += n
	if f.roll == nil {
		f.roll = time.AfterFunc(f.since.Add(filesEvery).Sub(now), f.rollOver)
	}
}

// sayHeldLocked writes how many things done were not written, if any,
// as the first line of the next filesEvery.
func (f *filesLog) sayHeldLocked() {
	if f.roll != nil {
		f.roll.Stop()
		f.roll = nil
	}
	f.lines = 0
	if f.held == 0 {
		return
	}
	f.write(fmt.Sprintf("%s did %d more things in a shared window", f.whoLocked(), f.held))
	f.held, f.lines = 0, 1
}

// rollOver says what was held, once its filesEvery is over.
func (f *filesLog) rollOver() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.held > 0 {
		f.sayHeldLocked()
		f.since = f.clock()
	}
}

// end writes all there is to write, as the share ends.
func (f *filesLog) end() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.flushLocked()
	f.sayHeldLocked()
}

func (f *filesLog) clock() time.Time {
	if f.now == nil {
		return time.Now()
	}
	return f.now()
}

// whoLocked is the guest, in a line.
func (f *filesLog) whoLocked() string {
	if f.who == "" {
		return "the guest"
	}
	return fmt.Sprintf("the guest %q", f.who)
}

// line is a run of things done, as the Window Log says it.
func (f *filesLog) line(r *filesRun) string {
	who := f.whoLocked()
	in := fmt.Sprintf("%q", f.place(r.dir))
	if r.n == 1 {
		e := r.first
		at := fmt.Sprintf("%q", f.place(e.Path))
		switch e.Op {
		case guest.FileDownloaded:
			return fmt.Sprintf("%s downloaded %s (%s) from a shared window", who, at, words.Size(e.Bytes))
		case guest.FileUploaded:
			return fmt.Sprintf("%s uploaded %s (%s) to a shared window", who, at, words.Size(e.Bytes))
		case guest.FileRenamed:
			return fmt.Sprintf("%s renamed %s to %q in a shared window", who, at, f.place(e.To))
		case guest.FileRemoved:
			return fmt.Sprintf("%s removed %s from a shared window", who, at)
		case guest.FolderCreated:
			return fmt.Sprintf("%s created the folder %s in a shared window", who, at)
		}
	}
	switch r.op {
	case guest.FileDownloaded:
		return fmt.Sprintf("%s downloaded %d files from %s in a shared window", who, r.n, in)
	case guest.FileUploaded:
		return fmt.Sprintf("%s uploaded %d files to %s in a shared window", who, r.n, in)
	case guest.FileRenamed:
		return fmt.Sprintf("%s renamed %d items in %s in a shared window", who, r.n, in)
	case guest.FileRemoved:
		return fmt.Sprintf("%s removed %d items from %s in a shared window", who, r.n, in)
	case guest.FolderCreated:
		return fmt.Sprintf("%s created %d folders in %s in a shared window", who, r.n, in)
	}
	return fmt.Sprintf("%s did %d things in %s in a shared window", who, r.n, in)
}

// place is a guest's path inside the folder, from the folder's name.
func (f *filesLog) place(p string) string {
	return strings.TrimSuffix(f.folder+"/"+strings.TrimPrefix(path.Clean("/"+p), "/"), "/")
}
