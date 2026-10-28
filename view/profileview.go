package view

import (
	"slices"
	"strings"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/settings"
	"github.com/marrasen/kakel/words"
)

// profileDialog asks for a profile: a new one when old is nil, or old
// to edit. have are the profiles there are, whose names its name must
// not repeat on the same machine.
func (w *Window) profileDialog(have []settings.Profile, old *settings.Profile, u *gunim.UI) {
	name, line, dir := widget.NewTextField(), widget.NewTextField(), widget.NewTextField()
	env := widget.NewTextArea()
	env.Rows = 3
	env.Placeholder = "NAME=value, one a line"
	// The machine it runs on: this computer, or a saved server. A
	// profile stays where it was made, as its program and folder are
	// that machine's.
	hosts, labels := []string{""}, []string{"This computer"}
	for _, h := range w.saved {
		if !h.Window {
			hosts, labels = append(hosts, h.ID), append(labels, h.Name)
		}
	}
	if old != nil && !slices.Contains(hosts, old.HostID) {
		hosts, labels = append(hosts, old.HostID), append(labels, w.nameOf(machines.ID(old.HostID)))
	}
	machine := widget.NewDropdown(widget.Labels(labels...))
	machine.Label = "Machine"
	hostID := func() string { return hosts[max(0, min(machine.Selected(), len(hosts)-1))] }
	placeholders := func() {
		line.Placeholder, dir.Placeholder = `such as C:\msys64\usr\bin\bash.exe --login -i`, "Optional"
		if hostID() != "" {
			line.Placeholder = "such as tmux new -A -s main"
		}
	}
	machine.OnChange = func(int, *gunim.UI) gunim.Intent { placeholders(); return nil }
	id, title := "", "Add Profile"
	if old != nil {
		id, title = old.ID, "Edit Profile"
		machine.SetSelected(slices.Index(hosts, old.HostID), nil)
		machine.Disabled = true
		name.SetText(old.Name, nil)
		line.SetText(app.ProfileLine(*old), nil)
		dir.SetText(old.Folder, nil)
		env.SetText(strings.Join(old.Env, "\n"), nil)
	}
	placeholders()
	have = slices.Clone(have)
	parse := func() (settings.Profile, error) {
		return app.ParseProfile(have, id, hostID(), name.Text(), line.Text(), dir.Text(), env.Text())
	}
	form := widget.NewForm()
	if len(hosts) > 1 {
		form.Add("Machine", machine)
	}
	d := widget.NewDialog(title)
	d.Body = form.
		Add("Name", name).
		Add("Command line", line).
		Add("Folder", dir).
		Add("Environment", env)
	d.SetButtons("Save", "Cancel")
	d.Check = func() string {
		if _, err := parse(); err != nil {
			return words.UpperFirst(err.Error())
		}
		return ""
	}
	d.OnAccept = func(u *gunim.UI) gunim.Intent {
		p, err := parse()
		if err != nil {
			return nil
		}
		return app.SaveProfile{Profile: p}
	}
	d.OnDismiss = widget.Sends(app.DialogClosed{})
	w.openDialog(d, u)
}

// profilesOn are the profiles on the saved server m, the one its new
// terminal runs first; none on this computer or a kakel window.
func (w *Window) profilesOn(m machines.ID) []settings.Profile {
	if m == "" || !slices.ContainsFunc(w.saved, func(h remote.Host) bool { return machines.ID(h.ID) == m && !h.Window }) {
		return nil
	}
	var out []settings.Profile
	for _, p := range w.profiles {
		if machines.ID(p.HostID) == m {
			out = append(out, p)
		}
	}
	if i := slices.IndexFunc(out, func(p settings.Profile) bool { return p.ID == w.serverShells[string(m)] }); i > 0 {
		def := out[i]
		out = slices.Insert(slices.Delete(out, i, i+1), 0, def)
	}
	return out
}

// hidden reports whether the found profile id on machine m is hidden.
func (w *Window) hidden(m machines.ID, id string) bool {
	return slices.ContainsFunc(w.found, func(f settings.FoundProfile) bool { return f.HostID == string(m) && f.ID == id && f.Hidden })
}

// isServer reports whether m is a saved server, not a kakel window.
func (w *Window) isServer(m machines.ID) bool {
	return m != "" && slices.ContainsFunc(w.saved, func(h remote.Host) bool { return machines.ID(h.ID) == m && !h.Window })
}

// offersTerminal reports whether machine m offers a new terminal:
// anything but a saved server always does, and that when its own shell
// or one of its profiles is shown.
func (w *Window) offersTerminal(m machines.ID) bool {
	return !w.isServer(m) || !w.hidden(m, app.LoginShell) || len(w.profilesOn(m)) > 0
}

// showsFiles reports whether machine m offers its files.
func (w *Window) showsFiles(m machines.ID) bool { return !w.hidden(m, app.FoundHome) }

// offersNothing reports whether everything the saved server m offers
// is hidden or gone, as for a server used only to reach others.
func (w *Window) offersNothing(m machines.ID) bool {
	return w.isServer(m) && !w.offersTerminal(m) && !w.showsFiles(m) && len(w.foldersOn(m)) == 0
}
