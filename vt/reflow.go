package vt

import (
	"slices"
	"sort"

	"github.com/marrasen/kakel/grid"
)

// Moved says where a cell went in a resize: the line and column it was on before, in, and after, out. Lines are
// numbered as LineNumber numbers them.
type Moved func(line uint64, col int) (uint64, int)

// stayed is the Moved of a resize that moved no cell to another line.
func stayed(line uint64, col int) (uint64, int) { return line, col }

// lineMap says where each cell of a buffer went when it was re-wrapped. Rows count from the oldest one the buffer
// held, history first.
type lineMap struct {
	// oldRows is how many rows there were, logical the logical line each was part of, and start how far into that
	// line it began.
	oldRows int
	logical []int32
	start   []int32

	// first is the first new row of each logical line, with one more entry past the last, and newStart how far into
	// its logical line each new row begins.
	first    []int32
	newStart []int32

	// rows is how many rows there are now.
	rows int
}

// at is the new row and column of what was at row and col, the column past the row's end for a point past its text.
func (m *lineMap) at(row, col int) (int, int) {
	if row < 0 {
		return row, col
	}
	if row >= m.oldRows {
		return m.rows + row - m.oldRows, col
	}
	l := m.logical[row]
	off := int(m.start[row]) + col
	lo, hi := int(m.first[l]), int(m.first[l+1])
	r := lo + sort.Search(hi-lo, func(i int) bool { return int(m.newStart[lo+i]) > off }) - 1
	r = max(r, lo)
	c := off - int(m.newStart[r])
	if r >= m.rows {
		r = max(m.rows-1, 0)
	}
	return r, max(c, 0)
}

// reflow re-wraps history and screen at cols by rows around the cursor at cx, cy, taking rows back from history no
// further up than row floor, and returns where the cells went and how many rows are now above the screen.
func (b *buffer) reflow(cols, rows int, blank grid.Cell, cy, cx, floor int) (*lineMap, int) {
	all := make([]line, 0, len(b.scrollback)+len(b.lines))
	all = append(append(all, b.scrollback...), b.lines...)
	screen := len(b.scrollback)
	b.cols, b.fill = cols, blank
	cy += screen

	m := &lineMap{
		oldRows: len(all),
		logical: make([]int32, len(all)),
		start:   make([]int32, len(all)),
	}
	out := make([]line, 0, len(all))
	var joined []grid.Cell
	for i := 0; i < len(all); {
		// Rows i to j-1 are one logical line.
		j := i
		for j < len(all)-1 && wraps(all[j]) {
			j++
		}
		j++

		l := int32(len(m.first))
		m.first = append(m.first, int32(len(out)))
		var cells []grid.Cell
		if j == i+1 {
			m.logical[i], m.start[i] = l, 0
			cells = lastRow(all[i], blank, i == cy, cx)
		} else {
			joined = joined[:0]
			for k := i; k < j; k++ {
				m.logical[k], m.start[k] = l, int32(len(joined))
				if k < j-1 {
					joined = append(joined, wrappedRow(all[k])...)
				} else {
					joined = append(joined, lastRow(all[k], blank, k == cy, cx)...)
				}
			}
			cells = joined
		}
		out = b.wrap(out, cells, cols, &m.newStart)
		i = j
	}
	m.first = append(m.first, int32(len(out)))
	m.rows = len(out)

	// The old screen's top row is where the new screen starts, before it is made the right height.
	top, _ := m.at(screen, 0)
	cr, _ := m.at(cy, cx)
	switch h := len(out) - top; {
	case h > rows:
		// Rows below the cursor go first, then rows off the top into history, as a shorter screen does.
		drop := h - rows
		below := min(drop, len(out)-1-cr)
		clear(out[len(out)-below:])
		out = out[:len(out)-below]
		top += drop - below
	case h < rows:
		// Rows come back out of history, down to the floor, before blank ones are added below.
		take := min(rows-h, top)
		if floor >= 0 {
			f, _ := m.at(floor, 0)
			take = min(take, max(top-f, 0))
		}
		top -= take
		for len(out)-top < rows {
			out = append(out, b.blankLine())
		}
	}
	m.rows = len(out)

	trim := 0
	if top > b.maxScroll {
		trim = top - b.maxScroll
	}
	b.scrollback = slices.Clip(out[trim:top])
	b.lines = append([]line(nil), out[top:]...)
	return m, top
}

// wrap appends cells to out as rows of cols, marking every row but the last as wrapped, and appends to starts how
// far into cells each row begins.
func (b *buffer) wrap(out []line, cells []grid.Cell, cols int, starts *[]int32) []line {
	row := b.blankLine()
	*starts = append(*starts, 0)
	x := 0
	next := func(at int) {
		row[cols-1].Wrapped = true
		out = append(out, row)
		row = b.blankLine()
		*starts = append(*starts, int32(at))
		x = 0
	}
	for i := 0; i < len(cells); i++ {
		c := cells[i]
		c.Wrapped = false
		switch c.Width {
		case 2:
			if cols < 2 {
				// Too narrow for it at all.
				if i+1 < len(cells) && cells[i+1].Width == 0 {
					i++
				}
				continue
			}
			if x+2 > cols {
				// The column left over takes no rune, as print leaves it.
				if x < cols {
					row[x].Rune = 0
				}
				next(i)
			}
			cont := c
			cont.Rune, cont.Comb, cont.Width = 0, nil, 0
			if i+1 < len(cells) && cells[i+1].Width == 0 {
				cont = cells[i+1]
				cont.Wrapped = false
				i++
			}
			row[x], row[x+1] = c, cont
			x += 2
		case 0:
			// Half a wide character with no lead.
			if x == cols {
				next(i)
			}
			row[x] = b.fill
			x++
		default:
			if x == cols {
				next(i)
			}
			row[x] = c
			x++
		}
	}
	return append(out, row)
}

// wraps reports whether a row runs on to the next.
func wraps(l line) bool { return len(l) > 0 && l[len(l)-1].Wrapped }

// wrappedRow is a row that runs on to the next, less the column a wide character left over when it moved down.
func wrappedRow(l line) line {
	if n := len(l); n > 0 && l[n-1].Rune == 0 && l[n-1].Width == 1 {
		return l[:n-1]
	}
	return l
}

// lastRow is the last row of a logical line without its blank end, keeping the cursor's cell when the cursor is on
// it.
func lastRow(l line, blank grid.Cell, cursor bool, cx int) line {
	n := len(l)
	for n > 0 && blankOn(l[n-1], blank.BG) {
		n--
	}
	if cursor {
		n = max(n, min(cx+1, len(l)))
	}
	return l[:n]
}
