package view

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/access"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/look"
	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/settings"
	"github.com/marrasen/kakel/shellsetup"
	"github.com/marrasen/kakel/themes"
)

// one is a group of the one pane in slot id.
func one(id string) *settings.LayoutNode {
	return &settings.LayoutNode{ID: "#" + id, Kind: "group", Front: id, Kids: []*settings.LayoutNode{{ID: id, Kind: "item"}}}
}

// tabsOf is a group of the panes in slots ids, the first in front.
func tabsOf(ids ...string) *settings.LayoutNode {
	g := &settings.LayoutNode{ID: "#" + ids[0], Kind: "group", Front: ids[0]}
	for _, id := range ids {
		g.Kids = append(g.Kids, &settings.LayoutNode{ID: id, Kind: "item"})
	}
	return g
}

// splitOf is a split of kind, row or column, of kids, the first taking
// share of the room.
func splitOf(id, kind string, share float32, kids ...*settings.LayoutNode) *settings.LayoutNode {
	return &settings.LayoutNode{ID: id, Kind: kind, Share: share, Kids: kids}
}

// shopLayout is a window of four panes in two columns, each split in
// two, the left one wider: the backend and the client here, which type
// lines as they open, a log on www, and the database on db-primary.
func shopLayout() settings.SavedLayout {
	return settings.SavedLayout{ID: "shop", Name: "Shop", Place: &settings.WindowPlace{W: 1600, H: 1000},
		Root: splitOf("#1", "row", 0.55,
			splitOf("#2", "column", 0, one("p1"), one("p2")),
			splitOf("#3", "column", 0.6, one("p3"), one("p4"))),
		Panes: map[string]settings.SavedPane{
			"p1": {Kind: settings.LayoutTerminal, Title: "backend", Folder: "/home/me/src/shop/backend", Start: []string{"go run ./cmd/api"}},
			"p2": {Kind: settings.LayoutTerminal, Title: "client", Folder: "/home/me/src/shop/client", Start: []string{"npm install", "npm run dev"}},
			"p3": {Kind: settings.LayoutTerminal, Title: "log tail", Host: "web", HostID: "s1", Folder: "/var/log/shop"},
			"p4": {Kind: settings.LayoutTerminal, Host: "db-primary", HostID: "s2"},
		}}
}

// mixedLayout is a window of a terminal beside a group of tabs: a
// terminal on www, a command, a file manager and a terminal through the
// kakel window desk.
func mixedLayout() settings.SavedLayout {
	return settings.SavedLayout{ID: "mixed", Name: "Mixed", Root: splitOf("#1", "row", 0.4, one("p1"), tabsOf("p2", "p3", "p4", "p5")),
		Panes: map[string]settings.SavedPane{
			"p1": {Kind: settings.LayoutTerminal},
			"p2": {Kind: settings.LayoutTerminal, Host: "web", HostID: "s1", Start: []string{"tmux attach"}},
			"p3": {Kind: settings.LayoutCommand, Host: "db-primary", HostID: "s2", Line: "tail -f /var/log/syslog"},
			"p4": {Kind: settings.LayoutFiles, Folder: "/home/me/src"},
			"p5": {Kind: settings.LayoutTerminal, Host: "desk", HostID: "s3", Window: true, Start: []string{"htop"}},
		}}
}

// devLayout is a window as a developer lays one out: the backend top
// left, the client top right, a log tail bottom left and the database
// server bottom right, the backend with a long name and many lines.
func devLayout() settings.SavedLayout {
	return settings.SavedLayout{ID: "dev", Name: "Dev", Place: &settings.WindowPlace{W: 1920, H: 1080},
		Root: splitOf("#1", "column", 0.6,
			splitOf("#2", "row", 0.5, one("p1"), one("p2")),
			splitOf("#3", "row", 0.5, one("p3"), one("p4"))),
		Panes: map[string]settings.SavedPane{
			"p1": {Kind: settings.LayoutTerminal, Title: "backend API with the payments worker", Folder: "/home/me/src/shop/backend",
				Start: []string{"source .venv/bin/activate", "export DEBUG=1", "make migrate", "make seed", "make worker &", "make run"}},
			"p2": {Kind: settings.LayoutTerminal, Title: "client", Folder: "/home/me/src/shop/client", Start: []string{"npm run dev"}},
			"p3": {Kind: settings.LayoutCommand, Title: "log tail", Host: "web", HostID: "s1", Line: "tail -f /var/log/shop/app.log"},
			"p4": {Kind: settings.LayoutTerminal, Title: "database", Host: "db-primary", HostID: "s2", Start: []string{"psql shop"}},
		}}
}

// manyTabsLayout is a terminal beside a group of n tabs, terminals but the
// third, a file manager.
func manyTabsLayout(id string, n int) settings.SavedLayout {
	l := settings.SavedLayout{ID: id, Name: "Tabs " + strconv.Itoa(n), Panes: map[string]settings.SavedPane{"p0": {Kind: settings.LayoutTerminal}}}
	var ids []string
	for i := 1; i <= n; i++ {
		slot := "p" + strconv.Itoa(i)
		ids = append(ids, slot)
		p := settings.SavedPane{Kind: settings.LayoutTerminal, Title: "worker " + strconv.Itoa(i)}
		if i == 3 {
			p = settings.SavedPane{Kind: settings.LayoutFiles, Folder: "/srv/data"}
		}
		if i == 2 {
			p.Start = []string{"make watch"}
		}
		l.Panes[slot] = p
	}
	l.Root = splitOf("#r", "row", 0.5, one("p0"), tabsOf(ids...))
	return l
}

// quietLayout is a command alone in a box, beside a terminal.
func quietLayout() settings.SavedLayout {
	return settings.SavedLayout{ID: "quiet", Name: "Quiet", Root: splitOf("#1", "row", 0.5, one("p1"), one("p2")),
		Panes: map[string]settings.SavedPane{
			"p1": {Kind: settings.LayoutTerminal, Title: "shell"},
			"p2": {Kind: settings.LayoutCommand, Line: "htop"},
		}}
}

// startState is the Layouts pane showing layouts, with bash found here
// and the server www running its login shell.
func startState(layouts ...settings.SavedLayout) app.State {
	st := layoutsState(layouts)
	st.Saved = append(st.Saved, remote.Host{ID: "s3", Name: "desk", Address: "desk.example", Window: true})
	st.Shells = []app.ShellChoice{{ID: "bash", Title: "Bash"}, {ID: "pwsh", Title: "PowerShell"}}
	st.FoundShells = []app.ShellChoice{{ID: "bash", Title: "bash", Line: "/bin/bash"}, {ID: "pwsh", Title: "PowerShell", Line: "pwsh -NoLogo"}}
	st.DefaultShell = "bash"
	return st
}

// openStartLines opens the editor for the layout with id from its card.
func openStartLines(t *testing.T, win *Window, id string) *startLines {
	t.Helper()
	c := cardOf(t, win, id)
	lastUI.Focus(c.lines)
	lastWindow.Input(gi.KeyPress{Key: gi.KeySpace, Time: time.Now()})
	lastWindow.Frame(time.Second / 60)
	if win.dialog == nil {
		t.Fatal("Start Lines opened nothing")
	}
	b, ok := win.dialog.Body.(*startLines)
	if !ok {
		t.Fatalf("Start Lines opened %q", win.dialog.Title)
	}
	return b
}

// startStage is a window showing layouts, the dialog open on the first.
func startStage(t *testing.T, layouts ...settings.SavedLayout) (*Window, *startLines, func(app.State)) {
	t.Helper()
	win, _, publish := windowStageOf(t, geom.Sz(1100, 820))
	publish(startState(layouts...))
	settle()
	b := openStartLines(t, win, layouts[0].ID)
	settle()
	return win, b, publish
}

// mapRect is where the map draws the pane in slot, in the window.
func mapRect(t *testing.T, b *startLines, slot string) geom.Rect {
	t.Helper()
	at, ok := lastUI.Bounds(b.m)
	if !ok {
		t.Fatal("the map is not drawn")
	}
	return b.m.rectOf(slot).Add(at.Min)
}

// clickAt clicks at at.
func clickAt(at geom.Point) {
	lastWindow.Input(gi.PointerDown{Pos: at, Button: gi.ButtonPrimary, Clicks: 1, Time: time.Now()})
	lastWindow.Input(gi.PointerUp{Pos: at, Button: gi.ButtonPrimary, Time: time.Now()})
}

// press presses k with mods.
func press(k gi.Key, mods gi.Mods) {
	lastWindow.Input(gi.KeyPress{Key: k, Mods: mods, Time: time.Now()})
	lastWindow.Input(gi.KeyRelease{Key: k, Mods: mods, Time: time.Now()})
	lastWindow.Frame(time.Second / 60)
}

// Screenshots of the start lines editor, with KAKEL_SHOTS set to a
// folder, under a private X server, as
//
//	Xvfb :93 & DISPLAY=:93 KAKEL_SHOTS=/tmp/shots go test ./view -run TestShotsStartLines
func TestShotsStartLines(t *testing.T) {
	dir := os.Getenv("KAKEL_SHOTS")
	if dir == "" {
		t.Skip("KAKEL_SHOTS names no folder to write the screenshots to")
	}
	shot := func(name string) {
		gunimtest.WritePNG(t, filepath.Join(dir, name+".png"), gunimtest.Pixels(t, lastWindow))
	}
	all := []settings.SavedLayout{shopLayout(), mixedLayout(), devLayout(), manyTabsLayout("eight", 8), quietLayout()}
	for _, th := range []string{"Dark", "Paper"} {
		pre := "startlines-" + th + "-"
		win, _, publish := themedStage(t, geom.Sz(1100, 820), th)
		publish(startState(all...))
		settle()
		shot(pre + "01-cards")
		b := openStartLines(t, win, "shop")
		for i := 1; i <= 3; i++ {
			frames(3)
			shot(pre + "02-opening-" + strconv.Itoa(i))
		}
		settle()
		shot(pre + "03-shop")
		// The pointer over a box lights it.
		lastWindow.Input(gi.PointerMove{Pos: mapRect(t, b, "p2").Center(), Time: time.Now()})
		settle()
		shot(pre + "04-hover")
		lastWindow.Input(gi.PointerLeave{Time: time.Now()})
		// Picked to the right: the mark glides, the page slides in.
		lastUI.Focus(b.m)
		press(gi.KeyRight, 0)
		for i := 1; i <= 4; i++ {
			frames(2)
			shot(pre + "05-gliding-" + strconv.Itoa(i))
		}
		settle()
		shot(pre + "06-log-tail")
		// Enter puts the keyboard in its lines; a line typed for another
		// shell is marked.
		press(gi.KeyEnter, 0)
		lastWindow.Input(gi.TextInput{Text: "Get-Content app.log -Wait", Time: time.Now()})
		for i := 1; i <= 2; i++ {
			frames(3)
			shot(pre + "07-typed-" + strconv.Itoa(i))
		}
		settle()
		shot(pre + "08-wrong-shell")
		// A second line, coming in on the box.
		lastWindow.Input(gi.KeyPress{Key: gi.KeyEnter, Time: time.Now()})
		lastWindow.Input(gi.TextInput{Text: "tail -f app.log", Time: time.Now()})
		for i := 1; i <= 2; i++ {
			frames(3)
			shot(pre + "09-second-line-" + strconv.Itoa(i))
		}
		settle()
		win.dialog.Close(lastUI)
		settle()

		// The group of tabs: a command picked, then a file manager, then a
		// terminal through a window.
		b = openStartLines(t, win, "mixed")
		settle()
		shot(pre + "10-mixed")
		b.m.pick("p2", lastUI)
		for i := 1; i <= 3; i++ {
			frames(2)
			shot(pre + "11-into-tabs-" + strconv.Itoa(i))
		}
		settle()
		b.m.pick("p3", lastUI)
		for i := 1; i <= 3; i++ {
			frames(2)
			shot(pre + "12-to-command-" + strconv.Itoa(i))
		}
		settle()
		shot(pre + "13-command")
		b.m.pick("p4", lastUI)
		settle()
		shot(pre + "14-files")
		b.m.pick("p5", lastUI)
		settle()
		shot(pre + "15-through-window")
		win.dialog.Close(lastUI)
		settle()

		// As the user lays a window out, with a long name and many lines.
		openStartLines(t, win, "dev")
		settle()
		shot(pre + "16-dev")
		win.dialog.Close(lastUI)
		settle()
		b = openStartLines(t, win, "eight")
		settle()
		b.m.pick("p5", lastUI)
		settle()
		shot(pre + "17-eight-tabs")
		win.dialog.Close(lastUI)
		settle()
		b = openStartLines(t, win, "quiet")
		settle()
		b.m.pick("p2", lastUI)
		settle()
		shot(pre + "18-quiet-alone")
		win.dialog.Close(lastUI)
		settle()
	}
	// Narrow and short.
	for _, size := range []geom.Size{geom.Sz(420, 760), geom.Sz(640, 560)} {
		name := "startlines-narrow-" + strconv.Itoa(int(size.W)) + "x" + strconv.Itoa(int(size.H))
		win, _, publish := windowStageOf(t, size)
		publish(startState(all...))
		settle()
		for _, id := range []string{"shop", "mixed", "dev", "eight"} {
			openStartLines(t, win, id)
			settle()
			shot(name + "-" + id)
			win.dialog.Close(lastUI)
			settle()
		}
	}
}

// themedStage is windowStageOf drawn in the built-in theme named.
func themedStage(t *testing.T, size geom.Size, name string) (*Window, *gunim.UI, func(app.State)) {
	t.Helper()
	win, _, publish := windowStageOf(t, size)
	for _, th := range themes.Built() {
		if th.Name != name {
			continue
		}
		lk, err := look.Of(th)
		if err != nil {
			t.Fatal(err)
		}
		look.Register(lastWindow, []look.Themed{lk})
		if err := lastWindow.Client().SetTheme(name); err != nil {
			t.Fatal(err)
		}
	}
	return win, lastUI, publish
}

// The card's Start Lines… opens the editor for its layout, the first
// terminal picked and its lines in an editor coloured for its shell,
// with no line numbers, which Tab leaves; a screen reader reads each
// pane on the map with what it is, where, and how many lines it holds.
func TestStartLinesOpensFromTheCard(t *testing.T) {
	win, b, _ := startStage(t, shopLayout(), mixedLayout())
	if win.dialog.Title != "Start Lines for Shop" || b.m.Picked() != "p1" || b.ed == nil || b.ed.Text() != "go run ./cmd/api" {
		t.Fatalf("opened %q on %s holding %v", win.dialog.Title, b.m.Picked(), b.ed)
	}
	if !b.ed.TabMovesOn || b.ed.Numbers || kindsOf(b.ed) == "" {
		t.Fatal("the editor takes Tab, numbers its lines, or is not coloured for bash")
	}
	if lastUI.Focused() != gunim.Node(b.m) {
		t.Fatalf("opened, the keyboard is on %T", lastUI.Focused())
	}
	press(gi.KeyTab, 0)
	if lastUI.Focused() != gunim.Node(b.ed) {
		t.Fatalf("Tab from the map went to %T", lastUI.Focused())
	}
	press(gi.KeyTab, 0)
	if f := lastUI.Focused(); !slices.Contains(win.dialog.Buttons(), f) {
		t.Fatalf("Tab from the editor went to %T", f)
	}
	if b.ed.Text() != "go run ./cmd/api" {
		t.Fatalf("Tab typed into the editor: %q", b.ed.Text())
	}
	press(gi.KeyTab, gi.ModShift)
	press(gi.KeyTab, gi.ModShift)
	if lastUI.Focused() != gunim.Node(b.m) {
		t.Fatalf("Shift+Tab twice went to %T", lastUI.Focused())
	}

	a := b.m.Access()
	if a.Role != access.RoleList || len(a.Parts) != 4 || a.Active != 1 || a.Parts[1].Name != "client" ||
		a.Parts[1].Description != "terminal, 2 start lines" || a.Parts[2].Description != "terminal on www, 0 start lines" ||
		!a.Parts[0].State.Has(access.StateSelected) {
		t.Fatalf("a screen reader reads the map as %+v", a)
	}
}

// The map is the window's tree across the dialog, in line with the
// editor's edges and no taller than its cap: the columns side by side by
// their share, each split down by its own, and a group of tabs one box
// with its tabs along its top.
func TestTheMapIsTheWindowsTree(t *testing.T) {
	win, b, _ := startStage(t, shopLayout(), mixedLayout())
	r := func(slot string) geom.Rect { return b.m.rectOf(slot) }
	whole, _ := lastUI.Bounds(b.m)
	ed, _ := lastUI.Bounds(b.ed)
	if whole.Min.X != ed.Min.X || whole.Max.X != ed.Max.X || whole.Size().H > startMapTall.Default()+0.5 {
		t.Errorf("the map stands at %v, the editor at %v", whole, ed)
	}
	if r("p1").Max.X > r("p3").Min.X || r("p1").Max.Y > r("p2").Min.Y || r("p3").Max.Y > r("p4").Min.Y {
		t.Fatalf("the panes stand at %v %v %v %v", r("p1"), r("p2"), r("p3"), r("p4"))
	}
	if left := r("p1").Size().W / (r("p1").Size().W + r("p3").Size().W); left < 0.53 || left > 0.57 {
		t.Errorf("the left column has %v of the width, want 0.55", left)
	}
	if top := r("p3").Size().H / (r("p3").Size().H + r("p4").Size().H); top < 0.58 || top > 0.62 {
		t.Errorf("the log has %v of its column, want 0.6", top)
	}
	if r("p1").Size().H != r("p2").Size().H {
		t.Errorf("a split of no share is not halved: %v and %v", r("p1"), r("p2"))
	}
	win.dialog.Close(lastUI)
	settle()

	b = openStartLines(t, win, "mixed")
	settle()
	box := b.m.boxOf("p2")
	if box == nil || !slices.Equal(box.slots, []string{"p2", "p3", "p4", "p5"}) || b.m.boxOf("p1") == box {
		t.Fatalf("the group of tabs is %+v", box)
	}
	for i, tab := range box.tabs {
		if !box.r.Contains(tab.Min) || !box.r.Contains(tab.Max.Sub(geom.Pt(1, 1))) || tab.Max.Y > box.body.Min.Y ||
			i > 0 && tab.Min.X < box.tabs[i-1].Max.X {
			t.Fatalf("tab %d stands at %v in a box at %v", i, tab, box.r)
		}
	}
}

// A click on a pane picks it and puts the keyboard in its lines, and so
// do the arrow keys pick, to the pane beside on that side, along a box's
// tabs first, with Enter to go to the lines. The mark glides there, and
// the page of the pane picked comes in as the last fades, nothing
// jumping in a frame. A press let go on another pane, or a finger that
// goes on to scroll, picks nothing.
func TestAPaneIsPickedByClickAndByKeys(t *testing.T) {
	win, b, _ := startStage(t, shopLayout(), mixedLayout())
	old := b.pages.Page()
	clickAt(mapRect(t, b, "p4").Center())
	var xs, ys []float32
	faded := false
	for range 40 {
		lastWindow.Frame(time.Second / 60)
		mark := b.m.mark.Value()
		xs, ys = append(xs, mark.Min.X), append(ys, mark.Min.Y)
		if _, there := lastUI.Bounds(old); there {
			faded = true
		}
	}
	if b.m.Picked() != "p4" || b.ed == nil || b.ed.Text() != "" || !faded {
		t.Fatalf("clicked, %s is picked, its editor %v, the last page faded %v", b.m.Picked(), b.ed, faded)
	}
	if lastUI.Focused() != gunim.Node(b.ed) {
		t.Fatalf("clicked, the keyboard is on %T", lastUI.Focused())
	}
	if xs[0] == xs[len(xs)-1] || ys[0] == ys[len(ys)-1] || xs[len(xs)-1] != b.m.rectOf("p4").Min.X {
		t.Fatalf("the mark went from %v,%v to %v,%v", xs[0], ys[0], xs[len(xs)-1], ys[len(ys)-1])
	}
	noJumps(t, "the mark across", xs)
	noJumps(t, "the mark down", ys)

	// Pressed on one pane and let go on another, or pressed and taken
	// away to scroll, nothing is picked.
	lastWindow.Input(gi.PointerDown{Pos: mapRect(t, b, "p1").Center(), Button: gi.ButtonPrimary, Clicks: 1, Time: time.Now()})
	lastWindow.Input(gi.PointerUp{Pos: mapRect(t, b, "p2").Center(), Button: gi.ButtonPrimary, Time: time.Now()})
	lastWindow.Input(gi.PointerDown{Pos: mapRect(t, b, "p1").Center(), Button: gi.ButtonPrimary, Clicks: 1, Time: time.Now()})
	lastWindow.Input(gi.PointerUp{Pos: gi.Away, Button: gi.ButtonPrimary, Time: time.Now()})
	settle()
	if b.m.Picked() != "p4" {
		t.Fatalf("a press taken away picked %s", b.m.Picked())
	}

	lastUI.Focus(b.m)
	for _, c := range []struct {
		key  gi.Key
		want string
	}{{gi.KeyUp, "p3"}, {gi.KeyLeft, "p1"}, {gi.KeyDown, "p2"}, {gi.KeyRight, "p4"}, {gi.KeyRight, "p4"}, {gi.KeyHome, "p1"}, {gi.KeyEnd, "p4"}} {
		press(c.key, 0)
		if b.m.Picked() != c.want {
			t.Fatalf("%v picked %s, want %s", c.key, b.m.Picked(), c.want)
		}
	}
	press(gi.KeyEnter, 0)
	settle()
	if lastUI.Focused() != gunim.Node(b.ed) || win.dialog == nil {
		t.Fatalf("Enter on the map left the keyboard on %T", lastUI.Focused())
	}
	win.dialog.Close(lastUI)
	settle()

	b = openStartLines(t, win, "mixed")
	settle()
	lastUI.Focus(b.m)
	for _, c := range []struct {
		key  gi.Key
		want string
	}{{gi.KeyRight, "p2"}, {gi.KeyRight, "p3"}, {gi.KeyRight, "p4"}, {gi.KeyLeft, "p3"}, {gi.KeyLeft, "p2"}, {gi.KeyLeft, "p1"}} {
		press(c.key, 0)
		if b.m.Picked() != c.want {
			t.Fatalf("in the tabs, %v picked %s, want %s", c.key, b.m.Picked(), c.want)
		}
	}
	// Back into the box of tabs from the side, to the tab on that side;
	// and a click on a tab picks it.
	press(gi.KeyRight, 0)
	settle()
	clickAt(mapRect(t, b, "p5").Center())
	settle()
	if b.m.Picked() != "p5" || b.m.showing(*b.m.boxOf("p5")) != "p5" {
		t.Fatalf("the tab clicked left %s picked", b.m.Picked())
	}
}

// From every pane, the arrow keys reach every other, whatever the shape
// of the window: three columns with the middle one split, splits inside
// splits, groups of tabs, a column beside four rows, and two by two.
func TestEveryPaneIsReachedByTheArrows(t *testing.T) {
	term := settings.SavedPane{Kind: settings.LayoutTerminal}
	panes := func(n int) map[string]settings.SavedPane {
		out := map[string]settings.SavedPane{}
		for i := 1; i <= n; i++ {
			out["p"+strconv.Itoa(i)] = term
		}
		return out
	}
	shapes := []settings.SavedLayout{
		{ID: "three", Name: "Three", Panes: panes(4), Root: splitOf("#1", "row", 0,
			one("p1"), splitOf("#2", "column", 0, one("p2"), one("p3")), one("p4"))},
		{ID: "nested", Name: "Nested", Panes: panes(6), Root: splitOf("#1", "row", 0.5,
			splitOf("#2", "column", 0.3, one("p1"), splitOf("#3", "row", 0.7, one("p2"), one("p3"))),
			splitOf("#4", "column", 0.6, splitOf("#5", "row", 0.4, one("p4"), one("p5")), one("p6")))},
		{ID: "groups", Name: "Groups", Panes: panes(6), Root: splitOf("#1", "row", 0.5,
			tabsOf("p1", "p2", "p3"), splitOf("#2", "column", 0.5, one("p4"), tabsOf("p5", "p6")))},
		{ID: "ladder", Name: "Ladder", Panes: panes(5), Root: splitOf("#1", "row", 0.3, one("p1"),
			splitOf("#2", "column", 0, one("p2"), one("p3"), one("p4"), one("p5")))},
		shopLayout(), devLayout(),
	}
	for _, l := range shapes {
		win, b, _ := startStage(t, l)
		for _, from := range b.m.order {
			reached := map[string]bool{from: true}
			queue := []string{from}
			for len(queue) > 0 {
				at := queue[0]
				queue = queue[1:]
				b.m.SetPicked(at, nil)
				for _, d := range []geom.Point{geom.Pt(1, 0), geom.Pt(-1, 0), geom.Pt(0, 1), geom.Pt(0, -1)} {
					if to := b.m.beside(d); to != "" && !reached[to] {
						reached[to] = true
						queue = append(queue, to)
					}
				}
			}
			if len(reached) != len(b.m.order) {
				t.Errorf("in %s, from %s the arrows reach %v of %v", l.Name, from, reached, b.m.order)
			}
		}
		win.dialog.Close(lastUI)
		settle()
	}
	// Right from the full-height left column goes to the column beside,
	// not past it.
	_, b, _ := startStage(t, shapes[0])
	b.m.SetPicked("p1", nil)
	if to := b.m.beside(geom.Pt(1, 0)); to != "p2" {
		t.Fatalf("Right from the left column went to %s", to)
	}
}

// A command, a file manager, a terminal through another kakel window, a
// pane of a kind this kakel does not know, and a slot the layout holds
// no pane for, are on the map and take no lines: picking one shows why,
// with no editor to Tab to, and the note on how lines are typed fades
// out. Save leaves the lines any of them holds as they are.
func TestOnlyATerminalReachedDirectlyTakesLines(t *testing.T) {
	l := mixedLayout()
	l.Panes["p6"] = settings.SavedPane{Kind: "browser", Start: []string{"kept"}}
	l.Root.Kids[1].Kids = append(l.Root.Kids[1].Kids, &settings.LayoutNode{ID: "p6", Kind: "item"}, &settings.LayoutNode{ID: "x9", Kind: "item"})
	win, b, _ := startStage(t, l)
	for _, slot := range []string{"p3", "p4", "p5", "p6", "x9"} {
		b.m.pick(slot, lastUI)
		settle()
		if b.ed != nil || slices.ContainsFunc(b.Focusables(), func(n gunim.Node) bool { _, ok := n.(*widget.CodeEditor); return ok }) {
			t.Fatalf("%s has an editor", slot)
		}
		if b.m.faces[slot].editable || b.noteOn.Value() != 0 {
			t.Fatalf("%s is drawn as taking lines, or the note shows", slot)
		}
		if _, ok := b.pages.Page().(*startPage).body.(*startWell); !ok {
			t.Fatalf("%s says nothing in place of an editor", slot)
		}
	}
	b.m.pick("p1", lastUI)
	settle()
	if b.ed == nil || b.noteOn.Value() != 1 {
		t.Fatal("the terminal here has no editor, or no note")
	}
	b.ed.SetText("make", lastUI)
	b.ed.OnChange("make", lastUI)
	in, ok := win.dialog.OnAccept(lastUI).(app.SaveStartLines)
	if !ok || len(in.Lines) != 1 || !slices.Equal(in.Lines["p1"], []string{"make"}) {
		t.Fatalf("Save sent %#v", in)
	}
}

// A layout's tree as a later kakel or a hand may write it opens without
// a fault: a slot of no pane, a pane of no slot, and a tree of no panes,
// which says so.
func TestAnOddTreeOpens(t *testing.T) {
	for _, l := range []settings.SavedLayout{
		{ID: "x", Name: "No pane", Root: &settings.LayoutNode{ID: "x9", Kind: "item"},
			Panes: map[string]settings.SavedPane{"p1": {Kind: settings.LayoutTerminal}}},
		{ID: "y", Name: "Empty", Root: &settings.LayoutNode{ID: "#1", Kind: "row"},
			Panes: map[string]settings.SavedPane{"p1": {Kind: settings.LayoutTerminal}}},
		{ID: "z", Name: "Empty group", Root: &settings.LayoutNode{ID: "#1", Kind: "group"},
			Panes: map[string]settings.SavedPane{"p1": {Kind: settings.LayoutTerminal}}},
	} {
		win, b, _ := startStage(t, l)
		lastUI.Focus(b.m)
		for _, k := range []gi.Key{gi.KeyRight, gi.KeyDown, gi.KeyHome, gi.KeyEnd, gi.KeyEnter} {
			press(k, 0)
		}
		clickAt(mapRect(t, b, "").Center())
		settle()
		well, ok := b.pages.Page().(*startPage).body.(*startWell)
		if !ok || b.ed != nil {
			t.Fatalf("%s shows an editor", l.Name)
		}
		if len(b.m.order) == 0 && well.says.Text != "This layout has no panes." {
			t.Fatalf("%s says %q", l.Name, well.says.Text)
		}
		if in := win.dialog.OnAccept(lastUI); in != (app.DialogClosed{}) {
			t.Fatalf("%s saved %#v", l.Name, in)
		}
		win.dialog.Close(lastUI)
		settle()
	}
}

// Lines typed are what Save sends, cleaned, for the terminals whose lines
// changed alone, the sign on the pane's box growing in as the first is
// typed and out as the last goes, and a line coming in on the box as it
// is typed; Cancel sends nothing of them, and Save with nothing changed
// nothing either.
func TestLinesTypedAreSaved(t *testing.T) {
	win, b, _ := startStage(t, shopLayout(), mixedLayout())
	b.m.pick("p3", lastUI)
	settle()
	lastUI.Focus(b.ed)
	lastWindow.Input(gi.TextInput{Text: "tail -f app.log", Time: time.Now()})
	var signs []float32
	for range 30 {
		lastWindow.Frame(time.Second / 60)
		signs = append(signs, b.m.faces["p3"].sign.Value())
	}
	if signs[0] > 0.5 || signs[len(signs)-1] != 1 {
		t.Fatalf("typed, the sign came %v", signs)
	}
	noJumps(t, "the sign", signs)
	// A second line comes in on the box, rather than standing there at
	// once.
	lastWindow.Input(gi.KeyPress{Key: gi.KeyEnter, Time: time.Now()})
	lastWindow.Input(gi.TextInput{Text: "less app.log", Time: time.Now()})
	var ins []float32
	for range 30 {
		lastWindow.Frame(time.Second / 60)
		_, in := b.m.faces["p3"].line(1)
		ins = append(ins, in)
	}
	if ins[0] > 0.5 || ins[len(ins)-1] != 1 {
		t.Fatalf("a second line came in %v", ins)
	}
	noJumps(t, "the second line", ins)

	// The client's lines taken out, the backend's given another.
	b.m.pick("p2", lastUI)
	settle()
	b.ed.SetText("", lastUI)
	b.ed.OnChange("", lastUI)
	b.texts["p1"] = "  go run ./cmd/api\n\n\tmake watch"
	in, ok := win.dialog.OnAccept(lastUI).(app.SaveStartLines)
	if !ok || in.Layout.ID != "shop" {
		t.Fatalf("Save sent %#v", in)
	}
	want := map[string][]string{"p1": {"  go run ./cmd/api", " make watch"}, "p2": nil, "p3": {"tail -f app.log", "less app.log"}}
	if len(in.Lines) != len(want) {
		t.Fatalf("Save sent the lines %q", in.Lines)
	}
	for slot, lines := range want {
		if got, ok := in.Lines[slot]; !ok || !slices.Equal(got, lines) {
			t.Fatalf("Save sent the lines %q", in.Lines)
		}
	}
	if b.m.faces["p2"].sign.Target() != 0 {
		t.Fatal("its lines taken out, the client still shows it has some")
	}

	drain()
	lastUI.Focus(b.ed)
	lastWindow.Input(gi.KeyPress{Key: gi.KeyEscape, Time: time.Now()})
	lastWindow.Frame(time.Second / 60)
	if in := nextIntent(t); in != (app.DialogClosed{}) {
		t.Fatalf("Cancel sent %#v", in)
	}
	b = openStartLines(t, win, "shop")
	if b.texts["p3"] != "" {
		t.Fatalf("opened again after Cancel, the log holds %q", b.texts["p3"])
	}
	if in := win.dialog.OnAccept(lastUI); in != (app.DialogClosed{}) {
		t.Fatalf("Save with nothing changed sent %#v", in)
	}
}

// Save with lines changed on a layout another window has since removed,
// or saved over, or given other lines on a terminal changed here, says
// so and stays open with what was typed; lines another window gave to
// another terminal meanwhile leave Save to go ahead.
func TestSaveSeesTheLayoutChangedMeanwhile(t *testing.T) {
	win, b, publish := startStage(t, shopLayout(), mixedLayout())
	b.ed.SetText("make run", lastUI)
	b.ed.OnChange("make run", lastUI)
	for _, c := range []struct {
		name   string
		change func(*settings.SavedLayout) bool
		says   string
	}{
		{"another terminal's lines", func(l *settings.SavedLayout) bool {
			p := l.Panes["p3"]
			p.Start = []string{"tail"}
			l.Panes["p3"] = p
			return true
		}, ""},
		{"this terminal's lines", func(l *settings.SavedLayout) bool {
			p := l.Panes["p1"]
			p.Start = []string{"other"}
			l.Panes["p1"] = p
			return true
		}, "Layout changed elsewhere"},
		{"the splits", func(l *settings.SavedLayout) bool { l.Root.Share = 0.3; return true }, "Layout changed elsewhere"},
		{"the layout removed", func(*settings.SavedLayout) bool { return false }, "Layout removed"},
	} {
		l := shopLayout()
		layouts := []settings.SavedLayout{mixedLayout()}
		if c.change(&l) {
			layouts = append(layouts, l)
		}
		publish(startState(layouts...))
		if got := win.dialog.Check(); got != c.says {
			t.Errorf("with %s changed, Save says %q, want %q", c.name, got, c.says)
		}
	}
	// Pressed, Save shakes and stays.
	lastUI.Focus(win.dialog.Buttons()[0])
	press(gi.KeySpace, 0)
	settle()
	if win.dialog == nil || b.texts["p1"] != "make run" {
		t.Fatal("a Save refused closed the dialog, or lost what was typed")
	}
}

// A line the terminal's shell has no way of writing is said for what it
// is, beside the shell the terminal starts, where the paste dialog says
// where to run it.
func TestALineForAnotherShellIsSaid(t *testing.T) {
	marks := startSlips("Get-Content app.log -Wait\nls", shellsetup.Posix, "Bash")
	if len(marks) != 1 || marks[0].Message != "PowerShell command. This terminal starts Bash." {
		t.Fatalf("the lines are marked %+v", marks)
	}
	if paste := pasteSlips("Get-Content app.log", shellsetup.Posix, false); len(paste) != 1 || paste[0].Message == marks[0].Message {
		t.Fatalf("the paste dialog marks %+v", paste)
	}
}

// Fourteen tabs in half the dialog each keep a place of their own, the
// one picked as wide as its name, the rest narrowed to their icons, and
// each says its name in a tooltip over the whole tab where its name is
// cut. As another comes to the front, the two change widths over frames,
// and the mark on the tab picked glides along the tabs with it.
func TestManyTabsKeepTheirPlace(t *testing.T) {
	_, b, _ := startStage(t, manyTabsLayout("many", 14))
	b.m.pick("p5", lastUI)
	settle()
	box := b.m.boxOf("p5")
	for i, tab := range box.tabs {
		if tab.Size().W < 16 || i > 0 && tab.Min.X < box.tabs[i-1].Max.X || tab.Max.X > box.r.Max.X {
			t.Fatalf("tab %d stands at %v in %v", i, tab, box.r)
		}
	}
	if w := box.tabs[4].Size().W; w < startTabLeast.Default() {
		t.Fatalf("the tab picked is %v wide", w)
	}
	lastWindow.Paint()
	for i, slot := range box.slots {
		if slot == "p5" {
			continue
		}
		if got := b.m.cutAt(box.tabs[i].Center()); got != b.m.faces[slot].name {
			t.Fatalf("tab %d says %q in its tooltip", i, got)
		}
	}
	b.m.pick("p6", lastUI)
	var widths, marks []float32
	for range 40 {
		lastWindow.Frame(time.Second / 60)
		box = b.m.boxOf("p6")
		widths = append(widths, box.tabs[5].Size().W)
		mark := b.m.tabMark(box)
		marks = append(marks, mark.Min.X)
		if abs32(mark.Min.Y-box.tabs[0].Min.Y) > 0.01 || abs32(mark.Size().H-box.tabs[0].Size().H) > 0.01 {
			t.Fatalf("the tab's mark left the tabs: %v", mark)
		}
	}
	if widths[0] >= widths[len(widths)-1] {
		t.Fatalf("the tab coming to the front went from %v wide to %v", widths[0], widths[len(widths)-1])
	}
	noJumps(t, "the tab's width", widths)
	noJumps(t, "the tab's mark", marks)
}

// Picked from another box, a box of tabs shows the mark on its tab
// fading in where the tab stands, not sweeping in from the box left.
func TestTheTabsMarkFadesIn(t *testing.T) {
	_, b, _ := startStage(t, mixedLayout())
	b.m.pick("p3", lastUI)
	var ons []float32
	for range 30 {
		lastWindow.Frame(time.Second / 60)
		box := b.m.boxOf("p3")
		on := b.m.tabOn[box.key].Value()
		ons = append(ons, on)
		if mark := b.m.tabMark(box); on > 0.01 && (abs32(mark.Min.Y-box.tabs[0].Min.Y) > 0.01 || mark.Min.X < box.r.Min.X) {
			t.Fatalf("the tab's mark stands at %v, out of the tabs at %v", mark, box.tabs)
		}
	}
	if ons[0] > 0.5 || ons[len(ons)-1] != 1 {
		t.Fatalf("the tab's mark came %v", ons)
	}
	noJumps(t, "the tab's mark", ons)
}

// A layout's card says how many start lines its terminals type, and
// says it anew, fading across, as lines are given.
func TestTheCardCountsTheStartLines(t *testing.T) {
	win, _, publish := windowStageOf(t, geom.Sz(1100, 820))
	publish(startState(shopLayout()))
	settle()
	c := cardOf(t, win, "shop")
	if got := pageText(c.holds); got != "4 terminals, 3 start lines" {
		t.Fatalf("the card says %q", got)
	}
	l := shopLayout()
	p := l.Panes["p3"]
	p.Start = []string{"tail -f app.log"}
	l.Panes["p3"] = p
	old := c.holds.Page()
	publish(startState(l))
	if _, there := lastUI.Bounds(old); !there || pageText(c.holds) != "4 terminals, 4 start lines" {
		t.Fatalf("given a line, the card says %q, the old words gone at once", pageText(c.holds))
	}
}

// The count of lines past a box's room changes by fading across, not in
// a frame.
func TestTheCountFadesAcross(t *testing.T) {
	_, b, _ := startStage(t, shopLayout())
	face := b.m.faces["p2"]
	b.m.pick("p2", lastUI)
	settle()
	b.ed.SetText(strings.Repeat("make\n", 30), lastUI)
	b.ed.OnChange(strings.Repeat("make\n", 30), lastUI)
	settle()
	was := face.more.now
	b.ed.SetText(strings.Repeat("make\n", 31), lastUI)
	b.ed.OnChange(strings.Repeat("make\n", 31), lastUI)
	var ts []float32
	for range 30 {
		lastWindow.Frame(time.Second / 60)
		ts = append(ts, face.more.t.Value())
	}
	if was == "" || face.more.now == was || ts[0] > 0.5 || ts[len(ts)-1] != 1 {
		t.Fatalf("the count went from %q to %q over %v", was, face.more.now, ts)
	}
	noJumps(t, "the count", ts)
}
