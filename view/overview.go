package view

import (
	"image/color"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/screen"
	"github.com/marrasen/kakel/ui"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"
)

// All Panes in a window of its own, over the whole of a monitor: each of
// kakel's windows shrinks from where it stands into a card, and the pane
// picked grows back into its window, which is in front as All Panes
// goes.

// overShade is how dark the screen behind the cards goes.
var overShade = color.NRGBA{A: 0x8c}

// SeeThrough is the theme th as All Panes' own window wears it: its
// background see-through, so the screen shows behind the cards.
func SeeThrough(th theme.Theme) theme.Theme {
	return th.With(theme.Set(gunim.WindowBackground, color.NRGBA{}))
}

// Overview is All Panes' own window's view.
type Overview struct {
	anim.Group
	shells *screen.Shells
	keys   *ui.Keymap
	sw     *switcher
	// shown says the switcher has been shown, and gone that it has gone
	// again: the window is on its way out.
	shown bool
	// dark is how dark the screen behind the cards is.
	dark *anim.Float
}

// NewOverview is All Panes' own window's view, drawing the panes from
// sh, and closed by its key in keys.
func NewOverview(sh *screen.Shells, keys *ui.Keymap) *Overview {
	o := &Overview{shells: sh, keys: keys, dark: anim.NewFloat(0)}
	o.Add(o.dark)
	return o
}

// Update shows st: the switcher, as the window opens, and the windows
// and panes as they change while it is open.
func (o *Overview) Update(st app.OverState, u *gunim.UI) {
	if next, unknown := keymapOf(st.Shortcuts); len(unknown) == 0 {
		// Its key closes it, as the user has it.
		o.keys.Become(next)
	}
	env := overEnv{shells: o.shells, keys: o.keys, fontSize: st.FontSize, faces: st.Font.Faces, titles: st.PaneTitles, machines: st.Machines}
	switch {
	case !o.shown && st.Close:
		// Asked to close before it showed.
		o.shown = true
		u.Send(o, app.OverviewDone{})
	case !o.shown && len(st.Windows) == 0:
		// Mounted, before the program has said what there is.
	case !o.shown:
		o.shown = true
		o.sw = newSwitcherOver(o, env, st.Windows, st.Panes, st.Focus, u)
		if len(o.sw.tiles) == 0 {
			o.sw = nil
			u.Send(o, app.OverviewDone{})
			return
		}
		u.Insert(o, o.sw)
		u.Focus(o.sw)
		o.sw.light(o.sw.hot, u)
		o.dark.Animate(1, widget.Settle.Get(u.Theme()))
	case o.sw != nil:
		o.sw.env = env
		o.sw.sync(st.Windows, st.Panes, u)
		if st.Close && o.sw != nil {
			o.sw.cancel(u)
		}
	}
	u.Invalidate()
}

// Handle implements [gunim.Handler]: the program hears when the window
// loses the keyboard, as to a click outside it, and closes it, and when
// it has it back.
func (o *Overview) Handle(e input.Event, u *gunim.UI) bool {
	switch e.(type) {
	case input.WindowFocusLost:
		u.Send(o, app.OverviewFocus{On: false})
	case input.WindowFocusGained:
		u.Send(o, app.OverviewFocus{On: true})
	}
	return false
}

// OutputArrived draws again: a shell of some pane wrote.
func (o *Overview) OutputArrived(u *gunim.UI) { u.Invalidate() }

// closeSwitcher has the switcher go; the window closes once it has.
func (o *Overview) closeSwitcher(u *gunim.UI) {
	if o.sw == nil {
		return
	}
	u.Remove(o.sw)
	o.sw = nil
	o.dark.Animate(0, widget.Settle.Get(u.Theme()))
	// The program closes it anyway, should it never finish going.
	u.Send(o, app.OverviewLeaving{})
}

// Layout implements [gunim.Node]: the switcher covers the window.
func (o *Overview) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	for k := range kids.All {
		k.Layout(gunim.Tight(c.Max))
		k.Place(geom.Point{})
	}
	return c.Max
}

// Paint implements [gunim.Node]: the screen behind the cards, darker as
// they come in. The window's own background is see-through, where the
// system lets a window be, so it is the screen itself that darkens.
func (o *Overview) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	shade := overShade
	shade.A = uint8(float32(shade.A) * min(max(o.dark.Value(), 0), 1))
	p.RRect(geom.Rect{Max: box.Point()}, 0, paint.Solid(shade))
	for k := range kids.All {
		k.Paint(p)
	}
}
