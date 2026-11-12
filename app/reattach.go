package app

import (
	"crypto/rand"
	"log"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/screen"
	"github.com/marrasen/kakel/serve"
	"github.com/marrasen/kakel/session"
	"github.com/marrasen/kakel/settings"
)

// Watching a layout's panes on another window again. A pane a layout
// opens through another kakel window is opened there, in a pane of that
// window's own, which stays open there when the pane here closes. So the
// next time the layout opens, a slot whose pane is still open there
// watches it again, its program and its screen as they are, rather than
// open another beside it; one gone opens a new one, as before.
//
// Which pane is written down per layout, per slot and per computer, in
// the settings beside the layouts and not in them: opening a layout
// never changes what was saved. A pane is only watched again in the run
// of the window it was opened in, which the window names by a token of
// its own each time it starts: the IDs of its panes are that run's, and
// one started again since gives the same IDs to other panes.

// newRunToken is a run's token: random, and nothing to read from it.
func newRunToken() string { return rand.Text() }

// thisComputer names this computer in what a layout writes down of the
// panes it opened elsewhere, as its host name.
func thisComputer() string {
	name, _ := os.Hostname()
	return strings.ToLower(name)
}

// windowKeptAs is how a layout's pane kept as r names its window in what
// is written down of it: a saved window by its id, and one connected to
// by address alone by that address.
func windowKeptAs(r settings.SavedPane) string {
	if r.HostID != "" {
		return r.HostID
	}
	return serveAddr(r.Host)
}

// recordOf is what was written down of the pane slot s opened on another
// window, from this computer, and false for none.
func (a *app) recordOf(s *slot) (settings.LayoutPane, bool) {
	if s == nil || s.layout == "" || a.settings == nil {
		return settings.LayoutPane{}, false
	}
	here := thisComputer()
	for _, p := range a.settings.LayoutPanes() {
		if p.Layout == s.layout && p.Slot == s.name && p.From == here {
			return p, true
		}
	}
	return settings.LayoutPane{}, false
}

// hasRecord reports whether slot s opened a pane on another window that
// was written down, to watch again.
func (a *app) hasRecord(s *slot) bool {
	_, ok := a.recordOf(s)
	return ok
}

// reattach watches again, in waiting pane id, the pane its slot opened
// on the window last time, kept as r, on m, the window connected and
// listing what it has open, and reports whether it does. It does only
// where all of these hold: the slot holds what it held then, the same
// window, terminal or command line; the window is in the run it was
// opened in; the window lists the pane, on the machine and of the kind
// it was; and no pane here watches it already, as when the layout is
// open twice. A pane whose program has ended is started again there
// first, as Reconnect does. Anything else, and anything that fails on
// the way, opens a new pane, as one never opened would; the record is
// only ever the window's own ID in that window's run, so the worst it
// can do is show a pane of the same window, which a new pane is too.
func (a *app) reattach(id string, m machines.ID, r settings.SavedPane, still func() bool, fail failure) bool {
	rec, ok := a.recordOf(a.slots[id])
	if !ok || rec.Window != windowKeptAs(r) || rec.Kind != r.Kind || rec.Line != r.Line {
		return false
	}
	window, key := dialOf(m), ""
	if _, k, far := m.Far(); far {
		key = k
	}
	w := a.machines.Get(window).Window
	if w == nil {
		return false
	}
	if run := w.Serve.Run(); run == "" || run != rec.Run {
		return false
	}
	open, listed := w.Serve.OpenNamed(rec.Pane)
	// Its kind, as the window said it when the pane opened, says it has
	// a screen: its size can be none while nothing there shows it.
	if !listed || rec.Key != key || open.Key() != key || open.Kind != rec.FarKind {
		return false
	}
	if pane, ok := w.Bound[open.ID]; ok && a.has(pane) {
		return false
	}
	a.starting++
	go func() {
		var err error
		if open.Ended {
			// Started again there, in its own pane, as Reconnect does,
			// over a connection the window holds.
			err = w.Serve.StartAgainConnected(serve.Attached{ID: open.ID, Host: open.Key(), Kind: open.Kind})
		}
		var sess session.Session
		if err == nil {
			sess, err = w.Serve.Attach(open, screen.Cols, screen.Rows)
		}
		a.events <- func() {
			a.starting--
			if !still() {
				// Only stopped watching: the pane there was open before
				// this layout came back to it, and stays open.
				if sess != nil {
					_ = sess.Close()
				}
				return
			}
			if err != nil {
				log.Printf("couldn't watch %q on %s again, so a new pane opens: %v", open.Label, a.machines.Name(m), err)
				a.startWindowNew(id, m, r, still, fail)
				return
			}
			a.next++
			pane := "p" + strconv.Itoa(a.next)
			if r.Kind == settings.LayoutCommand {
				// To run again, as any command pane is.
				a.commands[pane] = command{argv: splitLine(r.Line), dir: r.Folder}
			}
			p := a.paneOn(m, Pane{ID: pane, Title: a.freeTitle(a.slotCalled(m, r)), Command: r.Kind == settings.LayoutCommand})
			a.addPane(p, screen.Open(sess, a.palette, a.withLinks(a.hooks(pane), pane, m)), Placement{Instead: id})
			a.bindFar(w, open.ID, pane)
		}
	}()
	return true
}

// landedFor is what writes down the pane slot s, kept as r, opened on
// another window, once it is open, to watch again: nothing for a slot of
// no layout, or a window that gives no run token, as one of an older
// build. The pane written down already stays while it is still open
// there, as when the layout is open twice: the pane it names is the one
// the layout came back to first.
func (a *app) landedFor(s *slot, r settings.SavedPane) func(string, serve.Attached) {
	if s == nil || s.layout == "" || a.settings == nil {
		return nil
	}
	layout, name := s.layout, s.name
	return func(id string, far serve.Attached) {
		window := a.machineOf(id)
		w := a.machines.Get(window).Window
		if w == nil || far.ID == "" {
			return
		}
		run := w.Serve.Run()
		if run == "" {
			return
		}
		rec := settings.LayoutPane{Layout: layout, Slot: name, From: thisComputer(), Window: windowKeptAs(r),
			Run: run, Pane: far.ID, Key: a.farHostOf(id), FarKind: far.Kind, Kind: r.Kind, Line: r.Line}
		a.editRecords(func(have []settings.LayoutPane) []settings.LayoutPane {
			i := slices.IndexFunc(have, func(p settings.LayoutPane) bool { return sameSlot(p, rec) })
			switch {
			case i < 0:
				return append(have, rec)
			case a.stillThere(have[i]):
				return have
			}
			have[i] = rec
			return have
		})
	}
}

// sameSlot reports whether records a and b are of one slot of one
// layout, from one computer.
func sameSlot(a, b settings.LayoutPane) bool {
	return a.Layout == b.Layout && a.Slot == b.Slot && a.From == b.From
}

// stillThere reports whether the pane record names is still open on its
// window, in the run it was opened in, as far as this window can tell.
func (a *app) stillThere(rec settings.LayoutPane) bool {
	m := machines.ID(rec.Window)
	if _, saved := a.machines.Saved(m); !saved {
		m = a.windowAt(rec.Window)
	}
	w := a.machines.Get(m).Window
	if w == nil || w.Serve.Run() != rec.Run {
		return false
	}
	_, ok := w.Serve.OpenNamed(rec.Pane)
	return ok
}

// editRecords changes what is written down of the panes layouts opened
// on other windows. It is kept for convenience, so a failure to write
// it goes to the Window Log and is not shown.
func (a *app) editRecords(edit func([]settings.LayoutPane) []settings.LayoutPane) {
	if a.settings == nil {
		return
	}
	if err := a.settings.EditLayoutPanes(edit); err != nil {
		log.Printf("couldn't write down the panes layouts opened on other windows: %v", err)
	}
}

// recordSaved writes down, for layout l saved as layout from window
// panes whose slots names gives by pane, the panes it holds on other
// windows, in place of what was written down for it from this computer
// before: a slot still waiting keeps what was written for the slot it
// waits for, and a pane through a window, watching a pane there in a
// run the window names, that pane. So a layout saved over another, its
// slots named afresh, comes back to the same panes, and a slot whose
// pane is not one of those any more comes back to none.
func (a *app) recordSaved(layout string, l settings.SavedLayout, names map[string]string) {
	if a.settings == nil {
		return
	}
	here := thisComputer()
	var recs []settings.LayoutPane
	for id, name := range names {
		if s := a.slots[id]; s != nil {
			if old, ok := a.recordOf(s); ok {
				old.Layout, old.Slot = layout, name
				recs = append(recs, old)
			}
			continue
		}
		r := l.Panes[name]
		i := slices.IndexFunc(a.st.Panes, func(p Pane) bool { return p.ID == id })
		if i < 0 || !r.Window || r.Kind == settings.LayoutFiles {
			continue
		}
		p := a.st.Panes[i]
		w := a.machines.Get(p.Machine).Window
		if w == nil || w.Serve.Run() == "" {
			continue
		}
		for far, pane := range w.Bound {
			if pane != id {
				continue
			}
			if open, ok := w.Serve.OpenNamed(far); ok {
				recs = append(recs, settings.LayoutPane{Layout: layout, Slot: name, From: here, Window: windowKeptAs(r),
					Run: w.Serve.Run(), Pane: far, Key: open.Key(), FarKind: open.Kind, Kind: r.Kind, Line: r.Line})
			}
		}
	}
	slices.SortFunc(recs, func(x, y settings.LayoutPane) int { return strings.Compare(x.Slot, y.Slot) })
	a.editRecords(func(have []settings.LayoutPane) []settings.LayoutPane {
		have = slices.DeleteFunc(have, func(p settings.LayoutPane) bool { return p.Layout == layout && p.From == here })
		return append(have, recs...)
	})
}

// recordsOf is what is written down of the panes layout opened on other
// windows, from every computer.
func (a *app) recordsOf(layout string) []settings.LayoutPane {
	if a.settings == nil {
		return nil
	}
	return slices.DeleteFunc(a.settings.LayoutPanes(), func(p settings.LayoutPane) bool { return p.Layout != layout })
}

// restoreRecords puts back recs, written down for a layout removed, for
// it back as layout, its ID now.
func (a *app) restoreRecords(layout string, recs []settings.LayoutPane) {
	if len(recs) == 0 {
		return
	}
	a.editRecords(func(have []settings.LayoutPane) []settings.LayoutPane {
		for _, r := range recs {
			r.Layout = layout
			if !slices.ContainsFunc(have, func(p settings.LayoutPane) bool { return sameSlot(p, r) }) {
				have = append(have, r)
			}
		}
		return have
	})
}
