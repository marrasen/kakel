package termview

import (
	"iter"
	"time"

	"github.com/marrasen/gunim"
)

// Output has a window's terminals copy their screens as their programs
// write: once a frame, however often word comes that they wrote.
type Output struct {
	// wrote is set while word of output has come that is yet to be
	// copied, and retrying while a copy that found a screen still being
	// written is on its way to another.
	wrote    *gunim.UI
	retrying bool
}

// Arrived takes word that programs of the window's terminals wrote: the
// window draws again, and Copy copies their screens as it does.
func (o *Output) Arrived(u *gunim.UI) {
	o.wrote = u
	u.Invalidate()
}

// Copy has terms copy their screens, if word has come that programs
// wrote. A window calls it as it lays out.
func (o *Output) Copy(terms iter.Seq[*Term]) {
	u := o.wrote
	if u == nil {
		return
	}
	o.wrote = nil
	late := false
	for t := range terms {
		t.Sync()
		t.LookAgain(u)
		// The program may have asked for a blinking cursor just now,
		// as an editor does leaving a mode with a steady one.
		t.Blink(u)
		late = late || t.sh.T.Dirty()
	}
	if late && !o.retrying {
		// A screen was still being written, and shows as it was.
		// Its writer says when it is done; a key press or a theme,
		// which say nothing, are caught a moment later.
		o.retrying = true
		u.After(4*time.Millisecond, func(u *gunim.UI) {
			o.retrying = false
			o.Arrived(u)
		})
	}
}
