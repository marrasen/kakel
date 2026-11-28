package guestview

import (
	"strconv"
	"sync/atomic"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
)

// filesView is a Files pane's place on the page, which the program
// mounts the pane's file manager in. Each time it comes into the tree,
// as its tab comes to the front, it takes its ID again and says so, and
// the file manager is mounted there anew, as it was.
type filesView struct {
	v       *paneView
	id      gunim.ID
	folders uint32
}

// filesPlaces counts the places made, for their IDs.
var filesPlaces atomic.Int64

func newFilesView(v *paneView, folders uint32) *filesView {
	id := gunim.ID("kakel-files-place-" + strconv.FormatInt(filesPlaces.Add(1), 10))
	return &filesView{v: v, id: id, folders: folders}
}

// Layout implements [gunim.Node]: the file manager fills the place.
func (h *filesView) Layout(c gunim.Constraints, f gunim.Frame, kids gunim.Children) geom.Size {
	if u := f.UI(); u != nil && u.Presence(h) != gunim.Exiting && u.Mounted(h.id) != h {
		if err := u.SetID(h, h.id); err == nil {
			pane, folders, id := h.v.id, h.folders, h.id
			u.After(0, func(u *gunim.UI) {
				if u.Mounted(id) == h {
					u.Send(h.v.w.p, FilesPlaced{Pane: pane, Folders: folders, At: id})
				}
			})
		}
	}
	for k := range kids.All {
		k.Layout(gunim.Tight(c.Max))
		k.Place(geom.Point{})
	}
	return c.Max
}

// Paint implements [gunim.Node]. The file manager draws its own ground.
func (h *filesView) Paint(p *paint.Painter, _ gunim.Frame, _ geom.Size, kids gunim.Children) {
	for k := range kids.All {
		k.Paint(p)
	}
}

// Handle implements [gunim.Handler]: the keyboard coming into the file
// manager has the guest in its pane. Files dropped where the file
// manager takes none go nowhere.
func (h *filesView) Handle(e gi.Event, u *gunim.UI) bool {
	switch e.(type) {
	case gi.FocusEntered:
		h.v.entered(u)
	case gi.Drop:
		return true
	}
	return false
}

// Overhear implements [gunim.Overhearer]: a press in the file manager
// picks its pane, as one in a terminal does, and the guest follows the
// host no more.
func (h *filesView) Overhear(e gi.Event, u *gunim.UI) {
	if d, ok := e.(gi.PointerDown); ok && u.Presence(h) != gunim.Exiting {
		if r, drawn := u.Bounds(h); drawn && r.Contains(d.Pos) {
			h.v.w.picked(h.v.id, u)
		}
	}
}
