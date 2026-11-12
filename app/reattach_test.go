package app

import (
	"net"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/marrasen/kakel/remote"
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
