package view

import (
	"slices"
	"testing"
	"time"

	"github.com/marrasen/gunim"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/input"
	"github.com/marrasen/kakel/keys"
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

// File has one line for layouts, which opens a submenu of the layouts
// in the order kept, then Save Layout… and Manage Layouts under a line.
// Layouts takes L in File, and the two commands S and M in the submenu
// whatever the layouts are called, so a layout named Sales takes
// neither. A long name is cut short, so the submenu stays a menu's
// width.
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
	if len(got) != 7 || !slices.Equal(got[:2], []string{"Work", "Here"}) || !slices.Equal(got[3:], []string{"Sales", "Mail", "Save Layout…", "Manage Layouts"}) {
		t.Fatalf("the submenu is %q", got)
	}
	if long := []rune(got[2]); len(long) > menuNameMax || long[len(long)-1] != '…' {
		t.Errorf("the long name shows as %q", got[2])
	}
	if !sub[5].Break || slices.ContainsFunc(sub[:5], func(it widget.MenuItem) bool { return it.Break }) {
		t.Error("the line is not between the layouts and the commands")
	}
	for i, want := range map[int]rune{5: 's', 6: 'm'} {
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
	if got := shownLines(sub); !slices.Equal(got, []string{"Save Layout…", "Manage Layouts"}) || sub[0].Break {
		t.Fatalf("the submenu is %q, a line above it %v", got, sub[0].Break)
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
	if sub[1].Hint != "Ctrl+1" || sub[0].Hint != "" {
		t.Fatalf("Work's hint is %q, Here's %q", sub[0].Hint, sub[1].Hint)
	}
}

// Keyboard alone, in either form of the bar: Alt+F, L, then Down and
// Enter open a layout, S saves the window and Manage Layouts shows the
// pane.
func TestTheLayoutsSubmenuByKeys(t *testing.T) {
	barForms(t, func(t *testing.T, compact bool) {
		win, _ := layoutsStage(t, compact, threeLayouts())
		openLayoutsByKeys(t, win, compact)
		menuKey(gi.KeyDown, 0)
		menuKey(gi.KeyEnter, 0)
		if win.bar.IsOpen() {
			t.Fatal("the pick left the menus open")
		}
		if in := nextIntentPast(t); in != (app.OpenLayout{ID: "here"}) {
			t.Fatalf("Here's line sent %#v", in)
		}

		drain()
		openLayoutsByKeys(t, win, compact)
		for range 4 {
			menuKey(gi.KeyDown, 0)
		}
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
// Layouts is open leave its lines where they were, and the second line
// picked opens the layout shown there, not the one that has come to be
// second since. The new lines come once the menus close.
func TestTheLayoutsSubmenuOpenAsLayoutsChange(t *testing.T) {
	barForms(t, func(t *testing.T, compact bool) {
		win, publish := layoutsStage(t, compact, threeLayouts())
		openLayoutsByKeys(t, win, compact)
		more := append([]settings.SavedLayout{{ID: "new", Name: "New"}}, threeLayouts()...)
		publish(layoutsState(more))
		if got := shownLines(layoutsLine(t, win).Sub.Items); got[0] != "Work" {
			t.Fatalf("the open menus changed under the pointer: %q", got)
		}
		menuKey(gi.KeyDown, 0)
		menuKey(gi.KeyEnter, 0)
		if in := nextIntentPast(t); in != (app.OpenLayout{ID: "here"}) {
			t.Fatalf("the second line sent %#v", in)
		}
		settle()
		if got := shownLines(layoutsLine(t, win).Sub.Items); got[0] != "New" || len(got) != 6 {
			t.Fatalf("closed, the submenu is %q", got)
		}
	})
}
