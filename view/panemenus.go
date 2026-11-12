package view

import (
	"slices"
	"strings"
	"unicode"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/filemanager"
	"github.com/marrasen/gunim/icon"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/app"
)

// One menu bar for the window, which takes the lines of the pane in
// front: a file manager's File, Edit and View lines join kakel's menus
// of those names, and its Go menu comes after View, while it is in
// front. Its own menus hide, as kakel shows them.

// barMenu is a menu of the bar: its title and its lines.
type barMenu struct {
	title string
	items []menuItem
}

// baseLayout is kakel's own menus, before any pane's lines join them.
func baseLayout() []barMenu {
	out := make([]barMenu, len(menus))
	for i, m := range menus {
		out[i] = barMenu{title: m.title, items: slices.Clone(m.items)}
	}
	return out
}

// buildBar makes the bar's menus from the layout, and with refill fills
// those made from the window's state again: the servers. A window being
// made fills them as its state comes.
func (w *Window) buildBar(refill bool) {
	w.menus = w.menus[:0:0]
	w.subs = make([][][]menuItem, len(w.layout))
	for m, bm := range w.layout {
		w.subs[m] = make([][]menuItem, len(bm.items))
		for i, it := range bm.items {
			if it.sub != "" {
				w.subs[m][i] = submenus[it.sub].lines(w)
			}
		}
		w.menus = append(w.menus, w.barMenu(bm.title, bm.items, w.subs[m]))
	}
	if refill {
		w.servers(w.saved)
	}
	w.publishBar()
}

// barMenu makes a menu of the bar, or a submenu's lines, titled title,
// from items, with subs the lines of each item's submenu.
func (w *Window) barMenu(title string, items []menuItem, subs [][]menuItem) widget.BarMenu {
	bm := widget.BarMenu{Title: title}
	var first []int
	for i, it := range items {
		if it.keyFirst {
			first = append(first, i)
		}
		hint := it.hint
		if chord, ok := w.keys.ChordFor(it.id); ok && !it.caption && it.pane == "" {
			hint = chordLabel(chord)
		}
		line := widget.MenuItem{Label: w.commandTitle(it.id, it.title), Hint: hint, Icon: commandIcons[it.id],
			Checked: it.on, Caption: it.caption, Break: it.group || (it.caption && i > 0)}
		if it.sub != "" {
			line.Icon = submenus[it.sub].icon
			line.Sub = &widget.Submenu{Items: w.barMenu("", subs[i], nil).Items}
		}
		bm.Items = append(bm.Items, line)
	}
	return withAccessKeys(bm, first...)
}

// submenus are the submenus a line of the bar opens, by the name its
// sub gives: the icon of the line, and the submenu's lines, made from
// the window's state.
var submenus = map[string]struct {
	icon  *icon.Icon
	lines func(w *Window) []menuItem
}{
	"layouts": {icon.LayoutPanelLeft, (*Window).layoutLines},
}

// layoutLines are the lines of File's Layouts: each layout to open, in
// the order kept, and the commands that save and manage them, which
// keep their letters whatever the layouts are called.
func (w *Window) layoutLines() []menuItem {
	var out []menuItem
	for _, l := range w.layouts {
		out = append(out, menuItem{id: layoutOpenPrefix + l.ID, title: menuName(l.Name)})
	}
	return append(out,
		menuItem{id: "layout.save", title: "Save Layout…", group: len(out) > 0, keyFirst: true},
		menuItem{id: "layout.manage", title: "Manage Layouts", keyFirst: true})
}

// menuNameMax is the most characters a name takes on a menu line, so a
// long one leaves the menu a menu's width.
const menuNameMax = 40

// menuName is a name as a menu line shows it: cut short with an … past
// menuNameMax characters.
func menuName(s string) string {
	rs := []rune(s)
	if len(rs) <= menuNameMax {
		return s
	}
	return strings.TrimRightFunc(string(rs[:menuNameMax-1]), unicode.IsSpace) + "…"
}

// barPicks are what the lines of the bar's menus run, as the bar last
// took its menus: the layout, the submenus' lines, and the Machines
// menu's commands.
type barPicks struct {
	layout    []barMenu
	subs      [][][]menuItem
	serverIDs []string
}

// publishBar gives the bar the menus made last, and what their lines
// run with them. The bar keeps the menus it opened with until it
// closes, and a pick is an index into them, so while it is open the
// new menus wait, and come once it has closed.
func (w *Window) publishBar() {
	if w.bar.IsOpen() {
		w.barStale = true
		return
	}
	w.barStale = false
	w.bar.Menus = slices.Clone(w.menus)
	w.picks = barPicks{layout: w.layout, subs: w.subs, serverIDs: w.serverIDs}
}

// barClosed gives the bar the menus that waited while it was open,
// after the pick that closed it has run against the menus it showed.
func (w *Window) barClosed(u *gunim.UI) {
	if !w.barStale {
		return
	}
	u.After(0, func(u *gunim.UI) {
		if w.barStale {
			w.publishBar()
			u.Invalidate()
		}
	})
}

// pickLine runs the line at item of menu m, as the bar showed it.
func (w *Window) pickLine(m, item int, u *gunim.UI) gunim.Intent {
	p := w.picks
	switch {
	case m < 0 || m >= len(p.layout) || item < 0:
	case p.layout[m].title == "Machines":
		if item < len(p.serverIDs) {
			w.run(p.serverIDs[item], u)
		}
	case item < len(p.layout[m].items):
		w.runLine(p.layout[m].items[item], u)
	}
	return nil
}

// pickSubLine runs the line at path of menu m's submenus, as the bar
// showed them: the line whose submenu holds it, then its place there.
func (w *Window) pickSubLine(m int, path []int, u *gunim.UI) gunim.Intent {
	p := w.picks
	if len(path) != 2 || m < 0 || m >= len(p.subs) || path[0] < 0 || path[0] >= len(p.subs[m]) {
		return nil
	}
	if lines := p.subs[m][path[0]]; path[1] >= 0 && path[1] < len(lines) {
		w.runLine(lines[path[1]], u)
	}
	return nil
}

// runLine runs a line of a menu: a line of the pane in front's, or a
// command.
func (w *Window) runLine(it menuItem, u *gunim.UI) {
	if it.pane != "" {
		w.runPane(it.pane, u)
		return
	}
	w.run(it.id, u)
}

// terminalOnly are kakel's lines for a terminal alone, which a file
// manager in front leaves out of the menus, with the Scrollback caption.
var terminalOnly = map[string]bool{
	"edit.pasteImage": true, "pane.scrollback": true, "view.scrollUp": true, "view.scrollDown": true,
}

// paneLayout is kakel's menus with the lines of the file manager in
// front, fm's menus, joined.
func paneLayout(fm []filemanager.Menu) []barMenu {
	out := baseLayout()
	for i := range out {
		out[i].items = slices.DeleteFunc(out[i].items, func(it menuItem) bool {
			return terminalOnly[it.id] || it.caption && it.title == "Scrollback"
		})
	}
	for _, m := range fm {
		var lines []menuItem
		for i, it := range m.Items {
			lines = append(lines, menuItem{id: "fm." + it.Cmd, title: it.Label, pane: it.Cmd, hint: it.Hint, on: it.Checked,
				group: it.Line || i == 0})
		}
		if len(lines) == 0 {
			continue
		}
		at := slices.IndexFunc(out, func(b barMenu) bool { return b.title == m.Title })
		if at < 0 {
			// A menu of its own, as Go, after View.
			after := slices.IndexFunc(out, func(b barMenu) bool { return b.title == "View" }) + 1
			lines[0].group = false
			out = slices.Insert(out, after, barMenu{title: m.Title, items: lines})
			continue
		}
		items := out[at].items
		where := len(items)
		if m.Title == "File" {
			// Before Settings, Close and Exit, which end a File menu.
			if i := slices.IndexFunc(items, func(it menuItem) bool { return it.id == "app.settings" }); i >= 0 {
				where = i
			}
		}
		out[at].items = slices.Insert(items, where, lines...)
	}
	return out
}

// layoutMenus takes the lines of the pane in front into the menus,
// building the bar again where their lines changed, not their ticks.
func (w *Window) layoutMenus(u *gunim.UI) {
	if w.barStale {
		// Menus that waited for a bar closed with no word, as by being
		// taken off the window.
		w.publishBar()
	}
	next := baseLayout()
	if _, ok := w.fmHosts[w.focused]; ok {
		if fm := filemanager.Menus(u, app.FilePaneViews(w.focused)); fm != nil {
			next = paneLayout(fm)
		}
	}
	if !sameLines(next, w.layout) {
		// While a menu is open, the bar takes them once it closes.
		w.layout = next
		w.buildBar(true)
		return
	}
	w.layout = next
}

// sameLines reports whether a and b have the same menus and lines, ticked
// alike or not.
func sameLines(a, b []barMenu) bool {
	return slices.EqualFunc(a, b, func(x, y barMenu) bool {
		return x.title == y.title && slices.EqualFunc(x.items, y.items, func(p, q menuItem) bool {
			p.on, q.on = false, false
			return p == q
		})
	})
}

// tickPaneMenus ticks the pane's lines as the pane is now, as a menu
// opens: what it shows changes with no word to the window.
func (w *Window) tickPaneMenus(u *gunim.UI) {
	if _, ok := w.fmHosts[w.focused]; !ok {
		return
	}
	next := paneLayout(filemanager.Menus(u, app.FilePaneViews(w.focused)))
	if !sameLines(next, w.layout) {
		return
	}
	for m := range next {
		for i, it := range next[m].items {
			if it.pane != "" && m < len(w.menus) && i < len(w.menus[m].Items) {
				w.menus[m].Items[i].Checked = it.on
			}
		}
	}
	w.layout = next
	u.Invalidate()
}

// runPane runs cmd, a line of the menus of the file manager in front.
func (w *Window) runPane(cmd string, u *gunim.UI) {
	filemanager.Run(u, app.FilePaneViews(w.focused), cmd)
}
