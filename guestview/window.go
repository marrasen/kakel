package guestview

import (
	"slices"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/icon"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"
	"github.com/marrasen/gunim/widget/dock"

	"github.com/marrasen/kakel/guest"
	"github.com/marrasen/kakel/guestlook"
	"github.com/marrasen/kakel/input"
	"github.com/marrasen/kakel/look"
	"github.com/marrasen/kakel/termview"
	"github.com/marrasen/kakel/ui"
	"github.com/marrasen/kakel/winkeys"
)

// narrowAt is the width under which the window's panes stand one at a
// time, as the tabs of one group: a phone has room for no more. lowAt is
// the height under which they do for a finger: a phone on its side.
var (
	narrowAt = theme.Length("kakel.page.narrow", 600)
	lowAt    = theme.Length("kakel.page.low", 500)
)

// windowPage is the shared window: a bar of what it is and how it
// stands, the host's panes as they stand there, and, over them while the
// connection is lost, what is being done about that.
//
// The panes follow the host's, until the guest picks one themselves.
// From then each side has its own tabs in front, and the panes come, go
// and move as the host's do, until the guest follows the host again.
type windowPage struct {
	p    *page
	bar  *bar
	dock *dock.Dock
	// tabs is the dock, in the host's own colours, and picture draws it
	// as the host's window, whole.
	tabs    *widget.Themed
	picture *picture
	keys    *keyRow
	veil    *veil
	// keymap is what the terminals and the page do themselves.
	keymap *ui.Keymap

	st    State
	panes map[string]*paneView
	// host is the layout as the host has it, and themes the count of the
	// theme the panes wear.
	host   dock.Layout
	themes int
	// follow says the guest has picked nothing, and hostIn is the pane
	// the host was in last.
	follow bool
	hostIn string
	laid   bool
	// narrow says the panes stand one at a time.
	narrow bool
	shown  []string
	// bereft says the pane the guest was in has gone, and no other has been
	// come to since.
	bereft bool
	// wants is a pane to take the keyboard once it is laid out.
	wants string
	// room is the page's, as it was laid out last.
	room geom.Size
	// title is that of the pane the guest is in.
	title string
}

func newWindowPage(p *page) *windowPage {
	w := &windowPage{p: p, keymap: shortcuts(), panes: map[string]*paneView{}, follow: true}
	w.st, w.themes = p.st, p.st.Themes
	w.bar, w.veil, w.keys = newBar(w), newVeil(w), newKeyRow(w)
	w.dock = dock.NewDock(guestlook.LayoutWindow)
	w.dock.Locked = true
	w.dock.Content = func(id string) gunim.Node { return w.pane(id) }
	w.dock.Face = func(n *dock.Node) dock.Face {
		pn, _ := p.paneOf(n.ID)
		f := dock.Face{Title: pn.Title, Icon: iconOf(pn)}
		if n.ID == w.hostIn && w.st.HostName != "" {
			f.Badge, f.BadgeInk = w.st.HostName, &hostInk
		}
		return f
	}
	w.dock.OnChange = func(c dock.Change, u *gunim.UI) gunim.Intent {
		if s, ok := c.(dock.Show); ok {
			w.picked(s.ID, u)
		}
		w.retitle(u)
		return nil
	}
	w.tabs = widget.NewThemed(w.dock, w.tabsLook())
	w.picture = newPicture(w)
	return w
}

// tabsLook is what the dock and its tabs are drawn in: with room for a
// finger where the panes stand one at a time, and otherwise as the host
// has them, so that its panes are laid out as large.
func (w *windowPage) tabsLook() theme.Theme {
	if w.p.touch && w.narrow {
		return forFingers(w.st.Theme.Theme)
	}
	return w.st.Theme.Theme
}

// cell is how large the terminals' cells are to be at most: the host's,
// where the panes are the host's window, whole, so all have its one size
// of text, and none where they stand one at a time, each as large as the
// page has room for.
func (w *windowPage) cell() geom.Size {
	if _, said := w.picture.hostRoom(); !said || w.narrow {
		return geom.Size{}
	}
	return w.st.Cell
}

// sized has the picture and the terminals' text go to the sizes the host
// and the page say.
func (w *windowPage) sized(u *gunim.UI) {
	w.picture.set(u)
	cell := w.cell()
	for t := range w.terms {
		t.SetCell(cell, u)
	}
}

// shortcuts are kakel's own, of what there is to do here, and those a Mac
// copies and pastes with.
func shortcuts() *ui.Keymap {
	keys := ui.NewKeymap()
	keys.MustBind(map[ui.Chord]string{
		{Key: input.KeyV, Mods: input.ModCtrl | input.ModShift}:   "edit.paste",
		{Key: input.KeyV, Mods: input.ModSuper}:                   "edit.paste",
		{Key: input.KeyInsert, Mods: input.ModShift}:              "edit.paste",
		{Key: input.KeyC, Mods: input.ModCtrl | input.ModShift}:   "edit.copy",
		{Key: input.KeyC, Mods: input.ModSuper}:                   "edit.copy",
		{Key: input.KeyInsert, Mods: input.ModCtrl}:               "edit.copy",
		{Key: input.KeyPageUp, Mods: input.ModShift}:              "view.scrollUp",
		{Key: input.KeyPageDown, Mods: input.ModShift}:            "view.scrollDown",
		{Key: input.KeyTab, Mods: input.ModCtrl}:                  "pane.next",
		{Key: input.KeyTab, Mods: input.ModCtrl | input.ModShift}: "pane.previous",
		{Key: input.KeyPageDown, Mods: input.ModCtrl}:             "tab.next",
		{Key: input.KeyPageUp, Mods: input.ModCtrl}:               "tab.previous",
	})
	return keys
}

// shortcut does what a key the terminal left is the shortcut for.
func (w *windowPage) shortcut(e gi.Event, u *gunim.UI) bool {
	k, ok := e.(gi.KeyPress)
	if !ok {
		return false
	}
	ev, ok := winkeys.Event(k)
	if !ok {
		return false
	}
	id, _ := w.keymap.Lookup(ui.ChordOf(ev))
	switch id {
	case "pane.next", "pane.previous":
		w.dock.StepPane(id == "pane.next", u)
		w.picked(w.dock.Active(), u)
	case "tab.next", "tab.previous":
		w.dock.StepTab(id == "tab.next", u)
	default:
		return false
	}
	return true
}

// terms are the window's terminals.
func (w *windowPage) terms(yield func(*termview.Term) bool) {
	for _, v := range w.panes {
		if v.term != nil && !yield(v.term.Term) {
			return
		}
	}
}

// pane is the view of pane id, made on first use.
func (w *windowPage) pane(id string) *paneView {
	v, ok := w.panes[id]
	if !ok {
		v = newPaneView(w, id)
		w.panes[id] = v
	}
	return v
}

// may reports whether what is typed reaches the host.
func (w *windowPage) may() bool { return w.st.CanType && !w.st.Lost }

// update shows st.
func (w *windowPage) update(st State, u *gunim.UI) {
	was, in := w.st, w.dock.Active()
	w.st = st
	if st.Themes != w.themes {
		w.tabs.Use(w.tabsLook())
	}
	w.sized(u)
	for id, v := range w.panes {
		if st.Conn != was.Conn && !was.Lost {
			// Another connection already, too soon to be seen: what was
			// typed meanwhile is for nobody.
			v.deaf = u.Now().Add(deafFor)
		}
		_, here := w.p.paneOf(id)
		if !w.may() || st.Conn != was.Conn || !here {
			v.cut(u)
		}
		v.update(u)
		if !here {
			delete(w.panes, id)
		}
	}
	w.themes = st.Themes
	w.host = st.Layout
	if st.HostIn != w.hostIn || st.HostName != was.HostName {
		w.hostIn = st.HostIn
		if w.follow && st.HostIn != "" {
			w.dock.SetActive(st.HostIn, u)
		}
		// The host's name moves to its tab.
		w.dock.Refresh(u)
	}
	w.arrange(u)
	if w.follow && st.HostIn != "" && w.dock.Active() == st.HostIn {
		w.take(st.HostIn, u)
	}
	w.mourn(in, u)
	w.bar.update(st, u)
	w.retitle(u)
	w.veil.update(st, u)
	w.keys.update(u)
	u.Invalidate()
}

// mourn has the pane the guest comes to, where the one it was in has gone,
// take nothing typed right away: that may be the end of a line the other
// had the beginning of.
func (w *windowPage) mourn(in string, u *gunim.UI) {
	if in != "" {
		if _, here := w.p.paneOf(in); !here {
			w.bereft = true
		}
	}
	if now := w.dock.Active(); w.bereft && now != "" {
		if _, here := w.p.paneOf(now); here {
			w.bereft = false
			w.pane(now).deaf = u.Now().Add(deafFor)
		}
	}
}

// arrange has the dock show the host's panes, with the guest's own tabs
// in front once it has picked any, and one at a time on a narrow page.
func (w *windowPage) arrange(u *gunim.UI) {
	l := w.host.Clone()
	was := w.dock.Arrangement()
	active := w.dock.Active()
	if !w.follow {
		keepFronts(&l, was, active)
	}
	if w.narrow && len(l.Items()) > 0 {
		if w.follow && slices.Contains(l.Items(), w.hostIn) {
			// Not shown until it is in front, so not the dock's yet.
			active = w.hostIn
		}
		if !slices.Contains(l.Items(), active) {
			active = ""
			if shown := l.Shown(guestlook.LayoutWindow); len(shown) > 0 {
				active = shown[0]
			}
		}
		group := ""
		if g := w.host.GroupOf(active); g != nil {
			group = g.ID
		}
		l = oneGroup(l.Items(), active, group)
	}
	w.dock.SetLayout(l, u)
	w.sayShown(u)
}

// keepFronts puts in front in l the tabs that were in front in was, active
// before the others, where they still stand.
func keepFronts(l *dock.Layout, was dock.Layout, active string) {
	fronts := was.Shown(guestlook.LayoutWindow)
	// The last one shown in a group is in front of it.
	if i := slices.Index(fronts, active); i >= 0 {
		fronts = append(slices.Delete(fronts, i, i+1), active)
	}
	for _, id := range fronts {
		if l.Node(id) != nil {
			l.Show(id)
		}
	}
}

// sayShown tells the program which panes are on the screen, when that is
// new.
func (w *windowPage) sayShown(u *gunim.UI) {
	if shown := w.dock.Shown(); !w.laid || !slices.Equal(shown, w.shown) {
		w.laid = true
		w.shown = shown
		u.Send(w.p, Shown{slices.Clone(shown)})
	}
}

// picked hears that the guest picked pane id: from now on it has tabs of
// its own in front, and goes nowhere with the host.
func (w *windowPage) picked(id string, u *gunim.UI) {
	w.unfollow(u)
	w.sayShown(u)
	w.take(id, u)
}

// unfollow has the guest follow the host no more, from the pane it is in.
func (w *windowPage) unfollow(u *gunim.UI) {
	if !w.follow {
		return
	}
	w.follow = false
	if id := w.dock.Active(); id != "" {
		// Where it is first, for the host to hear that it stays there.
		u.Send(w.p, In{id})
	}
	u.Send(w.p, Follows{false})
	w.bar.following(false, u)
}

// followHost has the guest follow the host again: the host's tabs in
// front, and the pane the host is in.
func (w *windowPage) followHost(u *gunim.UI) {
	if w.follow {
		return
	}
	w.follow = true
	u.Send(w.p, Follows{true})
	w.bar.following(true, u)
	w.arrange(u)
	if w.hostIn != "" {
		w.take(w.hostIn, u)
	}
	w.retitle(u)
	u.Invalidate()
}

// retitle names the bar and the browser's tab after the pane the guest is
// in.
func (w *windowPage) retitle(u *gunim.UI) {
	title := ""
	if pn, ok := w.p.paneOf(w.dock.Active()); ok {
		title = pn.Title
	}
	if title == w.title {
		return
	}
	w.title = title
	w.bar.retitle(title, u)
	w.p.sayTitle(u)
}

// take gives pane id the keyboard.
func (w *windowPage) take(id string, u *gunim.UI) {
	w.dock.SetActive(id, u)
	w.retitle(u)
	if w.p.dialog != nil || u.HasFocus(w.pane(id)) {
		return
	}
	v := w.pane(id)
	switch {
	case v.term == nil && v.face.says != "":
		// What is typed is for none of the others.
		u.Focus(nil)
		u.Send(w.p, In{id})
	case u.Presence(v) == gunim.Exiting:
		// Not in the tree yet: once it is laid out.
		w.wants = id
	default:
		// It says so itself as it has it.
		u.FocusFirstLaidOut(v)
	}
}

// keyboard gives the keyboard to the pane it is in.
func (w *windowPage) keyboard(u *gunim.UI) {
	if id := w.dock.Active(); id != "" {
		w.take(id, u)
	}
}

// setAway hears that the page lost the keyboard, or has it back.
func (w *windowPage) setAway(away bool, u *gunim.UI) {
	for t := range w.terms {
		t.SetAway(away, u)
	}
}

// ring hears pane id's bell: the pane lights a moment.
func (w *windowPage) ring(id string, u *gunim.UI) {
	if v, ok := w.panes[id]; ok {
		v.ring(u)
	}
}

// hint says the guest may only watch, as it typed in v.
func (w *windowPage) hint(v *paneView, u *gunim.UI) {
	if !w.bar.swing.Active() {
		u.Announce(canWatch)
	}
	w.bar.shake(u)
	v.warn(u)
}

// touched hears that a finger is at the page, or a mouse is again.
func (w *windowPage) touched(u *gunim.UI) {
	w.tabs.Use(w.tabsLook())
	w.keys.update(u)
	w.narrows(u)
}

// narrows has the panes stand one at a time where there is room for no
// more, and as the host's where there is.
func (w *windowPage) narrows(u *gunim.UI) {
	narrow := w.narrowIn(w.room, u.Theme())
	if w.room == (geom.Size{}) || narrow == w.narrow {
		return
	}
	w.narrow = narrow
	w.bar.narrow(narrow, u)
	w.tabs.Use(w.tabsLook())
	w.sized(u)
	w.arrange(u)
	u.Invalidate()
}

// narrowIn reports whether the panes stand one at a time in room.
func (w *windowPage) narrowIn(room geom.Size, th *theme.Live) bool {
	return room.W < narrowAt.Get(th) || w.p.touch && room.H < lowAt.Get(th)
}

// Children implements [gunim.Composite].
func (w *windowPage) Children() []gunim.Node {
	return []gunim.Node{w.picture, w.keys.fold, w.bar, w.veil}
}

// Layout implements [gunim.Node].
func (w *windowPage) Layout(c gunim.Constraints, f gunim.Frame, kids gunim.Children) geom.Size {
	size := c.Max
	// A phone's keyboard takes none of the page's own height.
	if room := geom.Sz(size.W, size.H+f.Keyboard); room != w.room {
		w.room = room
		if w.p.u != nil {
			// Once this layout is done.
			w.p.u.After(0, w.narrows)
		}
	}
	top := barHeight.Get(f.Theme)
	bk := kids.At(2)
	bk.Layout(gunim.Tight(geom.Sz(size.W, top)))
	bk.Place(geom.Point{})
	ks := kids.At(1).Layout(gunim.Constraints{Min: geom.Sz(size.W, 0), Max: size})
	kids.At(1).Place(geom.Pt(0, size.H-ks.H))
	rest := geom.Sz(size.W, max(size.H-top-ks.H, 0))
	for _, i := range []int{0, 3} {
		kids.At(i).Layout(gunim.Tight(rest))
		kids.At(i).Place(geom.Pt(0, top))
	}
	if id := w.wants; id != "" && w.p.u != nil {
		w.wants = ""
		w.p.u.After(0, func(u *gunim.UI) {
			if w.dock.Active() == id && u.Presence(w.pane(id)) != gunim.Exiting {
				w.take(id, u)
			}
		})
	}
	return size
}

// Paint implements [gunim.Node].
func (w *windowPage) Paint(p *paint.Painter, _ gunim.Frame, _ geom.Size, kids gunim.Children) {
	for k := range kids.All {
		k.Paint(p)
	}
}

// veil lies over the panes while the connection is lost, and says so. The
// panes show through it as they were.
type veil struct {
	anim.Group
	w  *windowPage
	on *anim.Float
	// way is how far what it says makes way for a dialog.
	way  *anim.Float
	lost bool
	body *widget.Flex
	says *widget.Pages
	said string
}

// veilShade is how much of what is under the veil it hides, veilWide how
// wide what it says is, and veilPad the room round that.
var (
	veilShade  = theme.Number("kakel.page.veil.shade", 0.72)
	veilWide   = theme.Length("kakel.page.veil.wide", 320)
	veilPad    = theme.Length("kakel.page.veil.pad", 22)
	veilRadius = widget.DialogRadius
)

func newVeil(w *windowPage) *veil {
	v := &veil{w: w, on: anim.NewFloat(0), way: anim.NewFloat(0)}
	v.Add(v.on, v.way)
	title := widget.NewLabel(lostTitle)
	title.Face = widget.BoldFont
	v.said = lostSays
	v.says = widget.NewPages(veilLabel(v.said))
	v.says.InTurn = true
	leave := widget.NewButton("Leave")
	leave.OnClick = func(u *gunim.UI) gunim.Intent {
		w.p.askLeave(u)
		return nil
	}
	v.body = widget.Column(title, v.says, busyBar(), leave)
	v.body.Cross = widget.CrossCenter
	return v
}

func veilLabel(s string) *widget.Label {
	l := widget.NewLabel(s)
	l.Color = quietInk
	return l
}

// update shows whether the connection is lost, and what is said of it.
func (v *veil) update(st State, u *gunim.UI) {
	if st.Lost != v.lost {
		v.lost = st.Lost
		v.on.Animate(anim.OnOff(st.Lost), widget.Settle.Get(u.Theme()))
		if st.Lost {
			u.Announce(lostTitle + ". " + lostSays)
		}
	}
	says := lostSays
	if st.Why != "" {
		says = st.Why + " Trying again…"
	}
	if st.Lost && says != v.said {
		v.said = says
		v.says.Show(veilLabel(says), geom.Pt(0, 1), u)
	}
}

// asking has what the veil says make way for a dialog, or come back.
func (v *veil) asking(on bool, u *gunim.UI) {
	v.way.Animate(anim.OnOff(on), widget.Quick.Get(u.Theme()))
	u.Invalidate()
}

// Covers implements [gunim.Shaped]: what is under the veil is not
// clicked.
func (v *veil) Covers(geom.Point) bool { return v.lost }

// Handle implements [gunim.Handler].
func (v *veil) Handle(e gi.Event, _ *gunim.UI) bool {
	switch e.(type) {
	case gi.PointerDown, gi.PointerUp, gi.PointerMove, gi.Scroll:
		return v.lost
	}
	return false
}

// Children implements [gunim.Composite].
func (v *veil) Children() []gunim.Node { return []gunim.Node{v.body} }

// Layout implements [gunim.Node].
func (v *veil) Layout(c gunim.Constraints, f gunim.Frame, kids gunim.Children) geom.Size {
	k := kids.At(0)
	if !v.lost && v.on.Value() < 0.001 {
		// Not there, for the keyboard either.
		k.Layout(gunim.Constraints{})
		return c.Max
	}
	pad := veilPad.Get(f.Theme)
	w := max(min(veilWide.Get(f.Theme), c.Max.W-4*pad), 0)
	s := k.Layout(gunim.Constraints{Min: geom.Sz(w, 0), Max: geom.Sz(w, 0)})
	k.Place(geom.Pt((c.Max.W-s.W)/2, (c.Max.H-s.H)/2))
	return c.Max
}

// Paint implements [gunim.Node].
func (v *veil) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	on := min(max(v.on.Value(), 0), 1)
	if on < 0.001 {
		return
	}
	th := f.Theme
	shade := look.TermBackground.Get(th)
	shade.A = uint8(255 * on * min(max(veilShade.Get(th), 0), 1))
	p.RRect(geom.Rect{Max: box.Point()}, 0, paint.Solid(shade))
	if on *= 1 - min(max(v.way.Value(), 0), 1); on < 0.001 {
		return
	}
	defer p.Layer(paint.LayerOpts{Bounds: geom.Rect{Max: box.Point()}, Opacity: on})()
	defer p.Push(paint.Translate(geom.Pt(0, 12*(1-on))))()
	k := kids.At(0)
	s := k.Size()
	pad := veilPad.Get(th)
	r := geom.Rc((box.W-s.W)/2, (box.H-s.H)/2, s.W, s.H).Inset(geom.Uniform(-pad))
	p.RRectStroke(r, veilRadius.Get(th), paint.Solid(widget.DialogFill.Get(th)), paint.Stroke{Width: 1, Color: widget.DialogBorder.Get(th)})
	k.Paint(p)
}

// iconOf is the picture a pane's tab has.
func iconOf(p guest.Pane) *icon.Icon {
	if ic, ok := look.KindIcons[p.Icon]; ok {
		return ic
	}
	return look.KindIcons["terminal"]
}
