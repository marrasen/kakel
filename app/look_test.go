package app

import (
	"testing"

	"github.com/marrasen/kakel/settings"
)

// Saving the Settings dialog keeps what it chose, shows it to the
// windows, and tells the sounds; a kakel that never chose has the
// defaults.
func TestSavedSettingsAreKeptShownAndHeard(t *testing.T) {
	set, err := settings.Load(t.TempDir() + "/settings.json")
	if err != nil {
		t.Fatal(err)
	}
	a, _ := agentApp(t)
	a.settings = set
	type told struct{ iface, any bool }
	var heard []told
	a.sound = func(iface, any bool) { heard = append(heard, told{iface, any}) }

	a.showLook()
	if a.st.Sounds != settings.DefaultSounds || a.st.Rings != settings.DefaultRings || a.st.SystemTitleBar {
		t.Fatalf("unchosen, the windows are shown %+v, %+v, %v", a.st.Sounds, a.st.Rings, a.st.SystemTitleBar)
	}
	if len(heard) != 1 || heard[0] != (told{false, true}) {
		t.Fatalf("the sounds heard %+v, want the bell's alone wanted", heard)
	}

	in := SaveLook{Sounds: Alerts{Interface: true}, Rings: Alerts{Lost: true}, SystemTitleBar: true}
	if err := a.saveLook(in); err != nil {
		t.Fatal(err)
	}
	if a.st.Sounds != in.Sounds || a.st.Rings != in.Rings || !a.st.SystemTitleBar {
		t.Fatalf("saved, the windows are shown %+v, %+v, %v", a.st.Sounds, a.st.Rings, a.st.SystemTitleBar)
	}
	if heard[len(heard)-1] != (told{true, true}) {
		t.Fatalf("the sounds last heard %+v, want the interface's", heard[len(heard)-1])
	}
	if set.Sounds() != in.Sounds || !set.SystemTitleBar() {
		t.Fatal("the settings did not keep what was saved")
	}
}
