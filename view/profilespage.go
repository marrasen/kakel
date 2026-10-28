package view

import (
	"slices"
	"strings"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/icon"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/settings"
	"github.com/marrasen/kakel/words"
)

// The Profiles page of Settings: every machine's profiles in one list,
// its terminals and then its files. Those kakel finds, its shells, home
// and drives, and a server's own shell and home, can be renamed and
// hidden; those the user adds, terminal profiles and favourites, can be
// changed and removed.

// profileRow is a line of the Profiles page.
type profileRow struct {
	machine machines.ID
	// name, kind and what are its cells: what it is called, Terminal or
	// Files, and its command line or folder.
	name, kind, what string
	// found is the ID of a profile kakel finds, empty for the user's.
	found  string
	hidden bool
	// profile is a terminal profile of the user's, and fav a favourite.
	profile *settings.Profile
	fav     *app.Favourite
}

// profilesPage holds the Profiles page's table and the buttons that
// work on the row the cursor is on.
type profilesPage struct {
	w     *Window
	table *widget.Table
	rows  map[widget.Key]profileRow
	keys  []widget.Key
	edit  *widget.Button
	hide  *widget.Button
	drop  *widget.Button
}

func (p *settingsPane) profilesTab() gunim.Node {
	pp := &profilesPage{w: p.w, rows: map[widget.Key]profileRow{}}
	p.profilesPage = pp
	pp.table = widget.NewTable(
		widget.TableColumn{Title: "Name", Width: 190},
		widget.TableColumn{Title: "Machine", Width: 150},
		widget.TableColumn{Title: "Kind", Width: 80},
		widget.TableColumn{Title: "Runs or opens"},
	)
	pp.table.Row = func(k widget.Key) widget.TableRow {
		r := pp.rows[k]
		name := r.name
		if r.hidden {
			name += " (hidden)"
		}
		ic := icon.SquareTerminal
		if r.kind == "Files" {
			ic = icon.Folder
		}
		where := "This computer"
		if r.machine != "" {
			where = pp.w.nameOf(r.machine)
		}
		return widget.TableRow{Cells: []string{name, where, r.kind, r.what}, Faint: r.hidden, Icon: ic}
	}
	pp.table.OnActivate = func(k widget.Key, u *gunim.UI) gunim.Intent { pp.editRow(pp.rows[k], u); return nil }
	addT, addF := iconButton(icon.Plus, "Add Terminal…"), iconButton(icon.FolderPlus, "Add Files…")
	addT.OnClick = func(u *gunim.UI) gunim.Intent { pp.w.profileDialog(pp.w.profiles, nil, u); return nil }
	addF.OnClick = func(u *gunim.UI) gunim.Intent { pp.w.favouriteDialog(nil, u); return nil }
	pp.edit, pp.hide, pp.drop = iconButton(icon.Pencil, "Edit…"), iconButton(icon.Eye, "Show"), iconButton(icon.Trash2, "Remove")
	onRow := func(b *widget.Button, do func(profileRow, *gunim.UI)) {
		b.OnClick = func(u *gunim.UI) gunim.Intent {
			if k, ok := pp.table.Cursor(); ok {
				do(pp.rows[k], u)
			}
			return nil
		}
	}
	onRow(pp.edit, pp.editRow)
	onRow(pp.hide, func(r profileRow, u *gunim.UI) {
		if r.found != "" && r.hidden {
			u.Send(pp.table, app.HideFound{HostID: string(r.machine), ID: r.found, Show: true})
		}
	})
	// One kakel finds is found again each run: removed, it is hidden.
	onRow(pp.drop, func(r profileRow, u *gunim.UI) {
		switch {
		case r.found != "":
			u.Send(pp.table, app.HideFound{HostID: string(r.machine), ID: r.found})
		case r.profile != nil:
			u.Send(pp.table, app.RemoveProfile{ID: r.profile.ID})
		case r.fav != nil:
			u.Send(pp.table, app.RemoveFavourite{Machine: r.fav.Machine, Path: r.fav.Path})
		}
	})
	return settingsPage(
		settingsSection("Profiles",
			faintLabel("What each machine offers to open: its terminals and its files. kakel finds the shells, the drives and each server's own shell and home: you can rename those, and removing one hides it everywhere until Show brings it back. Terminals and folders you add can be changed or removed."),
			widget.NewSized(pp.table, 0, 420),
			buttons(addT, addF, pp.edit, pp.hide, pp.drop)),
	)
}

// show lists the profiles as st has them, the cursor where it was.
func (pp *profilesPage) show(st app.State, u *gunim.UI) {
	clear(pp.rows)
	pp.keys = pp.keys[:0]
	add := func(key string, r profileRow) {
		k := widget.Key(key)
		pp.rows[k] = r
		pp.keys = append(pp.keys, k)
	}
	found := func(m machines.ID, id, name, kind, what string) {
		f := foundOf(st.Found, m, id)
		if f.Name != "" {
			name = f.Name
		}
		add("found:"+string(m)+"\x00"+id, profileRow{machine: m, name: name, kind: kind, what: what, found: id, hidden: f.Hidden})
	}
	mine := func(m machines.ID) {
		for i, p := range st.Profiles {
			if machines.ID(p.HostID) == m {
				add("profile:"+p.ID, profileRow{machine: m, name: p.Name, kind: "Terminal", what: app.ProfileLine(p), profile: &st.Profiles[i]})
			}
		}
	}
	favs := func(m machines.ID) {
		for i, f := range st.Favourites {
			if f.Machine == m {
				add("fav:"+string(m)+"\x00"+f.Path, profileRow{machine: m, name: f.Label(), kind: "Files", what: f.Path, fav: &st.Favourites[i]})
			}
		}
	}
	for _, sh := range st.FoundShells {
		found("", sh.ID, sh.Title, "Terminal", "found here")
	}
	mine("")
	found("", app.FoundHome, "Home", "Files", "your home folder")
	for _, d := range st.Drives {
		found("", d.ID, d.Name, "Files", d.Path)
	}
	favs("")
	for _, h := range st.Saved {
		m := machines.ID(h.ID)
		if h.Window {
			// A kakel window's terminals are its own; its files are
			// offered, and can be hidden from its place.
			found(m, app.FoundHome, "Home", "Files", "the window's home folder")
			continue
		}
		found(m, app.LoginShell, "Login Shell", "Terminal", "the account's own shell")
		mine(m)
		found(m, app.FoundHome, "Home", "Files", "the account's home folder")
		favs(m)
	}
	pp.table.SetKeys(slices.Clone(pp.keys), u)
}

// editRow opens the dialog that changes r.
func (pp *profilesPage) editRow(r profileRow, u *gunim.UI) {
	switch {
	case r.profile != nil:
		p := *r.profile
		pp.w.profileDialog(pp.w.profiles, &p, u)
	case r.fav != nil:
		f := *r.fav
		pp.w.favouriteDialog(&f, u)
	case r.found != "":
		pp.w.foundDialog(r, u)
	}
}

// foundDialog changes a profile kakel finds: its name, and for a shell
// here, the folder it starts in and its variables.
func (w *Window) foundDialog(r profileRow, u *gunim.UI) {
	f := foundOf(w.found, r.machine, r.found)
	name, dir := widget.NewTextField(), widget.NewTextField()
	env := widget.NewTextArea()
	env.Rows = 3
	env.Placeholder = "NAME=value, one a line"
	name.Placeholder, dir.Placeholder = "As kakel finds it", "Optional"
	name.SetText(f.Name, nil)
	dir.SetText(f.Folder, nil)
	env.SetText(strings.Join(f.Env, "\n"), nil)
	form := widget.NewForm().Add("Name", name)
	shellHere := r.machine == "" && r.kind == "Terminal"
	if shellHere {
		form.Add("Folder", dir).Add("Environment", env)
	}
	d := widget.NewDialog("Edit " + r.name)
	d.Body = form
	d.SetButtons("Save", "Cancel")
	parse := func() (settings.FoundProfile, error) {
		out := f
		out.Name = strings.TrimSpace(name.Text())
		if !shellHere {
			return out, nil
		}
		out.Folder = strings.TrimSpace(dir.Text())
		if out.Folder != "" {
			if _, err := app.FolderHere(out.Folder); err != nil {
				return out, err
			}
		}
		vars, err := app.ParseEnv(env.Text())
		out.Env = vars
		return out, err
	}
	d.Check = func() string {
		if _, err := parse(); err != nil {
			return words.UpperFirst(err.Error())
		}
		return ""
	}
	d.OnAccept = func(u *gunim.UI) gunim.Intent {
		out, err := parse()
		if err != nil {
			return nil
		}
		return app.SaveFound{Found: out}
	}
	d.OnDismiss = widget.Sends(app.DialogClosed{})
	w.openDialog(d, u)
}

// favouriteDialog asks for a favourite, a files profile: a new one
// when old is nil, or old to change.
func (w *Window) favouriteDialog(old *app.Favourite, u *gunim.UI) {
	name, dir := widget.NewTextField(), widget.NewTextField()
	name.Placeholder, dir.Placeholder = "The folder's own", `such as D:\Projects`
	hosts, labels := []machines.ID{""}, []string{"This computer"}
	for _, h := range w.saved {
		if !h.Window {
			hosts, labels = append(hosts, machines.ID(h.ID)), append(labels, h.Name)
		}
	}
	machine := widget.NewDropdown(widget.Labels(labels...))
	machine.Label = "Machine"
	at := func() machines.ID { return hosts[max(0, min(machine.Selected(), len(hosts)-1))] }
	title, was := "Add Files", ""
	if old != nil {
		title, was = "Edit "+old.Label(), old.Path
		if !slices.Contains(hosts, old.Machine) {
			hosts, labels = append(hosts, old.Machine), append(labels, w.nameOf(old.Machine))
			machine.SetItems(widget.Labels(labels...))
		}
		machine.SetSelected(slices.Index(hosts, old.Machine), nil)
		machine.Disabled = true
		name.SetText(old.Name, nil)
		dir.SetText(old.Path, nil)
	}
	form := widget.NewForm()
	if len(hosts) > 1 {
		form.Add("Machine", machine)
	}
	d := widget.NewDialog(title)
	d.Body = form.Add("Name", name).Add("Folder", dir)
	d.SetButtons("Save", "Cancel")
	d.Check = func() string {
		folder := strings.TrimSpace(dir.Text())
		if folder == "" {
			return "Enter a folder"
		}
		if at() == "" {
			if _, err := app.FolderHere(folder); err != nil {
				return words.UpperFirst(err.Error())
			}
		}
		return ""
	}
	d.OnAccept = func(u *gunim.UI) gunim.Intent {
		return app.SaveFavourite{Machine: at(), Was: was, Path: dir.Text(), Name: name.Text()}
	}
	d.OnDismiss = widget.Sends(app.DialogClosed{})
	w.openDialog(d, u)
}
