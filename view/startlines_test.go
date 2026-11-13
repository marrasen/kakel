package view

import (
	"image/color"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/access"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/text"
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
	// run shoots n frames, each every frames apart, named name-1 on.
	run := func(name string, n, every int) {
		for i := 1; i <= n; i++ {
			frames(every)
			shot(name + "-" + strconv.Itoa(i))
		}
	}
	all := []settings.SavedLayout{shopLayout(), mixedLayout(), devLayout(), manyTabsLayout("eight", 8), quietLayout(),
		manyTabsLayout("many", 14), shortTabs("two", 0.7, "bash", "zsh"),
		shortTabs("seven", 0.7, "api", "web", "db", "client", "tests", "logs", "server")}
	for _, th := range []string{"Dark", "Paper"} {
		pre := "startlines-" + th + "-"
		win, _, publish := themedStage(t, geom.Sz(1100, 820), th)
		publish(startState(all...))
		settle()
		shot(pre + "01-cards")
		b := openStartLines(t, win, "shop")
		run(pre+"02-opening", 3, 3)
		settle()
		shot(pre + "03-shop")
		// The pointer over a box lights it.
		lastWindow.Input(gi.PointerMove{Pos: mapRect(t, b, "p2").Center(), Time: time.Now()})
		settle()
		shot(pre + "04-hover")
		lastWindow.Input(gi.PointerLeave{Time: time.Now()})
		// Picked to the right, from one terminal to another: the mark
		// glides as the old page goes out and the new one comes in.
		lastUI.Focus(b.m)
		press(gi.KeyRight, 0)
		run(pre+"05-gliding", 8, 2)
		settle()
		shot(pre + "06-log-tail")
		// Enter puts the keyboard in its lines; the placeholder fades as
		// the first letter is typed, and a line typed for another shell
		// is marked.
		press(gi.KeyEnter, 0)
		settle()
		shot(pre + "07-placeholder")
		lastWindow.Input(gi.TextInput{Text: "G", Time: time.Now()})
		run(pre+"08-first-letter", 3, 2)
		lastWindow.Input(gi.TextInput{Text: "et-Content app.log -Wait", Time: time.Now()})
		settle()
		shot(pre + "09-wrong-shell")
		// A second line, coming in on the box.
		lastWindow.Input(gi.KeyPress{Key: gi.KeyEnter, Time: time.Now()})
		lastWindow.Input(gi.TextInput{Text: "tail -f app.log", Time: time.Now()})
		run(pre+"10-second-line", 2, 3)
		settle()
		// The client, its box holding as many lines as fit, then one more:
		// the last line fades out as the count fades in on its row, and
		// back.
		b.m.pick("p2", lastUI)
		settle()
		face := b.m.faces["p2"]
		typed := func(n int) {
			lines := []string{"npm install", "npm run lint", "npm run build", "npm run test", "npm run dev", "npm run e2e"}
			joined := strings.Join(lines[:n], "\n")
			b.ed.SetText(joined, lastUI)
			b.ed.OnChange(joined, lastUI)
		}
		typed(face.fit)
		settle()
		shot(pre + "11-as-many-as-fit")
		typed(face.fit + 1)
		run(pre+"12-one-more", 5, 2)
		settle()
		shot(pre + "13-one-more")
		typed(face.fit)
		run(pre+"14-one-fewer", 5, 2)
		settle()
		win.dialog.Close(lastUI)
		settle()

		// The group of tabs: a terminal, a command, a file manager, then
		// a terminal through a window.
		b = openStartLines(t, win, "mixed")
		settle()
		shot(pre + "15-mixed")
		b.m.pick("p2", lastUI)
		run(pre+"16-into-tabs", 3, 2)
		settle()
		// From a terminal to a command along the tabs: the tab mark and
		// the page together.
		b.m.pick("p3", lastUI)
		run(pre+"17-to-command", 8, 2)
		settle()
		shot(pre + "18-command")
		b.m.pick("p4", lastUI)
		settle()
		shot(pre + "19-files")
		b.m.pick("p5", lastUI)
		settle()
		shot(pre + "20-through-window")
		win.dialog.Close(lastUI)
		settle()

		// As the user lays a window out, with a long name and many lines.
		openStartLines(t, win, "dev")
		settle()
		shot(pre + "21-dev")
		win.dialog.Close(lastUI)
		settle()
		for _, c := range []struct{ id, pick, name string }{
			{"eight", "p5", "22-eight-tabs"}, {"many", "p5", "23-fourteen-tabs"},
			{"two", "p1", "24-two-short-tabs"}, {"seven", "p2", "25-seven-short-tabs"},
		} {
			b = openStartLines(t, win, c.id)
			settle()
			b.m.pick(c.pick, lastUI)
			settle()
			shot(pre + c.name)
			win.dialog.Close(lastUI)
			settle()
		}
		b = openStartLines(t, win, "quiet")
		settle()
		b.m.pick("p2", lastUI)
		settle()
		shot(pre + "26-quiet-alone")
		// The pointer over the quiet box, picked and not.
		lastWindow.Input(gi.PointerMove{Pos: mapRect(t, b, "p2").Center(), Time: time.Now()})
		settle()
		shot(pre + "27-quiet-hover-picked")
		b.m.pick("p1", lastUI)
		settle()
		shot(pre + "28-quiet-hover")
		lastWindow.Input(gi.PointerLeave{Time: time.Now()})
		win.dialog.Close(lastUI)
		settle()

		// A pick made with a click while the editor has the keyboard: the
		// keyboard goes to the lines of the pane clicked.
		b = openStartLines(t, win, "shop")
		settle()
		lastUI.Focus(b.ed)
		settle()
		shot(pre + "29-editor-has-keys")
		clickAt(mapRect(t, b, "p2").Center())
		run(pre+"30-pick-from-editor", 6, 2)
		settle()
		shot(pre + "31-picked-from-editor")
		win.dialog.Close(lastUI)
		settle()
	}
	// The card's line with its count of start lines, in a narrow pane.
	for _, th := range []string{"Dark", "Paper"} {
		for _, width := range []float32{280, 340} {
			_, _, publish := themedStage(t, geom.Sz(width, 900), th)
			publish(startState(all...))
			settle()
			shot("startlines-card-" + th + "-" + strconv.Itoa(int(width)))
		}
	}
	// Narrow and short.
	for _, th := range []string{"Dark", "Paper"} {
		for _, size := range []geom.Size{geom.Sz(420, 760), geom.Sz(640, 560)} {
			name := "startlines-narrow-" + th + "-" + strconv.Itoa(int(size.W)) + "x" + strconv.Itoa(int(size.H))
			win, _, publish := themedStage(t, size, th)
			publish(startState(all...))
			settle()
			for _, id := range []string{"shop", "mixed", "dev", "eight", "many", "two"} {
				b := openStartLines(t, win, id)
				settle()
				shot(name + "-" + id)
				if id == "shop" {
					// The log tail's empty editor, its placeholder cut to
					// the width.
					b.m.pick("p3", lastUI)
					settle()
					shot(name + "-placeholder")
				}
				win.dialog.Close(lastUI)
				settle()
			}
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

// The map is the window's tree across the dialog, its boxes in line with
// the editor's edges and no taller than its cap, with room round them in
// the map for the focus ring: the columns side by side by their share,
// each split down by its own, and a group of tabs one box with its tabs
// along its top.
func TestTheMapIsTheWindowsTree(t *testing.T) {
	win, b, _ := startStage(t, shopLayout(), mixedLayout())
	r := func(slot string) geom.Rect { return b.m.rectOf(slot) }
	whole, _ := lastUI.Bounds(b.m)
	ed, _ := lastUI.Bounds(b.ed)
	boxes := r("p1").Union(r("p4")).Add(whole.Min)
	if abs32(boxes.Min.X-ed.Min.X) > 0.01 || abs32(boxes.Max.X-ed.Max.X) > 0.01 || boxes.Size().H > startMapTall.Default()+0.5 {
		t.Errorf("the boxes stand at %v, the editor at %v", boxes, ed)
	}
	if room := min(boxes.Min.X-whole.Min.X, boxes.Min.Y-whole.Min.Y, whole.Max.X-boxes.Max.X, whole.Max.Y-boxes.Max.Y); room < startRingRoom-0.01 {
		t.Errorf("the boxes stand at %v in a map at %v, %v from its edge", boxes, whole, room)
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
	if login := startSlips("Get-Content app.log", shellsetup.Posix, "Login Shell"); len(login) != 1 ||
		login[0].Message != "PowerShell command. This terminal starts the login shell." {
		t.Fatalf("on a server's own shell, the line is marked %+v", login)
	}
	if paste := pasteSlips("Get-Content app.log", shellsetup.Posix, false); len(paste) != 1 || paste[0].Message == marks[0].Message {
		t.Fatalf("the paste dialog marks %+v", paste)
	}
}

// Fourteen tabs in half the dialog each keep a place of their own, the
// one picked naming itself as far as its room goes, the rest narrowed to
// their icons, and
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
	// The tab in front names itself, as far as its room goes.
	if named, room := tabName(lastUI.Theme(), box.tabs[4], b.m.faces["p5"]); named < 1 || room < 12 {
		t.Fatalf("the tab picked shows %v of its name, in %v px", named, room)
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
	was := face.more.now()
	b.ed.SetText(strings.Repeat("make\n", 31), lastUI)
	b.ed.OnChange(strings.Repeat("make\n", 31), lastUI)
	var ts []float32
	for range 30 {
		lastWindow.Frame(time.Second / 60)
		ts = append(ts, face.more.on(face.more.now()))
	}
	if was == "" || face.more.now() == was || ts[0] > 0.5 || ts[len(ts)-1] != 1 {
		t.Fatalf("the count went from %q to %q over %v", was, face.more.now(), ts)
	}
	noJumps(t, "the count", ts)
}

// shortTabs is a terminal beside a group of terminals named names, the
// group taking share of the width.
func shortTabs(id string, share float32, names ...string) settings.SavedLayout {
	l := settings.SavedLayout{ID: id, Name: id, Panes: map[string]settings.SavedPane{"p0": {Kind: settings.LayoutTerminal}}}
	var ids []string
	for i, name := range names {
		slot := "p" + strconv.Itoa(i+1)
		ids = append(ids, slot)
		l.Panes[slot] = settings.SavedPane{Kind: settings.LayoutTerminal, Title: name}
	}
	l.Root = splitOf("#r", "row", 1-share, one("p0"), tabsOf(ids...))
	return l
}

// A tab's name shows whole or not at all: a tab behind is as wide as its
// name or narrowed to its icon, never between, and the tab in front
// shows as much of its name as fits, faded where it is cut, never none.
// So two short names in a wide box both show, as do seven, and in a box
// of fourteen the tab in front still names itself.
func TestTabNamesShowWholeOrNotAtAll(t *testing.T) {
	for _, l := range []settings.SavedLayout{
		shortTabs("two", 0.7, "bash", "zsh"),
		shortTabs("seven", 0.7, "api", "web", "db", "client", "tests", "logs", "server"),
		manyTabsLayout("eight", 8), manyTabsLayout("many", 14),
	} {
		win, b, _ := startStage(t, l)
		b.m.pick("p1", lastUI)
		settle()
		lastWindow.Paint()
		th := lastUI.Theme()
		box := b.m.boxOf("p1")
		for i, slot := range box.slots {
			f := b.m.faces[slot]
			named, room := tabName(th, box.tabs[i], f)
			whole := text.Default().Shape(f.name, startTabSize.Get(th)).Advance
			switch {
			case slot == "p1" && (named < 1 || room < min(whole, 12)):
				t.Errorf("in %s, the tab in front shows %v of its name in %v px", l.Name, named, room)
			case slot != "p1" && named > 0 && (named < 1 || room < whole):
				t.Errorf("in %s, tab %s shows %v of its name in %v px of the %v it needs", l.Name, slot, named, room, whole)
			case l.ID == "two" || l.ID == "seven":
				if named < 1 || b.m.cutAt(box.tabs[i].Center()) != "" {
					t.Errorf("in %s, the short name %q is not whole", l.Name, f.name)
				}
			}
		}
		win.dialog.Close(lastUI)
		settle()
	}
}

// Up and Down across a T, two panes over one, take the user back where
// they came from.
func TestUpAndDownAcrossATGoBack(t *testing.T) {
	term := settings.SavedPane{Kind: settings.LayoutTerminal}
	l := settings.SavedLayout{ID: "tee", Name: "Tee", Panes: map[string]settings.SavedPane{"p1": term, "p2": term, "p3": term},
		Root: splitOf("#1", "column", 0.5, splitOf("#2", "row", 0.5, one("p1"), one("p2")), one("p3"))}
	_, b, _ := startStage(t, l)
	lastUI.Focus(b.m)
	for _, from := range []string{"p2", "p1", "p2"} {
		b.m.pick(from, lastUI)
		press(gi.KeyDown, 0)
		press(gi.KeyUp, 0)
		if b.m.Picked() != from {
			t.Fatalf("Down and Up from %s went to %s", from, b.m.Picked())
		}
	}
}

// A line on a box gives way to the count of the lines past its room,
// fading out on its row before the count fades in, and comes back once
// the count has gone, the two never drawn on the row at once. Typed fast, the
// count and a line coming in carry on from where they stand.
func TestALineGivesWayToTheCount(t *testing.T) {
	_, b, _ := startStage(t, shopLayout())
	face := b.m.faces["p2"]
	b.m.pick("p2", lastUI)
	settle()
	lines := func(n int) {
		s := strings.Repeat("make\n", n)
		b.ed.SetText(s, lastUI)
		b.ed.OnChange(s, lastUI)
	}
	lines(1)
	settle()
	fit := face.fit
	if fit < 3 {
		t.Fatalf("the box has room for %d lines", fit)
	}
	watch := func(what string, n int, lineFrom, lineTo, countTo float32) {
		t.Helper()
		lines(n)
		var ls, cs []float32
		for range 30 {
			lastWindow.Frame(time.Second / 60)
			l, c := face.lineShown(fit-1), face.countShown()
			ls, cs = append(ls, l), append(cs, c)
			if drawn := face.countDrawn(); l > 0.01 && drawn > 0.01 {
				t.Fatalf("%s, the line shows %v and the count %v on one row", what, l, drawn)
			}
		}
		if abs32(ls[0]-lineFrom) > 0.5 || ls[len(ls)-1] != lineTo || cs[len(cs)-1] != countTo {
			t.Fatalf("%s, the line went %v and the count %v", what, ls, cs)
		}
		noJumps(t, what+", the line", ls)
		noJumps(t, what+", the count", cs)
	}
	lines(fit)
	settle()
	watch("one line too many", fit+1, 1, 0, 1)
	watch("back to as many as fit", fit, 0, 1, 0)

	// Fast: a line every few frames, past the room and back.
	var cs, ls []float32
	for _, n := range []int{fit + 1, fit + 2, fit + 3, fit + 2, fit + 1, fit} {
		lines(n)
		for range 4 {
			lastWindow.Frame(time.Second / 60)
			cs = append(cs, face.countShown())
		}
	}
	settle()
	for i := 1; i < len(cs); i++ {
		if abs32(cs[i]-cs[i-1]) > 0.4 {
			t.Fatalf("typed fast, the count jumped %v", cs)
		}
	}
	lines(1)
	settle()
	lines(2)
	frames(3)
	lines(3)
	for range 20 {
		lastWindow.Frame(time.Second / 60)
		ls = append(ls, face.lineShown(1))
	}
	for i := 1; i < len(ls); i++ {
		if ls[i] < ls[i-1]-0.01 {
			t.Fatalf("typed fast, the second line went back %v", ls)
		}
	}
}

// The tab picked wears a pill its name reads on at 5:1 or better, in the
// light theme and the dark, and an icon dimmed for a pane that takes no
// lines still reads on it, as it is drawn in the words' colour at part
// strength.
func TestThePickedTabsNameReadsOnItsPill(t *testing.T) {
	for _, name := range []string{"Dark", "Paper"} {
		themedStage(t, geom.Sz(400, 300), name)
		// The theme's colours fade in over frames.
		settle()
		th := lastUI.Theme()
		ink, accent := widget.Ink.Get(th), widget.Accent.Get(th)
		for _, fill := range []color.NRGBA{widget.FieldFill.Get(th), widget.CardFill.Get(th)} {
			pill := tabPill(over(fill, look.RowActive.Get(th)), accent, ink)
			if c := contrast(ink, pill); c < 5 {
				t.Errorf("in %s, the name reads at %.2f:1 on the pill %v", name, c, pill)
			}
			if c := contrast(over(pill, dimmed(ink, pill)), pill); c < 3 {
				t.Errorf("in %s, a dimmed icon reads at %.2f:1 on the pill %v", name, c, pill)
			}
		}
	}
}

// A dimmed icon on a tab behind, on a box not picked, where the icon is
// all the tab shows, reads at 3:1 or better in the light theme and the
// dark, on a terminal's box and on a quiet one.
func TestADimmedIconReadsOnABoxAtRest(t *testing.T) {
	for _, name := range []string{"Dark", "Paper"} {
		themedStage(t, geom.Sz(400, 300), name)
		settle()
		th := lastUI.Theme()
		faint := look.Faint.Get(th)
		for _, fill := range []color.NRGBA{widget.FieldFill.Get(th), widget.CardFill.Get(th)} {
			if c := contrast(over(fill, dimmed(faint, fill)), fill); c < 3 {
				t.Errorf("in %s, a dimmed icon reads at %.2f:1 on %v", name, c, fill)
			}
			if d := dimmed(faint, fill); d == faint && contrast(over(fill, fade(faint, 0.7)), fill) >= 3 {
				t.Errorf("in %s, an icon that reads dimmed on %v is not dimmed", name, fill)
			}
		}
	}
}

// A box of tabs shows those behind as icons or as dots, never between, at
// whatever width it rests, and turns from one to the other over frames.
// The tab in front keeps its icon's room, the dots giving theirs up
// first.
func TestCrowdedTabsRestAsIconsOrDots(t *testing.T) {
	jump := func(v *anim.Float, want float32, _ anim.Spring) { v.Jump(want) }
	for _, n := range []int{8, 10, 14, 16, 21} {
		win, b, _ := startStage(t, manyTabsLayout("many", n))
		th := lastUI.Theme()
		box := *b.m.boxOf("p1")
		key := box.key
		for _, w := range []float32{80, 100, 120, 200, 260, 357, 500} {
			box.r = geom.Rc(0, 0, w, 120)
			b.m.layTabs(&box, th, jump)
			icons := b.m.icons[key].Value()
			if icons != 0 && icons != 1 {
				t.Fatalf("%d tabs in %v px rest %v of the way to icons", n, w, icons)
			}
			avail := w - 8 - 2*float32(n-1)
			front := box.tabs[0].Size().W
			if front < min(2*tabPad+tabIcon, avail)-0.01 {
				t.Errorf("%d tabs in %v px: the tab in front is %v wide", n, w, front)
			}
			for i, tab := range box.tabs {
				if tab.Max.X > box.r.Max.X+0.01 || i > 0 && tab.Min.X < box.tabs[i-1].Max.X-0.01 {
					t.Errorf("%d tabs in %v px: tab %d stands at %v", n, w, i, tab)
				}
				if i > 0 && icons == 1 && tab.Size().W < tabIconLeast-0.01 {
					t.Errorf("%d tabs in %v px: tab %d shows its icon in %v px", n, w, i, tab.Size().W)
				}
			}
		}
		// From icons to dots, over frames.
		box.r = geom.Rc(0, 0, 500, 120)
		b.m.layTabs(&box, th, jump)
		box.r = geom.Rc(0, 0, 60, 120)
		var ons []float32
		ease := func(v *anim.Float, want float32, s anim.Spring) {
			if v.Target() != want {
				v.Animate(want, s)
			}
		}
		for range 40 {
			b.m.layTabs(&box, th, ease)
			b.m.icons[key].Step(time.Second / 60)
			ons = append(ons, b.m.icons[key].Value())
		}
		if ons[0] < 0.5 || ons[len(ons)-1] != 0 {
			t.Fatalf("%d tabs went to dots %v", n, ons)
		}
		noJumps(t, "the icons", ons)
		win.dialog.Close(lastUI)
		settle()
	}
}

// Picking another pane turns the page over a well that stays where it is:
// still from terminal to terminal, and fading its colour, not its place,
// from a terminal to a command.
func TestTheWellStaysAsAPaneIsPicked(t *testing.T) {
	_, b, _ := startStage(t, mixedLayout())
	was := b.well.Value()
	var codes []float32
	b.m.pick("p3", lastUI)
	for range 40 {
		lastWindow.Frame(time.Second / 60)
		if got := b.well.Value(); got != was {
			t.Fatalf("the well moved from %v to %v", was, got)
		}
		codes = append(codes, b.code.Value())
	}
	if codes[0] < 0.5 || codes[len(codes)-1] != 0 {
		t.Fatalf("the well turned from the editor's %v", codes)
	}
	noJumps(t, "the well's colour", codes)
}

// In a narrow window the page's words go on two lines, the pane's name
// first and where it starts under it; in a wide one they share a line.
// A pane named for its shell says the shell once.
func TestTheCaptionWrapsWhenNarrow(t *testing.T) {
	for _, c := range []struct {
		size geom.Size
		two  bool
	}{{geom.Sz(1100, 820), false}, {geom.Sz(420, 760), true}} {
		win, _, publish := windowStageOf(t, c.size)
		publish(startState(shopLayout()))
		settle()
		b := openStartLines(t, win, "shop")
		settle()
		pg := b.pages.Page().(*startPage).caption
		if two := pg.whereAt.Min.Y > 0; two != c.two || pg.nameAt.Min.Y != 0 {
			t.Errorf("at %v, the name stands at %v and where at %v", c.size, pg.nameAt, pg.whereAt)
		}
		b.m.pick("p4", lastUI)
		settle()
		if pg := b.pages.Page().(*startPage).caption; pg.name != "Login Shell" || pg.where != "on db-primary" || !pg.follows {
			t.Errorf("the server's shell says %q %q", pg.name, pg.where)
		}
		win.dialog.Close(lastUI)
		settle()
	}
}

// A pane that has no start lines says what start lines are for and what
// the pane is, and an empty editor shows a command, as a format.
func TestAPaneWithoutLinesSaysWhatItIs(t *testing.T) {
	l := mixedLayout()
	l.Panes["p6"] = settings.SavedPane{Kind: "browser"}
	l.Root.Kids[1].Kids = append(l.Root.Kids[1].Kids, &settings.LayoutNode{ID: "p6", Kind: "item"}, &settings.LayoutNode{ID: "x9", Kind: "item"})
	_, b, _ := startStage(t, l)
	if b.ed.Placeholder != "npm run dev" {
		t.Fatalf("the editor reads %q while empty", b.ed.Placeholder)
	}
	for slot, want := range map[string]string{
		"p3": "Start lines are for terminals. This pane runs a command.",
		"p4": "Start lines are for terminals. This pane shows files.",
		"p5": "Start lines are for terminals kakel starts. This one opens through another kakel window.",
		"p6": "Start lines are for terminals. This kakel can't open this kind of pane.",
		"x9": "Start lines are for terminals. This slot has no pane.",
	} {
		if got := b.why(slot); got != want {
			t.Errorf("%s says %q", slot, got)
		}
	}
}

// The well stays under the page's body on every frame as the window is
// resized, at once or dragged a step a frame: only a pick moves it.
func TestTheWellKeepsToThePageAsTheWindowResizes(t *testing.T) {
	_, b, _ := startStage(t, mixedLayout())
	check := func(when string) {
		t.Helper()
		at, ok := lastUI.Bounds(b)
		body, ok2 := lastUI.Bounds(b.pages.Page().(*startPage).body)
		if !ok || !ok2 {
			t.Fatalf("%s: the dialog is not drawn", when)
		}
		want := body.Add(geom.Pt(-at.Min.X, -at.Min.Y))
		got := b.well.Value()
		if d := max(abs32(got.Min.X-want.Min.X), abs32(got.Min.Y-want.Min.Y), abs32(got.Max.X-want.Max.X), abs32(got.Max.Y-want.Max.Y)); d > 0.5 {
			t.Fatalf("%s: the well stands at %v, the page's body at %v", when, got, want)
		}
	}
	lastWindow.Offscreen().Resize(geom.Sz(700, 560))
	for i := range 30 {
		lastWindow.Frame(time.Second / 60)
		check("frame " + strconv.Itoa(i) + " after a resize")
	}
	for i := range 30 {
		lastWindow.Offscreen().Resize(geom.Sz(700+float32(i)*12, 560+float32(i)*8))
		lastWindow.Frame(time.Second / 60)
		check("frame " + strconv.Itoa(i) + " of a drag")
	}
}

// A screen reader reads the page's caption beside its editor, or beside
// the sentence of a pane that takes no lines, never round it.
func TestTheCaptionIsBesideTheEditor(t *testing.T) {
	_, b, _ := startStage(t, mixedLayout())
	// parentOf is the node holding the node named name of role, and that
	// node.
	parentOf := func(role access.Role, name string) (*access.Node, *access.Node) {
		var walk func(n *access.Node) (*access.Node, *access.Node)
		walk = func(n *access.Node) (*access.Node, *access.Node) {
			for _, k := range n.Children {
				if k.Role == role && k.Name == name {
					return n, k
				}
				if p, at := walk(k); at != nil {
					return p, at
				}
			}
			return nil, nil
		}
		return walk(lastWindow.Access().Root)
	}
	besides := func(caption string, role access.Role, name string) {
		t.Helper()
		capParent, capNode := parentOf(access.RoleLabel, caption)
		if capNode == nil {
			t.Fatalf("no caption %q", caption)
		}
		if len(capNode.Children) != 0 {
			t.Fatalf("the caption %q holds %d nodes", caption, len(capNode.Children))
		}
		if p, at := parentOf(role, name); at == nil || p.ID != capParent.ID {
			t.Fatalf("%q is not beside the caption %q", name, caption)
		}
	}
	besides("Bash on this computer", access.RoleTextField, "Start lines for Bash")
	b.m.pick("p3", lastUI)
	settle()
	besides("tail -f /var/log/syslog, Command on db-primary", access.RoleLabel, "Start lines are for terminals. This pane runs a command.")
}
