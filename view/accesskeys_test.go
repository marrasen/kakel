package view

import (
	"testing"
	"time"
	"unicode"

	"github.com/marrasen/gunim"
	"github.com/marrasen/kakel/app"

	"github.com/marrasen/gunim/filemanager"
	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/widget"
)

// accessKeyOf is the letter a menu line is picked by, and whether it
// has one.
func accessKeyOf(s string) (rune, bool) {
	rs := []rune(s)
	for i := 0; i < len(rs)-1; i++ {
		if rs[i] == '&' {
			if rs[i+1] == '&' {
				i++
				continue
			}
			return unicode.ToLower(rs[i+1]), true
		}
	}
	return 0, false
}

// Every menu has a letter of its own on the bar, and every line in a
// menu a letter no other line in it has, so Alt and the letters reach
// each one.
func TestEveryMenuLineHasItsOwnAccessKey(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(app.State{Fonts: []string{"Go Mono (bundled)", "PxPlus IBM VGA8", "DejaVu Sans Mono"}})
	titles := map[rune]string{}
	for _, m := range win.bar.Menus {
		k, ok := accessKeyOf(m.Title)
		if !ok || titles[k] != "" {
			t.Errorf("the menu %q has access key %q, which %q has too", m.Title, k, titles[k])
		}
		titles[k] = m.Title
		lines := map[rune]string{}
		for _, it := range m.Items {
			if it.Caption {
				continue
			}
			k, ok := accessKeyOf(it.Label)
			if !ok || lines[k] != "" {
				t.Errorf("in %q, %q has access key %q, which %q has too", shownText(m.Title), it.Label, k, lines[k])
			}
			lines[k] = it.Label
		}
	}
}

// A & in a line's own text shows, and marks no key.
func TestAnAmpersandInALineShows(t *testing.T) {
	m := withAccessKeys(widget.BarMenu{Title: "Servers", Items: widget.Labels("R&D box")})
	if got := shownText(m.Items[0].Label); got != "R&D box" {
		t.Fatalf("the line shows as %q", got)
	}
	if k, _ := accessKeyOf(m.Items[0].Label); k != 'r' {
		t.Fatalf("its access key is %q", k)
	}
}

// Alt and a letter in a terminal is the terminal's, as a shell moves by
// words with it; the menus open by it only where it is nobody's.
func TestAltAndALetterInATerminalIsTheTerminals(t *testing.T) {
	win, tm := termStage(t, geom.Sz(900, 600))
	lastUI.Focus(tm)
	lastWindow.Input(gi.KeyPress{Key: gi.KeyF, Mods: gi.ModAlt, Time: time.Now()})
	lastWindow.Frame(time.Second / 60)
	if win.bar.IsOpen() {
		t.Fatal("Alt+F in a terminal opened a menu")
	}
	lastUI.Focus(nil)
	lastWindow.Input(gi.KeyPress{Key: gi.KeyF, Mods: gi.ModAlt, Time: time.Now()})
	lastWindow.Frame(time.Second / 60)
	if !win.bar.IsOpen() {
		t.Fatal("Alt+F with the keyboard nowhere opened no menu")
	}
}

// A menu line that cannot act on the pane in front is greyed: Disconnect
// and Find in Scrollback on a file manager pane here, and Disconnect
// and the connection's log come back for one on a server.
func TestMenuLinesThatCannotActAreGreyed(t *testing.T) {
	win, _, publish := windowStage(t)
	greyed := func(id string) bool {
		for m := range menus {
			for i, it := range menus[m].items {
				if it.id == id {
					return win.bar.Menus[m].Items[i].Disabled
				}
			}
		}
		t.Fatalf("no menu line runs %s", id)
		return false
	}
	publish(app.State{Panes: []app.Pane{{ID: "p1", Title: "here", Kind: app.KindFileManager}}, Stage: &app.Box{Pane: "p1"}, Focus: "p1"})
	if !greyed("conn.disconnect") || greyed("pane.close") || !greyed("pane.scrollback") {
		t.Fatalf("on a file manager pane here: Disconnect greyed %v, Close Pane %v, Find in Scrollback %v",
			greyed("conn.disconnect"), greyed("pane.close"), greyed("pane.scrollback"))
	}
	publish(app.State{Panes: []app.Pane{{ID: "p1", Title: "there", Kind: app.KindFileManager, Machine: "srv"}}, Stage: &app.Box{Pane: "p1"}, Focus: "p1"})
	if greyed("conn.disconnect") || greyed("conn.log") {
		t.Fatal("on a pane on a server, Disconnect or its log is greyed")
	}
}

// The lines always in the Servers menu keep their letters, whatever the
// saved servers are called.
func TestTheServersMenusOwnLinesKeepTheirLetters(t *testing.T) {
	m := withAccessKeys(widget.BarMenu{Title: "Servers",
		Items: widget.Labels("quark", "alpha", "rho", "Quick Connect…", "Add Server…", "Reload Server List")}, 3, 4, 5)
	for i, want := range map[int]rune{3: 'q', 4: 'a', 5: 'r'} {
		if k, _ := accessKeyOf(m.Items[i].Label); k != want {
			t.Errorf("%q has access key %q, want %q", shownText(m.Items[i].Label), k, want)
		}
	}
}

// The keyboard coming into a file manager pane, as by a click in its
// list, makes it the pane in front, as a click in a terminal does: the
// menus act on it.
func TestTheKeyboardComingIntoAFileManagerPaneFrontsIt(t *testing.T) {
	win, _, publish := windowStage(t)
	filemanager.RegisterViews(lastWindow)
	fw := filePane(t, "p2")
	publish(app.State{Panes: []app.Pane{{ID: "p1", Title: "Help", Kind: app.KindHelp}, {ID: "p2", Title: "b", Kind: app.KindFileManager}},
		Stage: &app.Box{ID: "s1", A: &app.Box{Pane: "p1"}, B: &app.Box{Pane: "p2"}, Share: 0.5}, Focus: "p1"})
	fw.Attach(lastWindow.Client(), app.FilePaneHost("p2"))
	var listing gunim.Node
	framesUntil(t, "the file manager shows in its place", func() bool {
		listing = filemanager.FocusIn(lastUI, app.FilePaneViews("p2"))
		return listing != nil
	})
	// The keyboard stays in the help beside it, where the window put it,
	// as the file manager comes.
	if n := win.focusNode("p1", lastUI); n == nil || lastUI.Focused() != n {
		t.Fatalf("the file manager coming took the keyboard from the help pane: %T has it", lastUI.Focused())
	}
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	lastUI.Focus(listing)
	for {
		if in, ok := nextIntent(t).(app.FocusPane); ok {
			if in.Pane != "p2" {
				t.Fatalf("the keyboard coming into p2 put %q in front", in.Pane)
			}
			return
		}
	}
}

// The keyboard coming back to a pane as a dialog closes does not put it
// in front again, when the program put another pane there meanwhile.
func TestADialogClosingLeavesThePaneInFront(t *testing.T) {
	win, _, publish := windowStage(t)
	st := app.State{Panes: []app.Pane{{ID: "p1", Title: "Help", Kind: app.KindHelp}, {ID: "p2", Title: "Jobs", Kind: app.KindJobs}},
		Stage: &app.Box{ID: "s1", A: &app.Box{Pane: "p1"}, B: &app.Box{Pane: "p2"}, Share: 0.5}, Focus: "p1"}
	publish(st)
	lastUI.Focus(win.focusNode("p1", lastUI))
	win.run("pane.rename", lastUI)
	for range 5 {
		lastWindow.Frame(time.Second / 60)
	}
	if win.dialog == nil {
		t.Fatal("Rename Pane opened no dialog")
	}
	st.Focus = "p2"
	publish(st)
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	lastWindow.Input(gi.KeyPress{Key: gi.KeyEscape, Time: time.Now()})
	for range 30 {
		lastWindow.Frame(time.Second / 60)
	}
	for len(lastWindow.Client().Intents()) > 0 {
		if in, ok := (<-lastWindow.Client().Intents()).Intent.(app.FocusPane); ok && in.Pane == "p1" {
			t.Fatal("closing the dialog put p1 in front again")
		}
	}
}
