package view

import (
	"slices"
	"testing"

	"github.com/marrasen/kakel/app"

	"github.com/marrasen/gunim/widget"
)

// The palette finds commands by the words people use for them, and has
// the ones the menus alone had.
func TestThePaletteFindsCommandsByOtherWords(t *testing.T) {
	win, _, _ := windowStage(t)
	find := func(title string) widget.PaletteItem {
		t.Helper()
		i := slices.IndexFunc(win.palette.Items, func(it widget.PaletteItem) bool { return it.Title == title })
		if i < 0 {
			t.Fatalf("the palette has no %q", title)
		}
		return win.palette.Items[i]
	}
	if it := find("Exit"); !slices.Contains(it.Also, "quit") {
		t.Fatalf("Exit is found by %v", it.Also)
	}
	if it := find("Larger Font"); !slices.Contains(it.Also, "zoom in") {
		t.Fatalf("Larger Font is found by %v", it.Also)
	}
	find("Scroll Page Up")
	find("Open the Menus")
}

// The palette starts with what opens in this window, said alike: a
// terminal, then a file manager, both on the machine of the pane in
// front. A file manager in a window of its own comes later.
func TestThePaletteStartsWithANewTerminalAndANewFileManager(t *testing.T) {
	win, _, _ := windowStage(t)
	var titles []string
	for _, it := range win.palette.Items[:2] {
		titles = append(titles, it.Title)
	}
	if want := []string{"New Terminal", "New File Manager"}; !slices.Equal(titles, want) {
		t.Fatalf("the palette starts with %q, want %q", titles, want)
	}
	if !slices.ContainsFunc(win.palette.Items, func(it widget.PaletteItem) bool { return it.Title == "New File Manager Window" }) {
		t.Fatal("the palette has no New File Manager Window")
	}
}

// Next Pane goes in the sidebar's order, the machine's panes together,
// not in the order the panes were opened.
func TestNextPaneGoesInTheSidebarsOrder(t *testing.T) {
	win, _, publish := windowStage(t)
	panes := []app.Pane{{ID: "p1", Title: "one", Kind: app.KindFileManager}, {ID: "p2", Title: "two", Kind: app.KindFileManager, Machine: "srv"}, {ID: "p3", Title: "three", Kind: app.KindFileManager}}
	publish(app.State{Panes: panes, Stage: &app.Box{Pane: "p1"}, Focus: "p1"})
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	win.run("pane.nextInSidebar", lastUI)
	var got app.FocusPane
	for got.Pane == "" {
		if in, ok := nextIntent(t).(app.FocusPane); ok {
			got = in
		}
	}
	if got.Pane != "p3" {
		t.Fatalf("Next Pane from one went to %s, want three, the next on this computer", got.Pane)
	}
}
