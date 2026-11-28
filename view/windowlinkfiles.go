package view

import (
	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/guest"
)

// The files of a shared window, as its dialog and its cards offer them:
// what the guest may do with them, and the folder they are in.

// filesMays are what a guest may do with the files, as a choice offers
// them, in the order of guest.FilesMay.
var filesMays = []string{"None", "Look", "Change"}

// linkFiles is the choice of what a guest may do with the files, and the
// folder, which fades from one to the next, with Choose… beside it.
type linkFiles struct {
	may    *widget.Segmented
	folder *widget.Pages
	choose *widget.Button
	// path is the folder shown, "" for none.
	path string
	// row is the folder with Choose….
	row *widget.Flex
}

// newLinkFiles is the choice, with may and folder chosen. choose is what
// Choose… sends.
func newLinkFiles(may guest.FilesMay, folder string, choose gunim.Intent) *linkFiles {
	f := &linkFiles{may: widget.NewSegmented(filesMays...), folder: widget.NewPages(folderLabel("")), choose: widget.NewButton("Choose…")}
	f.may.Tooltip = "Files"
	f.choose.Tooltip = "Choose the folder to share"
	f.choose.OnClick = widget.Sends(choose)
	f.row = widget.Row(f.folder, f.choose).Grow(f.folder, 1)
	f.row.Cross, f.row.Gap = widget.CrossCenter, linkCardGap
	f.show(may, folder, nil)
	return f
}

// folderLabel is a folder as it is shown: its path, which is this
// computer's own, or None.
func folderLabel(path string) *widget.Label {
	if path == "" {
		return faintLabel("None")
	}
	l := selectable(path)
	l.MaxLines = 1
	return l
}

// show shows may and folder, with no files for no folder.
func (f *linkFiles) show(may guest.FilesMay, folder string, u *gunim.UI) {
	if folder != f.path {
		f.path = folder
		f.folder.Show(folderLabel(folder), geom.Point{}, u)
	}
	if folder == "" {
		may = guest.FilesNone
	}
	if i := int(may); f.may.Selected() != i {
		f.may.SetSelected(i, u)
	}
	f.may.Disabled = folder == ""
}

// chosen is what the guest may do, as chosen.
func (f *linkFiles) chosen() guest.FilesMay {
	if f.path == "" {
		return guest.FilesNone
	}
	return guest.FilesMay(f.may.Selected())
}

// focusables are the parts Tab moves through.
func (f *linkFiles) focusables() []gunim.Node {
	if f.may.Disabled {
		return []gunim.Node{f.choose}
	}
	return []gunim.Node{f.may, f.choose}
}
