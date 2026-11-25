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
// or take up no room, which can make a name read as another.
func plain(s string, most int) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsSpace(r):
			return ' '
		case unicode.Is(unicode.Cc, r), unicode.Is(unicode.Cf, r), unicode.Is(unicode.Zl, r), unicode.Is(unicode.Zp, r):
			return -1
		case unicode.Is(unicode.Co, r), unicode.Is(unicode.Cs, r):
			return utf8.RuneError
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > most {
		s = strings.TrimSpace(string([]rune(s)[:most]))
	}
	return s
}
