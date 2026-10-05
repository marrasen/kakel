package app

import (
	"context"
	"errors"
	"image"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/marrasen/gunim/install"

	"github.com/marrasen/kakel/appicon"
	"github.com/marrasen/kakel/conf"
	"github.com/marrasen/kakel/settings"
	"github.com/marrasen/kakel/single"
)

// Installing kakel is gunim's install package: a kakel started from
// anywhere but its own folder opens the installer, which puts it where
// a user's programs go, for that user alone and with no administrator,
// with a Start menu entry or a desktop file, an entry the system lists
// to take it away again, and, when asked, a shortcut on the desktop and
// a start with the computer, into the tray. `kakel -install` and
// `kakel -uninstall` do the same from a shell. kakel keeps its own
// updates: they are its Updates setting's to tell of, fetch, or leave.

// autoUpdate is the installer's offer to update kakel by itself, which
// sets the Updates setting.
const autoUpdate = "kakel.autoupdate"

// Installer is how kakel installs itself, for gunim's installer.
func Installer() install.App {
	return install.App{
		Name:        ProgramName,
		ID:          ProgramName,
		Version:     releaseVersion(),
		Publisher:   "Marcus Johansson",
		Description: "Terminals, files and tunnels on your machines",
		URL:         "https://github.com/marrasen/kakel",
		IconFunc:    func() image.Image { return appicon.Draw(256) },
		Categories:  "System;TerminalEmulator;",
		Autostart:   &install.Autostart{Args: []string{"-tray"}, Label: "Start kakel with the computer, in the tray"},
		Choices: []install.Choice{{
			Key: autoUpdate, Label: "Update automatically", Detail: "A new release is put in place for the next start.",
			On: updatesSetting() == settings.UpdatesInstall, Current: true,
		}},
		Updates:      install.GitHub{Repo: "marrasen/kakel", Asset: releaseAsset},
		NoAutoUpdate: true,
		Formerly:     formerly(),
		Data:         dataDirs(),
		Installed:    installed,
		Uninstalling: func(context.Context, install.Installation) error { removeConPTY(); return nil },
		Quit:         quitRunning,
	}
}

// releaseVersion is this build's version when it is a release, and ""
// for a build from a working tree, which then runs as it is: it neither
// opens the installer nor looks for updates.
func releaseVersion() string {
	if v := thisVersion(); isRelease(v) {
		return v
	}
	return ""
}

// releaseAsset names a release's archive for a system, as `make
// release` names it and as kakel's releases have always been named, so
// an older kakel's own updates and the install scripts find it too.
func releaseAsset(version, goos, goarch string) string {
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return ProgramName + "_v" + strings.TrimPrefix(version, "v") + "_" + goos + "_" + goarch + ext
}

// formerly is where kakel installed itself before gunim's installer:
// ~/.local/bin/kakel on Linux. On Windows it went where it goes now.
func formerly() []string {
	if runtime.GOOS != "linux" {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(home, ".local", "bin", ProgramName)}
}

// dataDirs are the folders kakel keeps the user's settings, saved
// servers and keys in, which an uninstall offers to take too.
func dataDirs() []string {
	var out []string
	if d, err := conf.Dir(); err == nil {
		out = append(out, d)
	}
	if d, err := conf.Private(); err == nil && !slices.Contains(out, d) {
		out = append(out, d)
	}
	return out
}

// updatesSetting is the Updates setting as it stands, for the installer
// to start its offer from.
func updatesSetting() string {
	path, err := settings.Path()
	if err != nil {
		return settings.UpdatesNotify
	}
	s, err := settings.Load(path)
	if err != nil {
		return settings.UpdatesNotify
	}
	return s.Updates()
}

// installed keeps what the installer's offer to update by itself says
// in the Updates setting. Unticked, updates are told of, unless they
// were off.
func installed(_ context.Context, in install.Installation) error {
	path, err := settings.Path()
	if err != nil {
		return err
	}
	s, err := settings.Load(path)
	if err != nil {
		return err
	}
	want := s.Updates()
	switch {
	case in.Chose(autoUpdate):
		want = settings.UpdatesInstall
	case want == settings.UpdatesInstall:
		want = settings.UpdatesNotify
	}
	if want == s.Updates() {
		return nil
	}
	return s.PutUpdates(want)
}

// quitRunning ends the kakel running, as before an uninstall, asking
// it as kakel -quit does, and waits for it to go.
func quitRunning(ctx context.Context) error {
	dir, err := settings.Dir()
	if err != nil {
		return err
	}
	taken, _ := single.Hand(dir, single.Handover{Args: []string{"-quit"}})
	if !taken {
		return nil
	}
	for end := time.Now().Add(60 * time.Second); time.Now().Before(end); {
		if !single.Running(dir) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return errors.New("the kakel running didn't end; close it and try again")
}
