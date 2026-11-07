package view

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
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

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/input"
	"github.com/marrasen/kakel/keys"
	"github.com/marrasen/kakel/look"
	"github.com/marrasen/kakel/ui"
	"github.com/marrasen/kakel/winkeys"
	"github.com/marrasen/kakel/words"
)

// The shortcut editor: a heading and a search over the groups of
// commands, and the commands of the group picked. A row is quiet, its
// name and its keys, until the pointer or the keyboard comes to it. Then
// it shows the rest: a cross on each key, a button to add one, and the
// way back to what kakel comes with. The help is this pane. A change is
// written to the shortcuts file at once, and comes back through the
// state as the file being read again does.

// The editor's looks.
var (
	shortcutPadX       = theme.Length("kakel.shortcuts.pad", 20)
	shortcutHeadPad    = theme.Insets("kakel.shortcuts.head", geom.Insets{Left: 20, Right: 20, Top: 28, Bottom: 6})
	shortcutTopPad     = theme.Insets("kakel.shortcuts.top", geom.Insets{Left: 20, Right: 20, Top: 16, Bottom: 14})
	shortcutBannerPad  = theme.Insets("kakel.shortcuts.banner", geom.Insets{Left: 20, Right: 20, Top: 4, Bottom: 10})
	shortcutMsgPad     = theme.Insets("kakel.shortcuts.message", geom.Insets{Left: 0, Right: 20, Top: 0, Bottom: 10})
	shortcutSidePad    = theme.Insets("kakel.shortcuts.side", geom.Insets{Left: 12, Right: 12, Top: 19, Bottom: 12})
	shortcutItemPad    = theme.Insets("kakel.shortcuts.item", geom.Insets{Left: 8, Right: 10, Top: 6, Bottom: 6})
	shortcutRowGap     = theme.Length("kakel.shortcuts.rowgap", 0)
	shortcutRowH       = theme.Length("kakel.shortcuts.rowh", 40)
	shortcutGutter     = theme.Length("kakel.shortcuts.gutter", 14)
	shortcutSlot       = theme.Length("kakel.shortcuts.slot", 30)
	shortcutColGap     = theme.Length("kakel.shortcuts.colgap", 16)
	shortcutChipGap    = theme.Length("kakel.shortcuts.gap", 6)
	shortcutRadius     = theme.Length("kakel.shortcuts.radius", 8)
	shortcutInset      = theme.Length("kakel.shortcuts.inset", 8)
	shortcutSideW      = theme.Length("kakel.shortcuts.sidew", 190)
	shortcutSearchGive = theme.Length("kakel.shortcuts.searchgive", 420)
	shortcutSearchW    = theme.Length("kakel.shortcuts.searchw", 300)
	shortcutWideFrom   = theme.Length("kakel.shortcuts.widefrom", 760)
	shortcutNameW      = theme.Length("kakel.shortcuts.namew", 280)
	shortcutBand       = theme.Length("kakel.shortcuts.band", 0.13)
	shortcutHoverBand  = theme.Length("kakel.shortcuts.hoverband", 0.03)
	shortcutRule       = theme.Length("kakel.shortcuts.rule", 0.05)
	shortcutDotW       = theme.Length("kakel.shortcuts.dotw", 7)
	shortcutMsgGap     = theme.Length("kakel.shortcuts.msggap", 14)
	shortcutHeadSize   = theme.Length("kakel.shortcuts.headsize", 11)
	shortcutRuleH      = theme.Length("kakel.shortcuts.ruleh", 13)
	shortcutFlyBlur    = theme.Length("kakel.shortcuts.flyblur", 14)
	shortcutLosing     = theme.Length("kakel.shortcuts.losing", 0.35)
	shortcutBar        = theme.Length("kakel.shortcuts.bar", 2)
)

// lineKind says what a line of the editor is.
type lineKind uint8

const (
	lineHead     lineKind = iota // a group's heading
	lineCommand                  // a command, whose shortcuts can change
	lineLauncher                 // the launcher's key, kept in the settings
	lineFixed                    // a key that cannot change, as the reader's are
)

// The groups the sidebar has besides the menus'.
const (
	groupAll     = widget.Key("all")
	groupChanged = widget.Key("changed")
	groupRule    = widget.Key("rule")
)

// helpLine is one line of the editor.
type helpLine struct {
	key   widget.Key
	kind  lineKind
	title string
	// id is the command's name in the shortcuts file.
	id string
	// note says more about a line that has no id.
	note string
	// shown is what a fixed line says its key is.
	shown string
	// also are other words the line is found by.
	also []string
	// group is the heading the line is under.
	group widget.Key
}

// shortcutsPane is the shortcut editor.
type shortcutsPane struct {
	anim.Group
	w      *Window
	search *widget.TextField
	reset  *widget.Button
	// groups is the sidebar's choice as a list that drops down, for a
	// pane too narrow to have the sidebar.
	groups     *widget.Dropdown
	groupsFold *widget.Fold
	subFold    *widget.Fold
	searchAt   *widget.Sized
	resetFade  *rowFade
	u          *gunim.UI
	// losing is the chip a question on another row would take away, dim
	// while it is asked.
	losing *rowChip
	banner *widget.Fold
	why    *widget.Label
	top    gunim.Node
	side   *widget.List
	sideBy gunim.Node
	list   *widget.VirtualList
	under  *shortcutList
	main   *widget.Flex
	fly    *flyingChord
	// sideOpen is how far the sidebar is out, gliding away as the pane
	// gets narrow.
	sideOpen *anim.Float
	sideLaid bool
	sideX    float32

	lines map[widget.Key]*helpLine
	// order is every line, and showing those the filter leaves.
	order, showing []widget.Key
	// group is the sidebar's choice.
	group widget.Key
	// blocked says the shortcuts file can't be changed now.
	blocked bool
	// flight is a shortcut on its way from one row to another.
	flight *chordFlight
	// said is what to say when the file has the change sent at saidAt, its
	// count of reads then, and undoLauncher the launcher's key to go back to.
	said         string
	saidAt       uint64
	undoLauncher string
	// goingTo is a row waiting to be built, to take the keyboard.
	goingTo widget.Key
}

// chordFlight is a shortcut moved onto another row: from where it stood,
// on the row it left, to the row it is going to.
type chordFlight struct {
	chord ui.Chord
	from  geom.Rect
	to    widget.Key
	// launched says it is on its way.
	launched bool
}

func newShortcutsPane(w *Window) *shortcutsPane {
	p := &shortcutsPane{w: w, lines: map[widget.Key]*helpLine{}, group: groupAll, sideOpen: anim.NewFloat(1)}
	p.Add(p.sideOpen)
	p.search = widget.NewTextField()
	p.search.Placeholder = "Search commands or keys"
	p.search.Icon = icon.Search
	p.search.Clearable = true
	p.search.OnChange = func(_ string, u *gunim.UI) gunim.Intent { p.filter(u); return nil }
	p.reset = widget.NewButton("Reset All")
	p.reset.OnClick = func(u *gunim.UI) gunim.Intent {
		// The launcher's key is kept apart from the file, and goes back with it.
		p.undoLauncher = ""
		if p.changed(p.lines[p.launcherLine()]) {
			p.undoLauncher = cmp.Or(p.w.launcherKey, "none")
			u.Send(p.w, app.SetLauncherKey{Key: "", InRow: true})
		}
		p.edit(keys.Op{ResetAll: true}, "Shortcuts reset", u)
		return nil
	}
	sub := faintLabel("Hover a row to change its keys. Changes save at once.")
	sub.MaxLines = 1
	p.subFold = widget.NewFold(sub, true)
	titles := widget.Column(p.subFold)
	titles.Cross = widget.CrossStretch
	p.groups = widget.NewDropdown(nil)
	p.groups.OnChange = func(i int, u *gunim.UI) gunim.Intent {
		if items := p.groupItems(); i < len(items) {
			p.pick(items[i].key, u)
		}
		return nil
	}
	p.groupsFold = widget.NewFold(p.groups, false)
	p.groupsFold.Axis = widget.Horizontal
	p.searchAt = widget.NewSized(p.search, shortcutSearchW.Default(), 0)
	p.resetFade = newRowFade(p.reset, false)
	top := widget.Row(titles, p.groupsFold, p.resetFade, p.searchAt).Grow(titles, 1)
	top.Cross = widget.CrossCenter
	topPad := widget.NewPad(top)
	topPad.Padding = shortcutTopPad
	p.top = topPad

	again := widget.NewButton("Read Again")
	again.OnClick = widget.Sends(app.ReloadShortcuts{})
	p.why = widget.NewLabel("")
	p.why.Color = widget.ToastErrorInk
	bannerRow := widget.Row(p.why, again).Grow(p.why, 1)
	bannerRow.Cross = widget.CrossCenter
	bannerPad := widget.NewPad(bannerRow)
	bannerPad.Padding = shortcutBannerPad
	p.banner = widget.NewFold(bannerPad, false)

	p.side = widget.NewList()
	p.side.OnActivate = func(k widget.Key, u *gunim.UI) gunim.Intent {
		if k == groupRule {
			return nil
		}
		p.pick(k, u)
		return nil
	}
	sidePad := widget.NewPad(widget.NewScroll(p.side))
	sidePad.Padding = shortcutSidePad
	p.sideBy = sidePad

	p.list = widget.NewVirtualList(p.buildLine)
	p.list.Estimate = 40
	p.list.Spacing = shortcutRowGap
	p.under = newShortcutList(p)
	p.main = widget.Column(p.banner, p.under).Grow(p.under, 1)
	p.main.Cross = widget.CrossStretch
	p.main.Gap = shortcutRowGap
	p.fly = newFlyingChord(p)
	p.fill()
	p.side.SetSelected(groupAll, nil)
	p.showing = p.chosen()
	p.list.SetKeys(p.showing, nil)
	return p
}

// pick shows a group, whichever way it was picked.
func (p *shortcutsPane) pick(k widget.Key, u *gunim.UI) {
	p.group = k
	p.side.SetSelected(k, u)
	p.syncSide(u)
	p.filter(u)
	for i, it := range p.groupItems() {
		if it.key == k && p.groups.Selected() != i {
			p.groups.SetSelected(i, u)
		}
	}
}

// groupItem is one choice of the sidebar.
type groupItem struct {
	key   widget.Key
	title string
	count string
}

// groupItems are the sidebar's choices: everything, what has changed, and
// each group.
func (p *shortcutsPane) groupItems() []groupItem {
	changed := 0
	for _, l := range p.lines {
		if p.changed(l) {
			changed++
		}
	}
	out := []groupItem{{key: groupAll, title: "All Commands"}, {key: groupChanged, title: "Changed"}}
	if changed > 0 {
		out[1].count = strconv.Itoa(changed)
	}
	for _, k := range p.order {
		if l := p.lines[k]; l.kind == lineHead {
			out = append(out, groupItem{key: k, title: sideTitle(l.title)})
		}
	}
	return out
}

// launcherLine is the launcher's line.
func (p *shortcutsPane) launcherLine() widget.Key {
	for _, k := range p.order {
		if p.lines[k].kind == lineLauncher {
			return k
		}
	}
	return ""
}

// groupTitles are the names of the groups.
func (p *shortcutsPane) groupTitles() []string {
	var out []string
	for _, it := range p.groupItems() {
		out = append(out, it.title)
	}
	return out
}

// sideTitle is a group's name in the sidebar, where room is short.
func sideTitle(head string) string {
	if strings.HasPrefix(head, "The reader's keys") {
		return "File Reader"
	}
	return head
}

// syncSide brings the sidebar up to date.
func (p *shortcutsPane) syncSide(u *gunim.UI) {
	// A rule between the two views of everything and the groups.
	items := p.groupItems()
	items = slices.Insert(items, 2, groupItem{key: groupRule})
	widget.Sync(p.side, u, items, func(it groupItem) widget.Key { return it.key },
		func(it groupItem) *groupRow { r := newGroupRow(it); r.pick(it.key == p.group); return r },
		func(r *groupRow, it groupItem, _ *gunim.UI) { r.set(it); r.pick(it.key == p.group) })
}

// groupRow is a line of the sidebar: a name, and how many at its end.
type groupRow struct {
	name, count *widget.Label
	// rule says it is the line between the views and the groups.
	rule  bool
	inner gunim.Node
}

func newGroupRow(it groupItem) *groupRow {
	r := &groupRow{name: widget.NewLabel(it.title), count: faintLabel(it.count), rule: it.key == groupRule}
	r.name.MaxLines = 1
	row := widget.Row(r.name, r.count).Grow(r.name, 1)
	row.Cross = widget.CrossCenter
	pad := widget.NewPad(row)
	pad.Padding = shortcutItemPad
	r.inner = pad
	return r
}

func (r *groupRow) set(it groupItem) { r.name.Text, r.count.Text = it.title, it.count }

// pick colours the name, quiet unless it is the group picked.
func (r *groupRow) pick(on bool) {
	if on {
		r.name.Color = widget.Ink
		return
	}
	r.name.Color = look.Faint
}

// Children implements [gunim.Composite].
func (r *groupRow) Children() []gunim.Node { return []gunim.Node{r.inner} }

// Layout implements [gunim.Node].
func (r *groupRow) Layout(c gunim.Constraints, f gunim.Frame, kids gunim.Children) geom.Size {
	k := kids.At(0)
	if r.rule {
		k.Layout(gunim.Tight(geom.Size{}))
		k.Place(geom.Point{})
		return geom.Sz(c.Max.W, shortcutRuleH.Get(f.Theme))
	}
	s := k.Layout(c)
	k.Place(geom.Point{})
	return geom.Sz(c.Max.W, s.H)
}

// Paint implements [gunim.Node].
func (r *groupRow) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	if r.rule {
		line := widget.Ink.Get(f.Theme)
		line.A = uint8(float32(line.A) * shortcutRule.Get(f.Theme) * 2)
		p.RRect(geom.Rc(8, box.H/2, max(box.W-16, 0), 1), 0, paint.Solid(line))
		return
	}
	kids.At(0).Paint(p)
}

// fill makes the lines: the launcher's key, then the commands as the
// menus group them, then what is bound to a saved thing, then the keys
// that cannot change.
func (p *shortcutsPane) fill() {
	clear(p.lines)
	p.order = p.order[:0]
	var group widget.Key
	add := func(l *helpLine) {
		l.key = widget.Key(fmt.Sprintf("%04d", len(p.order)))
		if l.kind == lineHead {
			group = l.key
		}
		l.group = group
		p.lines[l.key] = l
		p.order = append(p.order, l.key)
	}
	add(&helpLine{kind: lineHead, title: "Launcher"})
	add(&helpLine{kind: lineLauncher, title: "Launcher Key", note: "Works from any program", also: []string{"global", "hot key"}})
	listed := map[string]bool{}
	for _, s := range helpSections(func(string) string { return "" }) {
		add(&helpLine{kind: lineHead, title: s.title})
		for _, l := range s.lines {
			if l[2] == "" {
				add(&helpLine{kind: lineFixed, title: l[0], shown: l[1]})
				continue
			}
			listed[l[2]] = true
			add(&helpLine{kind: lineCommand, title: l[0], id: l[2], also: commandAlso[l[2]]})
		}
	}
	// Bound by a file to one thing of many, such as a saved server,
	// which no menu lists under a name of its own.
	var saved []string
	for _, b := range p.w.keys.Bindings() {
		if !listed[b.ID] && !slices.Contains(saved, b.ID) {
			saved = append(saved, b.ID)
		}
	}
	slices.Sort(saved)
	if len(saved) > 0 {
		at := len(p.order)
		add(&helpLine{kind: lineHead, title: "Saved things"})
		for _, id := range saved {
			add(&helpLine{kind: lineCommand, title: id, id: id})
		}
		// Before the keys that cannot change, which close the list.
		tail := slices.Clone(p.order[at:])
		for i := len(p.order) - 1; i >= 0; i-- {
			if l := p.lines[p.order[i]]; l.kind == lineHead && strings.HasPrefix(l.title, "The reader's keys") {
				rest := slices.Clone(p.order[i:at])
				p.order = append(append(p.order[:i:i], tail...), rest...)
				break
			}
		}
	}
}

// chordsOf says the shortcuts a line has now, as a person reads them.
func (p *shortcutsPane) chordsOf(l *helpLine) []string {
	var out []string
	for _, c := range p.chordValues(l) {
		out = append(out, chordLabel(c))
	}
	return out
}

// chordValues is the shortcuts a line has now.
func (p *shortcutsPane) chordValues(l *helpLine) []ui.Chord {
	switch l.kind {
	case lineCommand:
		return p.w.keys.ChordsFor(l.id)
	case lineLauncher:
		if c, err := ui.ParseChord(strings.TrimSpace(cmp.Or(p.w.launcherKey, app.DefaultLauncherKey))); err == nil {
			return []ui.Chord{c}
		}
	}
	return nil
}

// changed reports whether a line differs from what kakel comes with.
func (p *shortcutsPane) changed(l *helpLine) bool {
	switch l.kind {
	case lineCommand:
		return !slices.Equal(p.w.keys.ChordsFor(l.id), Shortcuts().ChordsFor(l.id))
	case lineLauncher:
		k := strings.TrimSpace(p.w.launcherKey)
		return k != "" && !strings.EqualFold(k, app.DefaultLauncherKey)
	}
	return false
}

// matches reports whether a line is one the search and the sidebar ask for.
func (p *shortcutsPane) matches(l *helpLine, want []string) bool {
	if l.kind == lineHead {
		return false
	}
	// A search looks everywhere, as the group picked is easily forgotten.
	if len(want) == 0 {
		switch p.group {
		case groupAll:
		case groupChanged:
			if !p.changed(l) {
				return false
			}
		default:
			if l.group != p.group {
				return false
			}
		}
	}
	hay := strings.ToLower(strings.Join(append(append([]string{l.title, l.id, l.shown}, p.chordsOf(l)...), l.also...), " "))
	for _, w := range want {
		if !strings.Contains(hay, w) {
			return false
		}
	}
	return true
}

// chosen is the lines asked for, with the heading of each group that
// keeps one.
func (p *shortcutsPane) chosen() []widget.Key {
	want := strings.Fields(strings.ToLower(p.search.Text()))
	var out []widget.Key
	var head widget.Key
	for _, k := range p.order {
		l := p.lines[k]
		if l.kind == lineHead {
			head = k
			continue
		}
		if !p.matches(l, want) {
			continue
		}
		if head != "" {
			out = append(out, head)
		}
		head = ""
		out = append(out, k)
	}
	return out
}

// filter shows the lines asked for: those going shrink and those coming
// grow, as the list does.
func (p *shortcutsPane) filter(u *gunim.UI) {
	p.showing = p.chosen()
	p.list.SetKeys(p.showing, u)
	if u != nil {
		u.Invalidate()
	}
}

// show brings the rows up to date with the window's keys.
func (p *shortcutsPane) show(w *Window, u *gunim.UI) {
	if p.savedCount() > 0 {
		p.fill()
	}
	problem := cmp.Or(w.shortcutsRefused, w.shortcutsProblem)
	p.why.Text = problem
	p.banner.SetOpen(problem != "", u)
	p.blocked = problem != ""
	changed := false
	for _, l := range p.lines {
		changed = changed || p.changed(l)
	}
	p.reset.Disabled = p.blocked || !changed
	// Off stage it is out of the tree, where nothing can be added to it;
	// it catches up as it comes back.
	if u == nil || u.Presence(p) == gunim.Exiting {
		return
	}
	p.u = u
	p.syncSide(u)
	p.groups.SetItems(widget.Labels(p.groupTitles()...))
	p.filter(u)
	for _, k := range p.showing {
		if r := p.rowOf(k); r != nil {
			r.sync(u)
		}
	}
	// The file has the change: what flies has set off, or never will.
	if w.shortcutsRead != p.saidAt {
		p.flight = nil
		if !p.fly.live {
			p.sayDone(u)
		}
	}
}

// focusTarget is what takes the keyboard as the pane comes to the front:
// the groups, or the search where the pane is too narrow to have them.
func (p *shortcutsPane) focusTarget() gunim.Node {
	if p.sideLaid && p.side.SkipFocus {
		return p.search
	}
	return p.side
}

// rowOf is a line's row, while it is built.
func (p *shortcutsPane) rowOf(k widget.Key) *shortcutRow {
	if n, ok := p.list.Row(k); ok {
		if r, ok := n.(*shortcutRow); ok {
			return r
		}
	}
	return nil
}

// savedCount is how many commands the keys name that the lines lack.
func (p *shortcutsPane) savedCount() int {
	known := map[string]bool{}
	for _, l := range p.lines {
		known[l.id] = true
	}
	n := 0
	for _, b := range p.w.keys.Bindings() {
		if !known[b.ID] {
			n++
		}
	}
	return n
}

// edit sends a change to the file. What it says is said once the file
// has it, so a change that fails is not called made.
func (p *shortcutsPane) edit(op keys.Op, said string, u *gunim.UI) {
	p.said, p.saidAt = said, p.w.shortcutsRead
	u.Send(p.w, app.EditShortcut{Op: op, BuiltIn: Shortcuts().Bindings()})
}

// sayDone says the change the file has now, with the way back.
func (p *shortcutsPane) sayDone(u *gunim.UI) {
	if p.said == "" {
		return
	}
	said, launcher := p.said, p.undoLauncher
	p.said, p.undoLauncher = "", ""
	p.w.toast(widget.Toast{Title: said, Key: "shortcut-editor", Action: "Undo",
		OnClick: func(u *gunim.UI) gunim.Intent {
			if launcher != "" {
				u.Send(p.w, app.SetLauncherKey{Key: launcher, InRow: true})
			}
			return app.RestoreShortcuts{}
		}}, u)
}

// buildLine makes a line's node as it comes into view.
func (p *shortcutsPane) buildLine(k widget.Key) gunim.Node {
	l := p.lines[k]
	if l == nil {
		return &emptyNode{}
	}
	if l.kind == lineHead {
		h := faintLabel(strings.ToUpper(sideTitle(l.title)))
		h.Face, h.Size = widget.BoldFont, shortcutHeadSize
		pad := widget.NewPad(h)
		pad.Padding = shortcutHeadPad
		return pad
	}
	return newShortcutRow(p, l)
}

// move takes the keyboard to the next command along, by arrow.
func (p *shortcutsPane) move(from widget.Key, by int, u *gunim.UI) bool {
	i := slices.Index(p.showing, from)
	if i < 0 {
		return false
	}
	for i += by; i >= 0 && i < len(p.showing); i += by {
		k := p.showing[i]
		if l := p.lines[k]; l.kind != lineCommand && l.kind != lineLauncher {
			continue
		}
		p.goTo(k, u)
		return true
	}
	if by < 0 {
		u.FocusFirst(p.search)
		return true
	}
	return false
}

// goTo gives a row the keyboard, which brings it into view, or scrolls to
// a row not built yet and gives it the keyboard as it comes.
func (p *shortcutsPane) goTo(k widget.Key, u *gunim.UI) {
	if r := p.rowOf(k); r != nil {
		u.FocusFirst(r.add)
		return
	}
	p.u, p.goingTo = u, k
	p.list.ScrollToKey(k, u)
}

// selectRow lights a row, and puts out the one lit.
func (p *shortcutsPane) selectRow(k widget.Key, u *gunim.UI) {
	was := p.under.selected
	if was == k {
		return
	}
	p.under.selected = k
	if r := p.rowOf(was); r != nil {
		r.refresh(u)
	}
	if r := p.rowOf(k); r != nil {
		r.refresh(u)
	}
	u.Invalidate()
}

// Handle implements [gunim.Handler]: the keyboard coming into the pane
// makes it the one in front.
func (p *shortcutsPane) Handle(e gi.Event, u *gunim.UI) bool {
	if _, ok := e.(gi.FocusEntered); ok {
		p.u = u
	}
	return false
}

// KeyFirst implements [gunim.KeyFirst]: Down leaves the search for the
// rows. The field keeps every other key it is given.
func (p *shortcutsPane) KeyFirst(e gi.KeyPress, u *gunim.UI) bool {
	// Typing anywhere in the pane is a search: the pane opens without
	// the search lit, and the text follows the keyboard there.
	if e.Typed && e.Key != gi.KeySpace && !e.Mods.Has(gi.ModControl) && !e.Mods.Has(gi.ModAlt) && u.Focused() != gunim.Node(p.search) {
		u.Focus(p.search)
		return false
	}
	if e.Key != gi.KeyDown || e.Mods != 0 || u.Focused() != gunim.Node(p.search) {
		return false
	}
	for _, k := range p.showing {
		if l := p.lines[k]; l.kind == lineCommand || l.kind == lineLauncher {
			p.goTo(k, u)
			return true
		}
	}
	return false
}

// Children implements [gunim.Composite].
func (p *shortcutsPane) Children() []gunim.Node {
	return []gunim.Node{p.top, p.sideBy, p.main, p.fly}
}

// Layout implements [gunim.Node]: the heading across the top, the groups
// down the left until the pane is narrow, and the rows in what is left.
func (p *shortcutsPane) Layout(c gunim.Constraints, f gunim.Frame, kids gunim.Children) geom.Size {
	th := f.Theme
	wide := float32(0)
	if c.Max.W >= shortcutWideFrom.Get(th) {
		wide = 1
	}
	if !p.sideLaid {
		p.sideOpen.Jump(wide)
		p.sideLaid = true
	} else if p.sideOpen.Target() != wide {
		p.sideOpen.Animate(wide, widget.Settle.Get(th))
	}
	// Gone from the pane, the sidebar is gone from the Tab order too.
	p.side.SkipFocus = wide == 0
	// The keyboard doesn't stay on what has gone, as when the pane opens
	// narrow or narrows with the keyboard there.
	if wide == 0 && p.u != nil && p.u.Focused() == gunim.Node(p.side) {
		p.u.FocusFirstLaidOut(p.search)
	}
	if p.groupsFold.Open() != (wide == 0) {
		p.groupsFold.SetOpen(wide == 0, p.u)
		p.subFold.SetOpen(wide == 1, p.u)
	}
	if p.reset.Disabled {
		p.resetFade.setTo(0, th)
	} else {
		p.resetFade.setTo(1, th)
	}
	p.search.Placeholder = "Search commands or keys"
	if wide == 0 {
		p.search.Placeholder = "Search"
	}
	// Room for the heading, the groups and Reset All goes first.
	p.searchAt.Width = max(min(shortcutSearchW.Get(th), c.Max.W-shortcutSearchGive.Get(th)), 100)
	top := kids.At(0)
	ts := top.Layout(gunim.Constraints{Min: geom.Sz(c.Max.W, 0), Max: geom.Sz(c.Max.W, c.Max.H)})
	top.Place(geom.Point{})
	sideW := shortcutSideW.Get(th)
	p.sideX = sideW * p.sideOpen.Value()
	h := max(c.Max.H-ts.H, 0)
	side := kids.At(1)
	side.Layout(gunim.Tight(geom.Sz(sideW, h)))
	side.Place(geom.Pt(p.sideX-sideW, ts.H))
	main := kids.At(2)
	main.Layout(gunim.Tight(geom.Sz(max(c.Max.W-p.sideX, 0), h)))
	main.Place(geom.Pt(p.sideX, ts.H))
	fly := kids.At(3)
	fly.Layout(gunim.Constraints{Max: c.Max})
	fly.Place(p.fly.place(p))
	return c.Max
}

// Paint implements [gunim.Node].
func (p *shortcutsPane) Paint(pt *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	pt.RRect(geom.Rect{Max: box.Point()}, 0, paint.Solid(widget.Background.Get(f.Theme)))
	kids.At(0).Paint(pt)
	// The groups glide out from behind the pane's edge, and draw nothing past it.
	if p.sideX > 0.5 {
		end := pt.Layer(paint.LayerOpts{Bounds: geom.Rc(0, 0, p.sideX, box.H), Clip: true, Opacity: 1})
		kids.At(1).Paint(pt)
		end()
	}
	kids.At(2).Paint(pt)
	// A line between the groups and the rows.
	if p.sideX > 1 {
		line := widget.Ink.Get(f.Theme)
		line.A = uint8(float32(line.A) * shortcutRule.Get(f.Theme) * 2)
		y := kids.At(0).Size().H
		pt.RRect(geom.Rc(p.sideX-0.5, y, 1, max(box.H-y, 0)), 0, paint.Solid(line))
	}
	p.fly.aim()
	kids.At(3).Paint(pt)
}

// shortcutList is the rows, with the light of the selected row gliding
// behind them. Its place is kept along the whole list rather than in
// view, so it scrolls with the rows.
type shortcutList struct {
	anim.Group
	p        *shortcutsPane
	selected widget.Key
	lit      widget.Key
	y, h, a  *anim.Float
	// tone is how far the light has warmed to the accent, as the row
	// waits on the user.
	tone *anim.Float
}

func newShortcutList(p *shortcutsPane) *shortcutList {
	l := &shortcutList{p: p, y: anim.NewFloat(0), h: anim.NewFloat(0), a: anim.NewFloat(0), tone: anim.NewFloat(0)}
	l.Add(l.y, l.h, l.a, l.tone)
	return l
}

// Children implements [gunim.Composite].
func (l *shortcutList) Children() []gunim.Node { return []gunim.Node{l.p.list} }

// Layout implements [gunim.Node].
func (l *shortcutList) Layout(c gunim.Constraints, f gunim.Frame, kids gunim.Children) geom.Size {
	k := kids.At(0)
	k.Layout(gunim.Tight(c.Max))
	k.Place(geom.Point{})
	if k := l.p.goingTo; k != "" && l.p.u != nil {
		if r := l.p.rowOf(k); r != nil {
			l.p.goingTo = ""
			l.p.u.FocusFirstLaidOut(r.add)
		}
	}
	top, h, ok := float32(0), float32(0), false
	if l.selected != "" {
		top, h, ok = l.p.list.RowRect(l.selected)
	}
	if !ok {
		l.a.Animate(0, widget.Settle.Get(f.Theme))
		return c.Max
	}
	at := top + l.p.list.Offset()
	motion := widget.Quick.Get(f.Theme)
	switch {
	case l.a.Target() == 0 && l.a.Value() < 0.01:
		l.y.Jump(at)
		l.h.Jump(h)
	case l.lit != l.selected || l.y.Active() || l.h.Active():
		l.y.Animate(at, motion)
		l.h.Animate(h, motion)
	default:
		// The row itself is moving, as a message opens above it.
		l.y.Jump(at)
		l.h.Jump(h)
	}
	l.lit = l.selected
	l.a.Animate(1, motion)
	to := float32(0)
	if r := l.p.rowOf(l.selected); r != nil && (r.listening != nil || r.doing != nil) {
		to = 1
	}
	if l.tone.Target() != to {
		l.tone.Animate(to, motion)
	}
	return c.Max
}

// Paint implements [gunim.Node].
func (l *shortcutList) Paint(pt *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	if a := l.a.Value(); a > 0.01 {
		end := pt.Layer(paint.LayerOpts{Bounds: geom.Rect{Max: box.Point()}, Clip: true, Opacity: min(a, 1)})
		in := shortcutInset.Get(f.Theme)
		r := geom.Rc(in, l.y.Value()-l.p.list.Offset(), max(box.W-2*in, 0), l.h.Value())
		th := f.Theme
		quiet := widget.Ink.Get(th)
		quiet.A = uint8(float32(quiet.A) * shortcutBand.Get(th))
		fill := anim.Mix(anim.ColorCodec, quiet, widget.TableCursor.Get(th), min(max(l.tone.Value(), 0), 1))
		pt.RRect(r, shortcutRadius.Get(th), paint.Solid(fill))
		bar := shortcutBar.Get(th)
		pt.RRect(geom.Rc(r.Min.X+3, r.Min.Y+10, bar, max(r.Size().H-20, 0)), bar/2, paint.Solid(widget.Accent.Get(th)))
		end()
	}
	kids.At(0).Paint(pt)
}

// rowFade shows its child or fades it away, keeping its place in the
// grid. It still takes the pointer and the keyboard while faded: a row's
// controls show as either comes to them.
type rowFade struct {
	anim.Group
	child gunim.Node
	t     *anim.Float
}

func newRowFade(child gunim.Node, on bool) *rowFade {
	f := &rowFade{child: child, t: anim.NewFloat(0)}
	if on {
		f.t.Jump(1)
	}
	f.Add(f.t)
	return f
}

// set fades it in or out.
func (f *rowFade) set(on bool, th *theme.Live) {
	to := float32(0)
	if on {
		to = 1
	}
	if f.t.Target() != to {
		f.t.Animate(to, widget.Quick.Get(th))
	}
}

// setTo fades it to a strength of its own.
func (f *rowFade) setTo(v float32, th *theme.Live) {
	if f.t.Target() != v {
		f.t.Animate(v, widget.Quick.Get(th))
	}
}

// Children implements [gunim.Composite].
func (f *rowFade) Children() []gunim.Node { return []gunim.Node{f.child} }

// Layout implements [gunim.Node].
func (f *rowFade) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	k := kids.At(0)
	s := k.Layout(c)
	k.Place(geom.Point{})
	return s
}

// Paint implements [gunim.Node].
func (f *rowFade) Paint(p *paint.Painter, _ gunim.Frame, box geom.Size, kids gunim.Children) {
	t := min(max(f.t.Value(), 0), 1)
	if t < 0.01 {
		return
	}
	if t > 0.99 {
		kids.At(0).Paint(p)
		return
	}
	defer p.Layer(paint.LayerOpts{Bounds: geom.Rect{Max: box.Point()}.Inset(geom.Uniform(-4)), Opacity: t})()
	kids.At(0).Paint(p)
}

// changedDot marks a line changed from what kakel comes with.
type changedDot struct{ box geom.Size }

// Layout implements [gunim.Node].
func (n *changedDot) Layout(c gunim.Constraints, f gunim.Frame, _ gunim.Children) geom.Size {
	d := shortcutDotW.Get(f.Theme)
	n.box = c.Constrain(geom.Sz(d, d))
	return n.box
}

// Paint implements [gunim.Node].
func (*changedDot) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, _ gunim.Children) {
	p.RRect(geom.Rect{Max: box.Point()}, box.W/2, paint.Solid(widget.Accent.Get(f.Theme)))
}

// shortcutRow is a line's row, on a grid every row shares: a mark for a
// changed line, its name, its keys from one column, and a fixed place
// each for adding a key and going back. Below it opens a message.
type shortcutRow struct {
	anim.Group
	p    *shortcutsPane
	line *helpLine

	dot      *rowFade
	name     *widget.Label
	keysRow  *widget.Flex
	addFade  *rowFade
	backFade *rowFade
	hintFade *rowFade
	dashFade *rowFade
	keysX    float32
	add      *widget.IconButton
	back     *widget.IconButton

	chips      map[ui.Chord]*rowChip
	listening  *widget.ChordInput
	listenFold *widget.Fold
	pending    *widget.Fold
	pendingVis *rowFade

	msg      *widget.Fold
	msgFade  *rowFade
	msgIcon  *widget.Icon
	said     *widget.Label
	act      *widget.Button
	cancel   *widget.Button
	choice   *widget.Fold
	choiceAt *widget.Sized
	doing    func(u *gunim.UI)

	hovered bool
	hover   *anim.Float
	// addAt is where the add button is, gliding, and addW the width of the
	// row it was placed for.
	addAt   *anim.Float
	addLaid bool
	addW    float32
	tries   uint64
}

// rowChip is one of a row's shortcuts.
type rowChip struct {
	chip *widget.Chip
	vis  *rowFade
	fold *widget.Fold
}

func newShortcutRow(p *shortcutsPane, l *helpLine) *shortcutRow {
	r := &shortcutRow{p: p, line: l, chips: map[ui.Chord]*rowChip{}, tries: p.w.launcherTries, hover: anim.NewFloat(0), addAt: anim.NewFloat(0)}
	r.Add(r.hover, r.addAt)
	r.name = widget.NewLabel(l.title)
	r.name.MaxLines = 1
	r.dot = newRowFade(&changedDot{}, p.changed(l))

	// What the line is called in the file, or more about it, is in its
	// tooltip: most people never need it, and the search still finds it.
	r.add = widget.NewIconButton(icon.Plus, strings.TrimSuffix("Add a shortcut. "+cmp.Or(l.id, l.note), " "))
	r.add.OnClick = func(u *gunim.UI) gunim.Intent { r.listen(u); return nil }
	r.addFade = newRowFade(r.add, false)
	r.back = widget.NewIconButton(icon.RotateCcw, "Back to what kakel comes with")
	r.back.OnClick = func(u *gunim.UI) gunim.Intent { r.resetLine(u); return nil }
	r.backFade = newRowFade(r.back, false)
	r.hintFade = newRowFade(faintLabel("Add Shortcut"), false)
	r.dashFade = newRowFade(faintLabel("—"), l.kind != lineFixed && len(p.chordValues(l)) == 0)

	var kids []gunim.Node
	if l.kind == lineFixed {
		r.add.Disabled, r.back.Disabled = true, true
		kids = append(kids, faintLabel(l.shown))
	}
	for _, c := range p.chordValues(l) {
		rc := r.newChip(c, true)
		r.chips[c] = rc
		kids = append(kids, rc.fold)
	}
	r.keysRow = widget.Row(kids...)
	r.keysRow.Cross = widget.CrossCenter
	r.keysRow.Gap = shortcutChipGap

	r.msgIcon = widget.NewIcon(icon.Info, "")
	r.said = widget.NewLabel("")
	r.act = widget.NewButton("Move Here")
	r.act.Kind = widget.ButtonPrimary
	r.act.OnClick = func(u *gunim.UI) gunim.Intent {
		if do := r.doing; do != nil {
			r.doing = nil
			do(u)
		}
		return nil
	}
	r.cancel = widget.NewButton("Cancel")
	r.cancel.Ghost = true
	r.cancel.OnClick = func(u *gunim.UI) gunim.Intent { r.closeMessage(u); u.FocusFirst(r.add); return nil }
	acts := widget.Row(r.act, r.cancel)
	acts.Gap = shortcutChipGap
	r.choice = widget.NewFold(acts, false)
	r.choice.Axis = widget.Horizontal
	r.choiceAt = widget.NewSized(&emptyNode{}, 0, 1)
	msgRow := widget.Row(r.choiceAt, r.msgIcon, r.said, r.choice)
	msgRow.Gap = shortcutMsgGap
	msgRow.Cross = widget.CrossCenter
	pad := widget.NewPad(msgRow)
	pad.Padding = shortcutMsgPad
	r.msgFade = newRowFade(pad, false)
	r.msg = widget.NewFold(r.msgFade, false)
	r.refresh(nil)
	return r
}

// newChip makes a shortcut's chip, open at once for a row just built and
// growing in for a shortcut just added.
func (r *shortcutRow) newChip(c ui.Chord, open bool) *rowChip {
	chip := keyChip(c)
	chip.OnRemove = func(u *gunim.UI) gunim.Intent { r.remove(c, u); return nil }
	chip.SetCross(r.lit(), nil)
	vis := newRowFade(chip, true)
	f := widget.NewFold(vis, open)
	f.Axis, f.Comes = widget.Horizontal, true
	return &rowChip{chip: chip, vis: vis, fold: f}
}

// keyChip is a chip showing a chord as the keys pressed for it.
func keyChip(c ui.Chord) *widget.Chip {
	label := chordLabel(c)
	chip := widget.NewChip("", label)
	chip.Keys = strings.Split(label, "+")
	return chip
}

// lit reports whether the row shows its controls.
func (r *shortcutRow) lit() bool {
	if r.line.kind == lineFixed {
		return false
	}
	return r.hovered || r.p.under.selected == r.line.key || r.listening != nil || r.msg != nil && r.msg.Open()
}

// refresh brings what depends on the row being lit up to date. Chips set
// their own motion from u; the rest take it from the next layout.
func (r *shortcutRow) refresh(u *gunim.UI) {
	lit := r.lit()
	for _, rc := range r.chips {
		rc.chip.SetCross(lit, u)
		rc.chip.Disabled = r.p.blocked && r.line.kind == lineCommand
	}
	off := r.p.blocked && r.line.kind == lineCommand
	if r.line.kind != lineFixed {
		r.add.Disabled, r.back.Disabled = off, off
	}
	if u != nil {
		u.Invalidate()
	}
}

// sync brings the chips and marks up to date, each change moving.
func (r *shortcutRow) sync(u *gunim.UI) {
	want := r.p.chordValues(r.line)
	f := r.p.flight
	for c, rc := range r.chips {
		if slices.Contains(want, c) {
			continue
		}
		// One chip at a time: what flies is the chip, so it has left.
		if f != nil && f.chord == c {
			rc.vis.t.Jump(0)
		}
		u.Remove(rc.fold)
		delete(r.chips, c)
	}
	for i, c := range want {
		if _, had := r.chips[c]; had {
			continue
		}
		rc := r.newChip(c, false)
		r.chips[c] = rc
		u.InsertAt(r.keysRow, i, rc.fold)
		if f != nil && f.chord == c && f.to == r.line.key && !f.launched {
			f.launched = true
			rc.vis.t.Jump(0)
			// Said once the keys have landed, so they have the moment.
			r.p.fly.launch(c, f.from, rc.fold, func() {
				rc.vis.t.Jump(1)
				r.p.sayDone(u)
			}, u)
		}
	}
	r.refresh(u)
	if r.line.kind == lineLauncher && r.tries != r.p.w.launcherTries {
		r.tries = r.p.w.launcherTries
		if r.p.w.launcherProblem != "" {
			r.refuse(r.p.w.launcherProblem, u)
		} else {
			r.closeMessage(u)
		}
	}
}

// listen starts reading a shortcut for the row.
func (r *shortcutRow) listen(u *gunim.UI) {
	if r.listening != nil {
		u.FocusFirst(r.listening)
		return
	}
	r.closeMessage(u)
	c := widget.NewChordInput("Press the keys. Esc cancels.")
	c.OnCommit = func(k gi.KeyPress, u *gunim.UI) gunim.Intent { r.pressed(k, u); return nil }
	c.OnDismiss = func(u *gunim.UI) gunim.Intent { r.stopListening(u); return nil }
	f := widget.NewFold(c, false)
	f.Axis, f.Comes = widget.Horizontal, true
	r.listening, r.listenFold = c, f
	u.InsertAt(r.keysRow, len(r.chips), f)
	u.FocusFirstLaidOut(c)
	r.refresh(u)
}

// stopListening ends the reading, and brings the keyboard back to the
// row when it was on the input.
func (r *shortcutRow) stopListening(u *gunim.UI) {
	if r.listening == nil {
		return
	}
	mine := u.Focused() == gunim.Node(r.listening)
	r.closeMessage(u)
	u.Remove(r.listenFold)
	r.listening, r.listenFold = nil, nil
	if mine {
		u.Focus(r.add)
	}
	r.refresh(u)
}

// pressed judges a press: refused says why and goes on listening;
// anything that asks first shows the key on the row and asks.
func (r *shortcutRow) pressed(k gi.KeyPress, u *gunim.UI) {
	v := r.p.judge(r.line, k)
	switch {
	case v.refused != "":
		r.listening.Listen(u)
		r.refuse(v.refused, u)
	case v.same:
		r.stopListening(u)
	case v.ask != "":
		c := v.chord
		r.stopListening(u)
		r.showPending(c, u)
		r.p.dimLosing(c, v.taken, u)
		r.ask(v.ask, v.button, func(u *gunim.UI) {
			// What flies here takes its place; the two don't show together.
			r.pendingVis.t.Jump(0)
			r.closeMessage(u)
			r.bind(c, v.taken, u)
			u.FocusFirst(r.add)
		}, u)
		u.FocusFirstLaidOut(r.act)
	default:
		r.stopListening(u)
		r.bind(v.chord, "", u)
	}
}

// showPending shows a key not yet taken on the row, as a chip that can't
// be removed.
func (r *shortcutRow) showPending(c ui.Chord, u *gunim.UI) {
	chip := keyChip(c)
	chip.SetCross(false, nil)
	chip.KeepFocus = true
	chip.Outline = true
	r.pendingVis = newRowFade(chip, true)
	f := widget.NewFold(r.pendingVis, false)
	f.Axis, f.Comes = widget.Horizontal, true
	r.pending = f
	u.InsertAt(r.keysRow, len(r.chips), f)
}

// bind puts the row's command on c. taken is the command the chord leaves.
func (r *shortcutRow) bind(c ui.Chord, taken string, u *gunim.UI) {
	if r.line.kind == lineLauncher {
		u.Send(r.p.w, app.SetLauncherKey{Key: c.String(), InRow: true})
		return
	}
	r.p.flight = nil
	if taken != "" {
		r.p.takeOff(c, taken, r.line.key, u)
	}
	r.p.edit(keys.Op{Chord: c, Command: r.line.id}, chordLabel(c)+" now runs "+r.line.title, u)
}

// remove takes a shortcut off the row.
func (r *shortcutRow) remove(c ui.Chord, u *gunim.UI) {
	if r.line.kind == lineLauncher {
		u.Send(r.p.w, app.SetLauncherKey{Key: "none", InRow: true})
		return
	}
	r.p.edit(keys.Op{Chord: c, Unbind: true}, chordLabel(c)+" removed", u)
}

// resetLine puts the row back to what kakel comes with.
func (r *shortcutRow) resetLine(u *gunim.UI) {
	if r.line.kind == lineLauncher {
		u.Send(r.p.w, app.SetLauncherKey{Key: "", InRow: true})
		return
	}
	r.p.edit(keys.Op{Command: r.line.id, Reset: true}, r.line.title+" reset", u)
}

// refuse opens the row's message on why a key can't be had.
func (r *shortcutRow) refuse(said string, u *gunim.UI) {
	r.msgIcon.Icon, r.msgIcon.Color = icon.CircleAlert, widget.ToastErrorInk
	r.said.Text = said
	r.doing = nil
	if r.listening != nil {
		r.listening.Refused = true
	}
	r.msgFade.setTo(1, u.Theme())
	r.choice.SetOpen(false, u)
	r.msg.SetOpen(true, u)
	r.refresh(u)
}

// ask opens the row's message on a question, with a button doing do.
func (r *shortcutRow) ask(said, button string, do func(*gunim.UI), u *gunim.UI) {
	r.msgIcon.Icon, r.msgIcon.Color = icon.Info, widget.Accent
	r.said.Text = said
	r.doing, r.act.Label = do, button
	r.msgFade.setTo(1, u.Theme())
	r.choice.SetOpen(true, u)
	r.msg.SetOpen(true, u)
	r.refresh(u)
}

// closeMessage shuts the row's message, and takes away a key waiting on it.
func (r *shortcutRow) closeMessage(u *gunim.UI) {
	r.doing = nil
	r.p.undimLosing(u)
	// The words go first, so nothing is left cut in half as the row closes.
	r.msgFade.setTo(0, u.Theme())
	r.msg.SetOpen(false, u)
	if r.pending != nil {
		u.Remove(r.pending)
		r.pending = nil
	}
	r.refresh(u)
}

// Handle implements [gunim.Handler]: the keyboard coming in lights the
// row, the pointer shows its controls, and the arrows go to the next.
func (r *shortcutRow) Handle(e gi.Event, u *gunim.UI) bool {
	if r.line.kind == lineFixed {
		return false
	}
	switch e := e.(type) {
	case gi.FocusEntered:
		r.p.selectRow(r.line.key, u)
	case gi.PointerEnter:
		r.setHover(true, u)
	case gi.PointerLeave:
		r.setHover(false, u)
	case gi.PointerDown:
		if e.Button == gi.ButtonPrimary && r.listening == nil {
			r.p.selectRow(r.line.key, u)
			u.Focus(r.add)
			// A click on "Add shortcut" does what it says.
			if len(r.chips) == 0 && e.Pos.X >= r.keysX && e.Pos.Y < shortcutRowH.Default() && r.doing == nil && !r.add.Disabled {
				r.listen(u)
			}
			return true
		}
	case gi.KeyPress:
		if e.Mods != 0 {
			return false
		}
		switch e.Key {
		case gi.KeyUp:
			return r.p.move(r.line.key, -1, u)
		case gi.KeyDown:
			return r.p.move(r.line.key, 1, u)
		case gi.KeyEscape:
			if r.msg.Open() {
				r.closeMessage(u)
				u.FocusFirst(r.add)
				return true
			}
		}
	}
	return false
}

// setHover notes the pointer coming or going.
func (r *shortcutRow) setHover(on bool, u *gunim.UI) {
	if r.hovered == on {
		return
	}
	r.hovered = on
	to := float32(0)
	if on {
		to = 1
	}
	r.hover.Animate(to, widget.Quick.Get(u.Theme()))
	r.refresh(u)
}

// Children implements [gunim.Composite].
func (r *shortcutRow) Children() []gunim.Node {
	return []gunim.Node{r.dot, r.name, r.keysRow, r.addFade, r.backFade, r.msg, r.hintFade, r.dashFade}
}

// Layout implements [gunim.Node].
func (r *shortcutRow) Layout(c gunim.Constraints, f gunim.Frame, kids gunim.Children) geom.Size {
	th := f.Theme
	w := c.Max.W
	pad, gutter, slot, gap := shortcutPadX.Get(th), shortcutGutter.Get(th), shortcutSlot.Get(th), shortcutColGap.Get(th)
	rowH := shortcutRowH.Get(th)
	nameX := pad
	// The keys start one column along, however wide the pane, so the eye
	// has no way to cross; a narrow pane gives the name less.
	keysX := nameX + min(shortcutNameW.Get(th), max(w*0.42, 80))
	r.keysX = keysX
	mid := func(k gunim.Child, x float32, s geom.Size) { k.Place(geom.Pt(x, (rowH-s.H)/2)) }

	lit := r.lit()
	changed := r.p.changed(r.line)
	free := r.line.kind != lineFixed && r.listening == nil && r.doing == nil
	empty := len(r.chips) == 0
	r.addFade.set(lit && free, th)
	r.hintFade.set(lit && free && empty, th)
	r.dashFade.set(!lit && free && empty, th)
	r.dot.set(changed, th)
	r.backFade.set(lit && changed && free, th)

	name := kids.At(1)
	ns := name.Layout(gunim.Constraints{Max: geom.Sz(max(keysX-nameX-gap-gutter, 0), rowH)})
	mid(name, nameX, ns)
	// The mark of a changed line has a place of its own, before the keys.
	dot := kids.At(0)
	ds := dot.Layout(gunim.Constraints{Max: geom.Sz(gutter, rowH)})
	mid(dot, keysX-gutter, ds)
	// One column for the keys, and the buttons at the row's end, whatever
	// the row holds. A row with no keys has its add where the keys would
	// be, with its words, and a fixed line has the room for its keys.
	addX := max(w-pad-2*slot, keysX)
	keysRoom := addX - keysX - gap
	if r.line.kind == lineFixed {
		keysRoom = w - keysX - pad
	}
	ks := kids.At(2)
	kss := ks.Layout(gunim.Constraints{Max: geom.Sz(max(keysRoom, 0), rowH)})
	mid(ks, keysX, kss)
	add := kids.At(3)
	as := add.Layout(gunim.Constraints{Max: geom.Sz(slot, rowH)})
	// Between the two places it glides. A pane changing size moves it at once.
	addTo := addX + (slot-as.W)/2
	if empty && free {
		addTo = keysX
	}
	switch {
	case !r.addLaid || r.addW != w:
		r.addAt.Jump(addTo)
		r.addLaid, r.addW = true, w
	case r.addAt.Target() != addTo:
		r.addAt.Animate(addTo, widget.Quick.Get(th))
	}
	mid(add, r.addAt.Value(), as)
	back := kids.At(4)
	bs := back.Layout(gunim.Constraints{Max: geom.Sz(slot, rowH)})
	mid(back, addX+slot+(slot-bs.W)/2, bs)
	hint := kids.At(6)
	hs := hint.Layout(gunim.Constraints{Max: geom.Sz(max(keysRoom-slot, 0), rowH)})
	mid(hint, keysX+as.W+shortcutChipGap.Get(th), hs)
	dash := kids.At(7)
	dhs := dash.Layout(gunim.Constraints{Max: geom.Sz(max(keysRoom, 0), rowH)})
	mid(dash, keysX, dhs)

	// The message starts under the keys it is about.
	r.choiceAt.Width = max(keysX-gap, 0)
	msg := kids.At(5)
	ms := msg.Layout(gunim.Constraints{Min: geom.Sz(w, 0), Max: geom.Sz(w, 0)})
	msg.Place(geom.Pt(0, rowH))
	return geom.Sz(w, rowH+ms.H)
}

// Paint implements [gunim.Node]: a tint under the pointer, then the row.
func (r *shortcutRow) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	th := f.Theme
	in := shortcutInset.Get(th)
	if t := min(max(r.hover.Value(), 0), 1); t > 0.01 && r.p.under.selected != r.line.key {
		c := widget.Ink.Get(th)
		c.A = uint8(float32(c.A) * t * shortcutHoverBand.Get(th))
		p.RRect(geom.Rc(in, 0, max(box.W-2*in, 0), box.H), shortcutRadius.Get(th), paint.Solid(c))
	}
	for i := range 8 {
		kids.At(i).Paint(p)
	}
}

// verdict is what a pressed key comes to.
type verdict struct {
	chord   ui.Chord
	same    bool
	refused string
	// ask is a question with button its yes, and taken the command the
	// chord leaves.
	ask, button, taken string
}

// judge says what pressing k on a line comes to.
func (p *shortcutsPane) judge(l *helpLine, k gi.KeyPress) verdict {
	ev, ok := winkeys.Event(k)
	if !ok {
		return verdict{refused: "kakel can't use this key. Try another."}
	}
	chord := ui.ChordOf(ev)
	v := verdict{chord: chord}
	name := chordLabel(chord)
	if err := keys.Bindable(chord); err != nil {
		v.refused = name + " alone can't be a shortcut. Hold Ctrl, Alt or Super with it, or use a function key."
		return v
	}
	if k.Typed {
		v.refused = name + " types a character on your keyboard, so it can't be a shortcut."
		return v
	}
	if l.kind == lineLauncher {
		if _, err := app.LauncherHotKey(chord.String()); err != nil {
			v.refused = words.UpperFirst(err.Error()) + "."
		}
		return v
	}
	warn := shortcutWarning(chord)
	id, bound := p.w.keys.Lookup(chord)
	switch {
	case bound && id == l.id:
		v.same = true
	case bound:
		v.ask, v.button, v.taken = name+" is taken by "+p.titleOf(id)+". Move it here?", "Move Here", id
	case warn != "":
		v.ask, v.button = name+warn+" Use it anyway?", "Use Anyway"
	}
	return v
}

// shortcutWarning says what a chord costs, after its name, or nothing.
func shortcutWarning(c ui.Chord) string {
	const programs = " won't reach programs in your panes any more."
	k := c.Key
	letter := k >= input.KeyA && k <= input.KeyZ
	digit := k >= input.Key1 && k <= input.Key9 || k == input.Key0
	moves := false
	switch k {
	case input.KeyUp, input.KeyDown, input.KeyLeft, input.KeyRight, input.KeyHome, input.KeyEnd,
		input.KeyInsert, input.KeyDelete, input.KeyPageUp, input.KeyPageDown:
		moves = true
	}
	switch {
	case c.Mods == input.ModCtrl|input.ModAlt:
		return " can stop a character being typed on keyboards with AltGr."
	case moves && (c.Mods == 0 || c.Mods == input.ModShift || c.Mods == input.ModCtrl || c.Mods == input.ModAlt):
		return programs
	case letter && (c.Mods == input.ModCtrl || c.Mods == input.ModAlt):
		return programs
	case digit && c.Mods == input.ModAlt:
		return programs
	}
	return ""
}

// titleOf is a command's title.
func (p *shortcutsPane) titleOf(id string) string {
	for _, l := range p.lines {
		if l.id == id {
			return l.title
		}
	}
	return id
}

// dimLosing dims the chip a question would take away.
func (p *shortcutsPane) dimLosing(c ui.Chord, id string, u *gunim.UI) {
	if id == "" {
		return
	}
	for _, l := range p.lines {
		if l.id != id {
			continue
		}
		if r := p.rowOf(l.key); r != nil {
			if rc := r.chips[c]; rc != nil {
				rc.vis.setTo(shortcutLosing.Get(u.Theme()), u.Theme())
				p.losing = rc
				u.Invalidate()
			}
		}
	}
}

// undimLosing brings a dimmed chip back.
func (p *shortcutsPane) undimLosing(u *gunim.UI) {
	if p.losing != nil {
		p.losing.vis.setTo(1, u.Theme())
		p.losing = nil
		u.Invalidate()
	}
}

// takeOff notes that chord is about to fly from the row of command id to
// the row at to.
func (p *shortcutsPane) takeOff(c ui.Chord, id string, to widget.Key, u *gunim.UI) {
	for _, l := range p.lines {
		if l.id != id {
			continue
		}
		if r := p.rowOf(l.key); r != nil {
			if rc := r.chips[c]; rc != nil {
				if from, ok := u.Bounds(rc.chip); ok {
					p.flight = &chordFlight{chord: c, from: from, to: to}
				}
			}
		}
	}
}

// flyingChord is a shortcut on its way from one row to another, drawn
// over them both. It is the only sight of the chip on its way: the chip
// has gone from the row it left, and shows on the row it comes to as this
// lands. It takes no pointer and no keys.
type flyingChord struct {
	anim.Group
	chip   *widget.Chip
	t      *anim.Float
	from   geom.Rect
	target *widget.Fold
	land   func()
	live   bool
	at     geom.Point
	pane   *shortcutsPane
	u      *gunim.UI
}

func newFlyingChord(p *shortcutsPane) *flyingChord {
	f := &flyingChord{pane: p, chip: widget.NewChip("", ""), t: anim.NewFloat(0)}
	f.chip.SetCross(false, nil)
	f.Add(f.t)
	return f
}

// launch starts it from a place in the window to the fold of the chip it
// becomes, and land runs as it arrives.
func (f *flyingChord) launch(c ui.Chord, from geom.Rect, to *widget.Fold, land func(), u *gunim.UI) {
	f.chip.Label = chordLabel(c)
	f.chip.Keys = strings.Split(f.chip.Label, "+")
	f.from, f.target, f.land, f.live, f.u = from, to, land, true, u
	f.t.Jump(0)
	f.t.Animate(1, widget.Settle.Get(u.Theme()))
	f.aim()
	u.Invalidate()
}

// place is where it is drawn, in the pane's space, as worked out from
// the frame drawn last.
func (f *flyingChord) place(*shortcutsPane) geom.Point { return f.at }

// aim works out where it is drawn. The box of a node is known once the
// frame has drawn it, so this runs from the pane's Paint, and the place
// it finds is taken up by the next layout.
func (f *flyingChord) aim() {
	if !f.live || f.u == nil || f.pane == nil {
		return
	}
	own, ok := f.u.Bounds(f.pane)
	if !ok {
		return
	}
	// Until the frame that first draws the chip, it is where it set off.
	to, drawn := f.u.Bounds(f.target)
	if !drawn {
		to = f.from
	}
	t := min(max(f.t.Value(), 0), 1)
	f.at = geom.Pt(f.from.Min.X+(to.Min.X-f.from.Min.X)*t-own.Min.X, f.from.Min.Y+(to.Min.Y-f.from.Min.Y)*t-own.Min.Y)
}

// Step implements [gunim.Animator].
func (f *flyingChord) Step(dt time.Duration) bool {
	moving := f.Group.Step(dt)
	if f.live && !moving {
		f.live = false
		if f.land != nil {
			f.land()
			f.land = nil
		}
	}
	return moving
}

// Children implements [gunim.Composite].
func (f *flyingChord) Children() []gunim.Node { return []gunim.Node{f.chip} }

// Covers implements [gunim.Shaped]: it takes no pointer.
func (f *flyingChord) Covers(geom.Point) bool { return false }

// Layout implements [gunim.Node].
func (f *flyingChord) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	k := kids.At(0)
	s := k.Layout(gunim.Constraints{Max: c.Max})
	k.Place(geom.Point{})
	return s
}

// Paint implements [gunim.Node]: the keys, lifted off the rows, their
// shadow going as they come down.
func (f *flyingChord) Paint(p *paint.Painter, fr gunim.Frame, box geom.Size, kids gunim.Children) {
	if !f.live {
		return
	}
	th := fr.Theme
	lift := 1 - min(max(f.t.Value(), 0), 1)
	c := widget.DialogShadow.Get(th)
	c.A = uint8(float32(c.A) * lift)
	r := geom.Rc(0, 0, max(box.W-box.H, 0), box.H)
	p.ShadowRRect(r, widget.ChipCapRadius.Get(th), paint.Fill{}, paint.Shadow{Offset: geom.Pt(0, 4), Blur: shortcutFlyBlur.Get(th), Color: c, Outside: true})
	kids.At(0).Paint(p)
}
