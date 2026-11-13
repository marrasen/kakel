package app

import (
	"crypto/rand"
	"errors"
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
// Which panes is written down per layout, per slot and per computer, in
// the settings beside the layouts and not in them: opening a layout
// never changes what was saved. A slot can have several written down,
// one for each copy of the layout open at once. A pane is only watched
// again in the run of the window it was opened in, which the window
// names by a token of its own each time it starts: the IDs of its panes
// are that run's, and one started again since gives the same IDs to
// other panes.
//
// Only a pane this kakel opened on that window is ever written down, or
// one it watches again from what was written: never one the window's
// own user opened and this one attached to by hand, which is theirs.

// newRunToken is a run's token: random, and nothing to read from it.
func newRunToken() string { return rand.Text() }

// mostPerSlot is the most panes written down for one slot of a layout on
// one computer: one for each copy of the layout open at once.
const mostPerSlot = 4

// farPane is what this kakel opened on another window for a pane here,
// or watches again there: the window, as connected to and as a record
// names it, the run of the window it is in, what the window calls it,
// the window's key for the machine it runs on, and its kind there; and
// for one a layout opened, the layout and the slot it fills.
type farPane struct {
	window        machines.ID
	kept          string
	run, far, key string
	kind          string
	layout, slot  string
}

// thisComputer names this computer in what a layout writes down of the
// panes it opened elsewhere, as its host name.
func thisComputer() string {
	name, _ := os.Hostname()
	return strings.ToLower(name)
}

// windowKeptOf is window as a record names it: its id when saved, and
// otherwise the address it was connected to at.
func (a *app) windowKeptOf(window machines.ID) string {
	if _, saved := a.machines.Saved(window); saved {
		return string(window)
	}
	if target, _, ok := a.machines.Quick(window); ok {
		return serveAddr(target)
	}
	return string(window)
}

// windowIs reports whether kept, as a record names a window, is window:
// by its id while it is saved, or by the address it is reached at,
// saved or not. A window saved since the record was written, or written
// while it was saved and reached by address now, is the same window.
func (a *app) windowIs(kept string, window machines.ID) bool {
	if kept == a.windowKeptOf(window) {
		return true
	}
	addr := ""
	if h, saved := a.machines.Saved(window); saved {
		addr = h.ServeAddr()
	} else if target, _, ok := a.machines.Quick(window); ok {
		addr = target
	}
	return addr != "" && sameWindow(serveAddr(kept), serveAddr(addr))
}

// noteFarPane notes pane id here as one this kakel opened on window w,
// connected as window, which calls it far.
func (a *app) noteFarPane(window machines.ID, w *machines.Window, id string, far serve.Attached) {
	if far.ID == "" {
		return
	}
	a.farPanes[id] = farPane{window: window, kept: a.windowKeptOf(window), run: w.Serve.Run(), far: far.ID, key: a.farHostOf(id), kind: far.Kind}
}

// farPaneMoved notes that pane id here, one this kakel opened on window
// w, watches what the window calls far now, as after it was started
// again there in a pane of its own.
func (a *app) farPaneMoved(w *machines.Window, id, far string) {
	if fp, ok := a.farPanes[id]; ok {
		fp.far, fp.run, fp.key = far, w.Serve.Run(), a.farHostOf(id)
		a.farPanes[id] = fp
	}
}

// recordsFor is what was written down of the panes slot s opened on
// other windows, from this computer, oldest first.
func (a *app) recordsFor(s *slot) []settings.LayoutPane {
	if s == nil || s.layout == "" || a.settings == nil {
		return nil
	}
	here := thisComputer()
	return slices.DeleteFunc(a.layoutPanes(), func(p settings.LayoutPane) bool {
		return p.Layout != s.layout || p.Slot != s.name || p.From != here
	})
}

// layoutPanes is what is written down of the panes layouts opened on
// other windows, with what is waiting to be written: one list for every
// read, whether written yet or not.
func (a *app) layoutPanes() []settings.LayoutPane {
	if a.settings == nil {
		return nil
	}
	have := a.settings.LayoutPanes()
	for _, e := range a.recordEdits {
		have = e(have)
	}
	return have
}

// hasRecord reports whether slot s opened a pane on another window that
// was written down, to watch again.
func (a *app) hasRecord(s *slot) bool { return len(a.recordsFor(s)) > 0 }

// claimOf names far pane far on window for claims: one being watched
// again, or attached to by hand, is claimed while that is on its way,
// so a second copy of a layout opening at the same moment, or a hand
// attach, does not take it too.
func claimOf(window machines.ID, far string) string { return string(window) + "\x00" + far }

// isClaimed reports whether far pane far on window is claimed.
func (a *app) isClaimed(window machines.ID, far string) bool {
	_, ok := a.claimed[claimOf(window, far)]
	return ok
}

// watchedHere reports whether a pane here watches what window w calls
// far.
func (a *app) watchedHere(w *machines.Window, far string) bool {
	pane, ok := w.Bound[far]
	return ok && a.has(pane)
}

// reattach watches again, in waiting pane id, a pane its slot opened on
// the window before, kept as r, on m, the window connected, and reports
// whether it does. Of what was written down for the slot it takes the
// first that the slot still holds the same of (the same window, terminal
// or command line), that is of the window's run now, that the window
// lists on the same machine and of the same kind, and that no pane here
// watches, nor is about to. What the window has listed and can no longer
// be watched again goes from what is written down. A pane whose program
// has ended is started again there first, as Reconnect does. Anything
// else, and anything that fails on the way, opens a new pane, as one
// never opened would.
//
// The record is only ever the window's own ID in that window's run, so
// the worst a window that lies can do is have this one show a pane of
// that same window, which a new pane is too.
func (a *app) reattach(id string, m machines.ID, r settings.SavedPane, still func() bool, fail failure) bool {
	recs := a.recordsFor(a.slots[id])
	if len(recs) == 0 {
		return false
	}
	window, key := dialOf(m), ""
	if _, k, far := m.Far(); far {
		key = k
	}
	w := a.machines.Get(window).Window
	if w == nil || !listed(w) {
		return false
	}
	run := w.Serve.Run()
	if run == "" {
		return false
	}
	var pick *settings.LayoutPane
	var stale []settings.LayoutPane
	var open serve.Open
	for _, rec := range recs {
		o, ok := w.Serve.OpenNamed(rec.Pane)
		switch {
		case !a.windowIs(rec.Window, window) || rec.Kind != r.Kind || rec.Line != r.Line || rec.Key != key,
			rec.Run != run, !ok, o.Key() != rec.Key, o.Kind != rec.FarKind:
			// The slot holds something else now, or the pane is not
			// there in this run: it can never be watched again.
			stale = append(stale, rec)
		case pick != nil, a.isClaimed(window, rec.Pane), a.watchedHere(w, rec.Pane):
		default:
			pick, open = &rec, o
		}
	}
	if len(stale) > 0 {
		a.queueRecords(func(have []settings.LayoutPane) []settings.LayoutPane {
			return slices.DeleteFunc(have, func(p settings.LayoutPane) bool { return slices.Contains(stale, p) })
		})
	}
	if pick == nil {
		return false
	}
	rec := *pick
	claim := claimOf(window, rec.Pane)
	a.claimed[claim] = id
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
			// Refused where it is no longer open there, as when it
			// closed after the list read here, for a new pane to open.
			sess, err = w.Serve.AttachIn(open, screen.Cols, screen.Rows)
		}
		a.events <- func() {
			a.starting--
			delete(a.claimed, claim)
			if !still() {
				// Only stopped watching: the pane there was open before
				// this layout came back to it, and stays open.
				if sess != nil {
					_ = sess.Close()
				}
				a.settleRecords()
				return
			}
			switch {
			case errors.Is(err, serve.ErrNotOpen) || errors.Is(err, serve.ErrCannotAttachIn):
				// Gone there since the list read here: a new one in its
				// place, as for one never opened.
				log.Printf("%q is not open on %s any more, so a new pane opens", open.Label, a.machines.Name(m))
				if errors.Is(err, serve.ErrNotOpen) {
					a.queueRecords(func(have []settings.LayoutPane) []settings.LayoutPane {
						return slices.DeleteFunc(have, func(p settings.LayoutPane) bool { return p == rec })
					})
				}
				a.startWindowNew(id, m, r, still, fail)
				return
			case err != nil:
				// Still open there, but not to be watched now, as one
				// whose machine the window is not connected to: said,
				// and Retry tries the same pane again. A new one opened
				// beside it would be left over once it can be.
				title := "Couldn't open a shell on " + a.machines.Name(m)
				if r.Kind == settings.LayoutCommand {
					title = "Couldn't run " + a.slotCalled(m, r) + " on " + a.machines.Name(m)
				}
				err = errors.New(serve.Line(err.Error()))
				a.failed(title, reason(err))
				fail(title, err, true)
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
			a.farPanes[pane] = farPane{window: window, kept: rec.Window, run: run, far: open.ID, key: key, kind: open.Kind, layout: rec.Layout, slot: rec.Slot}
			a.bindFar(w, open.ID, pane)
		}
	}()
	return true
}

// landedFor is what writes down the pane slot s, kept as r, opened on
// another window, once it is open and the window has named it: beside
// what was written for the slot before, the oldest going past
// mostPerSlot. Nothing is written for a slot of no layout, or a window
// that gives no run token, as one of an older build.
func (a *app) landedFor(s *slot, r settings.SavedPane) func(string) {
	if s == nil || s.layout == "" || a.settings == nil {
		return nil
	}
	return func(id string) {
		fp, ok := a.farPanes[id]
		if !ok || fp.run == "" {
			return
		}
		// As the slot is named now: a save while it waited names it
		// afresh, or lets it go.
		layout, name := s.layout, s.name
		if layout == "" {
			return
		}
		fp.layout, fp.slot = layout, name
		a.farPanes[id] = fp
		rec := settings.LayoutPane{Layout: layout, Slot: name, From: thisComputer(), Window: fp.kept,
			Run: fp.run, Pane: fp.far, Key: fp.key, FarKind: fp.kind, Kind: r.Kind, Line: r.Line}
		a.queueRecords(func(have []settings.LayoutPane) []settings.LayoutPane {
			have = append(have, rec)
			var mine []int
			for i, p := range have {
				if sameSlot(p, rec) {
					mine = append(mine, i)
				}
			}
			if extra := len(mine) - mostPerSlot; extra > 0 {
				drop := mine[:extra]
				out := have[:0]
				for i, p := range have {
					if !slices.Contains(drop, i) {
						out = append(out, p)
					}
				}
				have = out
			}
			return have
		})
	}
}

// sameSlot reports whether records a and b are of one slot of one
// layout, from one computer.
func sameSlot(a, b settings.LayoutPane) bool {
	return a.Layout == b.Layout && a.Slot == b.Slot && a.From == b.From
}

// queueRecords adds edit to what is to be written of the panes layouts
// opened on other windows, which is written once the layouts opening
// have settled: one write for a layout's panes, rather than one for each
// in the middle of its opening.
func (a *app) queueRecords(edit func([]settings.LayoutPane) []settings.LayoutPane) {
	a.recordEdits = append(a.recordEdits, edit)
	a.settleRecords()
}

// settleRecords writes what is waiting to be written of the panes
// layouts opened elsewhere, once no layout's pane is still starting.
func (a *app) settleRecords() {
	if len(a.recordEdits) == 0 {
		return
	}
	for _, window := range a.naming {
		// Not for a window that has listed what it has open with no run
		// token, one of a build that may never name a pane: nothing is
		// written of it anyway.
		w := a.machines.Get(window).Window
		if w == nil {
			// Gone: it will not name the pane now.
			continue
		}
		if !listed(w) || w.Serve.Run() != "" {
			return
		}
	}
	for _, s := range a.slots {
		if !s.failed {
			return
		}
	}
	a.flushRecords()
}

// flushRecords writes what is waiting to be written of the panes
// layouts opened on other windows, now. It is kept for convenience, so
// a failure to write it goes to the Window Log and is not shown.
func (a *app) flushRecords() {
	edits := a.recordEdits
	a.recordEdits = nil
	if len(edits) == 0 || a.settings == nil {
		return
	}
	a.recordWrites++
	err := a.settings.EditLayoutPanes(func(have []settings.LayoutPane) []settings.LayoutPane {
		for _, e := range edits {
			have = e(have)
		}
		return have
	})
	if err != nil {
		log.Printf("couldn't write down the panes layouts opened on other windows: %v", err)
	}
}

// editRecords writes edit to what is written down of the panes layouts
// opened on other windows now, with anything waiting to be written.
func (a *app) editRecords(edit func([]settings.LayoutPane) []settings.LayoutPane) {
	a.recordEdits = append(a.recordEdits, edit)
	a.flushRecords()
}

// recordSaved writes down, for layout l saved as layout from window
// panes whose slots names gives by pane, the panes it holds on other
// windows, in place of what was written down for it from this computer
// before: a slot still waiting keeps what was written for the slot it
// waits for, and is named afresh, so what it opens is written under its
// new name; and a pane this kakel opened on a window, or watches again
// there, that pane, whether that window is connected now or not. So a
// layout saved over another, its slots named afresh, comes back to the
// same panes, and a slot whose pane is not one of those any more comes
// back to none. Another copy of the layout open now keeps the panes it
// watches, under the new names of its slots, where the saved window
// still has them: renamed gives each slot's new name by its old one, as
// slotsRenamed finds them.
func (a *app) recordSaved(layout string, l settings.SavedLayout, names, renamed map[string]string) {
	if a.settings == nil {
		return
	}
	here := thisComputer()
	had := slices.DeleteFunc(a.layoutPanes(), func(p settings.LayoutPane) bool { return p.Layout != layout || p.From != here })
	var recs []settings.LayoutPane
	for id, name := range names {
		if s := a.slots[id]; s != nil {
			for _, old := range a.recordsFor(s) {
				old.Layout, old.Slot = layout, name
				recs = append(recs, old)
			}
			s.layout, s.name = layout, name
			continue
		}
		r := l.Panes[name]
		fp, ok := a.farPanes[id]
		if !ok || fp.run == "" || !r.Window || r.Kind == settings.LayoutFiles {
			continue
		}
		fp.layout, fp.slot = layout, name
		a.farPanes[id] = fp
		recs = append(recs, settings.LayoutPane{Layout: layout, Slot: name, From: here, Window: fp.kept,
			Run: fp.run, Pane: fp.far, Key: fp.key, FarKind: fp.kind, Kind: r.Kind, Line: r.Line})
	}
	for id, fp := range a.farPanes {
		if _, saved := names[id]; saved || fp.layout != layout {
			continue
		}
		name, ok := renamed[fp.slot]
		if !ok {
			continue
		}
		for _, rec := range had {
			if rec.Slot == fp.slot && rec.Pane == fp.far && rec.Run == fp.run {
				rec.Slot = name
				recs = append(recs, rec)
				fp.slot = name
				a.farPanes[id] = fp
				break
			}
		}
	}
	slices.SortStableFunc(recs, func(x, y settings.LayoutPane) int { return strings.Compare(x.Slot, y.Slot) })
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
	return slices.DeleteFunc(a.layoutPanes(), func(p settings.LayoutPane) bool { return p.Layout != layout })
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
			if !slices.Contains(have, r) {
				have = append(have, r)
			}
		}
		return have
	})
}
