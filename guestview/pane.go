package guestview

import (
	"strings"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/icon"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/guest"
	"github.com/marrasen/kakel/look"
	"github.com/marrasen/kakel/pastecheck"
	"github.com/marrasen/kakel/screen"
	"github.com/marrasen/kakel/termview"
	"github.com/marrasen/kakel/ui"
	"github.com/marrasen/kakel/winkeys"
)

// paneView is one pane of the window: a terminal, or the place of one
// that is not shared.
type paneView struct {
	anim.Group
	w  *windowPage
	id string
	// pages hold what it shows, which is face, and term its terminal,
	// where it shows one.
	pages *widget.Pages
	face  face
	term  *term
	// themed is what the terminal's own colours are worn by, and themes
	// the count of the theme it wears.
	themed *widget.Themed
	themes int
	// dim is how far it has dimmed, its program having ended, and rung how
	// lit the bell has it.
	dim, rung *anim.Float
	ended     *widget.Badge
	// ringing says the bell has lit it and has yet to let go, and warned
	// lights its edge as the guest types and may only watch.
	ringing bool
	warned  *anim.Float
	// early is what was typed before the pane could take it.
	early early
	// untyped is the state's count of what was for nobody, as last seen,
	// and deaf until when what is typed is for nobody too: it would be the
	// end of something whose beginning is lost.
	untyped int
	deaf    time.Time
}

// face is what a pane shows: a terminal, or in place of one says, which
// is empty for a terminal that is yet to come.
type face struct {
	sh   *screen.Shell
	says string
	icon *icon.Icon
}

// faceOf is what pane pn shows.
func (w *windowPage) faceOf(pn guest.Pane) face {
	switch held := w.st.Held[pn.ID]; {
	case pn.Kind != guest.KindTerminal:
		return face{says: notShared, icon: iconOf(pn)}
	case held == HeldLarge:
		return face{says: tooLarge, icon: iconOf(pn)}
	case held == HeldMany:
		return face{says: tooManyPanes, icon: iconOf(pn)}
	}
	return face{sh: w.p.shells.Get(pn.ID)}
}

// endedShade is how much a terminal whose program has ended is dimmed,
// paneTag how far its badge stands from the corner, bellLight how lit a
// pane is as its bell rings, and warnEdge how wide its edge is lit where
// nothing may be typed.
var (
	endedShade = theme.Number("kakel.page.ended.shade", 0.45)
	paneTag    = theme.Length("kakel.page.pane.tag", 8)
	bellLight  = theme.Number("kakel.page.bell.light", 0.14)
	warnEdge   = theme.Length("kakel.page.warn.edge", 3)
)

func newPaneView(w *windowPage, id string) *paneView {
	v := &paneView{w: w, id: id, dim: anim.NewFloat(0), rung: anim.NewFloat(0), warned: anim.NewFloat(0), ended: widget.NewBadge("Ended")}
	v.Add(v.dim, v.rung, v.warned)
	v.ended.Ink = look.Faint
	v.untyped = w.st.Untyped[id]
	pn, _ := w.p.paneOf(id)
	v.ended.SetHidden(!pn.Ended, nil)
	v.dim.Jump(anim.OnOff(pn.Ended))
	v.pages = widget.NewPages(v.showing(w.faceOf(pn)))
	return v
}

// showing is the node of what the pane shows.
func (v *paneView) showing(f face) gunim.Node {
	v.face, v.themes = f, v.w.themes
	if f.sh == nil {
		v.term, v.themed = nil, nil
		return newStandIn(v, f)
	}
	t := &term{Term: termview.New(f.sh, v.w.keymap), v: v}
	t.TextInput = v.w.st.CanType
	t.OnFocus = v.entered
	t.OnPaste = v.pasted
	t.OnClipboardUnreadable = v.unreadable
	if v.w.p.away {
		t.SetAway(true, nil)
	}
	t.SetFitted(true, v.w.p.u)
	v.term = t
	v.themed = widget.NewThemed(&termview.Pad{Term: t.menu()}, v.w.st.Theme.Content)
	return v.themed
}

// entered hears that the pane has the keyboard.
func (v *paneView) entered(u *gunim.UI) {
	v.w.dock.SetActive(v.id, u)
	u.Send(v.w.p, In{v.id})
	v.w.keys.update(u)
}

// unreadable hears that the clipboard could not be read.
func (v *paneView) unreadable(err error, u *gunim.UI) { v.w.p.failed(notPasted, err, u) }

// update shows the pane as the state has it.
func (v *paneView) update(u *gunim.UI) {
	pn, ok := v.w.p.paneOf(v.id)
	if !ok {
		return
	}
	if f := v.w.faceOf(pn); f != v.face {
		in := u.HasFocus(v)
		at := u
		if u.Presence(v) == gunim.Exiting {
			// Out of the tree, as a tab behind another: it is changed at
			// once, and what it held is let go of.
			at = nil
		}
		v.pages.Show(v.showing(f), geom.Point{}, at)
		if f.says != "" {
			v.cut(u)
		}
		if in {
			// The keyboard stays in the pane.
			u.FocusFirstLaidOut(v)
		}
	}
	if v.themed != nil && v.themes != v.w.st.Themes {
		v.themes = v.w.st.Themes
		v.themed.Use(v.w.st.Theme.Content)
	}
	if t := v.term; t != nil {
		t.Sync()
		// Through a lost connection, so a phone's keyboard stays up.
		t.TextInput = v.w.st.CanType
	}
	if n := v.w.st.Untyped[v.id]; n != v.untyped {
		v.untyped = n
		v.lose(u)
	}
	v.send(u)
	if v.ended.Hidden() == pn.Ended {
		v.ended.SetHidden(!pn.Ended, u)
		v.dim.Animate(anim.OnOff(pn.Ended), widget.Settle.Get(u.Theme()))
	}
}

// ring lights the pane a moment.
func (v *paneView) ring(u *gunim.UI) {
	if v.ringing {
		return
	}
	v.ringing = true
	v.rung.Animate(1, anim.Tween{Duration: 60 * time.Millisecond})
	u.After(60*time.Millisecond, func(u *gunim.UI) {
		v.ringing = false
		v.rung.Animate(0, widget.Settle.Get(u.Theme()))
		u.Invalidate()
	})
	u.Invalidate()
}

// warn lights the pane's edge a moment, as the guest typed and may only
// watch.
func (v *paneView) warn(u *gunim.UI) {
	if v.warned.Target() == 1 {
		return
	}
	v.warned.Animate(1, widget.Quick.Get(u.Theme()))
	u.After(500*time.Millisecond, func(u *gunim.UI) {
		v.warned.Animate(0, widget.Settle.Get(u.Theme()))
		u.Invalidate()
	})
	u.Invalidate()
}

// Children implements [gunim.Composite].
func (v *paneView) Children() []gunim.Node { return []gunim.Node{v.pages, v.ended} }

// Layout implements [gunim.Node]. While a split slides, the terminal's
// text is sized along with the pane, as its screen is always all there.
func (v *paneView) Layout(c gunim.Constraints, f gunim.Frame, kids gunim.Children) geom.Size {
	size := c.Max
	k := kids.At(0)
	k.Layout(gunim.Tight(size))
	k.Place(geom.Point{})
	tag := paneTag.Get(f.Theme)
	s := kids.At(1).Layout(gunim.Constraints{Max: size})
	kids.At(1).Place(geom.Pt(c.Max.W-s.W-tag, tag))
	return c.Max
}

// Paint implements [gunim.Node]. The ground is the terminal's own to draw,
// so that a pane on its way out fades off what comes in its place.
func (v *paneView) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	r := geom.Rect{Max: box.Point()}
	th := f.Theme
	kids.At(0).Paint(p)
	if dim := min(max(v.dim.Value(), 0), 1); dim > 0.001 {
		c := look.TermBackground.Get(th)
		c.A = uint8(255 * dim * min(max(endedShade.Get(th), 0), 1))
		p.RRect(r, 0, paint.Solid(c))
	}
	if lit := min(max(v.rung.Value(), 0), 1); lit > 0.001 {
		c := widget.Accent.Get(th)
		c.A = uint8(255 * lit * min(max(bellLight.Get(th), 0), 1))
		p.RRect(r, 0, paint.Solid(c))
	}
	if lit := min(max(v.warned.Value(), 0), 1); lit > 0.001 {
		c := look.Guest.Get(th)
		c.A = uint8(float32(c.A) * lit)
		w := warnEdge.Get(th)
		p.RRectStroke(r.Inset(geom.Uniform(w/2)), 0, paint.Fill{}, paint.Stroke{Width: w, Color: c})
	}
	kids.At(1).Paint(p)
}

// newStandIn stands in for a terminal that is not shown: what kind of
// pane it is, and why, or nothing for one that is yet to come.
func newStandIn(v *paneView, f face) gunim.Node {
	if f.says == "" {
		return &blank{v: v}
	}
	ic := widget.NewIcon(f.icon, "")
	ic.Size, ic.Color = notSharedIcon, look.Faint
	says := widget.NewLabel(f.says)
	says.Color = look.Faint
	col := widget.Column(ic, says)
	col.Justify, col.Cross = widget.JustifyCenter, widget.CrossCenter
	return col
}

// blank takes the room of a terminal that is yet to come, and what is
// typed in it.
type blank struct{ v *paneView }

// Layout implements [gunim.Node].
func (*blank) Layout(c gunim.Constraints, _ gunim.Frame, _ gunim.Children) geom.Size { return c.Max }

// Paint implements [gunim.Node].
func (*blank) Paint(*paint.Painter, gunim.Frame, geom.Size, gunim.Children) {}

// Focusable implements [gunim.Focusable].
func (*blank) Focusable() bool { return true }

// TakesText implements [gunim.TextTaker], as the terminal does.
func (b *blank) TakesText() bool { return b.v.w.st.CanType }

// RawText implements [gunim.RawTexter], as the terminal does.
func (*blank) RawText() bool { return true }

// ClaimsKeys implements [gunim.KeyClaimer], as the terminal does.
func (*blank) ClaimsKeys() bool { return true }

// Handle implements [gunim.Handler].
func (b *blank) Handle(e gi.Event, u *gunim.UI) bool {
	switch e := e.(type) {
	case gi.FocusGained:
		b.v.entered(u)
	case gi.PointerDown:
		b.v.w.picked(b.v.id, u)
	case gi.KeyPress, gi.TextInput:
		return b.v.keyed(e, u)
	}
	return false
}

// notSharedIcon is how large the icon of a pane that is not shared is.
var notSharedIcon = theme.Length("kakel.page.notshared.icon", 36)

// term is a pane's terminal: the terminal's own view, which takes what
// is typed only while the guest may type.
type term struct {
	*termview.Term
	v *paneView
	// ctrl and alt say the next key typed is with them, as the row of keys
	// has it.
	ctrl, alt bool
	// eaten is what an input method is composing and has been typed
	// already.
	eaten string
}

// menu is the terminal in the menu its right click opens.
func (t *term) menu() gunim.Node {
	m := widget.NewContextMenu(t, nil)
	t.OnHeld = m.Open
	m.Prepare = func(_ geom.Point, u *gunim.UI) bool {
		t.v.w.picked(t.v.id, u)
		copies, pastes := t.v.w.p.hints()
		m.SetItems([]widget.MenuItem{
			{Label: "Copy", Hint: copies, Icon: icon.Copy, Disabled: t.Shell().T.SelectionText() == ""},
			{Label: "Paste", Hint: pastes, Icon: icon.ClipboardPaste, Disabled: !t.v.w.may()},
			{Label: "Select All", Icon: icon.TextSelect, Break: true},
		})
		return true
	}
	m.OnPick = func(i int, u *gunim.UI) gunim.Intent {
		switch i {
		case 0:
			t.CopySelection(u)
		case 1:
			t.PasteClipboard(u)
		case 2:
			t.Shell().T.SelectAll()
			t.Sync()
		}
		u.Focus(t)
		return nil
	}
	return m
}

// hints are the keys that copy and paste, as a menu has them: none where a
// finger is at the page, and has no keys.
func (p *page) hints() (copies, pastes string) {
	switch {
	case p.touch:
		return "", ""
	case p.cfg.Mac:
		return "Cmd+C", "Cmd+V"
	}
	return "Ctrl+Shift+C", "Ctrl+Shift+V"
}

// Handle implements [gunim.Handler]: what is typed, while the guest may
// type, and the rest as the terminal does.
func (t *term) Handle(e gi.Event, u *gunim.UI) bool {
	switch e := e.(type) {
	case gi.PointerDown:
		if e.Touch {
			// The terminal first, which asks whether it had the keyboard
			// already. The dock too is in the pane as a finger comes down.
			defer t.v.w.picked(t.v.id, u)
		} else {
			t.v.w.picked(t.v.id, u)
		}
		if t.v.shut() && t.Shell().T.MouseTaken(termview.MouseMods(e.Mods)) {
			return true
		}
	case gi.Scroll:
		if t.v.shut() && t.Shell().T.TakesWheel(termview.MouseMods(e.Mods)) {
			return true
		}
	case gi.Composing:
		e, done := t.eat(e, u)
		if done {
			return true
		}
		return t.Term.Handle(e, u)
	case gi.KeyPress, gi.TextInput:
		return t.v.keyed(e, u)
	}
	return t.Term.Handle(e, u)
}

// keyed takes a key pressed in the pane, or text typed there: as the
// terminal does where the pane is open, and once it is where it is not.
func (v *paneView) keyed(e gi.Event, u *gunim.UI) bool {
	w, t := v.w, v.term
	switch k := e.(type) {
	case gi.KeyPress:
		ev, ok := winkeys.Event(k)
		if ok && !k.Typed {
			if id, bound := w.keymap.Lookup(ui.ChordOf(ev)); bound {
				switch {
				case t != nil:
					return t.Term.Handle(e, u)
				case id != "edit.paste":
					return w.shortcut(e, u)
				}
				s, err := u.ReadClipboard()
				if err != nil {
					v.unreadable(err, u)
				} else if s != "" {
					v.pasted(s, u)
				}
				return true
			}
		}
		types := ok && !k.Typed
		switch {
		case w.st.Lost && k.Key == gi.KeyTab:
			// On to what the page offers meanwhile.
			return false
		case !types:
			return t == nil || !w.may() || t.Term.Handle(e, u)
		}
	case gi.TextInput:
		if t != nil {
			if k.Text = t.uneaten(k.Text); k.Text == "" {
				return true
			}
			e = t.withHeld(k, u)
		}
	}
	v.types(typed{ev: e}, u)
	return true
}

// letter is the key of a letter typed alone.
func letter(s string) (gi.Key, bool) {
	if len(s) != 1 {
		return 0, false
	}
	c := s[0] | 0x20
	if c < 'a' || c > 'z' {
		return 0, false
	}
	return gi.KeyA + gi.Key(c-'a'), true
}

// heldMods are the modifiers of the row of keys that go with the next key,
// which they are let go with.
func (t *term) heldMods(u *gunim.UI) (m gi.Mods) {
	if t.ctrl {
		m |= gi.ModControl
	}
	if t.alt {
		m |= gi.ModAlt
	}
	if m != 0 {
		t.ctrl, t.alt = false, false
		t.v.w.keys.update(u)
	}
	return m
}

// withHeld is text typed as the row of keys has it, with its modifiers: a
// key for a letter.
func (t *term) withHeld(e gi.TextInput, u *gunim.UI) gi.Event {
	if !t.ctrl && !t.alt {
		return e
	}
	mods := t.heldMods(u)
	if k, ok := letter(e.Text); ok {
		return gi.KeyPress{Key: k, Mods: mods, Time: e.Time}
	}
	if mods.Has(gi.ModAlt) {
		// As a terminal sends Alt: Escape, and the text.
		t.v.types(typed{ev: gi.KeyPress{Key: gi.KeyEscape, Time: e.Time}}, u)
	}
	return e
}

// eat types the letter an input method has begun to compose, where the row
// of keys has Ctrl or Alt for it: that is no beginning of a word. It
// returns what is left to show, and whether that is nothing.
func (t *term) eat(e gi.Composing, u *gunim.UI) (gi.Composing, bool) {
	if k, ok := letter(e.Text); ok && (t.ctrl || t.alt) {
		t.eaten = e.Text
		t.v.types(typed{ev: gi.KeyPress{Key: k, Mods: t.heldMods(u), Time: u.Now()}}, u)
		return e, true
	}
	if t.eaten == "" || e.Text == "" {
		return e, false
	}
	rest, ok := strings.CutPrefix(strings.ToLower(e.Text), strings.ToLower(t.eaten))
	if !ok {
		t.eaten = ""
		return e, false
	}
	n := len(e.Text) - len(rest)
	e.Text = e.Text[n:]
	e.Selected = [2]int{max(e.Selected[0]-n, 0), max(e.Selected[1]-n, 0)}
	return e, false
}

// uneaten is the text an input method composed, without what was typed as
// it began.
func (t *term) uneaten(s string) string {
	if n := len(t.eaten); n > 0 && len(s) >= n && strings.EqualFold(s[:n], t.eaten) {
		s = s[n:]
	}
	t.eaten = ""
	return s
}

// pasted asks about a paste of more than one line, and reports whether it
// took it.
func (v *paneView) pasted(s string, u *gunim.UI) bool {
	w := v.w
	if !w.may() {
		v.refused(u)
		return true
	}
	w.follow = false
	if !pastecheck.Needed(s) {
		v.types(typed{paste: s}, u)
		return true
	}
	bracketed := v.term != nil && v.term.Shell().T.Bracketed()
	w.p.openDialog(pastecheck.Ask{
		Text: s, Bracketed: bracketed,
		OnPaste: func(s string) {
			switch {
			case w.may() && w.panes[v.id] == v:
				v.types(typed{paste: s}, u)
			case w.st.Lost:
				w.p.said(notPasted, lostWhy, u)
			case w.panes[v.id] != v:
				w.p.said(notPasted, goneWhy, u)
			default:
				w.p.said(notPasted, watchWhy, u)
			}
		},
		OnClose: w.p.dialogClosed,
	}.Dialog(u), u)
	return true
}
