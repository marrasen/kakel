package view

import (
	"github.com/marrasen/kakel/app"
)

// alertRow is one kind of event the Settings dialog asks about: what
// it is called, and its setting in an app.Alerts.
type alertRow struct {
	title string
	field func(*app.Alerts) *bool
}

// alertRows are the events kakel tells of, in the order the dialog
// lists them. Interface sounds only, so it comes first and stands
// apart.
var alertRows = []alertRow{
	{"Connected", func(a *app.Alerts) *bool { return &a.Connected }},
	{"Connection lost", func(a *app.Alerts) *bool { return &a.Lost }},
	{"A long command finishes", func(a *app.Alerts) *bool { return &a.Finished }},
	{"The bell rings", func(a *app.Alerts) *bool { return &a.Bell }},
	{"Other errors and work finished", func(a *app.Alerts) *bool { return &a.Other }},
}

// The widths of the Settings dialog's columns: the events' names, and
// each of their checkboxes.
const (
	settingsName  = 250
	settingsCheck = 64
)
