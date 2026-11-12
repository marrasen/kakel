package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// SavedLayout is one kakel window written down to open again: its
// splits and tabs, and for each pane how to start it again. It is a
// recipe rather than a record of what ran: opening it starts every pane
// afresh, on the machine it names, as that machine is reached then.
type SavedLayout struct {
	// ID names the layout in a command id and at start. It is given
	// once, from the first name, and a rename leaves it as it was, as a
	// profile's is.
	ID   string `json:"id"`
	Name string `json:"name"`

	// Root is the window's tree. Its items are named for the slots in
	// Panes, "p1", "p2" and on, and not for the panes they were saved
	// from, whose names belong to the run that had them.
	Root *LayoutNode `json:"root"`

	// Panes are what goes in each slot, by the slot's name.
	Panes map[string]SavedPane `json:"panes"`

	// Place is where the window stood, and how big, when that was known.
	Place *WindowPlace `json:"place,omitempty"`

	// AtStart opens it as kakel starts.
	AtStart bool `json:"atStart,omitempty"`
}

// LayoutNode is one part of a saved window's tree, as gunim's dock
// writes its own nodes: the same fields under the same names, so one
// reads as the other. The settings keep a copy of the shape rather than
// gunim's type, as this package is also read by parts of kakel that draw
// nothing, and the dock would bring the whole of gunim's window with it.
type LayoutNode struct {
	// ID names an item's slot, or a split's or group's own name.
	ID string `json:"id"`
	// Kind is "item", "row", "column" or "group", as the dock spells it.
	Kind string `json:"kind"`
	// Share is a split's first part's share of the room; 0 is a half.
	Share float32 `json:"share,omitempty"`
	// Front is the ID of a group's tab in front.
	Front string `json:"front,omitempty"`
	// Kids are a split's two parts, or a group's tabs, in order.
	Kids []*LayoutNode `json:"kids,omitempty"`
}

// The kinds of pane a layout opens.
const (
	// LayoutTerminal is a shell.
	LayoutTerminal = "terminal"
	// LayoutCommand is one command, in a pane of its own.
	LayoutCommand = "command"
	// LayoutFiles is a file manager.
	LayoutFiles = "files"
)

// SavedPane is how one pane of a layout is started again.
type SavedPane struct {
	// Kind is LayoutTerminal, LayoutCommand or LayoutFiles.
	Kind string `json:"kind"`

	// Host is the machine it runs on, as the sidebar named it as it was
	// saved, and empty for this computer. HostID is the id of the saved
	// server Host names, and empty for a machine on no list: the id is
	// what it runs on, because a name can be given to another server, as
	// with a saved copy.
	Host   string `json:"host,omitempty"`
	HostID string `json:"hostId,omitempty"`

	// Profile is the id of the profile a terminal runs: one of the
	// user's or one kakel finds, on its machine, or the server's own
	// login shell. Command is, for a terminal on this computer that
	// runs no profile, the program and its arguments.
	Profile string   `json:"profile,omitempty"`
	Command []string `json:"command,omitempty"`

	// Folder is where it starts: a terminal's or a command's folder, or
	// the folder a file manager shows. Empty starts where its machine
	// or profile starts.
	Folder string `json:"folder,omitempty"`

	// Line is what a command pane runs.
	Line string `json:"line,omitempty"`

	// Title is the name the user gave the pane, and empty for one they
	// did not name.
	Title string `json:"title,omitempty"`
}

// ErrLayoutNameUsed is returned when a layout is given a name another
// layout has, so the caller can offer to replace that one.
var ErrLayoutNameUsed = errors.New("name already used")

// errEnterName refuses a layout with no name.
var errEnterName = errors.New("enter a name")

// ErrNoLayout is returned when the layout to change is not in the list,
// as when another window removed it.
var ErrNoLayout = errors.New("layout not found")

// layoutList is the layouts as the file holds them. It is read without
// the strictness the rest of the file is read with: a later kakel adds
// to what a layout and its panes hold, and a field this one does not
// know must not lock it out of the whole settings file, when it can
// still open the layout by what it does know.
type layoutList []SavedLayout

// UnmarshalJSON reads the layouts, passing over fields it does not know.
func (l *layoutList) UnmarshalJSON(raw []byte) error {
	var out []SavedLayout
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	*l = out
	return nil
}

// Layouts are the layouts the user saved, in the order they were saved.
func (s *Settings) Layouts() []SavedLayout {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SavedLayout, len(s.have.Layouts))
	for i, l := range s.have.Layouts {
		out[i] = l.Clone()
	}
	return out
}

// SaveLayout keeps l under its name, and saves. It returns the layout as
// the file now holds it.
//
// A layout of a new name goes after the others, with an ID made from
// the name. A name another layout has is refused with
// ErrLayoutNameUsed, unless replace says to save over that one: then l
// takes its place in the list, its ID, and whether it opens at start.
//
// All of it is decided on the list as the file holds it now, so a
// layout another kakel saved or removed since is seen.
func (s *Settings) SaveLayout(l SavedLayout, replace bool) (SavedLayout, error) {
	l = l.Clone()
	l.Name = strings.TrimSpace(l.Name)
	err := s.putLayouts(func(have []SavedLayout) ([]SavedLayout, error) {
		if l.Name == "" {
			return nil, errEnterName
		}
		i := slices.IndexFunc(have, func(at SavedLayout) bool { return strings.EqualFold(at.Name, l.Name) })
		switch {
		case i >= 0 && !replace:
			return nil, ErrLayoutNameUsed
		case i >= 0:
			l.ID, l.AtStart = have[i].ID, have[i].AtStart
			have[i] = l
			return have, nil
		}
		l.ID = NewID("", l.Name, "layout", func(id string) bool {
			return slices.ContainsFunc(have, func(at SavedLayout) bool { return at.ID == id })
		})
		return append(have, l), nil
	})
	if err != nil {
		return SavedLayout{}, err
	}
	return l.Clone(), nil
}

// ReplaceLayout puts l in place of the layout with its ID, where that
// one stood in the list, and saves: a layout saved again over one of the
// same name, or renamed, or set to open at start. A name another layout
// has is refused with ErrLayoutNameUsed, and an ID no layout has with
// ErrNoLayout.
func (s *Settings) ReplaceLayout(l SavedLayout) error {
	l = l.Clone()
	l.Name = strings.TrimSpace(l.Name)
	return s.editLayout(l.ID, func(at *SavedLayout) { *at = l })
}

// RenameLayout gives the layout with id another name, and saves. Its ID
// stays, so whatever names the layout by it goes on finding it.
func (s *Settings) RenameLayout(id, name string) error {
	name = strings.TrimSpace(name)
	return s.editLayout(id, func(at *SavedLayout) { at.Name = name })
}

// PutLayoutAtStart has the layout with id open as kakel starts, or not,
// and saves.
func (s *Settings) PutLayoutAtStart(id string, on bool) error {
	return s.editLayout(id, func(at *SavedLayout) { at.AtStart = on })
}

// DropLayout takes the layout with id out of the list and saves.
func (s *Settings) DropLayout(id string) error {
	return s.putLayouts(func(have []SavedLayout) ([]SavedLayout, error) {
		return slices.DeleteFunc(have, func(at SavedLayout) bool { return at.ID == id }), nil
	})
}

// editLayout changes the layout with id as edit says, on the list as the
// file holds it now, and saves, unless that leaves its name another
// layout's.
func (s *Settings) editLayout(id string, edit func(*SavedLayout)) error {
	return s.putLayouts(func(have []SavedLayout) ([]SavedLayout, error) {
		i := slices.IndexFunc(have, func(at SavedLayout) bool { return at.ID == id })
		if i < 0 {
			return nil, ErrNoLayout
		}
		edit(&have[i])
		// The ID is the one thing an edit does not change.
		have[i].ID = id
		if err := layoutNameFree(have, have[i].Name, id); err != nil {
			return nil, err
		}
		return have, nil
	})
}

// putLayouts rereads the file, edits the list it holds and saves, unless
// the edit fails.
func (s *Settings) putLayouts(edit func([]SavedLayout) ([]SavedLayout, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.rereadLocked(); err != nil {
		return fmt.Errorf("%w: %w", ErrUnsaveable, err)
	}
	layouts, err := edit(slices.Clone(s.have.Layouts))
	if err != nil {
		return err
	}
	before := s.have
	s.have.Layouts = layouts
	if err := s.saveLocked(); err != nil {
		s.have = before
		return err
	}
	return nil
}

// layoutNameFree refuses name when a layout other than the one with id
// has it. Names are told apart as the user reads them, so case does not
// make two.
func layoutNameFree(have []SavedLayout, name, id string) error {
	if name == "" {
		return errEnterName
	}
	if slices.ContainsFunc(have, func(at SavedLayout) bool { return at.ID != id && strings.EqualFold(at.Name, name) }) {
		return ErrLayoutNameUsed
	}
	return nil
}

// NewID is the ID a new item called name is given, as a profile and a
// layout are: prefix, then its name in lower case, with a dash for each
// run of anything but letters and digits, or fallback for a name of
// neither, and a number on the end where taken says another has that.
func NewID(prefix, name, fallback string, taken func(id string) bool) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			dash = false
			continue
		}
		dash = true
	}
	slug := b.String()
	if slug == "" {
		slug = fallback
	}
	id := prefix + slug
	for n := 2; taken(id); n++ {
		id = prefix + slug + "-" + strconv.Itoa(n)
	}
	return id
}

// Clone returns a deep copy of l, to change while l stays as it is.
func (l SavedLayout) Clone() SavedLayout {
	l.Root = l.Root.Clone()
	l.Panes = maps.Clone(l.Panes)
	for slot, p := range l.Panes {
		p.Command = slices.Clone(p.Command)
		l.Panes[slot] = p
	}
	if l.Place != nil {
		place := *l.Place
		l.Place = &place
	}
	return l
}

// Clone returns a deep copy of n.
func (n *LayoutNode) Clone() *LayoutNode {
	if n == nil {
		return nil
	}
	c := *n
	c.Kids = nil
	for _, k := range n.Kids {
		c.Kids = append(c.Kids, k.Clone())
	}
	return &c
}

// checkLayouts reports a saved layout that could not be opened, or nil.
// What a pane runs is the window's business, so only a layout that names
// nothing, opens nothing, or is in the list twice is turned away.
func checkLayouts(layouts []SavedLayout) error {
	ids := make(map[string]bool, len(layouts))
	for i, l := range layouts {
		switch {
		case l.ID == "":
			return fmt.Errorf("layout %d has no id", i+1)
		case strings.TrimSpace(l.Name) == "":
			return fmt.Errorf("layout %d, %q, has no name", i+1, l.ID)
		case l.Root == nil || len(l.Panes) == 0:
			return fmt.Errorf("layout %d, %q, has no panes", i+1, l.Name)
		case ids[l.ID]:
			return fmt.Errorf("layout %d, %q, is in the list twice", i+1, l.ID)
		}
		ids[l.ID] = true
		// One name twice opens the first whichever is picked.
		if slices.ContainsFunc(layouts[:i], func(o SavedLayout) bool { return strings.EqualFold(o.Name, l.Name) }) {
			return fmt.Errorf("layout %d, %q, has the name of another", i+1, l.Name)
		}
	}
	return nil
}
