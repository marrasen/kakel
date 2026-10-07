package view

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/filemanager"

	"github.com/marrasen/kakel/app"
)

// filePane starts a file manager on a folder of its own, as kakel's
// program does for file manager pane id, and stops it as the test ends.
func filePane(t *testing.T, id string) *filemanager.Window {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "dir")
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	fw, err := filemanager.NewHub(ctx, nil).NewPane(
		filemanager.Options{Dir: dir, PrefsPath: filepath.Join(root, "files.json"), Poll: -1},
		filemanager.PaneHost{ID: app.FilePaneViews(id)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		<-fw.Done()
	})
	return fw
}

// frames draws the last window made until cond holds, and fails the test
// after five seconds.
func framesUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("waited five seconds for this: %s", what)
		}
		lastWindow.Frame(time.Second / 60)
		time.Sleep(2 * time.Millisecond)
	}
}

// A file manager pane shows in the place its window gives it, keeps what
// it shows while its tab is not showing, and has the keyboard where the
// window gives it.
func TestAFileManagerPaneShowsInItsPlace(t *testing.T) {
	win, _, publish := windowStage(t)
	filemanager.RegisterViews(lastWindow)
	fw := filePane(t, "p1")
	both := []app.Pane{{ID: "p1", Title: "dir", Kind: app.KindFileManager}, {ID: "p2", Title: "Help", Kind: app.KindHelp}}
	publish(app.State{Panes: both, Stage: &app.Box{Pane: "p1"}, Focus: "p2"})
	fw.Attach(lastWindow.Client(), app.FilePaneHost("p1"))
	var listing gunim.Node
	framesUntil(t, "the file manager shows in its place", func() bool {
		listing = filemanager.FocusIn(lastUI, app.FilePaneViews("p1"))
		return listing != nil
	})
	h := win.fmHosts["p1"]
	if h == nil || lastUI.Presence(h) == gunim.Exiting {
		t.Fatal("the pane has no place on stage")
	}

	// Its tab hidden, it is parked, and keeps its views.
	publish(app.State{Panes: both, Stage: &app.Box{Pane: "p2"}, Focus: "p2"})
	for range 30 {
		lastWindow.Frame(time.Second / 60)
	}
	if filemanager.FocusIn(lastUI, app.FilePaneViews("p1")) != listing {
		t.Fatal("parked, the file manager lost its views")
	}

	// Back on stage with the keyboard, its listing has it.
	publish(app.State{Panes: both, Stage: &app.Box{Pane: "p1"}, Focus: "p1"})
	framesUntil(t, "the listing has the keyboard", func() bool { return lastUI.Focused() == listing })

	// Gone from the window, its place goes too.
	publish(app.State{Panes: both[1:], Stage: &app.Box{Pane: "p2"}, Focus: "p2"})
	for range 60 {
		lastWindow.Frame(time.Second / 60)
	}
	if _, ok := win.fmHosts["p1"]; ok || lastUI.Mounted(app.FilePaneHost("p1")) != nil {
		t.Fatal("the place of a pane that left stayed")
	}
}
