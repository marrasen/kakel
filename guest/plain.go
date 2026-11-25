package guest

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// plain is text the far end chose as it may be shown in a dialog, a
// title, or the Window Log: on one line, at most most characters, and
// with nothing in it that is acted on rather than read. It is serve.Plain
// for a line, less the characters that turn the direction of text round
// or show nothing, which can make a name read as another, and less the
// marks that pile up on a letter, or stand on none. Text with nothing to
// see is empty.
func plain(s string, most int) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsSpace(r):
			return ' '
		case unicode.Is(unicode.Cc, r), unicode.Is(unicode.Zl, r), unicode.Is(unicode.Zp, r), unseen(r):
			return -1
		case unicode.Is(unicode.Co, r), unicode.Is(unicode.Cs, r):
			return utf8.RuneError
		}
		return r
	}, s)
	// How many marks the letter before has, and a space takes none.
	marks := mostMarks
	s = strings.Map(func(r rune) rune {
		switch {
		case r == ' ':
			marks = mostMarks
		case !unicode.Is(unicode.M, r):
			marks = 0
		case marks == mostMarks:
			return -1
		default:
			marks++
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > most {
		s = strings.TrimSpace(string([]rune(s)[:most]))
	}
	return s
}

// mostMarks is how many marks a letter may have.
const mostMarks = 2

// unseen reports whether a character is drawn as nothing: those Unicode
// has a program without a use for them draw so, its
// Default_Ignorable_Code_Point, and two that fonts have as blanks.
func unseen(r rune) bool {
	const brailleBlank, nullNotehead = 0x2800, 0x1d159
	return unicode.In(r, unicode.Cf, unicode.Other_Default_Ignorable_Code_Point, unicode.Variation_Selector) ||
		r == brailleBlank || r == nullNotehead
}

// plainName is what a guest calls itself, as it may be shown: empty for one
// that gave no name, and noName for one with nothing to see in it.
func plainName(s string) string {
	name := plain(s, mostName)
	if name == "" && strings.TrimSpace(s) != "" {
		return noName
	}
	return name
}

// noName is what a guest is called whose name shows nothing.
const noName = "Guest"
