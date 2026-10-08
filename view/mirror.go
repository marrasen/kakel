package view

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/marrasen/kakel/grid"
	"github.com/marrasen/kakel/look"
	"github.com/marrasen/kakel/screen"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"
)

// All Panes draws the panes of every window, and a window draws only its
// own. Each window runs on a goroutine of its own, so another window's
// panes are drawn from what is shared: a terminal from its shell's
// screen, copied whole into a grid of All Panes' own, and any other pane
// from a copy of what it last drew, which its window leaves here.

// mirror is a terminal of another window, as All Panes draws it.
type mirror struct {
	sh    *screen.Shell
	cells *widget.CellGrid
	row   []widget.Cell
	// wrote is when the shell had last written as the screen was
	// copied, and copied that it has been.
	wrote  time.Time
	copied bool
}

func newMirror(sh *screen.Shell) *mirror {
	g := widget.NewCellGrid()
	g.Size = 15
	g.Background = look.TermBackground
	return &mirror{sh: sh, cells: g}
}

// sync copies the screen, when the shell has written since it last did.
// It copies every row and leaves the rows marked changed as they are:
// the window the pane is in copies those, and must still find them.
func (m *mirror) sync() {
	at := m.sh.Wrote()
	if m.copied && at.Equal(m.wrote) {
		return
	}
	m.wrote, m.copied = at, true
	m.sh.Drawn(func(g *grid.Grid) {
		cols, rows := g.Size()
		m.cells.Resize(cols, rows)
		for y := range rows {
			m.row = m.row[:0]
			for x := range cols {
				m.row = append(m.row, cellOf(g, x, y))
			}
			m.cells.SetRow(y, m.row)
		}
	})
	if m.sh.T.Dirty() {
		// Still being written: copied again next time.
		m.copied = false
	}
}

// natural lays the grid out at its own size in f, and returns that
// size: the cells at the size they are drawn in.
func (m *mirror) natural(f gunim.Frame) geom.Size {
	return m.cells.Layout(gunim.Constraints{}, f, gunim.Children{})
}

// overviews counts All Panes open, in any window: while it is, each
// window leaves a copy of what its panes drew for the others.
var overviews atomic.Int32

// shared holds the copies, by pane.
var shared = struct {
	mu    sync.Mutex
	drawn map[string]sharedDrawing
}{drawn: map[string]sharedDrawing{}}

// sharedDrawing is a copy of what a pane drew, and the size it drew it
// at.
type sharedDrawing struct {
	rec  *paint.Recording
	size geom.Size
}

// shareEvery is how often a window copies what its panes drew, while
// All Panes is open somewhere.
const shareEvery = 250 * time.Millisecond

// shareDrawings leaves a copy of what each of w's panes last drew, for
// All Panes in another window, at most every shareEvery.
func (w *Window) shareDrawings(now time.Time) {
	if overviews.Load() == 0 || now.Sub(w.sharedAt) < shareEvery {
		return
	}
	w.sharedAt = now
	shared.mu.Lock()
	defer shared.mu.Unlock()
	for id, d := range w.drawings {
		if d.Recording().Empty() {
			continue
		}
		// Copied by drawing it into a painter of its own: the drawing
		// is kept again, in place, as the pane next paints.
		var p paint.Painter
		p.Reset()
		p.Replay(d.Recording())
		rec := &paint.Recording{}
		p.Keep(0, rec)
		shared.drawn[id] = sharedDrawing{rec: rec, size: d.Size()}
	}
}

// sharedOf returns the copy of what pane id last drew, if there is one.
func sharedOf(id string) (sharedDrawing, bool) {
	shared.mu.Lock()
	defer shared.mu.Unlock()
	d, ok := shared.drawn[id]
	return d, ok
}

// forgetShared lets go of the copies of panes that have closed.
func forgetShared(live func(id string) bool) {
	shared.mu.Lock()
	defer shared.mu.Unlock()
	for id := range shared.drawn {
		if !live(id) {
			delete(shared.drawn, id)
		}
	}
}
