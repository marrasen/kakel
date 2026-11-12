package settings

import (
	"os"
	"reflect"
	"testing"
)

// What layouts opened on other windows comes back from the file as it
// went in; a field a later kakel adds to it does not lock the file out;
// and what was written for a layout no longer in the file goes.
func TestLayoutPanesComeBackFromTheFile(t *testing.T) {
	path := at(t)
	body := `{"version": 1, "layouts": [{"id": "a", "name": "A", "root": {"id": "p1", "kind": "item"}, "panes": {"p1": {"kind": "terminal"}}}],` +
		`"layoutPanes": [{"layout": "a", "slot": "p1", "from": "laptop", "window": "desk", "run": "R1", "pane": "p7", "farKind": "Terminal", "kind": "terminal", "later": 1},` +
		`{"layout": "gone", "slot": "p1", "from": "laptop", "window": "desk", "run": "R1", "pane": "p8", "kind": "terminal"}]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.LayoutPanes(); len(got) != 2 || got[0].Pane != "p7" {
		t.Fatalf("read %+v", got)
	}
	add := LayoutPane{Layout: "a", Slot: "p2", From: "laptop", Window: "desk", Run: "R1", Pane: "p9", Key: "srv", FarKind: "Terminal", Kind: LayoutCommand, Line: "top"}
	if err := s.EditLayoutPanes(func(have []LayoutPane) []LayoutPane { return append(have, add) }); err != nil {
		t.Fatal(err)
	}
	again, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []LayoutPane{{Layout: "a", Slot: "p1", From: "laptop", Window: "desk", Run: "R1", Pane: "p7", FarKind: "Terminal", Kind: LayoutTerminal}, add}
	if got := again.LayoutPanes(); !reflect.DeepEqual(got, want) {
		t.Fatalf("read back %+v, want %+v", got, want)
	}
}

// What layouts opened elsewhere is a convenience: a record that cannot
// be read is dropped, and a list that is no list is read as none,
// rather than the settings file being turned away.
func TestLayoutPanesThatCannotBeReadAreDropped(t *testing.T) {
	for name, panes := range map[string]string{
		"no list":        `{}`,
		"a bad record":   `[{"layout": "a", "slot": "p1", "pane": 7}, {"layout": "a", "slot": "p2", "from": "x", "pane": "p9", "kind": "terminal"}]`,
		"a list of text": `["what"]`,
	} {
		t.Run(name, func(t *testing.T) {
			path := at(t)
			body := `{"version": 1, "theme": "Phosphor", "layouts": [{"id": "a", "name": "A", "root": {"id": "p1", "kind": "item"}, "panes": {"p1": {"kind": "terminal"}}}], "layoutPanes": ` + panes + `}`
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			s, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			got := s.LayoutPanes()
			if name == "a bad record" && (len(got) != 1 || got[0].Pane != "p9") || name != "a bad record" && len(got) != 0 {
				t.Fatalf("read %+v", got)
			}
			if err := s.PutTheme("Dark"); err != nil {
				t.Fatalf("the settings no longer save: %v", err)
			}
		})
	}
}
