package term

import (
	"bytes"
	"slices"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	// laneRoom is how much a lane holds that the program has not been
	// given.
	laneRoom = 64 << 10

	// laneChunk is how much of it goes to the program at a time. What the
	// user types goes in between, so it waits for no more than this.
	laneChunk = 1 << 10

	// laneHeld is how long the start of a character or of a key waits for
	// the rest of it. The Esc key waits that long too, as it is how other
	// keys begin.
	laneHeld = 10 * time.Millisecond
)

// Lane types into a terminal for somebody who is not at it, as a guest of
// a shared window does. What it holds goes to the program only while
// nothing the user typed waits, so however much it is, it neither holds
// up the user's keys nor takes their room.
type Lane interface {
	// Type takes as much of p as there is room for, and says how much that
	// was. It does not wait.
	Type(p []byte) int

	// Room has word when there is more room than there was.
	Room() <-chan struct{}

	// Drop throws away what the program has not been given. Once it has
	// returned, none of it is, but for the little that was being handed
	// over.
	Drop()

	// Close is Drop, and takes nothing more.
	Close()
}

// Lane opens a lane to type in.
func (t *Terminal) Lane() Lane {
	l := &lane{t: t, room: make(chan struct{}, 1)}
	t.laneMu.Lock()
	t.lanes = append(t.lanes, l)
	t.laneMu.Unlock()
	return l
}

type lane struct {
	t    *Terminal
	room chan struct{}

	mu     sync.Mutex
	buf    []byte
	closed bool
	// held is since when all there is in buf is the start of something.
	held time.Time
}

func (l *lane) Type(p []byte) int {
	l.mu.Lock()
	n := min(len(p), laneRoom-len(l.buf))
	if l.closed || n <= 0 {
		l.mu.Unlock()
		return 0
	}
	l.buf = append(l.buf, p[:n]...)
	l.mu.Unlock()
	// The pane is taken over from its start lines, as by typing here.
	l.t.start.took(p[:n])
	word(l.t.laneReady)
	return n
}

func (l *lane) Room() <-chan struct{} { return l.room }

func (l *lane) Drop() {
	l.mu.Lock()
	l.buf = nil
	l.mu.Unlock()
	word(l.room)
}

func (l *lane) Close() {
	l.mu.Lock()
	l.buf, l.closed = nil, true
	l.mu.Unlock()
	word(l.room)
	l.t.laneMu.Lock()
	defer l.t.laneMu.Unlock()
	if i := slices.Index(l.t.lanes, l); i >= 0 {
		l.t.lanes = slices.Delete(l.t.lanes, i, i+1)
	}
}

// next takes a chunk for the program: whole characters and keys, as far as
// they have come, for what the user types goes in after it.
func (l *lane) next() []byte {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.buf) == 0 {
		return nil
	}
	n := CutTyped(l.buf, laneChunk)
	if n == len(l.buf) {
		n -= unfinished(l.buf)
	}
	if n == 0 {
		// The start of something, and nothing else.
		wait := l.t.laneHold
		if l.held.IsZero() {
			l.held = time.Now()
		} else {
			wait -= time.Since(l.held)
		}
		if wait > 0 {
			time.AfterFunc(wait, func() { word(l.t.laneReady) })
			return nil
		}
		n = len(l.buf)
	}
	l.held = time.Time{}
	b := bytes.Clone(l.buf[:n])
	if l.buf = l.buf[n:]; len(l.buf) == 0 {
		l.buf = nil
	}
	word(l.room)
	return b
}

// nextTyped is a chunk of what the lanes hold, each in its turn, or nil.
func (t *Terminal) nextTyped() []byte {
	t.laneMu.Lock()
	lanes := slices.Clone(t.lanes)
	t.laneMu.Unlock()
	for i := range lanes {
		at := (t.laneTurn + i) % len(lanes)
		if b := lanes[at].next(); b != nil {
			t.laneTurn = at + 1
			return b
		}
	}
	return nil
}

// dropLanes throws away what the lanes hold.
func (t *Terminal) dropLanes() {
	t.laneMu.Lock()
	lanes := slices.Clone(t.lanes)
	t.laneMu.Unlock()
	for _, l := range lanes {
		l.Drop()
	}
}

// CutTyped is where to cut what was typed to at most n bytes of it: not
// inside a character, nor inside a key's sequence, for what another types
// in between would break either. It is 0 for nothing only.
func CutTyped(b []byte, n int) int {
	if len(b) <= n {
		return len(b)
	}
	if at := n - unfinished(b[:n]); at > 0 {
		return at
	}
	return n
}

// longestKey is longer than the sequence of any key, or of the mouse.
const longestKey = 32

// unfinished is how many bytes at the end of b are the start of a
// character, or of a key's sequence, with the rest to come.
func unfinished(b []byte) int {
	from := max(0, len(b)-longestKey)
	if i := bytes.LastIndexByte(b[from:], esc); i >= 0 {
		i += from
		if !wholeKey(b[i:]) {
			if i > 0 && b[i-1] == esc {
				// Alt with a key that has a sequence of its own.
				i--
			}
			return len(b) - i
		}
	}
	for i := len(b) - 1; i >= 0 && i > len(b)-utf8.UTFMax; i-- {
		if utf8.RuneStart(b[i]) {
			if utf8.FullRune(b[i:]) {
				return 0
			}
			return len(b) - i
		}
	}
	return 0
}

// wholeKey reports whether the sequence that b, which begins with an ESC,
// begins with has come to its end.
func wholeKey(b []byte) bool {
	if len(b) < 2 {
		return false
	}
	switch b[1] {
	case '[':
		if len(b) > 2 && b[2] == 'M' {
			// The mouse as it was first reported, with three characters.
			rest := b[3:]
			for range 3 {
				if !utf8.FullRune(rest) {
					return false
				}
				_, n := utf8.DecodeRune(rest)
				rest = rest[n:]
			}
			return true
		}
		for _, c := range b[2:] {
			if c < 0x20 || c > 0x3f {
				return true
			}
		}
		return false
	case 'O':
		return len(b) > 2
	case ']', 'P', '_', '^', 'X':
		// An answer, which ends with a BEL, or with an ESC of its own.
		return bytes.IndexByte(b, bel) >= 0
	}
	return utf8.FullRune(b[1:])
}

// word leaves word on a channel of one, where one word is as good as two.
func word(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}
