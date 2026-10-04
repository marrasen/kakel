package app

import "github.com/marrasen/kakel/settings"

// Alerts says, for each kind of event, whether kakel tells of it by a
// sound or by rings around the window; see settings.Alerts.
type Alerts = settings.Alerts

// SaveLook keeps what the Settings dialog chose: the events kakel tells
// of by a sound and by rings, and whether windows take the system's
// title bar, which windows opened from then on do.
type SaveLook struct {
	Sounds, Rings  Alerts
	SystemTitleBar bool
}

// SoundSetter is told whether the interface sounds, and whether any
// sound is wanted at all, as kakel starts and each time that changes.
type SoundSetter func(iface, any bool)

// showLook puts the alerts and the title bar in the state, and tells the
// sounds, from the settings, or the defaults where they cannot be read.
func (a *app) showLook() {
	sounds, rings, system := settings.DefaultSounds, settings.DefaultRings, false
	if a.settings != nil {
		sounds, rings, system = a.settings.Sounds(), a.settings.Rings(), a.settings.SystemTitleBar()
	}
	a.st.Sounds, a.st.Rings, a.st.SystemTitleBar = sounds, rings, system
	if a.sound != nil {
		a.sound(sounds.Interface, anySound(sounds))
	}
}

// anySound reports whether any event plays a sound.
func anySound(s Alerts) bool {
	return s.Interface || s.Connected || s.Lost || s.Finished || s.Bell || s.Other
}

// saveLook keeps what the Settings dialog chose.
func (a *app) saveLook(in SaveLook) error {
	if a.settings == nil {
		return settings.ErrUnsaveable
	}
	if err := a.settings.PutLook(in.Sounds, in.Rings, in.SystemTitleBar); err != nil {
		return err
	}
	a.showLook()
	return nil
}
