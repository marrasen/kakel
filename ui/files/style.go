// Package files reads files for kakel's readers: a file's lines or its
// picture, and the reader that shows them.
//
// Nothing here reads a file on the goroutine that draws. A slow mount
// or a connection with a long way to go would stop the window, so a
// read is handed to another goroutine and the answer is handed back.
package files

import (
	"fmt"
	"image/color"
)

// Style colours a reader.
type Style struct {
	// FG and BG are an ordinary row, and the whole pane's background.
	FG, BG color.RGBA

	// SelectedFG and SelectedBG mark the row the keys act on.
	SelectedFG, SelectedBG color.RGBA

	// HeaderFG is the line at the top naming the machine, and PathFG the
	// one under it saying which directory is being shown.
	HeaderFG color.RGBA
	PathFG   color.RGBA

	// DirFG is a directory, LinkFG a symbolic link, and MarkedFG a name
	// the user has picked out. A reader borrows the last two: LinkFG for
	// a string, and MarkedFG for a heading, a bullet and a search match.
	DirFG, LinkFG, MarkedFG color.RGBA

	// ClipFG is a name waiting to be pasted somewhere.
	ClipFG color.RGBA

	// KeyFG is a key on the bar along the bottom, and OffBG the ground
	// behind one with nothing wired to it. A key that does nothing here
	// is still worth reading, so OffBG sits between the bar's own ground
	// and the one a working key is marked out on.
	KeyFG color.RGBA
	OffBG color.RGBA

	// NoteFG is the size or the time at the end of a row.
	NoteFG color.RGBA

	// ErrorFG is the line that says why the directory could not be read.
	ErrorFG color.RGBA
}

// size writes a byte count the way a person reads one.
func size(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for n/div >= unit && exp < 4 {
		div *= unit
		exp++
	}
	value := float64(n) / float64(div)
	suffix := [...]string{"kB", "MB", "GB", "TB", "PB"}[exp]
	if value < 10 {
		return fmt.Sprintf("%.1f %s", value, suffix)
	}
	return fmt.Sprintf("%.0f %s", value, suffix)
}

// sizeIn writes a byte count in the unit another one would be written
// in, and with the same decimals, so a pair reads as "1.9 of 4.2 MB"
// rather than as two counts the reader has to line up.
func sizeIn(n, of int64) string {
	const unit = 1024
	if of < unit {
		return fmt.Sprintf("%d", n)
	}
	div, exp := int64(unit), 0
	for of/div >= unit && exp < 4 {
		div *= unit
		exp++
	}
	if float64(of)/float64(div) < 10 {
		return fmt.Sprintf("%.1f", float64(n)/float64(div))
	}
	return fmt.Sprintf("%.0f", float64(n)/float64(div))
}
