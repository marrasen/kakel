package settings

import (
	"encoding/json"
	"slices"
)

// LayoutPane is a pane a layout opened through another kakel window,
// written down to watch it again the next time the layout opens, while
// it is still open there, rather than open another beside it. It is
// kept apart from the layout, which is a recipe: opening a layout never
// changes what was saved.
//
// It is kept per layout, per slot and per computer: a settings file a
// roaming profile shares between computers holds each one's own, as a
// pane one computer watches is no other's to take over.
type LayoutPane struct {
	// Layout and Slot are the layout's ID and the slot's name in it,
	// "p1" and on.
	Layout string `json:"layout"`
	Slot   string `json:"slot"`
	// From is the computer that opened it, by its host name.
	From string `json:"from"`

	// Window is the kakel window it was opened on: a saved window's id,
	// or the address of one connected to by address alone.
	Window string `json:"window"`
	// Run is the token that window gave for the run it was opened in: a
	// window started again since gives another, and its panes' ids are
	// another run's.
	Run string `json:"run"`
	// Pane is what the window calls the pane, and Key the window's key
	// for the machine it runs on, empty for its own; FarKind is what the
	// window said it is, as "Terminal".
	Pane    string `json:"pane"`
	Key     string `json:"key,omitempty"`
	FarKind string `json:"farKind,omitempty"`

	// Kind and Line are what the slot held as the pane opened: a
	// terminal, or the command line it ran. A slot that holds something
	// else since is not given the pane again.
	Kind string `json:"kind"`
	Line string `json:"line,omitempty"`
}

// layoutPaneList is the records as the file holds them, read as the
// layouts are: a field a later kakel adds must not lock this one out of
// the settings file.
type layoutPaneList []LayoutPane

// UnmarshalJSON reads the records, passing over fields it does not know.
func (l *layoutPaneList) UnmarshalJSON(raw []byte) error {
	var out []LayoutPane
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	*l = out
	return nil
}

// LayoutPanes are the panes layouts opened through other windows, as
// the file last read holds them.
func (s *Settings) LayoutPanes() []LayoutPane {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.have.LayoutPanes)
}

// EditLayoutPanes changes the records as edit says, on the list as the
// file holds it now, and saves. Records of a layout no longer in the
// file go, as one another kakel removed.
func (s *Settings) EditLayoutPanes(edit func([]LayoutPane) []LayoutPane) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.rereadLocked(); err != nil {
		return err
	}
	panes := edit(slices.Clone(s.have.LayoutPanes))
	panes = slices.DeleteFunc(panes, func(p LayoutPane) bool {
		return !slices.ContainsFunc(s.have.Layouts, func(l SavedLayout) bool { return l.ID == p.Layout })
	})
	if slices.Equal(panes, s.have.LayoutPanes) {
		return nil
	}
	before := s.have
	s.have.LayoutPanes = panes
	if err := s.saveLocked(); err != nil {
		s.have = before
		return err
	}
	return nil
}
