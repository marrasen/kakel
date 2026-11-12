package view

import (
	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/text"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/app"
)

// A layout's waiting pane: the place of a pane of the layout before it
// has started. It says what it waits for, over gunim's busy bar, and
// once that has failed, why, with Retry and Close. Each change of what
// it says fades across, gunim's pages carrying the one going out and
// the next in, and the buttons grow in under the reason. When the pane
// it waits for arrives, the stage keeps it over that pane while it
// fades away, so the one gives way to the other.

// waitView is the node a waiting pane shows.
type waitView struct {
	anim.Group
	w  *Window
	id string
	// pages holds what it says, and said is what that is.
	pages *widget.Pages
	said  app.Waiting
	// retry is the failed page's Retry, which takes the keyboard there,
	// and buttons the fold its buttons grow in by; both nil while it
	// waits.
	retry   *widget.Button
	buttons *widget.Fold
	// shown runs from 1 down to 0 as it leaves the stage, for it to fade
	// away over the pane that came in its place; ring shows round the
	// pane while Tab has put the keyboard on it, as round a chooser.
	shown, ring *anim.Float
}

// waitMost is the widest what a waiting pane says is set.
const waitMost = 440

// waitBar is how wide the busy bar is.
const waitBar = 220

func newWaitView(w *Window, id string) *waitView {
	v := &waitView{w: w, id: id, shown: anim.NewFloat(1), ring: anim.NewFloat(0)}
	v.Add(v.shown, v.ring)
	v.said = w.waits[id]
	v.pages = widget.NewPages(v.page(v.said))
	return v
}

// show has the pane say st, fading across from what it said, through u.
// The keyboard in the pane goes to what takes it on the page coming:
// Retry, or the pane itself while it waits again.
func (v *waitView) show(st app.Waiting, u *gunim.UI) {
	if st == v.said {
		return
	}
	v.said = st
	if u.Presence(v) == gunim.Exiting {
		// Off stage, as a tab behind another, or on its way out: out of
		// the tree, where nothing can be added to it. Its page is changed
		// at once, and read afresh as it comes back; nothing there has
		// the keyboard to move.
		v.pages.Show(v.page(st), geom.Point{}, nil)
		return
	}
	in := u.HasFocus(v)
	v.pages.Show(v.page(st), geom.Point{}, u)
	if in {
		u.Focus(v.focusTarget())
	}
	u.Invalidate()
}

// focusTarget is what takes the keyboard in the pane: Retry once it has
// failed, and the pane itself while it waits.
func (v *waitView) focusTarget() gunim.Node {
	if v.retry != nil {
		return v.retry
	}
	return v
}

// page is what the pane says for st: what it waits for over the busy
// bar, or why it failed, with the buttons.
func (v *waitView) page(st app.Waiting) gunim.Node {
	if st.Failed == "" {
		v.retry, v.buttons = nil, nil
		doing := widget.NewLabel(st.Doing)
		doing.Align = text.AlignCenter
		bar := widget.NewProgressBar()
		bar.Indeterminate = true
		return waitPage(doing, widget.NewSized(bar, waitBar, 0))
	}
	heading := widget.NewLabel(st.Failed)
	heading.Align = text.AlignCenter
	heading.Size = widget.HeadingSize
	kids := []gunim.Node{heading}
	if st.Why != "" {
		why := widget.NewLabel(st.Why)
		why.Align, why.Color, why.Selectable = text.AlignCenter, widget.Placeholder, true
		kids = append(kids, why)
	}
	retry := widget.NewButton("Retry")
	retry.Kind = widget.ButtonPrimary
	retry.OnClick = widget.Sends(app.RetryWaiting{Pane: v.id})
	closeB := widget.NewButton("Close")
	closeB.OnClick = widget.Sends(app.ClosePane{Pane: v.id})
	// The buttons grow in under the reason as the page comes.
	v.retry, v.buttons = retry, widget.NewFold(widget.Row(retry, closeB), false)
	v.buttons.Comes = true
	kids = append(kids, v.buttons)
	return waitPage(kids...)
}

// waitPage is a page of what a waiting pane says: kids, one under
// another, in the middle of the whole pane. Each page fills the pane,
// so the one going stays where it stood while the next comes, and what
// is under the buttons glides as they grow.
func waitPage(kids ...gunim.Node) gunim.Node {
	col := widget.Column(kids...)
	col.Justify, col.Cross = widget.JustifyCenter, widget.CrossCenter
	return col
}

// Children implements [gunim.Composite].
func (v *waitView) Children() []gunim.Node { return []gunim.Node{v.pages} }

// Focusable implements [gunim.Focusable]: the keyboard is in the pane
// while it waits, as in any other.
func (v *waitView) Focusable() bool { return true }

// Handle implements [gunim.Handler]: the pane taking the keyboard says
// so, as any pane does, and the ring follows Tab.
func (v *waitView) Handle(e input.Event, u *gunim.UI) bool {
	switch e := e.(type) {
	case input.FocusEntered:
		v.w.entered(v.id, u)
	case input.FocusRing:
		v.ring.Animate(anim.OnOff(e.On), widget.Quick.Get(u.Theme()))
		u.Invalidate()
		return true
	}
	return false
}

// Transition implements [gunim.Transitioner]: back on stage it shows at
// once, as the stage brings a pane in itself; leaving, it fades away
// with the theme's motion for going away, a little slower than the pane
// coming in clears, so its words are read going.
func (v *waitView) Transition(p gunim.Presence, f gunim.Frame) bool {
	switch p {
	case gunim.Entering:
		v.shown.Jump(1)
	case gunim.Exiting:
		v.shown.Animate(0, widget.Settle.Get(f.Theme))
	case gunim.Present:
	}
	return !v.shown.Active()
}

// Layout implements [gunim.Node]: the pages as tall as the pane and no
// wider than waitMost, in its middle.
func (v *waitView) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	box := c.Max
	k := kids.At(0)
	wide := min(waitMost, max(box.W-2*chooserPad, 0))
	k.Layout(gunim.Tight(geom.Sz(wide, box.H)))
	k.Place(geom.Pt((box.W-wide)/2, 0))
	return box
}

// Paint implements [gunim.Node].
func (v *waitView) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	if on := min(max(v.shown.Value(), 0), 1); on < 0.999 {
		if on < 0.001 {
			return
		}
		defer p.Layer(paint.LayerOpts{Bounds: geom.Rect{Max: box.Point()}, Opacity: on})()
	}
	p.RRect(geom.Rect{Max: box.Point()}, 0, paint.Solid(widget.Background.Get(f.Theme)))
	kids.At(0).Paint(p)
	if on := min(max(v.ring.Value(), 0), 1); on > 0.01 {
		ring := switcherRing.Get(f.Theme)
		ring.A = uint8(float32(ring.A) * on)
		p.RRectStroke(geom.Rect{Max: box.Point()}.Inset(geom.Uniform(3)), 6, paint.Fill{}, paint.Stroke{Width: 2, Color: ring})
	}
}
