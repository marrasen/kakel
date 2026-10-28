package app

import (
	"errors"
	"slices"
	"strings"

	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/settings"
	shellfind "github.com/marrasen/kakel/shells"
)

// Found profiles: the profiles kakel finds itself rather than the user
// describes. On this computer they are its shells, Command Prompt,
// PowerShell, Git Bash and each WSL distribution, its home folder and
// its drives; on a saved server, the account's own shell and its home.
//
// They are found again each run, so a program that moves or a drive
// added is seen. What the user changes of one, its name and for a
// shell here its folder and variables, is kept on top by its ID, and
// one hidden is taken out of every list that offers it.

// IDs of the found profiles that are not shells.
const (
	// FoundHome is a machine's home folder: the files a machine's Files
	// opens, and its place in the file manager's sidebar.
	FoundHome = "home"
	// drivePrefix starts the ID of a drive here, before its path.
	drivePrefix = "drive:"
)

// Intents for the found profiles.
type (
	// SaveFound keeps the user's change to a found profile.
	SaveFound struct{ Found settings.FoundProfile }
	// HideFound hides the found profile ID on the machine HostID, or
	// shows it again with Show.
	HideFound struct {
		HostID, ID string
		Show       bool
	}
)

// FoundDrive is a drive found here, as the file manager's sidebar
// lists it.
type FoundDrive struct {
	ID, Name, Path string
}

// foundEdit is what the user changed of the found profile id on the
// machine hostID: nothing, when they changed nothing.
func (a *app) foundEdit(hostID, id string) settings.FoundProfile {
	i := slices.IndexFunc(a.st.Found, func(f settings.FoundProfile) bool { return f.HostID == hostID && f.ID == id })
	if i < 0 {
		return settings.FoundProfile{HostID: hostID, ID: id}
	}
	return a.st.Found[i]
}

// hidden reports whether the found profile id on machine m is hidden.
func (a *app) hidden(m machines.ID, id string) bool {
	return a.foundEdit(string(m), id).Hidden
}

// saveFound keeps a change to a found profile and offers the shells as
// they are now. New terminals here go back to the default shell when
// the one they started is hidden, as when it is removed.
func (a *app) saveFound(f settings.FoundProfile) error {
	if a.settings == nil {
		return errors.New("the settings could not be read, so nothing can be kept")
	}
	f.Name = strings.TrimSpace(f.Name)
	f.Folder = strings.TrimSpace(f.Folder)
	if f.HostID == "" && f.Folder != "" {
		full, err := FolderHere(f.Folder)
		if err != nil {
			return err
		}
		f.Folder = full
	}
	if err := a.settings.PutFound(f); err != nil {
		return err
	}
	a.st.Found = a.settings.Found()
	a.mergeShells()
	a.showFavourites()
	a.notePlaces()
	if a.files != nil && len(a.fmPanes) > 0 {
		a.files.Refresh()
	}
	if f.Hidden && f.HostID == "" {
		if kept, chosen := a.settings.Shell(); chosen && kept == f.ID {
			return a.pickShell("")
		}
	}
	if f.Hidden && f.HostID != "" && f.ID == LoginShell && a.st.ServerShells[f.HostID] == "" {
		// Its new terminal runs a profile, if it has one shown.
		if ps := a.profilesOn(machines.ID(f.HostID)); len(ps) > 0 {
			err := a.settings.PutServerShell(f.HostID, ps[0].ID)
			a.st.ServerShells = a.settings.ServerShells()
			return err
		}
	}
	return nil
}

// hideFound hides a found profile, or shows it again.
func (a *app) hideFound(in HideFound) error {
	f := a.foundEdit(in.HostID, in.ID)
	f.Hidden = !in.Show
	return a.saveFound(f)
}

// foundRun is the folder and variables a shell here started with argv
// is given by the user, nil for the user's own shell: a profile's, or
// the change made to the shell kakel found.
func (a *app) foundRun(argv []string) (folder string, env []string) {
	if p, ok := a.profileOf(argv); ok {
		return p.Folder, p.Env
	}
	id := a.ownShell
	if len(argv) > 0 {
		s, ok := shellfind.Running(a.detected, argv)
		if !ok {
			return "", nil
		}
		id = s.ID
	}
	if id == "" {
		return "", nil
	}
	f := a.foundEdit("", id)
	return f.Folder, f.Env
}

// showsLogin reports whether the saved server m offers its account's
// own shell.
func (a *app) showsLogin(m machines.ID) bool { return !a.hidden(m, LoginShell) }

// offersTerminal reports whether machine m offers a new terminal:
// this computer always does, and a saved server when its own shell or
// one of its profiles is shown.
func (a *app) offersTerminal(m machines.ID) bool {
	if h, ok := a.machines.Saved(m); !ok || h.Window {
		return true
	}
	return a.showsLogin(m) || len(a.profilesOn(m)) > 0
}

// showsFiles reports whether machine m offers its files: this
// computer, a server, or one beyond a window, whose own home stays
// shown.
func (a *app) showsFiles(m machines.ID) bool { return !a.hidden(m, FoundHome) }

// offersNothing reports whether everything the saved server m offers
// is hidden or gone, as for a server used only to reach others: its
// terminals, its files and its favourites. Lists of things to open
// leave such a server out.
func (a *app) offersNothing(m machines.ID) bool {
	if h, ok := a.machines.Saved(m); !ok || h.Window {
		return false
	}
	return !a.showsLogin(m) && !a.showsFiles(m) && len(a.profilesOn(m)) == 0 && len(a.favouritesOn(m)) == 0
}

// noteDrives notes the drives found here, for Settings to offer.
func (a *app) noteDrives(places []FoundDrive) {
	if slices.Equal(a.st.Drives, places) {
		return
	}
	a.st.Drives = places
}

// driveID is the ID of the found profile of the drive at path.
func driveID(path string) string { return drivePrefix + path }

// dropFoundOn forgets what the user changed of the found profiles on
// the saved server m, as the server is removed. called is the server's
// name, for a failure to say.
func (a *app) dropFoundOn(m machines.ID, called string) {
	if a.settings == nil || !slices.ContainsFunc(a.st.Found, func(f settings.FoundProfile) bool { return f.HostID == string(m) }) {
		return
	}
	if err := a.settings.DropFoundOn(string(m)); err != nil {
		a.failed("Couldn't forget the hidden profiles on "+called, err.Error())
		return
	}
	a.st.Found = a.settings.Found()
}
