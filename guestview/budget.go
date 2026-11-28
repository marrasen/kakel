package guestview

import "sync"

// What a page holds of the host's terminals. How many there are, how
// large, and what is in them is the host's to say, and a tab that holds
// too much is ended by its browser, so there is a most, which the
// terminals on the screen have before any other, and their screens before
// their history and their images.
const (
	// mostHeld is the most the terminals take, in bytes: the largest
	// window there is on a large screen, in small text.
	mostHeld = 96 << 20

	// cellBytes is what a cell of a screen takes: of the ordinary screen,
	// the alternate one, and the copies that are drawn. lineCell is what
	// one of the history takes.
	cellBytes, lineCell = 450, 88

	// mostLines is the most history a terminal has, lineSlack how far it
	// may pass what it has before it is cut, and leastLines the least
	// there is any use in.
	mostLines, lineSlack, leastLines = 2000, 256, 100

	// mostImages is the most the images of a terminal take, and mostLinks
	// the most its hyperlinks do.
	mostImages, mostLinks = 8 << 20, 512 << 10

	// mostKept is the most terminals kept with their panes off the screen.
	mostKept = 16
)

// screenBytes is what a screen of cols by rows takes.
func screenBytes(cols, rows int) int { return cols * rows * cellBytes }

// room is what a terminal is made with: the size of its screen, how many
// lines of history it has, and how many bytes of images and of links, none
// for 0. other is what something that is no terminal takes.
type room struct{ cols, rows, lines, images, links, other int }

// bytes is what it takes at most.
func (r room) bytes() int {
	n := screenBytes(r.cols, r.rows) + r.images + r.links + r.other
	if r.lines > 0 {
		n += (r.lines + lineSlack) * r.cols * lineCell
	}
	return n
}

// spare is what it takes besides its screen.
func (r room) spare() int { return r.bytes() - screenBytes(r.cols, r.rows) }

// roomWith is room for a screen of cols by rows with share bytes besides,
// for its links, its history and its images.
func roomWith(cols, rows, share int) room {
	r := room{cols: cols, rows: rows, links: max(min(share/16, mostLinks), 0)}
	share -= r.links
	lines := min(share*3/4/(cols*lineCell)-lineSlack, mostLines)
	if lines >= leastLines {
		r.lines = lines
		share -= (lines + lineSlack) * cols * lineCell
	}
	if images := min(share, mostImages); images > 0 {
		r.images = images
	}
	return r
}

// budget counts what the terminals take, by pane. It is asked from any
// goroutine.
type budget struct {
	mu   sync.Mutex
	most int
	has  map[string]room
}

func newBudget(most int) *budget { return &budget{most: most, has: map[string]room{}} }

// free is what is left, with pane id's own counted as left.
func (b *budget) free(id string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.freeLocked(id)
}

func (b *budget) freeLocked(id string) int {
	n := b.most
	for k, r := range b.has {
		if k != id {
			n -= r.bytes()
		}
	}
	return n
}

// take gives pane id a room, where there is that much, and reports
// whether there was.
func (b *budget) take(id string, r room) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if r.bytes() > b.freeLocked(id) {
		return false
	}
	b.has[id] = r
	return true
}

// hold keeps n bytes for what is no terminal, as id, whatever is left: the
// terminals have what is left besides it.
func (b *budget) hold(id string, n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.has[id] = room{other: n}
}

// held is what is kept for what is no terminal.
func (b *budget) held() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for _, r := range b.has {
		n += r.other
	}
	return n
}

// resize gives pane id's terminal a screen of another size, with the
// history and the images it has, where there is room, and reports whether
// there was.
func (b *budget) resize(id string, cols, rows int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	r, ok := b.has[id]
	if !ok {
		return false
	}
	r.cols, r.rows = cols, rows
	if r.bytes() > b.freeLocked(id) {
		return false
	}
	b.has[id] = r
	return true
}

// of is pane id's room.
func (b *budget) of(id string) room {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.has[id]
}

// drop gives back pane id's.
func (b *budget) drop(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.has, id)
}
