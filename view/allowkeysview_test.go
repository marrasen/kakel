package view

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/serve"
)

// The serving dialog lists the keys that may connect, and offers to add
// one, pasted or of this machine's, and to take one off; the dialog
// opens again once the change has been tried.
func TestTheServingDialogAddsAndRemovesKeys(t *testing.T) {
	win, _, publish := windowStage(t)
	s := app.Serving{
		Keys:    []serve.AllowedKey{{Name: "laptop", Fingerprint: "SHA256:abcdefghijklmnopqrstuvwxyz", Type: "ssh-ed25519"}},
		Allowed: []string{"laptop"},
		Here:    []app.LocalKey{{Name: "me@desk", Path: "/home/me/.ssh/id_ed25519.pub"}},
	}
	publish(app.State{Serving: s})
	win.servingDialog(s, lastUI)
	var labels []string
	for _, f := range formOf(win.dialog.Body).Children() {
		if l, ok := f.(*widget.Label); ok {
			labels = append(labels, l.Text)
		}
	}
	if !slices.ContainsFunc(labels, func(l string) bool { return strings.HasPrefix(l, "laptop  ·  SHA256:abcdefghijk") }) {
		t.Fatalf("the dialog says %q", labels)
	}

	win.dialog.Close(lastUI)
	lastWindow.Frame(time.Second)
	win.addKeyDialogFrom(s, true, lastUI)
	var pick *widget.Dropdown
	for _, f := range formOf(win.dialog.Body).Children() {
		if d, ok := f.(*widget.Dropdown); ok {
			pick = d
		}
	}
	if pick == nil || !slices.Equal(pick.Items, []string{"Paste one below", "me@desk"}) {
		t.Fatalf("the keys offered are %+v", pick)
	}
	if got := win.dialog.Check(); !strings.Contains(got, "Paste a public key") {
		t.Fatalf("with nothing pasted, the dialog says %q", got)
	}
	pick.Selected = 1
	if got := win.dialog.Check(); got != "" {
		t.Fatalf("with a key picked, the dialog says %q", got)
	}
	if in := win.dialog.OnAccept(); in != (app.AllowKey{Path: "/home/me/.ssh/id_ed25519.pub"}) || !win.keysAsked {
		t.Fatalf("Add sends %+v, asked %v", in, win.keysAsked)
	}
	win.dialog.Close(lastUI)
	lastWindow.Frame(time.Second)

	// Tried: the serving dialog again.
	s.Edits++
	publish(app.State{Serving: s})
	lastWindow.Frame(time.Second)
	if win.keysAsked || win.dialog == nil || win.dialog.Title != "Serve This Window" {
		t.Fatalf("after the change, the dialog is %+v", win.dialog)
	}
}
