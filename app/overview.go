package app

import (
	"strconv"
	"time"

	"github.com/marrasen/kakel/keys"
	"github.com/marrasen/kakel/machines"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/driver"
	"github.com/marrasen/gunim/geom"
)

// All Panes shows every window's panes at once: a card for each of
// kakel's windows, holding its tabs, each split as it stands. A pane is
// picked there, or dragged onto another window's card, beside another
// pane, or out to a window of its own.

// OverWindow is one of kakel's windows as All Panes shows it.
type OverWindow struct {
	ID int
	// Tabs are its tabs, in order, and Front the group of the one in
	// front.
	Tabs  []OverTab
	Front int
	// Stage is the size of its stage, and StageAt where the stage is in
	// the window, and Size the window's own size, as it last said, in
	// its logical pixels; zero before it has.
	Stage   geom.Size
	StageAt geom.Rect
	Size    geom.Size
	// Home is where the window is on the screen, in the space of All
	// Panes' own window; empty where that cannot be said, and in the
	// windows' own state.
	Home geom.Rect
}

// OverTab is one tab of a window, how its panes are arranged, and Pane
// the one of them its tab names.
type OverTab struct {
	Group int
	Box   *Box
	Pane  string
}

// Intents for All Panes.
type (
	// StageSized says where the stage of the window it came from is in
	// it, and how large the window is, for All Panes to draw its tabs
	// that shape, and the window as it stands.
	StageSized struct {
		At     geom.Rect
		Window geom.Size
	}
	// ToggleOverview opens All Panes in a window of its own, over the
	// screen the window it came from is on, or closes it.
	ToggleOverview struct{}
	// OverviewDone says All Panes' own window has finished, and may
	// close, and OverviewLeaving that it is on its way.
	OverviewDone    struct{}
	OverviewLeaving struct{}
	// OverviewShown says All Panes opened, or closed with On false, in
	// the window it came from. While it is open the window hears of
	// output from every window's panes, not only its own.
	OverviewShown struct{ On bool }
	// PaneToTab moves a pane onto a tab of its own in the window
	// numbered Window. Asked of the window it is in, it takes the pane
	// out of its split onto a tab of its own there. Dropped on the
	// window's tab bar, or above its stage, Bar is set: the tab goes in
	// front of the tab of group Before, or last for 0, and comes to the
	// front.
	PaneToTab struct {
		Pane   string
		Window int
		Bar    bool
		Before int
	}
	// DockPane takes a pane into a split beside the pane Beside, in
	// whichever window that is: to its right, or below it with
	// Vertical, or to its left or above it with First.
	DockPane struct {
		Pane, Beside    string
		Vertical, First bool
	}
)

// overviewIn takes the intents about All Panes that leave the window
// in front as it is: a window behind says what it says without coming
// forward. It reports whether in was one.
func (a *app) overviewIn(w *ownWin, in any) bool {
	switch in := in.(type) {
	case StageSized:
		if w.fresh {
			// A window opened from All Panes has shown, and taken the
			// keyboard: All Panes, over it, takes it back.
			w.fresh = false
			if a.over.c != nil && !a.over.closing {
				a.over.c.ToFront()
			}
		}
		if w.stage == in.At && w.size == in.Window {
			a.quiet = true
			return true
		}
		w.stage, w.size = in.At, in.Window
		// Worth showing only where All Panes is open.
		a.quiet = !a.overviewOpen()
	case OverviewShown:
		w.overview = in.On
	default:
		return false
	}
	return true
}

// handleOverview carries out an intent of All Panes, and reports
// whether it was one.
func (a *app) handleOverview(in any) bool {
	switch in := in.(type) {
	case PaneToTab:
		a.paneToTab(in)
	case DockPane:
		a.dockPane(in)
	case ToggleOverview:
		a.toggleOverview()
	default:
		return false
	}
	return true
}

// overviewOpen reports whether All Panes is open, in a window of its
// own or over one of the windows.
func (a *app) overviewOpen() bool {
	if a.over.c != nil {
		return true
	}
	for _, w := range a.liveWins() {
		if w.overview {
			return true
		}
	}
	return false
}

// overview returns every window as All Panes shows it, oldest first.
func (a *app) overview() []OverWindow {
	var out []OverWindow
	for _, w := range a.liveWins() {
		ow := OverWindow{ID: w.id, Stage: w.stage.Size(), StageAt: w.stage, Size: w.size}
		if g, ok := a.groupOf[a.focusIn(w)]; ok {
			ow.Front = g
		}
		for _, t := range a.tabsOf(w) {
			ow.Tabs = append(ow.Tabs, OverTab{Group: t.Group, Box: a.groups[t.Group].clone(), Pane: t.Pane})
		}
		out = append(out, ow)
	}
	return out
}

// paneToTab carries out in: into another window as a tab of its own,
// or out of its split onto one in its own window.
func (a *app) paneToTab(in PaneToTab) {
	w := a.winByID(in.Window)
	from := a.ownerOf(in.Pane)
	if w == nil || w.gone || from == nil || a.closing[in.Pane] {
		return
	}
	if from != w {
		a.moveToWindow(in.Pane, w)
	} else {
		a.ownTab(in.Pane)
	}
	if g, ok := a.groupOf[in.Pane]; ok && in.Bar && w == a.cur {
		a.moveTab(g, in.Before)
	}
}

// ownTab takes pane id out of its split onto a tab of its own, in its
// window, and reports the group it is in now. A pane alone stays where
// it is.
func (a *app) ownTab(id string) int {
	g, ok := a.groupOf[id]
	if !ok || a.groups[g] == nil || a.groups[g].Pane == id {
		return g
	}
	w := a.ownerOf(id)
	next := a.take(id)
	a.nextGroup++
	a.groups[a.nextGroup] = &Box{Pane: id}
	a.groupOf[id] = a.nextGroup
	if w != nil && a.focusIn(w) == id && next != "" {
		// The tab it left keeps the pane beside it in front.
		a.groupFocus[g] = next
	}
	a.sortTabs()
	return a.nextGroup
}

// dockPane carries out in: the pane leaves its split, and joins the
// pane beside it in one.
func (a *app) dockPane(in DockPane) {
	to := a.ownerOf(in.Beside)
	if in.Pane == in.Beside || to == nil || to.gone || a.ownerOf(in.Pane) == nil || a.closing[in.Pane] || a.closing[in.Beside] {
		return
	}
	g := a.ownTab(in.Pane)
	a.dockGroup(g, in.Beside, in.Vertical, in.First, to)
}

// dockGroup takes group g's panes into a split beside the pane beside,
// in window to, on the side vertical and first say, and gives the pane
// that was in front in g the keyboard.
func (a *app) dockGroup(g int, beside string, vertical, first bool, to *ownWin) {
	into, ok := a.groupOf[beside]
	if !ok || a.groups[g] == nil || into == g || !a.movable(g) || a.closing[beside] || a.ownerOf(beside) != to {
		return
	}
	a.groupToWindow(g, to)
	moved := a.groups[g]
	a.splits++
	box := &Box{
		ID: "s" + strconv.Itoa(a.splits), Vertical: vertical, Share: 0.5, Opening: true,
		A: &Box{Pane: beside}, B: moved,
	}
	if first {
		box.A, box.B = moved, box.A
	}
	a.groups[into] = a.groups[into].replace(beside, box)
	pane := a.tabPane(g)
	for _, id := range moved.leaves(nil) {
		a.groupOf[id] = into
	}
	delete(a.groups, g)
	delete(a.groupFocus, g)
	a.sortTabs()
	if to == a.cur {
		a.focus(pane)
		return
	}
	a.setFocusIn(to, pane)
}

// OverviewTopic is the key All Panes' own window's state is published
// under.
const OverviewTopic = "overview"

// OverState is what All Panes shows in a window of its own.
type OverState struct {
	// Windows are kakel's windows, each saying where it stands, and
	// Panes their panes.
	Windows []OverWindow
	Panes   []Pane
	// Front numbers the window in front, and Focus is its pane with the
	// keyboard, which has the ring as All Panes opens.
	Front int
	Focus string
	// FontSize and Font are what the terminals are drawn in, PaneTitles
	// says panes show a line naming them, and Machines name the machines
	// on those lines.
	FontSize   float32
	Font       Font
	PaneTitles bool
	Machines   []machines.Info
	// Shortcuts are the changes the shortcuts file makes to the keys,
	// one of which closes it.
	Shortcuts []keys.Change
	// Close asks it to close, as its key does in a window it covers.
	Close bool
}

// OverviewOpener opens All Panes' own window over monitor m, in the
// theme named theme from its first frame, and returns its client and
// the window. It runs on a goroutine of its own.
type OverviewOpener func(m driver.Monitor, theme string) (gunim.Client, *gunim.Window, error)

// overState is All Panes' own window, while it is open: its client and
// window, the monitor it covers and where on it it stands, and the
// window to put in front once it has gone.
type overState struct {
	c       *gunim.Client
	gw      *gunim.Window
	mon     driver.Monitor
	at      geom.Rect
	opening bool
	closing bool
	raise   *ownWin
}

// overviewAlone reports whether All Panes opens in a window of its own:
// where kakel can open one and say where its windows are.
func (a *app) overviewAlone() bool {
	return a.openOverview != nil && a.monitors != nil && a.cur != nil && a.cur.gw != nil && len(a.monitors()) > 0
}

// toggleOverview opens All Panes' own window over the monitor the
// window in front is on, or has it close.
func (a *app) toggleOverview() {
	if a.over.c != nil {
		if a.over.closing {
			// Asked twice: it goes now.
			a.closeOverview()
			return
		}
		a.over.closing = true
		a.closeOverviewSoon()
		return
	}
	if a.over.opening || !a.overviewAlone() {
		return
	}
	mon, ok := a.monitorOf(a.cur)
	if !ok {
		return
	}
	a.over.opening = true
	open, theme := a.openOverview, a.st.Theme
	go func() {
		c, gw, err := open(mon, theme)
		a.events <- func() {
			a.over.opening = false
			if err != nil {
				a.failed("Couldn't open All Panes", err.Error())
				return
			}
			if a.gone {
				c.Close()
				return
			}
			a.over = overState{c: &c, gw: gw, mon: mon, at: mon.Bounds}
			if p, ok := gw.Placement(); ok {
				// Where it is: the system may have kept it off the task
				// bar.
				a.over.at = p.Bounds
			}
			_ = c.SetTheme(a.st.Theme)
			a.publishOverview()
			c.ToFront()
			go func() {
				for env := range c.Intents() {
					a.events <- func() {
						// Only this one's, not a closed one's late word.
						if a.over.c != nil && *a.over.c == c {
							a.handleOverviewWin(env.Intent)
						}
					}
				}
				a.events <- func() {
					if a.over.c != nil && *a.over.c == c {
						a.overviewGone()
					}
				}
			}()
		}
	}()
}

// handleOverviewWin carries out what All Panes' own window asks for.
func (a *app) handleOverviewWin(in any) {
	switch in := in.(type) {
	case FocusPane:
		// Its window comes to the front once All Panes has gone, which
		// is over it until then.
		if a.has(in.Pane) {
			a.focus(in.Pane)
			a.over.raise = a.ownerOf(in.Pane)
		}
	case PaneToTab, DockPane:
		a.handleOverview(in)
	case PaneToNewWindow:
		var space *gunim.Window
		if !a.over.closing {
			space = a.over.gw
		}
		a.paneToNewWindowFrom(in, space)
	case OverviewLeaving:
		a.over.closing = true
		a.closeOverviewSoon()
	case OverviewDone:
		a.closeOverview()
	}
}

// overviewWait is the longest All Panes' own window may take going
// before it is closed anyway, as when it draws no frames to go by.
const overviewWait = 3 * time.Second

// closeOverviewSoon closes All Panes' own window once overviewWait has
// passed, unless it has gone by then.
func (a *app) closeOverviewSoon() {
	c := a.over.c
	go func() {
		time.Sleep(overviewWait)
		a.events <- func() {
			if a.over.c == c {
				a.closeOverview()
			}
		}
	}()
}

// closeOverview closes All Panes' own window, if it is open.
func (a *app) closeOverview() {
	if a.over.c == nil {
		return
	}
	a.over.c.Close()
	a.overviewGone()
}

// overviewGone forgets All Panes' own window, gone, and puts the window
// of the pane picked there in front, or the window in front again.
func (a *app) overviewGone() {
	raise := a.over.raise
	a.over = overState{}
	if raise == nil {
		raise = a.cur
	}
	if raise != nil && !raise.gone && !a.gone {
		raise.c.ToFront()
	}
}

// publishOverview shows All Panes' own window every window, where each
// stands on the screen.
func (a *app) publishOverview() {
	if a.over.c == nil {
		return
	}
	mons := a.monitors()
	wins := a.overview()
	if len(wins) == 0 {
		// Nothing left to show.
		a.closeOverview()
		return
	}
	for i := range wins {
		w := a.winByID(wins[i].ID)
		if w == nil || w.gw == nil {
			continue
		}
		if p, ok := w.gw.Placement(); ok {
			wins[i].Home = inSpaceOf(screenRect(p, mons), a.over.at.Min, a.over.mon)
		}
	}
	_ = a.over.c.Publish(OverviewTopic, OverState{
		Windows: wins, Panes: a.allPanes(), Front: a.frontID(), Focus: a.st.Focus,
		FontSize: a.st.FontSize, Font: a.st.Font, PaneTitles: a.st.PaneTitles, Machines: a.st.Machines,
		Shortcuts: a.st.Shortcuts, Close: a.over.closing,
	})
}

// monitorOf returns the monitor window w is on: the one holding the
// middle of it, or the primary one where it cannot say.
func (a *app) monitorOf(w *ownWin) (driver.Monitor, bool) {
	mons := a.monitors()
	if len(mons) == 0 {
		return driver.Monitor{}, false
	}
	if w != nil && w.gw != nil {
		if p, ok := w.gw.Placement(); ok {
			return monitorAt(screenRect(p, mons).Center(), mons), true
		}
	}
	for _, m := range mons {
		if m.Primary {
			return m, true
		}
	}
	return mons[0], true
}

// monitorAt returns the monitor holding point at, or the nearest.
func monitorAt(at geom.Point, mons []driver.Monitor) driver.Monitor {
	best, far := mons[0], float32(-1)
	for _, m := range mons {
		if m.Bounds.Contains(at) {
			return m
		}
		c := m.Bounds.Center().Sub(at)
		if d := c.X*c.X + c.Y*c.Y; far < 0 || d < far {
			best, far = m, d
		}
	}
	return best
}

// screenRect is where a window placed at p stands on the screen: its
// bounds, or the work area of its monitor while it is maximized.
func screenRect(p driver.Placement, mons []driver.Monitor) geom.Rect {
	if !p.Maximized || len(mons) == 0 {
		return p.Bounds
	}
	m := monitorAt(p.Bounds.Center(), mons)
	if !m.WorkArea.Empty() {
		return m.WorkArea
	}
	return m.Bounds
}

// inSpaceOf is r, in screen coordinates, in the logical space of a
// window on monitor m with its top left corner at origin.
func inSpaceOf(r geom.Rect, origin geom.Point, m driver.Monitor) geom.Rect {
	k := m.CoordsPerLogical
	if k <= 0 {
		k = 1
	}
	at := func(p geom.Point) geom.Point {
		return geom.Pt((p.X-origin.X)/k, (p.Y-origin.Y)/k)
	}
	return geom.Rect{Min: at(r.Min), Max: at(r.Max)}
}
