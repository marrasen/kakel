package view

import (
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/filemanager"
	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/widget/dock"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/internal/sessiontest"
	"github.com/marrasen/kakel/screen"
	"github.com/marrasen/kakel/vt"
)

// twoTabs is a window with two tabs, p1's in front, and p2's.
func twoTabs() app.State {
	return app.State{
		Window: 1,
		Panes: []app.Pane{
			{ID: "p1", Title: "Jobs", Kind: app.KindJobs},
			{ID: "p2", Title: "Log", Kind: app.KindLog},
		},
		Layout: tabsLayout("p1", "p1", "p2"), Focus: "p1",
	}
}

// splitTabs is a window split in two, p1 at the left with p3 behind it,
// and p2 at the right.
func splitTabs() app.State {
	st := twoTabs()
	st.Panes = append(st.Panes, app.Pane{ID: "p3", Title: "Copies", Kind: app.KindCopies})
	st.Layout = boxLayout(&app.Box{ID: "s1", Share: 0.5, A: &app.Box{Pane: "p1"}, B: &app.Box{Pane: "p2"}}, "p3")
	return st
}

// settle runs the window a second, for the title bar to slide the tab
// bar into place.
func settle() {
	for range 60 {
		lastWindow.Frame(time.Second / 60)
	}
}

// drain empties the window's intents.
func drain() {
	settle()
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
}

// tabRect is where pane id's tab stands, in the window's space.
func tabRect(t *testing.T, win *Window, id string) geom.Rect {
	t.Helper()
	r, ok := lastUI.Bounds(win.dock)
	if !ok {
		t.Fatal("the dock is not drawn")
	}
	rc, ok := win.dock.TabRect(id)
	if !ok {
		t.Fatalf("the tab of %s is not laid out", id)
	}
	return rc.Add(r.Min)
}

// tabAt is the middle of pane id's tab, in the window's space.
func tabAt(t *testing.T, win *Window, id string) geom.Point {
	t.Helper()
	return tabRect(t, win, id).Center()
}

// click presses and lets go the primary button at p.
func click(p geom.Point, b gi.Button) {
	lastWindow.Input(gi.PointerMove{Pos: p})
	lastWindow.Input(gi.PointerDown{Pos: p, Button: b, Clicks: 1, Time: time.Now()})
	lastWindow.Input(gi.PointerUp{Pos: p, Button: b})
	lastWindow.Frame(time.Second / 60)
}

// paneRect is where pane id stands, in the window's space.
func paneRect(t *testing.T, win *Window, id string) geom.Rect {
	t.Helper()
	r, ok := lastUI.Bounds(win.bareNode(id))
	if !ok {
		t.Fatalf("pane %s is not drawn", id)
	}
	return r
}

// A window's only tab shows in the title row, after the menu button, and
// the window's title fades away for it, first.
func TestTheTabsTakeTheTitleRow(t *testing.T) {
	win, _, publish := windowStage(t)
	one := twoTabs()
	one.Panes, one.Layout = one.Panes[:1], tabsLayout("p1", "p1")
	publish(one)
	settle()
	if !win.tabs.laid || win.bar.Title != "" || win.bar.Subtitle != "" {
		t.Fatalf("with a tab, the title row is laid %v, and says %q, %q", win.tabs.laid, win.bar.Title, win.bar.Subtitle)
	}
	r := tabRect(t, win, "p1")
	lead, _ := lastUI.Bounds(win.leadRow)
	if r.Min.X < lead.Max.X || r.Min.Y > 8 {
		t.Fatalf("the tab stands at %v, the menu button's end at %v", r, lead.Max.X)
	}
	publish(app.State{Window: 1})
	settle()
	if win.tabs.laid || win.bar.Title != app.ProgramName {
		t.Fatalf("with no tab, the title row is laid %v, and says %q", win.tabs.laid, win.bar.Title)
	}
}

// In a split side by side, both bars stand in the title row, the left
// after the menu button, the right before the window's buttons; a split
// one above the other has its lower bar over its pane.
func TestTheBarsAlongTheTopAreTheTitleBar(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(splitTabs())
	settle()
	left, right := tabRect(t, win, "p1"), tabRect(t, win, "p2")
	lead, _ := lastUI.Bounds(win.leadRow)
	if left.Min.Y > 8 || right.Min.Y > 8 {
		t.Fatalf("the tabs stand at %v and %v, not in the title row", left, right)
	}
	if left.Min.X < lead.Max.X {
		t.Fatalf("the left tab at %v is under the menu button, which ends at %v", left, lead.Max.X)
	}
	tail := win.top.(*frame).tail
	tr, _ := lastUI.Bounds(tail)
	if right.Max.X > tr.Min.X {
		t.Fatalf("the right tab at %v is under the window's buttons, from %v", right, tr.Min.X)
	}
	if p := paneRect(t, win, "p2"); p.Min.Y < 28 {
		t.Fatalf("the pane stands at %v, under its bar", p)
	}
	st := splitTabs()
	st.Layout = boxLayout(&app.Box{ID: "s1", Vertical: true, Share: 0.5, A: &app.Box{Pane: "p1"}, B: &app.Box{Pane: "p2"}})
	publish(st)
	settle()
	low, upper := tabRect(t, win, "p2"), paneRect(t, win, "p1")
	if low.Min.Y <= upper.Min.Y {
		t.Fatalf("the lower tab at %v stands over the upper pane at %v", low, upper)
	}
	if p := paneRect(t, win, "p2"); p.Min.Y < low.Max.Y {
		t.Fatalf("the lower pane at %v runs under its bar at %v", p, low)
	}
}

// A chip makes way on a narrow bar, as its fade ends, and comes back.
func TestTheChipsFadeFromANarrowBar(t *testing.T) {
	win, _, publish := windowStageOf(t, geom.Sz(700, 400))
	st := splitTabs()
	st.Layout = boxLayout(&app.Box{ID: "s1", Share: 0.8, A: &app.Box{Pane: "p1"}, B: &app.Box{Pane: "p2"}})
	publish(st)
	settle()
	win.chips.show([]chip{{text: "a chip that takes some room to say"}})
	settle()
	if win.chips.on.Value() > 0.01 {
		t.Fatalf("on a narrow bar the chips show %v", win.chips.on.Value())
	}
	st.Layout = tabsLayout("p1", "p1", "p2")
	publish(st)
	settle()
	if win.chips.on.Value() < 0.99 {
		t.Fatalf("on a wide bar the chips show %v", win.chips.on.Value())
	}
}

// The tabs answer the mouse: a click shows, a middle click closes, the +
// asks for a tab like the one in front.
func TestTheTabsAnswerTheMouse(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(twoTabs())
	drain()
	click(tabAt(t, win, "p2"), gi.ButtonPrimary)
	if in := nextIntent(t); in != (app.FocusPane{Pane: "p2"}) {
		t.Fatalf("a click on the tab sent %#v", in)
	}
	if got := win.dock.Shown(); !slices.Equal(got, []string{"p2"}) {
		t.Fatalf("a click shows %v at once", got)
	}
	if _, ok := lastUI.Bounds(win.bareNode("p2")); !ok {
		t.Fatal("the stage did not take the pane picked at once")
	}
	publish(inFront(twoTabs(), "p2"))
	drain()
	click(tabAt(t, win, "p2"), gi.ButtonMiddle)
	if in := nextIntent(t); in != (app.ClosePane{Pane: "p2"}) {
		t.Fatalf("a middle click sent %#v", in)
	}
}

// A tab dragged over a pane lights where it would land; dropped at the
// edge it splits it, in the middle it joins the group.
func TestATabDroppedOnAPane(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(splitTabs())
	drain()
	r := paneRect(t, win, "p1")
	tab := dock.Drag{ID: "p2", Win: 1}
	drop := func(at geom.Point) dock.Target {
		t.Helper()
		lastWindow.Input(gi.DragOver{Pos: at, Data: tab})
		lastWindow.Frame(time.Second / 60)
		lastWindow.Input(gi.Drop{Pos: at, Data: tab})
		lastWindow.Frame(time.Second / 60)
		for {
			if in, ok := nextIntent(t).(app.MoveTab); ok {
				if in.Pane != "p2" {
					t.Fatalf("the window moved %q", in.Pane)
				}
				return in.To
			}
		}
	}
	if to := drop(geom.Pt(r.Min.X+6, r.Center().Y)); to.Side != dock.Left || to.At != "#gp1" {
		t.Fatalf("at the left edge the tab goes to %+v", to)
	}
	if to := drop(r.Center()); to.Side != dock.Into || to.At != "#gp1" {
		t.Fatalf("in the middle the tab goes to %+v", to)
	}
}

// A pane out of All Panes dragged over a window is taken as a tab is.
func TestAPaneFromAllPanesDropsAsATabDoes(t *testing.T) {
	win, _, publish := windowStage(t)
	st := splitTabs()
	publish(st)
	drain()
	r := paneRect(t, win, "p1")
	at := geom.Pt(r.Max.X-8, r.Center().Y)
	data := app.PaneDrag{Pane: "p2", Window: 1}
	lastWindow.Input(gi.DragOver{Pos: at, Data: data})
	lastWindow.Frame(time.Second / 60)
	lastWindow.Input(gi.Drop{Pos: at, Data: data})
	lastWindow.Frame(time.Second / 60)
	for {
		if in, ok := nextIntent(t).(app.MoveTab); ok {
			if in.Pane != "p2" || in.To.Side != dock.Right {
				t.Fatalf("the window sent %#v", in)
			}
			return
		}
	}
}

// A tab let go outside every window asks for one as large as this one.
func TestATabLetGoOutsideAsksForAWindow(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(twoTabs())
	drain()
	in := win.dockChange(dock.Detach{ID: "p2", At: geom.Pt(10, 20), Size: geom.Sz(1, 1)}, lastUI)
	if in != (app.PaneToNewWindow{Pane: "p2", At: geom.Pt(10, 20), Size: win.size}) {
		t.Fatalf("let go outside, the window asks %#v", in)
	}
}

// A double click on a tab has its pane fill the window, every frame on
// the way the pane at the size it comes to rest at, and its keyboard
// asked for; the restore button or a second double click puts them back.
func TestADoubleClickOnATabFillsTheWindow(t *testing.T) {
	win, _, publish := windowStage(t)
	st := splitTabs()
	publish(st)
	drain()
	half := paneRect(t, win, "p2")
	at := tabAt(t, win, "p2")
	lastWindow.Input(gi.PointerMove{Pos: at})
	lastWindow.Input(gi.PointerDown{Pos: at, Button: gi.ButtonPrimary, Clicks: 2, Time: time.Now()})
	lastWindow.Input(gi.PointerUp{Pos: at, Button: gi.ButtonPrimary})
	lastWindow.Frame(time.Second / 60)
	if win.dock.Maxed() != "p2" || !slices.Equal(win.dock.Shown(), []string{"p2"}) {
		t.Fatalf("maxed %q, showing %v", win.dock.Maxed(), win.dock.Shown())
	}
	if in := nextIntent(t); in != (app.FocusPane{Pane: "p2"}) {
		t.Fatalf("maxed, the window asks %#v", in)
	}
	full := paneRect(t, win, "p2").Size()
	if full.W <= half.Size().W+100 {
		t.Fatalf("the pane is %v, from %v", full, half)
	}
	if _, ok := lastUI.Bounds(win.bareNode("p1")); ok && lastUI.Presence(win.bareNode("p1")) != gunim.Exiting {
		t.Fatal("the pane hidden stays on stage")
	}
	for range 40 {
		lastWindow.Frame(time.Second / 60)
		if got := paneRect(t, win, "p2").Size(); math.Abs(float64(got.W-full.W)) > 0.01 || got.H != full.H {
			t.Fatalf("on the way the pane went from %v to %v", full, got)
		}
	}
	// A state from before says nothing new: it stays.
	publish(st)
	if win.dock.Maxed() != "p2" {
		t.Fatal("a state on its way put the panes back")
	}
	st.Focus = "p2"
	publish(st)
	settle()
	win.run("pane.maximize", lastUI)
	if win.dock.Maxed() != "" || !slices.Equal(win.dock.Shown(), []string{"p1", "p2"}) {
		t.Fatalf("restored: maxed %q, showing %v", win.dock.Maxed(), win.dock.Shown())
	}
}

// A hold over a tab brings it to the front, for a drag to go there.
func TestADragHeldOverATabOpensIt(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(twoTabs())
	drain()
	var bar *dock.Bar
	for _, n := range win.dock.Children() {
		if b, ok := n.(*dock.Bar); ok {
			bar = b
		}
	}
	box, _ := bar.TabRect("p2")
	if !bar.Handle(gi.DragOver{Pos: box.Center(), Data: app.PaneDrag{Pane: "p9", Window: 2}}, lastUI) {
		t.Fatal("the bar did not take the pane")
	}
	lastWindow.Frame(springHold / 2)
	lastWindow.Frame(time.Second / 60)
	if len(lastWindow.Client().Intents()) != 0 {
		t.Fatalf("half way, the bar sent %#v", nextIntent(t))
	}
	lastWindow.Frame(springHold)
	lastWindow.Frame(time.Second / 60)
	if in := nextIntent(t); in != (app.FocusPane{Pane: "p2"}) {
		t.Fatalf("held, the bar sent %#v", in)
	}
}

// springHold is how long a drag is held over a tab before the tab comes
// to the front, as gunim's tab bar has it.
const springHold = 500 * time.Millisecond

// Out of the tree, the Machines pane's list keeps no frames coming for a
// row it had last, and Escape in the list goes back to the pane last
// worked in.
func TestTheServersListRestsOffStage(t *testing.T) {
	win, _, publish := windowStage(t)
	st := withServers(twoPanes("p2", nil))
	st.Focus = "ps"
	st.Working = "p2"
	publish(st)
	drain()
	if !win.listShown || lastUI.Focused() == nil {
		t.Fatal("on stage, the list is not shown or has no keyboard")
	}
	lastWindow.Input(gi.KeyPress{Key: gi.KeyEscape})
	lastWindow.Frame(time.Second / 60)
	if in := nextIntent(t); in != (app.FocusPane{Pane: "p2"}) {
		t.Fatalf("Escape sent %#v", in)
	}
	publish(twoPanes("p2", nil))
	if win.listShown || win.anyBreathing(time.Now()) {
		t.Fatal("off stage, the list is taken as shown")
	}
}

// In a narrow pane the Machines pane's field and Add stay inside it,
// and the title makes room for them.
func TestTheServersButtonsFitANarrowPane(t *testing.T) {
	win, _, publish := windowStageOf(t, geom.Sz(300, 400))
	publish(withServers(app.State{}))
	settle()
	pane, _ := lastUI.Bounds(win.serversView)
	for _, n := range []gunim.Node{win.serversView.search, win.serversView.add} {
		r, _ := lastUI.Bounds(n)
		if r.Min.X < pane.Min.X || r.Max.X > pane.Max.X || r.Empty() {
			t.Fatalf("%T is at %v in a pane at %v", n, r, pane)
		}
	}
	if win.serversView.headShown {
		t.Fatal("the title shows in a pane too narrow for it")
	}
}

// twoTermTabs is twoTabs with a terminal in each tab, and the sessions
// typed into, by pane.
func twoTermTabs(t *testing.T, sh *screen.Shells) (app.State, map[string]*sessiontest.Typed) {
	t.Helper()
	quiet := screen.Hooks{Output: func() {}, Title: func(string) {}, Exit: func() {}, Clipboard: func(string) {}}
	sessions := map[string]*sessiontest.Typed{}
	for _, id := range []string{"p1", "p2"} {
		s := sessiontest.New()
		sessions[id] = s
		sh.Set(id, screen.Open(s, vt.DefaultPalette(), quiet))
		t.Cleanup(func() { _ = sh.Get(id).T.Close() })
	}
	st := twoTabs()
	st.Panes = []app.Pane{{ID: "p1", Title: "Terminal 1"}, {ID: "p2", Title: "Terminal 2"}}
	return st, sessions
}

// inFront is st with the tab of pane id in front.
func inFront(st app.State, id string) app.State {
	st.Layout = st.Layout.Clone()
	st.Layout.Show(id)
	st.Focus = id
	return st
}

// A switch to another tab shows in motion: what it holds comes in from
// its side. What is typed goes to it from the start, and once settled
// nothing moves.
func TestASwitchedTabComesIn(t *testing.T) {
	win, sh, publish := windowStage(t)
	st, sessions := twoTermTabs(t, sh)
	publish(st)
	settle()
	s := win.stage
	if len(s.covers) != 0 {
		t.Fatalf("the window's first tabs move: %v", s.covers)
	}
	publish(inFront(st, "p2"))
	c := s.covers["p2"]
	if c == nil || c.from != 1 {
		t.Fatalf("switched, the cover is %+v", c)
	}
	if v := c.left.Value(); v <= 0 || v >= 1 || !c.left.Active() {
		t.Fatalf("a moment after the switch, the pane is %v covered", v)
	}
	// Typed while it moves, it reaches the tab picked.
	lastWindow.Input(gi.TextInput{Text: "ls"})
	lastWindow.Frame(time.Second / 60)
	got := sessions["p2"].Sent()
	for end := time.Now().Add(time.Second); !strings.Contains(got, "ls") && time.Now().Before(end); got = sessions["p2"].Sent() {
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(got, "ls") || strings.Contains(sessions["p1"].Sent(), "ls") {
		t.Fatalf("typed as the tab came in, p2 was sent %q and p1 %q", got, sessions["p1"].Sent())
	}
	settle()
	if len(s.covers) != 0 || s.Step(time.Second/60) {
		t.Fatalf("settled, the covers are %v", s.covers)
	}
	// Back to the left, it comes from the left.
	publish(inFront(st, "p1"))
	if c := s.covers["p1"]; c == nil || c.from != -1 {
		t.Fatalf("switched back, the cover is %+v", c)
	}
}

// The tab in front moved away leaves the next one fading in, from
// neither side.
func TestATabMovedAwayFadesTheNextIn(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(twoTabs())
	drain()
	st := twoTabs()
	st.Panes, st.Layout, st.Focus = st.Panes[1:], tabsLayout("p2", "p2"), "p2"
	st.Layout.Windows[0].Root.ID = twoTabs().Layout.Windows[0].Root.ID
	publish(st)
	if c := win.stage.covers["p2"]; c == nil || c.from != 0 {
		t.Fatalf("moved away, the cover is %+v", c)
	}
}

// A click on a tab has its pane come in at once, not as the program
// answers, and once only.
func TestAClickedTabComesInAtOnce(t *testing.T) {
	win, _, publish := windowStage(t)
	st := twoTabs()
	publish(st)
	drain()
	click(tabAt(t, win, "p2"), gi.ButtonPrimary)
	c := win.stage.covers["p2"]
	if c == nil || c.from != 1 {
		t.Fatalf("clicked, the cover is %+v", c)
	}
	publish(inFront(st, "p2"))
	if win.stage.covers["p2"] != c {
		t.Fatal("the program's answer started it over")
	}
}

// A click on the tab in front of the other pane gives it the keyboard.
func TestAClickOnTheOtherPanesFrontTabGivesItTheKeyboard(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(splitTabs())
	drain()
	click(tabAt(t, win, "p2"), gi.ButtonPrimary)
	if in := nextIntent(t); in != (app.FocusPane{Pane: "p2"}) {
		t.Fatalf("the click sent %#v", in)
	}
}

// Maximize Pane is ticked as the pane fills the window, with no state
// from the program.
func TestMaximizePaneIsTicked(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(splitTabs())
	drain()
	ticked := func() bool {
		for m := range win.layout {
			for i, it := range win.layout[m].items {
				if it.id == "pane.maximize" {
					return win.bar.Menus[m].Items[i].Checked
				}
			}
		}
		t.Fatal("no Maximize Pane on the menus")
		return false
	}
	win.run("pane.maximize", lastUI)
	if !ticked() {
		t.Fatal("maxed, it is not ticked")
	}
	win.run("pane.maximize", lastUI)
	if ticked() {
		t.Fatal("restored, it is ticked")
	}
}

// A pane from another window over no pane nor bar lights the window, and
// a drop moves it in.
func TestAPaneFromAnotherWindowOverTheRestMovesIn(t *testing.T) {
	win, _, publish := windowStage(t)
	l := boxLayout(&app.Box{Pane: "p1"})
	id := l.AddWindow(true)
	l.Add("p9", dock.Target{Win: id})
	st := app.State{Panes: []app.Pane{{ID: "p1", Title: "Jobs", Kind: app.KindJobs}}, Layout: l, Focus: "p1"}
	publish(st)
	drain()
	r, _ := lastUI.Bounds(win.dock)
	at := geom.Pt(r.Center().X, r.Max.Y+2)
	data := app.PaneDrag{Pane: "p9", Window: id}
	if !win.dragOverDock(gi.DragOver{Pos: at, Data: data}, lastUI) {
		t.Fatal("the window did not take it")
	}
	settle()
	if win.dropLit.Value() < 0.9 {
		t.Fatalf("the window is lit %v", win.dropLit.Value())
	}
	if !win.dragOverDock(gi.Drop{Pos: at, Data: data}, lastUI) {
		t.Fatal("the drop was not taken")
	}
	lastWindow.Frame(time.Second / 60)
	if in := nextIntent(t); in != (app.PaneToWindow{Pane: "p9"}) {
		t.Fatalf("the drop sent %#v", in)
	}
}

// All Panes puts a tile where the dock puts its pane, in a split one
// above the other too.
func TestAPanesTileLandsWhereTheDockPutsIt(t *testing.T) {
	win, _, publish := windowStage(t)
	b := &app.Box{ID: "s1", Vertical: true, Share: 0.5, A: &app.Box{Pane: "p1"}, B: &app.Box{Pane: "p2"}}
	st := twoTabs()
	st.Layout = boxLayout(b)
	publish(st)
	settle()
	stage, _ := lastUI.Bounds(win.stage)
	for _, id := range []string{"p1", "p2"} {
		got, ok := placeIn(b, id, stage, lastUI.Theme())
		want := paneRect(t, win, id)
		if !ok || math.Abs(float64(got.Min.Y-want.Min.Y)) > 1 || math.Abs(float64(got.Max.Y-want.Max.Y)) > 1 {
			t.Errorf("%s lands at %v, and stands at %v", id, got, want)
		}
	}
}

// A file manager pane switched to in its tab comes in, has the keyboard,
// and fills its place once settled; parked again as its tab goes
// behind, it keeps its views.
func TestAFileManagerTabComesIn(t *testing.T) {
	win, _, publish := windowStage(t)
	filemanager.RegisterViews(lastWindow)
	fw := filePane(t, "p1")
	st := twoTabs()
	st.Panes[0] = app.Pane{ID: "p1", Title: "dir", Kind: app.KindFileManager}
	st = inFront(st, "p2")
	publish(st)
	fw.Attach(lastWindow.Client(), app.FilePaneHost("p1"))
	var listing gunim.Node
	framesUntil(t, "the file manager has its views", func() bool {
		listing = filemanager.FocusIn(lastUI, app.FilePaneViews("p1"))
		return listing != nil
	})
	settle()
	publish(inFront(st, "p1"))
	if c := win.stage.covers["p1"]; c == nil || c.from != -1 {
		t.Fatal("the file manager's tab did not come in")
	}
	framesUntil(t, "the listing has the keyboard", func() bool { return lastUI.Focused() == listing })
	settle()
	stage, _ := lastUI.Bounds(win.stage)
	if r, ok := lastUI.Bounds(win.fmHosts["p1"]); !ok || r.Max != stage.Max || r.Min.Y-stage.Min.Y > dock.BarSpace.Default() || len(win.stage.covers) != 0 {
		t.Fatalf("settled, the file manager is at %v on a stage at %v, %v covered", r, stage, win.stage.covers)
	}
	publish(inFront(st, "p2"))
	settle()
	if filemanager.FocusIn(lastUI, app.FilePaneViews("p1")) != listing {
		t.Fatal("parked after its tab came in, the file manager lost its views")
	}
}
