package view

import (
	"testing"
	"time"

	"github.com/marrasen/kakel/internal/sessiontest"
	"github.com/marrasen/kakel/screen"
	"github.com/marrasen/kakel/vt"
)

// A terminal shown in All Panes is live though no window draws it, as
// none does while All Panes covers the windows.
func TestAMirrorShowsOutputNoWindowDrew(t *testing.T) {
	quiet := screen.Hooks{Output: func() {}, Title: func(string) {}, Exit: func() {}, Clipboard: func(string) {}}
	sh := screen.Open(sessiontest.NewPrinted([]byte("hello mirror")), vt.DefaultPalette(), quiet)
	t.Cleanup(func() { _ = sh.T.Close() })
	m := newMirror(sh)
	deadline := time.Now().Add(5 * time.Second)
	for {
		m.sync(time.Now())
		if m.copied && m.g.At(0, 0).Rune == 'h' && m.g.At(6, 0).Rune == 'm' {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the mirror shows %q", string([]rune{m.g.At(0, 0).Rune, m.g.At(1, 0).Rune}))
		}
		time.Sleep(10 * time.Millisecond)
	}
}
