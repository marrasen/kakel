package guestview

import (
	"log"
	"sync"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/guest"
	"github.com/marrasen/kakel/internal/build"
	"github.com/marrasen/kakel/screen"
	"github.com/marrasen/kakel/termview"
)

// toastRoom is the room between the notices and the page's corner.
var toastRoom = theme.Length("kakel.page.toast.room", 12)

// page is all the window shows: one of the pages, with the dialogs and
// the notices over it.
type page struct {
	cfg    Config
	shells *screen.Shells
	u      *gunim.UI
	st     State

	pages *widget.Pages
	// on names the page shown, and card is it, but for the window.
	on   string
	card *card
	win  *windowPage

	toasts *widget.Toasts
	dialog *widget.Dialog
	// leaving is a dialog on its way out, which the next page waits for.
	leaving *widget.Dialog
	output  termview.Output
	title   string
	drawn   sync.Once
	// asks says the browser asks before the page closes.
	asks bool
	// away says the page is without the keyboard.
	away bool
	// touch says a finger was last at the page, and wears the theme it is
	// drawn in, for one or not.
	touch bool
	// next is the latest state, which waits for leaving where waits says
	// so.
	next  State
	waits bool
	wears struct {
		themes int
		touch  bool
	}
}

func newPage(shells *screen.Shells, cfg Config, st State) *page {
	p := &page{cfg: cfg, shells: shells, st: st, toasts: widget.NewToasts(), touch: cfg.Touch}
	p.wears.themes, p.wears.touch = st.Themes, cfg.Touch
	p.pages = widget.NewPages(p.pageOf(st))
	// One after the other: they are laid out alike.
	p.pages.InTurn = true
	p.on = nameOf(st)
	return p
}

// nameOf tells one page from another.
func nameOf(st State) string {
	switch st.Page {
	case PageJoin:
		return "join"
	case PageWaiting:
		if st.Lost {
			return "back"
		}
		return "waiting"
	case PageWindow:
		return "window"
	case PageOver:
	}
	return "over " + st.Over.Title + st.Over.Text
}

// pageOf makes the page st shows. The window is made once, and kept.
func (p *page) pageOf(st State) gunim.Node {
	p.card = nil
	switch st.Page {
	case PageJoin:
		p.card = joinCard(p, st)
	case PageWaiting:
		p.card = waitingCard(st)
	case PageWindow:
		if p.win == nil {
			p.win = newWindowPage(p)
		}
		return p.win
	case PageOver:
		p.card = overCard(st.Over)
	}
	return p.card
}

// Update shows st.
func (p *page) Update(st State, u *gunim.UI) {
	p.u = u
	was := p.st
	p.st, p.next = st, st
	p.wear(u)
	p.sayTitle(u)
	if on := nameOf(st); on != p.on {
		if p.closeDialog(u); p.leaving != nil && u.Drawn(p.leaving) {
			// The next page comes once the dialog has gone: both would lie
			// one over the other.
			p.st = was
			p.awaitDialog(u)
			return
		}
		p.on = on
		next := p.pageOf(st)
		from := geom.Pt(0, 1)
		if st.Page < was.Page {
			from = geom.Pt(0, -1)
		}
		if st.Page == PageWindow {
			// As it is, before it comes.
			p.win.update(st, u)
		} else if u.FullScreen() {
			// With it goes what full screen takes of the keyboard.
			u.SetFullScreen(false)
		}
		p.pages.Show(next, from, u)
		// What goes on is said to whoever cannot see it.
		if p.card != nil {
			u.Announce(p.card.says())
		}
	}
	if asks := st.Page == PageWindow; asks != p.asks {
		// Closed by mistake, as with the key that closes a tab, the page
		// would have to come back by itself.
		p.asks = asks
		u.SetCloseAsks(asks)
	}
	switch {
	case st.Page == PageWindow:
		p.win.update(st, u)
	case p.card != nil:
		p.card.update(st, u)
	}
	p.keyboard(u)
	u.Invalidate()
	if p.cfg.OnShown != nil {
		p.cfg.OnShown(st)
	}
}

// wear has the page drawn in the theme of its state, with room for a
// finger where one is at it.
func (p *page) wear(u *gunim.UI) {
	now := p.wears
	now.themes, now.touch = p.st.Themes, p.touch
	if now != p.wears {
		p.wears = now
		u.UseTheme(Own(p.st.Own, p.touch))
	}
}

// awaitDialog shows the latest state once the dialog on its way out has
// gone.
func (p *page) awaitDialog(u *gunim.UI) {
	if p.waits {
		return
	}
	p.waits = true
	var look func(u *gunim.UI)
	look = func(u *gunim.UI) {
		if p.leaving != nil && u.Drawn(p.leaving) {
			u.After(0, look)
			u.Invalidate()
			return
		}
		p.waits, p.leaving = false, nil
		p.Update(p.next, u)
	}
	u.After(0, look)
}

// sayTitle names the browser's tab after the pane the guest is in.
func (p *page) sayTitle(u *gunim.UI) {
	title := build.Name
	if p.st.Page == PageWindow && p.win != nil && p.win.title != "" {
		title = p.win.title + " — " + build.Name
	}
	if title != p.title {
		p.title = title
		u.SetTitle(title)
	}
}

// keyboard puts the keyboard where it belongs, if nothing has it.
func (p *page) keyboard(u *gunim.UI) {
	if u.Focused() != nil || p.dialog != nil {
		return
	}
	switch {
	case p.st.Page == PageWindow:
		p.win.keyboard(u)
	case p.card != nil && p.card.first != nil:
		u.FocusFirstLaidOut(p.card.first)
	}
}

// ring hears a pane's bell.
func (p *page) ring(id string, u *gunim.UI) {
	if p.win != nil {
		p.win.ring(id, u)
	}
}

// openDialog shows d over the page, until it closes.
func (p *page) openDialog(d *widget.Dialog, u *gunim.UI) {
	p.closeDialog(u)
	p.dialog = d
	u.Insert(p, d)
	p.asking(u)
}

// closeDialog closes the dialog there is, as when what it is about ends.
func (p *page) closeDialog(u *gunim.UI) {
	if p.dialog != nil {
		p.dialog.Close(u)
		p.leaving, p.dialog = p.dialog, nil
		p.asking(u)
	}
}

// asking has the window make way for a dialog, and come back after it.
func (p *page) asking(u *gunim.UI) {
	if p.win != nil {
		p.win.veil.asking(p.dialog != nil, u)
	}
}

// dialogClosed hears that the dialog closed by itself.
func (p *page) dialogClosed(u *gunim.UI) gunim.Intent {
	p.leaving, p.dialog = p.dialog, nil
	p.asking(u)
	// The keyboard goes back once the dialog has let go of it.
	u.After(0, p.keyboard)
	return nil
}

// askLeave asks before the guest leaves, as the link lets nobody back.
func (p *page) askLeave(u *gunim.UI) {
	if p.dialog != nil {
		return
	}
	d := widget.NewDialog("Leave Window")
	d.Careful = true
	d.Body = widget.NewLabel("To come back, you need a new link.")
	d.SetButtons("Leave", "Cancel")
	d.OnAccept = func(u *gunim.UI) gunim.Intent {
		p.dialogClosed(u)
		return Leave{}
	}
	d.OnDismiss = p.dialogClosed
	p.openDialog(d, u)
}

// failed says what did not work, and logs it. What the browser says of it
// is for the log.
func (p *page) failed(title string, err error, u *gunim.UI) {
	log.Printf("%s: %v", title, err)
	p.not(title, browserWhy, u)
}

// said says what was not done, and why, and logs it.
func (p *page) said(title, why string, u *gunim.UI) {
	log.Printf("%s: %s", title, why)
	p.not(title, why, u)
}

// not says what was not done, and why.
func (p *page) not(title, why string, u *gunim.UI) {
	p.toasts.Show(widget.Toast{Title: title, Body: why, Kind: widget.ToastError, Key: title}, u)
}

// Children implements [gunim.Composite].
func (p *page) Children() []gunim.Node { return []gunim.Node{p.pages, p.toasts} }

// Overhear implements [gunim.Overhearer]: whether it is a finger that is
// at the page.
func (p *page) Overhear(e input.Event, u *gunim.UI) {
	down, ok := e.(input.PointerDown)
	if !ok || down.Touch == p.touch {
		return
	}
	p.touch = down.Touch
	p.wear(u)
	if p.win != nil {
		p.win.touched(u)
	}
}

// Handle implements [gunim.Handler]: the page losing the keyboard, and
// getting it back, and the keys nothing else took.
func (p *page) Handle(e input.Event, u *gunim.UI) bool {
	switch e.(type) {
	case input.WindowFocusGained, input.WindowFocusLost:
		_, p.away = e.(input.WindowFocusLost)
		if p.win != nil {
			p.win.setAway(p.away, u)
		}
		return false
	}
	if p.st.Page == PageWindow && p.win != nil {
		return p.win.shortcut(e, u)
	}
	return false
}

// CatchKey implements [gunim.KeyCatcher]: a key pressed with the keyboard
// nowhere, as after a click on the bar, is for the pane the guest is in.
func (p *page) CatchKey(e input.Event, u *gunim.UI) bool {
	if u.Focused() != nil || p.dialog != nil || p.st.Page != PageWindow || p.win == nil {
		return false
	}
	switch e.(type) {
	case input.KeyPress, input.TextInput:
	default:
		return false
	}
	v, ok := p.win.panes[p.win.dock.Active()]
	if !ok || v.face.says != "" {
		return false
	}
	p.win.keyboard(u)
	return v.keyed(e, u)
}

// Layout implements [gunim.Node]: clear of what the system draws over the
// page, and of a phone's keyboard.
func (p *page) Layout(c gunim.Constraints, f gunim.Frame, kids gunim.Children) geom.Size {
	if p.win != nil {
		p.output.Copy(p.win.terms)
	}
	in := f.Safe
	in.Bottom = max(in.Bottom, f.Keyboard)
	box := geom.Rect{Max: c.Max.Point()}.Inset(in)
	for k := range kids.All {
		switch k.Node() {
		case gunim.Node(p.toasts):
			// In a little from the corner.
			in := toastRoom.Get(f.Theme)
			s := k.Layout(gunim.Constraints{Max: geom.Sz(max(box.Size().W-2*in, 0), max(box.Size().H-2*in, 0))})
			k.Place(geom.Pt(box.Max.X-s.W-in, box.Max.Y-s.H-in))
		default:
			k.Layout(gunim.Tight(box.Size()))
			k.Place(box.Min)
		}
	}
	return c.Max
}

// Paint implements [gunim.Node].
func (p *page) Paint(pt *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	pt.RRect(geom.Rect{Max: box.Point()}, 0, paint.Solid(widget.Background.Get(f.Theme)))
	for k := range kids.All {
		if k.Node() != gunim.Node(p.toasts) {
			k.Paint(pt)
		}
	}
	for k := range kids.All {
		if k.Node() == gunim.Node(p.toasts) {
			k.Paint(pt)
		}
	}
	if p.cfg.Drawn != nil {
		p.drawn.Do(p.cfg.Drawn)
	}
}

// paneOf is the pane with id, and whether the window has one.
func (p *page) paneOf(id string) (guest.Pane, bool) {
	for _, pn := range p.st.Panes {
		if pn.ID == id {
			return pn, true
		}
	}
	return guest.Pane{}, false
}
