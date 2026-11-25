//go:build js && wasm

package main

import (
	"log"
	"sync"
	"syscall/js"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"

	kinput "github.com/marrasen/kakel/input"
	"github.com/marrasen/kakel/screen"
	"github.com/marrasen/kakel/termview"
	"github.com/marrasen/kakel/ui"
)

// pageTopic is what the page's view is told things on.
const pageTopic = "page"

// pageState is what the page shows.
type pageState struct {
	// Cols and Rows are the size the far end gave the screen, or zero
	// for a screen of as many cells as fit the page.
	Cols, Rows int
}

// outputArrived tells the page that its program wrote.
type outputArrived struct{}

// page is all the window shows: one terminal, filling it.
type page struct {
	term   *termview.Term
	body   *widget.Themed
	output termview.Output
	shown  bool

	// mu guards at and cell, where the terminal's cells are on the page,
	// which the tests ask for.
	mu   sync.Mutex
	at   geom.Point
	cell geom.Size
}

func newPage(sh *screen.Shell, content theme.Theme) *page {
	p := &page{term: termview.New(sh, shortcuts())}
	p.term.TextInput = true
	p.term.OnClipboardUnreadable = func(err error, _ *gunim.UI) { log.Printf("the clipboard could not be read: %v", err) }
	p.body = widget.NewThemed(&termview.Pad{Term: p.term}, content)
	return p
}

// shortcuts are the keys of what the terminal does itself.
func shortcuts() *ui.Keymap {
	keys := ui.NewKeymap()
	keys.MustBind(map[ui.Chord]string{
		{Key: kinput.KeyV, Mods: kinput.ModCtrl | kinput.ModShift}: "edit.paste",
		{Key: kinput.KeyC, Mods: kinput.ModCtrl | kinput.ModShift}: "edit.copy",
		{Key: kinput.KeyInsert, Mods: kinput.ModCtrl}:              "edit.copy",
		{Key: kinput.KeyInsert, Mods: kinput.ModShift}:             "edit.paste",
		{Key: kinput.KeyPageUp, Mods: kinput.ModShift}:             "view.scrollUp",
		{Key: kinput.KeyPageDown, Mods: kinput.ModShift}:           "view.scrollDown",
	})
	return keys
}

// Update shows st.
func (p *page) Update(st pageState, u *gunim.UI) {
	if u.Focused() == nil {
		u.Focus(p.term)
	}
	fitted := st.Cols > 0 && st.Rows > 0
	p.term.SetFitted(fitted, u)
	if fitted {
		p.term.Shell().T.Hold(st.Cols, st.Rows)
	} else {
		p.term.Shell().T.Release()
	}
	p.term.Sync()
	u.Invalidate()
}

// Children implements [gunim.Composite].
func (p *page) Children() []gunim.Node { return []gunim.Node{p.body} }

// Handle implements [gunim.Handler]: the page losing the keyboard, and
// getting it back.
func (p *page) Handle(e input.Event, u *gunim.UI) bool {
	switch e.(type) {
	case input.WindowFocusGained:
		p.term.SetAway(false, u)
	case input.WindowFocusLost:
		p.term.SetAway(true, u)
	}
	return false
}

// Layout implements [gunim.Node].
func (p *page) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	p.output.Copy(func(yield func(*termview.Term) bool) { yield(p.term) })
	k := kids.At(0)
	k.Layout(gunim.Tight(c.Max))
	k.Place(geom.Point{})
	return c.Max
}

// Paint implements [gunim.Node].
func (p *page) Paint(pt *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	kids.At(0).Paint(pt)
	pad := termview.Padding.Get(f.Theme)
	at, cell := p.term.Drawn()
	p.mu.Lock()
	p.at, p.cell = at.Add(geom.Pt(pad, pad)), cell
	p.mu.Unlock()
	if !p.shown {
		// The page fades in once there is something to see on it.
		p.shown = true
		js.Global().Get("document").Get("body").Get("classList").Call("add", "ready")
		js.Global().Get("performance").Call("mark", "kakel-first-frame")
	}
}

// cells is where the top left cell is on the page, and a cell's size.
func (p *page) cells() (geom.Point, geom.Size) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.at, p.cell
}
