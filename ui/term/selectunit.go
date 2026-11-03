package term

import (
	"math"
	"strings"
	"unicode"

	"github.com/marrasen/kakel/grid"
	"github.com/marrasen/kakel/vt"
)

// selUnit is what a press selects by: a double click picks words and a
// triple click lines, and a drag after either goes on by whole ones.
type selUnit uint8

const (
	byCell selUnit = iota
	byWord
	byLine
)

// unitOf is what a press that is the clicks'th of a quick run selects
// by. A fourth click starts again from cells, as in other terminals.
func unitOf(clicks int) selUnit {
	return selUnit((max(clicks, 1) - 1) % 3)
}

// wordPunct are the marks a word takes in besides letters and digits,
// so a double click picks a whole path, address or option: a folder
// with its slashes, a file with its dot, a host with its colon and @.
const wordPunct = `-_./\~:@%+=?&#$`

// wordSpace and wordText are the classes of a blank and of a word's
// characters. Any other mark is a class of its own, its rune, so a
// double click on ==== picks the run of them and on ( the one.
const (
	wordSpace = -1
	wordText  = -2
)

// classOf is the class of cell x of a line, a wide character's second
// half taking its first's.
func classOf(cells []grid.Cell, x int) int {
	if x < 0 || x >= len(cells) {
		return wordSpace
	}
	c := cells[x]
	if c.Width == 0 && x > 0 {
		c = cells[x-1]
	}
	r := c.Rune
	switch {
	case r == 0 || unicode.IsSpace(r):
		return wordSpace
	case unicode.IsLetter(r) || unicode.IsNumber(r) || strings.ContainsRune(wordPunct, r):
		return wordText
	}
	return int(r)
}

// wraps reports whether a line runs on to the next one, the screen
// having wrapped it there rather than the program ending it.
func wraps(cells []grid.Cell) bool {
	return len(cells) > 0 && cells[len(cells)-1].Wrapped
}

// unitSpan is the first and last cell of the word or the line at is
// in, at's row a line number as [vt.Screen.LineNumber] numbers them.
// A line the screen wrapped is one line with the next, for both.
func (t *Terminal) unitSpan(at grid.Point, unit selUnit) (from, to grid.Point) {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := t.term.Screen()
	if unit == byLine {
		return lineSpan(s, at.Y)
	}
	return wordSpan(s, at)
}

// lineSpan is the whole of line n, with the lines the screen wrapped it
// on to and from.
func lineSpan(s *vt.Screen, n int) (from, to grid.Point) {
	cols, _ := s.Size()
	top := n
	for top > s.OldestLine() && wraps(s.LineAt(top-1)) {
		top--
	}
	bottom := n
	for wraps(s.LineAt(bottom)) && s.LineAt(bottom+1) != nil {
		bottom++
	}
	return grid.Point{Y: top}, grid.Point{X: max(cols-1, 0), Y: bottom}
}

// wordSpan is the run of cells of one class around at: a word, the
// blanks between two, or a run of one mark.
func wordSpan(s *vt.Screen, at grid.Point) (from, to grid.Point) {
	line := s.LineAt(at.Y)
	if line == nil {
		return at, at
	}
	at.X = max(min(at.X, len(line)-1), 0)
	if at.X > 0 && line[at.X].Width == 0 {
		at.X--
	}
	class := classOf(line, at.X)

	// Back to the first cell of the class, onto the line before where
	// the screen wrapped that one into this.
	from = at
	for {
		cells := s.LineAt(from.Y)
		switch {
		case from.X > 0 && classOf(cells, from.X-1) == class:
			from.X--
			continue
		case from.X == 0 && from.Y > s.OldestLine():
			if prev := s.LineAt(from.Y - 1); wraps(prev) && classOf(prev, len(prev)-1) == class {
				from = grid.Point{X: len(prev) - 1, Y: from.Y - 1}
				continue
			}
		}
		break
	}
	// On to the last, onto the line after where the screen wrapped
	// this one into that.
	to = at
	for {
		cells := s.LineAt(to.Y)
		switch {
		case to.X+1 < len(cells) && (cells[to.X+1].Width == 0 || classOf(cells, to.X+1) == class):
			to.X++
			continue
		case to.X+1 >= len(cells) && wraps(cells):
			if next := s.LineAt(to.Y + 1); len(next) > 0 && classOf(next, 0) == class {
				to = grid.Point{Y: to.Y + 1}
				continue
			}
		}
		break
	}
	return from, to
}

// before reports whether a comes before b in reading order.
func before(a, b grid.Point) bool {
	return a.Y < b.Y || a.Y == b.Y && a.X < b.X
}

// ordered is s's ends in reading order.
func ordered(s grid.Selection) (from, to grid.Point) {
	if before(s.Cursor, s.Anchor) {
		return s.Cursor, s.Anchor
	}
	return s.Anchor, s.Cursor
}

// between is the selection f of the way from a to b, each end moving
// along the text, cols cells a row, a cell at a time: what a word or a
// line picked by a click shows as it grows out to its ends.
func between(a, b grid.Selection, f float32, cols int) grid.Selection {
	f = max(min(f, 1), 0)
	cols = max(cols, 1)
	af, at := ordered(a)
	bf, bt := ordered(b)
	step := func(p, q grid.Point) grid.Point {
		i, j := p.Y*cols+p.X, q.Y*cols+q.X
		n := i + int(math.Round(float64(f)*float64(j-i)))
		y := n / cols
		if n < 0 && n%cols != 0 {
			y--
		}
		return grid.Point{X: n - y*cols, Y: y}
	}
	return grid.Selection{Anchor: step(af, bf), Cursor: step(at, bt), Active: true}
}

// SelectionGrowing reports whether a word or a line picked by a double
// or triple click is still growing out to its ends, which
// [Terminal.GrowSelection] moves on.
func (t *Terminal) SelectionGrowing() bool { return t.growing }

// GrowSelection shows a word or a line a click picked f of the way
// grown from where it started to its ends, all of it once f reaches 1
// or done says the motion ended.
func (t *Terminal) GrowSelection(f float32, done bool) {
	if !t.growing {
		return
	}
	t.grown = f
	if done || f >= 1 {
		t.growing = false
	}
	t.showSelection(t.drawnTop)
	t.pending.Store(true)
}
