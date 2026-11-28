// Package termview draws a terminal and takes what is typed and clicked
// in it: the view of a [screen.Shell], for a gunim window.
//
// It knows nothing of the program round it, and is built for a web
// browser as well as for the desktop. What it has to tell the program,
// such as a clipboard that could not be read, goes to the callbacks the
// program gives it.
package termview

import (
	"image"
	"image/color"
	"math"
	"time"

	"github.com/marrasen/kakel/look"

	"github.com/marrasen/kakel/screen"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/grid"
	"github.com/marrasen/kakel/input"
	"github.com/marrasen/kakel/ui"
	uiterm "github.com/marrasen/kakel/ui/term"
	"github.com/marrasen/kakel/winkeys"
)

// Term is the terminal on screen: a CellGrid showing the shell's
// screen, taking keys for it.
//
// A program's own node may embed it, to draw more over it or to take
// more of what is done to it. So the callbacks send nothing themselves:
// the program sends what it wants to from the node it put in its tree.
type Term struct {
	anim.Group
	// OnFocus says the pane took the keyboard.
	OnFocus func(u *gunim.UI)
	// OnPaste is the program's look at text about to be pasted, which
	// reports true when it took the text, to show it first.
	OnPaste func(text string, u *gunim.UI) bool
	// OnPasteImage says the user pasted, and the clipboard has no text:
	// what it may have is an image.
	OnPasteImage func(u *gunim.UI)
	// OnNoText says the user pasted with the middle button, which pastes
	// text alone, and the clipboard has none.
	OnNoText func(u *gunim.UI)
	// OnClipboardUnreadable says the user pasted, and why the clipboard
	// could not be read.
	OnClipboardUnreadable func(err error, u *gunim.UI)
	// OnCtrl tells every pane in the window that Ctrl went down or came
	// up, for the one under the pointer, which may be another than this.
	// With none, this one alone hears it.
	OnCtrl func(k gi.Key, mods gi.Mods, down bool, u *gunim.UI)
	// OnHeld says a finger held in the terminal was lifted at at, having
	// selected the word there, or kept the selection it was in, and what
	// it was dragged over since: the place for a menu. With none a finger
	// held selects nothing, and the press is the window's, as a right
	// click is.
	OnHeld func(at geom.Point, u *gunim.UI)
	// AlongWith is the other panes typing goes to from this one while it
	// has the keyboard, nil for none.
	AlongWith func() []*Term
	// TextInput has the platform's text input feed the terminal, as a
	// browser needs for a phone's keyboard and for input methods. A
	// desktop's input methods are left out of it for now.
	TextInput bool

	keys    *ui.Keymap
	sh      *screen.Shell
	cells   *widget.CellGrid
	row     []widget.Cell
	focused bool
	// away says the window is without the keyboard, another program
	// having it: no cursor shows, and none blinks, until it is back.
	away bool
	// along says typing goes to this pane too, from the one with the
	// keyboard, as Type in All Panes does: its cursor shows and blinks
	// as that one's does.
	along bool
	// wheel gathers the wheel's movement until it makes a whole notch.
	wheel float32
	// held is the button down in the pane, and at the cell the pointer
	// was last heard of in.
	held input.MouseButton
	// tap is a finger's press while tapping, which does what it does as
	// the finger lifts, if it neither scrolled nor was held; tapIn says
	// the terminal had the keyboard as it came. holding says a finger
	// held selects.
	tap                     gi.PointerDown
	tapping, tapIn, holding bool
	// edge is how many rows past the top, negative, or the bottom of the
	// cells the pointer is while it drags a selection, and edgeLines the
	// part of a line the view has to scroll toward it still.
	edge      int
	edgeLines float64
	// hoverMods are the modifiers the pointer last moved with, or held
	// since, and over says the pointer is over the pane.
	hoverMods input.Mods
	over      bool
	at        grid.Point
	// wantBlink says the program asked for a blinking cursor, blinking
	// that a blink is running, and blinkOff that the cursor is in the
	// off half of one. relit says a key or a move of the cursor lit it
	// since the blink last stepped, so the next step leaves it lit.
	wantBlink, blinking, blinkOff, relit bool
	// cursorAt is where the cursor was drawn, and cursorShown whether
	// it was; checking says a look at a cursor hidden is on its way.
	cursorAt              grid.Point
	cursorShown, checking bool
	// small is a size too small to give the shell until the pane has
	// held it a moment, since when, and settle keeps frames coming
	// while it waits.
	small      [2]int
	smallSince time.Time
	settle     *anim.Float
	// selGrow is how far a word or line a double or triple click
	// picked has grown out from the click, 0 to 1.
	selGrow *anim.Float
	// growStart is the terminal's count of growths begun, as selGrow
	// last started for one.
	growStart uint64
	// quick is the theme's motion for it, as the pointer last found
	// it, for a growth begun as the view scrolls under a drag.
	quick anim.Motion
	// pics are the inline images on screen as the painter holds
	// them, by the image each was made from.
	pics map[image.Image]*paint.Image
	// scale is how much a held screen bigger than the pane is shrunk to
	// fit it, and offset where it is drawn: 1 and nothing otherwise.
	scale  float32
	offset geom.Point
	// fit is a fitted screen's place in the pane; see [Term.SetFitted].
	fit fit
	// compose is what an input method is composing in the pane.
	compose compose
	// u is the window's, as last heard of, and recoloured the terminal's
	// count of palettes as the cells were last copied.
	u          *gunim.UI
	recoloured uint64
}

// LeastCols and LeastRows are the smallest screen a shell is given at
// once. A smaller one is given once the pane has held it for a moment:
// a pane sliding in or folding away passes through every width on the
// way, and a shell told each of them would print its prompt a few
// columns wide.
const LeastCols, LeastRows = 20, 3

// smallSettle is how long a pane holds a smaller screen before its
// shell is given it.
const smallSettle = 150 * time.Millisecond

// SetAway hides the cursor while the window is without the keyboard,
// and shows it again, lit, and blinking where it blinks, once it is
// back: the cursor is where typing goes, and none goes to a window in
// the background. u is nil for a terminal that is in no window yet.
func (t *Term) SetAway(away bool, u *gunim.UI) {
	if away == t.away {
		return
	}
	t.away = away
	if u == nil {
		// Nothing is pressed or lit yet, and no blink has begun.
		t.Sync()
		return
	}
	if !away {
		t.blinkAgain()
		t.Blink(u)
	} else {
		// Another program took the keyboard, as a browser a Ctrl+click
		// opened does on the press: the release, and Ctrl coming up,
		// go to it. The press ends here, and no link stays lit or
		// named at the foot of the pane.
		if t.held != input.MouseNone {
			// Let go where it was, so a program that has the mouse
			// hears the button come up, and a selection ends there.
			held := t.held
			t.held, t.edge, t.holding = input.MouseNone, 0, false
			t.mouse(input.MouseEvent{Kind: input.MouseRelease, Button: held, Col: t.at.X, Row: t.at.Y}, u)
		}
		t.tapping = false
		t.hoverMods = 0
		t.sh.T.SetHover(t.at.X, t.at.Y, 0)
	}
	t.Sync()
	u.Invalidate()
}

// blinkHalf is each half of a cursor's blink: once a second.
const blinkHalf = 500 * time.Millisecond

// Blink starts the cursor blinking, while the pane has the keyboard and
// its program asked for a blinking cursor.
func (t *Term) Blink(u *gunim.UI) {
	if t.blinking || !t.typedIn() || t.away || !t.wantBlink {
		return
	}
	// A light from while no blink ran is long over: this one starts
	// lit for a half, as each does.
	t.blinking, t.relit = true, false
	u.After(blinkHalf, t.blinkStep)
}

func (t *Term) blinkStep(u *gunim.UI) {
	switch {
	case !t.typedIn() || t.away || !t.wantBlink:
		t.blinking, t.blinkOff, t.relit = false, false, false
	case t.relit:
		// Lit a moment ago: it stays lit a whole half at least.
		t.relit = false
	default:
		t.blinkOff = !t.blinkOff
	}
	if t.blinking {
		u.After(blinkHalf, t.blinkStep)
	}
	t.Sync()
	u.Invalidate()
}

// blinkAgain lights the cursor, as a key or a move of the cursor does:
// the cursor is on screen where the user is looking. The blink keeps
// running, and goes on from there. It never stops it: a move of the
// cursor the shell's output made is seen while the window lays out,
// with nothing there to start a blink again.
func (t *Term) blinkAgain() {
	t.blinkOff, t.relit = false, true
}

// LookAgain looks at the cursor again once a hide would have landed:
// the terminal keeps a hidden cursor on screen a moment, across a
// program's repaint, and nothing else may draw the pane after it.
func (t *Term) LookAgain(u *gunim.UI) {
	if t.checking || !t.cursorShown {
		return
	}
	t.checking = true
	u.After(uiterm.CursorHideGrace+10*time.Millisecond, func(u *gunim.UI) {
		t.checking = false
		t.Sync()
		u.Invalidate()
	})
}

// New is the view of shell sh. Of the commands keys gives shortcuts to,
// the terminal carries out its own; see [Term.Command].
func New(sh *screen.Shell, keys *ui.Keymap) *Term {
	g := widget.NewCellGrid()
	// The terminal names the pointer's shape over its cells: a hand
	// over a link Ctrl and a click would follow.
	g.Pointer = func(geom.Point) gi.Cursor { return gi.CursorInherit }
	g.Size = 15
	g.Background = look.TermBackground
	t := &Term{keys: keys, sh: sh, cells: g, settle: anim.NewFloat(0)}
	t.selGrow = anim.NewFloat(1)
	t.fit.init(&t.Group)
	t.compose.init(&t.Group)
	t.recoloured = sh.T.Recoloured()
	// settle counts real time, whatever pace the theme sets.
	t.settle.RealTime()
	t.Add(t.settle, t.selGrow)
	// The whole screen, not just the rows changed since the last pane
	// drew it: this one may be in a window the pane has just moved to.
	sh.Drawn(func(g *grid.Grid) { g.MarkAllDirty() })
	t.Sync()
	return t
}

// Cells are the cells the terminal draws in, to set their faces and
// their size.
func (t *Term) Cells() *widget.CellGrid { return t.cells }

// Drawn is where the terminal's top left cell is drawn, in the
// terminal's own space, and the size a cell is drawn at.
func (t *Term) Drawn() (geom.Point, geom.Size) {
	cell := t.cells.CellSize()
	if t.placed() {
		cell = geom.Sz(cell.W*t.scale, cell.H*t.scale)
	}
	return t.offset, cell
}

// Shell is the shell the terminal shows.
func (t *Term) Shell() *screen.Shell { return t.sh }

// SetFitted has the screen keep the size it is, which the far end sets,
// and sizes its text so that the whole of it fills the pane as far as it
// can, in the middle; or has it take as many cells as fit the pane
// again, at the size of text the cells had. Either way it glides there.
func (t *Term) SetFitted(on bool, u *gunim.UI) {
	if on == t.fit.on {
		return
	}
	t.fit.set(on, t.cells)
	u.Invalidate()
}

// SetCell has a fitted screen's text no larger than the size whose cell
// fits in cell, the size of a cell the far end has: every screen given the
// same cell then has the one size of text, where each has the room. A
// zero cell has the text as large as the pane has room for. Either way
// the text glides to its new size.
func (t *Term) SetCell(cell geom.Size, u *gunim.UI) {
	if cell == t.fit.cell {
		return
	}
	t.fit.cell, t.fit.due = cell, t.fit.laid
	u.Invalidate()
}

// Fitted reports whether the screen is fitted; see [Term.SetFitted].
func (t *Term) Fitted() bool { return t.fit.on }

// CursorLit reports whether the cursor is in the lit half of its blink,
// as one that does not blink always is.
func (t *Term) CursorLit() bool { return !t.blinkOff }

// Focused reports whether the terminal has the keyboard.
func (t *Term) Focused() bool { return t.focused }

// CursorShown reports whether the terminal draws its cursor.
func (t *Term) CursorShown() bool { return t.cursorShown }

// Pressed is the mouse button held down in the terminal.
func (t *Term) Pressed() input.MouseButton { return t.held }

// Hover reports whether the pointer is over the terminal, and with
// which modifiers.
func (t *Term) Hover() (mods input.Mods, over bool) { return t.hoverMods, t.over }

// TakesText implements [gunim.TextTaker].
func (t *Term) TakesText() bool { return t.TextInput }

// RawText implements [gunim.RawTexter]: a phone's keyboard leaves what is
// typed as it is.
func (t *Term) RawText() bool { return true }

// TextCaret implements [gunim.CaretReporter]: the cursor's cell, for an
// input method to open beside.
func (t *Term) TextCaret() geom.Rect {
	at, cell := t.Drawn()
	return geom.Rc(at.X+float32(t.cursorAt.X)*cell.W, at.Y+float32(t.cursorAt.Y)*cell.H, cell.W, cell.H)
}

// ClaimsKeys implements [gunim.KeyClaimer]: the terminal wants every key
// the platform can give up, as a browser can the keys it reloads, finds
// and pastes with. A program in it may want any of them.
func (t *Term) ClaimsKeys() bool { return true }

// Children implements [gunim.Composite].
func (t *Term) Children() []gunim.Node { return []gunim.Node{t.cells} }

// Focusable implements [gunim.Focusable].
func (t *Term) Focusable() bool { return true }

// Layout implements [gunim.Node]. The shell takes as many cells as fit,
// or fits the pane as it is.
func (t *Term) Layout(c gunim.Constraints, f gunim.Frame, kids gunim.Children) geom.Size {
	k := kids.At(0)
	if t.fit.on {
		at := t.fit.layout(k, c.Max, t.cells, f)
		t.scale, t.offset = t.fit.glide(1, at, t.cells.CellSize().H, f)
		return c.Max
	}
	size := k.Layout(c)
	k.Place(geom.Point{})
	cols, rows := t.cells.Fit()
	give := false
	switch {
	case cols < 1 || rows < 1:
	case cols >= LeastCols && rows >= LeastRows:
		t.small, give = [2]int{}, true
	case t.small != [2]int{cols, rows}:
		// Too small to give at once: given if the pane stays so.
		t.small, t.smallSince = [2]int{cols, rows}, f.Now
		t.settle.Jump(0)
		t.settle.Animate(1, anim.Tween{Duration: smallSettle + 50*time.Millisecond})
	default:
		give = f.Now.Sub(t.smallSince) >= smallSettle
	}
	if give && t.sh.Resize(cols, rows) {
		t.Sync()
	}
	// A screen somebody watching has sized bigger than this pane is laid
	// out whole, and drawn shrunk to fit, keeping its shape, in the
	// middle of the pane.
	t.scale, t.offset = 1, geom.Point{}
	cell := t.cells.CellSize()
	cols, rows = t.cells.GridSize()
	need := geom.Sz(float32(cols)*cell.W, float32(rows)*cell.H)
	if t.sh.T.Held() && (need.W > size.W || need.H > size.H) && need.W > 0 && need.H > 0 {
		t.scale = min(size.W/need.W, size.H/need.H)
		k.Layout(gunim.Tight(need))
		t.offset = geom.Pt((size.W-need.W*t.scale)/2, (size.H-need.H*t.scale)/2)
	}
	t.scale, t.offset = t.fit.glide(t.scale, t.offset, cell.H, f)
	return size
}

// Paint implements [gunim.Node]: the cells, and over them the
// images programs put in the output.
func (t *Term) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	if t.fit.gliding() {
		// On its way from where it was larger, it keeps to the pane.
		defer p.Layer(paint.LayerOpts{Bounds: geom.Rect{Max: box.Point()}, Opacity: 1, Clip: true})()
	}
	if t.placed() {
		defer p.Push(paint.Translate(t.offset))()
		defer p.Push(paint.Scale(t.scale, geom.Point{}))()
	}
	kids.At(0).Paint(p)
	t.paintImages(p)
	t.paintCompose(p, f)
}

// paintImages draws the inline images on screen, each over the
// cells it was given. An image half scrolled off is drawn in part.
func (t *Term) paintImages(p *paint.Painter) {
	placed := t.sh.T.Images()
	if len(placed) == 0 && len(t.pics) == 0 {
		return
	}
	cols, rows := t.cells.GridSize()
	cell := t.cells.CellSize()
	kept := make(map[image.Image]*paint.Image, len(placed))
	for _, at := range placed {
		if at.Img == nil || at.Cols <= 0 || at.Rows <= 0 {
			continue
		}
		img, ok := t.pics[at.Img]
		if !ok {
			img = paint.NewImage(at.Img)
		}
		kept[at.Img] = img
		top, bottom := max(at.Top, 0), min(at.Top+at.Rows, rows)
		left, right := max(at.Col, 0), min(at.Col+at.Cols, cols)
		if top >= bottom || left >= right {
			continue
		}
		// The part of the image that shows, in its own pixels.
		w, h := img.Size()
		sx, sy := float32(w)/float32(at.Cols), float32(h)/float32(at.Rows)
		src := geom.Rc(float32(left-at.Col)*sx, float32(top-at.Top)*sy, float32(right-left)*sx, float32(bottom-top)*sy)
		dst := geom.Rc(float32(left)*cell.W, float32(top)*cell.H, float32(right-left)*cell.W, float32(bottom-top)*cell.H)
		p.Image(img, dst, paint.ImageOpts{Src: src, Opacity: 1})
	}
	t.pics = kept
}

// Sync draws the shell's screen and copies the rows that changed into
// the grid, with the cursor.
func (t *Term) Sync() {
	sh := t.sh
	sh.Drawn(func(g *grid.Grid) {
		if n := sh.T.Recoloured(); n != t.recoloured {
			t.recoloured = n
			// The colours glide with the theme's, where they show.
			u := t.u
			if u != nil && u.Presence(t.cells) == gunim.Exiting {
				u = nil
			}
			t.cells.FadeColors(u)
		}
		t.compose.ink, t.compose.ground = opaque(g.DefaultFG), opaque(g.DefaultBG)
		cols, rows := g.Size()
		t.cells.Resize(cols, rows)
		for y := range rows {
			if !g.RowDirty(y) {
				continue
			}
			t.row = t.row[:0]
			for x := range cols {
				t.row = append(t.row, CellOf(g, x, y))
			}
			t.cells.SetRow(y, t.row)
		}
		g.ClearDirty()
		cur := g.Cursor()
		shape := widget.CursorBlock
		switch cur.Style {
		case grid.CursorBar:
			shape = widget.CursorBar
		case grid.CursorUnderline:
			shape = widget.CursorUnderline
		case grid.CursorBlock:
		}
		t.wantBlink = cur.Blink
		// A pane whose program has ended takes no typing, and one without
		// the keyboard takes none now, so neither shows a cursor.
		// An input method's has its own.
		visible := cur.Visible && !sh.T.Exited() && t.typedIn() && !t.away && !t.compose.on()
		if at := (grid.Point{X: cur.X, Y: cur.Y}); at != t.cursorAt {
			t.cursorAt = at
			t.blinkAgain()
		}
		t.cursorShown = visible
		t.cells.SetCursor(widget.Cursor{Col: cur.X, Row: cur.Y, Shape: shape, Visible: visible,
			Blinked: t.blinkOff && t.wantBlink && t.typedIn()})
	})
}

// minContrast is how far at least a cell's text contrasts with its
// ground, as WCAG measures it: text a program colours too close to its
// ground, as ls does a folder anyone may write to, blue on green, is
// lightened or darkened until it reads. Dim text needs half as much.
const minContrast = 4.5

// shapeRune reports whether r draws a shape rather than a letter: a line
// of a box, a block, a shade, or a Powerline symbol. Pictures drawn with
// half blocks use the text's colour and the ground's as two pixels, so
// these keep their colours.
func shapeRune(r rune) bool {
	return r >= 0x2500 && r <= 0x259f || r >= 0xe0a0 && r <= 0xe0d4
}

// CellOf turns one of kakel's cells into gunim's, with its colours
// resolved, and its text made to read on its ground.
func CellOf(g *grid.Grid, x, y int) widget.Cell {
	c := g.At(x, y)
	if c.Width == 0 {
		return widget.Cell{}
	}
	fg, bg := g.FGOf(x, y), g.BGOf(x, y)
	switch {
	case c.Attr&grid.AttrHidden != 0:
		fg = bg
	case c.Attr&grid.AttrDim != 0:
		fg = grid.Blend(fg, bg, 1, 2)
		if !shapeRune(c.Rune) {
			fg = grid.Readable(fg, bg, minContrast/2)
		}
	case !shapeRune(c.Rune):
		fg = grid.Readable(fg, bg, minContrast)
	}
	out := widget.Cell{
		Rune: c.Rune,
		FG:   opaque(fg),
		BG:   opaque(bg),
		Wide: c.Width == 2,
	}
	if len(c.Comb) > 0 {
		out.Marks = string(c.Comb)
	}
	for _, s := range [...]struct {
		a grid.Attr
		s widget.CellStyle
	}{{grid.AttrBold, widget.CellBold}, {grid.AttrItalic, widget.CellItalic}, {grid.AttrUnderline, widget.CellUnderline}, {grid.AttrStrike, widget.CellStrike}} {
		if c.Attr&s.a != 0 {
			out.Style |= s.s
		}
	}
	return out
}

// opaque is a grid's colour as gunim has it.
func opaque(c color.RGBA) color.NRGBA { return color.NRGBA{R: c.R, G: c.G, B: c.B, A: 0xff} }

// Handle implements [gunim.Handler]: keys and text go to the shell,
// and the wheel scrolls back through what has scrolled off.
func (t *Term) Handle(e gi.Event, u *gunim.UI) bool {
	t.u = u
	switch e := e.(type) {
	case gi.FocusGained, gi.FocusLost:
		_, t.focused = e.(gi.FocusGained)
		t.composing(gi.Composing{}, u)
		if t.focused && t.OnFocus != nil {
			t.OnFocus(u)
		}
		// Lit, for a whole half, though a blink still runs from the
		// last time the pane had the keyboard.
		t.blinkAgain()
		t.Sync()
		t.Blink(u)
		t.Sync()
		u.Invalidate()
		return true
	case gi.KeyPress:
		t.ctrlChanged(e.Key, e.Mods, true, u)
		// A press that typed leaves it to the text, which follows.
		if e.Typed {
			return true
		}
		ev, ok := winkeys.Event(e)
		if !ok {
			return true
		}
		if id, bound := t.keys.Lookup(ui.ChordOf(ev)); bound {
			// The window's shortcut, unless the terminal carries it out.
			return t.Command(id, u)
		}
		t.key(ev)
		t.typed(u)
		return true
	case gi.KeyRelease:
		t.ctrlChanged(e.Key, e.Mods, false, u)
		return false
	case gi.Composing:
		if !t.TextInput {
			return false
		}
		t.composing(e, u)
		return true
	case gi.TextInput:
		t.composing(gi.Composing{}, u)
		for _, r := range e.Text {
			t.key(input.Event{Kind: input.Text, Rune: r, NormalText: true})
		}
		t.typed(u)
		return true
	case gi.PointerLeave:
		t.over = false
		return false
	case gi.Scroll:
		if e.Mods&gi.ModControl != 0 {
			// Ctrl and the wheel size the font, which the window does.
			return false
		}
		t.scroll(e, u)
		return true
	case gi.PointerDown:
		return t.press(e, u)
	case gi.PointerMove:
		return t.drag(e, u)
	case gi.PointerUp:
		return t.release(e, u)
	}
	return false
}

// typed shows what a key did, with the cursor lit.
func (t *Term) typed(u *gunim.UI) {
	t.Sync()
	t.Blink(u)
	u.Invalidate()
}

// ctrlChanged hands a Ctrl going down or coming up to every pane, or to
// this one when it stands alone.
func (t *Term) ctrlChanged(k gi.Key, mods gi.Mods, down bool, u *gunim.UI) {
	if t.OnCtrl != nil {
		t.OnCtrl(k, mods, down, u)
		return
	}
	t.CtrlHeld(k, mods, down, u)
}

// CtrlHeld lights or unlights the link under a still pointer as Ctrl
// goes down or comes up, rather than at the pointer's
// next move.
func (t *Term) CtrlHeld(k gi.Key, mods gi.Mods, down bool, u *gunim.UI) {
	if k != gi.KeyLeftControl && k != gi.KeyRightControl {
		return
	}
	m := MouseMods(mods) &^ input.ModCtrl
	if down {
		m |= input.ModCtrl
	}
	if !t.over || t.held != input.MouseNone || m == t.hoverMods {
		return
	}
	t.hoverMods = m
	t.sh.T.SetHover(t.at.X, t.at.Y, m)
	t.Sync()
	u.Invalidate()
}

// mouse hands a pointer event to the terminal, which reports it to a
// program that asked for the mouse, and otherwise selects.
func (t *Term) mouse(e input.MouseEvent, u *gunim.UI) bool {
	took, _ := t.sh.T.HandleMouse(e)
	t.quick = widget.Quick.Get(u.Theme())
	t.growSelection()
	t.Sync()
	u.Invalidate()
	return took
}

// growSelection starts the selection growing when the terminal began a
// growth since the last look: a word or line a double or triple click
// picked grows out from the click, and a drag by them grows or shrinks
// a whole one at a time.
func (t *Term) growSelection() {
	growing, start := t.sh.T.SelectionGrowing()
	if !growing || start == t.growStart {
		return
	}
	t.growStart = start
	t.selGrow.Jump(0)
	if t.quick != nil {
		t.selGrow.Animate(1, t.quick)
	}
	if !t.selGrow.Active() {
		// Motion turned off: it is there at once.
		t.sh.T.GrowSelection(1, true)
	}
}

// MouseMods is m as kakel's own mouse events have it.
func MouseMods(m gi.Mods) input.Mods {
	var out input.Mods
	if m.Has(gi.ModShift) {
		out |= input.ModShift
	}
	if m.Has(gi.ModAlt) {
		out |= input.ModAlt
	}
	if m.Has(gi.ModControl) {
		out |= input.ModCtrl
	}
	return out
}

// MouseButton is b as kakel's own mouse events have it, and none for a
// button they leave out.
func MouseButton(b gi.Button) input.MouseButton {
	switch b {
	case gi.ButtonPrimary:
		return input.MouseLeft
	case gi.ButtonMiddle:
		return input.MouseMiddle
	case gi.ButtonSecondary:
		return input.MouseRight
	}
	return input.MouseNone
}

func (t *Term) cellAt(p geom.Point) grid.Point {
	col, row := t.cells.CellAt(t.cellSpace(p))
	return grid.Point{X: col, Y: row}
}

// placed reports whether the cells are drawn anywhere but at the pane's
// corner, at their own size.
func (t *Term) placed() bool {
	return t.scale > 0 && (t.scale != 1 || t.offset != geom.Point{})
}

// cellSpace is p in the cells' own space.
func (t *Term) cellSpace(p geom.Point) geom.Point {
	if t.placed() {
		// Drawn shrunk, or in the middle: the point back in the cells'
		// own space.
		p = geom.Pt((p.X-t.offset.X)/t.scale, (p.Y-t.offset.Y)/t.scale)
	}
	return p
}

// rowsPast is how many rows above the cells, negative, or below them p
// is, and 0 over them.
func (t *Term) rowsPast(p geom.Point) int {
	h := t.cells.CellSize().H
	_, rows := t.cells.GridSize()
	if h <= 0 {
		return 0
	}
	y := t.cellSpace(p).Y
	switch {
	case y < 0:
		return int(y/h) - 1
	case y >= float32(rows)*h:
		return int((y-float32(rows)*h)/h) + 1
	}
	return 0
}

// Step implements [gunim.Animator]: a word or line picked by a double
// or triple click growing out to its ends, and the view scrolling
// toward a selection dragged past the cells' top or bottom, faster the
// further past, for as long as it is held there.
func (t *Term) Step(dt time.Duration) bool {
	moving := t.Group.Step(dt)
	if growing, start := t.sh.T.SelectionGrowing(); growing && start == t.growStart {
		t.sh.T.GrowSelection(t.selGrow.Value(), !t.selGrow.Active())
		t.Sync()
	}
	if t.edge == 0 || t.held != input.MouseLeft || !t.sh.T.Selecting() {
		t.edge, t.edgeLines = 0, 0
		return moving
	}
	n := t.edge
	if n < 0 {
		n = -n
	}
	n = min(n, 8)
	t.edgeLines += dt.Seconds() * float64(10*n)
	lines := int(t.edgeLines)
	t.edgeLines -= float64(lines)
	if lines > 0 {
		if t.edge < 0 {
			lines = -lines
		}
		if !t.sh.T.DragScroll(-lines) {
			// At the oldest line, or the live screen: nothing to scroll
			// toward. The next move past the edge starts it again.
			t.edgeLines = 0
			return moving
		}
		// A drag by words or lines took in another as it scrolled.
		t.growSelection()
		t.Sync()
	}
	return true
}

// press starts a selection, or a program's click; the middle button
// pastes, and the right is left to the window.
func (t *Term) press(e gi.PointerDown, u *gunim.UI) bool {
	at := t.cellAt(e.Pos)
	mods := MouseMods(e.Mods)
	// Any other press is in the place of a finger's that came before it.
	t.tapping = false
	switch {
	case e.Touch && e.Button == gi.ButtonPrimary:
		// It may scroll yet, or be held.
		t.tap, t.tapping, t.tapIn = e, true, t.focused
		return true
	case e.Touch && e.Button == gi.ButtonSecondary && t.OnHeld != nil && !t.sh.T.MouseTaken(mods):
		t.hold(at, u)
		return true
	case e.Focusing && e.Button == gi.ButtonPrimary:
		// The click that gives the pane the keyboard only does that: it
		// starts no selection, and a program with the mouse is not
		// clicked at a place nobody aimed for.
		return true
	case e.Button == gi.ButtonMiddle && !t.sh.T.MouseTaken(mods):
		// Text alone, the X11 way: an image is pasted with the key.
		s, err := u.ReadClipboard()
		switch {
		case err != nil:
			t.unreadable(err, u)
		case s != "":
			t.offer(s, u)
		case t.OnNoText != nil:
			t.OnNoText(u)
		}
		return true
	case e.Button == gi.ButtonSecondary && !t.sh.T.MouseTaken(mods):
		return false
	}
	t.held, t.at = MouseButton(e.Button), at
	return t.mouse(input.MouseEvent{Kind: input.MousePress, Button: t.held, Col: at.X, Row: at.Y, Mods: mods, Clicks: e.Clicks}, u)
}

// drag extends the selection, or tells a program the pointer moved.
func (t *Term) drag(e gi.PointerMove, u *gunim.UI) bool {
	at := t.cellAt(e.Pos)
	t.over = true
	// With Ctrl down, a link under the pointer is underlined, and a
	// click follows it.
	if t.held == input.MouseNone {
		mods := MouseMods(e.Mods)
		if mods != t.hoverMods || at != t.at {
			t.hoverMods = mods
			t.sh.T.SetHover(at.X, at.Y, mods)
			t.Sync()
			u.Invalidate()
		}
	}
	t.edge = 0
	if t.held == input.MouseLeft && t.sh.T.Selecting() {
		t.edge = t.rowsPast(e.Pos)
		if t.edge != 0 {
			// Past the edge the cell under the pointer stays the edge's,
			// so a frame is asked for here for the view to scroll.
			u.Invalidate()
		}
	}
	if at == t.at {
		return t.held != input.MouseNone
	}
	t.at = at
	return t.mouse(input.MouseEvent{Kind: input.MouseMove, Button: t.held, Col: at.X, Row: at.Y, Mods: MouseMods(e.Mods)}, u)
}

// hold selects the word at a cell, or keeps the selection that is there,
// as a finger is held on it. What it is dragged over is selected too.
func (t *Term) hold(at grid.Point, u *gunim.UI) {
	t.held, t.at, t.holding = input.MouseLeft, at, true
	if !t.sh.T.HoldSelection(at.X, at.Y) {
		t.mouse(input.MouseEvent{Kind: input.MousePress, Button: input.MouseLeft, Col: at.X, Row: at.Y, Clicks: 2}, u)
	}
}

// tapped does what a finger's press does, as it lifts at at. One let go at
// [gi.Away] scrolled, or is held, and did not tap. A tap that gave the
// terminal the keyboard only does that, as a click does.
func (t *Term) tapped(at geom.Point, u *gunim.UI) {
	d := t.tap
	t.tapping = false
	if at == gi.Away || !t.tapIn {
		return
	}
	d.Touch = false
	if t.press(d, u) {
		t.release(gi.PointerUp{Pos: d.Pos, Button: d.Button, Mods: d.Mods, Time: d.Time}, u)
	}
}

func (t *Term) release(e gi.PointerUp, u *gunim.UI) bool {
	if t.tapping && e.Button == gi.ButtonPrimary {
		t.tapped(e.Pos, u)
		return true
	}
	if t.held == input.MouseNone {
		return false
	}
	at := t.cellAt(e.Pos)
	held, holding := t.held, t.holding
	t.held, t.edge, t.holding = input.MouseNone, 0, false
	took := t.mouse(input.MouseEvent{Kind: input.MouseRelease, Button: held, Col: at.X, Row: at.Y, Mods: MouseMods(e.Mods)}, u)
	if holding && e.Pos != gi.Away {
		t.OnHeld(e.Pos, u)
	}
	return took
}

// CopySelection puts the selected text on the clipboard.
func (t *Term) CopySelection(u *gunim.UI) {
	if text := t.sh.T.SelectionText(); text != "" {
		u.SetClipboard(text)
	}
}

// Command carries out one of the window's commands that belongs to the
// terminal, and reports false for the rest, which go on to the window.
func (t *Term) Command(id string, u *gunim.UI) bool {
	switch id {
	case "edit.copy":
		t.CopySelection(u)
	case "edit.paste":
		t.PasteClipboard(u)
	case "view.scrollUp", "view.scrollDown":
		// Half a screen.
		page := 1
		if id == "view.scrollDown" {
			page = -1
		}
		t.sh.T.ScrollPages(page)
		t.Sync()
		u.Invalidate()
	default:
		return false
	}
	return true
}

// key hands one event to the terminal, which encodes it for the
// program, brings the view back to the live screen and clears the
// selection.
func (t *Term) key(ev input.Event) {
	t.blinkAgain()
	_, _ = t.sh.T.HandleKey(ev)
	// Each pane typed along with encodes the key itself, for the mode
	// its own program has set, such as the arrows an editor asks for.
	for _, o := range t.AlongOthers() {
		o.blinkAgain()
		_, _ = o.sh.T.HandleKey(ev)
		o.Sync()
	}
}

// offer pastes text the user pasted, unless the program takes it
// to show first.
func (t *Term) offer(s string, u *gunim.UI) {
	if t.OnPaste != nil && t.OnPaste(s, u) {
		return
	}
	t.Paste(s)
}

// Paste pastes text in the terminal, and in the panes typed in along
// with it.
func (t *Term) Paste(s string) {
	t.sh.T.Paste(s)
	for _, o := range t.AlongOthers() {
		o.sh.T.Paste(s)
		o.Sync()
	}
}

// AlongOthers is the other panes what is typed here goes to as well.
func (t *Term) AlongOthers() []*Term {
	if t.AlongWith == nil {
		return nil
	}
	return t.AlongWith()
}

// typedIn reports whether what is typed reaches this pane: it has the
// keyboard, or types along with the one that has.
func (t *Term) typedIn() bool { return t.focused || t.along }

// SetAlong has typing reach this pane from the one with the keyboard,
// or stop reaching it.
func (t *Term) SetAlong(on bool, u *gunim.UI) {
	if on == t.along {
		return
	}
	t.along = on
	t.blinkAgain()
	t.Blink(u)
	t.Sync()
	u.Invalidate()
}

// PasteClipboard pastes the text on the clipboard, and with no text
// there, asks the program to hand over the image that may be there
// instead. A clipboard holding both is text: copying from a browser
// leaves both, and the words are what was meant.
func (t *Term) PasteClipboard(u *gunim.UI) {
	s, err := u.ReadClipboard()
	switch {
	case err != nil:
		// Said, rather than taken for a clipboard with nothing on it.
		t.unreadable(err, u)
	case s != "":
		t.offer(s, u)
	case t.OnPasteImage != nil:
		t.OnPasteImage(u)
	}
}

// unreadable says the clipboard could not be read.
func (t *Term) unreadable(err error, u *gunim.UI) {
	if t.OnClipboardUnreadable != nil {
		t.OnClipboardUnreadable(err, u)
	}
}

// scroll hands the terminal a wheel notch at a time, which it takes as
// a report to a program that has the mouse, arrows on the alternate
// screen, and otherwise three lines of history.
func (t *Term) scroll(e gi.Scroll, u *gunim.UI) {
	notches := e.Notches.Y
	if notches == 0 {
		// A wheel that does not count notches: the distance, at the
		// 40 pixels a notch gunim's driver gives.
		notches = e.Delta.Y / 40
	}
	t.wheel += notches
	n := int(math.Trunc(float64(t.wheel)))
	if n == 0 {
		return
	}
	t.wheel -= float32(n)
	b := input.MouseWheelUp
	if n < 0 {
		b, n = input.MouseWheelDown, -n
	}
	at := t.cellAt(e.Pos)
	for range n {
		_, _ = t.sh.T.HandleMouse(input.MouseEvent{Kind: input.MousePress, Button: b, Col: at.X, Row: at.Y, Mods: MouseMods(e.Mods)})
	}
	t.quick = widget.Quick.Get(u.Theme())
	t.growSelection()
	t.Sync()
	u.Invalidate()
}

// Cursor implements [gunim.CursorShaper]: a hand over a link that a
// click would follow, and otherwise the I-beam, over text.
func (t *Term) Cursor(p geom.Point) gi.Cursor {
	at := t.cellAt(p)
	if _, on := t.sh.T.CursorAt(at.X, at.Y, t.hoverMods); on {
		return gi.CursorHand
	}
	return gi.CursorText
}
