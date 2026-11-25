package term

import "slices"

// Follower is somebody shown this terminal at the size it has here, for
// as long as the terminal is there: the guest of a shared window.
//
// It differs from a Watcher in what it never loses. A watcher decides the
// size and goes when the program does. A follower is told of each screen
// with its size, and stays through the program ending and another being
// started in its place.
//
// All of it is called with a lock held that the terminal needs back
// before it can draw anything, so none of it may block, or call the
// terminal for anything but a Lane.
type Follower interface {
	// Screen is the whole screen as it stands, cols by rows, as
	// vt.Terminal.Replay writes it. Anything queued and not passed on is
	// older, and must be thrown away.
	Screen(cols, rows int, p []byte)

	// Write is what the program said next. p is the caller's.
	Write(p []byte)

	// Stale says the screen changed by something no bytes say: a new
	// size, new colours, or a line of the window's own. What is written
	// from now on lands on a screen the follower does not have, so it
	// must be thrown away until CatchUp has given it the screen.
	//
	// The screen is not handed over here, because a window's edge being
	// dragged says this at every frame, and a follower that is busy needs
	// only the last.
	Stale()

	// Ended says the program has gone, and Started that another has been
	// started in its place.
	Ended()
	Started()
}

// Follow shows this terminal to f, the screen first, and returns what
// stops it. Once that has returned, f is told nothing more.
func (t *Terminal) Follow(f Follower) (stop func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	// What the follower is given is the screen at one moment, and its
	// stream starts from there: see Watch.
	t.watchMu.Lock()
	defer t.watchMu.Unlock()
	t.followers = append(t.followers, f)
	t.showLocked(f)
	if t.ended {
		f.Ended()
	}
	return func() {
		t.watchMu.Lock()
		defer t.watchMu.Unlock()
		if i := slices.Index(t.followers, f); i >= 0 {
			t.followers = slices.Delete(t.followers, i, i+1)
		}
	}
}

// CatchUp gives f the screen again: one that was told it is stale, or
// that fell behind and threw away what it missed. One that no longer
// follows is given nothing.
func (t *Terminal) CatchUp(f Follower) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.watchMu.Lock()
	defer t.watchMu.Unlock()
	if slices.Contains(t.followers, f) {
		t.showLocked(f)
	}
}

// showLocked gives f the screen. Both locks are held.
func (t *Terminal) showLocked(f Follower) {
	cols, rows := t.term.Screen().Size()
	f.Screen(cols, rows, []byte(t.liveScreen()))
}

// staleFollowers tells everybody following that the screen changed. The
// emulator's lock is held.
func (t *Terminal) staleFollowers() {
	t.watchMu.Lock()
	defer t.watchMu.Unlock()
	for _, f := range t.followers {
		f.Stale()
	}
}

// payOwed sends the screen to whoever was sent one that left out the
// sequence the program was part way through, once that has ended. The
// emulator's lock is held.
func (t *Terminal) payOwed() {
	if !t.owed || !t.term.AtRest() {
		return
	}
	t.owed = false
	t.sendScreen()
	t.staleFollowers()
}
