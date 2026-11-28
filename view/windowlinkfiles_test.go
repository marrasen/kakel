package view

import (
	"slices"
	"testing"
	"time"

	"github.com/marrasen/gunim"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/guest"
)

// A share's card has its files: no choice until there is a folder, Choose…
// asks for one, the folder fades in, and the choice sends what the guest
// may do.
func TestASharesCardHasItsFiles(t *testing.T) {
	in := isIn("l1", "Bob", "p1")
	win, st, publish := linkStage(t, in)
	win.chips.order[0].do(lastUI)
	settle()
	n, ok := win.linkBox.list.Row(widget.Key("l1:guest"))
	if !ok {
		t.Fatal("no card")
	}
	c := n.(*linkCard)
	if !c.filesRow.Open() || !c.folderRow.Open() || !c.files.may.Disabled || c.files.may.Selected() != int(guest.FilesNone) {
		t.Fatal("with no folder, files can be chosen")
	}
	sentIntents()
	lastUI.Focus(c.files.choose)
	hit(gi.KeyEnter)
	if got := sentIntents(); !slices.Contains(got, gunim.Intent(app.ChooseLinkFolder{Window: win.winID, ID: "l1"})) {
		t.Errorf("Choose… sent %+v", got)
	}

	was := c.files.folder.Page()
	in.Folder = "/home/anna/Reports"
	st.Links = []app.WindowLink{in}
	publish(st)
	going := 0
	for range 30 {
		if lastUI.Drawn(was) {
			going++
		}
		frames(1)
	}
	if c.files.may.Disabled || c.files.path != in.Folder || going < 3 {
		t.Fatalf("with a folder, the card shows %q, disabled %v, the old went in %d frames", c.files.path, c.files.may.Disabled, going)
	}
	if !slices.Contains(c.focusables(), gunim.Node(c.files.may)) {
		t.Error("Tab skips the choice")
	}
	lastUI.Focus(c.files.may)
	lastWindow.Input(gi.KeyPress{Key: gi.KeyRight, Time: time.Now()})
	frames(2)
	if got := sentIntents(); !slices.Contains(got, gunim.Intent(app.SetLinkFiles{ID: "l1", Files: guest.FilesLook})) {
		t.Errorf("Look sent %+v", got)
	}
	// As the program has it, whatever was chosen.
	in.Files = guest.FilesChange
	st.Links = []app.WindowLink{in}
	publish(st)
	settle()
	if c.files.may.Selected() != int(guest.FilesChange) {
		t.Errorf("Change shows as %d", c.files.may.Selected())
	}
}

// The dialog offers the folder the next share starts with, and makes the
// link with the files chosen.
func TestMakingALinkWithFiles(t *testing.T) {
	win, st, publish := linkStage(t)
	st.LinkSetup.Folders = map[int]string{win.winID: "/home/anna/Reports"}
	publish(st)
	settle()
	win.run("share.link", lastUI)
	settle()
	b := win.linkBox
	if b == nil || b.files.path != "/home/anna/Reports" || b.files.may.Disabled {
		t.Fatal("the dialog does not offer the folder")
	}
	sentIntents()
	lastUI.Focus(b.files.choose)
	hit(gi.KeyEnter)
	if got := sentIntents(); !slices.Contains(got, gunim.Intent(app.ChooseLinkFolder{Window: win.winID})) {
		t.Errorf("Choose… sent %+v", got)
	}
	b.files.may.SetSelected(int(guest.FilesChange), lastUI)
	lastUI.Focus(b.name)
	hit(gi.KeyEnter)
	want := app.ShareWindow{Window: win.winID, Name: "Anna", CanType: true, Relay: st.LinkSetup.Relay,
		Files: guest.FilesChange, Folder: "/home/anna/Reports"}
	if got := sentIntents(); !slices.Contains(got, gunim.Intent(want)) {
		t.Errorf("it sent %+v", got)
	}
}

// Once the dialog is open, its folder changes only as one is chosen: a
// Files pane going elsewhere leaves the folder the files were chosen for.
func TestTheDialogsFolderChangesOnlyByChoose(t *testing.T) {
	win, st, publish := linkStage(t)
	st.LinkSetup.Folders = map[int]string{win.winID: "/home/anna/Reports"}
	publish(st)
	settle()
	win.run("share.link", lastUI)
	settle()
	b := win.linkBox
	b.files.may.SetSelected(int(guest.FilesChange), lastUI)
	st.LinkSetup.Folders = map[int]string{win.winID: "/home/anna/elsewhere"}
	publish(st)
	settle()
	if b.files.path != "/home/anna/Reports" {
		t.Errorf("the folder followed the pane to %q", b.files.path)
	}
	st.LinkSetup.Folders = map[int]string{win.winID: "/home/anna/Chosen"}
	st.LinkSetup.Chosen = map[int]string{win.winID: "/home/anna/Chosen"}
	publish(st)
	settle()
	if b.files.path != "/home/anna/Chosen" {
		t.Errorf("the folder chosen shows as %q", b.files.path)
	}
}
