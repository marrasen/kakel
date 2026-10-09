package view

import (
	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/icon"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/viewer"
	"github.com/marrasen/gunim/widget"
	"github.com/marrasen/kakel/app"
)

// viewerWidth is the widest the viewer opens, and viewerRoom the room
// it leaves round itself in the window.
const (
	viewerWidth = 1000
	viewerRoom  = 80
)

// showViews opens the viewer for each file the program sent the window
// that it has not shown.
func (w *Window) showViews(st app.State, u *gunim.UI) {
	for _, v := range st.Views {
		if v.ID <= w.viewedSeen {
			continue
		}
		w.viewedSeen = v.ID
		w.openViewer(v, u)
	}
}

// openViewer shows a file in a dialog over the window: code in its
// colours, Markdown rendered, a picture, or the bytes of anything else.
// A link in a document opens only as a link in a pane does.
func (w *Window) openViewer(v app.Viewed, u *gunim.UI) {
	id := v.ID
	view := viewer.New(v.Name, v.Data, viewer.Options{Cut: v.Cut, OnLink: func(url string) gunim.Intent {
		return app.ViewLink{ID: id, URL: url}
	}})
	view.GoTo(v.Line, u)
	where := widget.NewLabel(v.Where + ": " + v.Path)
	where.Color, where.Size, where.MaxLines, where.Selectable = widget.Placeholder, smallText, 1, true
	d := widget.NewDialog(v.Name)
	d.Icon = icon.FileText
	d.Width = max(320, min(viewerWidth, w.size.W-viewerRoom))
	col := widget.Column(where, &viewerBox{view: view, h: max(160, w.size.H*0.62)})
	col.Cross = widget.CrossStretch
	d.Body = col
	d.SetButtons("Close", "")
	d.OnAccept = widget.Sends(app.DialogClosed{})
	d.OnDismiss = widget.Sends(app.DialogClosed{})
	w.openDialog(d, u)
}

// viewerBox gives the viewer a height of its own in the dialog.
type viewerBox struct {
	view *viewer.View
	h    float32
}

// Children implements [gunim.Composite].
func (b *viewerBox) Children() []gunim.Node { return []gunim.Node{b.view} }

// Layout implements [gunim.Node].
func (b *viewerBox) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	s := geom.Sz(c.Max.W, b.h)
	k := kids.At(0)
	k.Layout(gunim.Tight(s))
	k.Place(geom.Point{})
	return s
}

// Paint implements [gunim.Node].
func (b *viewerBox) Paint(p *paint.Painter, _ gunim.Frame, _ geom.Size, kids gunim.Children) {
	kids.At(0).Paint(p)
}
