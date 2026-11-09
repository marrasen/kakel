package vt

import (
	"sync/atomic"
	"weak"
)

// Pin is a line number that follows its line when a resize re-wraps the screen.
type Pin struct{ line atomic.Uint64 }

// Line is the number of the pinned line now.
func (p *Pin) Line() uint64 { return p.line.Load() }

// Pin starts keeping line up to date, numbered as LineNumber numbers it. The terminal lets go of the pin once the
// caller has.
func (t *Terminal) Pin(line uint64) *Pin {
	p := &Pin{}
	p.line.Store(line)
	t.pins.add(p)
	return p
}

// pins are the pins a terminal keeps up to date, held weakly.
type pins struct {
	held []weak.Pointer[Pin]
	// sweep is how many may be held before the ones let go of are swept out.
	sweep int
}

func (ps *pins) add(p *Pin) {
	if len(ps.held) >= ps.sweep {
		ps.drop()
		ps.sweep = max(2*len(ps.held), 8)
	}
	ps.held = append(ps.held, weak.Make(p))
}

// move moves every pin still held, and forgets the rest.
func (ps *pins) move(moved Moved) {
	ps.drop()
	for _, w := range ps.held {
		if p := w.Value(); p != nil {
			n, _ := moved(p.Line(), 0)
			p.line.Store(n)
		}
	}
}

// drop forgets the pins nobody holds any more.
func (ps *pins) drop() {
	keep := ps.held[:0]
	for _, w := range ps.held {
		if w.Value() != nil {
			keep = append(keep, w)
		}
	}
	clear(ps.held[len(keep):])
	ps.held = keep
}
