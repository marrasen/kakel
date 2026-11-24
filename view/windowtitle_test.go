package view

import (
	"testing"
	"time"

	"github.com/marrasen/kakel/app"

	"github.com/marrasen/kakel/internal/sessiontest"
	"github.com/marrasen/kakel/screen"

	"github.com/marrasen/kakel/vt"
)

func TestTheWindowIsNamedAfterThePaneInFront(t *testing.T) {
	win, sh, publish := windowStage(t)
	// The program calls itself by its path; the pane, as the program
	// side names it, by the shell's name.
	s := sessiontest.NewPrinted([]byte("\x1b]2;C:\\WINDOWS\\System32\\WindowsPowerShell\\v1.0\\powershell.exe\x07"))
	quiet := screen.Hooks{Output: func() {}, Title: func(string) {}, Exit: func() {}, Clipboard: func(string) {}}
	sh.Set("p1", screen.Open(s, vt.DefaultPalette(), quiet))
	t.Cleanup(func() { _ = sh.Get("p1").T.Close() })
	deadline := time.Now().Add(5 * time.Second)
	for sh.Get("p1").T.Title() == "" {
		if time.Now().After(deadline) {
			t.Fatal("the program's title never arrived")
		}
		time.Sleep(10 * time.Millisecond)
	}
	panes := []app.Pane{{ID: "p1", Title: "Windows PowerShell"}, {ID: "p2", Title: "files", Kind: app.KindFileManager}}
	publish(app.State{Panes: panes, Layout: boxLayout(&app.Box{Pane: "p1"}), Focus: "p1"})
	if got := lastWindow.Offscreen().Title(); got != "kakel — Windows PowerShell" {
		t.Fatalf("the window is called %q", got)
	}
	if win.bar.Title != "kakel" || win.bar.Subtitle != "Windows PowerShell" {
		t.Fatalf("the title bar says %q and %q", win.bar.Title, win.bar.Subtitle)
	}
	publish(app.State{Panes: panes, Layout: boxLayout(&app.Box{Pane: "p2"}), Focus: "p2"})
	if got := lastWindow.Offscreen().Title(); got != "kakel — files" {
		t.Fatalf("on a file manager pane, the window is called %q", got)
	}
}
