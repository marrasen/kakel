package view

import (
	"slices"
	"strings"

	"github.com/marrasen/gunim"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/settings"
)

// profilesOn are the profiles on the saved server m, in the order the
// user put them in; none on this computer or a kakel window.
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
	return app.InOrder(out, func(p settings.Profile) string { return p.ID }, w.profileOrder[string(m)])
}

// foundOf is what the user changed of the found profile id on machine
// m: nothing, when they changed nothing.
func foundOf(found []settings.FoundProfile, m machines.ID, id string) settings.FoundProfile {
	if i := slices.IndexFunc(found, func(f settings.FoundProfile) bool { return f.HostID == string(m) && f.ID == id }); i >= 0 {
		return found[i]
	}
	return settings.FoundProfile{HostID: string(m), ID: id}
}

// hidden reports whether the found profile id on machine m is hidden.
func (w *Window) hidden(m machines.ID, id string) bool { return foundOf(w.found, m, id).Hidden }

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
// is hidden or gone, as for a server used only to reach others: its
// terminals and its files.
func (w *Window) offersNothing(m machines.ID) bool {
	return w.isServer(m) && !w.offersTerminal(m) && !w.showsFiles(m)
}

// shellOpens is what opening sh on m asks: a shell or profile by its ID,
// a server's own shell, or the shell m opens by default.
func shellOpens(m machines.ID, sh app.ShellEntry) gunim.Intent {
	switch {
	case strings.HasPrefix(sh.Action, "shell:"):
		return app.OpenShellNamed{ID: strings.TrimPrefix(sh.Action, "shell:")}
	case sh.Action == "login":
		return app.OpenOn{Machine: m, Login: true}
	}
	return app.OpenOn{Machine: m}
}
