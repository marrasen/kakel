package view

import (
	"slices"
	"testing"

	"github.com/marrasen/kakel/app"

	"github.com/marrasen/kakel/machines"

	"github.com/marrasen/gunim/widget"
)

func TestTheRemoveQuestionSaysWhatItCloses(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(app.State{Panes: []app.Pane{{ID: "p1", Title: "Terminal 1", Machine: "srv"}}, Connected: []machines.ID{"srv"}, Dialing: []machines.ID{"far"}})
	if got := win.removeSays("srv"); got != "srv is connected. Removing it closes the connection and everything through it: 1 pane." {
		t.Fatalf("for a connected server it says %q", got)
	}
	if got := win.removeSays("far"); got != "Removing it cancels the connection in progress." {
		t.Fatalf("for a server being connected to it says %q", got)
	}
	publish(app.State{Panes: []app.Pane{{ID: "p1", Title: "Terminal 1", Machine: "srv", Ended: true}}, Dropped: []machines.ID{"srv"}})
	if got := win.removeSays("srv"); got != "Its connection was lost. Removing it closes its 1 ended pane." {
		t.Fatalf("for a dropped server it says %q", got)
	}
	publish(app.State{Panes: []app.Pane{{ID: "p1", Title: "Terminal 1", Machine: "srv", Ended: true}}, Dropped: []machines.ID{"srv"}, Dialing: []machines.ID{"srv"}})
	if got := win.removeSays("srv"); got != "Its connection was lost. Removing it cancels the reconnect in progress and closes its 1 ended pane." {
		t.Fatalf("for a dropped server reconnecting it says %q", got)
	}
	if got := win.removeSays("idle"); got != "" {
		t.Fatalf("for a server holding nothing it says %q", got)
	}
}

func TestSavedServersAreListedWithAWayToConnect(t *testing.T) {
	rows := sidebarRows(nil, nil, app.Share{}, nil, []machines.ID{"desk"}, nil)
	if !slices.ContainsFunc(rows, func(r sideItem) bool { return r.key == "machine:desk" && r.heading }) {
		t.Fatalf("a saved server has no heading: %+v", rows)
	}
	// One not saved is connected to from the Machines menu.
	win, _, publish := windowStage(t)
	publish(app.State{})
	i := win.menuAt("Machines")
	if i < 0 || !slices.ContainsFunc(win.bar.Menus[i].Items, func(it widget.MenuItem) bool { return shownText(it.Label) == "Quick Connect…" }) {
		t.Fatal("the Machines menu has no Quick Connect")
	}
}
