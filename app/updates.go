package app

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/marrasen/gunim/install"

	"github.com/marrasen/kakel/settings"
)

// Installing kakel, and keeping it up to date. gunim's installer does
// the installing; see Installer. A copy run without installing offers
// to install itself from its menu. The installed one, a release build,
// looks for a newer release a minute after it starts and once a day,
// and says so, or with Updates set to install, fetches it and puts it
// in place for the next start. A restart into the new copy is a
// question away.

// Intents for installing and updating.
type (
	// InstallKakel installs this copy for the user, with a shortcut on
	// the desktop and a start with the computer when asked, and updates
	// done by themselves with AutoUpdate.
	InstallKakel struct{ Desktop, Autostart, AutoUpdate bool }
	// SetUpdates keeps what kakel does with a newer release: one of
	// settings.UpdatesOff, UpdatesNotify and UpdatesInstall.
	SetUpdates struct{ What string }
	// ToggleAutostart starts the installed kakel with the computer, into
	// the tray, or no longer.
	ToggleAutostart struct{}
)

// Update is what the windows are told of installing and updating.
type Update struct {
	// Installed says this is the installed copy, Installable that this
	// system can have one, and Autostart that it starts with the
	// computer. Updates is the setting.
	Installed, Installable, Autostart bool
	Updates                           string
}

// updateEvery is how often the installed kakel looks for a newer
// release, and updateFirst how long after it starts it first looks.
var (
	updateEvery = 24 * time.Hour
	updateFirst = time.Minute
)

// restartInto is the program kakel starts as it ends, for a restart into
// a new copy; empty for none.
var restartInto string

// RestartInto is the program to start once kakel has ended, or "".
func RestartInto() string { return restartInto }

// executable is this program, as its path; a test sets it.
var executable = os.Executable

// installer is the description of kakel the installer works from; a
// test sets it.
var installer = Installer

// showUpdate tells the windows where kakel stands.
func (a *app) showUpdate() {
	exe, _ := executable()
	u := Update{Updates: settings.UpdatesNotify}
	if _, to, err := install.Where(installer()); err == nil {
		u.Installable = true
		u.Installed = exe != "" && samePath(exe, to)
	}
	if in, err := install.Find(installer()); err == nil {
		u.Autostart = in.Chose(install.PickAutostart)
	}
	if a.settings != nil {
		u.Updates = a.settings.Updates()
	}
	a.st.Update = u
}

// startUpdates takes away the copy the last update moved aside, and
// looks for newer releases from now on, while the settings want it.
func (a *app) startUpdates() {
	exe, err := executable()
	if err != nil {
		return
	}
	install.CleanOld(exe)
	a.showUpdate()
	if !isRelease(thisVersion()) || !a.opts.OneOfMany() || !a.st.Update.Installed {
		// A build from a working tree has no order against releases, a
		// kakel of its own leaves it to the one running, and a copy not
		// installed is the user's to keep as it is: Check for Updates
		// still offers it the newest.
		return
	}
	var look func(time.Duration)
	look = func(after time.Duration) {
		time.AfterFunc(after, func() {
			a.events <- func() {
				if a.gone {
					return
				}
				a.lookForUpdate()
				look(updateEvery)
			}
		})
	}
	look(updateFirst)
}

// isRelease reports whether version names a release, vX.Y.Z, not a
// build from a working tree.
func isRelease(version string) bool {
	if !strings.HasPrefix(version, "v") || strings.Count(version, ".") != 2 || strings.ContainsAny(version, "-+") {
		return false
	}
	return install.IsRelease(version)
}

// standing is how this build compares to a release.
type standing int

const (
	// unknown is the two having no order between them, because one of
	// them is not a plain vX.Y.Z: a build from a working tree calls
	// itself dev-<commit>. Such a build may hold work that is in no
	// release and lack work that every release has.
	unknown standing = iota
	// behind is the release being the later version, current the build
	// being that release, and ahead the build being the later version,
	// as one from main between releases is.
	behind
	current
	ahead
)

// against says how the build calling itself have compares to release.
func against(have, release string) standing {
	switch {
	case !isRelease(have) || !isRelease(release):
		return unknown
	case install.Newer(release, have):
		return behind
	case install.Newer(have, release):
		return ahead
	}
	return current
}

// lookForUpdate asks for the newest release, and acts on one newer.
func (a *app) lookForUpdate() {
	what := settings.UpdatesNotify
	if a.settings != nil {
		what = a.settings.Updates()
	}
	if what == settings.UpdatesOff || a.updating {
		return
	}
	a.updating = true
	go func() {
		newest, err := latestRelease(a.ctx)
		a.events <- func() {
			a.updating = false
			if err != nil || against(thisVersion(), newest.Version) != behind || newest.Version == a.staged {
				// Said nothing: the next look may reach GitHub, and an
				// update put in place waits for the next start.
				return
			}
			if what == settings.UpdatesInstall && a.writable() {
				a.fetchUpdate(newest, false)
				return
			}
			a.offerUpdate(newest)
		}
	}()
}

// writable reports whether this program's own copy can be replaced.
func (a *app) writable() bool {
	exe, err := executable()
	if err != nil {
		return false
	}
	f, err := os.OpenFile(exe+".new", os.O_CREATE|os.O_WRONLY, 0o755)
	if err != nil {
		return false
	}
	_ = f.Close()
	_ = os.Remove(exe + ".new")
	return true
}

// offerUpdate says a newer release is out, and fetches it if asked, or
// offers the restart into it when it is in place already.
func (a *app) offerUpdate(newest install.Release) {
	have := thisVersion()
	if newest.Version == a.staged {
		if exe, err := executable(); err == nil {
			a.offerRestart(exe, "kakel "+newest.Version+" is ready", "It starts the next time kakel does.")
		}
		return
	}
	go func() {
		ans, err := a.ask(a.ctx, Ask{Title: "kakel " + newest.Version + " is out",
			Text: "This is " + have + ". Update now, and restart into it when you like.", Yes: "Update", No: "Not Now"})
		a.events <- func() {
			if err == nil && ans.Yes {
				if a.writable() {
					a.fetchUpdate(newest, true)
					return
				}
				// Not writable, as a copy under Program Files: the page,
				// to fetch it by hand.
				a.offerRelease("kakel "+newest.Version+" is out", have, newest)
			}
		}
	}()
}

// stageRelease fetches a release, checks it against its SHA256SUMS, and
// puts it in place of the program at exe; a test sets it.
var stageRelease = func(ctx context.Context, r install.Release, exe string) error {
	return install.StageTo(ctx, installer(), r, exe)
}

// fetchUpdate fetches newest and puts it in place of this program, for
// the next start, and offers a restart into it. asked says the user
// asked, and hears a failure; one by itself fails quietly into the log.
func (a *app) fetchUpdate(newest install.Release, asked bool) {
	exe, err := executable()
	if err != nil {
		return
	}
	a.say("update", "Fetching kakel "+newest.Version+"…")
	go func() {
		err := stageRelease(a.ctx, newest, exe)
		a.events <- func() {
			a.say("update", "")
			if err != nil {
				if asked {
					a.failed("Couldn't update kakel", err.Error())
				} else {
					log.Printf("couldn't update kakel to %s: %v", newest.Version, err)
				}
				return
			}
			a.staged = newest.Version
			a.offerRestart(exe, "kakel "+newest.Version+" is ready", "It starts the next time kakel does.")
		}
	}()
}

// offerRestart asks whether to restart into exe now.
func (a *app) offerRestart(exe, title, text string) {
	go func() {
		ans, err := a.ask(a.ctx, Ask{Title: title, Text: text, Yes: "Restart Now", No: "Later"})
		a.events <- func() {
			if err == nil && ans.Yes {
				a.restartAs(exe)
			}
		}
	}()
}

// restartAs ends kakel, asking first as Exit does, and starts exe once
// it has.
func (a *app) restartAs(exe string) {
	restartInto = exe
	a.askToQuit()
}

// installKakel installs this copy, and offers to move to it.
func (a *app) installKakel(in InstallKakel) error {
	exe, err := executable()
	if err != nil {
		return err
	}
	s, err := install.NewSessionFor(installer(), exe, false)
	if err != nil {
		if errors.Is(err, install.ErrUnsupported) {
			return errors.New("installing is not done on this system yet")
		}
		return err
	}
	picks := map[string]bool{install.PickDesktop: in.Desktop, install.PickAutostart: in.Autostart, autoUpdate: in.AutoUpdate}
	got, err := s.Install(a.ctx, picks, nil)
	if err != nil {
		return err
	}
	if a.settings != nil {
		// The installer kept it in the settings file, as these settings
		// keep it too, so a save of theirs does not undo it.
		what := settings.UpdatesNotify
		if in.AutoUpdate {
			what = settings.UpdatesInstall
		}
		if err := a.settings.PutUpdates(what); err != nil {
			a.failed("Couldn't keep the update setting", err.Error())
		}
	}
	a.showUpdate()
	if !samePath(exe, got.Exe) {
		a.offerRestart(got.Exe, "kakel is installed", "In "+got.Dir+". Restart into the installed kakel now?")
	}
	return nil
}

// toggleAutostart starts the installed kakel with the computer, into
// the tray, or no longer.
func (a *app) toggleAutostart() error {
	in, err := install.Find(installer())
	if err != nil {
		return err
	}
	return install.Change(installer(), map[string]bool{install.PickAutostart: !in.Chose(install.PickAutostart)})
}

// samePath reports whether two paths name one file or folder, letter
// case aside where the system ignores it, and through links.
func samePath(a, b string) bool {
	a, b = resolvePath(a), resolvePath(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// resolvePath is path cleaned, with its links resolved where it exists.
func resolvePath(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	return filepath.Clean(path)
}
