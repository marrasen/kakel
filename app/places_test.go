package app

import (
	"os"
	"slices"
	"testing"

	"github.com/marrasen/gunim/filemanager"

	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/settings"
)

// A location is kept for its machine alone, with its look, and listed
// among the machine's places after those found, until moved.
func TestALocationIsAPlaceOfItsMachine(t *testing.T) {
	a := fontApp(t)
	dir := t.TempDir()
	a.handle(SaveFavourite{Path: dir, Name: "Work", Color: "teal", Icon: "code", Location: true})
	a.handle(SaveFavourite{Machine: "s1", Path: "/srv/www", Location: true})
	if favs, _ := a.settings.Favourites(); slices.ContainsFunc(favs, func(f settings.Favourite) bool { return f.Path == dir }) {
		t.Fatal("a location was kept as a favourite")
	}
	places := a.st.MachinePlaces(machines.Local)
	i := slices.IndexFunc(places, func(p MachinePlace) bool { return p.Key == FilesLocation+dir })
	if i < 1 || places[i].Name != "Work" || places[i].Color != "teal" || places[i].Icon != "code" || places[i].Location == nil {
		t.Fatalf("this computer's places are %+v", places)
	}
	if places[0].Key != FilesFound+FoundHome {
		t.Fatalf("this computer's places start with %+v", places[0])
	}
	if on := a.st.MachinePlaces("s1"); len(on) != 2 || on[1].Path != "/srv/www" {
		t.Fatalf("s1's places are %+v", on)
	}
	// Moved first, and made the default, it is where files open.
	a.handle(ReorderPlaces{Machine: machines.Local, Keys: []string{FilesLocation + dir, FilesFound + FoundHome}})
	if got := a.st.MachinePlaces(machines.Local); got[0].Key != FilesLocation+dir || got[1].Key != FilesFound+FoundHome {
		t.Fatalf("moved, the places are %+v", got)
	}
	a.handle(SetDefaultFiles{Key: FilesLocation + dir})
	if f := a.defaultFiles(machines.Local); f.path != dir || f.name != "Work" {
		t.Fatalf("files open at %+v", f)
	}
	// Moved to another folder, it stays the default, and first.
	moved := t.TempDir()
	a.handle(SaveFavourite{Was: dir, Path: moved, Name: "Work", Location: true})
	if f := a.defaultFiles(machines.Local); f.path != moved {
		t.Fatalf("moved, files open at %+v", f)
	}
	if got := a.st.MachinePlaces(machines.Local); got[0].Key != FilesLocation+moved {
		t.Fatalf("moved, the places are %+v", got)
	}
	// Where kakel found a folder, no location can be.
	home, _ := os.UserHomeDir()
	if err := a.saveFavourite(SaveFavourite{Path: home, Location: true}); err == nil {
		t.Fatal("a location was kept at the home folder")
	}
	a.handle(RemoveFavourite{Path: moved, Location: true})
	if slices.ContainsFunc(a.st.MachinePlaces(machines.Local), func(p MachinePlace) bool { return p.Location != nil }) {
		t.Fatal("the location removed is still a place")
	}
	if a.st.FilesDefault[""] != "" || slices.Contains(a.st.LocationOrder[""], FilesLocation+moved) {
		t.Fatalf("removed, it is still the default %q or in the order %q", a.st.FilesDefault[""], a.st.LocationOrder[""])
	}
	if len(a.st.Notices) != 0 {
		t.Fatalf("notices %+v", a.st.Notices)
	}
}

// A file manager window lists the places of the machine it shows under
// that machine's name, hidden ones left out, then the servers; a drag
// among them is kept.
func TestTheFileManagerListsThePlacesOfTheMachineShown(t *testing.T) {
	a := fontApp(t)
	a.st.Saved = []remote.Host{{ID: "s1", Name: "web"}}
	a.noteNames()
	web := []filemanager.Place{{Name: "web", Path: "/home/deploy", Kind: "drive", Group: "Machines", FS: serverFS + "s1"}}
	a.serverPlaces.Store(&web)
	dir := t.TempDir()
	a.handle(SaveFavourite{Path: dir, Name: "Work", Color: "teal", Location: true})
	a.handle(SaveFavourite{Machine: "s1", Path: "/srv/www", Location: true})

	here, _ := a.placesOn("")
	i := slices.IndexFunc(here, func(p filemanager.Place) bool { return p.Path == dir })
	if i < 0 || here[i].Group != hereGroup || here[i].Title != "This computer" || !here[i].Movable || here[i].Color != "teal" {
		t.Fatalf("this computer's places are %+v", here)
	}
	if last := here[len(here)-1]; last.Group != "Machines" {
		t.Fatalf("the servers are not last: %+v", last)
	}
	a.handle(HideFound{ID: FoundHome})
	if here, _ := a.placesOn(""); slices.ContainsFunc(here, func(p filemanager.Place) bool { return p.Kind == "home" }) {
		t.Fatal("the home hidden is still listed")
	}

	// Its files open, web's home is where they opened.
	a.serverPlaces.Store(&web)
	fs := serverFS + "s1"
	web, _ = a.placesOn(fs)
	var names []string
	for _, p := range web {
		if p.Group == hereGroup {
			if p.Title != "web" || p.FS != fs {
				t.Fatalf("web's place is %+v", p)
			}
			names = append(names, p.Name)
		}
	}
	if !slices.Equal(names, []string{"Home", "www"}) {
		t.Fatalf("web's places are %q", names)
	}
	// Dragged, www first: the order is kept for web.
	ordered := []filemanager.Place{web[1], web[0]}
	a.placesArranged(nil, hereGroup, ordered)
	waitFor(t, a, "the order kept", func() bool {
		return slices.Equal(a.st.LocationOrder["s1"], []string{FilesLocation + "/srv/www", FilesFound + FoundHome})
	})
	if web, _ := a.placesOn(fs); web[0].Name != "www" {
		t.Fatalf("after the drag web's places start with %+v", web[0])
	}
}
