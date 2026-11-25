package pastecheck

import (
	"strings"
	"testing"
)

func TestLinesLeavesOutTheLastBreak(t *testing.T) {
	for s, want := range map[string]int{
		"ls":            1,
		"ls\n":          1,
		"ls\r\n":        1,
		"a\nb":          2,
		"a\r\nb\r\n":    2,
		"a\rb":          2,
		"a\n\nb":        3,
		"\n\n":          2,
		"one\ntwo\nthr": 3,
	} {
		if got := Lines(s); got != want {
			t.Errorf("Lines(%q) = %d, want %d", s, got, want)
		}
	}
	if Needed("ls -la\n") {
		t.Error("a line copied whole is asked about")
	}
	if !Needed(strings.Repeat("x", Bytes+1)) {
		t.Error("a paste over 5 KB goes straight in")
	}
}
