package view

import (
	"strings"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/look"
	"github.com/marrasen/kakel/termview"
	"github.com/marrasen/kakel/words"
)

// term is a pane's terminal: the terminal's own view, with what the
// window adds to it. Files dropped on it go to the program, and rings
// round it say who else can see it.
type term struct {
	*termview.Term
	id string
	// dropLit is how lit the pane is for files from another program
	// dragged over it, which a drop types or copies there.
	dropLit *anim.Float
	// agent says the pane is shared with an agent, and marks are the
	// colours of the rings that say so.
	agent bool
	marks look.Marks
}

// newTerm is pane id's terminal in w.
func (w *Window) newTerm(id string) *term {
	t := &term{Term: termview.New(w.shells.Get(id), w.keys), id: id, dropLit: anim.NewFloat(0)}
	t.Add(t.dropLit)
	t.OnFocus = func(u *gunim.UI) { u.Send(t, app.FocusPane{Pane: id}) }
	t.OnPaste = func(s string, u *gunim.UI) bool { return w.checkPaste(t, s, u) }
	t.OnPasteImage = func(u *gunim.UI) { u.Send(t, app.PasteImage{Pane: id}) }
	t.OnNoText = func(u *gunim.UI) { u.Send(t, app.NoTextToPaste{}) }
	t.OnClipboardUnreadable = func(err error, u *gunim.UI) { u.Send(t, app.ClipboardUnreadable{Why: clipboardWhy(err)}) }
	t.OnCtrl = w.ctrlHeld
	t.AlongWith = func() []*termview.Term { return w.typingAlong(t) }
	return t
}

// Handle implements [gunim.Handler]: files dropped on the pane, and the
// rest the terminal's own.
func (t *term) Handle(e gi.Event, u *gunim.UI) bool {
	switch e := e.(type) {
	case gi.DragOver:
		// Files from another program, as they are dragged over.
		if _, ok := e.Data.(gi.Files); !ok {
			return false
		}
		t.dropLit.Animate(1, widget.Quick.Get(u.Theme()))
		u.Invalidate()
		return true
	case gi.DragLeave:
		t.dropLit.Animate(0, widget.Quick.Get(u.Theme()))
		u.Invalidate()
		return false
	case gi.Drop:
		t.dropLit.Animate(0, widget.Settle.Get(u.Theme()))
		if len(e.Paths) == 0 {
			return false
		}
		u.Send(t, app.DropFiles{Pane: t.id, Paths: e.Paths})
		return true
	}
	return t.Term.Handle(e, u)
}

// Paint implements [gunim.Node]: the terminal, and over it the rings.
func (t *term) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	t.Term.Paint(p, f, box, kids)
	t.paintRings(p, f, box)
	t.paintDropLit(p, f, box)
}

// clipboardWhy is why the clipboard could not be read, as a sentence
// for the user: the library's name taken off the front.
func clipboardWhy(err error) string {
	return words.UpperFirst(strings.TrimPrefix(err.Error(), "glfw: ")) + "."
}
