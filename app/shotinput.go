package app

import (
	"context"
	"time"

	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"

	"github.com/marrasen/kakel/steps"
)

// A screenshot script's keys and pointer arrive as a keyboard and a
// mouse send them, so what a script does is what a person does: a key
// that types sends its press, marked as typing, then the text, then
// its release, and a click moves the pointer there first.

// usKey is a key of a US keyboard that types: what it types alone and
// with Shift.
type usKey struct {
	key            gi.Key
	plain, shifted rune
}

// usKeys are the keys of a US keyboard that type.
var usKeys = func() []usKey {
	out := []usKey{{gi.KeySpace, ' ', ' '}}
	for i := range 26 {
		out = append(out, usKey{gi.KeyA + gi.Key(i), rune('a' + i), rune('A' + i)})
	}
	for i, shifted := range []rune(")!@#$%^&*(") {
		out = append(out, usKey{gi.Key0 + gi.Key(i), rune('0' + i), shifted})
	}
	return append(out, []usKey{
		{gi.KeyMinus, '-', '_'}, {gi.KeyEqual, '=', '+'}, {gi.KeyLeftBracket, '[', '{'},
		{gi.KeyRightBracket, ']', '}'}, {gi.KeyBackslash, '\\', '|'}, {gi.KeySemicolon, ';', ':'},
		{gi.KeyApostrophe, '\'', '"'}, {gi.KeyGraveAccent, '`', '~'}, {gi.KeyComma, ',', '<'},
		{gi.KeyPeriod, '.', '>'}, {gi.KeySlash, '/', '?'},
	}...)
}()

// keyFor returns the key that types r on a US keyboard, and whether
// Shift is held for it; false where no key types it, as for é.
func keyFor(r rune) (gi.Key, bool, bool) {
	for _, k := range usKeys {
		switch r {
		case k.plain:
			return k.key, false, true
		case k.shifted:
			return k.key, true, true
		}
	}
	return 0, false, false
}

// typedBy returns what press types, and its character without Shift:
// nothing typed with Control, Alt or Super held, as those make it a
// shortcut, and nothing for a key that types nothing.
func typedBy(press gi.KeyPress) (text string, char rune) {
	for _, k := range usKeys {
		if k.key != press.Key {
			continue
		}
		if press.Mods&(gi.ModControl|gi.ModAlt|gi.ModSuper) != 0 {
			return "", k.plain
		}
		if press.Mods&gi.ModShift != 0 {
			return string(k.shifted), k.plain
		}
		return string(k.plain), k.plain
	}
	return "", 0
}

// pressKey sends what a keyboard sends for press: the press, marked as
// typing when it types, the text it types, and the release.
func pressKey(send func(gi.Event) error, press gi.KeyPress) error {
	text, char := typedBy(press)
	press.Char, press.Typed, press.Time = char, text != "", time.Now()
	if err := send(press); err != nil {
		return err
	}
	if text != "" {
		if err := send(gi.TextInput{Text: text, Time: time.Now()}); err != nil {
			return err
		}
	}
	return send(gi.KeyRelease{Key: press.Key, Mods: press.Mods, Time: time.Now()})
}

// typeText types s as a keyboard would, a key at a time: a character
// no key types arrives as text alone, as from an input method.
func typeText(send func(gi.Event) error, s string) error {
	for _, r := range s {
		k, shift, ok := keyFor(r)
		if !ok {
			if err := send(gi.TextInput{Text: string(r), Time: time.Now()}); err != nil {
				return err
			}
			continue
		}
		var mods gi.Mods
		if shift {
			mods = gi.ModShift
		}
		if err := pressKey(send, gi.KeyPress{Key: k, Mods: mods}); err != nil {
			return err
		}
	}
	return nil
}

// modsOf are the modifiers a pointer step holds.
func modsOf(names []string) gi.Mods {
	var m gi.Mods
	for _, n := range names {
		switch n {
		case "ctrl":
			m |= gi.ModControl
		case "shift":
			m |= gi.ModShift
		case "alt":
			m |= gi.ModAlt
		case "super":
			m |= gi.ModSuper
		}
	}
	return m
}

// buttonOf is the mouse button a click step presses.
func buttonOf(b steps.Button) gi.Button {
	switch b {
	case steps.Secondary:
		return gi.ButtonSecondary
	case steps.Middle:
		return gi.ButtonMiddle
	}
	return gi.ButtonPrimary
}

// dragSteps is how many moves a drag makes on its way, and dragGap the
// time between them: enough for what watches a drag, as a tab held
// over another, to see it go by.
const (
	dragSteps = 12
	dragGap   = 16 * time.Millisecond
)

// point sends what a mouse sends for a pointer step.
func point(ctx context.Context, in func(gi.Event) error, step steps.Step) error {
	at := geom.Pt(step.At.X, step.At.Y)
	mods := modsOf(step.Mods)
	// The pointer goes there first, as a hand moves the mouse before
	// pressing: what lights under it does, and a press lands where it is.
	if err := in(gi.PointerMove{Pos: at, Mods: mods, Time: time.Now()}); err != nil {
		return err
	}
	switch step.Kind {
	case steps.Click:
		b := buttonOf(step.Button)
		for n := 1; n <= step.Clicks; n++ {
			if err := in(gi.PointerDown{Pos: at, Button: b, Mods: mods, Clicks: n, Time: time.Now()}); err != nil {
				return err
			}
			if err := in(gi.PointerUp{Pos: at, Button: b, Mods: mods, Time: time.Now()}); err != nil {
				return err
			}
		}
	case steps.Drag:
		if err := in(gi.PointerDown{Pos: at, Button: gi.ButtonPrimary, Mods: mods, Clicks: 1, Time: time.Now()}); err != nil {
			return err
		}
		to := geom.Pt(step.To.X, step.To.Y)
		for i := 1; i <= dragSteps; i++ {
			k := float32(i) / dragSteps
			p := geom.Pt(at.X+(to.X-at.X)*k, at.Y+(to.Y-at.Y)*k)
			if err := in(gi.PointerMove{Pos: p, Mods: mods, Time: time.Now()}); err != nil {
				return err
			}
			select {
			case <-time.After(dragGap):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if err := in(gi.PointerUp{Pos: to, Button: gi.ButtonPrimary, Mods: mods, Time: time.Now()}); err != nil {
			return err
		}
	case steps.Scroll:
		// As the desktop driver turns a notch into distance.
		const line = 40
		n := step.Notches
		if err := in(gi.Scroll{Pos: at, Delta: geom.Pt(0, n*line), Notches: geom.Pt(0, n), Mods: mods, Time: time.Now()}); err != nil {
			return err
		}
	case steps.Down:
		return in(gi.PointerDown{Pos: at, Button: gi.ButtonPrimary, Mods: mods, Clicks: 1, Time: time.Now()})
	case steps.Up:
		return in(gi.PointerUp{Pos: at, Button: gi.ButtonPrimary, Mods: mods, Time: time.Now()})
	case steps.Move:
	}
	return nil
}
