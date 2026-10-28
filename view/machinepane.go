package view

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/icon"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/look"
	"github.com/marrasen/kakel/machines"
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
)

// The room of a machine pane: round its sidebar, round the rest, between
// the parts of the rest, and between the parts of a row.
var (
	mpSidePad = theme.Insets("kakel.machine.side", geom.Insets{Left: 8, Right: 8, Top: 12, Bottom: 12})
	mpPad     = theme.Insets("kakel.machine.pad", geom.Insets{Left: 28, Right: 28, Top: 20, Bottom: 28})
	mpGap     = theme.Length("kakel.machine.gap", 12)
	mpRowGap  = theme.Length("kakel.machine.row", 8)
)

// mpItem is one row of a machine pane's sidebar: the machine, a
// heading, a profile, or the row that adds one.
type mpItem struct {
	key, title string
	icon       *icon.Icon
	heading    bool
	// profile is what a profile row is about.
	profile mpProfileOf
}

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
	right   *swapNode
	root    gunim.Node
	items   []mpItem
	sel     string
	// shown is the key and state the right side was built for, so a
	// state that changes nothing there leaves what is typed alone.
	shown   string
	shownOf string
	// added is the name of a profile just added, to pick once it comes.
	added string
	st    app.State
}

func newMachinePane(w *Window, id string) *machinePane {
	mp := &machinePane{w: w, id: id, list: widget.NewList(), right: &swapNode{}, sel: mpMachine}
	mp.list.ClickOnce = true
	mp.list.OnActivate = func(k widget.Key, u *gunim.UI) gunim.Intent {
		if it, ok := mp.item(string(k)); ok && !it.heading {
			mp.pick(string(k), u)
		}
		return nil
	}
	side := widget.NewPad(widget.NewScroll(mp.list))
	side.Padding = mpSidePad
	mp.right.child = blankPane{}
	rightPad := widget.NewPad(widget.NewScroll(mp.right))
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
	mp.sel = k
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
		}
	}
	mp.refresh(u)
}

// refresh lists the rows and builds the right side for the one picked.
func (mp *machinePane) refresh(u *gunim.UI) {
	mp.items = mp.rows()
	if mp.added != "" {
		for _, it := range mp.items {
			if it.profile.own != nil && it.profile.own.Name == mp.added {
				mp.sel, mp.added = it.key, ""
			}
		}
	}
	if _, ok := mp.item(mp.sel); !ok {
		mp.sel = mpMachine
	}
	widget.Sync(mp.list, u, mp.items, func(it mpItem) widget.Key { return widget.Key(it.key) },
		func(it mpItem) *mpRow { r := newMPRow(mp); r.set(it, mp.sel == it.key, nil); return r },
		func(r *mpRow, it mpItem, u *gunim.UI) { r.set(it, mp.sel == it.key, u) })
	of := mp.sel + "\x00" + mp.signature()
	if of == mp.shownOf && mp.shown == mp.sel {
		return
	}
	mp.shown, mp.shownOf = mp.sel, of
	it, _ := mp.item(mp.sel)
	switch {
	case mp.sel == mpMachine:
		mp.right.set(mp.machineSide(u), u)
	case mp.sel == mpAdd:
		mp.right.set(mp.profileSide(mpProfileOf{}, true, u), u)
	default:
		mp.right.set(mp.profileSide(it.profile, false, u), u)
	}
}

// signature changes when what the right side shows changes: the
// machine as saved, and the picked row's profile.
func (mp *machinePane) signature() string {
	var b strings.Builder
	if h, ok := mp.host(); ok {
		b.WriteString(h.Name + "\x00" + h.Address + "\x00" + h.User + "\x00" + h.Via + "\x00" + strings.Join(h.Identities, ","))
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
			return []mpItem{head}
		}
	case mp.machine != "":
		return []mpItem{head}
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
		profiles = append(profiles, mpProfileOf{id: app.LoginShell, name: "Login Shell", line: "the account's own shell", found: true,
			edit: foundOf(st.Found, mp.machine, app.LoginShell), def: def == ""})
		for i, p := range st.Profiles {
			if machines.ID(p.HostID) == mp.machine {
				profiles = append(profiles, mpProfileOf{id: p.ID, own: &st.Profiles[i], line: app.ProfileLine(p), def: p.ID == def})
			}
		}
	}
	slices.SortStableFunc(profiles, func(a, b mpProfileOf) int {
		switch {
		case a.def == b.def:
			return 0
		case a.def:
			return -1
		}
		return 1
	})
	out := []mpItem{head, {key: mpProfiles, title: "Profiles", heading: true}}
	for _, p := range profiles {
		out = append(out, mpItem{key: mpProfile + p.id, title: p.title(), icon: icon.SquareTerminal, profile: p})
	}
	return append(out, mpItem{key: mpAdd, title: "Add Profile", icon: icon.Plus})
}

// machineSide is the right side for the machine's own row.
func (mp *machinePane) machineSide(u *gunim.UI) gunim.Node {
	h, saved := mp.host()
	if !mp.draft && !saved {
		title := heading("This computer")
		if mp.machine != "" {
			title = heading(mp.w.nameOf(mp.machine))
		}
		col := widget.Column(title, faintLabel(localWho()),
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
	if saved {
		title = h.Name
		connect.Label = "Connect"
		connect.OnClick = func(u *gunim.UI) gunim.Intent { return app.OpenOn{Machine: mp.machine} }
		remove := widget.NewButton("Remove…")
		remove.OnClick = func(u *gunim.UI) gunim.Intent { mp.w.confirmRemove(mp.machine, u); return nil }
		actions = append(actions, remove)
	}
	col := widget.Column(heading(title), widget.NewCard(f.form), problem, buttons(actions...))
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
	line.Placeholder = `such as C:\msys64\usr\bin\bash.exe --login -i`
	if !here {
		line.Placeholder, dir.Placeholder = "such as tmux new -A -s main", "Optional, such as ~/src"
	}
	teach := widget.NewCheckbox("Teach the shell to say what it is doing")
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
	if !adding && !p.def {
		def := widget.NewButton("Make Default")
		def.OnClick = func(*gunim.UI) gunim.Intent { return app.SetDefaultProfile{Machine: mp.machine, ID: p.id} }
		actions = append(actions, def)
	}
	note := "A profile you made."
	switch {
	case adding:
		note = "A shell, or a program such as tmux, that a terminal on " + mp.w.machineName(mp.machine) + " can open."
	case p.found && p.edit.Hidden:
		note = "Found by kakel, and hidden: the eye on the left shows it again."
	case p.found:
		note = "Found by kakel. Rename it, or hide it with the eye on the left."
	}
	if p.def {
		note += " New terminals open it."
	}
	col := widget.Column(heading(title), faintLabel(note), widget.NewCard(form), problem, buttons(actions...))
	col.Cross, col.Gap = widget.CrossStretch, mpGap
	return widget.NewSized(col, 640, 0)
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
	mp       *machinePane
	inner    *widget.Flex
	icon     *widget.Icon
	title    *widget.Label
	star     *widget.IconButton
	eye      *widget.IconButton
	bin      *widget.IconButton
	heading  bool
	selected bool
	it       mpItem
	// shape says which parts the row holds, built again only when that
	// changes.
	shape string
}

func newMPRow(mp *machinePane) *mpRow {
	r := &mpRow{mp: mp, icon: widget.NewIcon(icon.SquareTerminal, ""), title: widget.NewLabel("")}
	r.star = widget.NewIconButton(icon.Star, "New terminals open it")
	r.eye = widget.NewIconButton(icon.Eye, "Hide it")
	r.bin = widget.NewIconButton(icon.Trash2, "Delete it")
	r.star.OnClick = func(*gunim.UI) gunim.Intent {
		return app.SetDefaultProfile{Machine: r.mp.machine, ID: r.it.profile.id}
	}
	r.eye.OnClick = func(*gunim.UI) gunim.Intent {
		p := r.it.profile
		return app.HideFound{HostID: string(r.mp.machine), ID: p.id, Show: p.edit.Hidden}
	}
	r.bin.OnClick = func(u *gunim.UI) gunim.Intent {
		if p := r.it.profile.own; p != nil {
			r.mp.confirmDelete(*p, u)
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
	r.title.Face = widget.Font
	r.title.Color = widget.Ink
	r.icon.Color = widget.Ink
	p := it.profile
	kids := []gunim.Node{}
	switch {
	case it.heading:
		r.title.Face, r.title.Color = widget.BoldFont, look.Faint
		kids = append(kids, r.title)
	default:
		r.icon.Icon = it.icon
		kids = append(kids, r.icon, r.title)
	}
	if strings.HasPrefix(it.key, mpProfile) {
		r.star.Icon, r.star.Ink, r.star.Tooltip = icon.Star, look.Faint, "Make it the one new terminals open"
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

// fmtType names n's type, and which of the row's buttons it is.
func fmtType(n gunim.Node) string {
	switch n.(type) {
	case *widget.Label:
		return "L"
	case *widget.Icon:
		return "I"
	case *widget.Spacer:
		return "S"
	}
	return fmt.Sprintf("%p", n)
}

// Children implements [gunim.Composite].
func (r *mpRow) Children() []gunim.Node { return []gunim.Node{r.inner} }

// Layout implements [gunim.Node]: a row as tall as the sidebar's, its
// heading with room above it.
func (r *mpRow) Layout(c gunim.Constraints, f gunim.Frame, kids gunim.Children) geom.Size {
	height, top := look.SidebarRow.Get(f.Theme)+4, float32(0)
	if r.heading {
		top = 12
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

// Paint implements [gunim.Node]: the picked row's fill, then the row.
func (r *mpRow) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	if r.selected && !r.heading {
		p.RRect(geom.Rect{Min: geom.Pt(2, 1), Max: geom.Pt(box.W-2, box.H-1)}, look.RowRadius.Get(f.Theme), paint.Solid(look.RowActive.Get(f.Theme)))
	}
	for k := range kids.All {
		if k.Node() == r.inner {
			k.Paint(p)
		}
	}
}

// swapNode shows one child, which can change: the one before leaves
// the tree as the next comes in.
type swapNode struct{ child gunim.Node }

// set shows n in place of what was shown.
func (s *swapNode) set(n gunim.Node, u *gunim.UI) {
	old := s.child
	s.child = n
	if u != nil && old != n {
		if old != nil {
			u.Remove(old)
		}
		u.Insert(s, n)
		u.Invalidate()
	}
}

// Children implements [gunim.Composite].
func (s *swapNode) Children() []gunim.Node { return []gunim.Node{s.child} }

// Layout implements [gunim.Node]: the child shown; one leaving takes
// no room.
func (s *swapNode) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	var sz geom.Size
	for k := range kids.All {
		if k.Node() != s.child {
			k.Layout(gunim.Constraints{})
			continue
		}
		sz = k.Layout(c)
		k.Place(geom.Pt(0, 0))
	}
	return sz
}

// Paint implements [gunim.Node].
func (s *swapNode) Paint(p *paint.Painter, _ gunim.Frame, _ geom.Size, kids gunim.Children) {
	for k := range kids.All {
		if k.Node() == s.child {
			k.Paint(p)
		}
	}
}

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
