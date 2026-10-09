package view

import (
	"testing"

	"github.com/marrasen/kakel/grid"
	"github.com/marrasen/kakel/vt"
)

// Text coloured too close to its ground, as ls colours a folder anyone
// may write to and PowerShell any folder, reads in the cells drawn; a
// half block, a pixel pair of a picture, keeps its colours.
func TestTextTooCloseToItsGroundIsMadeToRead(t *testing.T) {
	term := vt.New(10, 2, vt.DefaultPalette(), 0, vt.Callbacks{})
	_, _ = term.Write([]byte("\x1b[34;42mA\x1b[0m\x1b[44mB\x1b[0m\x1b[34;42m▀\x1b[0mC"))
	pal := vt.DefaultPalette()
	g := grid.New(10, 2, pal.FG, pal.BG)
	term.Screen().Render(g)
	for x, what := range []string{"blue on green", "default text on blue"} {
		c := cellOf(g, x, 0)
		if r := grid.Contrast(rgba(c.FG), rgba(c.BG)); r < 4.5-0.05 {
			t.Errorf("%s contrasts %.2f in the cell drawn, want 4.5", what, r)
		}
	}
	if c := cellOf(g, 2, 0); rgba(c.FG) != g.FGOf(2, 0) {
		t.Errorf("a half block's colour changed from %v to %v", g.FGOf(2, 0), c.FG)
	}
	if c := cellOf(g, 3, 0); rgba(c.FG) != g.FGOf(3, 0) {
		t.Errorf("plain text that reads changed colour from %v to %v", g.FGOf(3, 0), c.FG)
	}
}
