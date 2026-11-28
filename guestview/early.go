package guestview

import (
	"time"
	"unicode/utf8"

	"github.com/marrasen/gunim"
	gi "github.com/marrasen/gunim/input"
)

// What is typed in a pane that is not open yet, as one picked just now or
// one opened again, is kept until it is, and typed then: all of it, or
// none. The end of a line whose beginning went to nobody may be another
// command altogether, so once anything is lost, so is what comes after it
// without a pause, and the guest is told. The same goes for a line whose
// beginning went to a pane that has gone.

// The most that is kept, in keys and in bytes of pastes, and for how long.
const (
	mostEarlyKeys  = 200
	mostEarlyPaste = 64 << 10
	earlyFor       = 10 * time.Second
)

// deafFor is how long nothing may be typed for what is typed next to be
// taken for the beginning of something.
const deafFor = 800 * time.Millisecond

// typed is something typed: a key, text, or a paste.
type typed struct {
	ev    gi.Event
	paste string
}

// early is what was typed in a pane before it could take it.
type early struct {
	all         []typed
	keys, bytes int
	// stop calls off the end of the wait.
	stop func()
}

// add keeps what, and reports false where that is too much.
func (e *early) add(what typed) bool {
	n := 1
	if t, ok := what.ev.(gi.TextInput); ok {
		n = utf8.RuneCountInString(t.Text)
	}
	e.keys, e.bytes = e.keys+n, e.bytes+len(what.paste)
	if e.keys > mostEarlyKeys || e.bytes > mostEarlyPaste {
		return false
	}
	e.all = append(e.all, what)
	return true
}

// take takes out what there is.
func (e *early) take() []typed {
	all := e.all
	if e.stop != nil {
		e.stop()
	}
	*e = early{}
	return all
}

// shut reports whether the pane takes nothing typed yet.
func (v *paneView) shut() bool { return v.term == nil || v.w.st.Shut[v.id] }

// types types in the pane: now, or once it is open.
func (v *paneView) types(what typed, u *gunim.UI) {
	now := u.Now()
	switch {
	case !v.w.may():
		v.deaf = now.Add(deafFor)
		v.refused(u)
	case now.Before(v.deaf) && what.paste == "":
		v.deaf = now.Add(deafFor)
		v.w.p.not(notTyped, typeAgain, u)
	case v.shut():
		v.w.unfollow(u)
		if !v.early.add(what) {
			v.lose(u)
			return
		}
		if v.early.stop == nil {
			v.early.stop = u.After(earlyFor, func(u *gunim.UI) {
				v.early.stop = nil
				if len(v.early.all) > 0 {
					v.lose(u)
				}
			})
		}
	default:
		v.w.unfollow(u)
		v.do(what, u)
	}
}

// do types in the pane's terminal.
func (v *paneView) do(what typed, u *gunim.UI) {
	if what.paste != "" {
		v.term.Paste(what.paste)
		return
	}
	v.term.Term.Handle(what.ev, u)
}

// send types what was kept, once the pane is open.
func (v *paneView) send(u *gunim.UI) {
	if len(v.early.all) == 0 || !v.w.may() || v.shut() {
		return
	}
	for _, what := range v.early.take() {
		v.do(what, u)
	}
}

// lose drops what was kept, and what comes after it, and says so.
func (v *paneView) lose(u *gunim.UI) {
	v.early.take()
	v.deaf = u.Now().Add(deafFor)
	v.w.p.said(notTyped, typeAgain, u)
}

// cut hears that the connection went, or the guest may type no more: what
// was kept is for nobody.
func (v *paneView) cut(u *gunim.UI) {
	if len(v.early.all) > 0 {
		v.lose(u)
	}
}

// refused hints at why what was typed went nowhere.
func (v *paneView) refused(u *gunim.UI) {
	if !v.w.st.Lost {
		v.w.hint(v, u)
	}
}
