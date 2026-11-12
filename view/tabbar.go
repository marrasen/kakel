package view

import (
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/marrasen/kakel/app"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/icon"
	"github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"
	"github.com/marrasen/gunim/widget/dock"
)

// The window's panes stand in gunim's dock: each part of a split has a
// bar of tabs, and each tab is a pane. The bars along the window's top
// are its title bar: what stands there, the menu button at the left and
// the chips and the window's buttons at the right, leaves them room, and
// the room after their tabs moves the window. A bar lower down, in a
// split one above the other, stands over its pane. Here the dock draws
// the bars and the dividers, and the stage the panes, where the dock says.
//
// A click shows a tab; a middle click or its × closes it; the + opens a
// tab like the one in front, and its right click offers a terminal or a
// file manager. A tab dragged along its bar moves along it, onto another
// bar joins it, to a pane's edge splits it, into another window moves
// there, and let go outside every window opens one of its own. A double
// click on a tab has the pane fill the window.

// newDock makes the window's dock.
func (w *Window) newDock() *dock.Dock {
	d := dock.NewDock(0)
	d.Plus, d.Caption, d.TopPlain, d.Maximize = true, true, true, true
	d.Face = func(n *dock.Node) dock.Face { return w.tabFace(n.ID) }
	d.OnChange = w.dockChange
	d.OnPlusMenu = func(b *dock.Bar, at geom.Rect, u *gunim.UI) gunim.Intent {
		w.openPlusMenu(b, at, u)
		return nil
	}
	d.Takes = func(data any) bool {
		_, ok := data.(app.PaneDrag)
		return ok
	}
	d.OnDrop = func(group string, dr dock.Dropped, u *gunim.UI) gunim.Intent {
		if pd, ok := dr.Data.(app.PaneDrag); ok {
			return app.MoveTab{Pane: pd.Pane, To: dock.Target{At: group, Index: dr.Index}}
		}
		return nil
	}
	d.OnMax = func(item string, u *gunim.UI) gunim.Intent {
		w.showOnStage(u)
		w.tickSwitch("pane.maximize", item != "")
		if item != "" && item != w.focused {
			return app.FocusPane{Pane: item}
		}
		return nil
	}
	return d
}

// dockChange says what the user asked the dock for, as the program's
// intents. What the dock shows at once, the stage does too.
func (w *Window) dockChange(c dock.Change, u *gunim.UI) gunim.Intent {
	switch c := c.(type) {
	case dock.Show:
		// At once, coming in from its side, not as the program answers.
		w.tabsComeIn()
		w.showOnStage(u)
		return app.FocusPane{Pane: c.ID}
	case dock.Close:
		return app.ClosePane{Pane: c.ID}
	case dock.New:
		// Like the tab in front of the bar pressed, in that group.
		l := w.dock.Arrangement()
		if g := l.Node(c.Group); g != nil && g.Front != "" {
			u.Send(w, app.FocusPane{Pane: g.Front})
		}
		return app.NewTab{Group: c.Group}
	case dock.Move:
		return app.MoveTab{Pane: c.ID, To: c.To}
	case dock.Resize:
		return app.SplitMoved{Split: c.ID, Share: c.Share}
	case dock.Detach:
		// The tab's top left corner where the image's was, and the
		// window as large as this one.
		return app.PaneToNewWindow{Pane: c.ID, At: c.At, Size: w.size}
	}
	return nil
}

// tabFace is how pane id's tab looks.
func (w *Window) tabFace(id string) dock.Face {
	ic, ok := kindIcons[w.paneIcon(id, w.kindOf(id))]
	if !ok {
		ic = icon.SquareTerminal
	}
	return dock.Face{Title: w.tabTitle(id), Short: w.tabShort(id), Icon: ic, Closable: true}
}

// tabTitle is what pane id's tab says.
func (w *Window) tabTitle(id string) string {
	for _, p := range w.panes {
		if p.ID == id {
			return w.titleOf(p)
		}
	}
	return ""
}

// tabShort is what pane id's tab says where its title does not fit: the
// pane's own title, where the tab's names the machine before it, as "web:
// logs" does, and "" where nothing is shorter. A title naming the
// machine after, as "PowerShell on XPS25" does, has none: faded where
// it is cut, it still shows where the machine's name starts, as the
// pane's own title alone would not.
func (w *Window) tabShort(id string) string {
	for _, p := range w.panes {
		if p.ID == id {
			if full := w.titleOf(p); p.Title != "" && p.Title != full && strings.HasSuffix(full, p.Title) {
				return p.Title
			}
			return ""
		}
	}
	return ""
}

// paneIcon is the icon pane id shows, kind being what it would show
// anyway: a terminal in another kakel window, watched here, shows that
// window's mark, so it is not taken for one on this computer.
func (w *Window) paneIcon(id, kind string) string {
	if kind == app.KindWaiting {
		// A layout's pane still waiting shows what it waits to be.
		for _, p := range w.panes {
			if p.ID == id {
				return paneKindIcon(p)
			}
		}
	}
	if kind != app.KindTerminal {
		return kind
	}
	for _, p := range w.panes {
		if p.ID == id && slices.ContainsFunc(w.remoteWindows, func(rw app.RemoteWindow) bool { return rw.Name == p.Machine }) {
			return "window"
		}
	}
	return kind
}

// showLayout has the dock show st's layout, and the stage what the dock
// shows, the pane picked in a tab coming in from its side.
func (w *Window) showLayout(st app.State, u *gunim.UI) {
	w.dock.SetWin(st.Window, u)
	if reflect.DeepEqual(st.Layout, w.docked) {
		w.dock.Refresh(u)
	} else {
		w.docked = st.Layout.Clone()
		w.dock.SetLayout(w.docked, u)
	}
	if st.Focus != w.dockFocus {
		// Only as it changes: a state still on its way, from before a
		// double click maximized another pane, says nothing new.
		w.dockFocus = st.Focus
		w.dock.SetActive(st.Focus, u)
	}
	w.tabsComeIn()
	w.showOnStage(u)
	w.settleTitleRow(u.Theme())
	u.Invalidate()
}

// tabsComeIn has the pane newly in front of each group, as the dock has
// it, come in: from the side its tab stands on, or where the tab before
// has left the group, moved or closed, simply fading in.
func (w *Window) tabsComeIn() {
	fronts := map[string]string{}
	var walk func(n *dock.Node)
	walk = func(n *dock.Node) {
		if n.Kind == dock.Group {
			fronts[n.ID] = n.Front
			if old, ok := w.fronts[n.ID]; ok && old != n.Front {
				i := slices.IndexFunc(n.Kids, func(k *dock.Node) bool { return k.ID == old })
				j := slices.IndexFunc(n.Kids, func(k *dock.Node) bool { return k.ID == n.Front })
				from := float32(0)
				switch {
				case i < 0:
				case j < i:
					from = -1
				default:
					from = 1
				}
				w.stage.comeIn(n.Front, from)
			}
		}
		for _, k := range n.Kids {
			walk(k)
		}
	}
	l := w.dock.Arrangement()
	if win := l.Window(w.dock.Win()); win != nil && win.Root != nil {
		walk(win.Root)
	}
	w.fronts = fronts
}

// showOnStage has the stage hold the panes the dock shows.
func (w *Window) showOnStage(u *gunim.UI) {
	shown := w.dock.Shown()
	w.stage.show(shown, w.bareNode, u)
	on := map[string]bool{}
	for _, id := range shown {
		on[id] = true
	}
	w.parkFilePanes(on, u)
}

// groupTabs is how many tabs stand in the group of the pane with the
// keyboard.
func (w *Window) groupTabs() int {
	l := w.dock.Arrangement()
	if g := l.GroupOf(w.focused); g != nil {
		return len(g.Kids)
	}
	return 0
}

// inSplit reports whether more than one pane shows.
func (w *Window) inSplit() bool { return len(w.dock.Shown()) > 1 }

// frame lays the window out: the status line along the bottom, and over
// the rest the dock, its top row the title bar, with the stage under
// that row, and the parts of the title bar over the dock at each end.
type frame struct {
	w          *Window
	lead, tail gunim.Node
	chips      *chipBar
	// open is how much of the title row shows, from 0 to 1, and top where
	// it stands.
	open *anim.Float
	top  float32
	// controls is how wide the window's buttons are, as last laid out.
	controls float32
}

// Children implements [gunim.Composite].
func (r *frame) Children() []gunim.Node {
	return []gunim.Node{r.w.onStage, r.w.dock, r.lead, r.tail, r.w.status}
}

// chipsLeast is the room a tab keeps on a bar sharing the title row with
// the chips, which fade away from a bar narrower than that.
const chipsLeast = 160

// Layout implements [gunim.Node].
func (r *frame) Layout(c gunim.Constraints, f gunim.Frame, kids gunim.Children) geom.Size {
	size := c.Max
	th := f.Theme
	barH := dock.BarHeight.Get(th)
	open := min(max(r.open.Value(), 0), 1)
	y := barH*open - barH
	r.top = y
	rowC := gunim.Constraints{Min: geom.Sz(0, barH), Max: geom.Sz(size.W, barH)}
	// The status line first, for what it leaves.
	status := kids.At(4)
	ss := status.Layout(gunim.Constraints{Min: geom.Sz(size.W, 0), Max: size})
	status.Place(geom.Pt(0, size.H-ss.H))
	// The bar under the window's buttons has room for the chips, or they
	// make way for its tabs.
	tail := kids.At(3)
	bar, limited := r.w.dock.BarAt(geom.Pt(size.W-1, 0))
	r.chips.limited, r.chips.room = limited, bar.Size().W-chipsLeast-r.controls
	ts := tail.Layout(rowC)
	r.controls = ts.W - r.chips.wide*min(max(r.chips.on.Value(), 0), 1)
	tail.Place(geom.Pt(size.W-ts.W, y))
	lead := kids.At(2)
	ls := lead.Layout(gunim.Constraints{Min: geom.Sz(0, barH), Max: geom.Sz(max(size.W-ts.W, 0), barH)})
	lead.Place(geom.Pt(0, y))
	// The dock's top row stands in the title row, and it goes as far
	// down as the status line.
	h := max(size.H-ss.H-y, barH)
	r.w.dock.Lead, r.w.dock.Corner = ls.W, ts.W
	d := kids.At(1)
	d.Layout(gunim.Tight(geom.Sz(size.W, h)))
	d.Place(geom.Pt(0, y))
	// Under the title row, the stage, placing the panes as the dock laid
	// them out just now.
	r.w.stage.origin = geom.Pt(0, barH)
	s := kids.At(0)
	s.Layout(gunim.Tight(geom.Sz(size.W, max(h-barH, 0))))
	s.Place(geom.Pt(0, y+barH))
	return size
}

// Paint implements [gunim.Node]: the title bar's fill, behind its parts
// and the top row of tabs, and what stands on it.
func (r *frame) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	if h := dock.BarHeight.Get(f.Theme) + r.top; h > 0 {
		p.RRect(geom.Rc(0, 0, box.W, h), 0, paint.Solid(widget.MenubarFill.Get(f.Theme)))
	}
	for k := range kids.All {
		k.Paint(p)
	}
}

// rowFadeOut and rowFadeIn are how the window's title fades as the title
// row changes between it and the tabs: going quickly, coming a little
// slower.
var (
	rowFadeOut = anim.Tween{Duration: 110 * time.Millisecond, Ease: anim.EaseInOut}
	rowFadeIn  = anim.Tween{Duration: 180 * time.Millisecond, Ease: anim.EaseInOut}
)

// titleRow is the state of the title row: laid says it is laid out for
// tabs, which leave the menu button only its own room; otherwise the
// window's title follows the button, fading as far as titleFade says.
type titleRow struct {
	laid      bool
	titleFade *anim.Float
}

func newTitleRow() *titleRow { return &titleRow{titleFade: anim.NewFloat(1)} }

// settleTitleRow moves the title row toward showing the tabs, when the
// window has panes, or else the window's title. The title fades out
// before the row changes, and in after. Called as the tabs change and as
// the window lays out, each step happens once the one before has finished.
func (w *Window) settleTitleRow(th *theme.Live) {
	t := w.tabs
	if t.laid {
		// As wide as the menu button, in the theme on now.
		w.barBox.Width = widget.MenubarHeight.Get(th) + 12
	}
	switch want := len(w.panes) > 0; {
	case want == t.laid:
		if !t.laid {
			t.titleFade.Animate(1, rowFadeIn)
		}
	case t.laid:
		// The tabs have gone with their panes; the title comes.
		t.laid = false
		w.barBox.Width = 0
		w.bar.Title, w.bar.Subtitle = app.ProgramName, w.paneTitle
		w.leadRow.Grow(w.barFade, 1)
		t.titleFade.Jump(0)
		t.titleFade.Animate(1, rowFadeIn)
	default:
		t.titleFade.Animate(0, rowFadeOut)
		if !t.titleFade.Active() {
			t.laid = true
			w.barBox.Width = widget.MenubarHeight.Get(th) + 12
			w.bar.Title, w.bar.Subtitle = "", ""
			w.leadRow.Grow(w.barFade, 0)
		}
	}
}

// titleFader draws the menu bar it holds with its button as it is, and
// the window's title after the button faded by titleFade, so the title
// can fade without the button.
type titleFader struct {
	child gunim.Node
	row   *titleRow
}

// Children implements [gunim.Composite].
func (t *titleFader) Children() []gunim.Node { return []gunim.Node{t.child} }

// Layout implements [gunim.Node].
func (t *titleFader) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	k := kids.At(0)
	s := k.Layout(c)
	k.Place(geom.Point{})
	return s
}

// Paint implements [gunim.Node].
func (t *titleFader) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	fade := min(max(t.row.titleFade.Value(), 0), 1)
	if fade >= 1 {
		kids.At(0).Paint(p)
		return
	}
	// The bar's fill stays as the title fades.
	p.RRect(geom.Rect{Max: box.Point()}, 0, paint.Solid(widget.MenubarFill.Get(f.Theme)))
	// The compact menu's button is the bar's height and a little more,
	// 4 in from the bar's start.
	button := widget.MenubarHeight.Get(f.Theme) + 8
	func() {
		defer p.Layer(paint.LayerOpts{Bounds: geom.Rc(0, 0, button, box.H), Opacity: 1, Clip: true})()
		kids.At(0).Paint(p)
	}()
	if fade > 0 && box.W > button {
		defer p.Layer(paint.LayerOpts{Bounds: geom.Rc(button, 0, box.W-button, box.H), Opacity: fade, Clip: true})()
		kids.At(0).Paint(p)
	}
}

// dragOverDock hands the dock a tab, or a pane out of All Panes, dragged
// over the panes, in its own space: where it would land lights, and a
// drop puts it there. From another window, anywhere the dock takes it
// not, as over the status line, the window lights up, and a drop moves it
// in as a tab. It reports whether it took the drag.
func (w *Window) dragOverDock(e input.Event, u *gunim.UI) bool {
	at, ok := u.Bounds(w.dock)
	if !ok {
		return false
	}
	conv := func(data any) (dock.Drag, bool) {
		switch d := data.(type) {
		case dock.Drag:
			return d, true
		case app.PaneDrag:
			return dock.Drag{ID: d.Pane, Win: d.Window, Grab: d.Grab}, true
		}
		return dock.Drag{}, false
	}
	switch ev := e.(type) {
	case input.DragOver:
		d, ok := conv(ev.Data)
		if !ok {
			return false
		}
		ev.Data, ev.Pos = d, ev.Pos.Sub(at.Min)
		if w.dock.DragEvent(ev, u) {
			w.dropLit.Animate(0, widget.Quick.Get(u.Theme()))
			u.Invalidate()
			return true
		}
		if d.Win == w.dock.Win() {
			w.dropLit.Animate(0, widget.Quick.Get(u.Theme()))
			u.Invalidate()
			return false
		}
		u.AnswerDrag(widget.DropHint{Text: dock.DockHere.Get(u.Theme()), Effect: widget.DropMove})
		w.dropLit.Animate(1, widget.Quick.Get(u.Theme()))
		u.Invalidate()
		return true
	case input.DragLeave:
		w.dropLit.Animate(0, widget.Quick.Get(u.Theme()))
		u.Invalidate()
		return w.dock.DragEvent(ev, u)
	case input.Drop:
		d, ok := conv(ev.Data)
		if !ok {
			return false
		}
		w.dropLit.Animate(0, widget.Settle.Get(u.Theme()))
		u.Invalidate()
		ev.Data, ev.Pos = d, ev.Pos.Sub(at.Min)
		if w.dock.DragEvent(ev, u) {
			return true
		}
		if d.Win == w.dock.Win() {
			return false
		}
		u.Send(w, app.PaneToWindow{Pane: d.ID})
		return true
	}
	return false
}

// openPlusMenu offers what a new tab can hold, on the machine of the
// pane in front of bar b, in that group: a terminal, or a file manager;
// at is where the + is, on the bar.
func (w *Window) openPlusMenu(b *dock.Bar, at geom.Rect, u *gunim.UI) {
	w.closePlusMenu()
	front, group := b.Front(), b.GroupID()
	m := w.machineOf(front)
	menu := widget.NewMenu([]widget.MenuItem{
		{Label: "New Terminal", Icon: icon.SquareTerminal},
		{Label: "New File Manager", Icon: icon.Folder},
	})
	menu.OnPick = func(i int, u *gunim.UI) gunim.Intent {
		w.closePlusMenu()
		if front != "" {
			u.Send(w, app.FocusPane{Pane: front})
		}
		switch i {
		case 0:
			return app.OpenOn{Machine: m, Group: group}
		case 1:
			return app.OpenFilesOn{Machine: m, NewTab: true, Group: group}
		}
		return nil
	}
	w.plusMenu = u.OpenPopup(b, menu, gunim.PopupOptions{
		Anchor:  at,
		Max:     geom.Sz(280, 200),
		Dismiss: func(*gunim.UI) { w.closePlusMenu() },
	})
}

// closePlusMenu closes the +'s menu, if it is open.
func (w *Window) closePlusMenu() {
	if w.plusMenu != nil {
		w.plusMenu.Close()
		w.plusMenu = nil
	}
}
