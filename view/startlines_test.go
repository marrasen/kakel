package view

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
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
	"github.com/marrasen/kakel/themes"
)

// shopLayout is a window of four panes in two columns, each split in
// two, the left one wider: the backend and the client here, which type
// lines as they open, a log on www, and the database on db-primary.
func shopLayout() settings.SavedLayout {
	one := func(id string) *settings.LayoutNode {
		return &settings.LayoutNode{ID: "#" + id, Kind: "group", Front: id, Kids: []*settings.LayoutNode{{ID: id, Kind: "item"}}}
	}
	return settings.SavedLayout{ID: "shop", Name: "Shop", Place: &settings.WindowPlace{W: 1600, H: 1000},
		Root: &settings.LayoutNode{ID: "#1", Kind: "row", Share: 0.55, Kids: []*settings.LayoutNode{
			{ID: "#2", Kind: "column", Kids: []*settings.LayoutNode{one("p1"), one("p2")}},
			{ID: "#3", Kind: "column", Share: 0.6, Kids: []*settings.LayoutNode{one("p3"), one("p4")}},
		}},
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
	item := func(id string) *settings.LayoutNode { return &settings.LayoutNode{ID: id, Kind: "item"} }
	return settings.SavedLayout{ID: "mixed", Name: "Mixed", Root: &settings.LayoutNode{ID: "#1", Kind: "row", Share: 0.4, Kids: []*settings.LayoutNode{
		{ID: "#2", Kind: "group", Front: "p1", Kids: []*settings.LayoutNode{item("p1")}},
		{ID: "#3", Kind: "group", Front: "p2", Kids: []*settings.LayoutNode{item("p2"), item("p3"), item("p4"), item("p5")}},
	}}, Panes: map[string]settings.SavedPane{
		"p1": {Kind: settings.LayoutTerminal},
		"p2": {Kind: settings.LayoutTerminal, Host: "web", HostID: "s1", Start: []string{"tmux attach"}},
		"p3": {Kind: settings.LayoutCommand, Host: "db-primary", HostID: "s2", Line: "tail -f /var/log/syslog"},
		"p4": {Kind: settings.LayoutFiles, Folder: "/home/me/src"},
		"p5": {Kind: settings.LayoutTerminal, Host: "desk", HostID: "s3", Window: true},
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

// mapRect is where the map draws the pane in slot, in the window.
func mapRect(t *testing.T, b *startLines, slot string) geom.Rect {
	t.Helper()
	lastWindow.Paint()
	at, ok := lastUI.Bounds(b.m)
	if !ok {
		t.Fatal("the map is not drawn")
	}
	return b.m.rectOf(slot).Add(at.Min)
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
	for _, th := range []string{"Dark", "Paper"} {
		win, _, publish := themedStage(t, geom.Sz(1100, 820), th)
		publish(startState(shopLayout(), mixedLayout()))
		settle()
		shot("startlines-" + th + "-1-cards")
		b := openStartLines(t, win, "shop")
		for i := 1; i <= 3; i++ {
			frames(3)
			shot("startlines-" + th + "-2-opening-" + strconv.Itoa(i))
		}
		settle()
		shot("startlines-" + th + "-3-shop")
		// Picked to the right: the mark glides, the page slides in.
		lastUI.Focus(b.m)
		press(gi.KeyRight, 0)
		for i := 1; i <= 4; i++ {
			frames(2)
			shot("startlines-" + th + "-4-gliding-" + strconv.Itoa(i))
		}
		settle()
		shot("startlines-" + th + "-5-log-tail")
		// A line typed for another shell is marked.
		press(gi.KeyTab, 0)
		lastWindow.Input(gi.TextInput{Text: "Get-Content app.log -Wait", Time: time.Now()})
		for i := 1; i <= 2; i++ {
			frames(3)
			shot("startlines-" + th + "-6-typed-" + strconv.Itoa(i))
		}
		settle()
		shot("startlines-" + th + "-7-wrong-shell")
		win.dialog.Close(lastUI)
		settle()

		// The group of tabs: a command picked, then a file manager, then a
		// terminal through a window.
		b = openStartLines(t, win, "mixed")
		settle()
		shot("startlines-" + th + "-8-mixed")
		b.m.pick("p3", lastUI)
		for i := 1; i <= 3; i++ {
			frames(2)
			shot("startlines-" + th + "-9-to-command-" + strconv.Itoa(i))
		}
		settle()
		shot("startlines-" + th + "-10-command")
		b.m.pick("p4", lastUI)
		settle()
		shot("startlines-" + th + "-11-files")
		b.m.pick("p5", lastUI)
		settle()
		shot("startlines-" + th + "-12-through-window")
		win.dialog.Close(lastUI)
		settle()
	}
	// Narrow.
	for _, size := range []geom.Size{geom.Sz(420, 760), geom.Sz(640, 560)} {
		win, _, publish := windowStageOf(t, size)
		publish(startState(shopLayout(), mixedLayout()))
		settle()
		openStartLines(t, win, "shop")
		settle()
		shot("startlines-narrow-" + strconv.Itoa(int(size.W)) + "x" + strconv.Itoa(int(size.H)))
		win.dialog.Close(lastUI)
		settle()
		openStartLines(t, win, "mixed")
		settle()
		shot("startlines-narrow-" + strconv.Itoa(int(size.W)) + "x" + strconv.Itoa(int(size.H)) + "-mixed")
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
// which Tab leaves; a screen reader reads each pane on the map with what
// it is, where, and how many lines it holds.
func TestStartLinesOpensFromTheCard(t *testing.T) {
	win, _, publish := windowStageOf(t, geom.Sz(1100, 820))
	publish(startState(shopLayout(), mixedLayout()))
	settle()
	b := openStartLines(t, win, "shop")
	if win.dialog.Title != "Start Lines for Shop" || b.m.Picked() != "p1" || b.ed == nil || b.ed.Text() != "go run ./cmd/api" {
		t.Fatalf("opened %q on %s holding %v", win.dialog.Title, b.m.Picked(), b.ed)
	}
	if !b.ed.TabMovesOn || kindsOf(b.ed) == "" {
		t.Fatal("the editor takes Tab, or is not coloured for bash")
	}
	settle()
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

// The map is the window's tree in its proportions: the columns side by
// side by their share, each split down by its own, and a group of tabs
// one box with its tabs along its top.
func TestTheMapIsTheWindowsTree(t *testing.T) {
	win, _, publish := windowStageOf(t, geom.Sz(1100, 820))
	publish(startState(shopLayout(), mixedLayout()))
	settle()
	b := openStartLines(t, win, "shop")
	settle()
	r := func(slot string) geom.Rect { return b.m.rectOf(slot) }
	whole := r("p1").Union(r("p4"))
	if got := whole.Size().W / whole.Size().H; got < 1.55 || got > 1.65 {
		t.Errorf("the map is %v, not in the window's proportions", whole)
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

// A click on a pane picks it, and so do the arrow keys, to the pane
// beside on that side, along a box's tabs first. The mark glides there,
// and the page of the pane picked comes in as the last fades, nothing
// jumping in a frame.
func TestAPaneIsPickedByClickAndByKeys(t *testing.T) {
	win, _, publish := windowStageOf(t, geom.Sz(1100, 820))
	publish(startState(shopLayout(), mixedLayout()))
	settle()
	b := openStartLines(t, win, "shop")
	settle()
	old := b.pages.Page()
	at := mapRect(t, b, "p4").Center()
	lastWindow.Input(gi.PointerDown{Pos: at, Button: gi.ButtonPrimary, Clicks: 1, Time: time.Now()})
	lastWindow.Input(gi.PointerUp{Pos: at, Button: gi.ButtonPrimary, Time: time.Now()})
	var xs, ys []float32
	faded := false
	for range 40 {
		lastWindow.Frame(time.Second / 60)
		mark := b.m.mark.Value()
		xs, ys = append(xs, mark.Min.X), append(ys, mark.Min.Y)
		lastWindow.Paint()
		if _, there := lastUI.Bounds(old); there {
			faded = true
		}
	}
	if b.m.Picked() != "p4" || b.ed == nil || b.ed.Text() != "" || !faded {
		t.Fatalf("clicked, %s is picked, its editor %v, the last page faded %v", b.m.Picked(), b.ed, faded)
	}
	if xs[0] == xs[len(xs)-1] || ys[0] == ys[len(ys)-1] || xs[len(xs)-1] != b.m.rectOf("p4").Min.X {
		t.Fatalf("the mark went from %v,%v to %v,%v", xs[0], ys[0], xs[len(xs)-1], ys[len(ys)-1])
	}
	noJumps(t, "the mark across", xs)
	noJumps(t, "the mark down", ys)

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
	tab := mapRect(t, b, "p5").Center()
	lastWindow.Input(gi.PointerDown{Pos: tab, Button: gi.ButtonPrimary, Clicks: 1, Time: time.Now()})
	lastWindow.Input(gi.PointerUp{Pos: tab, Button: gi.ButtonPrimary, Time: time.Now()})
	settle()
	if b.m.Picked() != "p5" || b.m.showing(*b.m.boxOf("p5")) != "p5" {
		t.Fatalf("the tab clicked left %s picked", b.m.Picked())
	}
}

// A command or a file manager is on the map, and picking it shows why
// it holds no lines, with no editor to Tab to; a terminal through a
// kakel window has its editor.
func TestOnlyATerminalHoldsLines(t *testing.T) {
	win, _, publish := windowStageOf(t, geom.Sz(1100, 820))
	publish(startState(shopLayout(), mixedLayout()))
	settle()
	b := openStartLines(t, win, "mixed")
	for _, slot := range []string{"p3", "p4"} {
		b.m.pick(slot, lastUI)
		settle()
		if b.ed != nil || slices.ContainsFunc(b.Focusables(), func(n gunim.Node) bool { _, ok := n.(*widget.CodeEditor); return ok }) {
			t.Fatalf("%s has an editor", slot)
		}
		if b.m.faces[slot].editable {
			t.Fatalf("%s is drawn as holding lines", slot)
		}
	}
	b.m.pick("p5", lastUI)
	settle()
	if b.ed == nil || b.ed.Highlight != nil {
		t.Fatalf("the terminal through desk has editor %v, coloured %v", b.ed, b.ed.Highlight)
	}
}

// Lines typed are what Save sends, cleaned, the sign on the pane's box
// growing in as the first is typed and out as the last goes; Cancel
// sends nothing of them, and Save with nothing changed nothing either.
func TestLinesTypedAreSaved(t *testing.T) {
	win, _, publish := windowStageOf(t, geom.Sz(1100, 820))
	publish(startState(shopLayout(), mixedLayout()))
	settle()
	b := openStartLines(t, win, "shop")
	settle()
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
	want := map[string][]string{"p1": {"  go run ./cmd/api", " make watch"}, "p3": {"tail -f app.log"}}
	if len(in.Lines) != len(want) || !slices.Equal(in.Lines["p1"], want["p1"]) || !slices.Equal(in.Lines["p3"], want["p3"]) {
		t.Fatalf("Save sent the lines %q", in.Lines)
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
