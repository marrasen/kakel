package view

import (
	"testing"
	"time"

	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/look"
	"github.com/marrasen/kakel/themes"
)

// The text on stage holds its colour as updates come, once the themes
// were read again: each update carries them, and the same theme worn
// again dimmed every foreground for a moment, a flicker in Settings.
func TestSettingsTextHoldsItsColourAcrossUpdates(t *testing.T) {
	win, _, publish := windowStage(t)
	var looks []look.Themed
	st := app.State{Panes: []app.Pane{{ID: "p1", Title: "Settings", Kind: app.KindSettings}}, Stage: &app.Box{Pane: "p1"}, Focus: "p1", Theme: "Dark"}
	for _, th := range themes.Built() {
		l, err := look.Of(th)
		if err != nil {
			t.Fatal(err)
		}
		looks = append(looks, l)
	}
	st.Looks = looks
	st.Contents = map[string]theme.Theme{}
	for _, l := range looks {
		st.Contents[l.Name] = l.Content
	}
	publish(st)
	for range 60 {
		lastWindow.Frame(time.Second / 60)
	}
	want := widget.Ink.Get(win.onStage.ThemeScope())
	for i := range 20 {
		publish(st)
		for f := range 10 {
			lastWindow.Frame(time.Second / 60)
			if got := widget.Ink.Get(win.onStage.ThemeScope()); got != want {
				t.Fatalf("update %d frame %d: ink %v, want %v", i, f, got, want)
			}
		}
	}
}
