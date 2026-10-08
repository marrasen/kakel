package view

import (
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gunim/themeedit"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/look"
	"github.com/marrasen/kakel/themes"
)

// The theme editor opens in a tab, puts the cursor first, and keeps a
// cursor made to jump in the theme the window is drawn in.
func TestTheThemeEditorKeepsACursorThatJumps(t *testing.T) {
	win, _, publish := windowStage(t)
	var looks []look.Themed
	for _, th := range themes.Built() {
		l, err := look.Of(th)
		if err != nil {
			t.Fatal(err)
		}
		looks = append(looks, l)
	}
	publish(app.State{Panes: []app.Pane{{ID: "p1", Title: "Theme", Kind: app.KindThemeEditor}}, Stage: &app.Box{Pane: "p1"}, Focus: "p1",
		Theme: "Dark", Looks: looks})
	for range 3 {
		lastWindow.Frame(time.Second / 60)
	}
	if win.themeEditor == nil {
		t.Fatal("the theme editor's pane was not made")
	}
	if chosen := win.themeEditor.ed.Chosen(); len(chosen) == 0 || chosen[0] != widget.Caret.Key() {
		t.Fatalf("the editor puts %v first, want the cursor", chosen)
	}
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	if err := win.themeEditor.ed.Set(widget.Caret.Key(), themeedit.Instant, true, lastUI); err != nil {
		t.Fatal(err)
	}
	for {
		if in, ok := nextIntent(t).(app.SaveThemeEdits); ok {
			if in.Theme != "Dark" || !strings.Contains(string(in.Edits), `"motion.caret"`) {
				t.Fatalf("the editor saved %s for %q", in.Edits, in.Theme)
			}
			return
		}
	}
}
