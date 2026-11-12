package app

import (
	"errors"
	"slices"
	"strconv"

	"github.com/marrasen/gunim/widget/dock"
	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/settings"
	shellfind "github.com/marrasen/kakel/shells"
)

// Layouts: one kakel window written down, to open again later as it
// was. What is kept is the window's splits and tabs, and for each pane
// a recipe to start it again: its machine, its shell, its folder, its
// command. Nothing that ran is kept, so opening a layout starts each
// pane afresh.
//
// Only terminals, commands and file managers are kept, and only those
// this window runs itself. A tool pane, as Settings or the Jobs, opens
// from its own command and holds nothing of the user's; a reader is of
// a file or a pane that may not be there next time; and a pane through
// another kakel window is that window's to start.

// Intents for layouts.
type (
	// SaveLayout writes window Window down as a layout called Name: 0
	// is the window in front. A name another layout has is refused with
	// settings.ErrLayoutNameUsed, unless Replace says to save over that
	// one, which keeps its ID and whether it opens at start.
	SaveLayout struct {
		Window  int
		Name    string
		Replace bool
	}
	// RenameLayout gives the layout with ID another name.
	RenameLayout struct{ ID, Name string }
	// SetLayoutAtStart has the layout with ID open as kakel starts, or
	// not.
	SetLayoutAtStart struct {
		ID string
		On bool
	}
	// RemoveLayout takes the layout with ID off the list.
	RemoveLayout struct{ ID string }
)

// errNothingToSave refuses a layout of a window holding nothing a
// layout can open again.
var errNothingToSave = errors.New("this window has no terminals, commands or files to save")

// saveLayout writes a window down as a layout, and returns it as kept
// and how many of the window's panes it leaves out. A split's chooser,
// which is waiting to be given a pane, is not counted among them.
func (a *app) saveLayout(in SaveLayout) (settings.SavedLayout, int, error) {
	if a.settings == nil {
		return settings.SavedLayout{}, 0, errNoSettings
	}
	w := a.cur
	if in.Window != 0 {
		w = a.winByID(in.Window)
	}
	if w == nil || w.gone {
		return settings.SavedLayout{}, 0, errors.New("the window was closed")
	}
	l, skipped, err := a.layoutOf(w)
	if err != nil {
		return settings.SavedLayout{}, skipped, err
	}
	l.Name = in.Name
	kept, err := a.settings.SaveLayout(l, in.Replace)
	a.st.Layouts = a.settings.Layouts()
	return kept, skipped, err
}

// layoutOf is window w as a layout, not yet named, and how many of its
// panes it leaves out.
func (a *app) layoutOf(w *ownWin) (settings.SavedLayout, int, error) {
	win := a.layout.Window(w.id)
	if win == nil || win.Root == nil {
		return settings.SavedLayout{}, 0, errNothingToSave
	}
	// The tree without the panes left out, each taken out as a closed
	// pane is: a tab beside it comes to the front in its place, a split
	// left with one part gives way to it, and a group left with no tabs
	// goes.
	one := dock.Layout{Grouped: a.layout.Grouped, Windows: []dock.Window{{Root: win.Root.Clone(), Keep: true}}}
	recipes := map[string]settings.SavedPane{}
	skipped := 0
	for _, id := range win.Root.Items() {
		i := slices.IndexFunc(a.st.Panes, func(p Pane) bool { return p.ID == id })
		if i >= 0 {
			if r, ok := a.recipeOf(a.st.Panes[i]); ok {
				recipes[id] = r
				continue
			}
			if a.st.Panes[i].Kind != KindChooser {
				skipped++
			}
		}
		one.Remove(id)
	}
	if len(recipes) == 0 {
		return settings.SavedLayout{}, skipped, errNothingToSave
	}
	l := settings.SavedLayout{Panes: map[string]settings.SavedPane{}}
	l.Root = slotted(one.Windows[0].Root, recipes, l.Panes)
	if place, ok := a.placeOf(w); ok {
		l.Place = &place
	}
	return l, skipped, nil
}

// slotted is the tree n as a layout keeps it, its panes named for slots
// in the order they stand, "p1" first, and its splits and groups "#1"
// and on, so a layout reads the same however long the run it was saved
// from. Each pane's recipe goes into panes under its slot.
func slotted(n *dock.Node, recipes, panes map[string]settings.SavedPane) *settings.LayoutNode {
	items, parts := 0, 0
	var walk func(n *dock.Node) *settings.LayoutNode
	walk = func(n *dock.Node) *settings.LayoutNode {
		out := &settings.LayoutNode{Kind: string(n.Kind), Share: n.Share}
		if n.Kind == dock.Item {
			items++
			out.ID = "p" + strconv.Itoa(items)
			panes[out.ID] = recipes[n.ID]
			return out
		}
		parts++
		out.ID = "#" + strconv.Itoa(parts)
		for _, k := range n.Kids {
			kid := walk(k)
			if k.ID == n.Front {
				out.Front = kid.ID
			}
			out.Kids = append(out.Kids, kid)
		}
		return out
	}
	return walk(n)
}

// recipeOf is how pane p is started again, and false for a pane a
// layout does not keep.
func (a *app) recipeOf(p Pane) (settings.SavedPane, bool) {
	if s := a.slots[p.ID]; s != nil {
		// A layout's pane still waiting: as that layout kept it.
		return s.recipe, true
	}
	if p.On != "" || a.machines.IsWindow(p.Machine) {
		// Through another kakel window: that window's to start.
		return settings.SavedPane{}, false
	}
	if a.stillSaved(p.Machine) != nil {
		// On a server removed from the list while the pane stayed open:
		// with no id, its name would be dialled as an address, or found
		// as another server given it since.
		return settings.SavedPane{}, false
	}
	r := settings.SavedPane{Host: a.keptAs(p.Machine), HostID: a.serverID(p.Machine)}
	if p.Named {
		r.Title = p.Title
	}
	switch {
	case p.Kind == KindFileManager:
		r.Kind, r.Folder = settings.LayoutFiles, a.filesFolder(p.ID, p.Machine)
	case p.Kind != KindTerminal:
		return settings.SavedPane{}, false
	case p.Command:
		cmd, ok := a.commands[p.ID]
		if !ok {
			return settings.SavedPane{}, false
		}
		r.Kind, r.Line, r.Folder = settings.LayoutCommand, joinLine(cmd.argv), cmd.dir
	default:
		r.Kind, r.Folder = settings.LayoutTerminal, a.terminalDir(p.ID, p.Machine)
		r.Profile, r.Command = a.runsOf(p)
	}
	return r, true
}

// runsOf is what terminal pane p runs, as a layout keeps it: a profile's
// ID, or on this computer, a program that is no profile's, as it was
// started. A terminal on a server that runs no profile runs the
// account's own shell, which is kept as LoginShell: left empty, it would
// open whatever profile the server runs by then.
func (a *app) runsOf(p Pane) (profile string, argv []string) {
	if p.Machine != machines.Local {
		if run := a.farRuns[p.ID]; run != "" {
			return run, nil
		}
		return LoginShell, nil
	}
	argv = a.argvs[p.ID]
	if len(argv) == 0 {
		// The machine's default shell, which is what opens with none.
		return "", nil
	}
	if prof, ok := a.profileOf(argv); ok {
		return prof.ID, nil
	}
	if sh, ok := shellfind.Running(a.found, argv); ok {
		return sh.ID, nil
	}
	return "", slices.Clone(argv)
}

// terminalDir is the folder the shell in terminal pane id on machine
// last said it is in, and "" where it has not said, or said it of
// another machine: one an ssh typed in the pane logged in to. On this
// computer it is the path as this computer names it, as a WSL shell's
// is not. Nothing is read from the disk to see that it is there, as a
// share that has gone would hold up the program; whoever starts a shell
// there finds out.
func (a *app) terminalDir(id string, machine machines.ID) string {
	t := a.terminal(id)
	if t == nil {
		return ""
	}
	dir, host := t.Dir()
	if machine != machines.Local {
		// The machine at the far end names its own paths, under the
		// name its shell first gave.
		if first := t.FirstDirHost(); host != "" && first != "" && !sameHost(shortHost(host), shortHost(first)) {
			return ""
		}
		return dir
	}
	if !isThisMachine(host) {
		return ""
	}
	return a.localPath(id, dir)
}

// filesFolder is the folder file manager pane id shows on machine, and
// "" while it shows none of that machine's own, as inside a zip.
func (a *app) filesFolder(id string, machine machines.ID) string {
	a.fmTitleMu.Lock()
	f, ok := a.fmFolders[id]
	a.fmTitleMu.Unlock()
	if !ok || f[0] != fsOf(machine) {
		return ""
	}
	return f[1]
}

// renameLayout gives a layout another name.
func (a *app) renameLayout(in RenameLayout) error {
	if a.settings == nil {
		return errNoSettings
	}
	err := a.settings.RenameLayout(in.ID, in.Name)
	a.st.Layouts = a.settings.Layouts()
	return err
}

// setLayoutAtStart has a layout open as kakel starts, or not.
func (a *app) setLayoutAtStart(in SetLayoutAtStart) error {
	if a.settings == nil {
		return errNoSettings
	}
	err := a.settings.PutLayoutAtStart(in.ID, in.On)
	a.st.Layouts = a.settings.Layouts()
	return err
}

// removeLayout takes a layout off the list.
func (a *app) removeLayout(id string) error {
	if a.settings == nil {
		return errNoSettings
	}
	err := a.settings.DropLayout(id)
	a.st.Layouts = a.settings.Layouts()
	return err
}
