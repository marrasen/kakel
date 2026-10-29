package app

import (
	"errors"
	"slices"
	"strings"

	"github.com/marrasen/gunim/filemanager"

	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/settings"
)

// A machine's places are the folders its files list under its name, at
// the top of the file manager's sidebar, while it shows that machine's
// files: the folders kakel finds there, its home and here its drives,
// and the locations saved for it, in the order the user put them in.
// Favourites, saved on any machine, are listed under them whichever
// machine it shows.

// FilesLocation starts the key of a location, before its path, as
// FilesFound starts a found folder's.
const FilesLocation = "loc:"

// hereGroup is the file manager's group of the places of the machine a
// window shows. Its name stays, whichever machine that is, so the
// sidebar keeps its section where the user put it; its heading is the
// machine's name.
const hereGroup = "This computer"

// MachinePlace is a folder a machine's files list under its name: one
// kakel found there, or a location saved for it.
type MachinePlace struct {
	// Key is its key, as SetDefaultFiles and ReorderPlaces take it.
	Key string
	// FoundID is a found folder's ID, as Home's, "" for a location, and
	// Found the name kakel found it by.
	FoundID, Found string
	// Name is what it is called, Path where it is, "" for the account's
	// home, and Color and Icon its look.
	Name, Path, Color, Icon string
	// Kind is "home", "drive" or "folder".
	Kind string
	// Hidden says it is a found folder the user hid.
	Hidden bool
	// Location is the location it is, for one saved.
	Location *Favourite
}

// ReorderPlaces keeps Keys as the order of Machine's places.
type ReorderPlaces struct {
	Machine machines.ID
	Keys    []string
}

// placesOf are machine m's places: those found, with what the user
// changed of them in edits, then its locations among locs, in order.
func placesOf(m machines.ID, found []MachinePlace, edits []settings.FoundProfile, locs []Favourite, order []string) []MachinePlace {
	out := make([]MachinePlace, 0, len(found)+len(locs))
	for _, f := range found {
		if i := slices.IndexFunc(edits, func(e settings.FoundProfile) bool { return machines.ID(e.HostID) == m && e.ID == f.FoundID }); i >= 0 {
			e := edits[i]
			f.Hidden, f.Color, f.Icon = e.Hidden, e.Color, e.Icon
			if e.Name != "" {
				f.Name = e.Name
			}
		}
		out = append(out, f)
	}
	for i, l := range locs {
		if l.Machine == m {
			out = append(out, MachinePlace{Key: FilesLocation + l.Path, Name: l.Label(), Path: l.Path, Color: l.Color, Icon: l.Icon,
				Kind: "folder", Location: &locs[i]})
		}
	}
	return InOrder(out, func(p MachinePlace) string { return p.Key }, order)
}

// foundPlace is a folder kakel found, by its ID, the name it found it
// by, and its path.
func foundPlace(id, name, path string) MachinePlace {
	kind := "drive"
	if id == FoundHome {
		kind = "home"
	}
	return MachinePlace{Key: FilesFound + id, FoundID: id, Found: name, Name: name, Path: path, Kind: kind}
}

// MachinePlaces are machine m's places, the hidden ones too, in the
// order the user put them in.
func (st State) MachinePlaces(m machines.ID) []MachinePlace {
	found := []MachinePlace{foundPlace(FoundHome, "Home", "")}
	if m == machines.Local {
		for _, d := range st.Drives {
			found = append(found, foundPlace(d.ID, d.Name, d.Path))
		}
	}
	return placesOf(m, found, st.Found, st.Locations, st.LocationOrder[string(m)])
}

// fileManagerPlaces are the file manager's places on this computer.
func (a *app) fileManagerPlaces() ([]filemanager.Place, error) { return a.placesOn("") }

// placesOn are the file manager's places for a window showing the files
// of the file system fs: that machine's, under its name, then the
// servers'. The file manager asks off the program's goroutine: they are
// read from the settings, which lock, and what the program noted.
func (a *app) placesOn(fs string) ([]filemanager.Place, error) {
	var edits []settings.FoundProfile
	var locs []Favourite
	var order map[string][]string
	if a.settings != nil {
		edits, locs, order = a.settings.Found(), favouritesOf(a.settings.Locations()), a.settings.LocationOrder()
	}
	var servers []filemanager.Place
	if s := a.serverPlaces.Load(); s != nil {
		servers = *s
	}
	var found []MachinePlace
	// What the file manager found of each, by its key.
	byKey := map[string]filemanager.Place{}
	var err error
	m := machineOfFS(fs)
	switch {
	case fs == "":
		var here []filemanager.Place
		here, err = filemanager.LocalPlaces()
		var drives []FoundDrive
		for _, p := range here {
			id := placeID(p)
			if p.Kind != "home" {
				drives = append(drives, FoundDrive{ID: id, Name: p.Name, Path: p.Path})
			}
			f := foundPlace(id, p.Name, p.Path)
			found = append(found, f)
			byKey[f.Key] = p
		}
		if err == nil {
			a.noteDrives(drives)
		}
	case !strings.HasSuffix(fs, gone):
		// A server's home is where its files opened.
		if i := slices.IndexFunc(servers, func(p filemanager.Place) bool { return p.FS == fs && p.Path != "" }); i >= 0 {
			found = append(found, foundPlace(FoundHome, "Home", servers[i].Path))
		}
	}
	title := a.fsName(fs)
	var out []filemanager.Place
	for _, mp := range placesOf(m, found, edits, locs, order[string(m)]) {
		if mp.Hidden || mp.Path == "" || strings.HasSuffix(fs, gone) {
			continue
		}
		p := byKey[mp.Key]
		p.Name, p.Path, p.Color, p.Icon = mp.Name, mp.Path, mp.Color, mp.Icon
		if p.Kind == "" {
			p.Kind = mp.Kind
		}
		p.FS, p.Group, p.Title, p.Movable = fs, hereGroup, title, true
		out = append(out, p)
	}
	return append(out, servers...), err
}

// placesArranged keeps the order a drag in the file manager left the
// places of the machine a window shows in, those hidden after them.
func (a *app) placesArranged(_ *filemanager.Window, group string, places []filemanager.Place) {
	if group != hereGroup || len(places) == 0 || a.settings == nil {
		return
	}
	m := machineOfFS(places[0].FS)
	keys := make([]string, 0, len(places))
	for _, p := range places {
		// Found ones keep the kind the file manager found them by;
		// locations are folders.
		switch {
		case p.Kind == "folder":
			keys = append(keys, FilesLocation+p.Path)
		case p.Kind == "home":
			keys = append(keys, FilesFound+FoundHome)
		default:
			keys = append(keys, FilesFound+driveID(p.Path))
		}
	}
	for _, k := range a.settings.LocationOrder()[string(m)] {
		if !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	}
	err := a.settings.PutLocationOrder(string(m), keys)
	go func() {
		a.events <- func() {
			if err != nil {
				a.failed("Couldn't save the order of the folders", err.Error())
			}
			a.showFavourites()
			// The other windows show the order too.
			if a.files != nil && len(a.fmPanes) > 0 {
				a.files.Refresh()
			}
		}
	}()
}

// reorderPlaces keeps the order of a machine's places, as dragged in its
// pane.
func (a *app) reorderPlaces(in ReorderPlaces) error {
	if a.settings == nil {
		return errors.New("the settings could not be read, so nothing can be saved")
	}
	if err := a.settings.PutLocationOrder(string(in.Machine), in.Keys); err != nil {
		return err
	}
	a.showFavourites()
	if a.files != nil && len(a.fmPanes) > 0 {
		a.files.Refresh()
	}
	return nil
}
