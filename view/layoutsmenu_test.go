package view

import (
	"slices"
	"testing"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/input"
	"github.com/marrasen/kakel/keys"
	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/settings"
	"github.com/marrasen/kakel/ui"
)

// layoutsLine is File's Layouts line as the bar has it.
func layoutsLine(t *testing.T, win *Window) widget.MenuItem {
	t.Helper()
	var found []widget.MenuItem
	for _, it := range win.bar.Menus[0].Items {
		if shownText(it.Label) == "Layouts" {
			found = append(found, it)
		}
	}
	if len(found) != 1 || found[0].Sub == nil {
		t.Fatalf("File has %d Layouts lines: %+v", len(found), found)
	}
	return found[0]
}

// shownLines are a submenu's lines as drawn.
func shownLines(items []widget.MenuItem) []string {
	var out []string
	for _, it := range items {
		out = append(out, shownText(it.Label))
	}
	return out
}

// barForms runs f with the bar wide and with it behind ☰.
func barForms(t *testing.T, f func(t *testing.T, compact bool)) {
	for _, compact := range []bool{false, true} {
		name := "wide"
		if compact {
			name = "compact"
		}
		t.Run(name, func(t *testing.T) { f(t, compact) })
	}
}

// menuKey presses k with mods, and lets the menus move on every frame
// until they rest.
func menuKey(k gi.Key, mods gi.Mods) {
	lastWindow.Input(gi.KeyPress{Key: k, Mods: mods, Time: time.Now()})
	lastWindow.Input(gi.KeyRelease{Key: k, Mods: mods, Time: time.Now()})
	for range 20 {
		lastWindow.Frame(time.Second / 60)
	}
}

// openLayoutsByKeys opens File › Layouts with the keyboard alone: Alt+F,
// into the menu on a compact bar, and L. The submenu opens with its
// first line lit.
func openLayoutsByKeys(t *testing.T, win *Window, compact bool) {
	t.Helper()
	lastUI.Focus(nil)
	menuKey(gi.KeyF, gi.ModAlt)
	if compact {
		menuKey(gi.KeyRight, 0)
	}
	menuKey(gi.KeyL, 0)
	if !win.bar.IsOpen() || win.status.hinted != "Layouts" {
		t.Fatalf("Alt+F, L leaves the bar open %v on %q", win.bar.IsOpen(), win.status.hinted)
	}
}

// layoutsStage is a window with layouts, its bar wide or compact.
func layoutsStage(t *testing.T, compact bool, layouts []settings.SavedLayout) (*Window, func(app.State)) {
	win, _, publish := windowStage(t)
	win.bar.Compact = compact
	publish(layoutsState(layouts))
	drain()
	return win, publish
}

// File has one line for layouts, which opens a submenu of Save Layout…
// and Manage Layouts, first so a long list never pushes them out of
// view, then under a line the layouts in the order kept, each by its
// whole name. Layouts takes L in File, and the two commands S and M in
// the submenu whatever the layouts are called, so a layout named Sales
// takes neither.
func TestFileOpensTheLayoutsFromOneLine(t *testing.T) {
	win, _, publish := windowStage(t)
	layouts := append(threeLayouts(), settings.SavedLayout{ID: "sales", Name: "Sales"}, settings.SavedLayout{ID: "mail", Name: "Mail"})
	publish(layoutsState(layouts))
	line := layoutsLine(t, win)
	if k, _ := accessKeyOf(line.Label); k != 'l' || line.Icon == nil {
		t.Errorf("Layouts has access key %q and icon %v", k, line.Icon)
	}
	sub := line.Sub.Items
	got := shownLines(sub)
	want := []string{"Save Layout…", "Manage Layouts", "Work", "Here", layouts[2].Name, "Sales", "Mail"}
	if !slices.Equal(got, want) {
		t.Fatalf("the submenu is %q", got)
	}
	if !sub[2].Break || slices.ContainsFunc(append(slices.Clone(sub[:2]), sub[3:]...), func(it widget.MenuItem) bool { return it.Break }) {
		t.Error("the line is not between the commands and the layouts")
	}
	for i, want := range map[int]rune{0: 's', 1: 'm'} {
		if k, _ := accessKeyOf(sub[i].Label); k != want {
			t.Errorf("%s has access key %q", got[i], k)
		}
	}
	keys := map[rune]string{}
	for i, it := range sub {
		if k, ok := accessKeyOf(it.Label); ok {
			if keys[k] != "" {
				t.Errorf("%s and %s share %q", keys[k], got[i], k)
			}
			keys[k] = got[i]
		}
	}
	for _, it := range win.bar.Menus[0].Items {
		if s := shownText(it.Label); s == "Save Layout…" || s == "Manage Layouts" {
			t.Errorf("File still has %s", s)
		}
	}
}

// With no layouts, the submenu holds the two commands and no line.
func TestLayoutsWithNoneHoldsTheCommands(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(layoutsState(nil))
	sub := layoutsLine(t, win).Sub.Items
	if got := shownLines(sub); !slices.Equal(got, []string{"Save Layout…", "Manage Layouts"}) || sub[0].Break || sub[1].Break {
		t.Fatalf("the submenu is %q, with a line in it", got)
	}
}

// A key bound to open a layout shows beside its line, as other lines
// show theirs.
func TestALayoutsKeyShowsInTheMenu(t *testing.T) {
	win, _, publish := windowStage(t)
	st := layoutsState(threeLayouts())
	st.Shortcuts = []keys.Change{{Chord: ui.Chord{Key: input.Key1, Mods: input.ModCtrl}, Command: layoutOpenPrefix + "here", Written: "ctrl+1"}}
	st.ShortcutsRead++
	publish(st)
	sub := layoutsLine(t, win).Sub.Items
	if sub[3].Hint != "Ctrl+1" || sub[2].Hint != "" {
		t.Fatalf("Work's hint is %q, Here's %q", sub[2].Hint, sub[3].Hint)
	}
}

// Keyboard alone, in either form of the bar: Alt+F, L, then Down and
// Enter open a layout, S saves the window and Manage Layouts shows the
// pane.
func TestTheLayoutsSubmenuByKeys(t *testing.T) {
	barForms(t, func(t *testing.T, compact bool) {
		win, _ := layoutsStage(t, compact, threeLayouts())
		openLayoutsByKeys(t, win, compact)
		for range 3 {
			menuKey(gi.KeyDown, 0)
		}
		menuKey(gi.KeyEnter, 0)
		if win.bar.IsOpen() {
			t.Fatal("the pick left the menus open")
		}
		if in := nextIntentPast(t); in != (app.OpenLayout{ID: "here"}) {
			t.Fatalf("Here's line sent %#v", in)
		}

		drain()
		openLayoutsByKeys(t, win, compact)
		menuKey(gi.KeyDown, 0)
		menuKey(gi.KeyEnter, 0)
		if in := nextIntentPast(t); in != (app.ShowLayouts{}) {
			t.Fatalf("Manage Layouts sent %#v", in)
		}

		openLayoutsByKeys(t, win, compact)
		menuKey(gi.KeyS, 0)
		if win.dialog == nil || lastUI.Presence(win.dialog) == gunim.Exiting || win.dialog.Title != "Save Layout" {
			t.Fatal("S opened no Save Layout")
		}
	})
}

// The bar keeps the menus it opened with: layouts changing while File ›
// Layouts is open leave its lines where they were, and the second layout
// picked opens the layout shown there, not the one that has come to be
// second since. The new lines come once the menus close.
func TestTheLayoutsSubmenuOpenAsLayoutsChange(t *testing.T) {
	barForms(t, func(t *testing.T, compact bool) {
		win, publish := layoutsStage(t, compact, threeLayouts())
		openLayoutsByKeys(t, win, compact)
		more := append([]settings.SavedLayout{{ID: "new", Name: "New"}}, threeLayouts()...)
		publish(layoutsState(more))
		if got := shownLines(layoutsLine(t, win).Sub.Items); got[2] != "Work" {
			t.Fatalf("the open menus changed under the pointer: %q", got)
		}
		for range 3 {
			menuKey(gi.KeyDown, 0)
		}
		menuKey(gi.KeyEnter, 0)
		if in := nextIntentPast(t); in != (app.OpenLayout{ID: "here"}) {
			t.Fatalf("the second line sent %#v", in)
		}
		settle()
		if got := shownLines(layoutsLine(t, win).Sub.Items); got[2] != "New" || len(got) != 6 {
			t.Fatalf("closed, the submenu is %q", got)
		}
	})
}

// Menus that waited while the bar was open come once it closes with
// nothing picked, by Escape or by a press outside, in either form.
func TestWaitingMenusComeAsTheBarClosesUnpicked(t *testing.T) {
	more := append([]settings.SavedLayout{{ID: "new", Name: "New"}}, threeLayouts()...)
	for _, how := range []string{"escape", "outside"} {
		barForms(t, func(t *testing.T, compact bool) {
			win, publish := layoutsStage(t, compact, threeLayouts())
			openLayoutsByKeys(t, win, compact)
			publish(layoutsState(more))
			if got := shownLines(layoutsLine(t, win).Sub.Items); got[2] != "Work" {
				t.Fatalf("the open menus changed: %q", got)
			}
			switch how {
			case "escape":
				for i := 0; i < 4 && win.bar.IsOpen(); i++ {
					menuKey(gi.KeyEscape, 0)
				}
			case "outside":
				at := geom.Pt(880, 580)
				lastWindow.Inject(gi.PointerDown{Pos: at, Button: gi.ButtonPrimary, Clicks: 1, Time: time.Now()})
				lastWindow.Inject(gi.PointerUp{Pos: at, Button: gi.ButtonPrimary, Time: time.Now()})
			}
			settle()
			if win.bar.IsOpen() {
				t.Fatalf("%s left the menus open", how)
			}
			if got := shownLines(layoutsLine(t, win).Sub.Items); got[2] != "New" {
				t.Fatalf("closed by %s, the submenu is %q", how, got)
			}
			if in := nextIntentOr(); in != nil {
				t.Fatalf("closed by %s, the bar sent %#v", how, in)
			}
		})
	}
}

// nextIntentOr is the next intent waiting, past focus moves, or nil.
func nextIntentOr() gunim.Intent {
	for len(lastWindow.Client().Intents()) > 0 {
		in := (<-lastWindow.Client().Intents()).Intent
		if _, focus := in.(app.FocusPane); !focus {
			return in
		}
	}
	return nil
}

// A server picked in the Machines menu is the one it showed, though the
// list of servers changed while the menu was open.
func TestTheMachinesMenuPicksWhatItShowed(t *testing.T) {
	win, publish := layoutsStage(t, false, nil)
	m := win.menuAt("Machines")
	lastUI.Focus(nil)
	win.bar.Open(m, lastUI)
	settle()
	// Down onto the first server, www, under the caption.
	menuKey(gi.KeyDown, 0)
	st := layoutsState(nil)
	st.Saved = append([]remote.Host{{ID: "s0", Name: "aaa", Address: "aaa.example"}}, st.Saved...)
	publish(st)
	if got := shownText(win.bar.Menus[m].Items[1].Label); got != "www" {
		t.Fatalf("the open Machines menu changed: its first server is %q", got)
	}
	drain()
	menuKey(gi.KeyEnter, 0)
	if in, ok := nextIntentPast(t).(app.ConnectTo); !ok || in.Server != "s1" {
		t.Fatalf("picking www (s1) sent %#v", in)
	}
	settle()
	if got := shownText(win.bar.Menus[m].Items[1].Label); got != "aaa" {
		t.Fatalf("closed, the Machines menu's first server is %q", got)
	}
}

// A layout is picked by the pointer as by the keys: resting on Layouts
// opens the submenu, and a click on a layout's line opens it.
func TestALayoutIsPickedByThePointer(t *testing.T) {
	win, _ := layoutsStage(t, false, threeLayouts())
	bar, _ := lastUI.Bounds(win.bar)
	move := func(at geom.Point) {
		lastWindow.Inject(gi.PointerMove{Pos: at, Time: time.Now()})
		lastWindow.Frame(time.Second / 60)
	}
	click := func(at geom.Point) {
		lastWindow.Inject(gi.PointerDown{Pos: at, Button: gi.ButtonPrimary, Clicks: 1, Time: time.Now()})
		lastWindow.Inject(gi.PointerUp{Pos: at, Button: gi.ButtonPrimary, Time: time.Now()})
		settle()
	}
	// Where File's Layouts line is, by the line the bottom says the
	// pointer is on, from the top of File to its foot.
	win.bar.Open(0, lastUI)
	settle()
	x := bar.Min.X + 60
	var top, foot float32 = -1, -1
	for y := bar.Max.Y; y < bar.Max.Y+400; y++ {
		move(geom.Pt(x, y))
		on := win.status.hinted == "Layouts"
		if on && top < 0 {
			top = y
		}
		if !on && top >= 0 {
			foot = y
			break
		}
	}
	if top < 0 || foot < 0 {
		t.Fatal("File has no Layouts line under the pointer")
	}
	row := widget.MenuRowHeight.Get(lastUI.Theme())
	// The line under a break: its row is the last row high of the span.
	layouts := geom.Pt(x, foot-row/2)
	// The submenu opens beside File, its first line level with Layouts:
	// the first click across from Layouts that lands on Save Layout…
	// is in the submenu.
	sub := float32(-1)
	for at := x + 100; at < x+500 && sub < 0; at += 12 {
		if !win.bar.IsOpen() {
			win.bar.Open(0, lastUI)
			settle()
		}
		move(layouts)
		settle()
		click(geom.Pt(at, layouts.Y))
		if win.dialog != nil && lastUI.Presence(win.dialog) != gunim.Exiting {
			sub = at
			win.dialog.Close(lastUI)
			settle()
		}
	}
	if sub < 0 {
		t.Fatal("no click beside Layouts landed on Save Layout…")
	}
	// Here is the fourth line: under Save Layout…, Manage Layouts, a
	// break and Work.
	drain()
	win.bar.Open(0, lastUI)
	settle()
	move(layouts)
	settle()
	brk := (foot - top) - row
	click(geom.Pt(sub+10, layouts.Y+3*row+brk))
	if in := nextIntentPast(t); in != (app.OpenLayout{ID: "here"}) {
		t.Fatalf("the click on Here sent %#v", in)
	}
	if win.bar.IsOpen() {
		t.Fatal("the pick left the menus open")
	}
}
