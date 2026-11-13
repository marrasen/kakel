package view

import (
	"maps"
	"slices"
	"strings"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/access"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/icon"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/look"
	"github.com/marrasen/kakel/settings"
	"github.com/marrasen/kakel/words"
)

// Layouts: the Save Layout dialog, the palette's line for each layout,
// and the Layouts pane, a card for each layout to open, rename, give
// start lines or remove, and to have open as kakel starts.

// layoutOpenPrefix starts the command that opens a layout, by the
// layout's ID, which a rename leaves as it was, so a key bound to it
// goes on opening the same layout.
const layoutOpenPrefix = "layout.open."

// setLayouts takes the layouts, and puts them in the palette and the
// shortcut editor when they changed. An open Save Layout dialog says
// again what the name typed replaces.
func (w *Window) setLayouts(layouts []settings.SavedLayout, u *gunim.UI) {
	same := slices.EqualFunc(layouts, w.layouts, sameLayout)
	w.layouts = layouts
	if same {
		return
	}
	// File's Layouts, and with it the Machines menu and the palette.
	w.buildBar(true)
	if w.sayReplaces != nil {
		w.sayReplaces(u)
	}
}

// sameLayout reports whether a and b say the same in the palette, the
// shortcut editor and on their cards, and hold the same start lines, so
// what shows them is built again when they change.
func sameLayout(a, b settings.SavedLayout) bool {
	return a.ID == b.ID && a.Name == b.Name && a.AtStart == b.AtStart && app.LayoutNote(a) == app.LayoutNote(b) &&
		app.LayoutHolds(a) == app.LayoutHolds(b) && slices.Equal(app.LayoutMachines(a, nil), app.LayoutMachines(b, nil)) &&
		maps.EqualFunc(a.Panes, b.Panes, func(x, y settings.SavedPane) bool { return slices.Equal(x.Start, y.Start) })
}

// layoutNamed is the layout called name, as the settings tell names
// apart: trimmed, and in any case.
func (w *Window) layoutNamed(name string) (settings.SavedLayout, bool) {
	name = strings.TrimSpace(name)
	for _, l := range w.layouts {
		if strings.EqualFold(l.Name, name) {
			return l, true
		}
	}
	return settings.SavedLayout{}, false
}

// layoutHosts are the machines layout l opens panes on, by the names the
// window has for them now.
func (w *Window) layoutHosts(l settings.SavedLayout) []app.LayoutHost {
	return app.LayoutHosts(l, func(id string) (string, bool) {
		for _, h := range w.saved {
			if h.ID == id {
				return h.Name, true
			}
		}
		return "", false
	})
}

// layoutItems are the palette's lines that open each layout, in the
// launcher's words.
func (w *Window) layoutItems(hint func(id string) string) ([]widget.PaletteItem, []string) {
	var items []widget.PaletteItem
	var ids []string
	for _, l := range w.layouts {
		id := layoutOpenPrefix + l.ID
		also := []string{"layout", "open", "window", "saved"}
		for _, h := range w.layoutHosts(l) {
			also = append(also, h.Name)
		}
		items = append(items, widget.PaletteItem{Title: l.Name, Detail: app.LayoutNote(l), Icon: icon.LayoutPanelLeft, Hint: hint(id), Also: also})
		ids = append(ids, id)
	}
	return items, ids
}

// saveLayoutDialog asks for the name to save a window as a layout under:
// this window, or from a window of tools, the window last worked in. It
// offers the name of the layout that window was opened from or last
// saved as, to save over it again. While the name is another layout's, a
// line under it says that one is replaced, and Save replaces it. With no
// window to save, it says so in place of asking.
func (w *Window) saveLayoutDialog(u *gunim.UI) {
	if w.saveWindow == 0 {
		w.failed("Couldn't save the layout", app.ErrNothingToSave.Error(), u)
		return
	}
	name := widget.NewTextField()
	for _, l := range w.layouts {
		if l.ID == w.saveFrom {
			name.SetText(l.Name, nil)
		}
	}
	replaces := widget.NewLabel("")
	line := widget.NewFold(replaces, false)
	// Said as the name is typed, and as the layouts change under it: the
	// line grows in under the field, and folds away again, its words
	// kept until it has.
	say := func(u *gunim.UI) {
		l, taken := w.layoutNamed(name.Text())
		if taken {
			replaces.Text = "Replaces the layout " + l.Name + "."
		}
		line.SetOpen(taken, u)
	}
	say(nil)
	name.OnChange = func(_ string, u *gunim.UI) gunim.Intent {
		say(u)
		return nil
	}
	d := widget.NewDialog("Save Layout")
	d.Body = widget.NewForm().Add("Name", name).Add("", line)
	d.SetButtons("Save", "Cancel")
	d.Check = func() string {
		if strings.TrimSpace(name.Text()) == "" {
			return "Enter a name"
		}
		return ""
	}
	window := w.saveWindow
	d.OnAccept = func(*gunim.UI) gunim.Intent {
		_, taken := w.layoutNamed(name.Text())
		return app.SaveLayout{Window: window, Name: strings.TrimSpace(name.Text()), Replace: taken}
	}
	d.OnDismiss = widget.Sends(app.DialogClosed{})
	w.sayReplaces = func(u *gunim.UI) {
		if u.Presence(d) != gunim.Exiting {
			say(u)
		}
	}
	w.openDialog(d, u)
}

// renameLayoutDialog asks for another name for layout l.
func (w *Window) renameLayoutDialog(l settings.SavedLayout, u *gunim.UI) {
	name := widget.NewTextField()
	name.SetText(l.Name, nil)
	d := widget.NewDialog("Rename Layout")
	d.Body = widget.NewForm().Add("Name", name)
	d.SetButtons("Save", "Cancel")
	d.Check = func() string {
		typed := strings.TrimSpace(name.Text())
		if typed == "" {
			return "Enter a name"
		}
		if other, taken := w.layoutNamed(typed); taken && other.ID != l.ID {
			return "Name already used"
		}
		return ""
	}
	d.OnAccept = func(*gunim.UI) gunim.Intent {
		return app.RenameLayout{ID: l.ID, Name: strings.TrimSpace(name.Text())}
	}
	d.OnDismiss = widget.Sends(app.DialogClosed{})
	w.openDialog(d, u)
}

// The controls on a layout's card, in the order they are read and Tab
// takes them.
const (
	ctlOpen = iota
	ctlRename
	ctlLines
	ctlRemove
	ctlStart
	ctlCount
)

// layoutsPane lists the layouts as cards, in the order they were saved,
// under a heading with Save Layout beside it. With none, it says how
// one is made.
type layoutsPane struct {
	w     *Window
	head  *widget.Label
	save  *widget.Button
	empty *widget.Fold
	list  *widget.List
	body  *widget.Scroll
	// cards are the cards of the layouts shown, by ID, and ids those
	// layouts in order; a card on its way out is in neither.
	cards map[string]*layoutCard
	ids   []string
	// lastID and lastCtl are the card and the control the keyboard was
	// last on in the pane, for it to come back to.
	lastID  string
	lastCtl int
}

func newLayoutsPane(w *Window) *layoutsPane {
	p := &layoutsPane{w: w, head: widget.NewLabel("Layouts"), save: iconButton(icon.Save, "Save Layout…"), list: widget.NewList(),
		cards: map[string]*layoutCard{}}
	p.head.Size = widget.DialogTitleSize
	p.save.OnClick = func(u *gunim.UI) gunim.Intent {
		p.lastID = ""
		w.run("layout.save", u)
		return nil
	}
	none := widget.NewLabel("Arrange a window, then choose Save Layout… in File › Layouts.")
	none.Color = look.Faint
	pad := widget.NewPad(none)
	pad.Padding = layoutsEmptyPad
	p.empty = widget.NewFold(pad, true)
	col := widget.Column(p.empty, p.list)
	col.Cross, col.Gap = widget.CrossStretch, noGap
	p.body = widget.NewScroll(widget.NewPad(col))
	return p
}

// layoutsEmptyPad is the room round the words a Layouts pane with no
// layouts says: in line with the heading.
var layoutsEmptyPad = theme.Insets("kakel.layouts.empty.pad", geom.Insets{Left: 4, Top: 4, Bottom: 12})

// show brings the cards up to date with layouts, each card changing in
// place. Where the card the keyboard was on has gone, the keyboard goes
// to the same control on the card after it, or else the one before it,
// or with none left, to Save Layout.
func (p *layoutsPane) show(layouts []settings.SavedLayout, u *gunim.UI) {
	p.note(u)
	was := p.ids
	p.ids = p.ids[:0:0]
	live := map[string]bool{}
	for _, l := range layouts {
		p.ids = append(p.ids, l.ID)
		live[l.ID] = true
	}
	for id := range p.cards {
		if !live[id] {
			delete(p.cards, id)
		}
	}
	widget.Sync(p.list, u, layouts,
		func(l settings.SavedLayout) widget.Key { return widget.Key(l.ID) },
		func(l settings.SavedLayout) *layoutCard { return newLayoutCard(p, l) },
		func(c *layoutCard, l settings.SavedLayout, u *gunim.UI) { c.show(l, u) })
	for _, k := range p.list.Keys() {
		if c, ok := widget.RowOf[*layoutCard](p.list, k); ok && live[c.id] {
			p.cards[c.id] = c
		}
	}
	p.empty.SetOpen(len(layouts) == 0, u)
	if p.lastID == "" || live[p.lastID] {
		return
	}
	// The card the keyboard was on has gone. Where the keyboard has gone
	// somewhere else of the pane's since, it stays there.
	gone := p.lastID
	p.lastID = ""
	if f := u.Focused(); f != nil && p.inLive(f, u) {
		return
	}
	if f := u.Focused(); f != nil && !u.HasFocus(p) {
		// Elsewhere in the window, as in a dialog.
		return
	}
	at := slices.Index(was, gone)
	for _, near := range []int{at + 1, at - 1} {
		if at >= 0 && near >= 0 && near < len(was) && live[was[near]] {
			p.lastID = was[near]
			u.Focus(p.cards[p.lastID].control(p.lastCtl))
			return
		}
	}
	u.Focus(p.save)
}

// inLive reports whether n is the pane's own Save Layout, or a control
// on a card of a layout shown.
func (p *layoutsPane) inLive(n gunim.Node, u *gunim.UI) bool {
	if n == gunim.Node(p.save) {
		return true
	}
	for _, c := range p.cards {
		if u.HasFocus(c) {
			return true
		}
	}
	return false
}

// note keeps which card and control the keyboard is on, where it is on
// one of the pane's.
func (p *layoutsPane) note(u *gunim.UI) {
	f := u.Focused()
	if f == nil {
		return
	}
	if f == gunim.Node(p.save) {
		p.lastID = ""
		return
	}
	for id, c := range p.cards {
		for i := range ctlCount {
			if f == c.control(i) {
				p.lastID, p.lastCtl = id, i
				return
			}
		}
	}
}

// focusTarget is what takes the keyboard as the pane comes forward: the
// control it was last on, while its layout is still shown, or else Save
// Layout, which asks before it does anything.
func (p *layoutsPane) focusTarget(u *gunim.UI) gunim.Node {
	p.note(u)
	if c, ok := p.cards[p.lastID]; ok {
		return c.control(p.lastCtl)
	}
	return p.save
}

// Children implements [gunim.Composite].
func (p *layoutsPane) Children() []gunim.Node { return []gunim.Node{p.head, p.save, p.body} }

// Layout implements [gunim.Node]: the heading and Save Layout along the
// top, the cards under them.
func (p *layoutsPane) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	const pad, top = 20, 56
	head, save, body := kids.At(0), kids.At(1), kids.At(2)
	ss := save.Layout(gunim.Constraints{Max: geom.Sz(c.Max.W, top)})
	save.Place(geom.Pt(c.Max.W-pad-ss.W, (top-ss.H)/2))
	hs := head.Layout(gunim.Constraints{Max: geom.Sz(max(0, c.Max.W-3*pad-ss.W), top)})
	head.Place(geom.Pt(pad, (top-hs.H)/2))
	body.Layout(gunim.Tight(geom.Sz(c.Max.W, max(0, c.Max.H-top))))
	body.Place(geom.Pt(0, top))
	return c.Max
}

// Paint implements [gunim.Node].
func (p *layoutsPane) Paint(pt *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	pt.RRect(geom.Rect{Max: box.Point()}, 0, paint.Solid(widget.Background.Get(f.Theme)))
	for k := range kids.All {
		k.Paint(pt)
	}
}

// Handle implements [gunim.Handler]: the keyboard coming into the pane
// makes it the one in front.
func (p *layoutsPane) Handle(e gi.Event, u *gunim.UI) bool {
	if _, ok := e.(gi.FocusEntered); ok {
		p.w.entered(p.w.paneOfKind(app.KindLayouts), u)
	}
	return false
}

// layoutCard is one layout: a badge, its name and what it opens, the
// machines it opens panes on, and under them Open, Rename, Start
// Lines…, Remove and whether it opens as kakel starts, in a row that runs on to the next
// line where it has no room, each control gliding to its place.
type layoutCard struct {
	anim.Group
	pane  *layoutsPane
	id    string
	badge *jobBadge
	// name and holds cross-fade to new words as they change.
	name, holds         *widget.Pages
	nameText, holdsText string
	chips               *widget.Wrap
	// hosts are the chips by the machine they name, and order those
	// machines in order.
	hosts map[app.LayoutHost]*widget.Fold
	order []app.LayoutHost
	open  *widget.Button
	// rename, lines and remove are the card's other buttons, and start
	// whether it opens as kakel starts.
	rename, lines, remove *widget.Button
	start                 *widget.Switch
	// at is where each of the chips and the controls stands, gliding to
	// where the card puts it.
	at map[gunim.Node]*anim.Point
}

func newLayoutCard(p *layoutsPane, l settings.SavedLayout) *layoutCard {
	c := &layoutCard{
		pane: p, id: l.ID, badge: newJobBadge(),
		open: iconButton(icon.SquareArrowOutUpRight, "Open"), rename: iconButton(icon.Pencil, "Rename"),
		lines: iconButton(icon.ListStart, "Start Lines…"), remove: iconButton(icon.Trash2, "Remove"),
		start: widget.NewSwitch("Open at start"), hosts: map[app.LayoutHost]*widget.Fold{}, at: map[gunim.Node]*anim.Point{},
	}
	c.badge.icon.Icon, c.badge.icon.Color = icon.LayoutPanelLeft, widget.Accent
	c.nameText, c.holdsText = l.Name, cardHolds(l)
	c.name, c.holds = widget.NewPages(nameLabel(c.nameText)), widget.NewPages(holdsLabel(c.holdsText))
	c.open.Kind = widget.ButtonPrimary
	c.rename.Ghost, c.lines.Ghost, c.remove.Ghost = true, true, true
	id := l.ID
	c.open.OnClick = widget.Sends(app.OpenLayout{ID: id})
	c.rename.OnClick = func(u *gunim.UI) gunim.Intent {
		p.lastID, p.lastCtl = id, ctlRename
		for _, now := range p.w.layouts {
			if now.ID == id {
				p.w.renameLayoutDialog(now, u)
			}
		}
		return nil
	}
	c.lines.OnClick = func(u *gunim.UI) gunim.Intent {
		p.lastID, p.lastCtl = id, ctlLines
		for _, now := range p.w.layouts {
			if now.ID == id {
				p.w.startLinesDialog(now, u)
			}
		}
		return nil
	}
	c.remove.OnClick = func(*gunim.UI) gunim.Intent {
		p.lastID, p.lastCtl = id, ctlRemove
		return app.RemoveLayout{ID: id}
	}
	c.start.SetChecked(l.AtStart, nil)
	c.start.OnChange = func(on bool, _ *gunim.UI) gunim.Intent { return app.SetLayoutAtStart{ID: id, On: on} }
	var chips []gunim.Node
	for _, h := range p.w.layoutHosts(l) {
		// Open from the start, so it comes with the card, and shrinks
		// out as it goes.
		f := widget.NewFold(hostChip(h), true)
		f.Axis, f.Comes = widget.Horizontal, true
		c.hosts[h] = f
		c.order = append(c.order, h)
		chips = append(chips, f)
	}
	c.chips = widget.NewWrap(chips...)
	c.chips.Gap = itemGap
	return c
}

// cardHolds is what a card says layout l opens, and how many start
// lines its terminals type: "4 terminals, 3 start lines".
func cardHolds(l settings.SavedLayout) string {
	holds := app.LayoutHolds(l)
	if n := app.LayoutStartLines(l); n > 0 {
		holds += ", " + words.ManyOf(n, "start line", "start lines")
	}
	return holds
}

// nameLabel and holdsLabel are a card's name, and what it opens, as the
// card shows them.
func nameLabel(s string) *widget.Label {
	l := widget.NewLabel(s)
	l.Face = widget.BoldFont
	return l
}

func holdsLabel(s string) *widget.Label {
	l := widget.NewLabel(s)
	l.Size, l.Color = smallText, look.Faint
	return l
}

// hostChip is a chip naming a machine a layout opens panes on.
func hostChip(h app.LayoutHost) *itemChip {
	chip := newItemChip(h.Name)
	chip.mark.Icon, chip.mark.Color = icon.Server, look.Faint
	switch {
	case h.Here:
		chip.mark.Icon = icon.Laptop
	case h.Window:
		// As the Machines pane and the launcher mark a kakel window.
		chip.mark.Icon = icon.AppWindow
	}
	return chip
}

// control is the card's control i, one of ctlOpen and on.
func (c *layoutCard) control(i int) gunim.Node {
	return [ctlCount]gunim.Node{c.open, c.rename, c.lines, c.remove, c.start}[min(max(i, 0), ctlCount-1)]
}

// show brings the card up to date with l, in place: new words fade in
// over the old, a machine's chip grows in or shrinks out, and the switch
// slides.
func (c *layoutCard) show(l settings.SavedLayout, u *gunim.UI) {
	if l.Name != c.nameText {
		c.nameText = l.Name
		c.name.Show(nameLabel(l.Name), geom.Point{}, u)
	}
	if holds := cardHolds(l); holds != c.holdsText {
		c.holdsText = holds
		c.holds.Show(holdsLabel(holds), geom.Point{}, u)
	}
	hosts := c.pane.w.layoutHosts(l)
	for _, h := range c.order {
		if !slices.Contains(hosts, h) {
			u.Remove(c.hosts[h])
			delete(c.hosts, h)
		}
	}
	at := 0
	for _, h := range hosts {
		if _, ok := c.hosts[h]; !ok {
			f := widget.NewFold(hostChip(h), false)
			f.Axis, f.Comes = widget.Horizontal, true
			c.hosts[h] = f
			u.InsertAt(c.chips, at, f)
		}
		at++
	}
	c.order = hosts
	c.start.SetChecked(l.AtStart, u)
}

// Access implements [gunim.Accessible]: the card is an item of the list
// named for its layout, so its controls are read with the layout they
// work on.
func (c *layoutCard) Access() access.Info {
	return access.Info{Role: access.RoleListItem, Name: c.nameText, Description: c.holdsText}
}

// Children implements [gunim.Composite]: the words, then the controls in
// the order they are read.
func (c *layoutCard) Children() []gunim.Node {
	return []gunim.Node{c.badge, c.name, c.holds, c.chips, c.open, c.rename, c.lines, c.remove, c.start}
}

// place puts child k at pt, at once the first time and gliding after.
func (c *layoutCard) place(k gunim.Child, pt geom.Point, f gunim.Frame) geom.Point {
	a, ok := c.at[k.Node()]
	if !ok {
		a = anim.NewPoint(pt)
		c.at[k.Node()] = a
		c.Add(a)
	} else if a.Target() != pt {
		a.Animate(pt, widget.Settle.Get(f.Theme))
	}
	k.Place(a.Value())
	return a.Value()
}

// Layout implements [gunim.Node]: the badge with the words beside it,
// then the chips, then the controls in a row that runs on to another
// line where the card is too narrow for them all. The card is as tall
// as where its controls stand, so it grows and shrinks as they glide.
func (c *layoutCard) Layout(cs gunim.Constraints, f gunim.Frame, kids gunim.Children) geom.Size {
	const pad, gap, badgeGap, ctlGap, rowGap, switchGap = 16, 12, 14, 6, 8, 12
	w := cs.Max.W
	badge, name, holds, chips := kids.At(0), kids.At(1), kids.At(2), kids.At(3)
	bs := badge.Layout(gunim.Constraints{})
	left := float32(pad + bs.W + badgeGap)
	inner := max(0, w-left-pad)
	ns := name.Layout(gunim.Constraints{Max: geom.Sz(inner, 0)})
	hs := holds.Layout(gunim.Constraints{Max: geom.Sz(inner, 0)})
	text := ns.H + 3 + hs.H
	top := max(bs.H, text)
	badge.Place(geom.Pt(pad, pad+(top-bs.H)/2))
	name.Place(geom.Pt(left, pad+(top-text)/2))
	holds.Place(geom.Pt(left, pad+(top-text)/2+ns.H+3))
	y := pad + top + gap
	if s := chips.Layout(gunim.Constraints{Max: geom.Sz(inner, 0)}); s.H > 0 {
		c.place(chips, geom.Pt(left, y), f)
		y += s.H + gap
	}
	// The controls in lines, then each line's controls on its middle.
	type placed struct {
		k    gunim.Child
		s    geom.Size
		x    float32
		line int
	}
	var ctls []placed
	var lines []float32
	x := left
	for i := 4; i < kids.Len(); i++ {
		k := kids.At(i)
		s := k.Layout(gunim.Constraints{Max: geom.Sz(inner, 40)})
		if k.Node() == gunim.Node(c.start) && x > left {
			x += switchGap - ctlGap
		}
		if len(lines) == 0 || x > left && x+s.W > left+inner {
			x = left
			lines = append(lines, 0)
		}
		lines[len(lines)-1] = max(lines[len(lines)-1], s.H)
		ctls = append(ctls, placed{k, s, x, len(lines) - 1})
		x += s.W + ctlGap
	}
	tops := make([]float32, len(lines))
	for i := range lines {
		tops[i] = y
		y += lines[i] + rowGap
	}
	bottom := float32(0)
	for _, p := range ctls {
		at := c.place(p.k, geom.Pt(p.x, tops[p.line]+(lines[p.line]-p.s.H)/2), f)
		bottom = max(bottom, at.Y+p.s.H)
	}
	return geom.Sz(w, bottom+pad)
}

// Paint implements [gunim.Node].
func (c *layoutCard) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	p.RRect(geom.Rect{Max: box.Point()}, widget.CardRadius.Get(f.Theme), paint.Solid(widget.CardFill.Get(f.Theme)))
	for k := range kids.All {
		k.Paint(p)
	}
}
