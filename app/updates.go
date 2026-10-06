package app

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"

	"github.com/marrasen/gunim/install"

	"github.com/marrasen/kakel/settings"
)

// Installing kakel, and keeping it up to date. gunim's installer does
// the installing, and gunim's updates the looking; see Installer. A
// copy run without installing offers to install itself from its menu.
// The installed one, a release build, is told of a newer release a
// minute after it starts and once a day, and asks, or with Updates set
// to install, finds it in place for the next start. gunim's update
// window does the asking: what's new, then the download and the
// restart, with their progress shown. An update put in place by itself
// says so as it is ready, and again on the first start of the new
// release, with what it brought a click away.

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
	// ToggleFolders opens folders with the installed kakel's file
	// manager, in place of File Explorer, or no longer.
	ToggleFolders struct{}
	// ShowWhatsNew shows what the releases after From changed, up to
	// this one: every release's notes for "".
	ShowWhatsNew struct{ From string }
	// ShowReadyUpdate shows the update window on Release, put in place
	// by itself, to restart into it.
	ShowReadyUpdate struct{ Release install.Release }
	// ShowAbout opens the window about kakel: its version, what each
	// release changed, and Check for Updates.
	ShowAbout struct{}
)

// UpdateWindows opens gunim's windows of an update, on the program's
// gunim app: install.ShowUpdate and install.ShowWhatsNew.
type UpdateWindows interface {
	ShowUpdate(u install.Update) error
	ShowWhatsNew(from string) error
	ShowAbout(o install.About) error
}

// updatedFrom is the version the update finished as this kakel started
// replaced, or ""; a test sets it.
var updatedFrom = install.UpdatedFrom

// Update is what the windows are told of installing and updating.
type Update struct {
	// Installed says this is the installed copy, Installable that this
	// system can have one, and Autostart that it starts with the
	// computer. Folders says folders open with it, where FoldersHere
	// says they can. Updates is the setting.
	Installed, Installable, Autostart bool
	Folders, FoldersHere              bool
	Updates                           string
}

// live is the kakel running, for gunim's updates to reach: they look on
// a goroutine of their own, from before the app is made.
var live atomic.Pointer[app]

// toLive runs fn on the running kakel's own goroutine, or drops it when
// none is running yet, or it is ending: the next look asks again.
func toLive(fn func(a *app)) {
	a := live.Load()
	if a == nil {
		return
	}
	select {
	case a.events <- func() {
		if !a.gone {
			fn(a)
		}
	}:
	case <-a.ctx.Done():
	}
}

// restartInto is the program kakel starts as it ends, for a restart into
// a new copy; empty for none.
var restartInto string

// RestartInto is the program to start once kakel has ended, or "".
func RestartInto() string { return restartInto }

// executable is this program, as its path; a test sets it.
var executable = os.Executable

// installer is the description of kakel the installer works from; a
// test sets it. Set in init, as Installer's hooks reach back here.
var installer func() install.App

func init() { installer = Installer }

// showUpdate tells the windows where kakel stands.
func (a *app) showUpdate() {
	exe, _ := executable()
	// Windows alone lets a program open folders in File Explorer's place.
	u := Update{Updates: settings.UpdatesNotify, FoldersHere: runtime.GOOS == "windows"}
	if _, to, err := install.Where(installer()); err == nil {
		u.Installable = true
		u.Installed = exe != "" && samePath(exe, to)
	}
	if in, err := install.Find(installer()); err == nil {
		u.Autostart = in.Chose(install.PickAutostart)
		u.Folders = in.Chose(install.PickFolders)
		u.Updates = string(in.Updates)
	} else if a.settings != nil {
		u.Updates = a.settings.Updates()
	}
	a.st.Update = u
}

// startUpdates takes away the copy the last update moved aside, and
// lets gunim's updates reach this kakel: they look for newer releases
// as the mode Options › Updates… sets says.
func (a *app) startUpdates() {
	if exe, err := executable(); err == nil {
		install.CleanOld(exe)
	}
	a.showUpdate()
	if from := updatedFrom(); from != "" {
		a.sayUpdated(from)
	}
	if a.opts.OneOfMany() {
		// A kakel of its own leaves the questions to the one running.
		live.Store(a)
		go func() {
			<-a.ctx.Done()
			live.CompareAndSwap(a, nil)
		}()
	}
}

// setUpdates keeps what the installed kakel does with a newer release:
// in gunim's install, which its updates follow, and in the settings.
func (a *app) setUpdates(what string) error {
	if err := install.SetUpdates(installer(), install.UpdateMode(what)); err != nil {
		if errors.Is(err, install.ErrNotInstalled) {
			return errors.New("updates are for the installed kakel: install it first")
		}
		return err
	}
	if a.settings != nil {
		return a.settings.PutUpdates(what)
	}
	return nil
}

// updated says a release gunim's updates put in place by themselves is
// ready, with the update window, its notes and its restart, a click
// away. Without that window, it offers the restart.
func (a *app) updated(r install.Release) {
	exe, err := executable()
	if err != nil {
		return
	}
	a.staged = r.Version
	if a.updateWins == nil {
		a.offerRestart(exe, "kakel "+r.Version+" is ready", "It starts the next time kakel does.")
		return
	}
	a.post(Notice{Title: "kakel " + r.Version + " is ready", Body: "It starts the next time kakel does.",
		Action: "What's New", On: ShowReadyUpdate{Release: r}})
}

// sayUpdated says, on the first start of a release an update put in
// place by itself, that kakel was updated from version from, with what
// changed a click away.
func (a *app) sayUpdated(from string) {
	title := "kakel is updated to " + thisVersion()
	if a.inTray() && a.traySet.Notify != nil {
		// No window shows the notice: the tray tells, and About has
		// what's new.
		_ = a.traySet.Notify(title, "From "+from+". About kakel says what's new.")
		return
	}
	n := Notice{Title: title, Body: "From " + from + ".", Kind: NoticeWorked}
	if a.updateWins != nil {
		n.Action, n.On = "What's New", ShowWhatsNew{From: from}
	}
	a.post(n)
}

// showWhatsNew opens the window with what the releases after from
// changed, up to this one.
func (a *app) showWhatsNew(from string) error {
	if a.updateWins == nil {
		return errors.New("what's new shows in a window of its own, which can't open here")
	}
	return a.updateWins.ShowWhatsNew(from)
}

// showAbout opens the window about kakel, which checks for updates in
// place, and takes a newer release as the update window does.
func (a *app) showAbout() error {
	if a.updateWins == nil {
		a.notify("kakel "+thisVersion(), "", "")
		return nil
	}
	o := install.About{Quit: a.quitForUpdate}
	if exe, err := executable(); err == nil {
		if _, to, err := install.Where(installer()); err != nil || !samePath(exe, to) {
			o.Exe = exe
		}
	}
	return a.updateWins.ShowAbout(o)
}

// showUpdateWindow opens gunim's update window on newest, put in place
// already with ready, and reports whether it opened.
func (a *app) showUpdateWindow(newest install.Release, ready bool) bool {
	if a.updateWins == nil {
		return false
	}
	exe, err := executable()
	if err != nil {
		return false
	}
	u := install.Update{Release: newest, Ready: ready, Quit: a.quitForUpdate}
	if _, to, err := install.Where(installer()); err != nil || !samePath(exe, to) {
		// A copy that is not installed updates itself where it is.
		u.Exe = exe
	}
	if err := a.updateWins.ShowUpdate(u); err != nil {
		log.Printf("couldn't open the update window: %v", err)
		return false
	}
	return true
}

// quitForUpdate ends kakel, asking first as Exit does, for the restart
// into an update: the update's new copy waits for it, and starts once
// it has ended. It runs off the program's goroutine.
func (a *app) quitForUpdate() error {
	select {
	case a.events <- func() {
		// The new copy starts kakel itself.
		restartInto = ""
		a.askToQuit()
	}:
		return nil
	case <-a.ctx.Done():
		return nil
	}
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
	ready := newest.Version == a.staged
	if (ready || a.writable()) && a.showUpdateWindow(newest, ready) {
		return
	}
	if ready {
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
	picks := map[string]bool{install.PickDesktop: in.Desktop, install.PickAutostart: in.Autostart, install.PickUpdates: in.AutoUpdate}
	got, err := s.Install(a.ctx, picks, nil)
	if err != nil {
		return err
	}
	if a.settings != nil {
		_ = a.settings.PutUpdates(string(got.Updates))
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

// toggleFolders opens folders with the installed kakel, or no longer.
func (a *app) toggleFolders() error {
	in, err := install.Find(installer())
	if err != nil {
		return err
	}
	return install.Change(installer(), map[string]bool{install.PickFolders: !in.Chose(install.PickFolders)})
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
