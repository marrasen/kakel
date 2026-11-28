package guestview

import (
	"math"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/icon"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"
)

// The measures of the bar over the window: how tall it is, the room at
// its ends and between its parts, its icon, how far the state swings as
// it shakes, the least room for the title and who shares it, and the size
// of the latter.
var (
	barHeight = theme.TextRow("kakel.page.bar.height", 40)
	barPad    = theme.Length("kakel.page.bar.pad", 10)
	barGap    = theme.Length("kakel.page.bar.gap", 8)
	barIcon   = theme.Length("kakel.page.bar.icon", 20)
	barShake  = theme.Length("kakel.page.bar.shake", 12)
	barBy     = theme.Length("kakel.page.bar.by", 110)
	barSmall  = theme.FontSize("kakel.page.bar.small", 12)
)

// bar is over the window: the pane the guest is in, and under that who
// shares it, which no title takes the room of, whether the guest may type,
// the way back to following the host while it does not, and the way out.
// While the connection is lost, none of the ways is there: the veil says
// how it stands, and has a way out of its own.
type bar struct {
	anim.Group
	w *windowPage
	// title fades from one pane's title to the next.
	title *widget.Pages
	by    *widget.Label
	// watch and types say whether the guest may type: one shows at a
	// time, and swing is how far they are shaken to the side.
	types, watch *widget.Badge
	swing        *anim.Float
	// fill and unfill fill the screen and give it back, and leave and
	// leaveIcon leave, the one with room for the word and the other not.
	fill, unfill, leave, leaveIcon *widget.Fold
	full, short, lost              bool
	// follow and followIcon follow the host again, the one with room for
	// the word and the other not, and own says the guest does not follow.
	follow, followIcon *widget.Fold
	followButton       *widget.Button
	followIconButton   *widget.IconButton
	own                bool
}

func newBar(w *windowPage) *bar {
	b := &bar{w: w, title: widget.NewPages(barTitle("")), by: widget.NewLabel(""), types: widget.NewBadge(canType), watch: widget.NewBadge(canWatch),
		swing: anim.NewFloat(0)}
	b.Add(b.swing)
	b.title.InTurn = true
	b.by.MaxLines, b.by.Color, b.by.Size = 1, quietInk, barSmall
	b.types.Ink, b.watch.Ink = mayInk, watchInk
	b.types.SetHidden(!w.st.CanType || w.st.Lost, nil)
	b.watch.SetHidden(w.st.CanType || w.st.Lost, nil)
	b.lost = w.st.Lost
	b.fill = sideFold(iconButton(icon.Maximize, "Full Screen", func(u *gunim.UI) { u.SetFullScreen(true) }), w.p.cfg.CanFill)
	b.unfill = sideFold(iconButton(icon.Minimize, "Exit Full Screen", func(u *gunim.UI) { u.SetFullScreen(false) }), false)
	leave := widget.NewButton("Leave")
	// The keyboard stays in the terminal.
	leave.Icon, leave.Ghost, leave.KeepFocus = icon.LogOut, true, true
	leave.OnClick = func(u *gunim.UI) gunim.Intent {
		w.p.askLeave(u)
		return nil
	}
	b.leave = sideFold(leave, true)
	b.leaveIcon = sideFold(iconButton(icon.LogOut, "Leave", w.p.askLeave), false)
	b.followButton = widget.NewButton(followWord(w.st.HostName))
	b.followButton.Icon, b.followButton.Ghost, b.followButton.KeepFocus = icon.LocateFixed, true, true
	b.followButton.OnClick = func(u *gunim.UI) gunim.Intent {
		w.followHost(u)
		return nil
	}
	b.follow = sideFold(b.followButton, false)
	b.followIconButton = iconButton(icon.LocateFixed, followWord(w.st.HostName), w.followHost)
	b.followIcon = sideFold(b.followIconButton, false)
	return b
}

// barTitle is the title of the pane the guest is in, as the bar shows it.
func barTitle(s string) *widget.Label {
	l := widget.NewLabel(s)
	l.MaxLines, l.Face = 1, widget.BoldFont
	return l
}

// retitle shows the title of the pane the guest is in.
func (b *bar) retitle(title string, u *gunim.UI) {
	at := u
	if u.Presence(b) == gunim.Exiting {
		// Not on the screen yet: as it is when it comes.
		at = nil
	}
	b.title.Show(barTitle(title), geom.Point{}, at)
}

// iconButton is a button of an icon, named for what it does.
func iconButton(ic *icon.Icon, name string, do func(u *gunim.UI)) *widget.IconButton {
	b := widget.NewIconButton(ic, name)
	b.KeepFocus = true
	b.OnClick = func(u *gunim.UI) gunim.Intent {
		do(u)
		return nil
	}
	return b
}

// sideFold folds a node to the side.
func sideFold(n gunim.Node, open bool) *widget.Fold {
	f := widget.NewFold(n, open)
	f.Axis = widget.Horizontal
	return f
}

// update shows st.
func (b *bar) update(st State, u *gunim.UI) {
	b.by.Text = "Shared by " + hostOr(st.HostName)
	b.followButton.Label = followWord(st.HostName)
	b.followIconButton.Tooltip = followWord(st.HostName)
	types, watch := st.CanType && !st.Lost, !st.CanType && !st.Lost
	switch {
	case types && b.types.Hidden():
		u.Announce(canType)
	case watch && b.watch.Hidden():
		u.Announce(canWatch)
	}
	b.types.SetHidden(!types, u)
	b.watch.SetHidden(!watch, u)
	if st.Lost != b.lost {
		b.lost = st.Lost
		b.leaves(u)
	}
}

// following hears whether the guest follows the host.
func (b *bar) following(on bool, u *gunim.UI) {
	b.own = !on
	b.leaves(u)
}

// narrow has the bar do with less room, or not.
func (b *bar) narrow(on bool, u *gunim.UI) {
	b.short = on
	b.leaves(u)
}

// leaves has the ways back and out that there is room for show.
func (b *bar) leaves(u *gunim.UI) {
	b.leave.SetOpen(!b.short && !b.lost, u)
	b.leaveIcon.SetOpen(b.short && !b.lost, u)
	b.follow.SetOpen(!b.short && !b.lost && b.own, u)
	b.followIcon.SetOpen(b.short && !b.lost && b.own, u)
}

// shake swings what says the guest may only watch.
func (b *bar) shake(u *gunim.UI) {
	if b.swing.Active() {
		return
	}
	const swings = 0.2
	anim.Kick(b.swing, 2*math.Pi/swings, anim.Spring{Response: swings, Damping: 0.16})
	u.Invalidate()
}

// Children implements [gunim.Composite].
func (b *bar) Children() []gunim.Node {
	return []gunim.Node{b.title, b.by, b.types, b.watch, b.fill, b.unfill, b.leaveIcon, b.leave, b.followIcon, b.follow}
}

// Layout implements [gunim.Node]: from its right end in, and the title
// over who shares it in what is left.
func (b *bar) Layout(c gunim.Constraints, f gunim.Frame, kids gunim.Children) geom.Size {
	if u := b.w.p.u; u != nil && u.FullScreen() != b.full {
		b.full = !b.full
		full := b.full
		u.After(0, func(u *gunim.UI) {
			b.fill.SetOpen(!full && b.w.p.cfg.CanFill, u)
			b.unfill.SetOpen(full, u)
		})
	}
	size := c.Max
	th := f.Theme
	gap := barGap.Get(th)
	x := size.W - barPad.Get(th)
	left := barPad.Get(th) + barIcon.Get(th) + gap
	// right places a part at the right end of what is left.
	right := func(i int, space float32) {
		s := kids.At(i).Layout(gunim.Constraints{Max: geom.Sz(0, size.H)})
		x -= s.W
		kids.At(i).Place(geom.Pt(x, (size.H-s.H)/2))
		x -= space * min(s.W/24, 1)
	}
	right(7, gap)
	right(6, gap)
	right(5, gap/2)
	right(4, gap)
	// The way back to following, and what says whether the guest may
	// type, leave the title room for who shares it: a long name is cut.
	for _, i := range []int{9, 8} {
		s := kids.At(i).Layout(gunim.Constraints{Max: geom.Sz(max(x-left-barBy.Get(th), 1), size.H)})
		x -= s.W
		kids.At(i).Place(geom.Pt(x, (size.H-s.H)/2))
		x -= gap * min(s.W/24, 1)
	}
	swing := barShake.Get(th) * b.swing.Value()
	for _, i := range []int{3, 2} {
		s := kids.At(i).Layout(gunim.Constraints{Max: geom.Sz(max(x-left-barBy.Get(th), 1), size.H)})
		x -= s.W
		kids.At(i).Place(geom.Pt(x+swing, (size.H-s.H)/2))
	}
	x -= gap
	room := geom.Sz(max(x-left, 1), size.H)
	ts := kids.At(0).Layout(gunim.Constraints{Max: room})
	bs := kids.At(1).Layout(gunim.Constraints{Max: room})
	y := (size.H - ts.H - bs.H) / 2
	kids.At(0).Place(geom.Pt(left, y))
	kids.At(1).Place(geom.Pt(left, y+ts.H))
	return size
}

// Paint implements [gunim.Node].
func (b *bar) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	th := f.Theme
	p.RRect(geom.Rect{Max: box.Point()}, 0, paint.Solid(widget.MenubarFill.Get(th)))
	s := barIcon.Get(th)
	p.Image(appIcon(), geom.Rc(barPad.Get(th), (box.H-s)/2, s, s), paint.ImageOpts{Opacity: 1})
	for k := range kids.All {
		k.Paint(p)
	}
}

// keyRow is a row of keys a phone's keyboard has not, shown while a
// finger works in a terminal that can be typed in, and under it the rest,
// which its last key folds out.
type keyRow struct {
	fold, more *widget.Fold
	w          *windowPage
	// keys are the keys that type, as rowKeys and moreKeys have them, and
	// ctrl and alt those that go with the next one.
	keys            []*widget.Button
	ctrl, alt, rest *widget.Button
}

// rowKey is a key of the row.
type rowKey struct {
	name string
	icon *icon.Icon
	key  gi.Key
}

// rowKeys are the row's keys that type, and moreKeys those under it.
var (
	rowKeys = []rowKey{
		{"Esc", nil, gi.KeyEscape}, {"Tab", nil, gi.KeyTab},
		{"Left", icon.ArrowLeft, gi.KeyLeft}, {"Down", icon.ArrowDown, gi.KeyDown}, {"Up", icon.ArrowUp, gi.KeyUp}, {"Right", icon.ArrowRight, gi.KeyRight},
	}
	moreKeys = []rowKey{
		{"Home", nil, gi.KeyHome}, {"End", nil, gi.KeyEnd}, {"PgUp", nil, gi.KeyPageUp}, {"PgDn", nil, gi.KeyPageDown},
	}
)

// The measures of the row: the room between its keys, and round them.
var (
	keyGap  = theme.Length("kakel.page.keys.gap", 2)
	keyRoom = theme.Insets("kakel.page.keys.room", geom.Uniform(4))
)

// keySide is the room at the sides of a key's word.
const keySide = 2

func newKeyRow(w *windowPage) *keyRow {
	r := &keyRow{w: w}
	// held is a key that goes with the next.
	held := func(name string, on func(t *term) *bool) *widget.Button {
		b := widget.NewButton(name)
		b.KeepFocus = true
		b.OnClick = func(u *gunim.UI) gunim.Intent {
			if t := r.term(); t != nil {
				*on(t) = !*on(t)
				r.update(u)
			}
			return nil
		}
		return b
	}
	r.ctrl = held("Ctrl", func(t *term) *bool { return &t.ctrl })
	r.alt = held("Alt", func(t *term) *bool { return &t.alt })
	r.rest = widget.NewButton("")
	r.rest.Icon, r.rest.Tooltip, r.rest.KeepFocus = icon.Ellipsis, "More Keys", true
	r.rest.OnClick = func(u *gunim.UI) gunim.Intent {
		r.more.SetOpen(!r.more.Open(), u)
		r.update(u)
		return nil
	}
	first := []gunim.Node{r.key(rowKeys[0]), r.key(rowKeys[1]), r.ctrl}
	for _, k := range rowKeys[2:] {
		first = append(first, r.key(k))
	}
	under := []gunim.Node{r.alt}
	for _, k := range moreKeys {
		under = append(under, r.key(k))
	}
	r.more = widget.NewFold(keysIn(under), false)
	all := widget.Column(keysIn(append(first, r.rest)), r.more)
	all.Gap, all.Cross = keyGap, widget.CrossStretch
	pad := widget.NewPad(widget.NewThemed(all, theme.Make("keys", theme.Set(widget.ButtonPadding, keySide))))
	pad.Padding = keyRoom
	r.fold = widget.NewFold(pad, false)
	return r
}

// keysIn is a row of keys of one width, the width of the page together.
func keysIn(keys []gunim.Node) gunim.Node {
	row := widget.Row(keys...)
	row.Gap = keyGap
	for _, k := range keys {
		row.Grow(k, 1)
	}
	return row
}

// key is a key that types.
func (r *keyRow) key(k rowKey) *widget.Button {
	b := widget.NewButton(k.name)
	if k.icon != nil {
		b.Label, b.Icon, b.Tooltip = "", k.icon, k.name
	}
	b.KeepFocus = true
	b.OnClick = func(u *gunim.UI) gunim.Intent {
		if t := r.term(); t != nil {
			t.Handle(gi.KeyPress{Key: k.key, Mods: t.heldMods(u), Time: u.Now()}, u)
		}
		return nil
	}
	r.keys = append(r.keys, b)
	return b
}

// term is the terminal the keys go to.
func (r *keyRow) term() *term {
	if v, ok := r.w.panes[r.w.dock.Active()]; ok {
		return v.term
	}
	return nil
}

// update shows the row while it is of use.
func (r *keyRow) update(u *gunim.UI) {
	t := r.term()
	r.ctrl.Active, r.alt.Active = t != nil && t.ctrl, t != nil && t.alt
	r.rest.Active = r.more.Open()
	r.fold.SetOpen(r.w.p.touch && r.w.may() && t != nil, u)
	u.Invalidate()
}
