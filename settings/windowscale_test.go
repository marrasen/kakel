package settings

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/marrasen/kakel/internal/jsoncheck"
)

// oldWindowPlace is WindowPlace as a kakel before the scale had it,
// which read it strictly.
type oldWindowPlace struct {
	X         float32 `json:"x"`
	Y         float32 `json:"y"`
	W         float32 `json:"width"`
	H         float32 `json:"height"`
	Maximized bool    `json:"maximized,omitempty"`
}

// oldFile is the part of the file a kakel before the scale read the
// window places into: what it knows, strictly, the rest kept aside.
type oldFile struct {
	Version     int             `json:"version"`
	Window      *oldWindowPlace `json:"window,omitempty"`
	FilesWindow *oldWindowPlace `json:"filesWindow,omitempty"`
}

// loadOld reads the file at path as a kakel before the scale did.
func loadOld(t *testing.T, path string) oldFile {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var file oldFile
	known, _, err := jsoncheck.SplitUnknown(raw, &file)
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(known))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&file); err != nil {
		t.Fatalf("an older kakel cannot read the file: %v", err)
	}
	return file
}

// A window's place saved with its scale leaves the file one an older
// kakel reads, and the scale comes back, for the window, a files window
// and a layout.
func TestAScaleSavedLeavesTheFileToOlderKakels(t *testing.T) {
	path := at(t)
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	main := WindowPlace{X: 10, Y: 20, W: 800, H: 600, Scale: 1.25}
	files := WindowPlace{X: 30, Y: 40, W: 700, H: 500, Maximized: true, Scale: 2}
	if err := s.PutWindow(main); err != nil {
		t.Fatal(err)
	}
	if err := s.PutFilesWindow(files); err != nil {
		t.Fatal(err)
	}
	place := WindowPlace{X: 5, Y: 6, W: 640, H: 480, Scale: 1.5}
	l := SavedLayout{Name: "Work", Root: &LayoutNode{ID: "p1", Kind: "item"}, Panes: map[string]SavedPane{"p1": {Kind: LayoutTerminal}}, Place: &place}
	if _, _, err := s.SaveLayout(l, false); err != nil {
		t.Fatal(err)
	}

	old := loadOld(t, path)
	if old.Window == nil || *old.Window != (oldWindowPlace{X: 10, Y: 20, W: 800, H: 600}) {
		t.Errorf("an older kakel reads the window at %+v", old.Window)
	}
	if old.FilesWindow == nil || *old.FilesWindow != (oldWindowPlace{X: 30, Y: 40, W: 700, H: 500, Maximized: true}) {
		t.Errorf("an older kakel reads the files window at %+v", old.FilesWindow)
	}

	again, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := again.Window(); !ok || got != main {
		t.Errorf("the window comes back at %+v", got)
	}
	if got, ok := again.FilesWindow(); !ok || got != files {
		t.Errorf("the files window comes back at %+v", got)
	}
	if got := again.Layouts(); len(got) != 1 || got[0].Place == nil || *got[0].Place != place {
		t.Errorf("the layout comes back at %+v", got)
	}
}

// A file from before the scale opens its windows as it did, at no scale
// said.
func TestAPlaceWithNoScaleOpensAsBefore(t *testing.T) {
	path := at(t)
	body := `{"version": 1, "window": {"x": 1, "y": 2, "width": 300, "height": 200, "maximized": true}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := s.Window(); !ok || got != (WindowPlace{X: 1, Y: 2, W: 300, H: 200, Maximized: true}) {
		t.Fatalf("the window opens at %+v", got)
	}
}

// An older kakel moving the window keeps the scale of the place before,
// which then belongs to no place: it is passed over.
func TestAScaleOfAnotherPlaceIsPassedOver(t *testing.T) {
	path := at(t)
	body := `{"version": 1, "window": {"x": 50, "y": 60, "width": 900, "height": 700},` +
		`"windowScale": {"x": 10, "y": 20, "width": 800, "height": 600, "scale": 1.25}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Window(); got.Scale != 0 {
		t.Fatalf("the window took the scale of another place: %+v", got)
	}
}

// A place holding a field a later kakel added loads, by what this one
// knows of it.
func TestAPlaceFromALaterKakelLoads(t *testing.T) {
	path := at(t)
	body := `{"version": 1, "filesWindow": {"x": 1, "y": 2, "width": 300, "height": 200, "monitor": "DP-1"}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatalf("a place with a later field turns the file away: %v", err)
	}
	if got, ok := s.FilesWindow(); !ok || got.W != 300 {
		t.Fatalf("the files window opens at %+v", got)
	}
}
