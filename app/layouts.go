package app

import (
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/marrasen/gunim/widget/dock"
	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/relay"
	"github.com/marrasen/kakel/settings"
	shellfind "github.com/marrasen/kakel/shells"
	"github.com/marrasen/kakel/words"
)

// Layouts: one kakel window written down, to open again later as it
// was. What is kept is the window's splits and tabs, and for each pane
// a recipe to start it again: its machine, its shell, its folder, its
// command. Nothing that ran is kept, so opening a layout starts each
// pane afresh.
//
// Only terminals, commands and file managers are kept. A tool pane, as
// Settings or the Jobs, opens from its own command and holds nothing of
// the user's; and a reader is of a file or a pane that may not be there
// next time. A pane through another kakel window is kept as the window
// it went through and the machine there it ran on, and opening the
// layout watches again the pane it opened there last time, while that
// is still open there, or asks that window for a new one, as New
// Terminal there does.

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
	// RemoveLayout takes the layout with ID off the list, and says so
	// with Undo.
	RemoveLayout struct{ ID string }
	// RestoreLayout puts Layout, removed, back in the list where it
	// stood, at At, with Panes, what was written down of the panes it
	// opened on other windows: Undo after RemoveLayout.
	RestoreLayout struct {
		Layout settings.SavedLayout
		At     int
		Panes  []settings.LayoutPane
	}
	// ShowLayouts opens the Layouts pane, or goes to it.
	ShowLayouts struct{}
)

// KindLayouts is the Layouts pane, which lists the layouts to open,
// rename and remove.
const KindLayouts = "layouts"

// ErrNothingToSave refuses a layout of a window holding nothing a
// layout can open again.
var ErrNothingToSave = errors.New("this window has no terminals, commands or files to save")

// saveLayout writes a window down as a layout, and returns it as kept,
// what of the window's panes it leaves out, and whether it took the
// place of a layout of its name.
func (a *app) saveLayout(in SaveLayout) (settings.SavedLayout, leftOut, bool, error) {
	if a.settings == nil {
		return settings.SavedLayout{}, leftOut{}, false, errNoSettings
	}
	w := a.cur
	if in.Window != 0 {
		w = a.winByID(in.Window)
	}
	if w == nil || w.gone {
		return settings.SavedLayout{}, leftOut{}, false, errors.New("the window was closed")
	}
	l, names, left, err := a.layoutOf(w)
	if err != nil {
		return settings.SavedLayout{}, left, false, err
	}
	l.Name = in.Name
	kept, replaced, err := a.settings.SaveLayout(l, in.Replace)
	a.st.Layouts = a.settings.Layouts()
	if err == nil {
		// Saved as it, the window is offered it to save over next time.
		w.fromLayout = kept.ID
		a.recordSaved(kept.ID, kept, names)
	}
	return kept, left, replaced, err
}

// sayLayoutSaved says layout l was saved, or saved over one of its
// name, and what of the window's panes it left out.
func (a *app) sayLayoutSaved(l settings.SavedLayout, replaced bool, left leftOut) {
	title := "Saved " + l.Name
	if replaced {
		title = "Replaced " + l.Name
	}
	body := ""
	if said := left.said(); said != "" {
		body = "Left out: " + said + "."
	}
	a.worked(title, body, "")
}

// leftOut counts the panes of a window a layout leaves out that the user
// might look for when it opens: readers, tunnels' panes, and panes on a
// server or through a kakel window removed from the list. The
// panes that hold nothing of the user's, the tools such as Settings, the
// Jobs, the Layouts and a connection's log, and a split's chooser, are
// not counted, as they are opened again from their own commands.
type leftOut struct {
	readers, tunnels, through, removed, other int
}

// total is how many panes were left out.
func (l leftOut) total() int { return l.readers + l.tunnels + l.through + l.removed + l.other }

// said says what was left out, as "1 reader and 2 panes on a removed
// server", or "" for nothing.
func (l leftOut) said() string {
	var parts []string
	for _, n := range []struct {
		n         int
		one, many string
	}{
		{l.readers, "reader", "readers"},
		{l.tunnels, "tunnel", "tunnels"},
		{l.through, "pane through a removed window", "panes through a removed window"},
		{l.removed, "pane on a removed server", "panes on a removed server"},
		{l.other, "other pane", "other panes"},
	} {
		if n.n > 0 {
			parts = append(parts, words.ManyOf(n.n, n.one, n.many))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return listOf(parts)
}

// countLeftOut counts pane p, which the layout leaves out, where the user might
// look for it.
func (a *app) countLeftOut(l *leftOut, p Pane) {
	switch {
	case p.Kind == KindReader:
		l.readers++
	case p.Kind == KindTunnel:
		l.tunnels++
	case p.Kind != KindTerminal && p.Kind != KindFileManager:
		// A tool, or a split's chooser: opened again by its command.
	case a.stillSaved(p.Machine) != nil && a.isThrough(p):
		l.through++
	case a.stillSaved(p.Machine) != nil:
		l.removed++
	default:
		l.other++
	}
}

// layoutOf is window w as a layout, not yet named, the slot each pane
// kept went into, by pane, and what of its panes it leaves out.
func (a *app) layoutOf(w *ownWin) (settings.SavedLayout, map[string]string, leftOut, error) {
	var left leftOut
	win := a.layout.Window(w.id)
	if win == nil || win.Root == nil {
		return settings.SavedLayout{}, nil, left, ErrNothingToSave
	}
	// The tree without the panes left out, each taken out as a closed
	// pane is: a tab beside it comes to the front in its place, a split
	// left with one part gives way to it, and a group left with no tabs
	// goes.
	one := dock.Layout{Grouped: a.layout.Grouped, Windows: []dock.Window{{Root: win.Root.Clone(), Keep: true}}}
	recipes := map[string]settings.SavedPane{}
	for _, id := range win.Root.Items() {
		i := slices.IndexFunc(a.st.Panes, func(p Pane) bool { return p.ID == id })
		if i >= 0 {
			if r, ok := a.recipeOf(a.st.Panes[i]); ok {
				recipes[id] = r
				continue
			}
			a.countLeftOut(&left, a.st.Panes[i])
		}
		one.Remove(id)
	}
	if len(recipes) == 0 {
		return settings.SavedLayout{}, nil, left, ErrNothingToSave
	}
	l := settings.SavedLayout{Panes: map[string]settings.SavedPane{}}
	names := map[string]string{}
	l.Root = slotted(one.Windows[0].Root, recipes, l.Panes, names)
	if place, ok := a.placeOf(w); ok {
		l.Place = &place
	}
	return l, names, left, nil
}

// slotted is the tree n as a layout keeps it, its panes named for slots
// in the order they stand, "p1" first, and its splits and groups "#1"
// and on, so a layout reads the same however long the run it was saved
// from. Each pane's recipe goes into panes under its slot, and the
// slot's name into names, by the pane's ID.
func slotted(n *dock.Node, recipes, panes map[string]settings.SavedPane, names map[string]string) *settings.LayoutNode {
	items, parts := 0, 0
	var walk func(n *dock.Node) *settings.LayoutNode
	walk = func(n *dock.Node) *settings.LayoutNode {
		out := &settings.LayoutNode{Kind: string(n.Kind), Share: n.Share}
		if n.Kind == dock.Item {
			items++
			out.ID = "p" + strconv.Itoa(items)
			panes[out.ID] = recipes[n.ID]
			names[n.ID] = out.ID
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
	if p.Kind != KindTerminal && p.Kind != KindFileManager {
		return settings.SavedPane{}, false
	}
	if a.stillSaved(p.Machine) != nil {
		// On a server, or through a window, removed from the list while
		// the pane stayed open: with no id, its name would be dialled as
		// an address, or found as another given it since.
		return settings.SavedPane{}, false
	}
	through := a.isThrough(p)
	r := settings.SavedPane{Host: a.keptAs(p.Machine), HostID: a.serverID(p.Machine)}
	if through {
		var ok bool
		if r, ok = a.windowKept(p); !ok {
			return settings.SavedPane{}, false
		}
	}
	if p.Named {
		r.Title = p.Title
	}
	switch {
	case p.Kind == KindFileManager:
		files := p.Machine
		if p.On != "" {
			files = machines.FarID(p.Machine, p.On)
		}
		r.Kind, r.Folder = settings.LayoutFiles, a.filesFolder(p.ID, files)
	case p.Command:
		cmd, ok := a.commands[p.ID]
		if !ok {
			return settings.SavedPane{}, false
		}
		r.Kind, r.Line, r.Folder = settings.LayoutCommand, joinLine(cmd.argv), cmd.dir
	default:
		r.Kind = settings.LayoutTerminal
		if p.On == "" {
			// Beyond a window, a terminal starts where its machine starts
			// it: a folder kept would only be said not to be restored, at
			// each open.
			r.Folder = a.terminalDir(p.ID, p.Machine)
		}
		if !through {
			// Through a window, its shell is the window's to pick: this
			// one is not told which it ran.
			r.Profile, r.Command = a.runsOf(p)
		}
		// As the layout that opened it kept them.
		r.Start = slices.Clone(a.startLines[p.ID])
	}
	return r, true
}

// isThrough reports whether pane p is through another kakel window: on
// its own machine, or on one it reaches.
func (a *app) isThrough(p Pane) bool {
	return p.On != "" || a.machines.IsWindow(p.Machine) || a.machines.Get(p.Machine).Window != nil
}

// windowKept is how a layout names the kakel window pane p is through,
// and the machine beyond it there: a window in the server list by its
// name and id, as a server is, and one connected to by address alone by
// that address, the only way to reach it again. The machine beyond goes
// by the window's key for it, which a rename there leaves as it is, and
// by what the window calls it, which lasts where a quick connection's
// key there does not. False for a window known by neither.
func (a *app) windowKept(p Pane) (settings.SavedPane, bool) {
	r := settings.SavedPane{Window: true}
	if target, _, quick := a.machines.Quick(p.Machine); quick {
		r.Host = target
	} else if _, saved := a.machines.Saved(p.Machine); saved {
		r.Host, r.HostID = a.machines.Name(p.Machine), string(p.Machine)
	} else {
		return settings.SavedPane{}, false
	}
	if p.On != "" {
		r.On, r.OnKey = a.machines.FarName(p.Machine, p.On), p.On
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

// removeLayout takes a layout off the list, and says so, with Undo to
// put it back where it stood.
func (a *app) removeLayout(id string) error {
	if a.settings == nil {
		return errNoSettings
	}
	have := a.settings.Layouts()
	at := slices.IndexFunc(have, func(l settings.SavedLayout) bool { return l.ID == id })
	recs := a.recordsOf(id)
	if err := a.settings.DropLayout(id); err != nil {
		return err
	}
	a.st.Layouts = a.settings.Layouts()
	// The panes it opened elsewhere stay open there; what it wrote down
	// of them goes with it, and comes back with Undo.
	a.editRecords(func(p []settings.LayoutPane) []settings.LayoutPane {
		return slices.DeleteFunc(p, func(r settings.LayoutPane) bool { return r.Layout == id })
	})
	if at >= 0 {
		a.post(Notice{Title: "Removed " + have[at].Name, Kind: NoticeWorked, Action: "Undo", On: RestoreLayout{Layout: have[at], At: at, Panes: recs}})
	}
	return nil
}

// restoreLayout puts a layout removed back where it stood. One whose
// name another layout was given since stays removed.
func (a *app) restoreLayout(in RestoreLayout) error {
	if a.settings == nil {
		return errNoSettings
	}
	back, err := a.settings.PutLayoutBack(in.Layout, in.At)
	a.st.Layouts = a.settings.Layouts()
	if err == nil {
		a.restoreRecords(back.ID, in.Panes)
	}
	if errors.Is(err, settings.ErrLayoutNameUsed) {
		return errors.New("another layout is called " + strings.TrimSpace(in.Layout.Name) + " now")
	}
	return err
}

// showLayouts opens the Layouts pane, or goes to it.
func (a *app) showLayouts() {
	if id := a.paneOf(KindLayouts); id != "" {
		a.bringHere(id)
		return
	}
	a.next++
	a.addPane(Pane{ID: "p" + itoa(a.next), Title: "Layouts", Kind: KindLayouts}, nil, Placement{})
}

// savesFrom is the window Save Layout in w saves: w, or where w holds
// nothing a layout keeps, as a window of tools, the window last worked
// in. It is nil where neither holds anything to keep.
func (a *app) savesFrom(w *ownWin) *ownWin {
	for _, at := range []*ownWin{w, a.work} {
		if at != nil && !at.gone && a.keepsAny(at) {
			return at
		}
	}
	return nil
}

// keepsAny reports whether window w holds a pane a layout keeps, as
// recipeOf would find, without asking each what it shows.
func (a *app) keepsAny(w *ownWin) bool {
	return slices.ContainsFunc(a.panesIn(w), func(p Pane) bool {
		switch {
		case p.Kind == KindWaiting:
			return true
		case p.Kind != KindTerminal && p.Kind != KindFileManager:
			return false
		}
		return a.stillSaved(p.Machine) == nil
	})
}

// LayoutHolds says in words what layout l opens: "2 terminals and 1
// file manager".
func LayoutHolds(l settings.SavedLayout) string {
	n := map[string]int{}
	for _, p := range l.Panes {
		n[p.Kind]++
	}
	var parts []string
	for _, k := range []struct{ kind, one, many string }{
		{settings.LayoutTerminal, "terminal", "terminals"},
		{settings.LayoutCommand, "command", "commands"},
		{settings.LayoutFiles, "file manager", "file managers"},
	} {
		if n[k.kind] > 0 {
			parts = append(parts, words.ManyOf(n[k.kind], k.one, k.many))
		}
	}
	if len(parts) == 0 {
		return words.ManyOf(len(l.Panes), "pane", "panes")
	}
	return listOf(parts)
}

// LayoutMachines are the names of the machines layout l opens panes on,
// as LayoutHosts gives them.
func LayoutMachines(l settings.SavedLayout, named func(id string) (string, bool)) []string {
	var out []string
	for _, h := range LayoutHosts(l, named) {
		out = append(out, h.Name)
	}
	return out
}

// LayoutHost is a machine a layout opens panes on: its name, whether it
// is this computer, and whether it is another kakel window's own.
type LayoutHost struct {
	Name   string
	Here   bool
	Window bool
}

// LayoutHosts are the machines layout l opens panes on, each once, in
// the order its panes stand: "This computer", and a server or a kakel
// window by what named calls the saved one with its id now, or else by
// the name it was saved under, or for a window connected to by address,
// by its address as the sidebar shows it, a relay's secret left out. A
// machine beyond a window is named "db through desk", as the sidebar
// names it.
func LayoutHosts(l settings.SavedLayout, named func(id string) (string, bool)) []LayoutHost {
	var out []LayoutHost
	var walk func(n *settings.LayoutNode)
	walk = func(n *settings.LayoutNode) {
		if n == nil {
			return
		}
		if n.Kind == string(dock.Item) {
			p, ok := l.Panes[n.ID]
			if !ok {
				return
			}
			h := LayoutHost{Name: p.Host, Here: p.Host == "" && p.HostID == ""}
			switch {
			case h.Here:
				h.Name = "This computer"
			case p.HostID != "" && named != nil:
				if now, ok := named(p.HostID); ok {
					h.Name = now
				}
			case p.Window:
				h.Name = relay.Shown(p.Host)
			}
			if p.Window {
				if p.On != "" {
					h.Name = p.On + " through " + h.Name
				} else {
					h.Window = true
				}
			}
			if !slices.Contains(out, h) {
				out = append(out, h)
			}
			return
		}
		for _, k := range n.Kids {
			walk(k)
		}
	}
	walk(l.Root)
	return out
}
