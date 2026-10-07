package files

import (
	"github.com/marrasen/kakel/grid"
	"github.com/marrasen/kakel/input"
	"github.com/marrasen/kakel/ui"
)

// Key is one key on the bar along the bottom of the reader: the chord
// it wants, what the bar calls it, and what it does.
type Key struct {
	Chord ui.Chord

	// Typed is the character this key is, for one that is a character
	// rather than a place on the keyboard: "/" is "/" wherever a layout
	// puts it, and binding the key beside the right shift would find it
	// on one layout and not on another.
	Typed rune

	// Shown is how the bar spells the chord, which is its own spelling
	// rather than Chord.String(): "^G" fits a bar of ten keys where
	// "ctrl+G" does not.
	Shown string
	Title string
}

// press is the key press this chord is, for asking whether a key on the
// bar is this one and for running it from a click.
func (k Key) press() input.Event {
	if k.Typed != 0 {
		return input.Event{Kind: input.Text, Rune: k.Typed, NormalText: true}
	}
	return input.Event{Kind: input.KeyPress, Key: k.Chord.Key, Mods: k.Chord.Mods}
}

// chord names a key and the modifiers held with it.
func chord(k input.Key, mods input.Mods) ui.Chord { return ui.Chord{Key: k, Mods: mods} }

// keyCell returns the columns one key on the bar is drawn in.
//
// The width is divided by counting from the left edge each time rather
// than by stepping, so the remainder is spread down the bar and the
// last cell ends exactly at the right edge.
func keyCell(i, cols, n int) (start, end int) {
	if n <= 0 {
		return 0, 0
	}
	return cols * i / n, cols * (i + 1) / n
}

// keyAt returns which key on the bar a column belongs to.
//
// It asks keyCell rather than dividing the other way, because the two
// have to agree exactly: a click has to run the key it looks like it is
// on, and a bar is short enough that walking it costs nothing.
func keyAt(col, cols, n int) (int, bool) {
	if n <= 0 || cols <= 0 || col < 0 || col >= cols {
		return 0, false
	}
	for i := range n {
		if start, end := keyCell(i, cols, n); col >= start && col < end {
			return i, true
		}
	}
	return 0, false
}

// drawKeys paints the bar.
//
// Every cell is written once, with the same value each frame, so a
// reader nobody is touching leaves the row clean.
func drawKeys(v grid.View, y, cols int, keys []Key, st Style, wired func(Key) bool) {
	if cols <= 0 || len(keys) == 0 {
		return
	}
	for i, k := range keys {
		start, end := keyCell(i, cols, len(keys))
		// The key itself, on the bar's own ground, the way a number is
		// on the bar this was copied from.
		at := start
		for _, r := range k.Shown {
			if at >= end {
				break
			}
			v.Set(at, y, grid.Cell{Rune: r, FG: st.KeyFG, BG: st.BG, Width: 1})
			at++
		}
		// Then what it does, marked out, so the bar reads as a row of
		// keys rather than a sentence. A blank column in front of the
		// word, inside the marked-out part, so the key and its name do
		// not run into one another.
		fg, bg := st.SelectedFG, st.SelectedBG
		if wired != nil && !wired(k) {
			// Nothing is wired to it here, so it is shown without being
			// offered: dimmer than a key that works, and still lit
			// enough to read.
			fg, bg = st.KeyFG, st.OffBG
		}
		title := grid.Trim(" "+k.Title, end-at)
		at = v.SetString(at, y, title, fg, bg, 0)
		for ; at < end; at++ {
			v.Set(at, y, grid.Cell{Rune: ' ', FG: fg, BG: bg, Width: 1})
		}
	}
}
