package app

import (
	"net"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/serve"
	"github.com/marrasen/kakel/settings"
	"github.com/marrasen/kakel/ui"
)

// deskLayout keeps, in b, a layout of a terminal and a command through
// the window desk, and returns its ID.
func deskLayout(t *testing.T, b *app) string {
	t.Helper()
	return keep(t, b, alone("Desk",
		throughDesk(settings.SavedPane{Kind: settings.LayoutTerminal}),
		throughDesk(settings.SavedPane{Kind: settings.LayoutCommand, Line: waitCommand(120)}),
	)).ID
}

// farIDs are what desk calls the pane each pane of window w watches, in
// the order the panes stand, "" for one it does not name.
func farIDs(b *app, w *ownWin) []string {
	desk := b.machines.Get("desk").Window
	var out []string
	for _, p := range b.panesIn(w) {
		id := ""
		if desk != nil {
			for far, pane := range desk.Bound {
				if pane == p.ID {
					id = far
				}
			}
		}
		out = append(out, id)
	}
	return out
}

// openLanded opens layout id in b and waits for every pane to start and
// be named by the window it watches, returning the layout's window.
func openLanded(t *testing.T, a, b *app, opened *atomic.Int32, id string) *ownWin {
	t.Helper()
	w := opens(t, b, opened, id)
	asked := 0
	pumpBoth(t, a, b, "the panes to start", func() bool {
		answering(b, &asked)
		return len(waitingIn(b, w)) == 0 && !slices.Contains(farIDs(b, w), "")
	})
	// What the window has open is told as the program publishes, which
	// its own loop does after each event and a test does here.
	listedThere(t, a, b)
	return w
}

// listedThere has a tell b what it has open, and waits for b to hear it.
func listedThere(t *testing.T, a, b *app) {
	t.Helper()
	a.publish()
	want := a.opens().Open
	pumpBoth(t, a, b, "the window's list", func() bool {
		desk := b.machines.Get("desk").Window
		return desk == nil || slices.Equal(desk.Serve.Opens(), want)
	})
}

// closeLanded closes window w of b, and its panes, as its close button
// does once that is answered, and lets what follows land.
func closeLanded(a, b *app, w *ownWin) {
	for _, p := range b.panesIn(w) {
		b.remove(p.ID)
	}
	b.letWindowGo(w)
	settleEvents(b)
	settleEvents(a)
	a.publish()
}

// A layout's panes on another window are watched again the next time it
// opens, while they are still open there: the same panes, what they
// showed still showing, and nothing more opened there, however often it
// is opened and closed.
func TestALayoutsPanesAreWatchedAgain(t *testing.T) {
	a, b, opened := windowLayoutApps(t)
	id := deskLayout(t, b)

	w := openLanded(t, a, b, opened, id)
	first := farIDs(b, w)
	had := len(a.st.Panes)
	// Shown in a's window, as they would be on a screen: a test window
	// lays nothing out, and a pane nothing shows has no screen to send.
	for _, far := range first {
		a.terminal(far).Layout(ui.Size{Cols: 80, Rows: 24})
	}
	b.terminal(b.panesIn(w)[0].ID).Paste("echo still-here\r")
	shownIn(t, a, b, b.panesIn(w)[0].ID, "still-here")
	if recs := b.recordsOf(id); len(recs) != 2 || recs[0].Run != a.runToken {
		t.Fatalf("written down: %+v", recs)
	}

	for round := 1; round <= 3; round++ {
		closeLanded(a, b, w)
		w = openLanded(t, a, b, opened, id)
		if got := farIDs(b, w); !slices.Equal(got, first) || len(a.st.Panes) != had {
			t.Fatalf("round %d: watches %v, want %v; the window has %d panes, want %d", round, got, first, len(a.st.Panes), had)
		}
	}
	shownIn(t, a, b, b.panesIn(w)[0].ID, "still-here")
	if len(b.st.Notices) != 0 {
		t.Fatalf("it said %+v", b.st.Notices)
	}
	// Saved again, it is the layout it was.
	again, _, _, err := b.saveLayout(SaveLayout{Window: w.id, Name: "Again"})
	if err != nil {
		t.Fatal(err)
	}
	if kept := b.settings.Layouts()[0]; !slices.Equal(farIDs(b, w), first) || len(again.Panes) != len(kept.Panes) {
		t.Fatalf("saved again, %+v, want %+v", again.Panes, kept.Panes)
	}
}

// A window started again since, or one whose pane was closed there, is
// given new panes: all of them for the one, only that slot's for the
// other.
func TestPanesGoneThereAreOpenedAgain(t *testing.T) {
	a, b, opened := windowLayoutApps(t)
	id := deskLayout(t, b)
	w := openLanded(t, a, b, opened, id)
	first := farIDs(b, w)
	had := len(a.st.Panes)
	closeLanded(a, b, w)

	// The terminal closed there.
	a.remove(first[0])
	pumpBoth(t, a, b, "the window to list it gone", func() bool { a.publish(); _, ok := b.machines.Get("desk").Window.Serve.OpenNamed(first[0]); return !ok })
	w = openLanded(t, a, b, opened, id)
	got := farIDs(b, w)
	if got[0] == first[0] || got[1] != first[1] || len(a.st.Panes) != had {
		t.Fatalf("with the terminal closed there, watches %v (was %v); the window has %d panes, want %d", got, first, len(a.st.Panes), had)
	}
	closeLanded(a, b, w)

	// The window as if started again: another run, whose IDs are not
	// the panes kept.
	a.runToken = newRunToken()
	pumpBoth(t, a, b, "the new run to be told", func() bool { a.publish(); return b.machines.Get("desk").Window.Serve.Run() == a.runToken })
	w = openLanded(t, a, b, opened, id)
	if again := farIDs(b, w); slices.Contains(again, got[0]) || slices.Contains(again, got[1]) || len(a.st.Panes) != had+2 {
		t.Fatalf("in another run, watches %v (was %v); the window has %d panes, want %d", again, got, len(a.st.Panes), had+2)
	}
}

// The layout open twice at once: the second window is given panes of
// its own, as one pane watched twice would take the keys of both; and
// the panes written down are the first's, which the layout comes back to
// once both are closed.
func TestALayoutOpenTwiceGetsNewPanes(t *testing.T) {
	a, b, opened := windowLayoutApps(t)
	id := deskLayout(t, b)
	one := openLanded(t, a, b, opened, id)
	first := farIDs(b, one)
	had := len(a.st.Panes)

	two := openLanded(t, a, b, opened, id)
	if second := farIDs(b, two); slices.Contains(second, first[0]) || slices.Contains(second, first[1]) || len(a.st.Panes) != had+2 {
		t.Fatalf("open twice, the second watches %v, the first %v; the window has %d panes", second, first, len(a.st.Panes))
	}
	closeLanded(a, b, two)
	closeLanded(a, b, one)

	w := openLanded(t, a, b, opened, id)
	if got := farIDs(b, w); !slices.Equal(got, first) {
		t.Fatalf("opened again, watches %v, want the first's %v", got, first)
	}
}

// Saved over, its slots named afresh, a layout comes back to the panes
// the window it was saved from watched; Remove takes what was written
// down with it, and Undo brings it back.
func TestPanesWrittenDownFollowTheLayout(t *testing.T) {
	a, b, opened := windowLayoutApps(t)
	id := deskLayout(t, b)
	w := openLanded(t, a, b, opened, id)
	command := farIDs(b, w)[1]
	// The terminal closed here, the command is the layout's first slot.
	b.remove(b.panesIn(w)[0].ID)
	if _, _, _, err := b.saveLayout(SaveLayout{Window: w.id, Name: "Desk", Replace: true}); err != nil {
		t.Fatal(err)
	}
	if recs := b.recordsOf(id); len(recs) != 1 || recs[0].Slot != "p1" || recs[0].Pane != command || recs[0].Kind != settings.LayoutCommand {
		t.Fatalf("saved over, written down: %+v", recs)
	}
	closeLanded(a, b, w)
	had := len(a.st.Panes)
	w = openLanded(t, a, b, opened, id)
	if got := farIDs(b, w); !slices.Equal(got, []string{command}) || len(a.st.Panes) != had {
		t.Fatalf("watches %v, want the command's %s; the window has %d panes, want %d", got, command, len(a.st.Panes), had)
	}

	recs := b.recordsOf(id)
	b.handle(RemoveLayout{ID: id})
	if left := b.recordsOf(id); len(left) != 0 {
		t.Fatalf("removed, still written down: %+v", left)
	}
	undo := b.st.Notices[len(b.st.Notices)-1].On.(RestoreLayout)
	b.handle(undo)
	if back := b.recordsOf(id); !slices.Equal(back, recs) {
		t.Fatalf("back, written down %+v, want %+v", back, recs)
	}
}

// A window that gives no token for its run, as one of an older build,
// is given new panes each time, as before, and nothing is written down.
func TestAWindowWithNoRunGetsNewPanes(t *testing.T) {
	a, b, opened := windowLayoutApps(t)
	a.serving.older = true
	if err := a.stopServing(); err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(b.st.Saved[0].ServeAddr())
	a.handle(StartServing{Port: port})
	id := deskLayout(t, b)
	had := len(a.st.Panes)
	for round := 1; round <= 2; round++ {
		w := opens(t, b, opened, id)
		asked := 0
		// A window of an older build takes the channel before it opens
		// what is asked, so its panes come a moment after these.
		pumpBoth(t, a, b, "the panes to start", func() bool {
			answering(b, &asked)
			return len(waitingIn(b, w)) == 0 && len(a.st.Panes) >= had+2*round
		})
		settleEvents(a)
		if len(a.st.Panes) != had+2*round {
			t.Fatalf("round %d: the window has %d panes, want %d: %+v", round, len(a.st.Panes), had+2*round, a.st.Panes)
		}
		closeLanded(a, b, w)
	}
	if recs := b.recordsOf(id); len(recs) != 0 {
		t.Fatalf("written down: %+v", recs)
	}
}

// restarted is b as if kakel had been started again: a program of its
// own with b's settings file and server list, b having let go of
// everything first.
func restarted(t *testing.T, b *app) (*app, *atomic.Int32) {
	t.Helper()
	for _, name := range b.machines.Windows() {
		_ = b.disconnectWindow(name)
	}
	for len(b.st.Panes) > 0 {
		b.remove(b.st.Panes[0].ID)
	}
	settleEvents(b)
	c, opened := openingApp(t)
	set, err := settings.Load(b.settings.Path())
	if err != nil {
		t.Fatal(err)
	}
	c.settings = set
	book, err := remote.LoadBook(b.book.Path())
	if err != nil {
		t.Fatal(err)
	}
	c.book, c.st.Saved = book, book.Hosts()
	t.Cleanup(func() {
		for _, name := range c.machines.Windows() {
			_ = c.disconnectWindow(name)
		}
	})
	return c, opened
}

// A layout opened at start, kakel started again here while the window
// it opened panes on runs on, watches them again; that window started
// again too, it opens new ones.
func TestALayoutAtStartWatchesItsPanesAgain(t *testing.T) {
	for _, farToo := range []bool{false, true} {
		name := "here"
		if farToo {
			name = "here and there"
		}
		t.Run("started again "+name, func(t *testing.T) {
			a, b, opened := windowLayoutApps(t)
			id := deskLayout(t, b)
			if err := b.settings.PutLayoutAtStart(id, true); err != nil {
				t.Fatal(err)
			}
			w := openLanded(t, a, b, opened, id)
			first := farIDs(b, w)
			had := len(a.st.Panes)

			c, _ := restarted(t, b)
			if farToo {
				a.runToken = newRunToken()
				a.publish()
			}
			if on, _ := c.openAtStart(); !on {
				t.Fatal("nothing opened at start")
			}
			var cw *ownWin
			asked := 0
			pumpBoth(t, a, c, "the panes to start", func() bool {
				answering(c, &asked)
				if len(c.wins) < 2 {
					return false
				}
				cw = c.wins[len(c.wins)-1]
				return len(c.panesIn(cw)) == 2 && len(waitingIn(c, cw)) == 0 && !slices.Contains(farIDs(c, cw), "")
			})
			got := farIDs(c, cw)
			switch {
			case !farToo && (!slices.Equal(got, first) || len(a.st.Panes) != had):
				t.Fatalf("watches %v, want %v; the window has %d panes, want %d", got, first, len(a.st.Panes), had)
			case farToo && (slices.Contains(got, first[0]) || slices.Contains(got, first[1]) || len(a.st.Panes) != had+2):
				t.Fatalf("in another run, watches %v (was %v); the window has %d panes", got, first, len(a.st.Panes))
			}
		})
	}
}

// A pane whose program ended there, still open there, is started again
// there and watched, as Reconnect does, rather than a new one opened.
func TestAnEndedPaneThereIsStartedAgain(t *testing.T) {
	a, b, opened := windowLayoutApps(t)
	id := deskLayout(t, b)
	w := openLanded(t, a, b, opened, id)
	first := farIDs(b, w)
	had := len(a.st.Panes)
	b.terminal(b.panesIn(w)[0].ID).Paste("exit\r")
	pumpBoth(t, a, b, "the shell to end there", func() bool {
		i := slices.IndexFunc(a.st.Panes, func(p Pane) bool { return p.ID == first[0] })
		return i >= 0 && a.st.Panes[i].Ended
	})
	closeLanded(a, b, w)
	listedThere(t, a, b)

	w = openLanded(t, a, b, opened, id)
	here := b.panesIn(w)[0].ID
	pumpBoth(t, a, b, "it to run again", func() bool { return !b.terminal(here).Exited() })
	if got := farIDs(b, w); !slices.Equal(got, first) || len(a.st.Panes) != had {
		t.Fatalf("watches %v, want %v; the window has %d panes, want %d", got, first, len(a.st.Panes), had)
	}
	if tt := a.terminal(first[0]); tt == nil || tt.Exited() {
		t.Fatal("it was not started again there")
	}
}

// A layout's window closed as soon as it opens, its panes coming back to
// those on the window, leaves those open there: they were open before
// it came back to them.
func TestClosingWhileWatchingAgainLeavesThemThere(t *testing.T) {
	a, b, opened := windowLayoutApps(t)
	id := deskLayout(t, b)
	w := openLanded(t, a, b, opened, id)
	first := farIDs(b, w)
	had := len(a.st.Panes)
	closeLanded(a, b, w)

	w = opens(t, b, opened, id)
	closeLanded(a, b, w)
	pumpBoth(t, a, b, "every start to land", func() bool { return b.starting == 0 })
	settleEvents(a)

	for _, far := range first {
		if !a.has(far) {
			t.Fatalf("%s was closed there", far)
		}
	}
	if len(a.st.Panes) != had || len(b.st.Panes) != 0 {
		t.Fatalf("the window has %d panes, want %d; b holds %+v", len(a.st.Panes), had, b.st.Panes)
	}
}

// reopened opens layout id in b as openLanded does, and closes it
// again, returning what it watched on the window.
func reopened(t *testing.T, a, b *app, opened *atomic.Int32, id string) []string {
	t.Helper()
	w := openLanded(t, a, b, opened, id)
	ids := farIDs(b, w)
	closeLanded(a, b, w)
	listedThere(t, a, b)
	return ids
}

// Opened twice in one go, before either comes back to the panes it
// opened before, the layout watches each far pane once: the second copy
// gets panes of its own. A pane that ended there is started again once.
func TestALayoutOpenedTwiceAtOnceWatchesEachPaneOnce(t *testing.T) {
	a, b, opened := windowLayoutApps(t)
	id := deskLayout(t, b)
	first := reopened(t, a, b, opened, id)
	had := len(a.st.Panes)
	// The terminal ended there: both copies would start it again.
	a.terminal(first[0]).Paste("exit\r")
	pumpBoth(t, a, b, "the shell to end there", func() bool {
		i := slices.IndexFunc(a.st.Panes, func(p Pane) bool { return p.ID == first[0] })
		return i >= 0 && a.st.Panes[i].Ended
	})
	listedThere(t, a, b)

	before := opened.Load()
	b.handle(OpenLayout{ID: id})
	b.handle(OpenLayout{ID: id})
	pumpBoth(t, a, b, "both windows", func() bool { return opened.Load() == before+2 && len(b.wins) >= 3 })
	one, two := b.wins[len(b.wins)-2], b.wins[len(b.wins)-1]
	asked := 0
	pumpBoth(t, a, b, "every pane to start", func() bool {
		answering(b, &asked)
		return len(waitingIn(b, one))+len(waitingIn(b, two)) == 0 &&
			!slices.Contains(farIDs(b, one), "") && !slices.Contains(farIDs(b, two), "")
	})
	all := append(farIDs(b, one), farIDs(b, two)...)
	seen := map[string]bool{}
	for _, far := range all {
		if seen[far] {
			t.Fatalf("%s is watched twice: %v", far, all)
		}
		seen[far] = true
	}
	if len(a.st.Panes) != had+2 {
		t.Fatalf("the window has %d panes, want %d", len(a.st.Panes), had+2)
	}
}

// Asked to start again a pane that runs, as when two windows asked, a
// window says so at once rather than waiting for a start that is not
// coming, holding up what else is asked of it.
func TestStartingAgainAPaneThatRunsAnswersAtOnce(t *testing.T) {
	a, _ := agentApp(t)
	done := make(chan error, 1)
	go func() { done <- a.startAgainFor(serve.Attached{ID: a.st.Panes[0].ID, Kind: "Terminal"}, false) }()
	var err error
	waitFor(t, a, "the answer", func() bool {
		select {
		case err = <-done:
			return true
		default:
			return false
		}
	})
	if err != nil {
		t.Fatal(err)
	}
}

// A slot whose command line changed since its pane was written down, as
// in a layout another computer saved over, opens one new pane, which is
// then the one watched again: the window does not fill with panes.
func TestAChangedSlotGetsOneNewPane(t *testing.T) {
	a, b, opened := windowLayoutApps(t)
	id := deskLayout(t, b)
	reopened(t, a, b, opened, id)
	l := b.settings.Layouts()[0]
	p := l.Panes["p2"]
	p.Line = waitCommand(90)
	l.Panes["p2"] = p
	if err := b.settings.ReplaceLayout(l); err != nil {
		t.Fatal(err)
	}
	had := len(a.st.Panes)
	for round := 1; round <= 3; round++ {
		reopened(t, a, b, opened, id)
		if len(a.st.Panes) != had+1 {
			t.Fatalf("round %d: the window has %d panes, want %d", round, len(a.st.Panes), had+1)
		}
	}
	for _, r := range b.recordsOf(id) {
		if r.Slot == "p2" && r.Line != p.Line {
			t.Fatalf("what can no longer be watched again is still written down: %+v", r)
		}
	}
}

// With one copy of a layout open, each copy opened beside it and closed
// comes back to the same panes the next time: the window does not fill
// with panes.
func TestASecondCopyComesBackToItsOwnPanes(t *testing.T) {
	a, b, opened := windowLayoutApps(t)
	id := deskLayout(t, b)
	openLanded(t, a, b, opened, id)
	second := reopened(t, a, b, opened, id)
	had := len(a.st.Panes)
	for round := 1; round <= 3; round++ {
		if got := reopened(t, a, b, opened, id); !slices.Equal(got, second) || len(a.st.Panes) != had {
			t.Fatalf("round %d: watches %v, want %v; the window has %d panes, want %d", round, got, second, len(a.st.Panes), had)
		}
	}
}

// A pane closed there after the list read here was sent: the slot gets
// a new pane, not one that says it is not there.
func TestAPaneClosedSinceTheListGetsANewOne(t *testing.T) {
	a, b, opened := windowLayoutApps(t)
	id := deskLayout(t, b)
	first := reopened(t, a, b, opened, id)
	// Closed there with nothing told: b's list still has it, as a test
	// publishes nothing unasked.
	a.remove(first[0])
	had := len(a.st.Panes)

	w := opens(t, b, opened, id)
	asked := 0
	pumpBoth(t, a, b, "the panes to start", func() bool {
		answering(b, &asked)
		return len(waitingIn(b, w)) == 0 && !slices.Contains(farIDs(b, w), "")
	})
	got := farIDs(b, w)
	if got[0] == first[0] || got[1] != first[1] || len(a.st.Panes) != had+1 || b.terminal(b.panesIn(w)[0].ID).Exited() {
		t.Fatalf("watches %v (was %v); the window has %d panes, want %d", got, first, len(a.st.Panes), had+1)
	}
}

// Saved over while its window is not connected, as after a laptop slept,
// a layout keeps the panes it watched there: the next time it comes back
// to them.
func TestSavingWhileDisconnectedKeepsThePanes(t *testing.T) {
	a, b, opened := windowLayoutApps(t)
	id := deskLayout(t, b)
	w := openLanded(t, a, b, opened, id)
	first := farIDs(b, w)
	recs := b.recordsOf(id)
	if err := b.disconnectWindow("desk"); err != nil {
		t.Fatal(err)
	}
	pumpBoth(t, a, b, "the window to go", func() bool { return len(b.machines.Windows()) == 0 })
	b.st.Notices = nil
	if _, _, _, err := b.saveLayout(SaveLayout{Window: w.id, Name: "Desk", Replace: true}); err != nil {
		t.Fatal(err)
	}
	bySlot := func(r []settings.LayoutPane) []settings.LayoutPane {
		r = slices.Clone(r)
		slices.SortFunc(r, func(x, y settings.LayoutPane) int { return strings.Compare(x.Slot, y.Slot) })
		return r
	}
	if got := b.recordsOf(id); !slices.Equal(bySlot(got), bySlot(recs)) {
		t.Fatalf("saved disconnected, written down %+v, want %+v", got, recs)
	}
	closeLanded(a, b, w)
	had := len(a.st.Panes)
	w = openLanded(t, a, b, opened, id)
	if got := farIDs(b, w); !slices.Equal(got, first) || len(a.st.Panes) != had {
		t.Fatalf("watches %v, want %v; the window has %d panes, want %d", got, first, len(a.st.Panes), had)
	}
}

// A pane the window's own user opened, attached to here by hand, is
// saved in the layout as any pane through the window is, but never
// written down to come back to: it is theirs, not this layout's.
func TestAPaneAttachedByHandIsNotWrittenDown(t *testing.T) {
	a, b, opened := windowLayoutApps(t)
	id := deskLayout(t, b)
	w := openLanded(t, a, b, opened, id)
	theirs := a.st.Panes[0].ID
	b.handle(AttachWindow{Window: "desk", ID: theirs})
	pumpBoth(t, a, b, "the pane attached", func() bool { return len(b.panesIn(w)) == 3 })
	l, _, _, err := b.saveLayout(SaveLayout{Window: w.id, Name: "Desk", Replace: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Panes) != 3 {
		t.Fatalf("saved %+v", l.Panes)
	}
	recs := b.recordsOf(id)
	if len(recs) != 2 || slices.ContainsFunc(recs, func(r settings.LayoutPane) bool { return r.Pane == theirs }) {
		t.Fatalf("written down %+v", recs)
	}
}

// A layout of several panes on another window writes down what it
// opened there once, as its opening settles, not once for each pane.
func TestALayoutsPanesAreWrittenDownOnce(t *testing.T) {
	a, b, opened := windowLayoutApps(t)
	id := keep(t, b, alone("Four",
		throughDesk(settings.SavedPane{Kind: settings.LayoutTerminal}),
		throughDesk(settings.SavedPane{Kind: settings.LayoutTerminal, Title: "two"}),
		throughDesk(settings.SavedPane{Kind: settings.LayoutCommand, Line: waitCommand(120)}),
		throughDesk(settings.SavedPane{Kind: settings.LayoutCommand, Line: waitCommand(121)}),
	)).ID
	openLanded(t, a, b, opened, id)
	if len(b.recordsOf(id)) != 4 || b.recordWrites != 1 {
		t.Fatalf("written %d times: %+v", b.recordWrites, b.recordsOf(id))
	}
}

// A pane on a machine beyond the window is watched again too.
func TestAPaneBeyondAWindowIsWatchedAgain(t *testing.T) {
	_, conn, _ := tunnelApp(t)
	a, b, opened := windowLayoutApps(t)
	a.machines.At("srv").Conn = conn
	pumpBoth(t, a, b, "srv listed", func() bool { a.publish(); return true })
	id := keep(t, b, alone("Beyond", throughDesk(settings.SavedPane{Kind: settings.LayoutTerminal, On: "srv", OnKey: "srv"}))).ID
	first := reopened(t, a, b, opened, id)
	had := len(a.st.Panes)
	if got := reopened(t, a, b, opened, id); !slices.Equal(got, first) || len(a.st.Panes) != had {
		t.Fatalf("watches %v, want %v; the window has %d panes, want %d", got, first, len(a.st.Panes), had)
	}
	if recs := b.recordsOf(id); len(recs) != 1 || recs[0].Key != "srv" {
		t.Fatalf("written down %+v", recs)
	}
}

// A layout brought back by Undo under another ID, its own having been
// given to another layout meanwhile, brings back what it had written
// down under that ID.
func TestUndoUnderANewIDBringsThePanesBack(t *testing.T) {
	a, b, opened := windowLayoutApps(t)
	id := deskLayout(t, b)
	reopened(t, a, b, opened, id)
	recs := b.recordsOf(id)
	b.handle(RemoveLayout{ID: id})
	undo := b.st.Notices[len(b.st.Notices)-1].On.(RestoreLayout)
	// Another layout takes its ID, under another name.
	other := keep(t, b, alone("Desk", settings.SavedPane{Kind: settings.LayoutTerminal, Command: plainShell()}))
	if other.ID != id {
		t.Fatalf("the other layout is %q", other.ID)
	}
	if err := b.renameLayout(RenameLayout{ID: id, Name: "Office"}); err != nil {
		t.Fatal(err)
	}
	b.handle(undo)
	var back string
	for _, l := range b.settings.Layouts() {
		if l.Name == "Desk" {
			back = l.ID
		}
	}
	if back == "" || back == id {
		t.Fatalf("brought back as %q", back)
	}
	got := b.recordsOf(back)
	if len(got) != len(recs) || len(b.recordsOf(id)) != 0 {
		t.Fatalf("written down under %s: %+v; under %s: %+v", back, got, id, b.recordsOf(id))
	}
}
