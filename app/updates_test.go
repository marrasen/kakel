package app

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
	"github.com/marrasen/gunim/install"
	"github.com/marrasen/kakel/internal/testhome"
	"github.com/marrasen/kakel/settings"
)

func TestIsRelease(t *testing.T) {
	for v, want := range map[string]bool{
		"v1.2.3":                               true,
		"v0.1.0":                               true,
		"v1.2.3-4-gabcdef":                     false,
		"v1.2.3-4-gabcdef-dirty":               false,
		"v1.2.3-beta.1":                        true,
		"v1.2.3-rc.12":                         true,
		"v1.2.3-beta.1-2-gabcdef":              false,
		"v1.2.3-beta.1-dirty":                  false,
		"v1.2.3-beta":                          false,
		"v1.2.3-gamma.1":                       false,
		"v1.2.3-beta.1+meta":                   false,
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

// A beta takes betas as updates until the user says otherwise; a release
// takes them once asked to.
func TestBetasAreTakenByBetasAndWhenAsked(t *testing.T) {
	if isBeta("v1.2.3") || !isBeta("v1.2.3-beta.1") || isBeta("v1.2.3-4-gabcdef") {
		t.Fatal("isBeta tells betas wrong")
	}
	testhome.New(t)
	if wantsBeta() != isBeta(thisVersion()) {
		t.Fatal("unset, the build doesn't say whether betas are taken")
	}
	path, err := settings.Path()
	if err != nil {
		t.Fatal(err)
	}
	set, err := settings.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.PutBeta(true); err != nil {
		t.Fatal(err)
	}
	if !wantsBeta() || !(releases{}).github().Prerelease {
		t.Fatal("asked for betas, the updates don't take them")
	}
	if err := set.PutBeta(false); err != nil {
		t.Fatal(err)
	}
	if wantsBeta() {
		t.Fatal("told no betas, the updates still take them")
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
	if q := a.st.Asks[0]; q.Title != "kakel v99.0.0 is ready" || q.Yes != "Restart Now" || a.staged.Version != "v99.0.0" {
		t.Fatalf("the restart offer is %+v, staged %q", q, a.staged.Version)
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

// Restart Now on the notice of an update put in place by itself
// restarts into this program's place, asking first as Exit does, in a
// restart's words. Told no, kakel offers the restart again.
func TestTheReadyNoticeRestarts(t *testing.T) {
	a := updatesApp(t)
	a.updateWins = &fakeUpdateWindows{}
	t.Cleanup(func() { restartInto = "" })
	a.updated(install.Release{Version: "v99.0.0"})
	told := len(a.st.Notices)
	a.handle(RestartIntoUpdate{})
	waitFor(t, a, "the question", func() bool { return len(a.st.Asks) == 1 })
	if exe, _ := executable(); restartInto != exe {
		t.Fatalf("it restarts into %q", restartInto)
	}
	if q := a.st.Asks[0]; q.Title != "Restart kakel?" || q.Yes != "Restart" {
		t.Fatalf("it asks %+v", q)
	}
	a.handle(AskAnswered{ID: a.st.Asks[0].ID})
	waitFor(t, a, "the no", func() bool { return !a.leaving })
	if restartInto != "" {
		t.Fatalf("declined, it still restarts into %q", restartInto)
	}
	if len(a.st.Notices) != told+1 || a.st.Notices[told].Title != "kakel v99.0.0 is ready" {
		t.Fatalf("declined, the notices are %+v", a.st.Notices)
	}
}

// An update put in place with no window open, in the tray, is told from
// the tray, and its notice waits for the next window.
func TestAReadyUpdateWaitsForAWindow(t *testing.T) {
	a, one, two := twoWindowApp(t)
	a.updateWins = &fakeUpdateWindows{}
	var popped []string
	a.traySet = Tray{
		Set:      func(gunim.Tray) error { return nil },
		StayOpen: func(bool) {},
		Notify:   func(title, body string) error { popped = append(popped, title); return nil },
	}
	a.tray.on = true
	one.gone, two.gone = true, true
	a.updated(install.Release{Version: "v99.0.0"})
	if len(popped) != 1 || popped[0] != "kakel v99.0.0 is ready" {
		t.Fatalf("the tray said %q", popped)
	}
	three := a.addWindow(gunimtest.New(t, geom.Sz(400, 300), nil).Client(), nil)
	if got := a.stateFor(three, a.st).Notices; len(got) != 1 || got[0].Title != "kakel v99.0.0 is ready" {
		t.Fatalf("the next window shows %+v", got)
	}
	// With a window open the notice is that window's, and the tray is
	// left alone.
	a.front(three)
	a.updated(install.Release{Version: "v99.1.0"})
	if len(popped) != 1 || len(a.stateFor(three, a.st).Notices) != 2 {
		t.Fatalf("with a window open, the tray said %q and the window shows %+v", popped, a.stateFor(three, a.st).Notices)
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
	a.staged = install.Release{Version: "v99.0.0"}
	a.handle(CheckUpdates{})
	waitFor(t, a, "the offer", func() bool { return len(a.st.Asks) == 1 })
	if q := a.st.Asks[0]; q.Title != "kakel v99.0.0 is ready" || q.Yes != "Restart Now" {
		t.Fatalf("staged, Check for Updates offers %+v", q)
	}
}

// fakeUpdateWindows notes the update windows kakel opens.
type fakeUpdateWindows struct {
	mu      sync.Mutex
	updates []install.Update
	from    []string
	abouts  []install.About
}

func (f *fakeUpdateWindows) ShowUpdate(u install.Update) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, u)
	return nil
}

func (f *fakeUpdateWindows) ShowWhatsNew(from string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.from = append(f.from, from)
	return nil
}

func (f *fakeUpdateWindows) ShowAbout(o install.About) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.abouts = append(f.abouts, o)
	return nil
}

// seen runs fn on what f noted, under its lock.
func (f *fakeUpdateWindows) seen(fn func() bool) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return fn()
}

// With gunim's update window, a newer release opens it, a copy that is
// not installed naming itself to update where it is; a release put in
// place by itself says so, and its notice opens the window ready to
// restart.
func TestUpdatesOpenGunimsWindow(t *testing.T) {
	a := updatesApp(t)
	stubRelease(t, "v99.0.0")
	wins := &fakeUpdateWindows{}
	a.updateWins = wins
	a.offerUpdate(install.Release{Version: "v99.0.0"})
	waitFor(t, a, "the update window", func() bool { return wins.seen(func() bool { return len(wins.updates) == 1 }) })
	if len(a.st.Asks) != 0 {
		t.Fatalf("opened %+v, and asked %+v", wins.updates, a.st.Asks)
	}
	exe, _ := executable()
	if u := wins.updates[0]; u.Ready || u.Quit == nil || u.Exe != exe {
		t.Fatalf("the update is %+v", u)
	}

	a.updated(install.Release{Version: "v99.0.0"})
	n := a.st.Notices[len(a.st.Notices)-1]
	if len(n.Buttons) != 2 || n.Buttons[0].Label != "Restart Now" || n.Buttons[1].Label != "What's New" {
		t.Fatalf("ready, the notice's buttons are %+v", n.Buttons)
	}
	if _, ok := n.Buttons[0].On.(RestartIntoUpdate); !ok {
		t.Fatalf("Restart Now sends %T", n.Buttons[0].On)
	}
	ready, ok := n.Buttons[1].On.(ShowReadyUpdate)
	if !ok || a.staged.Version != "v99.0.0" || len(a.st.Asks) != 0 {
		t.Fatalf("ready, the notice is %+v", n)
	}
	a.handle(ready)
	waitFor(t, a, "the ready window", func() bool { return wins.seen(func() bool { return len(wins.updates) == 2 }) })
	if !wins.updates[1].Ready {
		t.Fatalf("the notice opened %+v", wins.updates)
	}

	// A later release put in place takes the notice's place, and the
	// first one's What's New, still showing in another window, opens the
	// later release too: the earlier one is no longer the one waiting.
	a.updated(install.Release{Version: "v99.1.0"})
	if later := a.st.Notices[len(a.st.Notices)-1]; later.Key == "" || later.Key != n.Key {
		t.Fatalf("the notices are keyed %q and %q", n.Key, later.Key)
	}
	a.handle(ready)
	waitFor(t, a, "the later window", func() bool { return wins.seen(func() bool { return len(wins.updates) == 3 }) })
	if u := wins.updates[2]; !u.Ready || u.Release.Version != "v99.1.0" {
		t.Fatalf("the first notice opened %+v", u)
	}
}

// The first start of a release put in place by itself says so, and its
// notice shows what changed since the version it replaced.
func TestAnUpdateSaysWhatsNew(t *testing.T) {
	a := updatesApp(t)
	wins := &fakeUpdateWindows{}
	a.updateWins = wins
	was := updatedFrom
	updatedFrom = func() string { return "v0.6.0" }
	t.Cleanup(func() { updatedFrom = was })
	a.startUpdates()
	n := a.st.Notices[len(a.st.Notices)-1]
	if n.Body != "From v0.6.0." || n.On != (ShowWhatsNew{From: "v0.6.0"}) {
		t.Fatalf("the notice is %+v", n)
	}
	a.handle(n.On)
	waitFor(t, a, "what's new", func() bool { return wins.seen(func() bool { return len(wins.from) == 1 }) })
	if wins.from[0] != "v0.6.0" {
		t.Fatalf("what's new opened from %q", wins.from)
	}
}

// About opens gunim's window about kakel, ready to take a newer release
// as the update window does.
func TestAboutOpensGunimsWindow(t *testing.T) {
	a := updatesApp(t)
	wins := &fakeUpdateWindows{}
	a.updateWins = wins
	a.handle(ShowAbout{})
	waitFor(t, a, "the window about kakel", func() bool { return wins.seen(func() bool { return len(wins.abouts) == 1 }) })
	if wins.abouts[0].Quit == nil {
		t.Fatal("About can't restart kakel into a newer release")
	}
	if wins.abouts[0].Ready != "" {
		t.Fatalf("About is told %q waits, with none in place", wins.abouts[0].Ready)
	}
	// A release the updates put in place by themselves is in place for
	// About too, which offers the restart into it alone.
	a.updated(install.Release{Version: "v9.9.9"})
	a.handle(ShowAbout{})
	waitFor(t, a, "About again", func() bool { return wins.seen(func() bool { return len(wins.abouts) == 2 }) })
	if wins.abouts[1].Ready != "v9.9.9" {
		t.Fatalf("About is told %q waits, want v9.9.9", wins.abouts[1].Ready)
	}
}
