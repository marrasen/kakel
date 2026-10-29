package view

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/icon"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/look"
	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/meter"
	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/settings"
	"github.com/marrasen/kakel/words"
)

// A machine's pane: a sidebar down the left with the machine at the top
// and its profiles under it, and what is picked there to the right, to
// change. The machine's own row has how it is reached; a profile's row
// its name, command line, folder, variables and whether its shell is
// taught. A star marks the profile a new terminal runs, an eye shows or
// hides one kakel found, and a bin deletes one the user made.

// Keys of the sidebar's rows that are not profiles.
const (
	mpMachine  = "machine"
	mpProfiles = "head:profiles"
	mpAdd      = "add:profile"
	// mpProfile starts a profile's key, before its ID.
	mpProfile = "profile:"
	mpFiles   = "head:files"
	mpAddDir  = "add:folder"
	// mpFolder starts a folder's key: a found one's ID, or a
	// favourite's path.
	mpFolder = "folder:"
	// mpSaved starts the key of a command, copy or tunnel kept.
	mpSaved = "saved:"
)

// The room of a machine pane: round its sidebar, round the rest, between
// the parts of the rest, and between the parts of a row.
var (
	mpSidePad = theme.Insets("kakel.machine.side", geom.Insets{Left: 8, Right: 8, Top: 12, Bottom: 12})
	mpPad     = theme.Insets("kakel.machine.pad", geom.Insets{Left: 28, Right: 28, Top: 20, Bottom: 28})
	mpGap     = theme.Length("kakel.machine.gap", 12)
	mpRowGap  = theme.Length("kakel.machine.row", 8)
	// mpHeadSize is the machine's name's size at the top of the
	// sidebar, mpHeadIcon its icon's, and mpHeadHeight that row's height.
	mpHeadSize   = theme.Length("kakel.machine.head.size", 20)
	mpHeadIcon   = theme.Length("kakel.machine.head.icon", 28)
	mpHeadHeight = theme.Length("kakel.machine.head.height", 64)
	// mpFootPad is the room round the status switch under the sidebar.
	mpFootPad = theme.Insets("kakel.machine.foot", geom.Uniform(10))
)

// mpItem is one row of a machine pane's sidebar: the machine, a
// heading, a profile, or the row that adds one.
type mpItem struct {
	key, title string
	icon       *icon.Icon
	heading    bool
	// profile is what a profile row is about, and folder a folder's;
	// saved is a command, copy or tunnel kept for next time.
	profile mpProfileOf
	folder  mpFolderOf
	saved   *mpSavedOf
	// tunnel is a tunnel's or SOCKS proxy's, and plus the key of the row
	// a heading's + picks.
	tunnel *mpTunnelOf
	plus   string
}

// mpSavedOf is something the machine keeps for next time: a command, a
// copy or a tunnel, one of them set.
type mpSavedOf struct {
	command *settings.SavedCommand
	copy    *settings.SavedCopy
	tunnel  *settings.SavedTunnel
}

// mpFolderOf is a folder as a machine pane's Files list it: a favourite
// the user saved, fav, or one kakel found, its home or a drive, with
// what the user changed of it.
type mpFolderOf struct {
	id   string
	fav  *app.Favourite
	edit settings.FoundProfile
	// name is what kakel found it as, and path where it is, for one
	// found; path is "" for a home, which is wherever the account's is.
	name, path string
}

// found reports whether kakel found the folder.
func (f mpFolderOf) found() bool { return f.id != "" }

// title is what the folder is called.
func (f mpFolderOf) title() string {
	switch {
	case f.fav != nil:
		return f.fav.Label()
	case f.edit.Name != "":
		return f.edit.Name
	}
	return f.name
}

// hidden reports whether the folder is one found and hidden.
func (f mpFolderOf) hidden() bool { return f.found() && f.edit.Hidden }

// mpProfileOf is a profile as a machine pane lists it: one the user
// made, own, or one kakel found, with what the user changed of it.
type mpProfileOf struct {
	id   string
	own  *settings.Profile
	edit settings.FoundProfile
	// name is what kakel found it as, and line what it runs, for one
	// found.
	name, line string
	def, found bool
}

// title is what the profile is called.
func (p mpProfileOf) title() string {
	switch {
	case p.own != nil:
		return p.own.Name
	case p.edit.Name != "":
		return p.edit.Name
	}
	return p.name
}

// machinePane is one machine's pane.
type machinePane struct {
	w  *Window
	id string
	// machine is the machine it is about, and draft says it is a
	// server not saved yet; both are read off its pane in each state.
	machine machines.ID
	draft   bool
	list    *widget.List
	right   *widget.Pages
	root    gunim.Node
	items   []mpItem
	sel     string
	// shown is the key and state the right side was built for, so a
	// state that changes nothing there leaves what is typed alone.
	shown   string
	shownOf string
	// added is the name of a profile just added, and addedDir the path
	// of a folder just saved, to pick once it comes.
	added, addedDir string
	// wantSel is a row to pick once it comes, as a tunnel just saved.
	wantSel string
	// rowAt counts the rows the program asked for, as the pane last
	// said; logSeen is the end of the log when it was last looked at.
	rowAt int
	// tunnelNow says what the tunnel picked is doing. It and
	// overviewLine are the shown page's: each page makes its own, as a
	// node is in one place only and the page going stays as it fades.
	tunnelNow *widget.Label
	// logView is the log page's view of the log, while it shows, and
	// logShown the log's end as it was last given.
	logView  *widget.LogView
	logShown int64
	// card is what the machine's card says of it, for the switch at the
	// bottom of the sidebar, and overviewLine the line at the top of
	// the machine's own page, kept up to date in place.
	card cardInfo
	// conn is the machine's status switch, at the bottom of the
	// sidebar in foot, which folds away on this computer and a draft.
	conn         *connSwitch
	foot         *widget.Fold
	overviewLine *widget.Label
	st           app.State
}

func newMachinePane(w *Window, id string) *machinePane {
	mp := &machinePane{w: w, id: id, list: widget.NewList(), right: widget.NewPages(&emptyNode{}), sel: mpMachine}
	mp.list.ClickOnce = true
	// The profiles are dragged into the order every list of them shows,
	// among themselves alone.
	mp.list.ReorderGroup = func(k widget.Key) string {
		if strings.HasPrefix(string(k), mpProfile) {
			return mpProfiles
		}
		return ""
	}
	mp.list.OnReorder = func(keys []widget.Key, _ *gunim.UI) gunim.Intent {
		var ids []string
		for _, k := range keys {
			if id, ok := strings.CutPrefix(string(k), mpProfile); ok {
				ids = append(ids, id)
			}
		}
		return app.ReorderProfiles{Machine: mp.machine, IDs: ids}
	}
	mp.list.OnActivate = func(k widget.Key, u *gunim.UI) gunim.Intent {
		if it, ok := mp.item(string(k)); ok && !it.heading {
			mp.pick(string(k), u)
		}
		return nil
	}
	list := widget.NewPad(widget.NewScroll(mp.list))
	list.Padding = mpSidePad
	mp.conn = newConnSwitch(w, func() (machines.ID, *cardInfo) { return mp.machine, &mp.card }, true)
	foot := widget.NewPad(mp.conn.sw)
	foot.Padding = mpFootPad
	mp.foot = widget.NewFold(foot, false)
	side := widget.Column(list, mp.foot).Grow(list, 1)
	side.Cross = widget.CrossStretch
	mp.overviewLine, mp.tunnelNow = faintLabel(""), widget.NewLabel("")
	// Each page scrolls by itself, but the log, which is all the room.
	rightPad := widget.NewPad(mp.right)
	rightPad.Padding = mpPad
	row := widget.Row(widget.NewSized(side, 280, 0), &divider{width: 1}, rightPad).Grow(rightPad, 1)
	row.Cross = widget.CrossStretch
	mp.root = row
	return mp
}

// Children implements [gunim.Composite].
func (mp *machinePane) Children() []gunim.Node { return []gunim.Node{mp.root} }

// Layout implements [gunim.Node]: the whole pane.
func (mp *machinePane) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	k := kids.At(0)
	k.Layout(gunim.Constraints{Min: c.Max, Max: c.Max})
	k.Place(geom.Pt(0, 0))
	return c.Max
}

// Paint implements [gunim.Node].
func (mp *machinePane) Paint(p *paint.Painter, _ gunim.Frame, _ geom.Size, kids gunim.Children) {
	kids.At(0).Paint(p)
}

// item is the sidebar's row with key k.
func (mp *machinePane) item(k string) (mpItem, bool) {
	i := slices.IndexFunc(mp.items, func(it mpItem) bool { return it.key == k })
	if i < 0 {
		return mpItem{}, false
	}
	return mp.items[i], true
}

// pick shows row k's settings on the right.
func (mp *machinePane) pick(k string, u *gunim.UI) {
	mp.sel, mp.wantSel = k, ""
	mp.refresh(u)
}

// show takes the state: its pane's machine, the rows for it, and the
// right side, built again only when what it shows has changed.
func (mp *machinePane) show(st app.State, u *gunim.UI) {
	mp.st = st
	for _, p := range st.Panes {
		if p.ID == mp.id {
			if mp.draft && !p.Draft {
				// Saved: the server's own row stays picked.
				mp.shown = ""
			}
			mp.machine, mp.draft = p.Machine, p.Draft
			if p.RowAt != mp.rowAt && p.Row != "" {
				// Shown once: the program forgets the row, so the pane
				// moved to another window does not show it again.
				mp.sel, mp.shown, mp.wantSel = p.Row, "", ""
				u.Send(mp.list, app.MachineRowShown{Pane: mp.id})
			}
			mp.rowAt = p.RowAt
		}
	}
	remote := mp.machine != machines.Local && !mp.draft
	if remote {
		mp.card = mp.w.cardFor(mp.machine, mp.w.nameOf(mp.machine))
		mp.conn.sync(time.Now(), u)
	}
	mp.foot.SetOpen(remote, u)
	mp.refresh(u)
}

// refresh lists the rows and builds the right side for the one picked.
func (mp *machinePane) refresh(u *gunim.UI) {
	mp.items = mp.rows()
	for _, it := range mp.items {
		switch {
		case mp.added != "" && it.profile.own != nil && it.profile.own.Name == mp.added:
			mp.sel, mp.added = it.key, ""
		case mp.addedDir != "" && it.folder.fav != nil && it.folder.fav.Path == mp.addedDir:
			mp.sel, mp.addedDir, mp.shown = it.key, "", ""
		case mp.wantSel != "" && it.key == mp.wantSel:
			mp.sel, mp.wantSel, mp.shown = it.key, "", ""
		}
	}
	adding := (mp.sel == mpAddTunnel || mp.sel == mpAddSOCKS) && mp.machine != machines.Local && !mp.draft
	if _, ok := mp.item(mp.sel); !ok && !adding {
		mp.sel = mpMachine
	}
	if mp.sel == mpLog {
		// New lines grow in at the bottom of the page shown.
		mp.showLog(u)
	} else {
		mp.logView = nil
	}
	if mp.machine != machines.Local {
		mp.overviewLine.Text = mp.overview(time.Now())
	}
	// What a tunnel is doing changes as bytes go through: said in
	// place, so its page is not built again each second.
	if it, ok := mp.item(mp.sel); ok && it.tunnel != nil {
		mp.tunnelNow.Text = it.tunnel.now()
	}
	// The list draws the picked row's fill, gliding to the next picked.
	picked := widget.Key(mp.sel)
	if adding {
		picked = ""
	}
	mp.list.SetSelected(picked, u)
	widget.Sync(mp.list, u, mp.items, func(it mpItem) widget.Key { return widget.Key(it.key) },
		func(it mpItem) *mpRow { r := newMPRow(mp); r.set(it, mp.sel == it.key, nil); return r },
		func(r *mpRow, it mpItem, u *gunim.UI) { r.set(it, mp.sel == it.key, u) })
	of := mp.sel + "\x00" + mp.signature()
	if of == mp.shownOf && mp.shown == mp.sel {
		return
	}
	// The page comes from the side its row lies on, from the one shown
	// before; the same row's page built again fades in where it is.
	from := geom.Point{}
	if was, now := mp.rowIndex(mp.shown), mp.rowIndex(mp.sel); mp.shown != mp.sel && was >= 0 && now >= 0 {
		from.Y = 1
		if now < was {
			from.Y = -1
		}
	}
	mp.shown, mp.shownOf = mp.sel, of
	var page gunim.Node
	it, _ := mp.item(mp.sel)
	switch {
	case mp.sel == mpMachine:
		page = mp.machineSide(u)
	case mp.sel == mpLog:
		page = mp.logSide(u)
	case adding:
		page = mp.addTunnelSide(mp.sel == mpAddSOCKS)
	case it.tunnel != nil:
		page = mp.tunnelSide(*it.tunnel)
	case mp.sel == mpAdd:
		page = mp.profileSide(mpProfileOf{}, true, u)
	case mp.sel == mpAddDir:
		page = mp.folderSide(mpFolderOf{}, true)
	case strings.HasPrefix(mp.sel, mpFolder):
		page = mp.folderSide(it.folder, false)
	case it.saved != nil:
		page = mp.savedSide(*it.saved)
	default:
		page = mp.profileSide(it.profile, false, u)
	}
	if mp.sel != mpLog {
		page = widget.NewScroll(page)
	}
	mp.right.Show(page, from, u)
}

// rowIndex is where row k stands in the sidebar, or -1; a + row
// stands under its heading.
func (mp *machinePane) rowIndex(k string) int {
	switch k {
	case mpAddTunnel:
		k = mpTunnels
	case mpAddSOCKS:
		k = mpSOCKS
	}
	return slices.IndexFunc(mp.items, func(it mpItem) bool { return it.key == k })
}

// signature changes when what the right side shows changes: the
// machine as saved, and the picked row's profile.
func (mp *machinePane) signature() string {
	var b strings.Builder
	switch mp.sel {
	case mpLog:
		// The log's page comes alive as the first line arrives.
		b.WriteString(boolWord(mp.st.Logs[mp.machine] != nil))
	case mpAddTunnel, mpAddSOCKS:
		// Open waits on the connection.
		b.WriteString(boolWord(mp.card.state == cardConnected))
	}
	if h, ok := mp.host(); ok && mp.sel == mpMachine {
		fmt.Fprintf(&b, "%+v", h)
		b.WriteString(boolWord(slices.Contains(mp.st.NoDisconnectAsk, mp.machine)))
	}
	if it, ok := mp.item(mp.sel); ok {
		p := it.profile
		b.WriteString("\x01" + p.title() + "\x00" + p.line)
		if p.own != nil {
			b.WriteString(strings.Join(p.own.Command, " ") + p.own.Folder + strings.Join(p.own.Env, ";"))
			if p.own.Setup != nil {
				b.WriteString(boolWord(*p.own.Setup))
			}
		}
		b.WriteString(p.edit.Folder + strings.Join(p.edit.Env, ";") + boolWord(p.def) + boolWord(p.edit.Hidden))
		if p.edit.Setup != nil {
			b.WriteString(boolWord(*p.edit.Setup))
		}
		if t := it.tunnel; t != nil {
			b.WriteString("\x04" + t.title() + boolWord(t.live()) + boolWord(t.open != nil) + boolWord(t.saved != nil))
		}
		if s := it.saved; s != nil && s.copy != nil {
			b.WriteString("\x03" + app.CopiedWhere(*s.copy, mp.w.nameOf))
		}
		f := it.folder
		b.WriteString("\x02" + f.title() + "\x00" + f.path + boolWord(f.hidden()))
		if f.fav != nil {
			b.WriteString(f.fav.Path + "\x00" + f.fav.Name)
		}
	}
	return b.String()
}

func boolWord(b bool) string {
	if b {
		return "y"
	}
	return "n"
}

// host is the saved server the pane is about, and whether it is one.
func (mp *machinePane) host() (remote.Host, bool) {
	if mp.draft || mp.machine == "" {
		return remote.Host{}, false
	}
	i := slices.IndexFunc(mp.st.Saved, func(h remote.Host) bool { return machines.ID(h.ID) == mp.machine })
	if i < 0 {
		return remote.Host{}, false
	}
	return mp.st.Saved[i], true
}

// rows are the sidebar's rows: the machine, then its profiles under
// their heading, the default first, then the row that adds one. A
// draft and a kakel window have no profiles here.
func (mp *machinePane) rows() []mpItem {
	st := mp.st
	head := mpItem{key: mpMachine, title: "This computer", icon: icon.Laptop}
	h, saved := mp.host()
	switch {
	case mp.draft:
		head.title, head.icon = "New server", icon.Server
		return []mpItem{head}
	case saved:
		head.title, head.icon = h.Name, icon.Server
		if h.Window {
			head.icon = icon.AppWindow
			// A kakel window's terminals are its own; its files are
			// offered here, and tunnels go through it.
			out := append([]mpItem{head, mp.logRow()}, mp.folderRows()...)
			return append(append(out, mp.tunnelRows(false)...), mp.tunnelRows(true)...)
		}
	case mp.machine != "":
		// Connected to and not saved: its connection only.
		head.title, head.icon = mp.w.nameOf(mp.machine), icon.Server
		out := []mpItem{head, mp.logRow()}
		return append(append(out, mp.tunnelRows(false)...), mp.tunnelRows(true)...)
	}
	var profiles []mpProfileOf
	if mp.machine == "" {
		def := st.DefaultShell
		for _, sh := range st.FoundShells {
			profiles = append(profiles, mpProfileOf{id: sh.ID, name: sh.Title, line: sh.Line, found: true,
				edit: foundOf(st.Found, "", sh.ID), def: sh.ID == def})
		}
		for i, p := range st.Profiles {
			if p.HostID == "" {
				profiles = append(profiles, mpProfileOf{id: p.ID, own: &st.Profiles[i], line: app.ProfileLine(p), def: p.ID == def})
			}
		}
	} else {
		def := st.ServerShells[string(mp.machine)]
		login := foundOf(st.Found, mp.machine, app.LoginShell)
		if def == "" && login.Hidden {
			// Its own shell hidden, New Terminal runs its first profile.
			if i := slices.IndexFunc(st.Profiles, func(p settings.Profile) bool { return machines.ID(p.HostID) == mp.machine }); i >= 0 {
				def = st.Profiles[i].ID
			}
		}
		profiles = append(profiles, mpProfileOf{id: app.LoginShell, name: "Login Shell", line: "the account's own shell", found: true,
			edit: login, def: def == ""})
		for i, p := range st.Profiles {
			if machines.ID(p.HostID) == mp.machine {
				profiles = append(profiles, mpProfileOf{id: p.ID, own: &st.Profiles[i], line: app.ProfileLine(p), def: p.ID == def})
			}
		}
	}
	// In the order the user dragged them into: the star says which is
	// the default, wherever it stands.
	profiles = app.InOrder(profiles, func(p mpProfileOf) string { return p.id }, st.ProfileOrder[string(mp.machine)])
	out := []mpItem{head}
	if mp.machine != machines.Local {
		out = append(out, mp.logRow())
	}
	out = append(out, mpItem{key: mpProfiles, title: "Profiles", heading: true})
	for _, p := range profiles {
		out = append(out, mpItem{key: mpProfile + p.id, title: p.title(), icon: icon.SquareTerminal, profile: p})
	}
	out = append(out, mpItem{key: mpAdd, title: "Add Profile", icon: icon.Plus})
	out = append(out, mp.folderRows()...)
	if mp.machine != machines.Local {
		out = append(append(out, mp.tunnelRows(false)...), mp.tunnelRows(true)...)
	}
	return append(out, mp.savedRows()...)
}

// logRow is the row of the machine's connection log.
func (mp *machinePane) logRow() mpItem {
	return mpItem{key: mpLog, title: "Connection log", icon: icon.ScrollText}
}

// savedRows are what the machine keeps for next time, each kind under
// its heading when it has any: the commands run on it, and the copies
// to or from it. Its tunnels have their own sections.
func (mp *machinePane) savedRows() []mpItem {
	st, m := mp.st, mp.machine
	h, _ := mp.host()
	// on reports whether what was kept on the machine with id and name,
	// reached through window, is this one's: by its ID, or by its name
	// when it was kept before servers had IDs. One beyond a kakel window
	// is kept under the window's ID, and is that machine's, not the
	// window's.
	on := func(id, name, window string) bool {
		if m == "" {
			return id == "" && name == "" && window == ""
		}
		if window != "" || strings.Contains(name, app.KeptFarSep) {
			return false
		}
		return machines.ID(id) == m || id == "" && name != "" && name == h.Name
	}
	var cmds, copies []mpItem
	for i, c := range st.SavedCommands {
		if on(c.HostID, c.Host, "") {
			cmds = append(cmds, mpItem{key: mpSaved + fmt.Sprintf("cmd:%#v", c), title: c.Line, icon: icon.SquareChevronRight,
				saved: &mpSavedOf{command: &st.SavedCommands[i]}})
		}
	}
	for i, c := range st.SavedCopies {
		if on(c.FromID, c.From, c.FromWindow) || on(c.ToID, c.To, c.ToWindow) {
			copies = append(copies, mpItem{key: mpSaved + fmt.Sprintf("copy:%#v", c), title: "Copy " + app.CopiedWhat(c), icon: icon.Copy,
				saved: &mpSavedOf{copy: &st.SavedCopies[i]}})
		}
	}
	var out []mpItem
	for _, sec := range []struct {
		title string
		rows  []mpItem
	}{{"Commands", cmds}, {"Copies", copies}} {
		if len(sec.rows) > 0 {
			out = append(out, mpItem{key: "head:" + sec.title, title: sec.title, heading: true})
			out = append(out, sec.rows...)
		}
	}
	return out
}

// tunnelTitle is what a saved tunnel's row says: where it goes.
func tunnelTitle(t settings.SavedTunnel) string {
	if t.Target == "" {
		return "SOCKS proxy on " + t.Listen
	}
	return t.Listen + " → " + t.Target
}

// savedSide is the right side for something kept: what it is, and the
// buttons that run it again or take it off the list.
func (mp *machinePane) savedSide(s mpSavedOf) gunim.Node {
	form := widget.NewForm()
	run, remove := widget.NewButton("Run"), widget.NewButton("Remove…")
	run.Kind = widget.ButtonPrimary
	var title, note string
	switch {
	case s.command != nil:
		c := *s.command
		title, note = c.Line, "A command kept from Run Command. It runs in a pane of its own."
		form.Add("Command line", widget.NewLabel(c.Line))
		if c.Dir != "" {
			form.Add("Folder", widget.NewLabel(c.Dir))
		}
		run.OnClick = func(*gunim.UI) gunim.Intent { return app.RunSavedCommand{Saved: c} }
	case s.copy != nil:
		c := *s.copy
		title, note = "Copy "+app.CopiedWhat(c), "A copy kept from the file manager, to run again."
		form.Add("Files", widget.NewLabel(strings.Join(c.Names, ", ")))
		form.Add("From and to", widget.NewLabel(app.CopiedWhere(c, mp.w.nameOf)))
		run.OnClick = func(*gunim.UI) gunim.Intent { return app.RunSavedCopy{Saved: c} }
	case s.tunnel != nil:
		t := *s.tunnel
		title, note = tunnelTitle(t), "A tunnel kept, to open again."
		run.Label = "Open"
		kind := map[string]string{"local": "A port here, reaching through " + mp.w.nameOf(mp.machine),
			"remote": "A port on " + mp.w.nameOf(mp.machine) + ", reaching back here",
			"socks":  "A SOCKS proxy here, through " + mp.w.nameOf(mp.machine)}[t.Kind]
		if kind == "" {
			kind = t.Kind
		}
		form.Add("Kind", widget.NewLabel(kind))
		form.Add("Listens on", widget.NewLabel(t.Listen))
		if t.Target != "" {
			form.Add("Reaches", widget.NewLabel(t.Target))
		}
		run.OnClick = func(*gunim.UI) gunim.Intent { return app.OpenSavedTunnel{Saved: t} }
	}
	remove.OnClick = func(u *gunim.UI) gunim.Intent { mp.confirmForget(s, u); return nil }
	col := widget.Column(heading(title), faintLabel(note), widget.NewCard(form), buttons(run, remove))
	col.Cross, col.Gap = widget.CrossStretch, mpGap
	return widget.NewSized(col, 640, 0)
}

// folderRows are the Files section's rows: the home folder and, here,
// the drives, which kakel finds, then the favourites saved on the
// machine, then the row that adds one.
func (mp *machinePane) folderRows() []mpItem {
	st := mp.st
	folder := func(f mpFolderOf) mpItem {
		key := mpFolder + "found:" + f.id
		if f.fav != nil {
			key = mpFolder + "fav:" + f.fav.Path
		}
		ic := icon.Folder
		switch {
		case f.id == app.FoundHome:
			ic = icon.House
		case strings.HasPrefix(f.id, "drive:"):
			ic = icon.HardDrive
		}
		return mpItem{key: key, title: f.title(), icon: ic, folder: f}
	}
	out := []mpItem{{key: mpFiles, title: "Files", heading: true},
		folder(mpFolderOf{id: app.FoundHome, name: "Home", edit: foundOf(st.Found, mp.machine, app.FoundHome)})}
	if mp.machine == "" {
		for _, d := range st.Drives {
			out = append(out, folder(mpFolderOf{id: d.ID, name: d.Name, path: d.Path, edit: foundOf(st.Found, "", d.ID)}))
		}
	}
	for i, f := range st.Favourites {
		if f.Machine == mp.machine {
			out = append(out, folder(mpFolderOf{fav: &st.Favourites[i]}))
		}
	}
	return append(out, mpItem{key: mpAddDir, title: "Add Folder", icon: icon.FolderPlus})
}

// machineSide is the right side for the machine's own row.
func (mp *machinePane) machineSide(u *gunim.UI) gunim.Node {
	// A line of this page's own: the page before still shows its line
	// as it fades.
	mp.overviewLine = faintLabel(mp.overviewLine.Text)
	h, saved := mp.host()
	if !mp.draft && !saved && mp.machine != machines.Local {
		// Connected to by its address, and not saved: its connection.
		col := widget.Column(heading(mp.w.nameOf(mp.machine)), faintLabel(mp.card.sub), mp.overviewLine)
		col.Cross, col.Gap = widget.CrossStretch, mpGap
		return widget.NewSized(col, 640, 0)
	}
	if !mp.draft && !saved {
		col := widget.Column(heading("This computer"), faintLabel(localWho()),
			faintLabel("Its terminals are its profiles, on the left: the star marks the one a new terminal opens."))
		col.Cross, col.Gap = widget.CrossStretch, mpGap
		return widget.NewSized(col, 640, 0)
	}
	var old *remote.Host
	if saved {
		old = &h
	}
	f := newServerFields(mp.w, old)
	save, connect := widget.NewButton("Save"), widget.NewButton("Save and Connect")
	save.Kind = widget.ButtonPrimary
	problem := faintLabel("")
	check := func() {
		_, why := f.host()
		problem.Text = why
		save.Disabled, connect.Disabled = why != "", why != ""
		if saved && why == "" {
			now, _ := f.host()
			save.Disabled = reflect.DeepEqual(now, h)
		}
	}
	f.changed = func(u *gunim.UI) {
		check()
		if mp.draft {
			u.Send(mp.list, app.DraftTyped{Pane: mp.id, Typed: f.typed()})
		}
		u.Invalidate()
	}
	check()
	send := func(connectToo bool) func(u *gunim.UI) gunim.Intent {
		return func(u *gunim.UI) gunim.Intent {
			now, why := f.host()
			if why != "" {
				return nil
			}
			pane := ""
			if mp.draft {
				pane = mp.id
			}
			return app.SaveServer{Host: now, Under: f.under, Shell: mp.st.ServerShells[now.ID], Pane: pane, Connect: connectToo}
		}
	}
	save.OnClick, connect.OnClick = send(false), send(true)
	actions := []gunim.Node{save, connect}
	title := "New server"
	parts := []gunim.Node{heading(title)}
	if saved {
		// Connecting is the switch at the bottom left.
		actions = actions[:1]
		parts = []gunim.Node{heading(h.Name), mp.overviewLine}
	}
	parts = append(parts, widget.NewCard(f.form), problem, buttons(actions...))
	if saved {
		// Saved at once, as the question's own tick is.
		ask := widget.NewCheckbox("Ask before disconnecting while something is open")
		ask.SetChecked(!slices.Contains(mp.st.NoDisconnectAsk, mp.machine), nil)
		ask.OnChange = func(on bool, _ *gunim.UI) gunim.Intent {
			return app.SetDisconnectAsk{Machine: mp.machine, Ask: on}
		}
		what := "server"
		if h.Window {
			what = "window"
		}
		remove := widget.NewButton("Remove…")
		remove.Kind = widget.ButtonDanger
		remove.OnClick = func(u *gunim.UI) gunim.Intent { mp.w.confirmRemove(mp.machine, u); return nil }
		gone := widget.Column(faintLabel("Removing this "+what+" removes it from the list, with its profiles and favourites."), buttons(remove))
		gone.Cross, gone.Gap = widget.CrossStretch, mpGap
		parts = append(parts, widget.NewCard(ask), sectionHeading("Remove"), widget.NewCard(gone))
	}
	col := widget.Column(parts...)
	col.Cross, col.Gap = widget.CrossStretch, mpGap
	return widget.NewSized(col, 640, 0)
}

// profileSide is the right side for a profile's row, or for adding one
// with adding.
func (mp *machinePane) profileSide(p mpProfileOf, adding bool, u *gunim.UI) gunim.Node {
	here := mp.machine == ""
	name, line, dir := widget.NewTextField(), widget.NewTextField(), widget.NewTextField()
	env := widget.NewTextArea()
	env.Rows = 3
	env.Placeholder = "NAME=value, one a line"
	dir.Placeholder = "Optional"
	line.Placeholder = "such as /usr/bin/fish -l"
	if runtime.GOOS == "windows" {
		line.Placeholder = `such as C:\msys64\usr\bin\bash.exe --login -i`
	}
	if !here {
		line.Placeholder, dir.Placeholder = "such as tmux new -A -s main", "Optional, such as ~/src"
	}
	teach := widget.NewCheckbox("Let kakel follow the shell's folder and commands")
	setup := mp.setupOn()
	switch {
	case p.own != nil:
		name.SetText(p.own.Name, nil)
		line.SetText(app.ProfileLine(*p.own), nil)
		dir.SetText(p.own.Folder, nil)
		env.SetText(strings.Join(p.own.Env, "\n"), nil)
		if p.own.Setup != nil {
			setup = *p.own.Setup
		}
	case p.found:
		name.SetText(p.edit.Name, nil)
		name.Placeholder = p.name
		dir.SetText(p.edit.Folder, nil)
		env.SetText(strings.Join(p.edit.Env, "\n"), nil)
		if p.edit.Setup != nil {
			setup = *p.edit.Setup
		}
	}
	teach.SetChecked(setup, nil)
	form := widget.NewForm().Add("Name", name)
	switch {
	case p.found:
		form.Add("Runs", widget.NewLabel(p.line))
	default:
		form.Add("Command line", line)
	}
	// A server's own shell takes no folder or variables: they would
	// have to go through a command, which it is not.
	if !(p.found && !here) {
		form.Add("Folder", dir).Add("Environment", env)
	}
	form.Add("", teach)
	save := widget.NewButton("Save")
	save.Kind = widget.ButtonPrimary
	problem := faintLabel("")
	setupOf := func() *bool {
		on := teach.Checked()
		if on == mp.setupOn() && (p.own == nil || p.own.Setup == nil) && (!p.found || p.edit.Setup == nil) {
			return nil
		}
		return &on
	}
	parse := func() (gunim.Intent, error) {
		switch {
		case p.found:
			f := p.edit
			f.HostID, f.ID = string(mp.machine), p.id
			f.Name, f.Setup = strings.TrimSpace(name.Text()), setupOf()
			if here {
				f.Folder = strings.TrimSpace(dir.Text())
				if f.Folder != "" {
					if _, err := app.FolderHere(f.Folder); err != nil {
						return nil, err
					}
				}
				vars, err := app.ParseEnv(env.Text())
				if err != nil {
					return nil, err
				}
				f.Env = vars
			}
			return app.SaveFound{Found: f}, nil
		default:
			id := ""
			if p.own != nil {
				id = p.id
			}
			got, err := app.ParseProfile(mp.st.Profiles, id, string(mp.machine), name.Text(), line.Text(), dir.Text(), env.Text())
			if err != nil {
				return nil, err
			}
			got.Setup = setupOf()
			return app.SaveProfile{Profile: got}, nil
		}
	}
	was, _ := parse()
	check := func(u *gunim.UI) {
		now, err := parse()
		problem.Text = ""
		save.Disabled = err != nil || !adding && reflect.DeepEqual(now, was)
		if err != nil {
			problem.Text = words.UpperFirst(err.Error())
		}
		if u != nil {
			u.Invalidate()
		}
	}
	changed := func(string, *gunim.UI) gunim.Intent { check(u); return nil }
	name.OnChange, line.OnChange, dir.OnChange = changed, changed, changed
	env.OnChange = changed
	teach.OnChange = func(bool, *gunim.UI) gunim.Intent { check(u); return nil }
	check(nil)
	save.OnClick = func(u *gunim.UI) gunim.Intent {
		in, err := parse()
		if err != nil {
			return nil
		}
		if adding {
			mp.added = strings.TrimSpace(name.Text())
		}
		return in
	}
	actions := []gunim.Node{save}
	title := p.title()
	switch {
	case adding:
		title = "Add Profile"
	case p.own != nil:
		del := widget.NewButton("Delete…")
		del.OnClick = func(u *gunim.UI) gunim.Intent { mp.confirmDelete(*p.own, u); return nil }
		actions = append(actions, del)
	}
	if !adding && !p.def && !p.edit.Hidden {
		def := widget.NewButton("Make Default")
		def.OnClick = func(*gunim.UI) gunim.Intent { return app.SetDefaultProfile{Machine: mp.machine, ID: p.id} }
		actions = append(actions, def)
	}
	note := "A profile you made."
	switch {
	case adding:
		note = "A shell, or a program such as tmux, that a terminal on " + mp.w.machineName(mp.machine) + " can open."
	case p.found && p.edit.Hidden:
		note = "Found by kakel, and hidden: the eye beside it in the list shows it again."
	case p.found:
		note = "Found by kakel. Rename it, or hide it with the eye beside it in the list."
	}
	if p.def {
		note += " New terminals open it."
	}
	col := widget.Column(heading(title), faintLabel(note), widget.NewCard(form), problem, buttons(actions...))
	col.Cross, col.Gap = widget.CrossStretch, mpGap
	return widget.NewSized(col, 640, 0)
}

// folderSide is the right side for a folder's row, or for saving one
// with adding: a favourite's name and folder, or a found one's name.
func (mp *machinePane) folderSide(f mpFolderOf, adding bool) gunim.Node {
	name, dir := widget.NewTextField(), widget.NewTextField()
	name.Placeholder, dir.Placeholder = "The folder's name", "such as /srv/www"
	if mp.machine == "" && runtime.GOOS == "windows" {
		dir.Placeholder = `such as D:\Projects`
	}
	form := widget.NewForm().Add("Name", name)
	switch {
	case f.fav != nil:
		name.SetText(f.fav.Name, nil)
		dir.SetText(f.fav.Path, nil)
		form.Add("Folder", dir)
	case f.found():
		name.SetText(f.edit.Name, nil)
		name.Placeholder = f.name
		where := f.path
		if where == "" {
			where = "the account's home folder"
		}
		form.Add("Opens", widget.NewLabel(where))
	default:
		form.Add("Folder", dir)
	}
	save := widget.NewButton("Save")
	save.Kind = widget.ButtonPrimary
	problem := faintLabel("")
	parse := func() (gunim.Intent, string) {
		if f.found() {
			e := f.edit
			e.HostID, e.ID, e.Name = string(mp.machine), f.id, strings.TrimSpace(name.Text())
			return app.SaveFound{Found: e}, ""
		}
		// Whether the folder is there is asked as it is saved, off the
		// window's goroutine: a drive can be slow to answer.
		path := strings.TrimSpace(dir.Text())
		if path == "" {
			return nil, "Enter a folder."
		}
		was := ""
		if f.fav != nil {
			was = f.fav.Path
		}
		return app.SaveFavourite{Machine: mp.machine, Was: was, Path: path, Name: strings.TrimSpace(name.Text())}, ""
	}
	before, _ := parse()
	check := func(u *gunim.UI) {
		now, why := parse()
		problem.Text = why
		save.Disabled = why != "" || !adding && reflect.DeepEqual(now, before)
		if u != nil {
			u.Invalidate()
		}
	}
	changed := func(_ string, u *gunim.UI) gunim.Intent { check(u); return nil }
	name.OnChange, dir.OnChange = changed, changed
	check(nil)
	save.OnClick = func(*gunim.UI) gunim.Intent {
		in, why := parse()
		if why != "" {
			return nil
		}
		if fav, ok := in.(app.SaveFavourite); ok {
			// The row to pick once it is saved: where it is kept, as the
			// program writes it.
			mp.addedDir = savedPath(mp.machine, fav.Path)
		}
		return in
	}
	actions := []gunim.Node{save}
	if !adding && !f.hidden() {
		open := widget.NewButton("Open")
		path := f.path
		if f.fav != nil {
			path = f.fav.Path
		}
		open.OnClick = func(*gunim.UI) gunim.Intent { return app.OpenFilesOn{Machine: mp.machine, Path: path} }
		actions = append(actions, open)
	}
	title, note := f.title(), "A folder you saved: it is a favourite in the file manager's sidebar too."
	switch {
	case adding:
		title, note = "Add Folder", "A folder on "+mp.w.machineName(mp.machine)+" to keep at hand: it is a favourite in the file manager's sidebar too."
	case f.hidden():
		note = "Found by kakel, and hidden: the eye beside it in the list shows it again."
	case f.found():
		note = "Found by kakel. Rename it, or hide it with the eye beside it in the list."
	}
	if f.fav != nil {
		del := widget.NewButton("Remove…")
		del.OnClick = func(u *gunim.UI) gunim.Intent { mp.confirmUnsave(*f.fav, u); return nil }
		actions = append(actions, del)
	}
	col := widget.Column(heading(title), faintLabel(note), widget.NewCard(form), problem, buttons(actions...))
	col.Cross, col.Gap = widget.CrossStretch, mpGap
	return widget.NewSized(col, 640, 0)
}

// savedPath is path as a favourite on machine m is kept: here in full,
// ~ the home folder; on a server without a slash at its end.
func savedPath(m machines.ID, path string) string {
	if m != "" {
		if len(path) > 1 {
			return strings.TrimRight(path, "/")
		}
		return path
	}
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, path[1:])
		}
	}
	if full, err := filepath.Abs(path); err == nil {
		return full
	}
	return path
}

// confirmForget asks before taking s off the list kept for next time.
func (mp *machinePane) confirmForget(s mpSavedOf, u *gunim.UI) {
	var title string
	var forget gunim.Intent
	switch {
	case s.command != nil:
		title, forget = "the command "+shortened(s.command.Line, 50), app.ForgetCommand{Saved: *s.command}
	case s.copy != nil:
		title, forget = "the copy of "+shortened(app.CopiedWhat(*s.copy), 50), app.ForgetCopy{Saved: *s.copy}
	case s.tunnel != nil && s.tunnel.Target == "":
		title, forget = "the SOCKS proxy on "+s.tunnel.Listen, app.ForgetTunnel{Saved: *s.tunnel}
	case s.tunnel != nil:
		title, forget = "the tunnel "+tunnelTitle(*s.tunnel), app.ForgetTunnel{Saved: *s.tunnel}
	default:
		return
	}
	d := widget.NewDialog("Remove " + title + "?")
	d.Body = widget.NewLabel("It is removed from the saved list.")
	d.SetButtons("Remove", "Cancel")
	d.Danger = true
	d.OnAccept = widget.Sends(forget)
	d.OnDismiss = widget.Sends(app.DialogClosed{})
	mp.w.openDialog(d, u)
}

// confirmUnsave asks before taking the favourite f off the list.
func (mp *machinePane) confirmUnsave(f app.Favourite, u *gunim.UI) {
	d := widget.NewDialog("Remove " + f.Label() + "?")
	d.Body = widget.NewLabel("It comes off the favourites. The folder and what is in it stay where they are.")
	d.SetButtons("Remove", "Cancel")
	d.Danger = true
	d.OnAccept = widget.Sends(app.RemoveFavourite{Machine: f.Machine, Path: f.Path})
	d.OnDismiss = widget.Sends(app.DialogClosed{})
	mp.w.openDialog(d, u)
}

// confirmDelete asks before deleting the profile p.
func (mp *machinePane) confirmDelete(p settings.Profile, u *gunim.UI) {
	d := widget.NewDialog("Delete " + p.Name + "?")
	d.Body = widget.NewLabel("Terminals open with it stay open. New terminals can no longer open it.")
	d.SetButtons("Delete", "Cancel")
	d.Danger = true
	d.OnAccept = widget.Sends(app.RemoveProfile{ID: p.ID})
	d.OnDismiss = widget.Sends(app.DialogClosed{})
	mp.w.openDialog(d, u)
}

// sectionHeading is the title of a part of a page.
func sectionHeading(s string) *widget.Label {
	l := widget.NewLabel(s)
	l.Face, l.Color = widget.BoldFont, look.Faint
	return l
}

// heading is a page's title.
func heading(s string) *widget.Label {
	l := widget.NewLabel(s)
	l.Face, l.Size = widget.BoldFont, widget.DialogTitleSize
	return l
}

// machineName is what m is called in a sentence: "this computer", or
// its name.
func (w *Window) machineName(m machines.ID) string {
	if m == "" {
		return "this computer"
	}
	return w.nameOf(m)
}

// setupOn is whether a shell on the pane's machine is taught when its
// profile does not say: Shell Setup here, and the server's own tick on
// a server.
func (mp *machinePane) setupOn() bool {
	if mp.machine == "" {
		return mp.st.ShellSetup
	}
	h, _ := mp.host()
	return h.Setup
}

// mpRow is a row of a machine pane's sidebar: its icon and name, and
// for a profile, its star, eye and bin.
type mpRow struct {
	mp    *machinePane
	inner *widget.Flex
	icon  *widget.Icon
	title *widget.Label
	star  *widget.IconButton
	eye   *widget.IconButton
	bin   *widget.IconButton
	// sub is who and where the machine is, under its name on the
	// machine's own row.
	sub   *widget.Label
	words *widget.Flex
	// plus is a heading's +, traffic a tunnel's last seconds, from
	// rate and rated, and badge the problems in a log not read yet.
	plus     *widget.IconButton
	traffic  *widget.LiveGraph
	rate     meter.Rate
	rated    *meter.Meter
	badge    *widget.Badge
	heading  bool
	selected bool
	it       mpItem
	// shape says which parts the row holds, built again only when that
	// changes.
	shape string
}

func newMPRow(mp *machinePane) *mpRow {
	r := &mpRow{mp: mp, icon: widget.NewIcon(icon.SquareTerminal, ""), title: widget.NewLabel(""), sub: faintLabel("")}
	r.sub.Size = smallText
	r.words = widget.Column(r.title, r.sub)
	r.star = widget.NewIconButton(icon.Star, "New terminals open it")
	r.eye = widget.NewIconButton(icon.Eye, "Hide it")
	r.bin = widget.NewIconButton(icon.Trash2, "Delete it")
	r.plus = widget.NewIconButton(icon.Plus, "")
	r.plus.OnClick = func(u *gunim.UI) gunim.Intent { r.mp.pick(r.it.plus, u); return nil }
	r.traffic = widget.NewLiveGraph(500*time.Millisecond, 30)
	r.traffic.Compact, r.traffic.Width, r.traffic.Ink = true, 30, widget.ToastSuccessInk
	r.traffic.Sample = func() float64 {
		if r.rated == nil {
			return 0
		}
		r.rate.Sample(r.rated, time.Now())
		past := r.rate.Past()
		if len(past) == 0 {
			return 0
		}
		return float64(past[len(past)-1])
	}
	r.badge = widget.NewBadge("")
	r.badge.Ink = widget.DialogDangerInk
	r.star.OnClick = func(*gunim.UI) gunim.Intent {
		return app.SetDefaultProfile{Machine: r.mp.machine, ID: r.it.profile.id}
	}
	r.eye.OnClick = func(*gunim.UI) gunim.Intent {
		if f := r.it.folder; f.found() {
			return app.HideFound{HostID: string(r.mp.machine), ID: f.id, Show: f.edit.Hidden}
		}
		p := r.it.profile
		return app.HideFound{HostID: string(r.mp.machine), ID: p.id, Show: p.edit.Hidden}
	}
	r.bin.OnClick = func(u *gunim.UI) gunim.Intent {
		if p := r.it.profile.own; p != nil {
			r.mp.confirmDelete(*p, u)
		}
		if f := r.it.folder.fav; f != nil {
			r.mp.confirmUnsave(*f, u)
		}
		if s := r.it.saved; s != nil {
			r.mp.confirmForget(*s, u)
		}
		return nil
	}
	return r
}

// set shows it, picked or not, building the row's parts again: in the
// tree, through u.
func (r *mpRow) set(it mpItem, picked bool, u *gunim.UI) {
	old := r.inner
	r.it, r.selected, r.heading = it, picked, it.heading
	r.title.Text = it.title
	r.title.Face, r.title.Size = widget.Font, widget.TextSize
	r.title.Color = widget.Ink
	r.icon.Color, r.icon.Size = widget.Ink, widget.IconSize
	p := it.profile
	kids := []gunim.Node{}
	switch {
	case it.heading:
		r.title.Face, r.title.Color = widget.BoldFont, look.Faint
		kids = append(kids, r.title)
		if it.plus != "" {
			r.plus.Tooltip = map[string]string{mpAddTunnel: "Add a tunnel", mpAddSOCKS: "Add a SOCKS proxy"}[it.plus]
			kids = append(kids, widget.NewSpacer(), r.plus)
		}
	case it.key == mpMachine:
		// The machine heads its pane: bigger, bold, with who and where
		// it is under its name.
		r.icon.Icon, r.icon.Size = it.icon, mpHeadIcon
		r.title.Face, r.title.Size = widget.BoldFont, mpHeadSize
		r.sub.Text = r.mp.headSub()
		kids = append(kids, r.icon, r.words)
	default:
		r.icon.Icon = it.icon
		kids = append(kids, r.icon, r.title)
	}
	if t := it.tunnel; t != nil {
		// Its icon says how it is: green while it forwards, red once it
		// stopped, faint while it is only saved.
		switch {
		case t.live():
			r.icon.Color = widget.ToastSuccessInk
		case t.open != nil:
			r.icon.Color = widget.DialogDangerInk
		default:
			r.title.Color, r.icon.Color = look.Faint, look.Faint
		}
		if m := meterOf(*t); m != r.rated {
			// Another tunnel's traffic counts from nothing.
			r.rate, r.rated = meter.Rate{}, m
		}
		r.traffic.SetRunning(t.live() && r.rated != nil)
		kids = append(kids, widget.NewSpacer(), r.traffic)
	}
	if it.key == mpLog {
		n := unseenBad(r.mp.st.Logs[r.mp.machine], r.mp.w.logSeen[r.mp.machine])
		r.badge.SetHidden(n == 0, u)
		if n > 0 {
			r.badge.Text = words.ManyOf(n, "problem", "problems")
		}
		kids = append(kids, widget.NewSpacer(), r.badge)
	}
	r.bin.Tooltip = "Delete it"
	if it.saved != nil {
		r.bin.Tooltip = "Take it off the list"
		kids = append(kids, widget.NewSpacer(), r.bin)
	}
	if strings.HasPrefix(it.key, mpFolder) {
		f := it.folder
		kids = append(kids, widget.NewSpacer())
		switch {
		case f.found():
			r.eye.Icon, r.eye.Tooltip = icon.Eye, "Hide it everywhere"
			if f.hidden() {
				r.eye.Icon, r.eye.Tooltip = icon.EyeOff, "Hidden: show it again"
				r.title.Color, r.icon.Color = look.Faint, look.Faint
			}
			kids = append(kids, r.eye)
		default:
			kids = append(kids, r.bin)
		}
	}
	if strings.HasPrefix(it.key, mpProfile) {
		r.star.Icon, r.star.Ink, r.star.Tooltip = icon.Star, look.Faint, "Make it the one new terminals open"
		r.star.Disabled = p.edit.Hidden
		if p.edit.Hidden {
			r.star.Tooltip = "Hidden: show it first to make it the default"
		}
		if p.def {
			r.star.Icon, r.star.Ink, r.star.Tooltip = icon.Star, widget.Accent, "New terminals open it"
		}
		kids = append(kids, widget.NewSpacer(), r.star)
		switch {
		case p.found:
			r.eye.Icon, r.eye.Tooltip = icon.Eye, "Hide it everywhere"
			if p.edit.Hidden {
				r.eye.Icon, r.eye.Tooltip = icon.EyeOff, "Hidden: show it again"
				r.title.Color, r.icon.Color = look.Faint, look.Faint
			}
			kids = append(kids, r.eye)
		case p.own != nil:
			kids = append(kids, r.bin)
		}
	}
	shape := ""
	for _, k := range kids {
		shape += strings.TrimPrefix(fmtType(k), "*widget.")
	}
	if shape == r.shape && r.inner != nil {
		return
	}
	r.shape = shape
	r.inner = widget.Row(kids...)
	r.inner.Cross, r.inner.Gap = widget.CrossCenter, mpRowGap
	if i := slices.IndexFunc(kids, func(n gunim.Node) bool { _, ok := n.(*widget.Spacer); return ok }); i >= 0 {
		r.inner.Grow(kids[i], 1)
	}
	if u != nil && old != nil {
		u.Remove(old)
		u.Insert(r, r.inner)
	}
}

// headSub is what the machine's own row says under its name: who and
// where it is, as its card says.
func (mp *machinePane) headSub() string {
	switch {
	case mp.draft:
		return "Not saved yet"
	case mp.machine == machines.Local:
		return localWho()
	}
	return mp.card.sub
}

// fmtType names n's type, and which of the row's buttons it is.
func fmtType(n gunim.Node) string {
	switch n.(type) {
	case *widget.Label:
		return "L"
	case *widget.Icon:
		return "I"
	case *widget.Spacer:
		return "S"
	case *widget.LiveGraph:
		return "G"
	case *widget.Badge:
		return "B"
	}
	return fmt.Sprintf("%p", n)
}

// Children implements [gunim.Composite].
func (r *mpRow) Children() []gunim.Node { return []gunim.Node{r.inner} }

// Layout implements [gunim.Node]: a row as tall as the sidebar's, its
// heading with room above it.
func (r *mpRow) Layout(c gunim.Constraints, f gunim.Frame, kids gunim.Children) geom.Size {
	height, top := look.SidebarRow.Get(f.Theme)+4, float32(0)
	switch {
	case r.heading:
		top = 12
	case r.it.key == mpMachine:
		height = mpHeadHeight.Get(f.Theme)
	}
	for k := range kids.All {
		if k.Node() != r.inner {
			k.Layout(gunim.Constraints{})
			continue
		}
		s := k.Layout(gunim.Constraints{Max: geom.Sz(max(0, c.Max.W-20), height)})
		k.Place(geom.Pt(10, top+(height-s.H)/2))
	}
	return c.Constrain(geom.Sz(c.Max.W, top+height))
}

// Paint implements [gunim.Node]: the row; the list draws the fill of
// the one picked.
func (r *mpRow) Paint(p *paint.Painter, _ gunim.Frame, _ geom.Size, kids gunim.Children) {
	for k := range kids.All {
		if k.Node() == r.inner {
			k.Paint(p)
		}
	}
}

// emptyNode stands where nothing is shown yet. It has a field, so two
// are two nodes in the tree.
type emptyNode struct{ _ byte }

// Layout implements [gunim.Node].
func (*emptyNode) Layout(gunim.Constraints, gunim.Frame, gunim.Children) geom.Size {
	return geom.Size{}
}

// Paint implements [gunim.Node].
func (*emptyNode) Paint(*paint.Painter, gunim.Frame, geom.Size, gunim.Children) {}

// divider is the line between a machine pane's sidebar and the rest.
// Its width is a field, so two dividers are two nodes.
type divider struct{ width float32 }

// Layout implements [gunim.Node].
func (d *divider) Layout(c gunim.Constraints, _ gunim.Frame, _ gunim.Children) geom.Size {
	return geom.Sz(d.width, c.Max.H)
}

// Paint implements [gunim.Node].
func (d *divider) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, _ gunim.Children) {
	p.RRect(geom.Rect{Max: geom.Pt(d.width, box.H)}, 0, paint.Solid(widget.PanelBorder.Get(f.Theme)))
}
