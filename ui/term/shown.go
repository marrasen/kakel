package term

import (
	"bytes"

	"github.com/marrasen/kakel/session"
	"github.com/marrasen/kakel/ui"
)

// A session may hand over whole screens, each at a size of its own, as
// that of a pane in another kakel's shared window does: see
// session.ScreenReader. The size of the terminal is then the session's to
// set, in order with the output, and neither the layout nor anything else
// here changes it.

// read takes the next of the session's output, or a screen.
func (r *run) read(p []byte) (int, *session.Screen, error) {
	if r.screens != nil {
		return r.screens.ReadScreen(p)
	}
	n, err := r.sess.Read(p)
	return n, nil, err
}

// takeScreen shows scr in place of what the terminal has. It runs on the
// goroutine reading the session, so it falls exactly where it stood in
// the output.
func (t *Terminal) takeScreen(updates *syncer, scr *session.Screen) {
	if scr.Started {
		t.drain()
		if scr.Cols == 0 {
			return
		}
	}
	cols, rows := max(scr.Cols, 1), max(scr.Rows, 1)
	t.cells.Store(int64(cols) * int64(rows))
	updates.restart(scr.Draw, func() {
		t.mu.Lock()
		t.term.Adopt(cols, rows, scr.Draw)
		t.screens++
		t.sendScreen()
		t.staleFollowers()
		t.said.Add(1)
		told := t.toTell
		t.toTell = nil
		t.mu.Unlock()
		for _, f := range told {
			f()
		}
	})
	t.outputArrived()
}

// showScreens takes over what a session's screens changed since the last
// draw, on the goroutine that draws: the size, and the selection, whose
// text has gone.
func (t *Terminal) showScreens() {
	cols, rows := t.g.Size()
	t.size, t.haveSize = ui.Size{Cols: cols, Rows: rows}, true
	t.selecting = false
	t.ClearSelection()
}

// restart drops whatever is held back, which a screen stands in place of,
// hands the screen over with show, and scans on from its end: it may end
// part way through a sequence.
func (s *syncer) restart(draw []byte, show func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.held, s.carry, s.on = nil, nil, false
	s.update++
	s.scan = scanner{}
	show()
	for i := 0; i < len(draw); i++ {
		if s.scan.state == inText {
			j := bytes.IndexByte(draw[i:], esc)
			if j < 0 {
				return
			}
			i += j
		}
		s.scan.step(draw[i])
	}
}
