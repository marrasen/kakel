package app

import (
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/marrasen/gunim/driver"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/kakel/jobs"
	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/settings"
	"github.com/marrasen/kakel/words"
)

// Closing the window, however it is asked for: the menu, the key, or
// the close button on the title bar. It asks first while anything is
// open, and says what: a window holding a copy half
// done and three shells is not one to lose to a slip of the mouse.

// askToQuit asks before the window goes, and says what is still open.
func (a *app) askToQuit() {
	if a.leaving {
		// Already asking.
		return
	}
	open := a.whatIsOpen()
	if len(open) == 0 {
		a.exitNow()
		return
	}
	a.leaving = true
	go func() {
		_, err := a.ask(a.ctx, Ask{
			Title: "Exit kakel?", Text: "Still open: " + listOf(open) + ".",
			Yes: "Exit", Danger: true,
		})
		a.events <- func() {
			a.leaving = false
			if err == nil {
				a.exitNow()
			} else {
				// A restart asked for is not done by a quit long after.
				restartInto = ""
			}
		}
	}()
}

// exitNow closes every pane, which closes the window.
func (a *app) exitNow() {
	a.takeSecretBack()
	a.leave()
}

// leave lets every window animate out with what it shows, and closes
// the panes once they have gone: see the run loop. Nothing is
// published meanwhile, so each window leaves as the user last saw it.
func (a *app) leave() {
	if a.gone {
		return
	}
	a.gone = true
	a.closeLauncher()
	a.closeFileManager()
	a.closeOverview()
	a.keepPlaces()
	for _, w := range a.wins {
		if !w.gone {
			w.c.Leave()
		}
	}
}

// closeAll closes every pane, as the window goes.
func (a *app) closeAll() {
	for len(a.st.Panes) > 0 {
		a.remove(a.st.Panes[0].ID)
	}
	a.hangUp()
}

// hangUpWait is the longest the program waits, on its way out, for its
// connections to close politely.
const hangUpWait = 2 * time.Second

// hangUp closes every connection, to servers, through jump hosts and to
// other windows, so each far end hears goodbye rather than a socket that
// went. It waits hangUpWait at most.
func (a *app) hangUp() {
	var closers []func() error
	a.machines.Each(func(_ machines.ID, m *machines.Machine) {
		if m.Conn != nil {
			closers = append(closers, m.Conn.Close)
		}
		if w := m.Window; w != nil {
			w.Leaving = true
			closers = append(closers, w.Serve.Close)
		}
	})
	for _, c := range a.machines.Hops() {
		closers = append(closers, c.Close)
	}
	if len(closers) == 0 {
		return
	}
	var wg sync.WaitGroup
	for _, c := range closers {
		wg.Go(func() { _ = c() })
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(hangUpWait):
	}
}

// whatIsOpen is what the window would take with it, worst first: file
// work part way through is the one thing that cannot be started again
// where it left off. A connection with nothing on it is not asked
// about: closing it loses nothing.
func (a *app) whatIsOpen() []string {
	var out []string
	// Copies and deletes running, kakel's own and those of the file
	// manager panes: these, a file manager counts itself.
	copies, deletes := 0, 0
	for _, r := range a.running {
		if r.quiet || r.job.Progress().Done {
			continue
		}
		if r.op.Kind == jobs.Delete {
			deletes++
		} else {
			copies++
		}
	}
	copies += a.fileOpsIn(nil)
	if copies > 0 {
		out = append(out, words.ManyOf(copies, "copy running", "copies running"))
	}
	if deletes > 0 {
		out = append(out, words.ManyOf(deletes, "delete running", "deletes running"))
	}
	// The tool panes have nothing to lose, and a connection with nothing
	// on it nothing to end.
	panes := slices.DeleteFunc(slices.Clone(a.st.Panes), func(p Pane) bool { return isToolKind(p.Kind) })
	if n := len(panes); n > 0 {
		out = append(out, words.ManyOf(n, "pane", "panes"))
	}
	if n := len(a.tunnels); n > 0 {
		out = append(out, words.ManyOf(n, "tunnel", "tunnels"))
	}
	if len(a.agents.by) > 0 {
		out = append(out, "an agent share")
	}
	if a.serving.server != nil {
		out = append(out, "this window, served")
	}
	return out
}

// listOf writes a few things as a person would say them.
func listOf(what []string) string {
	switch len(what) {
	case 0:
		return "nothing"
	case 1:
		return what[0]
	}
	return strings.Join(what[:len(what)-1], ", ") + " and " + what[len(what)-1]
}

// keepPlaces writes down where the windows are, and how big, and
// whether they are maximized, for the next start to open them there:
// the window in front, or else the first, of the windows with a
// terminal, and of the file manager windows. A minimized one is kept as
// it was before.
func (a *app) keepPlaces() {
	kept := map[bool]bool{}
	for _, w := range append([]*ownWin{a.cur}, a.liveWins()...) {
		if w == nil || w.gone || kept[a.filesWin(w)] {
			continue
		}
		kept[a.filesWin(w)] = true
		a.keepPlaceOf(w)
	}
}

// keepPlaceOf keeps where w is: for the next file manager window when
// w is one, and else for the next window opened with none open.
func (a *app) keepPlaceOf(w *ownWin) {
	if a.settings == nil || w == nil {
		return
	}
	p, ok := a.placement(w)
	if !ok || p.Bounds.Empty() {
		return
	}
	b := p.Bounds
	place := settings.WindowPlace{X: b.Min.X, Y: b.Min.Y, W: b.Size().W, H: b.Size().H, Maximized: p.Maximized}
	if a.filesWin(w) {
		a.keep("where the file manager window is", a.settings.PutFilesWindow(place))
		return
	}
	a.keep("where the window is", a.settings.PutWindow(place))
}

// placement is where w is on the screen, as its window says.
func (a *app) placement(w *ownWin) (driver.Placement, bool) {
	if a.placed != nil {
		return a.placed(w)
	}
	if w.gw == nil {
		return driver.Placement{}, false
	}
	return w.gw.Placement()
}

// filesWin reports whether w is a file manager window: it holds file
// manager panes alone, or, empty, held them alone as its last pane
// closed.
func (a *app) filesWin(w *ownWin) bool {
	panes := a.panesIn(w)
	if len(panes) == 0 {
		return w.files
	}
	return !slices.ContainsFunc(panes, func(p Pane) bool { return p.Kind != KindFileManager })
}

// cascade is how far a file manager window opens from another already
// where the last one was, in the screen's coordinates.
const cascade = 32

// filesPlace is where a new file manager window opens: where the last
// one was, as it closed, a step down and right of each window already
// there; or nil, where none was written down.
func (a *app) filesPlace() *driver.Placement {
	if a.settings == nil {
		return nil
	}
	kept, ok := a.settings.FilesWindow()
	if !ok || kept.W <= 0 || kept.H <= 0 {
		return nil
	}
	p := &driver.Placement{Bounds: geom.Rc(kept.X, kept.Y, kept.W, kept.H), Maximized: kept.Maximized}
	if p.Maximized {
		return p
	}
	for moved := true; moved; {
		moved = false
		for _, w := range a.liveWins() {
			o, ok := a.placement(w)
			if d := o.Bounds.Min.Sub(p.Bounds.Min); ok && !o.Maximized && abs32(d.X) < cascade/2 && abs32(d.Y) < cascade/2 {
				p.Bounds = p.Bounds.Add(geom.Pt(cascade, cascade))
				moved = true
			}
		}
	}
	return p
}

func abs32(v float32) float32 { return max(v, -v) }
