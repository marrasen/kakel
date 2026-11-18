package app

import (
	"testing"

	"github.com/marrasen/gunim/widget"
	"github.com/marrasen/kakel/internal/testhome"
)

// Word stops saved in the settings go over every theme, and Reset takes
// them off again.
func TestWordStopsGoOverEveryTheme(t *testing.T) {
	testhome.New(t)
	a := fontApp(t)
	a.handle(SetWordStops{Stops: "/"})
	if a.st.WordStops != "/" {
		t.Fatalf("the stops shown are %q, want /", a.st.WordStops)
	}
	if got, ok := a.settings.WordStops(); !ok || got != "/" {
		t.Fatalf("the stops saved are %q, %v, want /", got, ok)
	}
	if len(a.themes) == 0 {
		t.Fatal("no themes")
	}
	for _, th := range a.themes {
		if !th.Theme.Has(widget.WordStops.Key()) || !th.Content.Has(widget.WordStops.Key()) {
			t.Fatalf("the theme %q has no word stops", th.Name)
		}
	}
	a.handle(SetWordStops{Reset: true})
	if a.st.WordStops != widget.WordStops.Default() {
		t.Fatalf("after Reset the stops are %q, want gunim's", a.st.WordStops)
	}
	if _, ok := a.settings.WordStops(); ok {
		t.Fatal("after Reset the settings still keep stops")
	}
	for _, th := range a.themes {
		if th.Theme.Has(widget.WordStops.Key()) {
			t.Fatalf("after Reset the theme %q still sets word stops", th.Name)
		}
	}
}
