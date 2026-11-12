package settings

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

// aLayout is a window of a split, a terminal on a server beside two tabs
// on this computer, named name.
func aLayout(name string) SavedLayout {
	return SavedLayout{
		Name: name,
		Root: &LayoutNode{ID: "#1", Kind: "row", Share: 0.3, Kids: []*LayoutNode{
			{ID: "#2", Kind: "group", Front: "p1", Kids: []*LayoutNode{{ID: "p1", Kind: "item"}}},
			{ID: "#3", Kind: "group", Front: "p3", Kids: []*LayoutNode{{ID: "p2", Kind: "item"}, {ID: "p3", Kind: "item"}}},
		}},
		Panes: map[string]SavedPane{
			"p1": {Kind: LayoutTerminal, Host: "web", HostID: "h-web", Profile: "profile:tmux", Folder: "/srv"},
			"p2": {Kind: LayoutCommand, Line: "make watch", Folder: "/src", Title: "build"},
			"p3": {Kind: LayoutTerminal, Command: []string{"/bin/zsh", "-l"}},
		},
		Place: &WindowPlace{X: 10, Y: 20, W: 800, H: 600},
	}
}

// A layout saved comes back from the file as it went in, with an ID made
// from its name.
func TestALayoutComesBackFromTheFile(t *testing.T) {
	path := at(t)
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	kept, err := s.SaveLayout(aLayout("  Work, mostly "), false)
	if err != nil {
		t.Fatal(err)
	}
	if kept.ID != "work-mostly" || kept.Name != "Work, mostly" {
		t.Fatalf("kept as %q called %q", kept.ID, kept.Name)
	}

	again, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got := again.Layouts()
	if len(got) != 1 || !reflect.DeepEqual(got[0], kept) {
		t.Fatalf("read back %+v, want %+v", got, kept)
	}
}

// A settings file from before there were layouts loads, with none.
func TestSettingsWithoutLayoutsLoad(t *testing.T) {
	path := at(t)
	if err := os.WriteFile(path, []byte(`{"version": 1, "copies": [{"at": "/a", "into": "/b", "names": ["x"]}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Layouts(); len(got) != 0 {
		t.Fatalf("the layouts are %+v", got)
	}
}

// A name another layout has is refused in a way the caller can tell
// apart, whatever its case, whether added or renamed to.
func TestALayoutNameUsedIsRefused(t *testing.T) {
	s, err := Load(at(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveLayout(aLayout("Work"), false); err != nil {
		t.Fatal(err)
	}
	other, err := s.SaveLayout(aLayout("Home"), false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveLayout(aLayout("work"), false); !errors.Is(err, ErrLayoutNameUsed) {
		t.Fatalf("adding a second Work gave %v", err)
	}
	if err := s.RenameLayout(other.ID, "WORK"); !errors.Is(err, ErrLayoutNameUsed) {
		t.Fatalf("renaming Home to WORK gave %v", err)
	}
	if got := s.Layouts(); len(got) != 2 || got[1].Name != "Home" {
		t.Fatalf("after the refusals the layouts are %+v", got)
	}
}

// A layout replaced keeps its ID and its place in the list, and another
// given the same first name later gets an ID of its own.
func TestAReplacedLayoutKeepsItsIDAndPlace(t *testing.T) {
	s, err := Load(at(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"One", "Two", "Three"} {
		if _, err := s.SaveLayout(aLayout(name), false); err != nil {
			t.Fatal(err)
		}
	}
	two := s.Layouts()[1]
	now := aLayout("Second")
	now.ID = two.ID
	now.Panes = map[string]SavedPane{"p1": {Kind: LayoutFiles, Folder: "/home"}}
	now.Root = &LayoutNode{ID: "#1", Kind: "group", Front: "p1", Kids: []*LayoutNode{{ID: "p1", Kind: "item"}}}
	if err := s.ReplaceLayout(now); err != nil {
		t.Fatal(err)
	}
	if err := s.PutLayoutAtStart(two.ID, true); err != nil {
		t.Fatal(err)
	}
	got := s.Layouts()
	if len(got) != 3 || got[1].ID != "two" || got[1].Name != "Second" || !got[1].AtStart || got[1].Panes["p1"].Kind != LayoutFiles {
		t.Fatalf("after the replace the layouts are %+v", got)
	}
	again, err := s.SaveLayout(aLayout("Two"), false)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != "two-2" {
		t.Fatalf("a new Two is %q", again.ID)
	}
	if err := s.DropLayout("two"); err != nil {
		t.Fatal(err)
	}
	if err := s.RenameLayout("two", "Gone"); !errors.Is(err, ErrNoLayout) {
		t.Fatalf("renaming one removed gave %v", err)
	}
}

// Layouts handed out are copies: changing one leaves the settings as
// they are.
func TestLayoutsGivesBackACopy(t *testing.T) {
	s, err := Load(at(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveLayout(aLayout("Work"), false); err != nil {
		t.Fatal(err)
	}
	got := s.Layouts()
	got[0].Root.Kids[0].ID = "changed"
	got[0].Panes["p1"] = SavedPane{}
	if again := s.Layouts()[0]; again.Root.Kids[0].ID != "#2" || again.Panes["p1"].Host != "web" {
		t.Fatalf("a change to the copy reached the settings: %+v", again)
	}
}

// A file holding a layout with no panes, or two of one name, is refused:
// neither could be opened as named.
func TestALayoutThatCannotOpenIsRefused(t *testing.T) {
	for want, body := range map[string]string{
		`layout 1, "A", has no panes`: `{"version": 1, "layouts": [{"id": "a", "name": "A", "root": null, "panes": {}}]}`,
		`layout 2, "a", has the name of another`: `{"version": 1, "layouts": [` +
			`{"id": "a", "name": "A", "root": {"id": "p1", "kind": "item"}, "panes": {"p1": {"kind": "terminal"}}},` +
			`{"id": "b", "name": "a", "root": {"id": "p1", "kind": "item"}, "panes": {"p1": {"kind": "terminal"}}}]}`,
	} {
		path := at(t)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("loading gave %v, want %q", err, want)
		}
	}
}

// A layout or a pane holding a field a later kakel added loads: what
// this kakel knows of it opens, and the rest of the file is read.
func TestALayoutFromALaterKakelLoads(t *testing.T) {
	path := at(t)
	body := `{"version": 1, "theme": "Phosphor", "layouts": [{"id": "a", "name": "A", "colour": "red",` +
		`"root": {"id": "p1", "kind": "item", "pinned": true}, "panes": {"p1": {"kind": "terminal", "folder": "/src", "zoom": 2}}}]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Layouts(); len(got) != 1 || got[0].Panes["p1"].Folder != "/src" {
		t.Fatalf("the layouts are %+v", got)
	}
	if theme, _ := s.Theme(); theme != "Phosphor" {
		t.Fatalf("the theme is %q", theme)
	}
	if err := s.PutTheme("Dark"); err != nil {
		t.Fatalf("the settings no longer save: %v", err)
	}
}

// Saving over a name is decided on the file as it is, not as this
// kakel last read it: a layout another kakel saved since is replaced,
// keeping its ID and whether it opens at start, and the layout handed
// back is the one the file holds.
func TestALayoutIsSavedOverWhatAnotherKakelSaved(t *testing.T) {
	path := at(t)
	mine, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	kept, err := theirs.SaveLayout(aLayout("Work"), false)
	if err != nil {
		t.Fatal(err)
	}
	if err := theirs.PutLayoutAtStart(kept.ID, true); err != nil {
		t.Fatal(err)
	}

	if _, err := mine.SaveLayout(aLayout("work"), false); !errors.Is(err, ErrLayoutNameUsed) {
		t.Fatalf("saving over it unasked gave %v", err)
	}
	now := aLayout("  work  ")
	now.Panes = map[string]SavedPane{"p1": {Kind: LayoutFiles}}
	got, err := mine.SaveLayout(now, true)
	if err != nil {
		t.Fatal(err)
	}

	read, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	file := read.Layouts()
	if len(file) != 1 || !reflect.DeepEqual(file[0], got) {
		t.Fatalf("handed back %+v, the file holds %+v", got, file)
	}
	if got.ID != kept.ID || got.Name != "work" || !got.AtStart || got.Panes["p1"].Kind != LayoutFiles {
		t.Fatalf("saved over, it is %+v", got)
	}

	// Removed by the other since: saved anew, rather than refused.
	if err := theirs.DropLayout(kept.ID); err != nil {
		t.Fatal(err)
	}
	if again, err := mine.SaveLayout(aLayout("Work"), true); err != nil || again.AtStart || again.ID != "work" {
		t.Fatalf("saved again after the removal: %+v, %v", again, err)
	}
}
