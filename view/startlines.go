package view

import (
	"cmp"
	"image/color"
	"math"
	"slices"
	"strings"
	"time"

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
	"github.com/marrasen/kakel/wrongshell"
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
	// map is given down the dialog. Across, it takes the dialog's width,
	// and down, the window's proportions within them.
	startMapTall  = theme.Length("kakel.startlines.map.tall", 210)
	startMapShort = theme.Length("kakel.startlines.map.short", 110)
	// startMapGap is the room between two panes on the map, and
	// startMapRadius how round a pane's corners are.
	startMapGap    = theme.Length("kakel.startlines.map.gap", 6)
	startMapRadius = theme.Length("kakel.startlines.map.radius", 8)
	// startTabHeight is how tall a tab is, in a box of tabs, and
	// startTabLeast how wide the tab in front is kept, where the box is
	// crowded, for some of its name to show beside its icon.
	startTabHeight = theme.Length("kakel.startlines.tab.height", 24)
	startTabLeast  = theme.Length("kakel.startlines.tab.least", 64)
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

// startPick is the motion of a pane being picked on the map: the mark
// gliding to it, its tab coming to the front of its box, and its name
// lit. The page under the map turns in two halves on widget.PageTurn,
// the old page fading out before the new comes in; on this spring the
// pick is half way as the old page goes and comes to rest as the new
// one does, with no overshoot to land before it, so the two read as one
// movement.
var startPick = theme.Spring("kakel.startlines.pick", anim.Spring{Response: 0.25, Damping: 1})

// startLinesDialog opens the editor for layout l's start lines. Save
// keeps the lines of each terminal whose lines were changed, cleaned as a
// layout keeps them; Cancel keeps none of what was typed. Where the
// layout was removed or changed meanwhile, Save says so and the dialog
// stays open, what was typed in it kept.
func (w *Window) startLinesDialog(l settings.SavedLayout, u *gunim.UI) {
	b := newStartLines(w, l, u)
	// No icon by the title, as Save Layout has none.
	d := widget.NewDialog("Start Lines for " + l.Name)
	d.Width = startLinesWidth
	d.Body = b
	d.SetButtons("Save", "Cancel")
	d.Check = b.check
	d.OnAccept = func(*gunim.UI) gunim.Intent {
		lines := b.changed()
		if len(lines) == 0 {
			return app.DialogClosed{}
		}
		return app.SaveStartLines{Layout: l, Lines: lines}
	}
	d.OnDismiss = widget.Sends(app.DialogClosed{})
	w.openDialog(d, u)
}

// startLines is the dialog's body: the map, the page of the pane picked,
// and a line on how the lines are typed and kept, which fades out for a
// pane that takes none.
type startLines struct {
	anim.Group
	w     *Window
	l     settings.SavedLayout
	m     *paneMap
	pages *widget.Pages
	note  *widget.Label
	// noteOn is how far the note shows.
	noteOn *anim.Float
	// texts are the lines of each terminal that takes them as they stand
	// in its editor, by slot.
	texts map[string]string
	// shown is the slot whose page is shown, and ed the editor on it, nil
	// for a pane that takes no lines.
	shown string
	ed    *widget.CodeEditor
	// noteAt is where the note stands, as last laid out.
	noteAt geom.Rect
}

func newStartLines(w *Window, l settings.SavedLayout, u *gunim.UI) *startLines {
	s := &startLines{w: w, l: l, texts: map[string]string{}, noteOn: anim.NewFloat(0)}
	s.Add(s.noteOn)
	faces := map[string]*paneFace{}
	eachItem(l.Root, func(slot string) {
		if _, ok := faces[slot]; ok {
			return
		}
		p, ok := l.Panes[slot]
		f := s.faceOf(slot, p, ok)
		if f.editable {
			s.texts[slot] = strings.Join(p.Start, "\n")
		}
		faces[slot] = f
	})
	aspect := float32(16) / 10
	if pl := l.Place; pl != nil && pl.W > 0 && pl.H > 0 {
		aspect = pl.W / pl.H
	}
	s.m = newPaneMap(l.Root, faces, aspect)
	s.m.OnPick = func(slot string, from geom.Point, u *gunim.UI) gunim.Intent {
		s.show(slot, from, u)
		return nil
	}
	s.m.OnChoose = func(slot string, u *gunim.UI) gunim.Intent {
		// A pane chosen with a click, Enter or Space puts the keyboard in
		// its lines.
		if slot == s.shown && s.ed != nil {
			u.Focus(s.ed)
		}
		return nil
	}
	s.shown = s.m.firstEditable()
	s.m.SetPicked(s.shown, nil)
	s.pages = widget.NewPages(s.page(s.shown, u))
	// Each page lays out its name and its lines in the same places, so
	// the old one leaves before the new one comes, never read over it.
	s.pages.InTurn = true
	s.noteOn.Jump(anim.OnOff(s.ed != nil))
	s.note = widget.NewLabel("kakel types these lines as the layout opens, one at each prompt. " +
		"Put a line that keeps running, such as a dev server, last. The lines are saved as plain text, so keep passwords out.")
	s.note.Size = smallText
	return s
}

// eachItem calls do with the slot of each pane in tree n, in the order
// they stand.
func eachItem(n *settings.LayoutNode, do func(slot string)) {
	if n == nil {
		return
	}
	if n.Kind == "item" {
		do(n.ID)
		return
	}
	for _, k := range n.Kids {
		eachItem(k, do)
	}
}

// faceOf is what the map says of pane p in slot, or of a slot the
// layout holds no pane for, where known is false: its name, where it
// runs, and its lines. Only a terminal on a machine reached directly
// takes lines.
func (s *startLines) faceOf(slot string, p settings.SavedPane, known bool) *paneFace {
	f := newPaneFace(slot)
	f.kind = p.Kind
	f.editable = known && p.Kind == settings.LayoutTerminal && !p.Window
	f.icon = kindIcons[p.Kind]
	if !known || f.icon == nil {
		f.icon = icon.SquareDashed
	}
	if known {
		if host := app.LayoutPaneHost(p, s.serverNamed); !host.Here {
			f.where = host.Name
		}
	}
	f.name = s.paneName(p, known)
	if f.editable {
		f.lines = slices.Clone(p.Start)
		f.rows.set(f.lines, anim.Spring{}, false)
	}
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
		Default: w.defaultShell, Defaults: w.defaults})
}

// paneName is what the map calls pane p: the name the user gave it, or
// else its shell's, its command, or its folder's.
func (s *startLines) paneName(p settings.SavedPane, known bool) string {
	switch {
	case !known:
		return "Unknown pane"
	case p.Title != "":
		return p.Title
	case p.Kind == settings.LayoutCommand:
		return p.Line
	case p.Kind == settings.LayoutFiles:
		if f := strings.TrimRight(p.Folder, `/\`); f != "" {
			return f[strings.LastIndexAny(f, `/\`)+1:]
		}
		return "Files"
	case p.Kind == settings.LayoutTerminal:
		if name, _ := s.shellOf(p); name != "" {
			return name
		}
		return "Terminal"
	}
	return "Unknown pane"
}

// where says where pane p starts, as its lines find it: its shell, its
// machine and its folder.
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
			name = "Terminal"
		}
		b.WriteString(name + " on " + on)
		if p.Folder != "" {
			b.WriteString(", starts in " + p.Folder)
		}
	case settings.LayoutCommand:
		b.WriteString("Command on " + on)
		if p.Folder != "" {
			b.WriteString(", in " + p.Folder)
		}
	case settings.LayoutFiles:
		b.WriteString("Files on " + on)
		if p.Folder != "" {
			b.WriteString(", in " + p.Folder)
		}
	}
	return b.String()
}

// why is the sentence a page says in place of an editor, for a pane that
// takes no lines: the pane in slot, or with none, the layout.
func (s *startLines) why(slot string) string {
	p, known := s.l.Panes[slot]
	switch {
	case slot == "":
		return "This layout has no panes."
	case !known:
		return "kakel can't open this pane, so it takes no start lines."
	case p.Kind == settings.LayoutTerminal && p.Window:
		return "This terminal opens through another kakel window, so it takes no start lines yet."
	case p.Kind == settings.LayoutCommand:
		return "This pane runs a command, so it takes no start lines."
	case p.Kind == settings.LayoutFiles:
		return "This pane shows files, so it takes no start lines."
	}
	return "kakel can't open this kind of pane, so it takes no start lines."
}

// page is the part under the map for the pane in slot: its name and
// where it starts, then its lines in an editor coloured for its shell,
// or for a pane that takes no lines, why, in a well the editor's size.
func (s *startLines) page(slot string, u *gunim.UI) gunim.Node {
	s.ed = nil
	f := s.m.faces[slot]
	pg := &startPage{}
	if f != nil {
		pg.name = widget.NewLabel(f.name)
		pg.name.Size, pg.name.Face, pg.name.MaxLines = smallText, widget.BoldFont, 1
		if p, ok := s.l.Panes[slot]; ok {
			if where := s.where(p); where != "" {
				pg.where = widget.NewLabel("· " + where)
				pg.where.Size, pg.where.MaxLines = smallText, 1
			}
		}
	}
	if f == nil || !f.editable {
		pg.body = newStartWell(s.why(slot))
		return pg
	}
	p := s.l.Panes[slot]
	shell, kind := s.shellOf(p)
	ed := startEditor("Start lines for "+f.name, kind)
	ed.SetText(s.texts[slot], nil)
	if u != nil {
		ed.SetMarks(startSlips(s.texts[slot], kind, shell), u)
	}
	ed.OnChange = func(t string, u *gunim.UI) gunim.Intent {
		s.texts[slot] = t
		ed.SetMarks(startSlips(t, kind, shell), u)
		s.m.SetLines(slot, settings.StartLines([]string{t}), u)
		return nil
	}
	pg.body, s.ed = ed, ed
	return pg
}

// startEditor is the editor for a terminal's start lines, named label
// for a screen reader and coloured for a shell of kind: gunim's code
// editor, one field among the dialog's, so Tab moves on and a tab typed
// is a space, with no line numbers, which would make a few commands look
// like a source file.
func startEditor(label string, kind shellsetup.Route) *widget.CodeEditor {
	ed := widget.NewCodeEditor()
	ed.TabMovesOn, ed.Numbers = true, false
	ed.Label = label
	// A command every shell runs, so no shell is shown another's.
	ed.Placeholder = "such as npm run dev, one a line"
	ed.Highlight = pasteColours(kind, false)
	return ed
}

// startSlips are the marks on the lines t written for another shell than
// the terminal's, of kind and called shell. A line the terminal's shell
// has no way of writing is said for what it is, beside the shell the
// terminal starts, as the shell cannot change here.
func startSlips(t string, kind shellsetup.Route, shell string) []widget.CodeMark {
	var marks []widget.CodeMark
	for _, slip := range wrongshell.Find(t, kind) {
		msg := slip.Message
		if slip.Only != "" {
			msg = slip.Only + " command."
			if shell != "" {
				msg += " This terminal starts " + shell + "."
			}
		}
		marks = append(marks, widget.CodeMark{Line: slip.Line, Col: slip.Col, End: slip.End, Message: msg})
	}
	return marks
}

// show shows the page of the pane in slot, coming from from, and the
// note on how lines are typed while it takes lines.
func (s *startLines) show(slot string, from geom.Point, u *gunim.UI) {
	if slot == s.shown {
		return
	}
	s.shown = slot
	s.pages.Show(s.page(slot, u), from, u)
	if on := anim.OnOff(s.ed != nil); s.noteOn.Target() != on {
		s.noteOn.Animate(on, startPick.Get(u.Theme()))
	}
}

// changed are the lines of each terminal whose lines were changed, as a
// layout keeps them, by slot: nil for one whose lines were all taken
// out.
func (s *startLines) changed() map[string][]string {
	out := map[string][]string{}
	for slot, t := range s.texts {
		if now := settings.StartLines([]string{t}); !slices.Equal(now, s.l.Panes[slot].Start) {
			out[slot] = now
		}
	}
	return out
}

// check says why Save cannot keep the lines changed, or "": the layout
// was removed, or changed under them, as the window has the layouts now.
// The app checks again against the file as it saves.
func (s *startLines) check() string {
	lines := s.changed()
	if len(lines) == 0 {
		return ""
	}
	i := slices.IndexFunc(s.w.layouts, func(o settings.SavedLayout) bool { return o.ID == s.l.ID })
	if i < 0 {
		return "Layout removed"
	}
	if settings.StartLinesFit(s.w.layouts[i], s.l, lines) != nil {
		return "Layout changed elsewhere"
	}
	return ""
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

// Layout implements [gunim.Node]: the map across the top, the dialog's
// width and as tall as keeps the window's proportions within its limits,
// then the page, of a height that stays as panes are picked, then the
// line on how the lines are typed.
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
	s.noteAt = geom.Rc(0, y, w, ns.H)
	return geom.Sz(w, y+ns.H)
}

// Paint implements [gunim.Node]: the note fading as a pane that takes no
// lines is picked.
func (s *startLines) Paint(p *paint.Painter, _ gunim.Frame, _ geom.Size, kids gunim.Children) {
	kids.At(0).Paint(p)
	kids.At(1).Paint(p)
	if on := min(max(s.noteOn.Value(), 0), 1); on > 0.01 {
		end := p.Layer(paint.LayerOpts{Bounds: s.noteAt, Opacity: on})
		kids.At(2).Paint(p)
		end()
	}
}

// startPage is a pane's page: its name and where it starts along the
// top, and under them its lines, or why it takes none.
type startPage struct {
	name, where *widget.Label
	body        gunim.Node
}

// Children implements [gunim.Composite].
func (p *startPage) Children() []gunim.Node {
	var out []gunim.Node
	for _, l := range []*widget.Label{p.name, p.where} {
		if l != nil {
			out = append(out, l)
		}
	}
	return append(out, p.body)
}

// Layout implements [gunim.Node]: the name and where along the top, the
// body in the room left under them.
func (p *startPage) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	const gap, apart = 8, 5
	w := c.Max.W
	y := float32(0)
	if n := kids.Len() - 1; n > 0 {
		name := kids.At(0)
		ns := name.Layout(gunim.Constraints{Max: geom.Sz(w, 0)})
		name.Place(geom.Point{})
		y = ns.H
		if n > 1 {
			where := kids.At(1)
			ws := where.Layout(gunim.Constraints{Max: geom.Sz(max(w-ns.W-apart, 0), 0)})
			where.Place(geom.Pt(ns.W+apart, 0))
			y = max(y, ws.H)
		}
		y += gap
	}
	body := kids.At(kids.Len() - 1)
	body.Layout(gunim.Tight(geom.Sz(w, max(c.Max.H-y, 0))))
	body.Place(geom.Pt(0, y))
	return c.Max
}

// Paint implements [gunim.Node].
func (p *startPage) Paint(pt *paint.Painter, _ gunim.Frame, _ geom.Size, kids gunim.Children) {
	for k := range kids.All {
		k.Paint(pt)
	}
}

// startWell is a sentence in the middle of a well the editor's size,
// where a pane that takes no lines would have its editor.
type startWell struct{ says *widget.Label }

func newStartWell(s string) *startWell {
	l := widget.NewLabel(s)
	l.Align = text.AlignCenter
	return &startWell{says: l}
}

// Children implements [gunim.Composite].
func (s *startWell) Children() []gunim.Node { return []gunim.Node{s.says} }

// Layout implements [gunim.Node]: the room given, the sentence in its
// middle.
func (s *startWell) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	const pad = 16
	k := kids.At(0)
	ks := k.Layout(gunim.Constraints{Max: geom.Sz(max(c.Max.W-2*pad, 0), 0)})
	k.Place(geom.Pt((c.Max.W-ks.W)/2, max((c.Max.H-ks.H)/2, 0)))
	return c.Max
}

// Paint implements [gunim.Node].
func (s *startWell) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	th := f.Theme
	r := geom.Rect{Max: box.Point()}
	radius := widget.FieldRadius.Get(th)
	p.RRect(r, radius, paint.Solid(widget.FieldFill.Get(th)))
	p.RRectStroke(r.Inset(geom.Uniform(0.5)), radius, paint.Fill{}, paint.Stroke{Width: 1, Color: widget.FieldBorder.Get(th)})
	kids.At(0).Paint(p)
}

// fadeWords are words on the map and how far they show.
type fadeWords struct {
	s  string
	on *anim.Float
}

// shown is how far the words show, from 0 to 1.
func (w fadeWords) shown() float32 { return min(max(w.on.Value(), 0), 1) }

// inTurn is how far words that have come on as far as on are drawn,
// where words in the same place take turns: going, they are gone half
// way, and coming, they show from half way on, so the two never stand
// on each other.
func inTurn(on float32) float32 { return min(max(2*on-1, 0), 1) }

// fadeText is words that fade across to new ones as they change, the
// old going out before the new come in: the count of a pane's lines,
// and how many more it holds than are drawn. Each change fades from
// where the words stand, so words changed again while they fade carry
// on from there.
type fadeText struct {
	// shown are the words now, last, and the words before fading out.
	shown []fadeWords
}

// now is the words shown now, or "" for none.
func (f *fadeText) now() string {
	if n := len(f.shown); n > 0 && f.shown[n-1].on.Target() == 1 {
		return f.shown[n-1].s
	}
	return ""
}

// set changes the words to s, "" for none, fading across once laid out.
func (f *fadeText) set(s string, th *theme.Live, laid bool) {
	if s == f.now() {
		return
	}
	if !laid {
		f.shown = nil
		if s != "" {
			f.shown = []fadeWords{{s: s, on: anim.NewFloat(1)}}
		}
		return
	}
	q := widget.Quick.Get(th)
	// Words fading out that come back turn from where they stand.
	back := fadeWords{s: s, on: anim.NewFloat(0)}
	f.shown = slices.DeleteFunc(f.shown, func(w fadeWords) bool {
		if w.s == s {
			back = w
			return true
		}
		if w.on.Target() != 0 {
			w.on.Animate(0, q)
		}
		return false
	})
	if s != "" {
		back.on.Animate(1, q)
		f.shown = append(f.shown, back)
	}
}

// Step implements [anim.Stepper]: words faded out are let go.
func (f *fadeText) Step(dt time.Duration) bool {
	moving := false
	for _, w := range f.shown {
		if w.on.Step(dt) {
			moving = true
		}
	}
	f.shown = slices.DeleteFunc(f.shown, func(w fadeWords) bool { return w.on.Target() == 0 && !w.on.Active() })
	return moving
}

// on is how far words s show.
func (f *fadeText) on(s string) float32 {
	for _, w := range f.shown {
		if w.s == s {
			return w.shown()
		}
	}
	return 0
}

// showing is how far any words show, from 0 to 1.
func (f *fadeText) showing() float32 {
	t := float32(0)
	for _, w := range f.shown {
		t += w.shown()
	}
	return min(t, 1)
}

// width is how wide the words are drawn: between the old width and the
// new as they fade across.
func (f *fadeText) width(size float32) float32 {
	var sum, total float32
	for _, w := range f.shown {
		on := w.shown()
		sum += text.Default().Shape(w.s, size).Advance * on
		total += on
	}
	if total < 0.001 {
		return 0
	}
	return sum / total
}

// drawn is how far any words are drawn, from 0 to 1.
func (f *fadeText) drawn() float32 {
	t := float32(0)
	for _, w := range f.shown {
		t += inTurn(w.shown())
	}
	return min(t, 1)
}

// paint draws the words at at in c, the old fading out before the new
// fade in, faded out past room.
func (f *fadeText) paint(p *paint.Painter, th *theme.Live, at geom.Point, size float32, c color.NRGBA, room float32) {
	for _, w := range f.shown {
		if on := inTurn(w.shown()); on > 0.001 {
			text.Default().Shape(w.s, size).PaintFaded(p, at, fade(c, on), room, widget.TextFade.Get(th))
		}
	}
}

// fadeRows are the lines drawn on a pane's box, by row: each fades in
// as it is typed, from where it stands, and out as it is taken out,
// drawn until it has gone.
type fadeRows struct{ rows []fadeWords }

// set has the rows show lines, fading on spring where animate says.
func (r *fadeRows) set(lines []string, spring anim.Spring, animate bool) {
	want := func(on *anim.Float, to float32) {
		switch {
		case !animate:
			on.Jump(to)
		case on.Target() != to:
			on.Animate(to, spring)
		}
	}
	for i, s := range lines {
		if i == len(r.rows) {
			r.rows = append(r.rows, fadeWords{on: anim.NewFloat(0)})
		}
		// A line changed in place is drawn as it is now.
		r.rows[i].s = s
		want(r.rows[i].on, 1)
	}
	for _, w := range r.rows[len(lines):] {
		want(w.on, 0)
	}
	if !animate {
		r.rows = r.rows[:len(lines)]
	}
}

// Step implements [anim.Stepper]: rows faded out are let go.
func (r *fadeRows) Step(dt time.Duration) bool {
	moving := false
	for _, w := range r.rows {
		if w.on.Step(dt) {
			moving = true
		}
	}
	for n := len(r.rows); n > 0 && r.rows[n-1].on.Target() == 0 && !r.rows[n-1].on.Active(); n-- {
		r.rows = r.rows[:n-1]
	}
	return moving
}

// paneFace is what the map says of one pane: its icon and name, the
// machine it runs on when that is not this computer, and its start
// lines, with the springs that show them.
type paneFace struct {
	slot, kind  string
	name, where string
	icon        *icon.Icon
	// editable says the pane takes start lines: a terminal on a machine
	// reached directly.
	editable bool
	lines    []string
	// rows are the lines drawn, coming in and going out.
	rows fadeRows
	// fit is how many rows of lines the box has room for, as last laid
	// out. Where it has fewer than the lines, the last row gives way to
	// more, which says how many lines are not drawn.
	fit  int
	more fadeText
	// sign shows that it holds lines, coming as the first is typed and
	// going as the last is taken out; hover lights it under the pointer;
	// front is how far it is the tab its box shows, for a box of tabs; on
	// how far it is the pane picked; and countOn how far the count of its
	// lines shows by its name, in a box with no room for a line.
	sign, hover, front, on, countOn *anim.Float
}

func newPaneFace(slot string) *paneFace {
	return &paneFace{slot: slot, sign: anim.NewFloat(0), hover: anim.NewFloat(0), front: anim.NewFloat(1), on: anim.NewFloat(0),
		countOn: anim.NewFloat(0)}
}

// springs are the face's moving values.
func (f *paneFace) springs() []anim.Stepper {
	return []anim.Stepper{f.sign, f.hover, f.front, f.on, f.countOn, &f.rows, &f.more}
}

// mapBox is a box on the map, as last laid out: one pane, or a group's
// tabs, with where each tab stands along its top.
type mapBox struct {
	r     geom.Rect
	slots []string
	tabs  []geom.Rect
	// body is the room under the tabs, where the pane shown is said.
	body geom.Rect
	// key names a box of tabs, by its first tab.
	key string
}

// tabbed reports whether the box is a group of more than one tab.
func (b *mapBox) tabbed() bool { return len(b.slots) > 1 }

// paneMap draws a layout's window small, across the room it is given,
// each split by its share: each pane a box where it stands, and a group
// of tabs one box, its tabs along its top. One pane is picked: a mark
// glides to its box, and in a box of tabs, a second one along the tabs
// to its tab, which fades in on a box of tabs picked anew. A click picks
// a pane, as do the arrow keys, which go to the box beside the one
// picked on that side, and along a box's tabs first; Home and End pick
// the first and the last. A click, Enter or Space chooses the pane
// picked, for its lines to be typed.
type paneMap struct {
	anim.Group
	root   *settings.LayoutNode
	faces  map[string]*paneFace
	order  []string
	aspect float32
	picked string
	// came is the pane picked before, which an arrow back to where the
	// user came from goes to among boxes as near and as in line.
	came string
	// OnPick runs as a pane is picked, with from pointing from the box
	// picked before toward the one picked now, along the way it went
	// most: (1, 0) for one to the right.
	OnPick func(slot string, from geom.Point, u *gunim.UI) gunim.Intent
	// OnChoose runs as the pane picked is chosen: clicked, or Enter or
	// Space pressed on it.
	OnChoose func(slot string, u *gunim.UI) gunim.Intent

	boxes []mapBox
	// shows is the tab each box of tabs shows, by its key.
	shows map[string]string
	// mark is where the picked box's mark stands, and ring the focus
	// ring; tabOn is how far each box of tabs shows the mark on its tab
	// picked, by its key.
	mark  *anim.Rect
	ring  *anim.Float
	tabOn map[string]*anim.Float
	laid  bool
	hover string
	click widget.Clicker
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
		mark: anim.NewRect(geom.Rect{}), ring: anim.NewFloat(0), tabOn: map[string]*anim.Float{}}
	m.Add(m.mark, m.ring)
	var walk func(n *settings.LayoutNode)
	walk = func(n *settings.LayoutNode) {
		if n == nil {
			return
		}
		if n.Kind == "item" {
			if f, ok := faces[n.ID]; ok && !slices.Contains(m.order, n.ID) {
				m.order = append(m.order, n.ID)
				m.Add(f.springs()...)
			}
			return
		}
		for _, k := range n.Kids {
			walk(k)
		}
		if n.Kind == "group" {
			key := groupKey(n)
			m.shows[key] = n.Front
			on := anim.NewFloat(0)
			m.tabOn[key] = on
			m.Add(on)
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

// firstEditable is the first pane in reading order that takes lines, or
// else the first pane, or "" with none.
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
	if !slices.Contains(m.order, slot) {
		return
	}
	if slot != m.picked {
		m.came = m.picked
	}
	m.picked = slot
	if b := m.boxOf(slot); b != nil && b.tabbed() {
		m.shows[b.key] = slot
	}
	if u != nil {
		u.Invalidate()
	}
}

// SetLines says the pane in slot holds lines, its sign coming in with
// the first and going with the last, and a line drawn on its box coming
// in as it is typed and going as it is taken out.
func (m *paneMap) SetLines(slot string, lines []string, u *gunim.UI) {
	f, ok := m.faces[slot]
	if !ok {
		return
	}
	f.lines = lines
	var quick anim.Spring
	if u != nil {
		quick = widget.Quick.Get(u.Theme())
	}
	f.rows.set(lines, quick, u != nil)
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

// line is the line drawn in row i of face f, and how far it has come
// in.
func (f *paneFace) line(i int) (string, float32) {
	if i >= len(f.rows.rows) {
		return "", 0
	}
	w := f.rows.rows[i]
	return w.s, w.shown()
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

// choose chooses the pane picked.
func (m *paneMap) choose(u *gunim.UI) {
	if m.OnChoose == nil || m.picked == "" {
		return
	}
	if in := m.OnChoose(m.picked, u); in != nil {
		u.Send(m, in)
	}
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
	if i := slices.Index(b.slots, slot); b.tabbed() && i >= 0 && i < len(b.tabs) {
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
	if s := m.shows[b.key]; slices.Contains(b.slots, s) {
		return s
	}
	return b.slots[0]
}

// beside is the pane the arrow toward d goes to from the one picked:
// along a box's tabs first, then to a box on that side. Of the boxes
// beside the picked one, across from some part of it, the nearest is
// taken, and of those as near, the one most in line with it; with none
// across from it, the box nearest on that side. Of two as near and as
// in line, as the two panes over one across a T, the one the user came
// from is taken, so an arrow and its opposite go back and forth.
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
	const near = 1
	best, bestAway, bestOff, bestFacing := -1, float32(0), float32(0), false
	c := cur.r.Center()
	for i, b := range m.boxes {
		if b.r == cur.r {
			continue
		}
		var away, off float32
		var facing bool
		switch {
		case d.X > 0:
			away, off, facing = b.r.Min.X-cur.r.Max.X, apart(b.r.Min.Y, b.r.Max.Y, cur.r.Min.Y, cur.r.Max.Y, c.Y), overlaps(b.r.Min.Y, b.r.Max.Y, cur.r.Min.Y, cur.r.Max.Y)
		case d.X < 0:
			away, off, facing = cur.r.Min.X-b.r.Max.X, apart(b.r.Min.Y, b.r.Max.Y, cur.r.Min.Y, cur.r.Max.Y, c.Y), overlaps(b.r.Min.Y, b.r.Max.Y, cur.r.Min.Y, cur.r.Max.Y)
		case d.Y > 0:
			away, off, facing = b.r.Min.Y-cur.r.Max.Y, apart(b.r.Min.X, b.r.Max.X, cur.r.Min.X, cur.r.Max.X, c.X), overlaps(b.r.Min.X, b.r.Max.X, cur.r.Min.X, cur.r.Max.X)
		default:
			away, off, facing = cur.r.Min.Y-b.r.Max.Y, apart(b.r.Min.X, b.r.Max.X, cur.r.Min.X, cur.r.Max.X, c.X), overlaps(b.r.Min.X, b.r.Max.X, cur.r.Min.X, cur.r.Max.X)
		}
		if away < -near {
			continue
		}
		better := false
		// tie is whether b is as good as the best so far, where the box
		// the user came from is taken.
		tie := abs32(off-bestOff) < near && slices.Contains(b.slots, m.came)
		switch {
		case best < 0:
			better = true
		case facing != bestFacing:
			better = facing
		case facing:
			// Across from the picked box: the nearest, then the most in
			// line.
			better = away < bestAway-near || away <= bestAway+near && (off < bestOff-near || tie)
		default:
			better = off < bestOff-near || abs32(off-bestOff) < near && (away < bestAway-near || abs32(away-bestAway) <= near && tie)
		}
		if better {
			best, bestAway, bestOff, bestFacing = i, away, off, facing
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

// overlaps reports whether the span lo to hi and the span from to to
// share more than a pixel.
func overlaps(lo, hi, from, to float32) bool { return min(hi, to)-max(lo, from) > 1 }

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
		m.click.Press(e, slices.Index(m.order, m.at(e.Pos)))
		return true
	case gi.PointerUp:
		at := m.at(e.Pos)
		if m.click.Release(e, slices.Index(m.order, at)) {
			m.pick(at, u)
			m.choose(u)
		}
		return true
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
		if e.Mods&^gi.ModShift != 0 || len(m.order) == 0 {
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
		case gi.KeyEnter, gi.KeyKPEnter, gi.KeySpace:
			if e.Mods != 0 {
				return false
			}
			m.choose(u)
			return true
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
// where it runs, and its lines, or that it takes none.
func (f *paneFace) said() string {
	what, ok := map[string]string{settings.LayoutTerminal: "terminal", settings.LayoutCommand: "command", settings.LayoutFiles: "file manager"}[f.kind]
	if !ok {
		what = "pane"
	}
	if f.where != "" {
		what += " on " + f.where
	}
	if f.editable {
		return what + ", " + words.ManyOf(len(f.lines), "start line", "start lines")
	}
	return what + ", takes no start lines"
}

// Layout implements [gunim.Node]: the window's tree across the room
// given, each split by its share, and the marks sent gliding to the pane
// picked.
func (m *paneMap) Layout(c gunim.Constraints, f gunim.Frame, _ gunim.Children) geom.Size {
	th := f.Theme
	size := c.Max
	m.boxes = m.boxes[:0]
	gap := startMapGap.Get(th)
	var walk func(n *settings.LayoutNode, r geom.Rect)
	walk = func(n *settings.LayoutNode, r geom.Rect) {
		if n == nil {
			return
		}
		switch n.Kind {
		case "item":
			if _, ok := m.faces[n.ID]; ok && m.boxOf(n.ID) == nil {
				m.boxes = append(m.boxes, mapBox{r: r, slots: []string{n.ID}, body: r})
			}
			return
		case "group":
			b := mapBox{r: r, body: r, key: groupKey(n)}
			for _, k := range n.Kids {
				if _, ok := m.faces[k.ID]; ok && k.Kind == "item" && m.boxOf(k.ID) == nil && !slices.Contains(b.slots, k.ID) {
					b.slots = append(b.slots, k.ID)
				}
			}
			if len(b.slots) == 0 {
				return
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
	walk(m.root, geom.Rect{Max: size.Point()})
	glide := startPick.Get(th)
	ease := func(v *anim.Float, want float32, spring anim.Spring) {
		if !m.laid {
			v.Jump(want)
		} else if v.Target() != want {
			v.Animate(want, spring)
		}
	}
	// Each box of tabs shows the tab picked, or the one it showed last,
	// the one going fading out as the other fades in, its tab narrowing
	// as the other's widens.
	for i := range m.boxes {
		b := &m.boxes[i]
		shown := m.showing(*b)
		for _, slot := range b.slots {
			ease(m.faces[slot].front, anim.OnOff(slot == shown), glide)
			ease(m.faces[slot].on, anim.OnOff(slot == m.picked), glide)
		}
		if b.tabbed() {
			m.layTabs(b, th)
		}
		for _, slot := range b.slots {
			m.layFace(b.body, m.faces[slot], th)
		}
	}
	// The mark glides over the whole turn of the page, so the two come to
	// rest together.
	box := geom.Rect{}
	if b := m.boxOf(m.picked); b != nil {
		box = b.r
	}
	if !m.laid {
		m.mark.Jump(box)
	} else if m.mark.Target() != box {
		m.mark.Animate(box, glide)
	}
	// The tab's mark fades in on a box of tabs picked anew, and out of
	// the one left; along one box's tabs it glides with the tab shown.
	for key, on := range m.tabOn {
		b := m.boxOf(key)
		ease(on, anim.OnOff(b != nil && b.tabbed() && slices.Contains(b.slots, m.picked)), glide)
	}
	m.laid = true
	return size
}

// The parts of a tab: the room at its sides, its icon's size, and the
// room between its icon and its name; and the least room a tab is given,
// where it is drawn as a dot.
const tabPad, tabIcon, tabIconGap, tabDot = 6, 12, 5, 6

// tabNeed is how wide a tab is with name whole beside its icon, in
// whole pixels, so a tab given it has room for every letter.
func tabNeed(th *theme.Live, name string) float32 {
	run := text.Default().Shape(name, startTabSize.Get(th))
	return float32(math.Ceil(float64(2*tabPad + tabIcon + tabIconGap + run.Advance + 0.5)))
}

// layTabs lays out the tabs of box b along its top. The tab shown takes
// the room its name needs, as far as leaves the rest room for their
// icons. The rest are each as wide as their names, the shortest first,
// as far as the room left holds, and the others narrowed to their
// icons: never between, where a name would show cut. As another tab
// comes to the front, the two change widths together, and only then is a
// name seen cut, fading as its tab narrows.
func (m *paneMap) layTabs(b *mapBox, th *theme.Live) {
	const inset, gap = 4, 2
	n := len(b.slots)
	avail := max(b.r.Size().W-2*inset-gap*float32(n-1), 0)
	iconOnly := float32(2*tabPad + tabIcon)
	need := make([]float32, n)
	for i, slot := range b.slots {
		need[i] = tabNeed(th, m.faces[slot].name)
	}
	least := startTabLeast.Get(th)
	widthsIf := func(j int) []float32 {
		out := make([]float32, n)
		// As wide as its name, or where the rest would have no room for
		// their icons, as wide as shows some of it; and never so wide as
		// squeezes the rest past their icons. In a box too narrow for
		// every icon, it keeps its own, and the rest are dots.
		w := min(need[j], max(avail-float32(n-1)*iconOnly, min(need[j], least)))
		w = max(min(w, avail-float32(n-1)*(tabIcon+4)), min(iconOnly, avail-float32(n-1)*tabDot), 0)
		rest := avail - w
		icons := min(iconOnly, rest/float32(n-1))
		var others []int
		for i := range out {
			if i != j {
				out[i] = icons
				others = append(others, i)
			}
		}
		out[j] = w
		left := rest - icons*float32(n-1)
		slices.SortStableFunc(others, func(x, y int) int { return cmp.Compare(need[x], need[y]) })
		for _, i := range others {
			more := need[i] - icons
			if more > left {
				break
			}
			out[i] = need[i]
			left -= more
		}
		return out
	}
	widths := make([]float32, n)
	total := float32(0)
	for j, slot := range b.slots {
		t := min(max(m.faces[slot].front.Value(), 0), 1)
		if t < 0.001 {
			continue
		}
		for i, w := range widthsIf(j) {
			widths[i] += w * t
		}
		total += t
	}
	if total < 0.001 {
		widths, total = widthsIf(max(slices.Index(b.slots, m.showing(*b)), 0)), 1
	}
	sum := float32(0)
	for i := range widths {
		widths[i] /= total
		sum += widths[i]
	}
	scale := float32(1)
	if sum > avail && sum > 0 {
		scale = avail / sum
	}
	tabH := startTabHeight.Get(th)
	b.tabs = b.tabs[:0]
	x := b.r.Min.X + inset
	for i := range widths {
		w := widths[i] * scale
		b.tabs = append(b.tabs, geom.Rc(x, b.r.Min.Y+inset, w, tabH))
		x += w + gap
	}
	b.body = geom.Rect{Min: geom.Pt(b.r.Min.X, b.r.Min.Y+inset+tabH), Max: b.r.Max}
}

// The parts of a pane's face: the room round it, its icon's size, and
// the room under its name.
const facePad, faceIcon, faceGap = 10, 16, 6

// layFace works out how many of face f's lines box body r has room for,
// and whether the count of its lines shows by its name, where there is
// room for none.
func (m *paneMap) layFace(r geom.Rect, f *paneFace, th *theme.Live) {
	small := smallText.Get(th)
	y := r.Min.Y + facePad + faceIcon + faceGap
	if f.where != "" {
		y += text.Default().Shape(f.where, small).Height() + 4
	}
	row := monoFace(th).Shape("›", small).Height() + 2
	fit := 0
	if room := r.Max.Y - facePad/2 - y; room > 0 && row > 0 {
		fit = int(room / row)
	}
	n := len(f.lines)
	f.fit = fit
	switch {
	case n <= fit:
		f.more.set("", th, m.laid)
	case fit > 1:
		// The last row says how many more there are.
		f.more.set(words.ManyOf(n-(fit-1), "more line", "more lines"), th, m.laid)
	default:
		f.more.set(words.ManyOf(n, "line", "lines"), th, m.laid)
	}
	// With room for one line at most, the count stands by the name, where
	// it leaves the name whole.
	count := false
	if fit <= 1 && n > fit {
		name := text.GoSans(true, false).Shape(f.name, startNameSize.Get(th))
		x := r.Min.X + facePad + faceIcon + 7
		count = x+name.Advance+12+text.Default().Shape(f.more.now(), small).Advance <= r.Max.X-facePad
	}
	want := anim.OnOff(count)
	if !m.laid {
		f.countOn.Jump(want)
	} else if f.countOn.Target() != want {
		f.countOn.Animate(want, widget.Quick.Get(th))
	}
}

// monoFace is the face lines are drawn in, as a shell shows them.
func monoFace(th *theme.Live) *text.Face {
	if mono := widget.MonoFont.Get(th); mono != nil {
		return mono
	}
	return text.Default()
}

// Paint implements [gunim.Node]: each box, a terminal that takes lines
// filled as the editor is and a pane that takes none quieter, the marks
// of the pane picked over them, and the focus ring round the mark.
func (m *paneMap) Paint(p *paint.Painter, f gunim.Frame, _ geom.Size, _ gunim.Children) {
	th := f.Theme
	radius := startMapRadius.Get(th)
	m.cuts = m.cuts[:0]
	mark := m.mark.Value()
	field, quiet := widget.FieldFill.Get(th), widget.CardFill.Get(th)
	for i := range m.boxes {
		b := &m.boxes[i]
		p.RRect(b.r, radius, paint.Solid(mix(quiet, field, m.editableShown(b))))
		p.RRectStroke(b.r.Inset(geom.Uniform(0.5)), radius, paint.Fill{}, paint.Stroke{Width: 1, Color: widget.FieldBorder.Get(th)})
		for j, slot := range b.slots {
			if h := m.faces[slot].hover.Value(); h > 0.01 {
				lit := b.r
				if b.tabbed() {
					lit = b.tabs[j]
				}
				p.RRect(lit, radius, paint.Solid(fade(look.RowHover.Get(th), h)))
			}
		}
	}
	// The mark's fill, under the words.
	p.RRect(mark, radius, paint.Solid(look.RowActive.Get(th)))
	for i := range m.boxes {
		b := &m.boxes[i]
		if !b.tabbed() {
			continue
		}
		if on := min(max(m.tabOn[b.key].Value(), 0), 1); on > 0.01 {
			p.RRect(m.tabMark(b), radius-2, paint.Solid(fade(widget.Accent.Get(th), 0.4*on)))
		}
	}
	for i := range m.boxes {
		b := &m.boxes[i]
		end := m.clip(p, b.r, radius)
		picked := float32(0)
		for _, slot := range b.slots {
			picked = max(picked, min(max(m.faces[slot].on.Value(), 0), 1))
		}
		fill := mix(quiet, field, m.editableShown(b))
		for j, slot := range b.slots {
			if b.tabbed() {
				m.paintTab(p, th, b.tabs[j], m.faces[slot], picked, fill)
			}
		}
		for _, slot := range b.slots {
			// The face going fades out in the first half of the change,
			// and the one coming in in the second, so their words never
			// stand on each other.
			if t := min(max(2*m.faces[slot].front.Value()-1, 0), 1); t > 0.01 {
				m.paintFace(p, th, b.body, m.faces[slot], t, picked, fill)
			}
		}
		end()
	}
	accent := widget.Accent.Get(th)
	p.RRectStroke(mark.Inset(geom.Uniform(0.75)), radius, paint.Fill{}, paint.Stroke{Width: 1.5, Color: accent})
	widget.FocusRing(p, mark, radius, m.ring.Value(), th)
}

// editableShown is how far box b shows a pane that takes lines.
func (m *paneMap) editableShown(b *mapBox) float32 {
	t := float32(0)
	for _, slot := range b.slots {
		if fc := m.faces[slot]; fc.editable {
			t += min(max(fc.front.Value(), 0), 1)
		}
	}
	return min(t, 1)
}

// tabMark is where the mark on box b's tab picked stands: on the tab
// shown, and between two tabs as one comes to the front.
func (m *paneMap) tabMark(b *mapBox) geom.Rect {
	var r geom.Rect
	total := float32(0)
	for i, slot := range b.slots {
		t := min(max(m.faces[slot].front.Value(), 0), 1)
		if t < 0.001 {
			continue
		}
		tab := b.tabs[i]
		r.Min.X += tab.Min.X * t
		r.Min.Y += tab.Min.Y * t
		r.Max.X += tab.Max.X * t
		r.Max.Y += tab.Max.Y * t
		total += t
	}
	if total < 0.001 {
		return b.tabs[max(slices.Index(b.slots, m.showing(*b)), 0)]
	}
	return geom.Rect{Min: geom.Pt(r.Min.X/total, r.Min.Y/total), Max: geom.Pt(r.Max.X/total, r.Max.Y/total)}
}

// clip confines what is drawn to r, until the function it returns is
// called.
func (m *paneMap) clip(p *paint.Painter, r geom.Rect, radius float32) func() {
	return p.Layer(paint.LayerOpts{Bounds: r, Opacity: 1, Clip: true, Radius: radius})
}

// paintTab draws a tab of a box of tabs, picked as far as picked says,
// on fill: its icon, and its name where it has room, which fades out as
// the tab narrows to its icon. The name of the tab its box shows is in
// the text's colour, and the rest are quieter, unless the box is picked,
// when all are. The icon of a pane that takes no lines is quieter, and
// the icon of one that holds lines carries a dot. The whole tab says its
// name in a tooltip where it is cut short.
func (m *paneMap) paintTab(p *paint.Painter, th *theme.Live, r geom.Rect, f *paneFace, picked float32, fill color.NRGBA) {
	ink, faint := widget.Ink.Get(th), look.Faint.Get(th)
	words := mix(faint, ink, max(min(f.front.Value(), 1), picked))
	run := text.Default().Shape(f.name, startTabSize.Get(th))
	named, room := tabName(th, r, f)
	left := r.Min.X + tabPad
	middle := r.Min.X + (r.Size().W-tabIcon)/2
	at := geom.Rc(middle+(left-middle)*named, r.Min.Y+(r.Size().H-tabIcon)/2, tabIcon, tabIcon)
	// A tab too narrow for its icon, in a box of more tabs than it has
	// room for, is a dot, in the accent while it holds lines; widening,
	// it fades to its icon.
	iconOn := min(max((r.Size().W-tabIcon-2)/4, 0), 1)
	if iconOn < 0.99 {
		const dot = 4
		c := mix(words, widget.Accent.Get(th), min(max(f.sign.Value(), 0), 1))
		mid := r.Center()
		p.RRect(geom.Rc(mid.X-dot/2, mid.Y-dot/2, dot, dot), dot/2, paint.Solid(fade(c, 1-iconOn)))
	}
	switch {
	case iconOn > 0.999:
		m.paintIcon(p, th, f, at, words, fill, 1.4)
	case iconOn > 0.01:
		end := p.Layer(paint.LayerOpts{Bounds: r.Inset(geom.Uniform(-4)), Opacity: iconOn})
		m.paintIcon(p, th, f, at, words, fill, 1.4)
		end()
	}
	if named > 0.01 {
		run.PaintFaded(p, geom.Pt(at.Max.X+tabIconGap, r.Min.Y+(r.Size().H-run.Height())/2), fade(words, named), room, widget.TextFade.Get(th))
	}
	if named < 0.99 || run.Advance > room {
		m.cuts = append(m.cuts, mapCut{r: r, words: f.name})
	}
}

// tabName is how far a tab drawn at r for face f shows its name, and
// the room the name has beside the icon. The tab in front shows as much
// of its name as fits; one behind shows its name as far as its tab is
// widened from its icon toward the width that holds the name whole,
// which layTabs gives it or not, so it is seen cut only as it changes.
func tabName(th *theme.Live, r geom.Rect, f *paneFace) (named, room float32) {
	iconOnly := float32(2*tabPad + tabIcon)
	grown := min(max((r.Size().W-iconOnly)/max(tabNeed(th, f.name)-0.5-iconOnly, 1), 0), 1)
	named = max(min(max(f.front.Value(), 0), 1), grown)
	left := r.Min.X + tabPad
	middle := r.Min.X + (r.Size().W-tabIcon)/2
	iconEnd := middle + (left-middle)*named + tabIcon
	return named, max(r.Max.X-tabPad-iconEnd-tabIconGap, 0)
}

// lineShown is how far row i of face f shows its line: as far as the
// line has come in, and on the last row the box has room for, as far as
// the count of the lines past it leaves it. The two take turns there,
// the line gone before the count comes, and back.
func (f *paneFace) lineShown(i int) float32 {
	if i >= f.fit {
		return 0
	}
	_, on := f.line(i)
	if i == f.fit-1 {
		on *= inTurn(1 - f.countShown())
	}
	return on
}

// countShown is how far the count of a face's lines has come in, and
// countDrawn how far it is drawn, taking turns with the line it gives
// way to.
func (f *paneFace) countShown() float32 { return f.more.showing() }
func (f *paneFace) countDrawn() float32 { return f.more.drawn() }

// paintIcon draws face f's icon in r, in words' colour for a pane that
// takes lines and quieter for one that takes none, with a dot on its
// corner, ringed in fill, while it holds lines.
func (m *paneMap) paintIcon(p *paint.Painter, th *theme.Live, f *paneFace, r geom.Rect, words, fill color.NRGBA, thick float32) {
	c := words
	if !f.editable {
		c = look.Faint.Get(th)
	}
	drawIcon(p, f.icon, r, c, thick)
	if sign := min(f.sign.Value(), 1); sign > 0.01 {
		const dot, ring = 6, 1.5
		at := geom.Rc(r.Max.X-dot+2, r.Min.Y-2, dot, dot)
		p.RRect(at.Inset(geom.Uniform(-ring)), dot/2+ring, paint.Solid(fade(fill, sign)))
		p.RRect(at, dot/2, paint.Solid(fade(widget.Accent.Get(th), sign)))
	}
}

// paintFace draws what a pane is in r, at opacity t, on fill, picked as
// far as picked says: its icon and name, the machine it runs on, and,
// for a terminal with lines, as many of them as there is room for, and
// how many more.
func (m *paneMap) paintFace(p *paint.Painter, th *theme.Live, r geom.Rect, f *paneFace, t, picked float32, fill color.NRGBA) {
	if t < 0.999 {
		defer p.Layer(paint.LayerOpts{Bounds: r, Opacity: t})()
	}
	ink := widget.Ink.Get(th)
	// Quieter words, but in the text's colour on the mark of the pane
	// picked.
	quiet := mix(look.Faint.Get(th), ink, picked)
	small := smallText.Get(th)
	top := r.Min.Y + facePad
	m.paintIcon(p, th, f, geom.Rc(r.Min.X+facePad, top, faceIcon, faceIcon), ink, fill, 1.5)
	name := text.GoSans(true, false).Shape(f.name, startNameSize.Get(th))
	x := r.Min.X + facePad + faceIcon + 7
	room := max(r.Max.X-facePad-x, 0)
	if on := min(max(f.countOn.Value(), 0), 1); on > 0.01 {
		// The count by the name, in a box with no room for a line.
		w := f.more.width(small)
		room = max(room-(w+12)*on, 0)
		end := p.Layer(paint.LayerOpts{Bounds: r, Opacity: on})
		f.more.paint(p, th, geom.Pt(r.Max.X-facePad-w, top+(faceIcon-text.Default().Shape("1", small).Height())/2), small, quiet, w+1)
		end()
	}
	m.drawWords(p, th, name, f.name, geom.Pt(x, top+(faceIcon-name.Height())/2), ink, room)
	y := top + faceIcon + faceGap
	if f.where != "" {
		where := text.Default().Shape(f.where, small)
		if y+where.Height() <= r.Max.Y-facePad/2 {
			m.drawWords(p, th, where, f.where, geom.Pt(x, y), quiet, max(r.Max.X-facePad-x, 0))
		}
		y += where.Height() + 4
	}
	sign := min(f.sign.Value(), 1)
	count := f.countShown()
	if sign < 0.01 || len(f.rows.rows) == 0 && count < 0.01 {
		return
	}
	// The lines, as the shell will see them, as many as there is room
	// for, the last row giving way to how many more there are.
	mono := monoFace(th)
	room = max(r.Max.X-facePad-x, 0)
	bottom := r.Max.Y - facePad/2
	row := mono.Shape("›", small).Height() + 2
	end := p.Layer(paint.LayerOpts{Bounds: r, Opacity: sign})
	defer end()
	for i := range min(f.fit, len(f.rows.rows)) {
		line, _ := f.line(i)
		run := mono.Shape("› "+line, small)
		at := y + float32(i)*row
		if at+run.Height() > bottom {
			break
		}
		if on := f.lineShown(i); on > 0.01 {
			m.drawWords(p, th, run, line, geom.Pt(x, at), fade(ink, on), room)
		}
	}
	// The count on the last row, fading out as it moves by the name.
	if inRow := 1 - min(max(f.countOn.Value(), 0), 1); count > 0.01 && inRow > 0.01 {
		at := y + float32(max(f.fit-1, 0))*row
		if h := text.Default().Shape("1", small).Height(); at+h <= bottom {
			layer := p.Layer(paint.LayerOpts{Bounds: r, Opacity: inRow})
			f.more.paint(p, th, geom.Pt(x, at), small, quiet, room)
			layer()
		}
	}
}

// drawWords draws run at at in c, faded out where it runs past room, and
// notes words, whole, for the tooltip where it does.
func (m *paneMap) drawWords(p *paint.Painter, th *theme.Live, run text.Run, words string, at geom.Point, c color.NRGBA, room float32) {
	run.PaintFaded(p, at, c, room, widget.TextFade.Get(th))
	if run.Advance > room {
		m.cuts = append(m.cuts, mapCut{r: geom.Rc(at.X, at.Y, max(room, 1), run.Height()), words: words})
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
