package app

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/marrasen/gunim/install"
	"github.com/marrasen/kakel/settings"
)

func TestIsRelease(t *testing.T) {
	for v, want := range map[string]bool{
		"v1.2.3":                               true,
		"v0.1.0":                               true,
		"v1.2.3-4-gabcdef":                     false,
		"v1.2.3-4-gabcdef-dirty":               false,
		"v1.2.3-beta.1":                        false,
		"v0.5.1-0.20261005120000-0123456789ab": false,
		"dev-abcdef":                           false,
		"abcdef":                               false,
		"":                                     false,
	} {
		if got := isRelease(v); got != want {
			t.Errorf("isRelease(%q) = %v, want %v", v, got, want)
		}
	}
}

// updatesApp is an app with settings of its own.
func updatesApp(t *testing.T) *app {
	a, _ := agentApp(t)
	set, err := settings.Load(t.TempDir() + "/settings.json")
	if err != nil {
		t.Fatal(err)
	}
	a.settings = set
	return a
}

// stubRelease answers every look for the newest release with v, and
// counts the looks.
func stubRelease(t *testing.T, v string) *atomic.Int32 {
	was, wasVersion := latestRelease, thisVersion
	var looks atomic.Int32
	latestRelease = func(context.Context) (install.Release, error) {
		looks.Add(1)
		return install.Release{Version: v, Page: "https://example.com/release"}, nil
	}
	thisVersion = func() string { return "v1.0.0" }
	t.Cleanup(func() { latestRelease, thisVersion = was, wasVersion })
	return &looks
}

// gunim's updates, told of a newer release, have kakel ask; one put in
// place by itself has kakel offer the restart into it.
func TestGunimsUpdatesAreAskedAbout(t *testing.T) {
	a := updatesApp(t)
	stubRelease(t, "v99.0.0")
	live.Store(a)
	t.Cleanup(func() { live.Store(nil) })
	go toLive(func(a *app) { a.offerUpdate(install.Release{Version: "v99.0.0"}) })
	waitFor(t, a, "the offer", func() bool { return len(a.st.Asks) == 1 })
	if q := a.st.Asks[0]; q.Title != "kakel v99.0.0 is out" || q.Yes != "Update" {
		t.Fatalf("the offer is %+v", q)
	}
	a.handle(AskAnswered{ID: a.st.Asks[0].ID})
	waitFor(t, a, "the no", func() bool { return len(a.st.Asks) == 0 })
	go toLive(func(a *app) { a.updated(install.Release{Version: "v99.0.0"}) })
	waitFor(t, a, "the restart offer", func() bool { return len(a.st.Asks) == 1 })
	if q := a.st.Asks[0]; q.Title != "kakel v99.0.0 is ready" || q.Yes != "Restart Now" || a.staged != "v99.0.0" {
		t.Fatalf("the restart offer is %+v, staged %q", q, a.staged)
	}
}

// Installing copies this program where it is installed, keeps the
// update setting, and offers a restart into the installed copy.
func TestInstallingCopiesAndOffersARestart(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("installs into the user's registry here")
	}
	a := updatesApp(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	exe := filepath.Join(t.TempDir(), "kakel")
	if err := os.WriteFile(exe, []byte("the program"), 0o755); err != nil {
		t.Fatal(err)
	}
	was := executable
	executable = func() (string, error) { return exe, nil }
	t.Cleanup(func() { executable = was })
	a.showUpdate()
	if a.st.Update.Installed || !a.st.Update.Installable {
		t.Fatalf("before, the state says %+v", a.st.Update)
	}
	a.handle(InstallKakel{Autostart: true, AutoUpdate: true})
	to := filepath.Join(home, "data", "kakel", "kakel")
	if got, err := os.ReadFile(to); err != nil || string(got) != "the program" {
		t.Fatalf("installed, %s reads %q, %v", to, got, err)
	}
	if in, err := install.Find(installer()); err != nil || in.Updates != install.UpdatesInstall || !a.st.Update.Autostart {
		t.Fatalf("installed, the install says %+v, %v, and the state %+v", in, err, a.st.Update)
	}
	if a.settings.Updates() != settings.UpdatesInstall || a.st.Update.Updates != settings.UpdatesInstall {
		t.Fatalf("installed, the setting is %q and the state %+v", a.settings.Updates(), a.st.Update)
	}
	waitFor(t, a, "the restart offer", func() bool { return len(a.st.Asks) == 1 })
	if q := a.st.Asks[0]; q.Title != "kakel is installed" || q.Yes != "Restart Now" {
		t.Fatalf("the offer is %+v", q)
	}
	a.handle(AskAnswered{ID: a.st.Asks[0].ID})
	waitFor(t, a, "the offer to close", func() bool { return len(a.st.Asks) == 0 })
	if restartInto != "" {
		t.Fatalf("told later, it restarts into %q", restartInto)
	}
	// The installed copy, started, knows it is the one.
	executable = func() (string, error) { return to, nil }
	a.showUpdate()
	if !a.st.Update.Installed {
		t.Fatal("the installed copy doesn't know it is installed")
	}
}

// A restart the user then declines to quit for isn't done by a quit
// long after.
func TestADeclinedRestartIsForgotten(t *testing.T) {
	a := updatesApp(t)
	t.Cleanup(func() { restartInto = "" })
	a.restartAs("/new/kakel")
	waitFor(t, a, "the question", func() bool { return len(a.st.Asks) == 1 })
	a.handle(AskAnswered{ID: a.st.Asks[0].ID})
	waitFor(t, a, "the no", func() bool { return !a.leaving })
	if restartInto != "" {
		t.Fatalf("declined, it still restarts into %q", restartInto)
	}
}

// An update put in place isn't fetched again: asked, kakel offers the
// restart into it.
func TestAStagedUpdateIsNotFetchedAgain(t *testing.T) {
	a := updatesApp(t)
	stubRelease(t, "v99.0.0")
	a.staged = "v99.0.0"
	a.handle(CheckUpdates{})
	waitFor(t, a, "the offer", func() bool { return len(a.st.Asks) == 1 })
	if q := a.st.Asks[0]; q.Title != "kakel v99.0.0 is ready" || q.Yes != "Restart Now" {
		t.Fatalf("staged, Check for Updates offers %+v", q)
	}
}
