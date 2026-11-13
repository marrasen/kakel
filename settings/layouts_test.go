package settings

import (
	"errors"
	"os"
	"reflect"
	"slices"
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
	kept, _, err := s.SaveLayout(aLayout("  Work, mostly "), false)
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
	if _, _, err := s.SaveLayout(aLayout("Work"), false); err != nil {
		t.Fatal(err)
	}
	other, _, err := s.SaveLayout(aLayout("Home"), false)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SaveLayout(aLayout("work"), false); !errors.Is(err, ErrLayoutNameUsed) {
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
		if _, _, err := s.SaveLayout(aLayout(name), false); err != nil {
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
	again, _, err := s.SaveLayout(aLayout("Two"), false)
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
	if _, _, err := s.SaveLayout(aLayout("Work"), false); err != nil {
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

// A layout's panes through another kakel window come back from the file
// as they went in: the window by name and id, or by its address alone,
// and the machine beyond it. A layout written before those were kept,
// with none of them, loads as it was, its panes on no window.
func TestPanesThroughAWindowComeBackFromTheFile(t *testing.T) {
	path := at(t)
	old := `{"version": 1, "layouts": [{"id": "old", "name": "Old", "root": {"id": "p1", "kind": "item"},` +
		`"panes": {"p1": {"kind": "terminal", "host": "web", "hostId": "h-web", "profile": "login", "folder": "/srv"}}}]}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	want := SavedPane{Kind: LayoutTerminal, Host: "web", HostID: "h-web", Profile: "login", Folder: "/srv"}
	if got := s.Layouts(); len(got) != 1 || !reflect.DeepEqual(got[0].Panes["p1"], want) {
		t.Fatalf("the old layout reads %+v", got)
	}

	l := aLayout("Through")
	l.Panes["p1"] = SavedPane{Kind: LayoutTerminal, Host: "desk", HostID: "h-desk", Window: true, Folder: "/home/me"}
	l.Panes["p2"] = SavedPane{Kind: LayoutCommand, Host: "desk", HostID: "h-desk", Window: true, On: "db", OnKey: "k-db", Line: "top", Title: "db top"}
	l.Panes["p3"] = SavedPane{Kind: LayoutFiles, Host: "laptop.example:7022", Window: true, Folder: "/tmp"}
	kept, _, err := s.SaveLayout(l, false)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got := again.Layouts()
	if len(got) != 2 || !reflect.DeepEqual(got[1], kept) || !reflect.DeepEqual(got[1].Panes, l.Panes) {
		t.Fatalf("read back %+v, want %+v", got, kept)
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
	kept, _, err := theirs.SaveLayout(aLayout("Work"), false)
	if err != nil {
		t.Fatal(err)
	}
	if err := theirs.PutLayoutAtStart(kept.ID, true); err != nil {
		t.Fatal(err)
	}

	if _, _, err := mine.SaveLayout(aLayout("work"), false); !errors.Is(err, ErrLayoutNameUsed) {
		t.Fatalf("saving over it unasked gave %v", err)
	}
	now := aLayout("  work  ")
	now.Panes = map[string]SavedPane{"p1": {Kind: LayoutFiles}}
	got, _, err := mine.SaveLayout(now, true)
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
	if again, _, err := mine.SaveLayout(aLayout("Work"), true); err != nil || again.AtStart || again.ID != "work" {
		t.Fatalf("saved again after the removal: %+v, %v", again, err)
	}
}

// A layout put back after Remove goes where it stood, the place clamped
// to the list as it is now, with its ID; given a new ID where another
// layout took its own since, and refused where another took its name.
// Put back twice, it is there once.
func TestALayoutIsPutBack(t *testing.T) {
	s, err := Load(at(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"One", "Two", "Three"} {
		if _, _, err := s.SaveLayout(aLayout(name), false); err != nil {
			t.Fatal(err)
		}
	}
	two := s.Layouts()[1]
	if err := s.DropLayout(two.ID); err != nil {
		t.Fatal(err)
	}
	back, err := s.PutLayoutBack(two, 1)
	if err != nil || back.ID != two.ID {
		t.Fatal(back, err)
	}
	if _, err := s.PutLayoutBack(two, 1); err != nil {
		t.Fatalf("put back twice: %v", err)
	}
	if names := layoutNames(s); !slices.Equal(names, []string{"One", "Two", "Three"}) || s.Layouts()[1].ID != two.ID {
		t.Fatalf("put back, the layouts are %q", names)
	}

	if err := s.DropLayout(two.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SaveLayout(aLayout("two"), false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PutLayoutBack(two, 1); !errors.Is(err, ErrLayoutNameUsed) {
		t.Fatalf("put back over a name taken: %v", err)
	}

	if err := s.DropLayout(s.Layouts()[2].ID); err != nil {
		t.Fatal(err)
	}
	gone := two
	gone.Name, gone.ID = "Five", s.Layouts()[0].ID
	back, err = s.PutLayoutBack(gone, 99)
	if err != nil || back.ID == gone.ID || s.Layouts()[len(s.Layouts())-1].Name != "Five" {
		t.Fatalf("put back with its ID taken: %+v, %v; the layouts are %q", back, err, layoutNames(s))
	}
}

// layoutNames are the names of the layouts s holds, in order.
func layoutNames(s *Settings) []string {
	var out []string
	for _, l := range s.Layouts() {
		out = append(out, l.Name)
	}
	return out
}

// A terminal's start lines come back from the file as they went in, but
// for blank lines, which are dropped, and a line holding a line break,
// which is the lines it holds. A pane other than a terminal keeps none.
// A copy handed out is the caller's own.
func TestStartLinesComeBackFromTheFile(t *testing.T) {
	path := at(t)
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	l := aLayout("Work")
	p1, p2 := l.Panes["p1"], l.Panes["p2"]
	p1.Start = []string{"  ", " cd /srv", "tmux attach\r\nhtop", ""}
	p2.Start = []string{"make"}
	l.Panes["p1"], l.Panes["p2"] = p1, p2
	kept, _, err := s.SaveLayout(l, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{" cd /srv", "tmux attach", "htop"}
	if got := kept.Panes["p1"].Start; !slices.Equal(got, want) {
		t.Fatalf("kept the lines %q, want %q", got, want)
	}
	if got := kept.Panes["p2"].Start; got != nil {
		t.Fatalf("a command pane kept the lines %q", got)
	}

	again, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got := again.Layouts()
	if len(got) != 1 || !reflect.DeepEqual(got[0], kept) {
		t.Fatalf("read back %+v, want %+v", got, kept)
	}
	got[0].Panes["p1"].Start[0] = "changed"
	if line := again.Layouts()[0].Panes["p1"].Start[0]; line != " cd /srv" {
		t.Fatalf("a change to the copy reached the settings: %q", line)
	}
}

// Start lines written into the file by hand are read as a layout keeps
// them.
func TestStartLinesWrittenByHandAreTidied(t *testing.T) {
	path := at(t)
	body := `{"version": 1, "layouts": [{"id": "a", "name": "A", "root": {"id": "#1", "kind": "group", "front": "p1", "kids": [` +
		`{"id": "p1", "kind": "item"}, {"id": "p2", "kind": "item"}]}, "panes": {` +
		`"p1": {"kind": "terminal", "start": ["", "vite dev", "   "]}, "p2": {"kind": "files", "start": ["ls"]}}}]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got := s.Layouts()
	if len(got) != 1 || !slices.Equal(got[0].Panes["p1"].Start, []string{"vite dev"}) || got[0].Panes["p2"].Start != nil {
		t.Fatalf("the layout reads %+v", got)
	}
}
