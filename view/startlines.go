package view

import (
	"image/color"
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
	"github.com/marrasen/gunim/text"
	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/look"
	"github.com/marrasen/kakel/settings"
	"github.com/marrasen/kakel/shellsetup"
	"github.com/marrasen/kakel/words"
)

// Start lines: the dialog a layout's card opens to say what each of its
// terminals types as the layout opens it. A map of the window, each pane
// where it stands, picks the pane; under it are the lines of the pane
// picked, in an editor coloured for its shell.

// startLinesWidth is how wide the dialog is, and startLinesChrome how
// much of the window's height its title bar, buttons and margins take.
const (
	startLinesWidth  = 760
	startLinesChrome = 200
)

// The map's and the dialog's sizes.
var (
	// startMapTall and startMapShort are the most and the least room the
	// map is given down the dialog; across, it keeps the window's
	// proportions within them.
	startMapTall  = theme.Length("kakel.startlines.map.tall", 290)
	startMapShort = theme.Length("kakel.startlines.map.short", 150)
	// startMapGap is the room between two panes on the map, and
	// startMapRadius how round a pane's corners are.
	startMapGap    = theme.Length("kakel.startlines.map.gap", 6)
	startMapRadius = theme.Length("kakel.startlines.map.radius", 8)
	// startTabHeight is how tall a tab is, in a box of tabs.
	startTabHeight = theme.Length("kakel.startlines.tab.height", 24)
	// startPageHeight is how tall the part under the map is: where the
	// pane starts, and its lines.
	startPageHeight = theme.Length("kakel.startlines.page.height", 196)
	// startPageShort is the least room the page is given, in a short
	// window.
	startPageShort = theme.Length("kakel.startlines.page.short", 120)
	// startNameSize is the size of a pane's name on the map, and
	// startTabSize of a tab's.
	startNameSize = theme.Length("kakel.startlines.name", 13)
	startTabSize  = theme.Length("kakel.startlines.tab", 12)
)

// startLinesDialog opens the editor for layout l's start lines. Save
// keeps the lines of every terminal, each cleaned as a layout keeps
// them; Cancel keeps none of what was typed.
func (w *Window) startLinesDialog(l settings.SavedLayout, u *gunim.UI) {
	b := newStartLines(w, l, u)
	d := widget.NewDialog("Start Lines for " + l.Name)
	d.Icon = icon.SquareChevronRight
	d.Width = startLinesWidth
	d.Body = b
	d.SetButtons("Save", "Cancel")
	d.OnAccept = func(*gunim.UI) gunim.Intent {
		lines := b.lines()
		if b.same(lines) {
			return app.DialogClosed{}
		}
		return app.SaveStartLines{Layout: l, Lines: lines}
	}
	d.OnDismiss = widget.Sends(app.DialogClosed{})
	w.openDialog(d, u)
}

// startLines is the dialog's body: the map, the page of the pane picked,
// and a line on how the lines are typed and kept.
type startLines struct {
	w     *Window
	l     settings.SavedLayout
	m     *paneMap
	pages *widget.Pages
	note  *widget.Label
	// texts are each terminal's lines as they stand in its editor, by
	// slot.
	texts map[string]string
	// shown is the slot whose page is shown, and ed the editor on it, nil
	// for a pane that holds no lines.
	shown string
	ed    *widget.CodeEditor
}

func newStartLines(w *Window, l settings.SavedLayout, u *gunim.UI) *startLines {
	s := &startLines{w: w, l: l, texts: map[string]string{}}
	faces := map[string]*paneFace{}
	for slot, p := range l.Panes {
		if p.Kind == settings.LayoutTerminal {
			s.texts[slot] = strings.Join(p.Start, "\n")
		}
		faces[slot] = s.faceOf(slot, p)
	}
	aspect := float32(16) / 10
	if pl := l.Place; pl != nil && pl.W > 0 && pl.H > 0 {
		aspect = pl.W / pl.H
	}
	s.m = newPaneMap(l.Root, faces, aspect)
	s.m.OnPick = func(slot string, from geom.Point, u *gunim.UI) gunim.Intent {
		s.show(slot, from, u)
		return nil
	}
	s.shown = s.m.firstEditable()
	s.m.SetPicked(s.shown, nil)
	s.pages = widget.NewPages(s.page(s.shown, u))
	s.note = widget.NewLabel("Each line is typed at the next prompt, so a line that keeps running, such as a dev server, goes last. " +
		"The lines are saved in the settings file as plain text, so keep passwords out of them.")
	s.note.Size, s.note.Color = smallText, look.Faint
	return s
}

// faceOf is what the map says of pane p in slot: its name, where it
// runs, and its lines.
func (s *startLines) faceOf(slot string, p settings.SavedPane) *paneFace {
	f := newPaneFace(slot)
	f.kind, f.editable = p.Kind, p.Kind == settings.LayoutTerminal
	f.icon = kindIcons[p.Kind]
	host := app.LayoutPaneHost(p, s.serverNamed)
	if !host.Here {
		f.where = host.Name
	}
	f.name = s.paneName(p)
	f.lines = slices.Clone(p.Start)
	f.drawn = f.lines
	f.sign.Jump(anim.OnOff(len(f.lines) > 0))
	return f
}

// serverNamed is what the saved server or window with id is called now.
func (s *startLines) serverNamed(id string) (string, bool) {
	for _, h := range s.w.saved {
		if h.ID == id {
			return h.Name, true
		}
	}
	return "", false
}

// shellOf is the name and kind of the shell terminal p starts.
func (s *startLines) shellOf(p settings.SavedPane) (string, shellsetup.Route) {
	w := s.w
	return app.LayoutShell(p, app.KnownShells{Profiles: w.profiles, Shells: w.shellChoices, Found: w.foundShells,
		Default: w.defaultShell, ServerShells: w.serverShells, Defaults: w.defaults})
}

// paneName is what the map calls pane p: the name the user gave it, or
// else its shell's, its command, or its folder's.
func (s *startLines) paneName(p settings.SavedPane) string {
	switch {
	case p.Title != "":
		return p.Title
	case p.Kind == settings.LayoutCommand:
		return p.Line
	case p.Kind == settings.LayoutFiles:
		if f := strings.TrimRight(p.Folder, `/\`); f != "" {
			return f[strings.LastIndexAny(f, `/\`)+1:]
		}
		return "Files"
	}
	if name, _ := s.shellOf(p); name != "" {
		return name
	}
	return "Terminal"
}

// where says where the pane in slot starts, as its lines find it: its
// shell, its machine and its folder.
func (s *startLines) where(p settings.SavedPane) string {
	host := app.LayoutPaneHost(p, s.serverNamed)
	on := "this computer"
	if !host.Here {
		on = host.Name
	}
	var b strings.Builder
	switch p.Kind {
	case settings.LayoutTerminal:
		name, _ := s.shellOf(p)
		if name == "" {
			name = "the shell new terminals start"
		}
		b.WriteString("Starts " + name + " on " + on)
	case settings.LayoutCommand:
		b.WriteString("Runs on " + on)
	default:
		b.WriteString("Shows files on " + on)
	}
	if p.Folder != "" {
		b.WriteString(", in " + p.Folder)
	}
	return b.String()
}

// page is the part under the map for the pane in slot: where it starts,
// then its lines in an editor coloured for its shell, or for a pane that
// holds no lines, why.
func (s *startLines) page(slot string, u *gunim.UI) gunim.Node {
	p := s.l.Panes[slot]
	where := widget.NewLabel(s.where(p))
	where.Size, where.Color = smallText, look.Faint
	pg := &startPage{where: where}
	s.ed = nil
	switch p.Kind {
	case settings.LayoutCommand:
		pg.body = startSays("Start lines are for terminals. This pane runs its own command: " + p.Line)
		return pg
	case settings.LayoutFiles:
		pg.body = startSays("Start lines are for terminals. This pane shows files, and types nothing.")
		return pg
	}
	if p.Window {
		pg.also = startSays("These lines are saved. This version of kakel does not yet type into a terminal through another kakel window.")
	}
	_, kind := s.shellOf(p)
	ed := widget.NewCodeEditor()
	ed.TabMovesOn = true
	ed.Label = "Start lines for " + s.m.faces[slot].name
	ed.Highlight = pasteColours(kind, false)
	ed.SetText(s.texts[slot], nil)
	if u != nil {
		ed.SetMarks(pasteSlips(s.texts[slot], kind, false), u)
	}
	ed.OnChange = func(t string, u *gunim.UI) gunim.Intent {
		s.texts[slot] = t
		ed.SetMarks(pasteSlips(t, kind, false), u)
		s.m.SetLines(slot, settings.StartLines([]string{t}), u)
		return nil
	}
	pg.body, s.ed = ed, ed
	return pg
}

// startSays is a sentence on a page.
func startSays(s string) *widget.Label {
	l := widget.NewLabel(s)
	l.Color = look.Faint
	return l
}

// show shows the page of the pane in slot, coming from from.
func (s *startLines) show(slot string, from geom.Point, u *gunim.UI) {
	if slot == s.shown {
		return
	}
	s.shown = slot
	s.pages.Show(s.page(slot, u), from, u)
}

// lines are every terminal's lines as a layout keeps them, by slot.
func (s *startLines) lines() map[string][]string {
	out := map[string][]string{}
	for slot, t := range s.texts {
		if l := settings.StartLines([]string{t}); l != nil {
			out[slot] = l
		}
	}
	return out
}

// same reports whether lines are the lines the layout holds already.
func (s *startLines) same(lines map[string][]string) bool {
	had := map[string][]string{}
	for slot, p := range s.l.Panes {
		if len(p.Start) > 0 && p.Kind == settings.LayoutTerminal {
			had[slot] = p.Start
		}
	}
	return maps.EqualFunc(had, lines, slices.Equal)
}

// Focusables are what Tab moves through, before the dialog's buttons:
// the map, then the editor of the pane picked, where it has one.
func (s *startLines) Focusables() []gunim.Node {
	if s.ed != nil {
		return []gunim.Node{s.m, s.ed}
	}
	return []gunim.Node{s.m}
}

// Children implements [gunim.Composite].
func (s *startLines) Children() []gunim.Node { return []gunim.Node{s.m, s.pages, s.note} }

// Layout implements [gunim.Node]: the map across the top, as tall as
// keeps the window's proportions within its limits, then the page, of a
// height that stays as panes are picked, then the line on how the lines
// are typed.
func (s *startLines) Layout(c gunim.Constraints, f gunim.Frame, kids gunim.Children) geom.Size {
	const gap = 14
	th := f.Theme
	w := c.Max.W
	m, pages, note := kids.At(0), kids.At(1), kids.At(2)
	ns := note.Layout(gunim.Constraints{Max: geom.Sz(w, 0)})
	short, ph := startMapShort.Get(th), startPageHeight.Get(th)
	tall := min(max(w/s.m.aspect, short), startMapTall.Get(th))
	if room := s.w.size.H - startLinesChrome - ns.H - 2*gap; room > 0 && tall+ph > room {
		// In a short window the map gives up its room first, then the
		// page, so the dialog scrolls only where there is no room for
		// either.
		tall = max(room-ph, short)
		ph = max(room-tall, startPageShort.Get(th))
	}
	m.Layout(gunim.Tight(geom.Sz(w, tall)))
	m.Place(geom.Point{})
	y := tall + gap
	pages.Layout(gunim.Tight(geom.Sz(w, ph)))
	pages.Place(geom.Pt(0, y))
	y += ph + gap
	note.Place(geom.Pt(0, y))
	return geom.Sz(w, y+ns.H)
}

// Paint implements [gunim.Node].
func (s *startLines) Paint(p *paint.Painter, _ gunim.Frame, _ geom.Size, kids gunim.Children) {
	for k := range kids.All {
		k.Paint(p)
	}
}

// startPage is a pane's page: where it starts, a note where one is
// needed, and its lines, or why it holds none.
type startPage struct {
	where, also *widget.Label
	body        gunim.Node
}

// Children implements [gunim.Composite].
func (p *startPage) Children() []gunim.Node {
	if p.also != nil {
		return []gunim.Node{p.where, p.also, p.body}
	}
	return []gunim.Node{p.where, p.body}
}

// Layout implements [gunim.Node]: the words along the top, the editor in
// the room left under them.
func (p *startPage) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	const gap = 8
	w := c.Max.W
	y := float32(0)
	for i := 0; i < kids.Len()-1; i++ {
		s := kids.At(i).Layout(gunim.Constraints{Max: geom.Sz(w, 0)})
		kids.At(i).Place(geom.Pt(0, y))
		y += s.H + gap
	}
	body := kids.At(kids.Len() - 1)
	if _, editor := p.body.(*widget.CodeEditor); editor {
		body.Layout(gunim.Tight(geom.Sz(w, max(c.Max.H-y, 0))))
	} else {
		body.Layout(gunim.Constraints{Max: geom.Sz(w, 0)})
	}
	body.Place(geom.Pt(0, y))
	return c.Max
}

// Paint implements [gunim.Node].
func (p *startPage) Paint(pt *paint.Painter, _ gunim.Frame, _ geom.Size, kids gunim.Children) {
	for k := range kids.All {
		k.Paint(pt)
	}
}

// paneFace is what the map says of one pane: its icon and name, the
// machine it runs on when that is not this computer, and its start
// lines, with the springs that show them.
type paneFace struct {
	slot, kind  string
	name, where string
	icon        *icon.Icon
	// editable says the pane holds start lines: a terminal.
	editable bool
	lines    []string
	// drawn are the lines drawn: lines, or as the last is taken out, the
	// lines before, fading out with the sign.
	drawn []string
	// sign shows that it holds lines, coming as the first is typed and
	// going as the last is taken out; hover lights it under the pointer;
	// front is how far it is the tab its box shows, for a box of tabs.
	sign, hover, front *anim.Float
}

func newPaneFace(slot string) *paneFace {
	return &paneFace{slot: slot, sign: anim.NewFloat(0), hover: anim.NewFloat(0), front: anim.NewFloat(1)}
}

// mapBox is a box on the map, as last laid out: one pane, or a group's
// tabs, with where each tab stands along its top.
type mapBox struct {
	r     geom.Rect
	slots []string
	tabs  []geom.Rect
	// body is the room under the tabs, where the pane shown is said.
	body geom.Rect
}

// tabbed reports whether the box is a group of more than one tab.
func (b *mapBox) tabbed() bool { return len(b.slots) > 1 }

// paneMap draws a layout's window small, in the window's proportions:
// each pane a box where it stands, and a group of tabs one box, its tabs
// along its top. One pane is picked: a mark glides to its box, and in a
// box of tabs, a second one along the tabs to its tab. A click picks a
// pane, as do the arrow keys, which go to the box beside the one picked
// on that side, and along a box's tabs first; Home and End pick the
// first and the last.
type paneMap struct {
	anim.Group
	root   *settings.LayoutNode
	faces  map[string]*paneFace
	order  []string
	aspect float32
	picked string
	// OnPick runs as a pane is picked, with from pointing from the box
	// picked before toward the one picked now, along the way it went
	// most: (1, 0) for one to the right.
	OnPick func(slot string, from geom.Point, u *gunim.UI) gunim.Intent

	boxes []mapBox
	// shows is the tab each box of tabs shows, by its first tab's slot.
	shows map[string]string
	// mark is where the picked box's mark stands, tab where the picked
	// tab's does, tabOn how far it shows, and ring the focus ring.
	mark, tab   *anim.Rect
	tabOn, ring *anim.Float
	laid        bool
	hover       string
	// cuts are the words drawn cut short, where they were drawn, for
	// the tooltip to say them whole.
	cuts []mapCut
	tip  widget.PartTip
}

// mapCut is words cut short on the map, and where they stand.
type mapCut struct {
	r     geom.Rect
	words string
}

func newPaneMap(root *settings.LayoutNode, faces map[string]*paneFace, aspect float32) *paneMap {
	m := &paneMap{root: root, faces: faces, aspect: aspect, shows: map[string]string{},
		mark: anim.NewRect(geom.Rect{}), tab: anim.NewRect(geom.Rect{}), tabOn: anim.NewFloat(0), ring: anim.NewFloat(0)}
	m.Add(m.mark, m.tab, m.tabOn, m.ring)
	var walk func(n *settings.LayoutNode)
	walk = func(n *settings.LayoutNode) {
		if n == nil {
			return
		}
		if n.Kind == "item" {
			if f, ok := faces[n.ID]; ok {
				m.order = append(m.order, n.ID)
				m.Add(f.sign, f.hover, f.front)
			}
			return
		}
		for _, k := range n.Kids {
			walk(k)
		}
		if n.Kind == "group" {
			m.shows[groupKey(n)] = n.Front
		}
	}
	walk(root)
	return m
}

// groupKey names group n among the map's boxes of tabs: by its first
// tab.
func groupKey(n *settings.LayoutNode) string {
	for _, k := range n.Kids {
		if k.Kind == "item" {
			return k.ID
		}
	}
	return n.ID
}

// firstEditable is the first pane in reading order that holds lines, or
// else the first pane.
func (m *paneMap) firstEditable() string {
	for _, slot := range m.order {
		if m.faces[slot].editable {
			return slot
		}
	}
	if len(m.order) > 0 {
		return m.order[0]
	}
	return ""
}

// Picked is the slot of the pane picked.
func (m *paneMap) Picked() string { return m.picked }

// SetPicked picks the pane in slot, its mark gliding to it; before the
// map is laid out it stands there at once. It sends nothing.
func (m *paneMap) SetPicked(slot string, u *gunim.UI) {
	if _, ok := m.faces[slot]; !ok {
		return
	}
	m.picked = slot
	if b := m.boxOf(slot); b != nil && b.tabbed() {
		m.shows[b.slots[0]] = slot
	}
	if u != nil {
		u.Invalidate()
	}
}

// SetLines says the pane in slot holds lines, its sign coming in with
// the first and going with the last.
func (m *paneMap) SetLines(slot string, lines []string, u *gunim.UI) {
	f, ok := m.faces[slot]
	if !ok {
		return
	}
	f.lines = lines
	if len(lines) > 0 {
		f.drawn = lines
	}
	on := anim.OnOff(len(lines) > 0)
	if u == nil {
		f.sign.Jump(on)
		return
	}
	if f.sign.Target() != on {
		f.sign.Animate(on, widget.Quick.Get(u.Theme()))
	}
	u.Invalidate()
}

// pick picks the pane in slot as the user asked for it, and says so.
func (m *paneMap) pick(slot string, u *gunim.UI) {
	if slot == "" || slot == m.picked {
		return
	}
	var from geom.Point
	if a, b := m.boxOf(m.picked), m.boxOf(slot); a != nil && b != nil {
		was, now := m.rectOf(m.picked), m.rectOf(slot)
		d := now.Center().Sub(was.Center())
		switch {
		case d.X == 0 && d.Y == 0:
		case abs32(d.X) >= abs32(d.Y):
			from.X = sign32(d.X)
		default:
			from.Y = sign32(d.Y)
		}
	}
	m.SetPicked(slot, u)
	if m.OnPick != nil {
		if in := m.OnPick(slot, from, u); in != nil {
			u.Send(m, in)
		}
	}
	u.Cue(gunim.CueSelect, nil)
}

func sign32(f float32) float32 {
	if f < 0 {
		return -1
	}
	return 1
}

// boxOf is the box the pane in slot stands in, or nil.
func (m *paneMap) boxOf(slot string) *mapBox {
	for i := range m.boxes {
		if slices.Contains(m.boxes[i].slots, slot) {
			return &m.boxes[i]
		}
	}
	return nil
}

// rectOf is where the pane in slot is drawn: its tab in a box of tabs,
// or else its box.
func (m *paneMap) rectOf(slot string) geom.Rect {
	b := m.boxOf(slot)
	if b == nil {
		return geom.Rect{}
	}
	if i := slices.Index(b.slots, slot); b.tabbed() && i >= 0 {
		return b.tabs[i]
	}
	return b.r
}

// at is the pane at pos: a tab under it, or the pane its box shows.
func (m *paneMap) at(pos geom.Point) string {
	for _, b := range m.boxes {
		if !b.r.Contains(pos) {
			continue
		}
		for i, t := range b.tabs {
			if t.Contains(pos) {
				return b.slots[i]
			}
		}
		return m.showing(b)
	}
	return ""
}

// showing is the pane box b shows: its tab picked, or else the one it
// showed last, or the one in front.
func (m *paneMap) showing(b mapBox) string {
	if !b.tabbed() {
		return b.slots[0]
	}
	if slices.Contains(b.slots, m.picked) {
		return m.picked
	}
	if s := m.shows[b.slots[0]]; slices.Contains(b.slots, s) {
		return s
	}
	return b.slots[0]
}

// beside is the pane the arrow toward d goes to from the one picked:
// along a box's tabs first, then the box nearest on that side, the one
// most in line with the picked first.
func (m *paneMap) beside(d geom.Point) string {
	cur := m.boxOf(m.picked)
	if cur == nil {
		return m.firstEditable()
	}
	if cur.tabbed() && d.Y == 0 {
		i := slices.Index(cur.slots, m.picked) + int(d.X)
		if i >= 0 && i < len(cur.slots) {
			return cur.slots[i]
		}
	}
	best, bestAway, bestOff := -1, float32(0), float32(0)
	c := cur.r.Center()
	for i, b := range m.boxes {
		if b.r == cur.r {
			continue
		}
		var away, off float32
		switch {
		case d.X > 0:
			away, off = b.r.Min.X-cur.r.Max.X, apart(b.r.Min.Y, b.r.Max.Y, cur.r.Min.Y, cur.r.Max.Y, c.Y)
		case d.X < 0:
			away, off = cur.r.Min.X-b.r.Max.X, apart(b.r.Min.Y, b.r.Max.Y, cur.r.Min.Y, cur.r.Max.Y, c.Y)
		case d.Y > 0:
			away, off = b.r.Min.Y-cur.r.Max.Y, apart(b.r.Min.X, b.r.Max.X, cur.r.Min.X, cur.r.Max.X, c.X)
		default:
			away, off = cur.r.Min.Y-b.r.Max.Y, apart(b.r.Min.X, b.r.Max.X, cur.r.Min.X, cur.r.Max.X, c.X)
		}
		if away < -1 {
			continue
		}
		if best < 0 || off < bestOff || off == bestOff && away < bestAway {
			best, bestAway, bestOff = i, away, off
		}
	}
	if best < 0 {
		return ""
	}
	b := m.boxes[best]
	if b.tabbed() && d.X != 0 {
		// Into a box of tabs from the side, to the tab on that side.
		if d.X > 0 {
			return b.slots[0]
		}
		return b.slots[len(b.slots)-1]
	}
	return m.showing(b)
}

// apart is how far the span lo to hi stands from the span from to to, 0
// where they overlap, less a little the nearer its middle is to mid, so
// of two boxes beside, the one more in line is taken.
func apart(lo, hi, from, to, mid float32) float32 {
	gap := max(lo-to, from-hi, 0)
	return gap*1000 + abs32((lo+hi)/2-mid)
}

// Focusable implements [gunim.Focusable].
func (m *paneMap) Focusable() bool { return len(m.order) > 0 }

// Handle implements [gunim.Handler].
func (m *paneMap) Handle(e gi.Event, u *gunim.UI) bool {
	th := u.Theme()
	switch e := e.(type) {
	case gi.PointerDown:
		if e.Button != gi.ButtonPrimary {
			return false
		}
		m.pick(m.at(e.Pos), u)
		return false
	case gi.PointerMove:
		m.hovers(m.at(e.Pos), u)
		m.tip.Handle(e, u, m, m.cutAt(e.Pos))
		return false
	case gi.PointerLeave:
		m.hovers("", u)
		m.tip.Handle(e, u, m, "")
		return false
	case gi.FocusRing:
		m.ring.Animate(anim.OnOff(e.On), widget.RingFade.Get(th))
		return false
	case gi.FocusLost:
		m.ring.Animate(0, widget.RingFade.Get(th))
		return false
	case gi.KeyPress:
		if e.Mods&^gi.ModShift != 0 {
			return false
		}
		var to string
		switch e.Key {
		case gi.KeyLeft:
			to = m.beside(geom.Pt(-1, 0))
		case gi.KeyRight:
			to = m.beside(geom.Pt(1, 0))
		case gi.KeyUp:
			to = m.beside(geom.Pt(0, -1))
		case gi.KeyDown:
			to = m.beside(geom.Pt(0, 1))
		case gi.KeyHome:
			to = m.order[0]
		case gi.KeyEnd:
			to = m.order[len(m.order)-1]
		default:
			return false
		}
		m.pick(to, u)
		u.ShowFocusRing()
		return true
	}
	return false
}

// hovers lights the pane at slot under the pointer, and puts out the
// one lit before.
func (m *paneMap) hovers(slot string, u *gunim.UI) {
	if slot == m.hover {
		return
	}
	q := widget.Quick.Get(u.Theme())
	if f := m.faces[m.hover]; f != nil {
		f.hover.Animate(0, q)
	}
	if f := m.faces[slot]; f != nil {
		f.hover.Animate(1, q)
	}
	m.hover = slot
}

// cutAt is the whole of the words cut short at pos, or "".
func (m *paneMap) cutAt(pos geom.Point) string {
	for _, c := range m.cuts {
		if c.r.Contains(pos) {
			return c.words
		}
	}
	return ""
}

// Access implements [gunim.Accessible]: a list of the panes, each read
// with what it is, where it runs and how many lines it holds, the one
// picked selected.
func (m *paneMap) Access() access.Info {
	info := access.Info{Role: access.RoleList, Name: "Panes"}
	for i, slot := range m.order {
		f := m.faces[slot]
		part := access.Info{Role: access.RoleListItem, Name: f.name, Description: f.said(), Bounds: m.rectOf(slot), Key: uint64(i + 1)}
		if slot == m.picked {
			part.State |= access.StateSelected
			info.Active = i + 1
		}
		info.Parts = append(info.Parts, part)
	}
	return info
}

// said is what a screen reader reads after the pane's name: what it is,
// where it runs, and its lines.
func (f *paneFace) said() string {
	what := map[string]string{settings.LayoutTerminal: "terminal", settings.LayoutCommand: "command", settings.LayoutFiles: "file manager"}[f.kind]
	if f.where != "" {
		what += " on " + f.where
	}
	if f.editable {
		what += ", " + words.ManyOf(len(f.lines), "start line", "start lines")
	}
	return what
}

// Layout implements [gunim.Node]: the window's tree in the room given,
// kept in the window's proportions and in the middle, and the marks
// sent gliding to the pane picked.
func (m *paneMap) Layout(c gunim.Constraints, f gunim.Frame, _ gunim.Children) geom.Size {
	th := f.Theme
	size := c.Max
	w := min(size.W, size.H*m.aspect)
	r := geom.Rc((size.W-w)/2, 0, w, size.H)
	m.boxes = m.boxes[:0]
	gap := startMapGap.Get(th)
	tabH := startTabHeight.Get(th)
	var walk func(n *settings.LayoutNode, r geom.Rect)
	walk = func(n *settings.LayoutNode, r geom.Rect) {
		if n == nil {
			return
		}
		switch n.Kind {
		case "item":
			if _, ok := m.faces[n.ID]; ok {
				m.boxes = append(m.boxes, mapBox{r: r, slots: []string{n.ID}, body: r})
			}
			return
		case "group":
			b := mapBox{r: r, body: r}
			for _, k := range n.Kids {
				if _, ok := m.faces[k.ID]; ok && k.Kind == "item" {
					b.slots = append(b.slots, k.ID)
				}
			}
			if len(b.slots) == 0 {
				return
			}
			if b.tabbed() {
				const inset, tabGap = 4, 2
				each := min((r.Size().W-2*inset-tabGap*float32(len(b.slots)-1))/float32(len(b.slots)), 140)
				for i := range b.slots {
					x := r.Min.X + inset + float32(i)*(each+tabGap)
					b.tabs = append(b.tabs, geom.Rc(x, r.Min.Y+inset, max(each, 0), tabH))
				}
				b.body = geom.Rect{Min: geom.Pt(r.Min.X, r.Min.Y+inset+tabH), Max: r.Max}
			}
			m.boxes = append(m.boxes, b)
			return
		}
		kids := n.Kids
		if len(kids) == 0 {
			return
		}
		row := n.Kind == "row"
		along := r.Size().H
		if row {
			along = r.Size().W
		}
		room := max(along-gap*float32(len(kids)-1), 0)
		shares := make([]float32, len(kids))
		for i := range shares {
			shares[i] = 1 / float32(len(kids))
		}
		if len(kids) == 2 {
			s := n.Share
			if s <= 0 || s >= 1 {
				s = 0.5
			}
			shares[0], shares[1] = s, 1-s
		}
		at := float32(0)
		for i, k := range kids {
			part := room * shares[i]
			if row {
				walk(k, geom.Rc(r.Min.X+at, r.Min.Y, part, r.Size().H))
			} else {
				walk(k, geom.Rc(r.Min.X, r.Min.Y+at, r.Size().W, part))
			}
			at += part + gap
		}
	}
	walk(m.root, r)
	// Each box of tabs shows the tab picked, or the one it showed last,
	// the one going fading out as the other fades in.
	for _, b := range m.boxes {
		shown := m.showing(b)
		for _, slot := range b.slots {
			want := anim.OnOff(slot == shown)
			if fc := m.faces[slot]; !m.laid {
				fc.front.Jump(want)
			} else if fc.front.Target() != want {
				fc.front.Animate(want, widget.Crossfade.Get(th))
			}
		}
	}
	box, tab, inTab := geom.Rect{}, geom.Rect{}, false
	if b := m.boxOf(m.picked); b != nil {
		box, tab = b.r, b.r
		if i := slices.Index(b.slots, m.picked); b.tabbed() && i >= 0 {
			tab, inTab = b.tabs[i], true
		}
	}
	settle := widget.Settle.Get(th)
	switch {
	case !m.laid:
		m.mark.Jump(box)
		m.tab.Jump(tab)
		m.tabOn.Jump(anim.OnOff(inTab))
	default:
		if m.mark.Target() != box {
			m.mark.Animate(box, settle)
		}
		if inTab && m.tab.Target() != tab {
			if m.tabOn.Value() < 0.01 {
				// Coming to a box of tabs, the tab's mark starts from the
				// box's mark, so it glides in with it.
				m.tab.Jump(m.mark.Value())
			}
			m.tab.Animate(tab, settle)
		}
		if on := anim.OnOff(inTab); m.tabOn.Target() != on {
			m.tabOn.Animate(on, widget.Quick.Get(th))
		}
	}
	m.laid = true
	return size
}

// Paint implements [gunim.Node]: each box, quieter for a pane that holds
// no lines, the marks of the pane picked over them, and the focus ring
// round the mark.
func (m *paneMap) Paint(p *paint.Painter, f gunim.Frame, _ geom.Size, _ gunim.Children) {
	th := f.Theme
	radius := startMapRadius.Get(th)
	m.cuts = m.cuts[:0]
	mark := m.mark.Value()
	for _, b := range m.boxes {
		fill := widget.CardFill.Get(th)
		if !m.faces[m.showing(b)].editable && !b.tabbed() {
			fill = fade(fill, 0.55)
		}
		p.RRect(b.r, radius, paint.Solid(fill))
		p.RRectStroke(b.r.Inset(geom.Uniform(0.5)), radius, paint.Fill{}, paint.Stroke{Width: 1, Color: widget.FieldBorder.Get(th)})
		for _, slot := range b.slots {
			if h := m.faces[slot].hover.Value(); h > 0.01 {
				lit := b.r
				if b.tabbed() {
					lit = b.tabs[slices.Index(b.slots, slot)]
				}
				p.RRect(lit, radius, paint.Solid(fade(look.RowHover.Get(th), h)))
			}
		}
	}
	// The mark's fill, under the words.
	p.RRect(mark, radius, paint.Solid(look.RowActive.Get(th)))
	if t := m.tabOn.Value(); t > 0.01 {
		p.RRect(m.tab.Value(), radius-2, paint.Solid(fade(widget.Accent.Get(th), 0.4*min(t, 1))))
	}
	for _, b := range m.boxes {
		end := m.clip(p, b.r, radius)
		for i, slot := range b.slots {
			if b.tabbed() {
				m.paintTab(p, th, b.tabs[i], m.faces[slot])
			}
		}
		for _, slot := range b.slots {
			if t := m.faces[slot].front.Value(); t > 0.01 {
				m.paintFace(p, th, b.body, m.faces[slot], min(t, 1))
			}
		}
		end()
	}
	accent := widget.Accent.Get(th)
	p.RRectStroke(mark.Inset(geom.Uniform(0.75)), radius, paint.Fill{}, paint.Stroke{Width: 1.5, Color: accent})
	widget.FocusRing(p, mark, radius, m.ring.Value(), th)
}

// clip confines what is drawn to r, until the function it returns is
// called.
func (m *paneMap) clip(p *paint.Painter, r geom.Rect, radius float32) func() {
	return p.Layer(paint.LayerOpts{Bounds: r, Opacity: 1, Clip: true, Radius: radius})
}

// paintTab draws a tab of a box of tabs: its icon and name, plain on the
// tab its box shows and faint on the rest.
func (m *paneMap) paintTab(p *paint.Painter, th *theme.Live, r geom.Rect, f *paneFace) {
	const pad, iconSize = 6, 12
	ink := mix(look.Faint.Get(th), widget.Ink.Get(th), f.front.Value())
	drawIcon(p, f.icon, geom.Rc(r.Min.X+pad, r.Min.Y+(r.Size().H-iconSize)/2, iconSize, iconSize), ink, 1.4)
	run := text.Default().Shape(f.name, startTabSize.Get(th))
	x := r.Min.X + pad + iconSize + 5
	room := max(r.Max.X-pad-x, 0)
	if sign := f.sign.Value(); sign > 0.01 {
		room = max(room-10*min(sign, 1), 0)
		dot := geom.Rc(r.Max.X-pad-6, r.Min.Y+(r.Size().H-6)/2, 6, 6)
		p.RRect(dot, 3, paint.Solid(fade(widget.Accent.Get(th), sign)))
	}
	m.drawWords(p, th, run, f.name, geom.Pt(x, r.Min.Y+(r.Size().H-run.Height())/2), ink, room)
}

// paintFace draws what a pane is in r, at opacity t: its icon and name,
// the machine it runs on, and, for a terminal with lines, how many and
// the first of them.
func (m *paneMap) paintFace(p *paint.Painter, th *theme.Live, r geom.Rect, f *paneFace, t float32) {
	const pad, iconSize = 10, 16
	if t < 0.999 {
		defer p.Layer(paint.LayerOpts{Bounds: r, Opacity: t})()
	}
	ink, faint := widget.Ink.Get(th), look.Faint.Get(th)
	if !f.editable {
		ink = faint
	}
	top := r.Min.Y + pad
	drawIcon(p, f.icon, geom.Rc(r.Min.X+pad, top, iconSize, iconSize), ink, 1.5)
	name := text.GoSans(true, false).Shape(f.name, startNameSize.Get(th))
	x := r.Min.X + pad + iconSize + 7
	room := max(r.Max.X-pad-x, 0)
	sign := min(f.sign.Value(), 1)
	var count text.Run
	if sign > 0.01 && len(f.drawn) > 0 {
		count = text.Default().Shape(words.ManyOf(len(f.drawn), "line", "lines"), smallText.Get(th))
		room = max(room-(count.Advance+14)*sign, 0)
	}
	m.drawWords(p, th, name, f.name, geom.Pt(x, top+(iconSize-name.Height())/2), ink, room)
	if count.Face != nil {
		// The count, in a pill in the corner, growing in with the first
		// line typed.
		pill := geom.Rc(r.Max.X-pad-count.Advance-12, top+(iconSize-count.Height())/2-2, count.Advance+12, count.Height()+4)
		at := pill.Center()
		scale := 0.6 + 0.4*sign
		pill = geom.Rect{Min: geom.Pt(at.X-pill.Size().W/2*scale, at.Y-pill.Size().H/2*scale), Max: geom.Pt(at.X+pill.Size().W/2*scale, at.Y+pill.Size().H/2*scale)}
		end := p.Layer(paint.LayerOpts{Bounds: pill, Opacity: sign})
		p.RRect(pill, pill.Size().H/2, paint.Solid(widget.ButtonPrimaryFill.Get(th)))
		count.Paint(p, geom.Pt(pill.Min.X+(pill.Size().W-count.Advance)/2, pill.Min.Y+(pill.Size().H-count.Height())/2), widget.ButtonPrimaryInk.Get(th))
		end()
	}
	y := top + iconSize + 6
	if f.where != "" {
		where := text.Default().Shape(f.where, smallText.Get(th))
		if y+where.Height() <= r.Max.Y-pad/2 {
			m.drawWords(p, th, where, f.where, geom.Pt(x, y), faint, max(r.Max.X-pad-x, 0))
		}
		y += where.Height() + 4
	}
	if count.Face == nil {
		return
	}
	// The lines, as the shell will see them, as many as there is room
	// for, and how many more under them.
	mono := widget.MonoFont.Get(th)
	if mono == nil {
		mono = text.Default()
	}
	size := smallText.Get(th)
	room = max(r.Max.X-pad-x, 0)
	end := p.Layer(paint.LayerOpts{Bounds: r, Opacity: sign})
	defer end()
	for i, line := range f.drawn {
		run := mono.Shape("› "+line, size)
		more := len(f.drawn) - i - 1
		if more > 0 && y+2*run.Height()+2 > r.Max.Y-pad/2 {
			// No room for this line and the next: the count of the rest.
			rest := text.Default().Shape(words.ManyOf(more+1, "more line", "more lines"), size)
			if y+rest.Height() <= r.Max.Y-pad/2 {
				rest.PaintFaded(p, geom.Pt(x, y), faint, room, widget.TextFade.Get(th))
			}
			return
		}
		if y+run.Height() > r.Max.Y-pad/2 {
			return
		}
		m.drawWords(p, th, run, line, geom.Pt(x, y), ink, room)
		y += run.Height() + 2
	}
}

// drawWords draws run at at in c, faded out where it runs past room, and
// notes words, whole, for the tooltip where it does.
func (m *paneMap) drawWords(p *paint.Painter, th *theme.Live, run text.Run, words string, at geom.Point, c color.NRGBA, room float32) {
	run.PaintFaded(p, at, c, room, widget.TextFade.Get(th))
	if run.Advance > room {
		m.cuts = append(m.cuts, mapCut{r: geom.Rc(at.X, at.Y, room, run.Height()), words: words})
	}
}

// fade is c with its alpha scaled by t.
func fade(c color.NRGBA, t float32) color.NRGBA {
	c.A = uint8(float32(c.A) * min(max(t, 0), 1))
	return c
}

// mix blends from a toward b by t, from 0 to 1.
func mix(a, b color.NRGBA, t float32) color.NRGBA {
	t = min(max(t, 0), 1)
	m := func(x, y uint8) uint8 { return uint8(float32(x) + (float32(y)-float32(x))*t + 0.5) }
	return color.NRGBA{R: m(a.R, b.R), G: m(a.G, b.G), B: m(a.B, b.B), A: m(a.A, b.A)}
}
