package guestview

import (
	"bytes"
	"io"
	"sync"

	"github.com/marrasen/kakel/session"
)

// feed is the session a pane's terminal is on for as long as there is
// one. The pane itself is opened when it comes on the screen, closed some
// time after it goes, and opened again when a lost connection is back.
// Each time the feed goes on with the whole screen, so the terminal keeps
// its history and its place. Nothing is to be typed while there is none:
// the page keeps it until the pane is open.
type feed struct {
	// fits reports whether the terminal may have a screen of a size, which
	// it then has the room for. onEnd hears that a pane has ended, and
	// whether it did by having one that does not fit. onReady hears each
	// time that a pane's first screen is on the terminal, and whether
	// something typed was for nobody since the pane before. They are
	// called from the terminal's goroutines, and do not wait.
	fits    func(cols, rows int) bool
	onEnd   func(tooLarge bool)
	onReady func(untyped bool)

	mu  sync.Mutex
	cur session.ScreenReader
	// first is the screen cur begins with, which has been read from it
	// already, and given that it is out and onReady is yet to hear.
	first *session.Screen
	given bool
	// untyped says something typed was for nobody.
	untyped bool
	// changed is closed, and made again, as cur changes.
	changed chan struct{}
	closed  bool
	over    chan struct{}
}

// newFeed is a feed with nothing to show.
func newFeed() *feed {
	return &feed{changed: make(chan struct{}), over: make(chan struct{})}
}

var _ session.ScreenReader = (*feed)(nil)

// Mirrors says that another terminal runs the program: see
// session.Mirrors.
func (f *feed) Mirrors() bool { return true }

// attach goes on with p, whose first screen is first, and lets go of what
// was there. With fresh, as over another connection, what the terminal was
// typed and has not sent was for the one before.
func (f *feed) attach(p session.ScreenReader, first *session.Screen, fresh bool) {
	if first != nil {
		first.Started = first.Started || fresh
	}
	f.mu.Lock()
	old, late := f.cur, session.ScreenReader(nil)
	if f.closed {
		late, p, first = p, nil, nil
	}
	f.cur, f.first, f.given = p, first, false
	close(f.changed)
	f.changed = make(chan struct{})
	f.mu.Unlock()
	for _, gone := range []session.ScreenReader{old, late} {
		if gone != nil {
			_ = gone.Close()
		}
	}
}

// detach closes the pane, and leaves the screen as it is.
func (f *feed) detach() { f.attach(nil, nil, false) }

// ended lets go of p, which has, or whose screen does not fit.
func (f *feed) ended(p session.ScreenReader, tooLarge bool) {
	f.mu.Lock()
	ours := f.cur == p
	if ours {
		f.cur, f.first, f.given = nil, nil, false
	}
	f.mu.Unlock()
	if ours {
		_ = p.Close()
		if f.onEnd != nil {
			f.onEnd(tooLarge)
		}
	}
}

// ReadScreen implements session.ScreenReader.
func (f *feed) ReadScreen(b []byte) (int, *session.Screen, error) {
	for {
		f.mu.Lock()
		cur, changed, first := f.cur, f.changed, f.first
		f.first = nil
		closed := f.closed
		// Asked for more, the terminal has what it was given.
		ready, untyped := f.given, f.untyped
		f.given = first != nil
		if ready {
			f.untyped = false
		}
		f.mu.Unlock()
		if ready && f.onReady != nil {
			f.onReady(untyped)
		}
		switch {
		case closed:
			return 0, nil, io.EOF
		case first != nil:
			return 0, first, nil
		case cur == nil:
			<-changed
			continue
		}
		n, scr, err := cur.ReadScreen(b)
		if scr != nil && scr.Cols > 0 && f.fits != nil && !f.fits(scr.Cols, scr.Rows) {
			f.ended(cur, true)
			continue
		}
		if err != nil {
			f.ended(cur, false)
		}
		if n > 0 || scr != nil {
			return n, scr, nil
		}
	}
}

// Read is ReadScreen, for a caller that minds no sizes: there is none.
func (f *feed) Read(b []byte) (int, error) {
	for {
		n, _, err := f.ReadScreen(b)
		if n > 0 || err != nil {
			return n, err
		}
	}
}

// Write types b. While there is no pane it is for nobody, which onReady
// hears: nothing more of what was typed along with it is to be sent.
func (f *feed) Write(b []byte) (int, error) {
	f.mu.Lock()
	cur, closed := f.cur, f.closed
	f.mu.Unlock()
	switch {
	case closed:
		return 0, io.ErrClosedPipe
	case cur != nil:
		// A pane that has ended says so when it is read.
		if _, err := cur.Write(b); err == nil {
			return len(b), nil
		}
	}
	if !moves(b) {
		f.mu.Lock()
		f.untyped = true
		f.mu.Unlock()
	}
	return len(b), nil
}

// moves reports whether b is what a terminal tells a program of the mouse.
func moves(b []byte) bool {
	return bytes.HasPrefix(b, []byte("\x1b[<")) || bytes.HasPrefix(b, []byte("\x1b[M"))
}

// Resize does nothing: the size is the host's.
func (f *feed) Resize(int, int) error { return nil }

// Wait waits for the feed to be closed.
func (f *feed) Wait() error {
	<-f.over
	return nil
}

// Close ends the feed, and the pane.
func (f *feed) Close() error {
	f.mu.Lock()
	if !f.closed {
		f.closed = true
		close(f.over)
	}
	f.mu.Unlock()
	f.detach()
	return nil
}
