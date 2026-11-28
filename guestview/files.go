package guestview

import (
	"context"
	"errors"
	"log"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/filemanager"

	"github.com/marrasen/kakel/guest"
)

// The Files panes of the window: gunim's file manager, one in each, over
// the share's folder, which the host reaches through the guest's files
// channel. All of them read the folder through one channel, made as the
// first is shown and let go of as the last goes, or as the host shares
// no files. They are the program's: the view gives each a place, says
// so, and the file manager is mounted there.
//
// What the guest may do is the host's to say and to keep to: at Look the
// file manager offers nothing that changes the folder, and whatever the
// host says, it refuses what it does not allow. The folder's top is the
// top of all the file manager shows.

// filesMemory is the room the file manager takes of what the page holds,
// for its pictures, whatever is shown.
const filesMemory = 48 << 20

// filesRoom is what the budget counts the file manager's room under.
const filesRoom = "\x00files"

// filesLinger is how long a file manager stays once its pane shows it no
// more, for the page to see it go.
const filesLinger = time.Second

// FilesPlaced says the view has a place for the file manager of pane
// Pane, as the folder of count Folders shows: the node of ID At, which it
// is mounted under.
type FilesPlaced struct {
	Pane    string
	Folders uint32
	At      gunim.ID
}

// filesFlow is the program's half of the Files panes.
type filesFlow struct {
	// hub and fs are the file manager and the folder it reads, of the
	// folder of count folders, and none while no files are shown. stop
	// ends the hub.
	hub     *filemanager.Hub
	stop    context.CancelFunc
	fs      *sharedFS
	folders uint32
	may     guest.FilesMay
	// reached is the connection the folder's channel was made over, and
	// asking the one it is being asked over, 0 for none.
	reached, asking int
	panes           map[string]*filesPane
	// made counts the file managers made, for their IDs.
	made int
}

// filesPane is one pane's file manager.
type filesPane struct {
	win *filemanager.Window
	// host is the ID its views are mounted under.
	host string
	// at is where it is mounted, "" until it is.
	at gunim.ID
	// shows is the folder it shows, and picked the items selected there,
	// as it says them.
	mu     sync.Mutex
	shows  string
	picked []string
}

// showing is the folder p shows, and the items selected there.
func (p *filesPane) showing() (string, []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.shows, slices.Clone(p.picked)
}

// filesOn reports whether the window shows the share's folder in its
// Files panes: the host shares one, and the guest is in the window.
func (f *flow) filesOn() bool {
	return !f.over && f.st.Page == PageWindow && f.st.Files != guest.FilesNone && f.st.Folder != ""
}

// filesPanes are the window's Files panes.
func (f *flow) filesPanes() []guest.Pane {
	var out []guest.Pane
	for _, p := range f.st.Panes {
		if p.Kind == guest.KindFiles {
			out = append(out, p)
		}
	}
	return out
}

// tendFiles has a file manager in each Files pane while the folder is
// shown, over the folder as it is shared now, and none once it is not.
func (f *flow) tendFiles() {
	ff := &f.files
	panes := f.filesPanes()
	switch {
	case !f.filesOn() || len(panes) == 0:
		f.shutFiles()
		return
	case ff.hub != nil && ff.folders != f.st.Folders:
		// Another folder: what showed the one before goes.
		f.shutFiles()
	}
	if ff.hub == nil {
		ctx, stop := context.WithCancel(f.ctx)
		ff.hub, ff.stop, ff.folders = filemanager.NewHub(ctx, nil), stop, f.st.Folders
		ff.fs = newSharedFS("kakel-share:"+strconv.FormatUint(uint64(ff.folders), 10), f.st.Folder)
		ff.panes, ff.may = map[string]*filesPane{}, f.st.Files
		f.budget.hold(filesRoom, filesMemory)
	}
	if f.st.Files != ff.may {
		ff.may = f.st.Files
		for _, p := range ff.panes {
			p.win.SetReadOnly(ff.may != guest.FilesChange)
		}
	}
	f.reachFiles()
	here := map[string]bool{}
	for _, pn := range panes {
		here[pn.ID] = true
		// Made once the folder is reached, so the first it reads is there.
		if ff.panes[pn.ID] == nil && ff.reached != 0 {
			f.openFiles(pn)
		}
	}
	for id, p := range ff.panes {
		if !here[id] {
			delete(ff.panes, id)
			f.after(filesLinger, p.win.Stop)
		}
	}
}

// reachFiles asks the host for the folder's channel, over the connection
// there is, where the folder has none over it.
func (f *flow) reachFiles() {
	ff := &f.files
	s, n := f.sess, f.dial
	if s == nil || ff.reached == n || ff.asking == n {
		return
	}
	ff.asking = n
	fs, folders := ff.fs, ff.folders
	go func() {
		c, err := s.Files(f.ctx)
		f.post(func() {
			if ff.asking == n {
				ff.asking = 0
			}
			switch {
			case err != nil:
				if !errors.Is(err, context.Canceled) && !errors.Is(err, guest.ErrLost) && !errors.Is(err, guest.ErrNoFiles) {
					log.Printf("couldn't reach the shared folder: %v", err)
				}
			case ff.fs != fs || ff.folders != folders || f.dial != n || f.sess != s:
				_ = c.Close()
			default:
				ff.reached = n
				fs.set(c)
				f.tendFiles()
			}
		})
	}()
}

// openFiles makes the file manager of Files pane pn, in its folder where
// the host says it is in one.
func (f *flow) openFiles(pn guest.Pane) {
	ff := &f.files
	ff.made++
	dir := "/"
	if pn.Folder != "" {
		dir = pn.Folder
	}
	o := filemanager.Options{
		FS: ff.fs, Dir: dir, Top: "/", TopName: f.st.Folder, ReadOnly: ff.may != guest.FilesChange,
		Memory: filesMemory, PrefsPath: f.cfg.FilesPrefs, Name: "Files",
		Log: func(line string) { log.Print(line) },
	}
	p := &filesPane{host: "kakel-files-" + strconv.Itoa(ff.made)}
	host := filemanager.PaneHost{
		ID: p.host,
		Folder: func(_, path string) {
			p.mu.Lock()
			defer p.mu.Unlock()
			p.shows = path
		},
		Selected: func(_ string, paths []string) {
			p.mu.Lock()
			defer p.mu.Unlock()
			p.picked = slices.Clone(paths)
		},
	}
	win, err := ff.hub.NewPane(o, host)
	if err != nil {
		log.Printf("couldn't show the shared folder: %v", err)
		return
	}
	p.win = win
	ff.panes[pn.ID] = p
}

// placed hears that the view has a place for a file manager.
func (f *flow) placed(in FilesPlaced) {
	ff := &f.files
	p := ff.panes[in.Pane]
	if p == nil || in.Folders != ff.folders {
		return
	}
	p.at = in.At
	p.win.Attach(f.c, in.At)
}

// shutFiles lets go of the file managers, the folder's channel and their
// room, once the page has seen them go.
func (f *flow) shutFiles() {
	ff := &f.files
	if ff.hub == nil {
		return
	}
	wins := make([]*filemanager.Window, 0, len(ff.panes))
	for _, p := range ff.panes {
		wins = append(wins, p.win)
	}
	fs, stop := ff.fs, ff.stop
	*ff = filesFlow{made: ff.made}
	f.budget.drop(filesRoom)
	f.after(filesLinger, func() {
		for _, w := range wins {
			w.Stop()
		}
		stop()
		fs.set(nil)
	})
}

// filesLost hears that the connection is lost: the folder's channel went
// with it, and is asked for again over the next.
func (f *flow) filesLost() {
	ff := &f.files
	ff.reached, ff.asking = 0, 0
	if ff.fs != nil {
		ff.fs.set(nil)
	}
}

// deliverFiles hands an intent of a file manager's views to it, and
// reports whether it was one's.
func (f *flow) deliverFiles(e gunim.Envelope) bool {
	for _, p := range f.files.panes {
		if p.win.Deliver(e) {
			return true
		}
	}
	return false
}
