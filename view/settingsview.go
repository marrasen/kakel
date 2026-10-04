package view

import (
	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/look"
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

// settingsDialog sets how kakel tells of what happens, by sounds and by
// rings around the window, and whether windows take the system's
// title bar. The events are a table: a row each, with a column for its
// sound and one for its rings.
func (w *Window) settingsDialog(u *gunim.UI) {
	cell := func(n gunim.Node, width float32) gunim.Node { return widget.NewSized(n, width, 0) }
	row := func(name gunim.Node, sound, ring gunim.Node) gunim.Node {
		r := widget.Row(cell(name, settingsName), cell(sound, settingsCheck), cell(ring, settingsCheck))
		r.Cross = widget.CrossCenter
		return r
	}
	check := func(on bool, tip string) *widget.Checkbox {
		c := widget.NewCheckbox("")
		c.On, c.Tooltip = on, tip
		return c
	}
	faint := func(text string) gunim.Node {
		l := widget.NewLabel(text)
		l.Color = look.Faint
		return l
	}
	rows := []gunim.Node{row(widget.NewLabel(""), faint("Sound"), faint("Rings"))}
	iface := check(w.sounds.Interface, "Sound: buttons, menus and switches")
	rows = append(rows, row(widget.NewLabel("Buttons, menus and switches"), iface, widget.NewLabel("")))
	var sounds, rings []*widget.Checkbox
	for _, r := range alertRows {
		snd := check(*r.field(&w.sounds), "Sound: "+r.title)
		rng := check(*r.field(&w.rings), "Rings: "+r.title)
		sounds, rings = append(sounds, snd), append(rings, rng)
		rows = append(rows, row(widget.NewLabel(r.title), snd, rng))
	}
	table := widget.Column(rows...)
	system := widget.NewCheckbox("Use the system's title bar")
	system.On = w.systemTitleBar
	d := widget.NewDialog("Settings")
	d.Body = widget.Column(
		table,
		widget.NewSized(widget.NewLabel(""), 0, 8),
		system,
		faint("The title bar your window manager draws, with its own buttons, for the windows opened from now on."),
	)
	d.SetButtons("Save", "Cancel")
	d.OnAccept = func() gunim.Intent {
		in := app.SaveLook{SystemTitleBar: system.On}
		in.Sounds.Interface = iface.On
		for i, r := range alertRows {
			*r.field(&in.Sounds) = sounds[i].On
			*r.field(&in.Rings) = rings[i].On
		}
		return in
	}
	d.Dismiss = app.DialogClosed{}
	w.openDialog(d, u)
}
