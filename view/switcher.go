package view

import (
	"image/color"
	"math"
	"slices"
	"strconv"
	"time"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/look"
	"github.com/marrasen/kakel/ui"
	"github.com/marrasen/kakel/winkeys"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/text"
	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"
)

// The pane switcher, All Panes: every pane of every window shown live
// and shrunk, a card for each window holding its tabs, each split as it
// stands. The panes on stage shrink from where they stand into their
// places, and the rest come in. The arrows or the pointer move the
// ring, and the pane picked grows back into its place: on this stage,
// or in its own window, which comes to the front with it. A pane dragged
// onto another window's card moves there, onto a pane joins it in a
// split, and onto empty room opens a window of its own.

var (
	switcherRing = theme.Color("kakel.switcher.ring", color.NRGBA{R: 0x5e, G: 0x9c, B: 0xff, A: 0xff})
)

// switcher is the overview, over the window while it is open.
type switcher struct {
	anim.Group
	w *Window
	// wins are the windows shown, a card each, and byID their panes.
	wins  []app.OverWindow
	byID  map[string]app.Pane
	tiles []*tile
	cards []*card
	hot   int
	in    *anim.Float
	// picked is the pane chosen, growing to fill the stage; -1 while
	// the choice is open.
	picked int
	size   geom.Size
	// landed is when the pane picked was asked onto the stage; zero
	// until then.
	landed time.Time
	// stageAt is where the stage stands. It is covered whole while the
	// switcher is there, from the first frame to the last: the tiles are
	// the panes, and the panes themselves must not show beside them.
	stageAt geom.Rect
	// mates are the panes that share the picked pane's split, which grow
	// into their places beside it, so the split comes together as it
	// lands rather than appearing once it has.
	mates map[*tile]bool
	// away says the pane picked is in another window, which comes to
	// the front with it once it has grown.
	away bool
	// pressed is the tile the pointer went down on, at pressAt, which
	// is picked when the pointer comes up there, and carried is the
	// tile dragged out of it, held grab from its top left corner.
	pressed *tile
	pressAt geom.Point
	carried *tile
	grab    geom.Point
	// drop is where the pane carried would go, let go where the pointer
	// is, and dropped says it was let go over the switcher, which has
	// asked for it to go there.
	drop    overDrop
	dropped bool
	// lit is how lit drop's half of a pane is.
	lit *anim.Float
}

type tile struct {
	id    string
	title string
	// win and group are the window and the tab it is in.
	win, group int
	box        *anim.Rect
	fade       *anim.Float
	ring       *anim.Float
	// caption is the line over the pane while panes show their titles,
	// drawn shrunk with it, so the tile lands as the pane stands; empty
	// while they do not.
	caption text.Run
	titled  bool
	// full is the size of its place on its window's stage, caption
	// and all.
	full geom.Size
	// mirror draws it while it is a terminal of another window, or one
	// of this window's never drawn here.
	mirror *mirror
	// placed says it has had a place in the overview.
	placed bool
}

// card is a window, as the overview shows it.
type card struct {
	win   int
	own   bool
	label text.Run
	box   *anim.Rect
	fade  *anim.Float
	// lit is how lit it is as the place a pane carried over it would
	// go.
	lit *anim.Float
	// at is its place, and tabs its tabs' places, as last laid out.
	at   geom.Rect
	tabs []tabSlot
	// gone says its window has: it fades away.
	gone bool
}

// tabSlot is a tab's place in its card: at, its window's stage shrunk,
// under its label.
type tabSlot struct {
	group int
	at    geom.Rect
	label text.Run
	front bool
}

// The room the overview leaves, in logical pixels.
const (
	overMargin   = 40
	overCardGap  = 28
	overCardPad  = 12
	overHeader   = 30
	overTabGap   = 16
	overTabLabel = 20
	// overMost is the most a stage is shown at in the overview: never
	// so large it does not read as one.
	overMost = 0.6
)

func newSwitcher(w *Window, wins []app.OverWindow, panes []app.Pane, focus string, u *gunim.UI) *switcher {
	s := &switcher{w: w, in: anim.NewFloat(0), picked: -1, lit: anim.NewFloat(0)}
	s.Add(s.in, s.lit)
	if r, ok := u.Bounds(w.stage); ok {
		s.stageAt = r
	}
	s.sync(wins, panes, u)
	for i, t := range s.tiles {
		if t.id == focus {
			s.hot = i
		}
		// A pane on stage starts where it stands, its caption included;
		// the rest come in to their places.
		if r, ok := s.slotAt(t, u); ok {
			t.box.Jump(r)
			t.fade.Jump(1)
		}
	}
	// This window's card starts as the stage it shrinks from.
	for _, c := range s.cards {
		if c.own && !s.stageAt.Empty() {
			c.box.Jump(s.stageAt)
		}
	}
	return s
}

// overviewOf is every window as st has them: st.Overview, or, published
// by hand without it, as a test does, this window alone.
func overviewOf(st app.State) ([]app.OverWindow, []app.Pane) {
	all := st.AllPanes
	if all == nil {
		all = st.Panes
	}
	if len(st.Overview) > 0 {
		return st.Overview, all
	}
	own := app.OverWindow{ID: st.Window}
	seen := map[*app.Box]bool{}
	for _, t := range st.Tabs {
		if b := st.Groups[t.Pane]; b != nil {
			seen[b] = true
			own.Tabs = append(own.Tabs, app.OverTab{Group: t.Group, Box: b, Pane: t.Pane})
		}
	}
	for _, p := range st.Panes {
		if b := st.Groups[p.ID]; b != nil && seen[b] || slices.ContainsFunc(own.Tabs, func(t app.OverTab) bool { return slices.Contains(boxLeaves(t.Box, nil), p.ID) }) {
			continue
		}
		// A tab of its own, numbered apart from the program's.
		own.Tabs = append(own.Tabs, app.OverTab{Group: -1 - len(own.Tabs), Box: &app.Box{Pane: p.ID}, Pane: p.ID})
	}
	return []app.OverWindow{own}, all
}

// sync takes the windows and panes as they are now: a tile each pane,
// those already shown kept, and a card each window, those gone fading.
func (s *switcher) sync(wins []app.OverWindow, panes []app.Pane, u *gunim.UI) {
	if s.picked >= 0 {
		return
	}
	s.wins = wins
	s.byID = map[string]app.Pane{}
	for _, p := range panes {
		s.byID[p.ID] = p
	}
	hot := ""
	if s.hot >= 0 && s.hot < len(s.tiles) {
		hot = s.tiles[s.hot].id
	}
	old := map[string]*tile{}
	for _, t := range s.tiles {
		old[t.id] = t
	}
	s.tiles = s.tiles[:0]
	cards := map[int]*card{}
	for _, c := range s.cards {
		cards[c.win] = c
	}
	var order []*card
	for n, ow := range wins {
		c := cards[ow.ID]
		if c == nil {
			c = &card{win: ow.ID, box: anim.NewRect(geom.Rect{}), fade: anim.NewFloat(0), lit: anim.NewFloat(0)}
			s.Add(c.box, c.fade, c.lit)
		}
		delete(cards, ow.ID)
		c.own, c.gone = ow.ID == s.w.winID, false
		name := "Window " + strconv.Itoa(n+1)
		if c.own {
			name = "This window"
		}
		c.label = text.Default().Shape(name, 13)
		c.tabs = c.tabs[:0]
		for _, tb := range ow.Tabs {
			title := s.byID[tb.Pane].Title
			c.tabs = append(c.tabs, tabSlot{group: tb.Group, label: text.Default().Shape(title, 12), front: tb.Group == ow.Front})
			for _, id := range boxLeaves(tb.Box, nil) {
				t := old[id]
				if t == nil {
					t = &tile{id: id, box: anim.NewRect(geom.Rect{}), fade: anim.NewFloat(0), ring: anim.NewFloat(0)}
					s.Add(t.box, t.fade, t.ring)
				}
				delete(old, id)
				p := s.byID[id]
				t.title, t.win, t.group = p.Title, ow.ID, tb.Group
				// Every pane lands under its caption, one never on stage
				// as well.
				t.titled = s.w.titles
				if t.titled {
					t.caption = text.Default().Shape(s.w.captionOf(p), smallText.Get(u.Theme()))
				}
				if _, own := s.w.terms[id]; (!own || ow.ID != s.w.winID) && t.mirror == nil {
					if sh := s.w.shells.Get(id); sh != nil {
						t.mirror = newMirror(sh)
					}
				}
				if t.mirror != nil {
					t.mirror.cells.Size = s.w.fontSize
					if t.mirror.cells.Size <= 0 {
						t.mirror.cells.Size = 15
					}
					t.mirror.cells.Faces = s.w.font.Faces
				}
				s.tiles = append(s.tiles, t)
			}
		}
		order = append(order, c)
	}
	// Windows gone fade where they stood.
	for _, c := range s.cards {
		if cards[c.win] == c {
			c.gone = true
			c.fade.Animate(0, widget.Quick.Get(u.Theme()))
			order = append(order, c)
		}
	}
	s.cards = order
	if s.carried != nil && !slices.Contains(s.tiles, s.carried) {
		s.carried = nil
	}
	s.hot = max(0, slices.IndexFunc(s.tiles, func(t *tile) bool { return t.id == hot }))
	if len(s.tiles) == 0 {
		s.cancel(u)
	}
	u.Invalidate()
}

// slotAt is where tile t's pane stands on stage, with the line over it
// while it has one, if it is on stage.
func (s *switcher) slotAt(t *tile, u *gunim.UI) (geom.Rect, bool) {
	if t.win != s.w.winID && len(s.wins) > 1 {
		return geom.Rect{}, false
	}
	if c := s.w.captions[t.id]; t.titled && c != nil {
		if r, ok := u.Bounds(c); ok {
			return r, true
		}
	}
	return s.w.standsAt(t.id, u)
}

// natural is the size pane id draws at, from its terminal's grid or
// its last drawing, or size when it has neither.
func (w *Window) natural(id string, size geom.Size) geom.Size {
	if term, ok := w.terms[id]; ok {
		cell := term.cells.CellSize()
		cols, rows := term.cells.GridSize()
		if n := geom.Sz(cell.W*float32(cols), cell.H*float32(rows)); n.W > 0 && n.H > 0 {
			return n
		}
	}
	if d := w.drawings[id]; d != nil && !d.Recording().Empty() {
		if n := d.Size(); n.W > 0 && n.H > 0 {
			return n
		}
	}
	return size
}

// paintMiniature draws pane id shrunk into r, keeping its shape: a
// terminal live, any other pane as it was last drawn. It reports false
// when there is nothing of it to draw.
func (w *Window) paintMiniature(p *paint.Painter, f gunim.Frame, id string, r geom.Rect, size geom.Size) bool {
	term, live := w.terms[id]
	d := w.drawings[id]
	nat := w.natural(id, size)
	if nat.W <= 0 || nat.H <= 0 || !live && (d == nil || d.Recording().Empty()) {
		return false
	}
	scale := min(r.Size().W/nat.W, r.Size().H/nat.H)
	defer p.Layer(paint.LayerOpts{Bounds: r, Opacity: 1, Clip: true})()
	defer p.Push(paint.Translate(r.Min))()
	defer p.Push(paint.Scale(scale, geom.Point{}))()
	if live {
		term.cells.Paint(p, f, nat, gunim.Children{})
	} else {
		p.Replay(d.Recording())
	}
	return true
}

// paintElsewhere draws pane id of another window shrunk into r: a
// terminal from its mirror, any other pane from what its window shared
// of it. It reports false when there is nothing of it to draw.
func paintElsewhere(p *paint.Painter, f gunim.Frame, t *tile, r geom.Rect) bool {
	var nat geom.Size
	var draw func()
	switch d, ok := sharedOf(t.id); {
	case t.mirror != nil:
		nat = t.mirror.natural(f)
		draw = func() { t.mirror.cells.Paint(p, f, nat, gunim.Children{}) }
	case ok:
		nat = d.size
		draw = func() { p.Replay(d.rec) }
	default:
		return false
	}
	if nat.W <= 0 || nat.H <= 0 {
		return false
	}
	scale := min(r.Size().W/nat.W, r.Size().H/nat.H)
	defer p.Layer(paint.LayerOpts{Bounds: r, Opacity: 1, Clip: true})()
	defer p.Push(paint.Translate(r.Min))()
	defer p.Push(paint.Scale(scale, geom.Point{}))()
	draw()
	return true
}

// padded is r, a pane's place, less the room round a terminal's cells
// when pane id is a terminal: where its tile's picture lands.
func (w *Window) padded(id string, r geom.Rect, u *gunim.UI) geom.Rect {
	switch w.kindOf(id) {
	case app.KindTerminal, app.KindLog:
		// Drawn by a terminal, which bareNode holds in from the edges.
	default:
		return r
	}
	return r.Inset(geom.Uniform(termPadding.Get(u.Theme())))
}

// standsAt is where pane id stands on stage now, if it is on stage.
func (w *Window) standsAt(id string, u *gunim.UI) (geom.Rect, bool) {
	if term, ok := w.terms[id]; ok {
		if r, ok := u.Bounds(term); ok {
			return r, true
		}
	}
	if n := w.drawn[id]; n != nil {
		return u.Bounds(n)
	}
	return geom.Rect{}, false
}

// stageOf is the size of window win's stage: as it said, or as this
// window's is when it has not.
func (s *switcher) stageOf(win app.OverWindow) geom.Size {
	if win.Stage.W > 0 && win.Stage.H > 0 {
		return win.Stage
	}
	if r := s.stageAt.Size(); r.W > 0 && r.H > 0 {
		return r
	}
	return geom.Sz(800, 500)
}

// overLayout is where the overview puts each window's card, and each
// tab in it, at scale: every stage shrunk by the same amount, so text
// reads the same size in each.
type overLayout struct {
	scale float32
	cards []geom.Rect
	tabs  [][]geom.Rect
}

// tabGrid is how many columns and rows of tabs a card of n tabs has.
func tabGrid(n int) (cols, rows int) {
	n = max(n, 1)
	cols = int(math.Ceil(math.Sqrt(float64(n))))
	return cols, (n + cols - 1) / cols
}

// cardSize is the size of a card of n tabs, each a stage of size shown
// at scale.
func cardSize(stage geom.Size, n int, scale float32) geom.Size {
	cols, rows := tabGrid(n)
	return geom.Sz(
		2*overCardPad+float32(cols)*stage.W*scale+float32(cols-1)*overTabGap,
		overHeader+overCardPad+float32(rows)*(overTabLabel+stage.H*scale)+float32(rows-1)*overTabGap,
	)
}

// shelves puts cards of sizes in rows, as many to a row as fit in
// width, and returns the cards each row holds. ok is false when one is
// wider than width, or the rows taller than height.
func shelves(sizes []geom.Size, width, height float32) (rows [][]int, ok bool) {
	var row []int
	x, h, total := float32(0), float32(0), float32(0)
	for i, sz := range sizes {
		if sz.W > width {
			return nil, false
		}
		if len(row) > 0 && x+overCardGap+sz.W > width {
			rows = append(rows, row)
			total += h + overCardGap
			row, x, h = nil, 0, 0
		}
		if len(row) > 0 {
			x += overCardGap
		}
		row = append(row, i)
		x += sz.W
		h = max(h, sz.H)
	}
	if len(row) > 0 {
		rows = append(rows, row)
		total += h
	}
	return rows, total <= height
}

// layOut places a card for each of stages, holding as many tabs as
// tabs says, in size: as large as they all fit.
func layOut(size geom.Size, stages []geom.Size, tabs []int) overLayout {
	room := geom.Sz(size.W-2*overMargin, size.H-2*overMargin)
	sizes := func(scale float32) []geom.Size {
		out := make([]geom.Size, len(stages))
		for i, st := range stages {
			out[i] = cardSize(st, tabs[i], scale)
		}
		return out
	}
	lo, hi := float32(0.01), float32(overMost)
	if _, ok := shelves(sizes(hi), room.W, room.H); ok {
		lo = hi
	} else {
		for range 24 {
			mid := (lo + hi) / 2
			if _, ok := shelves(sizes(mid), room.W, room.H); ok {
				lo = mid
			} else {
				hi = mid
			}
		}
	}
	out := overLayout{scale: lo, cards: make([]geom.Rect, len(stages)), tabs: make([][]geom.Rect, len(stages))}
	all := sizes(lo)
	rows, _ := shelves(all, room.W, room.H)
	if rows == nil {
		// Too many to fit at all: one row, off the edge.
		rows = [][]int{make([]int, len(stages))}
		for i := range stages {
			rows[0][i] = i
		}
	}
	height := float32(0)
	for i, row := range rows {
		h := float32(0)
		for _, k := range row {
			h = max(h, all[k].H)
		}
		height += h
		if i > 0 {
			height += overCardGap
		}
	}
	y := max(overMargin, (size.H-height)/2)
	for _, row := range rows {
		width, h := float32(0), float32(0)
		for j, k := range row {
			width += all[k].W
			if j > 0 {
				width += overCardGap
			}
			h = max(h, all[k].H)
		}
		x := max(overMargin, (size.W-width)/2)
		for _, k := range row {
			c := geom.Rc(x, y+(h-all[k].H)/2, all[k].W, all[k].H)
			out.cards[k] = c
			cols, _ := tabGrid(tabs[k])
			st := geom.Sz(stages[k].W*lo, stages[k].H*lo)
			for n := range max(tabs[k], 1) {
				col, r := n%cols, n/cols
				tx := c.Min.X + overCardPad + float32(col)*(st.W+overTabGap)
				ty := c.Min.Y + overHeader + float32(r)*(overTabLabel+st.H+overTabGap) + overTabLabel
				out.tabs[k] = append(out.tabs[k], geom.Rc(tx, ty, st.W, st.H))
			}
			x += all[k].W + overCardGap
		}
		y += h + overCardGap
	}
	return out
}

// Transition implements [gunim.Transitioner]: the overview fades in and
// out.
func (s *switcher) Transition(p gunim.Presence, f gunim.Frame) bool {
	switch p {
	case gunim.Entering:
		s.in.Animate(1, widget.Settle.Get(f.Theme))
	case gunim.Exiting:
		s.in.Animate(0, widget.Settle.Get(f.Theme))
	case gunim.Present:
	}
	return !s.in.Active() && !s.moving() && s.onStage(time.Now())
}

// onStage reports whether the pane picked is on the stage, so the
// switcher can go without the stage before it showing for a frame. A
// pane that never gets there lets the switcher go after landWait.
func (s *switcher) onStage(now time.Time) bool {
	if s.picked < 0 || s.picked >= len(s.tiles) || s.away {
		return true
	}
	if s.landed.IsZero() {
		return false
	}
	return s.w.focused == s.tiles[s.picked].id || now.Sub(s.landed) > landWait
}

// awayAfter is how long a pane of another window, picked, grows before
// its window comes to the front with it.
const awayAfter = 250 * time.Millisecond

// landWait is how long the switcher waits for the pane picked to come
// on stage before it goes anyway.
const landWait = 500 * time.Millisecond

func (s *switcher) moving() bool {
	for _, t := range s.tiles {
		if t.box.Active() {
			return true
		}
	}
	return false
}

// Layout implements [gunim.Node]: the overview covers the window.
func (s *switcher) Layout(c gunim.Constraints, f gunim.Frame, _ gunim.Children) geom.Size {
	s.size = c.Max
	for _, t := range s.tiles {
		if t.mirror != nil {
			t.mirror.sync()
		}
	}
	if s.picked < 0 {
		s.place(f)
	}
	// Windows gone are let go once they have faded.
	s.cards = slices.DeleteFunc(s.cards, func(c *card) bool { return c.gone && !c.fade.Active() && c.fade.Value() <= 0.01 })
	return c.Max
}

// place lays the cards out, and moves every card and tile toward its
// place.
func (s *switcher) place(f gunim.Frame) {
	motion := widget.Settle.Get(f.Theme)
	var shown []*card
	var stages []geom.Size
	var tabs []int
	wins := map[int]app.OverWindow{}
	for _, ow := range s.wins {
		wins[ow.ID] = ow
	}
	for _, c := range s.cards {
		if c.gone {
			continue
		}
		shown = append(shown, c)
		stages = append(stages, s.stageOf(wins[c.win]))
		tabs = append(tabs, len(c.tabs))
	}
	lay := layOut(s.size, stages, tabs)
	for i, c := range shown {
		r := lay.cards[i]
		if c.at.Empty() && c.box.Value().Empty() {
			// Coming in, it grows a little into its place.
			c.box.Jump(r.Inset(geom.Uniform(min(r.Size().W, r.Size().H) * 0.06)))
		}
		c.at = r
		c.box.Animate(r, motion)
		c.fade.Animate(1, motion)
		for j := range c.tabs {
			if j < len(lay.tabs[i]) {
				c.tabs[j].at = lay.tabs[i][j]
			}
		}
		stage := stages[i]
		for _, t := range s.tiles {
			if t.win != c.win {
				continue
			}
			j := slices.IndexFunc(c.tabs, func(tb tabSlot) bool { return tb.group == t.group })
			ow := wins[c.win]
			if j < 0 || j >= len(ow.Tabs) {
				continue
			}
			whole := geom.Rect{Max: stage.Point()}
			at, ok := placeIn(ow.Tabs[j].Box, t.id, whole, f.Theme)
			if !ok {
				at = whole
			}
			t.full = at.Size()
			slot := c.tabs[j].at
			r := geom.Rect{
				Min: slot.Min.Add(geom.Pt(at.Min.X*lay.scale, at.Min.Y*lay.scale)),
				Max: slot.Min.Add(geom.Pt(at.Max.X*lay.scale, at.Max.Y*lay.scale)),
			}
			if !t.placed && t.fade.Value() == 0 {
				// Fading in, it starts a little smaller in its place.
				t.box.Jump(geom.Rect{Min: r.Center(), Max: r.Center()}.Inset(geom.Uniform(-min(r.Size().W, r.Size().H) * 0.45)))
			}
			t.placed = true
			t.box.Animate(r, motion)
			if t != s.carried {
				t.fade.Animate(1, motion)
			}
		}
	}
}

// Paint implements [gunim.Node].
func (s *switcher) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, _ gunim.Children) {
	th := f.Theme
	t := min(max(s.in.Value(), 0), 1)
	// The window's own ground, solid once the switcher is in: the panes
	// are shown only as the tiles, not behind them as well.
	scrim := widget.Background.Get(th)
	scrim.A = uint8(float32(scrim.A) * t)
	p.RRect(geom.Rect{Max: box.Point()}, 0, paint.Solid(scrim))
	// The stage is covered at once, and until the switcher has gone:
	// the tiles standing where the panes stand are the panes, zooming,
	// not copies over them.
	p.RRect(s.stageAt, 0, paint.Solid(widget.Background.Get(th)))
	for _, c := range s.cards {
		s.paintCard(p, f, c, t)
	}
	for i, tl := range s.tiles {
		if i == s.picked || s.mates[tl] {
			continue
		}
		s.paintTile(p, f, tl, t)
	}
	s.paintDrop(p, f)
	// The pane picked grows over the rest, and those beside it in its
	// split with it. Their shadows and corners go as they land, so they
	// are the panes on the stage when the switcher goes.
	if s.picked >= 0 && s.picked < len(s.tiles) {
		for _, tl := range s.tiles {
			if s.mates[tl] {
				s.paintTile(p, f, tl, t)
			}
		}
		s.paintTile(p, f, s.tiles[s.picked], t)
	}
}

// paintCard draws card c, at in, the switcher's own fade: a raised
// panel naming its window, and over each of its tabs the tab's name.
func (s *switcher) paintCard(p *paint.Painter, f gunim.Frame, c *card, in float32) {
	th := f.Theme
	r := c.box.Value()
	alpha := min(max(c.fade.Value(), 0), 1) * in
	if alpha < 0.01 || r.Size().W < 2 || r.Size().H < 2 {
		return
	}
	defer p.Layer(paint.LayerOpts{Bounds: r.Inset(geom.Uniform(-30)), Opacity: alpha})()
	p.ShadowRRect(r, 10, paint.Solid(widget.MenuFill.Get(th)), paint.Shadow{Offset: geom.Pt(0, 6), Blur: 24, Color: color.NRGBA{A: 0x70}})
	if lit := min(max(c.lit.Value(), 0), 1); lit > 0.01 {
		ring := switcherRing.Get(th)
		tint := ring
		tint.A = uint8(0x22 * lit)
		ring.A = uint8(float32(ring.A) * lit)
		p.RRect(r, 10, paint.Solid(tint))
		p.RRectStroke(r.Inset(geom.Uniform(1)), 10, paint.Fill{}, paint.Stroke{Width: 2, Color: ring})
	}
	// The labels move and shrink with the card, as it grows from the
	// window it stands for into its place.
	if c.at.Empty() {
		return
	}
	k := r.Size().W / c.at.Size().W
	at := func(q geom.Point) geom.Point {
		return r.Min.Add(geom.Pt((q.X-c.at.Min.X)*k, (q.Y-c.at.Min.Y)*k))
	}
	head := geom.Rect{Min: r.Min, Max: geom.Pt(r.Max.X, r.Min.Y+overHeader*k)}
	func() {
		defer p.Layer(paint.LayerOpts{Bounds: head, Opacity: 1, Clip: true})()
		defer p.Push(paint.Translate(at(c.at.Min.Add(geom.Pt(overCardPad, (overHeader-c.label.Height())/2)))))()
		defer p.Push(paint.Scale(k, geom.Point{}))()
		c.label.Paint(p, geom.Point{}, widget.Ink.Get(th))
	}()
	for _, tb := range c.tabs {
		if tb.at.Empty() {
			continue
		}
		ink := look.Faint.Get(th)
		if tb.front {
			ink = widget.Ink.Get(th)
		}
		line := geom.Rect{Min: at(geom.Pt(tb.at.Min.X, tb.at.Min.Y-overTabLabel)), Max: at(geom.Pt(tb.at.Max.X, tb.at.Min.Y))}
		func() {
			defer p.Layer(paint.LayerOpts{Bounds: line, Opacity: 1, Clip: true})()
			defer p.Push(paint.Translate(line.Min.Add(geom.Pt(0, (overTabLabel*k-tb.label.Height()*k)/2))))()
			defer p.Push(paint.Scale(k, geom.Point{}))()
			tb.label.Paint(p, geom.Point{}, ink)
		}()
	}
}

func (s *switcher) paintTile(p *paint.Painter, f gunim.Frame, tl *tile, t float32) {
	r := tl.box.Value()
	if r.Size().W < 2 || r.Size().H < 2 {
		return
	}
	alpha := min(max(tl.fade.Value(), 0), 1)
	s.paintPane(p, f, tl, r, alpha, t, tl.ring.Value())
}

// paintPane draws tile tl's pane shrunk into r, at alpha, with a
// shadow as dark as shadow says and the ring as lit as ring says.
func (s *switcher) paintPane(p *paint.Painter, f gunim.Frame, tl *tile, r geom.Rect, alpha, shadow, ring float32) {
	th := f.Theme
	close := p.Layer(paint.LayerOpts{Bounds: r.Inset(geom.Uniform(-30)), Opacity: alpha})
	defer close()
	// Square and flat where it stands on the stage, as the pane is, and
	// rounded, lifted, as the switcher comes in.
	radius := 6 * min(max(shadow, 0), 1)
	p.ShadowRRect(r, radius, paint.Solid(widget.Background.Get(th)), paint.Shadow{Offset: geom.Pt(0, 4), Blur: 18, Color: color.NRGBA{A: uint8(0x90 * shadow)}})
	// The caption line, shrunk as the pane is, over it.
	if tl.titled {
		// Shrunk with the tile, and never taller than the line on stage:
		// a terminal's grid can be a little shorter than its pane.
		full := tl.full
		if full.H <= 0 {
			full = s.natural(tl, s.size)
		}
		scale := min(1, r.Size().H/full.H)
		line := r
		line.Max.Y = r.Min.Y + captionHeight*scale
		p.RRect(line, 0, paint.Solid(widget.MenuFill.Get(th)))
		func() {
			defer p.Layer(paint.LayerOpts{Bounds: line, Opacity: 1, Clip: true})()
			defer p.Push(paint.Translate(line.Min))()
			defer p.Push(paint.Scale(scale, geom.Point{}))()
			tl.caption.Paint(p, geom.Pt(10, (captionHeight-tl.caption.Height())/2), look.Faint.Get(th))
		}()
		r.Min.Y = line.Max.Y
	}
	// A terminal live, and any other pane as it was last drawn.
	if tl.win != s.w.winID || !s.w.paintMiniature(p, f, tl.id, r, s.size) {
		if !paintElsewhere(p, f, tl, r) {
			s.paintNothing(p, f, tl, r)
		}
	}
	if on := min(max(ring, 0), 1); on > 0.01 {
		c := switcherRing.Get(th)
		c.A = uint8(float32(c.A) * on)
		grow := 3 * on
		p.RRectStroke(r.Inset(geom.Uniform(-grow)), 6+grow, paint.Fill{}, paint.Stroke{Width: 2, Color: c})
	}
}

// paintNothing draws a pane there is no picture of yet: its name, in
// the middle of its place.
func (s *switcher) paintNothing(p *paint.Painter, f gunim.Frame, tl *tile, r geom.Rect) {
	name := text.Default().Shape(tl.title, 12)
	if name.Advance > r.Size().W-8 || name.Height() > r.Size().H {
		return
	}
	c := r.Center()
	name.Paint(p, geom.Pt(c.X-name.Advance/2, c.Y-name.Height()/2), look.Faint.Get(f.Theme))
}

// natural is the size of tile t's pane on its stage, with its caption
// while it has one.
func (s *switcher) natural(t *tile, size geom.Size) geom.Size {
	n := s.w.natural(t.id, size)
	if t.titled {
		n.H += captionHeight
	}
	return n
}

// placeIn is where pane id stands when group, the arrangement it is in,
// fills r: split as gunim's Split splits, the first half its share of
// the room less the gap between them. ok is false when id is not in it.
func placeIn(group *app.Box, id string, r geom.Rect, th *theme.Live) (geom.Rect, bool) {
	switch {
	case group == nil:
		return geom.Rect{}, false
	case group.Pane != "":
		return r, group.Pane == id
	}
	length := r.Size().W
	if group.Vertical {
		length = r.Size().H
	}
	v := min(max(group.Share, 0), 1)
	gap := widget.SplitGap.Get(th) * min(max(min(v, 1-v)*20, 0), 1)
	first := float32(int((length-gap)*v + 0.5))
	a, b := r, r
	if group.Vertical {
		a.Max.Y = r.Min.Y + first
		b.Min.Y = a.Max.Y + gap
	} else {
		a.Max.X = r.Min.X + first
		b.Min.X = a.Max.X + gap
	}
	if at, ok := placeIn(group.A, id, a, th); ok {
		return at, true
	}
	return placeIn(group.B, id, b, th)
}

// light puts the ring on tile i.
func (s *switcher) light(i int, u *gunim.UI) {
	s.hot = i
	for k, t := range s.tiles {
		to := float32(0)
		if k == i {
			to = 1
		}
		t.ring.Animate(to, widget.Quick.Get(u.Theme()))
	}
	u.Invalidate()
}

// pick chooses tile i: it grows to fill the stage, and the overview
// fades away. A pane of another window grows to fill this one, and that
// window comes to the front with it as it has.
func (s *switcher) pick(i int, u *gunim.UI) {
	if s.picked >= 0 || i < 0 || i >= len(s.tiles) {
		return
	}
	s.picked = i
	t := s.tiles[i]
	stage := geom.Rect{Max: s.size.Point()}
	if r, ok := u.Bounds(s.w.stage); ok {
		stage = r
	}
	settle := widget.Settle.Get(u.Theme())
	t.ring.Animate(0, widget.Quick.Get(u.Theme()))
	s.away = t.win != s.w.winID
	// Into its place in its split, and the panes beside it into theirs;
	// alone, it fills the stage.
	group := s.w.groups[t.id]
	into := stage
	if s.away {
		group = nil
		for _, ow := range s.wins {
			for _, tb := range ow.Tabs {
				if tb.Group == t.group {
					group = tb.Box
				}
			}
		}
		// Another window's: it fills this one, as it will fill its own.
		into = geom.Rect{Max: s.size.Point()}
		stage = into
	}
	s.mates = map[*tile]bool{}
	if r, ok := placeIn(group, t.id, stage, u.Theme()); ok {
		into = r
	}
	t.box.Animate(s.w.padded(t.id, into, u), settle)
	for k, o := range s.tiles {
		if k == i {
			continue
		}
		if r, ok := placeIn(group, o.id, stage, u.Theme()); ok {
			s.mates[o] = true
			o.ring.Animate(0, widget.Quick.Get(u.Theme()))
			o.box.Animate(s.w.padded(o.id, r, u), settle)
		}
	}
	// The rest fade as it grows, shrinking a little where they sit, so
	// none is left standing over the sidebar as the overview goes.
	for k, o := range s.tiles {
		if k == i || s.mates[o] {
			continue
		}
		r := o.box.Target()
		o.fade.Animate(0, widget.Quick.Get(u.Theme()))
		o.box.Animate(r.Inset(geom.Uniform(min(r.Size().W, r.Size().H)*0.08)), widget.Quick.Get(u.Theme()))
	}
	for _, c := range s.cards {
		c.fade.Animate(0, widget.Quick.Get(u.Theme()))
	}
	if s.away {
		// Its window comes to the front once it has grown, with the
		// pane; this one stays as it was under it.
		s.w.closeSwitcher(true, u)
		id := t.id
		u.After(awayAfter, func(u *gunim.UI) { u.Send(s.w, app.FocusPane{Pane: id}) })
		return
	}
	// The pane comes on stage at once, with the keyboard, so it is live
	// the moment it is picked, under the switcher, which covers the
	// stage until the pane has grown into place; then the switcher goes,
	// and the stage shows the pane where it landed.
	s.stageAt = stage
	s.landed = time.Now()
	u.Send(s.w, app.FocusPane{Pane: t.id})
	s.w.closeSwitcher(false, u)
}

// cancel closes the overview without a choice; the panes on stage go
// back where they stood.
func (s *switcher) cancel(u *gunim.UI) {
	if s.picked >= 0 {
		return
	}
	for _, t := range s.tiles {
		if r, ok := s.slotAt(t, u); ok {
			t.box.Animate(r, widget.Settle.Get(u.Theme()))
			continue
		}
		t.fade.Animate(0, widget.Settle.Get(u.Theme()))
	}
	for _, c := range s.cards {
		if c.own && !s.stageAt.Empty() {
			c.box.Animate(s.stageAt, widget.Settle.Get(u.Theme()))
		}
		c.fade.Animate(0, widget.Settle.Get(u.Theme()))
	}
	s.picked = len(s.tiles) // the choice is closed
	s.w.closeSwitcher(true, u)
}

// Focusable implements [gunim.Focusable].
func (s *switcher) Focusable() bool { return s.picked < 0 }

// Handle implements [gunim.Handler].
func (s *switcher) Handle(e input.Event, u *gunim.UI) bool {
	switch e := e.(type) {
	case input.KeyPress:
		switch e.Key {
		case input.KeyLeft:
			s.light(s.toward(-1, 0), u)
		case input.KeyRight:
			s.light(s.toward(1, 0), u)
		case input.KeyUp:
			s.light(s.toward(0, -1), u)
		case input.KeyDown:
			s.light(s.toward(0, 1), u)
		case input.KeyTab:
			step := 1
			if e.Mods&input.ModShift != 0 {
				step = len(s.tiles) - 1
			}
			s.light((s.hot+step)%max(len(s.tiles), 1), u)
		case input.KeyEnter, input.KeyKPEnter, input.KeySpace:
			s.pick(s.hot, u)
		case input.KeyEscape:
			s.cancel(u)
		default:
			// Its shortcut again closes it; held down, it does not
			// open and close over and over.
			if ev, ok := winkeys.Event(e); ok && !e.Repeat {
				if id, bound := s.w.keys.Lookup(ui.ChordOf(ev)); bound && id == "view.switcher" {
					s.cancel(u)
				}
			}
		}
		return true
	case input.PointerMove:
		if s.pressed != nil && s.carried == nil && moved(e.Pos, s.pressAt) {
			s.carry(e.Pos, u)
			return true
		}
		if i := s.tileAt(e.Pos); i >= 0 && i != s.hot {
			s.light(i, u)
		}
		return true
	case input.PointerDown:
		if i := s.tileAt(e.Pos); i >= 0 && e.Button == input.ButtonPrimary {
			// Picked as the pointer comes up, unless it drags the pane
			// away first.
			s.pressed, s.pressAt = s.tiles[i], e.Pos
		} else if i < 0 && s.cardAt(e.Pos) == nil {
			s.cancel(u)
		}
		return true
	case input.PointerUp:
		t := s.pressed
		s.pressed = nil
		if t != nil && s.carried == nil && s.tileAt(e.Pos) == slices.Index(s.tiles, t) {
			s.pick(slices.Index(s.tiles, t), u)
		}
		return true
	case input.DragOver, input.DragLeave, input.Drop:
		return s.dropHere(e, u)
	case input.DragEnd:
		s.dragEnded(e, u)
		return true
	case input.TextInput, input.KeyRelease:
		return true
	}
	return false
}

// toward returns the tile nearest the one with the ring in direction
// dx, dy, or that one when there is none that way.
func (s *switcher) toward(dx, dy float32) int {
	if s.hot < 0 || s.hot >= len(s.tiles) {
		return 0
	}
	from := s.tiles[s.hot].box.Target().Center()
	best, score := s.hot, float32(math.Inf(1))
	for i, t := range s.tiles {
		if i == s.hot {
			continue
		}
		d := t.box.Target().Center().Sub(from)
		along := d.X*dx + d.Y*dy
		if along <= 1 {
			continue
		}
		across := float32(math.Abs(float64(d.X*dy))) + float32(math.Abs(float64(d.Y*dx)))
		if sc := along + 2*across; sc < score {
			best, score = i, sc
		}
	}
	return best
}

func (s *switcher) tileAt(p geom.Point) int {
	for i, t := range s.tiles {
		if t.box.Value().Contains(p) {
			return i
		}
	}
	return -1
}

// cardAt returns the card under p, or nil.
func (s *switcher) cardAt(p geom.Point) *card {
	for _, c := range s.cards {
		if !c.gone && c.box.Value().Contains(p) {
			return c
		}
	}
	return nil
}
