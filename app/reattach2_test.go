package app

import (
	"net"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/settings"
	"github.com/marrasen/kakel/ui"
)

// usualKey has served window a allow a key in the usual place, so a
// window connected to by address alone reaches it, and serve again.
func usualKey(t *testing.T, a *app) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(home, ".ssh", "id_ed25519")
	writeKey(t, key)
	pub, err := os.ReadFile(key + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	allowed, err := os.ReadFile(a.serving.allowed)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.serving.allowed, append(allowed, pub...), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.stopServing(); err != nil {
		t.Fatal(err)
	}
	a.handle(StartServing{Port: "0"})
}

// A layout saved from a window connected to by address comes back to
// its panes there once that window is in the server list: what it opens
// is written down by the saved window's id, while the layout names it
// by its address; and what was written down by its address before it
// was saved is found by that address.
func TestAWindowSavedSinceComesBackToItsPanes(t *testing.T) {
	a, b, opened := windowLayoutApps(t)
	usualKey(t, a)
	addr := a.st.Serving.Addr
	id := keep(t, b, alone("Quick", settings.SavedPane{Kind: settings.LayoutTerminal, Host: addr, Window: true})).ID
	byAddress := reopened(t, a, b, opened, id)
	for _, name := range b.machines.Windows() {
		_ = b.disconnectWindow(name)
	}
	pumpBoth(t, a, b, "the window to go", func() bool { return len(b.machines.Windows()) == 0 })
	b.st.Notices = nil
	host, port, _ := net.SplitHostPort(addr)
	if err := b.book.Put(remote.Host{Name: "laptop", Address: host, Port: atoi(t, port), Window: true}, ""); err != nil {
		t.Fatal(err)
	}
	b.st.Saved = b.book.Hosts()
	had := len(a.st.Panes)
	if got := reopened(t, a, b, opened, id); !slices.Equal(got, byAddress) || len(a.st.Panes) != had {
		t.Fatalf("saved since, watches %v, want %v; the window has %d panes, want %d", got, byAddress, len(a.st.Panes), had)
	}

	// Its pane closed there, a new one is written down by the saved
	// window's id, and is watched again after that.
	a.remove(byAddress[0])
	listedThere(t, a, b)
	byID := reopened(t, a, b, opened, id)
	if recs := b.recordsOf(id); len(recs) != 1 || recs[0].Window == serveAddr(addr) {
		t.Fatalf("written down %+v", recs)
	}
	had = len(a.st.Panes)
	if got := reopened(t, a, b, opened, id); !slices.Equal(got, byID) || len(a.st.Panes) != had {
		t.Fatalf("watches %v, want %v; the window has %d panes, want %d", got, byID, len(a.st.Panes), had)
	}
}

// A pane that cannot be watched again for now, as one whose machine the
// window has lost, fails its slot, saying why, and opens nothing new:
// Retry, once the window reaches the machine again, watches the same
// pane again.
func TestAPaneNotToBeWatchedNowIsTriedAgain(t *testing.T) {
	_, conn, _ := tunnelApp(t)
	a, b, opened := windowLayoutApps(t)
	a.machines.At("srv").Conn = conn
	pumpBoth(t, a, b, "srv listed", func() bool { a.publish(); return true })
	id := keep(t, b, alone("Beyond", throughDesk(settings.SavedPane{Kind: settings.LayoutTerminal, On: "srv", OnKey: "srv"}))).ID
	first := reopened(t, a, b, opened, id)
	// The window loses srv: the pane there ends, and stays open.
	_ = conn.Close()
	pumpBoth(t, a, b, "it to end there", func() bool {
		i := slices.IndexFunc(a.st.Panes, func(p Pane) bool { return p.ID == first[0] })
		return i >= 0 && a.st.Panes[i].Ended
	})
	a.machines.At("srv").Conn = nil
	listedThere(t, a, b)
	had := len(a.st.Panes)

	w := opens(t, b, opened, id)
	pumpBoth(t, a, b, "the pane to fail", func() bool {
		ps := waitingIn(b, w)
		return len(ps) == 1 && b.slots[ps[0].ID].failed
	})
	if len(a.st.Panes) != had || len(b.recordsOf(id)) != 1 {
		t.Fatalf("the window has %d panes, want %d; written down %+v", len(a.st.Panes), had, b.recordsOf(id))
	}

	_, again, _ := tunnelApp(t)
	a.machines.At("srv").Conn = again
	listedThere(t, a, b)
	b.handle(RetryWaiting{Pane: waitingIn(b, w)[0].ID})
	pumpBoth(t, a, b, "the pane to start", func() bool { return len(waitingIn(b, w)) == 0 && !slices.Contains(farIDs(b, w), "") })
	if got := farIDs(b, w); !slices.Equal(got, first) || len(a.st.Panes) != had {
		t.Fatalf("retried, watches %v, want %v; the window has %d panes, want %d", got, first, len(a.st.Panes), had)
	}
}

// What is waiting to be written is read as written: a layout opened
// again while another's pane still waits, which holds the write, comes
// back to its panes all the same; and a window gone before it named a
// pane holds nothing up.
func TestRecordsNotYetWrittenAreRead(t *testing.T) {
	a, b, opened := windowLayoutApps(t)
	id := deskLayout(t, b)
	b.slots["stuck"] = &slot{}
	t.Cleanup(func() { delete(b.slots, "stuck") })
	first := reopened(t, a, b, opened, id)
	if len(b.settings.LayoutPanes()) != 0 || len(b.recordsOf(id)) != 2 {
		t.Fatalf("written %+v, read %+v", b.settings.LayoutPanes(), b.recordsOf(id))
	}
	had := len(a.st.Panes)
	if got := reopened(t, a, b, opened, id); !slices.Equal(got, first) || len(a.st.Panes) != had {
		t.Fatalf("watches %v, want %v; the window has %d panes, want %d", got, first, len(a.st.Panes), had)
	}

	delete(b.slots, "stuck")
	b.naming["gone"] = machines.ID("nowhere")
	b.settleRecords()
	if len(b.settings.LayoutPanes()) != 2 {
		t.Fatalf("with a window gone waited on, written %+v", b.settings.LayoutPanes())
	}
}

// A slot still waiting when its layout is saved over, named afresh,
// writes down what it opens under its new name.
func TestASlotNamedAfreshWritesUnderItsNewName(t *testing.T) {
	a, b, opened := windowLayoutApps(t)
	id := keep(t, b, alone("Mixed",
		settings.SavedPane{Kind: settings.LayoutTerminal, Command: plainShell()},
		throughDesk(settings.SavedPane{Kind: settings.LayoutTerminal}),
	)).ID
	w := opens(t, b, opened, id)
	pumpBoth(t, a, b, "the host key question", func() bool { return len(b.st.Asks) > 0 && len(waitingIn(b, w)) == 1 })
	b.remove(b.panesIn(w)[0].ID)
	if _, _, _, err := b.saveLayout(SaveLayout{Window: w.id, Name: "Mixed", Replace: true}); err != nil {
		t.Fatal(err)
	}
	asked := 0
	pumpBoth(t, a, b, "the pane to start", func() bool {
		answering(b, &asked)
		return len(waitingIn(b, w)) == 0 && !slices.Contains(farIDs(b, w), "")
	})
	if recs := b.recordsOf(id); len(recs) != 1 || recs[0].Slot != "p1" {
		t.Fatalf("written down %+v", recs)
	}
}

// A pane being watched again is not attached to by hand as well: the
// hand attach goes to the pane that is about to watch it; and one
// attached to by hand as a layout opens is watched once.
func TestAHandAttachWaitsForTheClaim(t *testing.T) {
	a, b, opened := windowLayoutApps(t)
	id := deskLayout(t, b)
	first := reopened(t, a, b, opened, id)
	// Shown in a's window, as on a screen: one nothing shows has no
	// screen to attach to.
	a.terminal(first[0]).Layout(ui.Size{Cols: 80, Rows: 24})
	listedThere(t, a, b)
	b.addPane(Pane{ID: "pw", Title: "waiting", Kind: KindWaiting}, nil, Placement{})
	b.claimed[claimOf("desk", first[0])] = "pw"
	b.handle(AttachWindow{Window: "desk", ID: first[0]})
	settleEvents(b)
	delete(b.claimed, claimOf("desk", first[0]))
	if len(b.st.Panes) != 1 || b.st.Focus != "pw" {
		t.Fatalf("attached by hand while claimed: %+v, the keyboard on %q", b.st.Panes, b.st.Focus)
	}
	b.remove("pw")

	b.handle(AttachWindow{Window: "desk", ID: first[0]})
	w := openLanded(t, a, b, opened, id)
	pumpBoth(t, a, b, "the hand attach", func() bool { return len(b.st.Panes) == 3 })
	watched := map[string]int{}
	for _, p := range b.st.Panes {
		for far, pane := range b.machines.Get("desk").Window.Bound {
			if pane == p.ID {
				watched[far]++
			}
		}
	}
	for far, n := range watched {
		if n > 1 {
			t.Fatalf("%s is watched %d times; the layout watches %v", far, n, farIDs(b, w))
		}
	}
}

// Two copies opened one after the other before the window lists what it
// has open come back to their own panes, in that order.
func TestTwoCopiesComeBackInTheOrderOpened(t *testing.T) {
	a, b, opened := windowLayoutApps(t)
	id := deskLayout(t, b)
	one := openLanded(t, a, b, opened, id)
	two := openLanded(t, a, b, opened, id)
	firsts, seconds := farIDs(b, one), farIDs(b, two)
	closeLanded(a, b, two)
	closeLanded(a, b, one)
	listedThere(t, a, b)
	_ = b.disconnectWindow("desk")
	pumpBoth(t, a, b, "the window to go", func() bool { return len(b.machines.Windows()) == 0 })
	b.st.Notices = nil

	had := slices.Clone(b.wins)
	b.handle(OpenLayout{ID: id})
	b.handle(OpenLayout{ID: id})
	// The copy opened first is the one whose waiting panes were made
	// first, whichever of the two windows the system opened first: read
	// while they wait, as a pane started is numbered as it starts.
	made := map[*ownWin]int{}
	pumpBoth(t, a, b, "both windows", func() bool {
		for _, w := range b.wins {
			if ps := waitingIn(b, w); !slices.Contains(had, w) && len(ps) == 2 && made[w] == 0 {
				made[w] = paneNumber(ps[0].ID)
			}
		}
		return len(made) == 2
	})
	var fresh []*ownWin
	for w := range made {
		fresh = append(fresh, w)
	}
	one, two = fresh[0], fresh[1]
	if made[two] < made[one] {
		one, two = two, one
	}
	asked := 0
	pumpBoth(t, a, b, "every pane to start", func() bool {
		answering(b, &asked)
		return len(waitingIn(b, one))+len(waitingIn(b, two)) == 0 &&
			!slices.Contains(farIDs(b, one), "") && !slices.Contains(farIDs(b, two), "")
	})
	if got1, got2 := farIDs(b, one), farIDs(b, two); !slices.Equal(got1, firsts) || !slices.Equal(got2, seconds) {
		t.Fatalf("the first watches %v, want %v; the second %v, want %v", got1, firsts, got2, seconds)
	}
}

// Saved over from one copy, a layout keeps the panes another copy open
// now watches, under the new names of its slots; one whose slot the
// saved window no longer has goes.
func TestSavingOverKeepsTheOtherCopysPanes(t *testing.T) {
	a, b, opened := windowLayoutApps(t)
	id := deskLayout(t, b)
	one := openLanded(t, a, b, opened, id)
	two := openLanded(t, a, b, opened, id)
	mine, theirs := farIDs(b, one), farIDs(b, two)
	// The terminal closed in the first: the command is its first slot.
	b.remove(b.panesIn(one)[0].ID)
	if _, _, _, err := b.saveLayout(SaveLayout{Window: one.id, Name: "Desk", Replace: true}); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range b.recordsOf(id) {
		if r.Slot != "p1" {
			t.Fatalf("written down %+v", b.recordsOf(id))
		}
		got = append(got, r.Pane)
	}
	if len(got) != 2 || !slices.Contains(got, mine[1]) || !slices.Contains(got, theirs[1]) {
		t.Fatalf("written down %v, want the commands %s and %s", got, mine[1], theirs[1])
	}
}
