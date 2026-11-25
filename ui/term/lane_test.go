package term

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

// slowProgram takes what is typed a read at a time, when the test says.
type slowProgram struct {
	*fakeSession
	reads chan int
	mu    sync.Mutex
	took  []byte
}

func (s *slowProgram) Write(p []byte) (int, error) {
	for done := 0; done < len(p); {
		n, ok := <-s.reads
		if !ok {
			n = len(p)
		}
		n = min(n, len(p)-done)
		s.mu.Lock()
		s.took = append(s.took, p[done:done+n]...)
		s.mu.Unlock()
		done += n
	}
	return len(p), nil
}

func (s *slowProgram) typed() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return bytes.Clone(s.took)
}

// However much a lane types, all of it arrives, in order, and what the
// user types meanwhile goes first, none of it lost.
func TestALaneNeitherLosesNorHoldsUp(t *testing.T) {
	s := &slowProgram{fakeSession: newFakeSession(), reads: make(chan int)}
	term, err := New(Config{Session: s})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = term.Close() }()
	l := term.Lane()
	text := []byte(strings.Repeat("åäö 漢字 ab", 100_000))
	go func() {
		for rest := text; len(rest) > 0; {
			if n := l.Type(rest); n > 0 {
				rest = rest[n:]
			} else {
				<-l.Room()
			}
		}
	}()
	for range 5 {
		s.reads <- 512
	}
	// The user's key, behind a program that has a megabyte to come.
	term.Send([]byte{3})
	for range 4 {
		s.reads <- 512
	}
	got := s.typed()
	if !bytes.Contains(got, []byte{3}) {
		t.Fatalf("the user's key waits behind %d bytes", len(got))
	}
	if i := bytes.IndexByte(got, 3); !bytes.HasPrefix(text, got[:i]) || !strings.HasPrefix(string(text[i:]), string([]rune(string(text[i : i+8])))[:1]) {
		t.Error("the user's key cut a character in two")
	}
	close(s.reads)
	waitFor(t, func() bool { return len(s.typed()) == len(text)+1 })
	if got := bytes.ReplaceAll(s.typed(), []byte{3}, nil); !bytes.Equal(got, text) {
		t.Error("what was typed arrived wrong")
	}
}

// What is dropped is typed no more, and a lane that is closed takes
// nothing.
func TestALaneDrops(t *testing.T) {
	s := &slowProgram{fakeSession: newFakeSession(), reads: make(chan int)}
	term, err := New(Config{Session: s})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = term.Close() }()
	l := term.Lane()
	if n := l.Type(bytes.Repeat([]byte("x"), 3*laneRoom)); n < laneRoom || n > laneRoom+laneChunk {
		t.Errorf("it took %d bytes", n)
	}
	s.reads <- 1
	l.Drop()
	if n := l.Type([]byte("after")); n != 5 {
		t.Errorf("there is room for %d", n)
	}
	close(s.reads)
	waitFor(t, func() bool { return bytes.HasSuffix(s.typed(), []byte("after")) })
	if n := len(s.typed()); n > 2*laneChunk+5 {
		t.Errorf("%d bytes were typed", n)
	}
	l.Close()
	if n := l.Type([]byte("no")); n != 0 {
		t.Error("a closed lane takes typing")
	}
}

// Two who type have a turn each.
func TestLanesTakeTurns(t *testing.T) {
	s := &slowProgram{fakeSession: newFakeSession(), reads: make(chan int)}
	term, err := New(Config{Session: s})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = term.Close() }()
	a, b := term.Lane(), term.Lane()
	a.Type(bytes.Repeat([]byte("a"), laneRoom))
	b.Type(bytes.Repeat([]byte("b"), laneRoom))
	for range 5 {
		s.reads <- laneChunk
	}
	if got := s.typed(); !bytes.ContainsRune(got, 'a') || !bytes.ContainsRune(got, 'b') {
		t.Error("one has waited for all of the other")
	}
	close(s.reads)
	time.Sleep(time.Millisecond)
}

func TestTypingIsCutBetweenKeys(t *testing.T) {
	for in, want := range map[string]int{
		"abcdefgh":                     6,
		"abcdé":                        6,
		"abcdeé":                       5,
		"abcd\x1b[A":                   4,
		"abc\x1b[A":                    6,
		"abc\x1b\x1b[A":                3,
		"a漢漢漢":                         4,
		"\x1b[200~text":                6,
		"abc":                          3,
		"abcd\x1bx":                    6,
		"abcde\x1bx":                   5,
		"abcd\x1bOP":                   4,
		"a\x1b[M 漢":                    1,
		"\x80\x80\x80\x80\x80\x80\x80": 6,
	} {
		if got := CutTyped([]byte(in), 6); got != want {
			t.Errorf("%q is cut at %d, want %d", in, got, want)
		}
	}
}

// A character or a key that comes in two goes to the program in one, with
// what the user types before it or after.
func TestALaneKeepsAKeyWhole(t *testing.T) {
	for _, whole := range []string{"漢", "🙂", "\x1b[1;5A", "\x1bOP", "\x1b\x1b[A", "\x1b[<35;120;40M", "\x1b[200~", "\x1b]11;rgb:0/0/0\x07", "\x1bé"} {
		for cut := 1; cut < len(whole); cut++ {
			f := newFakeSession()
			term, err := New(Config{Session: f})
			if err != nil {
				t.Fatal(err)
			}
			l := term.Lane()
			l.Type([]byte("a" + whole[:cut]))
			waitFor(t, func() bool { return strings.HasPrefix(f.sentText(), "a") })
			term.Send([]byte("x"))
			waitFor(t, func() bool { return strings.Contains(f.sentText(), "x") })
			l.Type([]byte(whole[cut:] + "b"))
			waitFor(t, func() bool { return strings.HasSuffix(f.sentText(), "b") })
			if got := f.sentText(); !strings.Contains(got, whole) {
				t.Errorf("%q cut at %d: the program read %q", whole, cut, got)
			}
			_ = term.Close()
		}
	}
}

// What is not whole, and stays so, goes as it is soon after: the Esc key
// is the start of every other.
func TestALaneHoldsNothingForLong(t *testing.T) {
	for _, part := range []string{"\x1b", "\x1b[", "\xe6\xbc"} {
		f := newFakeSession()
		term, err := New(Config{Session: f})
		if err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		term.Lane().Type([]byte(part))
		waitFor(t, func() bool { return f.sentText() == part })
		if took := time.Since(start); took > time.Second {
			t.Errorf("%q is held for %v", part, took)
		}
		_ = term.Close()
	}
}
