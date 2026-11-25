package term

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/marrasen/kakel/session"
	"github.com/marrasen/kakel/ui"
	"github.com/marrasen/kakel/vt"
)

// followed keeps what a follower is told, in order.
type followed struct {
	mu   sync.Mutex
	told []string
	last session.Screen
}

func (f *followed) add(s string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.told = append(f.told, s)
}

func (f *followed) Screen(cols, rows int, p []byte) {
	f.add(fmt.Sprintf("screen %dx%d", cols, rows))
	f.mu.Lock()
	f.last = session.Screen{Cols: cols, Rows: rows, Draw: p}
	f.mu.Unlock()
}
func (f *followed) Write(p []byte) { f.add("write " + string(p)) }
func (f *followed) Stale()         { f.add("stale") }
func (f *followed) Ended()         { f.add("ended") }
func (f *followed) Started()       { f.add("started") }

// take is what it was told since the last take.
func (f *followed) take() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	told := f.told
	f.told = nil
	return told
}

// A follower hears everything that changes the screen, in its order, and
// stays through the program ending and another being started.
func TestAFollowerHearsWhatChangesTheScreen(t *testing.T) {
	term, f := newTestTerm(t, 20, 4, Config{})
	f.feed(t, term, "before")
	w := &followed{}
	stop := term.Follow(w)
	want := func(when string, want ...string) {
		t.Helper()
		if got := w.take(); !slices.Equal(got, want) {
			t.Errorf("%s it is told %q, want %q", when, got, want)
		}
	}
	want("at first", "screen 20x4")
	f.feed(t, term, "hello")
	want("of output", "write hello")
	term.Layout(ui.Size{Cols: 30, Rows: 5})
	f.feed(t, term, "after")
	want("of a size", "stale", "write after")
	term.CatchUp(w)
	want("to catch up", "screen 30x5")
	term.Hold(40, 6)
	want("of a held size", "stale")
	term.Release()
	want("of a size given back", "stale")
	term.Layout(ui.Size{Cols: 30, Rows: 5})
	want("of the same size")
	term.Say("the window says")
	want("of what the window says", "stale")
	pal := vt.DefaultPalette()
	pal.BG.R++
	term.SetPalette(pal)
	want("of new colours", "stale")

	endProgram(t, term, f)
	waitFor(t, func() bool { w.mu.Lock(); defer w.mu.Unlock(); return len(w.told) > 0 })
	want("of the end", "ended")
	late := &followed{}
	term.Follow(late)
	if got := late.take(); !slices.Equal(got, []string{"screen 30x5", "ended"}) {
		t.Errorf("one that comes after the end is told %q", got)
	}
	next := restart(t, term)
	want("of another program", "started", "stale")
	next.feed(t, term, "back")
	want("of its output", "write back")

	stop()
	stop()
	next.feed(t, term, "more")
	term.CatchUp(w)
	term.Layout(ui.Size{Cols: 33, Rows: 5})
	want("once it has stopped")
}

// A follower that comes while the program is part way through an image is
// given the screen again once the image is there.
func TestAFollowerMidImageGetsTheImage(t *testing.T) {
	term, f := newTestTerm(t, 40, 10, Config{})
	img := anInlineImage(t, 10, 4)
	// Past what is sent of a sequence that has not ended.
	body := strings.Index(img, ":") + 1
	pad := strings.Repeat(" ", 200<<10)
	img = img[:body] + pad + img[body:]
	for len(img) > 60<<10 {
		f.feed(t, term, img[:30<<10])
		img = img[30<<10:]
	}
	w := &followed{}
	defer term.Follow(w)()
	// Followed as a guest's terminal does.
	there, _ := newTestTerm(t, 40, 10, Config{})
	there.mu.Lock()
	there.term.Adopt(w.last.Cols, w.last.Rows, w.last.Draw)
	there.mu.Unlock()
	w.take()
	for len(img) > 0 {
		n := min(len(img), 30<<10)
		f.feed(t, term, img[:n])
		img = img[n:]
	}
	if len(term.Images()) != 1 {
		t.Fatal("the pane has no image")
	}
	told := w.take()
	if len(told) == 0 || told[len(told)-1] != "stale" {
		t.Fatalf("it is told %d things, none that the screen changed", len(told))
	}
	there.mu.Lock()
	for _, s := range told {
		if b, ok := strings.CutPrefix(s, "write "); ok {
			_, _ = there.term.Write([]byte(b))
		}
	}
	there.mu.Unlock()
	if got := there.AllText(); strings.TrimSpace(got) != "" {
		t.Errorf("the rest of the image was printed: %.80q", got)
	}
	term.CatchUp(w)
	there.mu.Lock()
	there.term.Adopt(w.last.Cols, w.last.Rows, w.last.Draw)
	there.mu.Unlock()
	if len(there.Images()) != 1 {
		t.Error("the image never came")
	}
}

// screens is a session that hands over whole screens.
type screens struct {
	*fakeSession
	next chan *session.Screen
}

func (s screens) ReadScreen(p []byte) (int, *session.Screen, error) {
	select {
	case scr := <-s.next:
		return 0, scr, nil
	case b, ok := <-s.out:
		if !ok {
			return 0, nil, io.EOF
		}
		return copy(p, b), nil, nil
	}
}

// A screen stands in place of all that came before it, an update held back
// among it, at its own size, and the selection goes with the text.
func TestAScreenStandsInPlaceOfAllBefore(t *testing.T) {
	s := screens{newFakeSession(), make(chan *session.Screen)}
	term, err := New(Config{Session: s, Size: ui.Size{Cols: 20, Rows: 4}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = term.Close() }()
	term.Layout(ui.Size{Cols: 20, Rows: 4})
	s.feed(t, term, "old text\x1b[?2026hheld back")
	draw(term, 20, 4)
	term.SelectAll()
	s.next <- &session.Screen{Cols: 12, Rows: 3, Draw: []byte("\x1b[H\x1b[2Jnew\x1b[?1")}
	waitFor(t, func() bool { return strings.Contains(term.Text(), "new") })
	// On from the middle of the sequence the screen ended in.
	s.feed(t, term, "h there")
	if got := term.Text(); !strings.HasPrefix(got, "new there\n") || strings.Contains(got, "held") {
		t.Errorf("the screen is %q", got)
	}
	draw(term, 12, 3)
	if got := term.Size(); got != (ui.Size{Cols: 12, Rows: 3}) {
		t.Errorf("the size is %v", got)
	}
	if got := term.SelectionText(); got != "" {
		t.Errorf("%q is selected", got)
	}
	if n := s.sizeCount(); n != 0 {
		t.Errorf("the session was given %d sizes", n)
	}
	// The end of an update that was dropped, and one of its own.
	s.feed(t, term, "\x1b[?2026l\r\n\x1b[?2026hframe\x1b[?2026l")
	waitFor(t, func() bool { return strings.Contains(term.Text(), "frame") })
}
