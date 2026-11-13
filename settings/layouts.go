package settings

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"regexp"
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

	// Rev is a token given afresh each time the layout's tree and panes
	// are written as a whole: saved anew, or saved over. A change of
	// start lines, a rename, Open at start and putting a removed layout
	// back keep it. What was linked to a slot of the layout holds only
	// while Rev is the one it was linked against. A layout written
	// before this field, or by a kakel without it, has none, which is a
	// revision like any other until the layout is saved over.
	Rev string `json:"rev,omitempty"`
}

// newRev is a fresh revision token for a layout.
func newRev() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
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

	// Window says Host and HostID name another kakel window, which opens
	// the pane in a pane of its own that this one watches: a window in
	// the server list by its name and id, as a server is, or one
	// connected to by address alone by that address in Host, with no id.
	// On is, for a pane on a machine that window reaches, that machine as
	// the window names it, and empty for the window's own machine; OnKey
	// is what the window knew it by, which a rename there leaves as it
	// was. The key is asked for first, and the name where the window
	// knows no machine by the key, as one it reaches by a quick
	// connection, whose key is new each time it runs. A layout from
	// before these were kept has none of them, and holds no such pane.
	Window bool   `json:"window,omitempty"`
	On     string `json:"on,omitempty"`
	OnKey  string `json:"onKey,omitempty"`

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

	// Start are a terminal's start lines: what is typed into its shell as
	// the layout opens it, one line at each prompt, as the keyboard types
	// it, each followed by a return. Only a terminal has them, and none
	// is blank.
	Start []string `json:"start,omitempty"`
}

// StartLines are lines as a layout keeps them as start lines: a line
// holding line breaks is the lines it holds, each is trimmed on the
// right, a tab or a run of them is a space, as a tab typed into a shell
// starts its completion, and blank lines are dropped. A space at the
// start stays: it keeps the line out of a shell's history where the
// shell is set to ignore lines that start with one. None is nil.
func StartLines(lines []string) []string {
	var out []string
	for _, l := range lines {
		for _, one := range strings.FieldsFunc(l, func(r rune) bool { return r == '\r' || r == '\n' }) {
			if one = strings.TrimRightFunc(tabs.ReplaceAllString(one, " "), unicode.IsSpace); strings.TrimSpace(one) != "" {
				out = append(out, one)
			}
		}
	}
	return out
}

// tabs is a run of tabs.
var tabs = regexp.MustCompile("\t+")

// tidy is l with each pane's start lines as a layout keeps them, and
// none on a pane other than a terminal. The panes are copied where one
// changes, so a layout shared with another list is left as it is.
func (l SavedLayout) tidy() SavedLayout {
	copied := false
	for slot, p := range l.Panes {
		start := StartLines(p.Start)
		if p.Kind != LayoutTerminal {
			start = nil
		}
		if slices.Equal(start, p.Start) && (start == nil) == (p.Start == nil) {
			continue
		}
		if !copied {
			l.Panes = maps.Clone(l.Panes)
			copied = true
		}
		p.Start = start
		l.Panes[slot] = p
	}
	return l
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
	// Start lines written by hand are read as they are kept.
	for i := range out {
		out[i] = out[i].tidy()
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
// the file now holds it, and whether it took the place of another.
//
// A layout of a new name goes after the others, with an ID made from
// the name. A name another layout has is refused with
// ErrLayoutNameUsed, unless replace says to save over that one: then l
// takes its place in the list, its ID, and whether it opens at start.
//
// All of it is decided on the list as the file holds it now, so a
// layout another kakel saved or removed since is seen. Either way it is
// given a new Rev.
func (s *Settings) SaveLayout(l SavedLayout, replace bool) (SavedLayout, bool, error) {
	l = l.Clone().tidy()
	l.Name = strings.TrimSpace(l.Name)
	l.Rev = newRev()
	replaced := false
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
			replaced = true
			return have, nil
		}
		l.ID = NewID("", l.Name, "layout", func(id string) bool {
			return slices.ContainsFunc(have, func(at SavedLayout) bool { return at.ID == id })
		})
		return append(have, l), nil
	})
	if err != nil {
		return SavedLayout{}, false, err
	}
	return l.Clone(), replaced, nil
}

// ReplaceLayout puts l in place of the layout with its ID, where that
// one stood in the list, and saves, as a layout written as a whole, with
// a new Rev. A name another layout has is refused with
// ErrLayoutNameUsed, and an ID no layout has with ErrNoLayout.
func (s *Settings) ReplaceLayout(l SavedLayout) error {
	l = l.Clone().tidy()
	l.Name = strings.TrimSpace(l.Name)
	l.Rev = newRev()
	return s.editLayout(l.ID, func(at *SavedLayout) { *at = l })
}

// ErrLayoutSavedOver is returned when start lines are put on a layout
// that was saved over since they were written for it: its slots may
// hold other panes now.
var ErrLayoutSavedOver = errors.New("layout saved over")

// PutStartLines gives each slot lines names the start lines it holds
// for it, none for nil, and saves: lines holds the slots whose lines
// were changed, and the others keep theirs, so two windows giving lines
// to two terminals of one layout both stand. Only a terminal takes
// lines. was is the layout as the lines were written for it, and
// StartLinesFit says whether they still fit the layout as the file holds
// it now; one no longer in the list is refused with ErrNoLayout. A name
// or Open at start changed since stays as it is now.
func (s *Settings) PutStartLines(was SavedLayout, lines map[string][]string) error {
	return s.putLayouts(func(have []SavedLayout) ([]SavedLayout, error) {
		i := slices.IndexFunc(have, func(at SavedLayout) bool { return at.ID == was.ID })
		if i < 0 {
			return nil, ErrNoLayout
		}
		if err := StartLinesFit(have[i], was, lines); err != nil {
			return nil, err
		}
		l := have[i].Clone()
		for slot, start := range lines {
			if p, ok := l.Panes[slot]; ok && p.Kind == LayoutTerminal {
				p.Start = StartLines(start)
				l.Panes[slot] = p
			}
		}
		have[i] = l
		return have, nil
	})
}

// StartLinesFit reports whether lines, start lines written for the
// layout as was held it, fit it as now holds it: ErrLayoutSavedOver
// where now has other splits or tabs, or a pane started another way, or
// other lines on a slot lines changes, as when another window saved over
// it or gave that terminal lines of its own; nil where they fit. Both are
// compared as the settings file carries them, so words the file cannot
// hold exactly, as a folder named in bytes that are not UTF-8, read the
// same on both sides.
func StartLinesFit(now, was SavedLayout, lines map[string][]string) error {
	now, was = asFiled(now), asFiled(was)
	if !reflect.DeepEqual(now.Root, was.Root) || !maps.EqualFunc(now.Panes, was.Panes, sameRecipe) {
		return ErrLayoutSavedOver
	}
	for slot := range lines {
		if !slices.Equal(now.Panes[slot].Start, was.Panes[slot].Start) {
			return ErrLayoutSavedOver
		}
	}
	return nil
}

// asFiled is l as it reads back from the settings file: written out and
// read in again, its start lines tidied as the file's are.
func asFiled(l SavedLayout) SavedLayout {
	raw, err := json.Marshal(l)
	if err != nil {
		return l.tidy()
	}
	var out SavedLayout
	if err := json.Unmarshal(raw, &out); err != nil {
		return l.tidy()
	}
	return out.tidy()
}

// sameRecipe reports whether a and b start the same pane, whatever start
// lines each holds.
func sameRecipe(a, b SavedPane) bool {
	a.Start, b.Start = nil, nil
	return reflect.DeepEqual(a, b)
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

// PutLayoutBack puts l, as it was before it was removed, back in the
// list at index at, or at the end where the list is shorter now, and
// saves: Undo after DropLayout. It keeps its ID, unless another layout
// was given that ID since, when it is given one of its own as a new
// layout is. A name another layout took since is refused with
// ErrLayoutNameUsed, as a layout saved under it would be. A layout back
// in the list already, just as it was but for its Rev, as after a second
// Undo or after a window opened from it was saved as it again, is left as
// it is, and returned as the file holds it.
//
// It keeps the Rev it had. Put back under its own ID, it is the layout
// panes were linked to, so their links hold again. Given a new ID, it is
// linked to by nothing: the panes linked to its old ID are another
// layout's now, or none.
func (s *Settings) PutLayoutBack(l SavedLayout, at int) (SavedLayout, error) {
	l = l.Clone().tidy()
	err := s.putLayouts(func(have []SavedLayout) ([]SavedLayout, error) {
		sameButRev := func(o SavedLayout) bool {
			o.Rev = l.Rev
			return reflect.DeepEqual(o, l)
		}
		if i := slices.IndexFunc(have, sameButRev); i >= 0 {
			// Back already: nothing to do.
			l = have[i].Clone()
			return have, nil
		}
		// Checked against every layout: one given its ID since is
		// another layout.
		if err := layoutNameFree(have, strings.TrimSpace(l.Name), ""); err != nil {
			return nil, err
		}
		taken := func(id string) bool {
			return slices.ContainsFunc(have, func(o SavedLayout) bool { return o.ID == id })
		}
		if l.ID == "" || taken(l.ID) {
			l.ID = NewID("", l.Name, "layout", taken)
		}
		return slices.Insert(have, min(max(at, 0), len(have)), l), nil
	})
	if err != nil {
		return SavedLayout{}, err
	}
	return l.Clone(), nil
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
	for i := range layouts {
		layouts[i] = layouts[i].tidy()
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
		p.Start = slices.Clone(p.Start)
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
