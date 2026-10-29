package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/marrasen/kakel/machines"
	shellfind "github.com/marrasen/kakel/shells"
)

// What opens on a machine by default, by name: the shell a new terminal
// runs, and the folder its files open at. A machine's buttons, menus and
// tabs say these names, not "Terminal" and "Files".

// MachineDefaults is what opens on a machine by default, each by its
// name.
type MachineDefaults struct {
	// Shell is the name of the shell a new terminal runs, and Files the
	// name of the folder its files open at.
	Shell, Files string
	// Shells are the shells that open on it, in the order the user put
	// them in, each by its name.
	Shells []ShellEntry
	// FilesKey is its default folder's key, as SetDefaultFiles takes it.
	FilesKey string
}

// SetDefaultFiles makes the folder Key where files on Machine open:
// FilesFound and a found folder's ID, as Home, FilesLocation and a
// location's path, or FilesFavourite and a favourite's path.
type SetDefaultFiles struct {
	Machine machines.ID
	Key     string
}

// How a default folder's key starts: one kakel found, as Home or a
// drive, or a favourite.
const (
	FilesFound     = "found:"
	FilesFavourite = "fav:"
)

// filesChoice is a folder files on a machine can open at: its key, its
// name, and its path, "" for the account's home.
type filesChoice struct {
	key, name, path string
}

// filesChoices are the folders files on m can open at, those hidden
// left out: its places, in order, then its favourites.
func (a *app) filesChoices(m machines.ID) []filesChoice {
	var out []filesChoice
	for _, p := range a.st.MachinePlaces(m) {
		if !p.Hidden {
			out = append(out, filesChoice{key: p.Key, name: p.Name, path: p.Path})
		}
	}
	// Favourites, as they could be the default before there were
	// locations.
	for _, f := range a.st.Favourites {
		if f.Machine == m {
			out = append(out, filesChoice{key: FilesFavourite + f.Path, name: f.Label(), path: f.Path})
		}
	}
	return out
}

// defaultFiles is the folder files on m open at: the one the user made
// the default, while it is there and shown; or else, as before there
// were defaults, the one favourite saved for a server; or else the first
// shown, so a hidden home is never where they open.
func (a *app) defaultFiles(m machines.ID) filesChoice {
	choices := a.filesChoices(m)
	if key := a.st.FilesDefault[string(m)]; key != "" {
		for _, c := range choices {
			if c.key == key {
				return c
			}
		}
	}
	if m != machines.Local {
		if favs := a.favouritesOn(m); len(favs) == 1 {
			for _, c := range choices {
				if c.key == FilesFavourite+favs[0] {
					return c
				}
			}
		}
	}
	if len(choices) > 0 {
		return choices[0]
	}
	return filesChoice{key: FilesFound + FoundHome, name: "Home"}
}

// defaultShellName is the name of the shell a new terminal on m runs.
func (a *app) defaultShellName(m machines.ID) string {
	if m == machines.Local {
		id := a.defaultShell()
		for _, s := range a.st.Shells {
			if s.ID == id {
				return s.Title
			}
		}
		for _, s := range a.found {
			if s.ID == id && s.Title != "" {
				return s.Title
			}
		}
		// None found: the user's own shell, by its program's name.
		for _, v := range []string{"SHELL", "COMSPEC"} {
			if p := os.Getenv(v); p != "" {
				base := filepath.Base(p)
				return strings.TrimSuffix(strings.TrimSuffix(base, ".exe"), ".EXE")
			}
		}
		return "Shell"
	}
	if h, saved := a.machines.Saved(m); saved && h.Window || a.machines.Get(m).Window != nil {
		// A kakel window's terminals run its own shell.
		return "Shell"
	}
	if run := a.st.ServerShells[string(m)]; run != "" {
		if p, ok := a.farProfile(m, run); ok {
			return p.Name
		}
	}
	if !a.showsLogin(m) {
		if ps := a.profilesOn(m); len(ps) > 0 {
			return ps[0].Name
		}
	}
	if name := a.foundEdit(string(m), LoginShell).Name; name != "" {
		return name
	}
	return "Login Shell"
}

// machineDefaults are the names of what opens by default on each machine
// known: this computer, the saved servers and windows, and those
// connected to.
func (a *app) machineDefaults() map[machines.ID]MachineDefaults {
	out := map[machines.ID]MachineDefaults{}
	add := func(m machines.ID) {
		if _, ok := out[m]; !ok {
			files := a.defaultFiles(m)
			out[m] = MachineDefaults{Shell: a.defaultShellName(m), Files: files.name, FilesKey: files.key, Shells: a.shellsOn(m)}
		}
	}
	add(machines.Local)
	for _, h := range a.st.Saved {
		add(machines.ID(h.ID))
	}
	for _, i := range a.machines.Infos() {
		add(i.ID)
	}
	return out
}

// setDefaultFiles makes a folder where files on a machine open.
func (a *app) setDefaultFiles(in SetDefaultFiles) error {
	if a.settings == nil {
		return errors.New("the settings could not be read, so nothing can be saved")
	}
	err := a.settings.PutFilesDefault(string(in.Machine), in.Key)
	a.st.FilesDefault = a.settings.FilesDefault()
	return err
}

// localShellName is what a terminal here running argv is called: its
// shell's name, as found or as the user renamed it; the default's for
// the user's own shell; or else its program's name.
func (a *app) localShellName(argv []string) string {
	if len(argv) == 0 {
		return a.defaultShellName(machines.Local)
	}
	if s, ok := shellfind.Running(a.found, withoutFolder(argv)); ok && s.Title != "" {
		return s.Title
	}
	base := filepath.Base(argv[0])
	return strings.TrimSuffix(strings.TrimSuffix(base, ".exe"), ".EXE")
}

// ShellEntry is a shell that opens on a machine, as a menu offers it:
// what opening it asks (Action, as the launcher spells it: "shell:" and a
// shell's or profile's ID, "login" for a server's own shell, or
// "terminal" for the one a machine opens by default), its name, and
// whether a new terminal runs it.
type ShellEntry struct {
	Action, Name string
	Default      bool
}

// shellsOn are the shells that open on m, in the order the user put
// them in, each by its name: here, each shell; on a saved server, its
// own shell, when shown, and its profiles; elsewhere, the one it runs.
func (a *app) shellsOn(m machines.ID) []ShellEntry {
	if m == machines.Local {
		var out []ShellEntry
		def := a.defaultShell()
		for _, sh := range a.st.Shells {
			out = append(out, ShellEntry{Action: "shell:" + sh.ID, Name: sh.Title, Default: sh.ID == def})
		}
		if len(out) == 0 {
			out = append(out, ShellEntry{Action: "terminal", Name: a.defaultShellName(m), Default: true})
		}
		return out
	}
	if !a.offersTerminal(m) {
		return nil
	}
	profiles := a.profilesOn(m)
	if len(profiles) == 0 && a.showsLogin(m) {
		return []ShellEntry{{Action: "terminal", Name: a.defaultShellName(m), Default: true}}
	}
	def := a.defaultShellName(m)
	run := a.st.ServerShells[string(m)]
	type withID struct {
		id string
		e  ShellEntry
	}
	var all []withID
	if a.showsLogin(m) {
		all = append(all, withID{LoginShell, ShellEntry{Action: "login", Name: a.loginName(m), Default: run == ""}})
	}
	for _, p := range profiles {
		all = append(all, withID{p.ID, ShellEntry{Action: "shell:" + p.ID, Name: p.Name, Default: p.ID == run || run == "" && !a.showsLogin(m) && p.Name == def}})
	}
	all = InOrder(all, func(w withID) string { return w.id }, a.st.ProfileOrder[string(m)])
	out := make([]ShellEntry, len(all))
	for i, w := range all {
		out[i] = w.e
	}
	return out
}

// loginName is what a server's own shell is called: as the user renamed
// it, or Login Shell.
func (a *app) loginName(m machines.ID) string {
	if name := a.foundEdit(string(m), LoginShell).Name; name != "" {
		return name
	}
	return "Login Shell"
}
