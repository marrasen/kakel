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
	"github.com/marrasen/kakel/input"
	"github.com/marrasen/kakel/keys"
	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/settings"
	"github.com/marrasen/kakel/ui"
)

// threeLayouts are a layout on three machines, one of one terminal
// here, opened at start, and one with a long name.
func threeLayouts() []settings.SavedLayout {
	item := func(id string) *settings.LayoutNode { return &settings.LayoutNode{ID: id, Kind: "item"} }
	return []settings.SavedLayout{
		{ID: "work", Name: "Work", Root: &settings.LayoutNode{ID: "#1", Kind: "row", Kids: []*settings.LayoutNode{
			{ID: "#2", Kind: "group", Front: "p1", Kids: []*settings.LayoutNode{item("p1"), item("p2")}},
			{ID: "#3", Kind: "group", Front: "p3", Kids: []*settings.LayoutNode{item("p3"), item("p4"), item("p5")}},
		}}, Panes: map[string]settings.SavedPane{
			"p1": {Kind: settings.LayoutTerminal},
			"p2": {Kind: settings.LayoutTerminal, Host: "web", HostID: "s1"},
			"p3": {Kind: settings.LayoutCommand, Host: "db-primary", HostID: "s2", Line: "tail -f /var/log/syslog"},
			"p4": {Kind: settings.LayoutFiles, Folder: "/home/me"},
			"p5": {Kind: settings.LayoutTerminal, Host: "web", HostID: "s1"},
		}},
		{ID: "here", Name: "Here", AtStart: true, Root: item("p1"), Panes: map[string]settings.SavedPane{"p1": {Kind: settings.LayoutTerminal}}},
		{ID: "long", Name: "Deploying the whole of the shop front on a Friday afternoon, with the database too", Root: item("p1"),
			Panes: map[string]settings.SavedPane{"p1": {Kind: settings.LayoutTerminal, Host: "web", HostID: "s1"}}},
	}
}

// layoutsState is a window showing the Layouts pane, with layouts,
// and a file manager in a tab behind it. Save Layout saves this window.
// The server the layouts saved as web is called www now.
func layoutsState(layouts []settings.SavedLayout) app.State {
	return app.State{
		Window: 1,
		Saved:  []remote.Host{{ID: "s1", Name: "www", Address: "web.example"}, {ID: "s2", Name: "db-primary", Address: "10.0.0.12"}},
		Panes:  []app.Pane{{ID: "p1", Title: "Layouts", Kind: app.KindLayouts}, {ID: "f1", Title: "home", Kind: app.KindFileManager}},
		Layout: boxLayout(&app.Box{Pane: "p1"}, "f1"), Focus: "p1",
		Layouts: layouts, SaveWindow: 1,
	}
}

// behind is st with the file manager in front, and the Layouts pane in
// the tab behind it.
func behind(st app.State) app.State {
	st.Layout, st.Focus = boxLayout(&app.Box{Pane: "f1"}, "p1"), "f1"
	return st
}

// pageText is what a card's cross-fading words say now.
func pageText(p *widget.Pages) string { return p.Page().(*widget.Label).Text }

// chipsOf are the machines a card names, in order.
func chipsOf(c *layoutCard) []string {
	var out []string
	for _, h := range c.order {
		out = append(out, h.Name)
	}
	return out
}

// cardOf is the card of the layout with id.
func cardOf(t *testing.T, win *Window, id string) *layoutCard {
	t.Helper()
	for _, k := range win.layoutsView.list.Keys() {
		if c, ok := widget.RowOf[*layoutCard](win.layoutsView.list, k); ok && c.id == id {
			return c
		}
	}
	t.Fatalf("no card for %s", id)
	return nil
}

// heightsOf is how tall card c is drawn, frame by frame, for n frames.
func heightsOf(c gunim.Node, n int) []float32 {
	var out []float32
	for range n {
		lastWindow.Frame(time.Second / 60)
		r, _ := lastUI.Bounds(c)
		out = append(out, r.Size().H)
	}
	return out
}

// noJumps fails where what is measured moves by more than half of all
// it moves in one frame.
func noJumps(t *testing.T, what string, heights []float32) {
	t.Helper()
	span := slices.Max(heights) - slices.Min(heights)
	for i := 1; i < len(heights); i++ {
		if d := heights[i] - heights[i-1]; d > span/2 || d < -span/2 {
			t.Fatalf("%s jumped from %v to %v in a frame: %v", what, heights[i-1], heights[i], heights)
		}
	}
}

// The pane shows a card for each layout, saying what it opens and on
// which machines, by the names the servers have now, and whether it
// opens at start. Its buttons and switch ask for what they say.
func TestTheLayoutsPaneShowsACardForEachLayout(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(layoutsState(threeLayouts()))
	settle()
	work := cardOf(t, win, "work")
	if got := pageText(work.holds); got != "3 terminals, 1 command and 1 file manager" {
		t.Errorf("Work says it holds %q", got)
	}
	if got := chipsOf(work); !slices.Equal(got, []string{"This computer", "www", "db-primary"}) {
		t.Errorf("Work's machines are %q", got)
	}
	if work.start.Checked() || !cardOf(t, win, "here").start.Checked() {
		t.Error("the switches say the wrong layout opens at start")
	}
	if a := work.Access(); a.Role != access.RoleListItem || a.Name != "Work" {
		t.Errorf("a screen reader reads the card as %+v", a)
	}

	drain()
	for _, c := range []struct {
		press func()
		want  gunim.Intent
	}{
		{func() { lastUI.Focus(work.open) }, app.OpenLayout{ID: "work"}},
		{func() { lastUI.Focus(work.start) }, app.SetLayoutAtStart{ID: "work", On: true}},
	} {
		c.press()
		lastWindow.Input(gi.KeyPress{Key: gi.KeySpace, Time: time.Now()})
		lastWindow.Frame(time.Second / 60)
		if in := nextIntent(t); in != c.want {
			t.Errorf("pressed, the card sent %#v, want %#v", in, c.want)
		}
	}

	lastUI.Focus(work.rename)
	lastWindow.Input(gi.KeyPress{Key: gi.KeySpace, Time: time.Now()})
	lastWindow.Frame(time.Second / 60)
	if win.dialog == nil || win.dialog.Title != "Rename Layout" {
		t.Fatalf("Rename opened %+v", win.dialog)
	}
	name := formOf(win.dialog.Body).Focusables()[0].(*widget.TextField)
	name.SetText(" here ", nil)
	if said := win.dialog.Check(); said != "Name already used" {
		t.Errorf("renamed to another's name, the dialog says %q", said)
	}
	name.SetText(" Office ", nil)
	if in := win.dialog.OnAccept(lastUI); in != (app.RenameLayout{ID: "work", Name: "Office"}) {
		t.Errorf("renamed, the dialog sent %#v", in)
	}
}

// Renamed, a layout's card stays and its name fades across to the new
// one, the list not moving; a server renamed, its chip shrinks out as
// the new name's grows in; saved over with more panes, what it holds
// fades across.
func TestALayoutCardChangesInPlace(t *testing.T) {
	win, _, publish := windowStage(t)
	all := threeLayouts()
	publish(layoutsState(all))
	settle()
	work := cardOf(t, win, "work")
	height := win.layoutsView.list.Height()
	old := work.name.Page()

	all[0].Name = "Office"
	publish(layoutsState(all))
	var heights []float32
	stayed := false
	for range 30 {
		lastWindow.Frame(time.Second / 60)
		heights = append(heights, win.layoutsView.list.Height())
		if _, there := lastUI.Bounds(old); there {
			stayed = true
		}
	}
	if cardOf(t, win, "work") != work || pageText(work.name) != "Office" || !stayed {
		t.Fatalf("renamed, the card says %q, the same card %v, the old name faded %v", pageText(work.name), cardOf(t, win, "work") == work, stayed)
	}
	for _, h := range heights {
		if h < height-2 || h > height+2 {
			t.Fatalf("renamed, the list went from %v to %v", height, heights)
		}
	}

	st := layoutsState(all)
	st.Saved[0].Name = "web2"
	publish(st)
	gone := work.hosts[app.LayoutHost{Name: "www"}]
	if gone != nil {
		t.Fatal("the chip of the old name is still kept")
	}
	if got := chipsOf(work); !slices.Equal(got, []string{"This computer", "web2", "db-primary"}) {
		t.Fatalf("the server renamed, the machines are %q", got)
	}
	var widths []float32
	fold := work.hosts[app.LayoutHost{Name: "web2"}]
	for range 30 {
		r, _ := lastUI.Bounds(fold)
		widths = append(widths, r.Size().W)
		lastWindow.Frame(time.Second / 60)
	}
	if widths[0] > widths[len(widths)-1]/2 {
		t.Fatalf("the new chip came at once: %v", widths)
	}
	noJumps(t, "the new chip", widths)

	all[0].Panes["p6"] = settings.SavedPane{Kind: settings.LayoutTerminal}
	all[0].Root.Kids[0].Kids = append(all[0].Root.Kids[0].Kids, &settings.LayoutNode{ID: "p6", Kind: "item"})
	publish(layoutsState(all))
	if cardOf(t, win, "work") != work || pageText(work.holds) != "4 terminals, 1 command and 1 file manager" {
		t.Fatalf("saved over, the card says %q", pageText(work.holds))
	}
}

// A layout added grows in, and one removed shrinks out where it stood
// while the others close up, nothing jumping in a frame. Remove asks
// for it, and once it has gone the keyboard goes to Remove on the card
// after it; refused, the keyboard stays where it was.
func TestLayoutCardsGrowInAndShrinkOut(t *testing.T) {
	win, _, publish := windowStage(t)
	all := threeLayouts()
	publish(layoutsState(all[:2]))
	settle()

	before := win.layoutsView.list.Height()
	publish(layoutsState(all))
	var grown []float32
	for range 40 {
		lastWindow.Frame(time.Second / 60)
		grown = append(grown, win.layoutsView.list.Height()-before)
	}
	if grown[0] > grown[len(grown)-1]/2 {
		t.Fatalf("the card added came at once: %v", grown)
	}
	noJumps(t, "the card added", grown)

	here := cardOf(t, win, "here")
	drain()
	lastUI.Focus(here.remove)
	lastWindow.Input(gi.KeyPress{Key: gi.KeySpace, Time: time.Now()})
	lastWindow.Frame(time.Second / 60)
	if in := nextIntent(t); in != (app.RemoveLayout{ID: "here"}) {
		t.Fatalf("Remove sent %#v", in)
	}
	// Refused: the program says so, and the layout stays.
	publish(layoutsState(all))
	if lastUI.Focused() != gunim.Node(here.remove) {
		t.Fatalf("Remove refused, the keyboard is on %T", lastUI.Focused())
	}

	work := cardOf(t, win, "work")
	lastUI.Focus(work.remove)
	lastWindow.Input(gi.KeyPress{Key: gi.KeySpace, Time: time.Now()})
	lastWindow.Frame(time.Second / 60)
	publish(layoutsState(all[1:]))
	var tops, heights []float32
	for range 60 {
		r, _ := lastUI.Bounds(here)
		tops = append(tops, r.Min.Y)
		heights = append(heights, win.layoutsView.list.Height())
		lastWindow.Frame(time.Second / 60)
	}
	noJumps(t, "the list", heights)
	noJumps(t, "the card after it", tops)
	if tops[0] <= tops[len(tops)-1] {
		t.Fatalf("the card after the one removed did not close up: %v", tops)
	}
	if lastUI.Focused() != gunim.Node(here.remove) {
		t.Fatalf("after Remove the keyboard is on %T", lastUI.Focused())
	}
}

// A dialog a card opened gives the keyboard back to the control that
// opened it, wherever its card stands, once the state that follows has
// come; Save Layout's to Save Layout.
func TestTheKeyboardComesBackWhereItWas(t *testing.T) {
	win, _, publish := windowStage(t)
	all := threeLayouts()
	publish(layoutsState(all))
	settle()
	long := cardOf(t, win, "long")
	lastUI.Focus(long.rename)
	lastWindow.Input(gi.KeyPress{Key: gi.KeySpace, Time: time.Now()})
	lastWindow.Frame(time.Second / 60)
	lastWindow.Input(gi.KeyPress{Key: gi.KeyEscape, Time: time.Now()})
	settle()
	publish(layoutsState(all))
	settle()
	if lastUI.Focused() != gunim.Node(long.rename) {
		t.Fatalf("Rename cancelled, the keyboard is on %T", lastUI.Focused())
	}

	// The first layout renamed in place: the keyboard stays on its card.
	work := cardOf(t, win, "work")
	lastUI.Focus(work.open)
	all[0].Name = "Office"
	publish(layoutsState(all))
	settle()
	if lastUI.Focused() != gunim.Node(work.open) {
		t.Fatalf("renamed, the keyboard is on %T", lastUI.Focused())
	}
}

// Off stage, the pane misses the first layout being removed elsewhere;
// brought back, it takes the keyboard to a card still shown.
func TestALayoutGoneOffStage(t *testing.T) {
	win, _, publish := windowStage(t)
	all := threeLayouts()
	publish(layoutsState(all))
	settle()
	lastUI.Focus(cardOf(t, win, "work").open)
	publish(behind(layoutsState(all)))
	for range 300 {
		lastWindow.Frame(time.Second / 60)
	}
	publish(behind(layoutsState(all[1:])))
	publish(layoutsState(all[1:]))
	settle()
	if f := lastUI.Focused(); f == nil || !win.layoutsView.inLive(f, lastUI) {
		t.Fatalf("back on stage, the keyboard is on %T", f)
	}
	if _, ok := win.layoutsView.cards["work"]; ok {
		t.Fatal("back on stage, the pane still shows the layout removed")
	}
}

// Enter, then Enter again, in a Save Layout opened over the pane saves,
// and opens nothing.
func TestEnterTwiceInSaveLayoutOpensNothing(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(layoutsState(threeLayouts()))
	settle()
	drain()
	win.run("layout.save", lastUI)
	lastWindow.Frame(time.Second / 60)
	typeInto(formOf(win.dialog.Body).Focusables()[0].(*widget.TextField), "Evening")
	for range 2 {
		lastWindow.Input(gi.KeyPress{Key: gi.KeyEnter, Time: time.Now()})
		lastWindow.Input(gi.KeyRelease{Key: gi.KeyEnter, Time: time.Now()})
		settle()
	}
	saved := false
	for len(lastWindow.Client().Intents()) > 0 {
		switch in := (<-lastWindow.Client().Intents()).Intent.(type) {
		case app.SaveLayout:
			saved = in.Name == "Evening"
		case app.OpenLayout:
			t.Fatalf("the second Enter opened %s", in.ID)
		}
	}
	if !saved {
		t.Fatal("the first Enter saved nothing")
	}
}

// With no window to save, Save Layout says so and asks nothing.
func TestSaveLayoutWithNothingToSave(t *testing.T) {
	win, _, publish := windowStage(t)
	st := layoutsState(nil)
	st.SaveWindow = 0
	publish(st)
	win.run("layout.save", lastUI)
	if win.dialog != nil && lastUI.Presence(win.dialog) != gunim.Exiting {
		t.Fatalf("with nothing to save, %q opened", win.dialog.Title)
	}
}

// With no layouts the pane says how to make one, and those words fold
// away as the first card grows in, rather than vanish.
func TestTheEmptyLayoutsPaneSaysHowToMakeOne(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(layoutsState(nil))
	settle()
	p := win.layoutsView
	if !p.empty.Open() || lastUI.Focused() != gunim.Node(p.save) {
		t.Fatalf("empty, the words show %v and the keyboard is on %T", p.empty.Open(), lastUI.Focused())
	}
	full, _ := lastUI.Bounds(p.empty)
	publish(layoutsState(threeLayouts()[:1]))
	heights := heightsOf(p.empty, 40)
	if heights[0] < full.Size().H/2 || heights[len(heights)-1] != 0 {
		t.Fatalf("the words were %v tall, then %v", full.Size().H, heights)
	}
	noJumps(t, "the words", heights)
}

// Save Layout saves the window the program names, and offers the name
// of the layout that window came from. While
// the name typed is another layout's, in any case and spacing, a line
// grows in under it saying that one is replaced, and Save asks to
// replace it; a free name folds the line away and saves a new one.
func TestSaveLayoutSaysWhatItReplaces(t *testing.T) {
	win, _, publish := windowStage(t)
	st := layoutsState(threeLayouts())
	st.SaveWindow, st.SaveFrom = 2, "here"
	publish(st)
	win.run("layout.save", lastUI)
	d := win.dialog
	if d == nil || d.Title != "Save Layout" {
		t.Fatalf("Save Layout opened %+v", d)
	}
	form := formOf(d.Body)
	name := form.Focusables()[0].(*widget.TextField)
	line := form.Children()[3].(*widget.Fold)
	if name.Text() != "Here" || !line.Open() {
		t.Fatalf("the name offered is %q, the line open %v", name.Text(), line.Open())
	}
	settle()

	typeInto(name, "Mine")
	heights := heightsOf(line, 40)
	if heights[0] == 0 || heights[len(heights)-1] != 0 {
		t.Fatalf("a free name, the line was %v", heights)
	}
	noJumps(t, "the line", heights)
	if in := d.OnAccept(lastUI); in != (app.SaveLayout{Window: 2, Name: "Mine"}) {
		t.Fatalf("saved under a free name, the dialog sent %#v", in)
	}

	typeInto(name, "  work ")
	heights = heightsOf(line, 40)
	if heights[0] > heights[len(heights)-1]/2 || !line.Open() {
		t.Fatalf("a taken name, the line was %v", heights)
	}
	noJumps(t, "the line", heights)
	if said := line.Children()[0].(*widget.Label).Text; said != "Replaces the layout Work." {
		t.Errorf("the line says %q", said)
	}
	if in := d.OnAccept(lastUI); in != (app.SaveLayout{Window: 2, Name: "work", Replace: true}) {
		t.Fatalf("saved under a taken name, the dialog sent %#v", in)
	}
	// Work removed elsewhere while the dialog is open: the line folds.
	st.Layouts = st.Layouts[1:]
	publish(st)
	settle()
	if line.Open() {
		t.Fatal("Work removed, the line still says it is replaced")
	}
	name.SetText("  ", nil)
	if said := d.Check(); said != "Enter a name" {
		t.Fatalf("with no name the dialog says %q", said)
	}
}

// typeInto empties field f and types s into it, as its OnChange hears.
func typeInto(f *widget.TextField, s string) {
	lastUI.Focus(f)
	f.SetText("", lastUI)
	lastWindow.Input(gi.TextInput{Text: s, Time: time.Now()})
	lastWindow.Frame(time.Second / 60)
}

// Screenshots of the Layouts pane and the Save Layout dialog, with
// KAKEL_SHOTS set to a folder, under a private X server.
func TestShotsLayouts(t *testing.T) {
	dir := os.Getenv("KAKEL_SHOTS")
	if dir == "" {
		t.Skip("KAKEL_SHOTS names no folder to write the screenshots to")
	}
	shot := func(name string) {
		gunimtest.WritePNG(t, filepath.Join(dir, name+".png"), gunimtest.Pixels(t, lastWindow))
	}
	win, _, publish := windowStageOf(t, geom.Sz(1000, 640))
	publish(layoutsState(nil))
	settle()
	shot("layouts-1-empty")
	all := threeLayouts()
	publish(layoutsState(all))
	for i := 1; i <= 3; i++ {
		frames(3)
		shot("layouts-2-adding-" + strconv.Itoa(i))
	}
	settle()
	shot("layouts-3-three")
	publish(layoutsState(all[1:]))
	for i := 1; i <= 3; i++ {
		frames(3)
		shot("layouts-4-removing-" + strconv.Itoa(i))
	}
	settle()
	shot("layouts-5-removed")
	publish(layoutsState(all))
	settle()
	win.run("layout.save", lastUI)
	settle()
	shot("save-1-empty")
	name := formOf(win.dialog.Body).Focusables()[0].(*widget.TextField)
	typeInto(name, "Evening")
	settle()
	shot("save-2-free")
	typeInto(name, "work")
	for i := 1; i <= 3; i++ {
		frames(3)
		shot("save-3-taken-" + strconv.Itoa(i))
	}
	settle()
	shot("save-4-taken")
	typeInto(name, "work2")
	for i := 1; i <= 2; i++ {
		frames(3)
		shot("save-5-free-again-" + strconv.Itoa(i))
	}
	win.dialog.Close(lastUI)
	settle()

	// Narrow, the buttons go under the words, and a long name wraps.
	// The palette and the menus open in windows of their own, which
	// these pictures do not hold.
	for _, width := range []float32{280, 340, 640, 1000} {
		_, _, sized := windowStageOf(t, geom.Sz(width, 900))
		sized(layoutsState(all))
		settle()
		shot("layouts-6-width-" + strconv.Itoa(int(width)))
	}
	// Renamed in place, frame by frame.
	_, _, publish = windowStageOf(t, geom.Sz(1000, 640))
	publish(layoutsState(all))
	settle()
	renamed := threeLayouts()
	renamed[0].Name = "Office"
	publish(layoutsState(renamed))
	for i := 1; i <= 3; i++ {
		frames(3)
		shot("layouts-7-renaming-" + strconv.Itoa(i))
	}
	settle()
	shot("layouts-8-renamed")
}

// In a pane as narrow as 280, every control on a card is on the card.
func TestNarrowLayoutCardsCutNothing(t *testing.T) {
	for _, width := range []float32{280, 340} {
		win, _, publish := windowStageOf(t, geom.Sz(width, 900))
		publish(layoutsState(threeLayouts()))
		settle()
		for _, id := range []string{"work", "here", "long"} {
			c := cardOf(t, win, id)
			card, _ := lastUI.Bounds(c)
			for i := range ctlCount {
				r, ok := lastUI.Bounds(c.control(i))
				if !ok || r.Min.X < card.Min.X || r.Max.X > card.Max.X || r.Max.Y > card.Max.Y {
					t.Errorf("at %v wide, %s's control %d stands at %v on a card at %v", width, id, i, r, card)
				}
			}
		}
	}
}

// The palette has a line for each layout, in the launcher's words, and
// picking one opens it. Save Layout and Manage Layouts are among the
// commands the shortcut editor lists, to be given a key.
func TestThePaletteOpensALayout(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(layoutsState(threeLayouts()))
	i := slices.IndexFunc(win.palette.Items, func(it widget.PaletteItem) bool { return it.Title == "Work" })
	if i < 0 || win.palette.Items[i].Detail != app.LayoutNote(threeLayouts()[0]) || !slices.Contains(win.palette.Items[i].Also, "db-primary") {
		t.Fatalf("the palette has %+v", win.palette.Items)
	}
	drain()
	win.palette.OnPick(i, lastUI)
	if in := nextIntent(t); in != (app.OpenLayout{ID: "work"}) {
		t.Fatalf("picked, the palette sent %#v", in)
	}
	listed := map[string]bool{}
	for _, s := range helpSections(func(string) string { return "" }) {
		for _, l := range s.lines {
			listed[l[2]] = true
		}
	}
	if !listed["layout.save"] || !listed["layout.manage"] || !isItem(layoutOpenPrefix+"work") {
		t.Fatal("the layouts' commands cannot be given a key")
	}
}

// The shortcut editor has a line to open each layout, which comes and
// goes with the layout; a key bound there opens it. A key left bound to
// a layout removed is listed as such, to take off or to work again
// should the layout come back.
func TestALayoutCanBeGivenAKey(t *testing.T) {
	win, publish, st := shortcutsStage(t)
	st.Layouts = threeLayouts()
	publish(st)
	p := win.shortcuts
	if l := lineOf(t, p, layoutOpenPrefix+"work"); l.title != "Open Work" || p.lines[l.group].title != "Layouts" {
		t.Fatalf("Work's line is %+v under %q", l, p.lines[l.group].title)
	}
	ctrl1 := ui.Chord{Key: input.Key1, Mods: input.ModCtrl}
	st.Shortcuts = []keys.Change{{Chord: ctrl1, Command: layoutOpenPrefix + "work", Written: "ctrl+1"}}
	st.ShortcutsRead++
	publish(st)
	if got := win.keys.ChordsFor(layoutOpenPrefix + "work"); !slices.Equal(got, []ui.Chord{ctrl1}) {
		t.Fatalf("Work's keys are %v", got)
	}
	// Pressed with the Layouts pane in front.
	on := layoutsState(st.Layouts)
	on.Shortcuts, on.ShortcutsRead = st.Shortcuts, st.ShortcutsRead
	publish(on)
	settle()
	drain()
	lastWindow.Input(gi.KeyPress{Key: gi.Key1, Mods: gi.ModControl, Time: time.Now()})
	lastWindow.Frame(time.Second / 60)
	if in := nextIntentPast(t); in != (app.OpenLayout{ID: "work"}) {
		t.Fatalf("its key sent %#v", in)
	}
	publish(st)

	st.Layouts = st.Layouts[1:]
	publish(st)
	if l := lineOf(t, p, layoutOpenPrefix+"work"); l.title != "Open Removed Layout work" {
		t.Fatalf("removed, Work's line is %+v", l)
	}
	st.Layouts = threeLayouts()
	publish(st)
	if l := lineOf(t, p, layoutOpenPrefix+"work"); l.title != "Open Work" {
		t.Fatalf("back, Work's line is %+v", l)
	}
}
