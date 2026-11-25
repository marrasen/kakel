package termview

import (
	"image/color"
	"math"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/grid"
)

// compose is the text an input method is composing, which no program has
// yet: it is drawn over the cells from the cursor on, underlined.
type compose struct {
	text string
	// caret is how many cells in its caret is, and wide how many it takes.
	caret, wide int
	cells       *widget.CellGrid
	// ink and ground are the screen's own colours.
	ink, ground color.NRGBA
}

func (c *compose) init(g *anim.Group) {
	c.cells = widget.NewCellGrid()
	g.Add(c.cells)
}

// on reports whether anything is composed.
func (c *compose) on() bool { return c.text != "" }

// Composing is the text an input method is composing in the terminal, ""
// for none.
func (t *Term) Composing() string { return t.compose.text }

// composing takes an input method's latest text, empty once it is done.
func (t *Term) composing(e gi.Composing, u *gunim.UI) {
	if e.Text == "" && !t.compose.on() {
		return
	}
	c := &t.compose
	c.text, c.caret, c.wide = e.Text, 0, 0
	at := max(0, min(e.Selected[1], len(e.Text)))
	for i, r := range e.Text {
		w := max(grid.RuneWidth(r), 0)
		if i < at {
			c.caret += w
		}
		c.wide += w
	}
	if c.on() {
		// Where typing goes, as a key brings it there.
		t.sh.T.ScrollView(math.MinInt32)
	}
	t.blinkAgain()
	t.typed(u)
}

// paintCompose draws what is composed, in the cells' own space: from the
// cursor on, or as far to the left of it as it needs, and its end where
// the screen is too narrow for all of it.
func (t *Term) paintCompose(p *paint.Painter, f gunim.Frame) {
	c := &t.compose
	cols, rows := t.cells.GridSize()
	if !c.on() || cols < 1 || t.cursorAt.Y >= rows {
		return
	}
	ink, ground := t.compose.ink, t.compose.ground
	row := make([]widget.Cell, 0, c.wide+1)
	for _, r := range c.text {
		w := grid.RuneWidth(r)
		if w < 1 {
			// A mark on the character before.
			n := len(row)
			if n > 1 && row[n-2].Wide {
				n--
			}
			if n > 0 {
				row[n-1].Marks += string(r)
			}
			continue
		}
		row = append(row, widget.Cell{Rune: r, FG: ink, BG: ground, Wide: w == 2, Style: widget.CellUnderline})
		if w == 2 {
			row = append(row, widget.Cell{})
		}
	}
	// And the caret's, past its end.
	row = append(row, widget.Cell{Rune: ' ', FG: ink, BG: ground})
	caret := min(c.caret, len(row)-1)
	if over := len(row) - cols; over > 0 {
		row, caret = row[over:], max(caret-over, 0)
	}
	g := c.cells
	g.Faces, g.Size, g.Background = t.cells.Faces, t.cells.Size, t.cells.Background
	g.Resize(len(row), 1)
	g.SetRow(0, row)
	g.SetCursor(widget.Cursor{Col: caret, Shape: widget.CursorBar, Visible: t.typedIn() && !t.away, Blinked: t.blinkOff && t.wantBlink, Color: ink})
	size := g.Layout(gunim.Constraints{}, f, gunim.Children{})
	cell := t.cells.CellSize()
	x := min(t.cursorAt.X, cols-len(row))
	defer p.Push(paint.Translate(geom.Pt(float32(x)*cell.W, float32(t.cursorAt.Y)*cell.H)))()
	g.Paint(p, f, size, gunim.Children{})
}
