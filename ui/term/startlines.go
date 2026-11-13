package term

import (
	"strings"
	"sync"
	"time"

	"github.com/marrasen/kakel/vt"
)

// Start lines: lines typed into a shell as it opens, as the keyboard
// would type them, each followed by a return. A layout types them into a
// terminal it opens.
//
// A line is typed at a prompt, and the next one at the prompt after it,
// so a line that runs for ever, as a dev server does, holds back the
// lines after it. The prompt is known by the sign the shell gives:
// OSC 133 A marks, or OSC 9;9 from the Command Prompt, as Shell Setup
// teaches them. A shell that gives none, or none in time, is typed into
// once it has written something and then been quiet for a moment.
//
// A key the user presses or a paste ends the typing: the user has taken
// over, and the lines left are dropped. So does the program ending, or
// another started in the pane.

// The delays the lines are typed after.
var (
	// promptSettle is how long a prompt has to stand before a line is
	// typed at it. A shell that has more input waiting, as Shell Setup's
	// own lines, prints a prompt and goes straight on to the next line,
	// which writes something or starts a command: a line typed at that
	// prompt would land behind the input waiting, and the one after it
	// at the prompt after that, while the first still ran.
	promptSettle = 250 * time.Millisecond

	// quietFor is how long a shell that gives no sign of its prompt has
	// to be quiet, after writing something, before a line is typed.
	quietFor = 500 * time.Millisecond

	// marksWait is how long a shell that was taught to give a sign is
	// waited for to give the first, after it first wrote something,
	// before it is typed into as one that gives none: one whose setup
	// failed, as fish on a server set up as a POSIX shell.
	marksWait = 5 * time.Second
)

// starter types a terminal's start lines. It hears what the terminal
// sees from the moment the terminal starts, so lines handed to it a
// moment later, once the pane is placed, still know a prompt that came
// first.
//
// Its own lock, as what it hears comes from the goroutine reading the
// program, the one drawing, and its timers. It never takes the
// terminal's emulator lock while holding its own.
type starter struct {
	mu sync.Mutex

	// after runs f once d has passed, and gives back what stops it;
	// time.AfterFunc unless a test gives a clock of its own.
	after func(d time.Duration, f func()) (stop func() bool)

	// said says the program has written something. seen says a sign of
	// each kind has come, and stands that one has come and no command
	// has started since.
	said   bool
	seen   [3]bool
	stands [3]bool

	// lines are the lines still to type, and sign the sign a prompt is
	// known by, NoSign for none.
	lines []string
	sign  vt.PromptSign
	// quiet says a line is typed when the shell has been quiet: it gives
	// no sign, or gave none in time.
	quiet bool
	// asking says the last line was held back because the shell asks for
	// a password or the like: keys typed then answer it, and are no take
	// over.
	asking bool

	// gen counts the timers set, and a timer that fires for an older one
	// does nothing. stop stops the one set, and stopWait the wait for the
	// first sign.
	gen      int
	stop     func() bool
	stopWait func() bool

	// send types bytes into the program, and asked reports whether the
	// line the cursor is on asks for a password, a passphrase or a host
	// key, which no line is typed into.
	send  func([]byte)
	asked func() bool
}

// TypeAtPrompts types lines into the program, each followed by a
// return, as start lines are: the first at the first prompt, and each
// after it at the next, where sign is how the shell says it is at a
// prompt, and vt.NoSign for a shell that says nothing, which is typed
// into once quiet. Lines handed while others wait take their place.
func (t *Terminal) TypeAtPrompts(lines []string, sign vt.PromptSign) {
	t.start.begin(lines, sign)
}

// LinesLeft is how many start lines are still waiting to be typed.
func (t *Terminal) LinesLeft() int {
	t.start.mu.Lock()
	defer t.start.mu.Unlock()
	return len(t.start.lines)
}

// newStarter is a starter typing with send, held back while asked says
// the shell asks for a secret.
func newStarter(send func([]byte), asked func() bool) *starter {
	return &starter{
		send:  send,
		asked: asked,
		after: func(d time.Duration, f func()) func() bool { return time.AfterFunc(d, f).Stop },
	}
}

// begin starts typing lines, by sign.
func (s *starter) begin(lines []string, sign vt.PromptSign) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropLocked()
	if len(lines) == 0 {
		return
	}
	s.lines = append([]string(nil), lines...)
	s.sign = sign
	s.quiet = sign == vt.NoSign
	switch {
	case s.quiet:
		if s.said {
			s.settleLocked(quietFor)
		}
	case s.stands[sign]:
		s.settleLocked(promptSettle)
	case s.said && !s.seen[sign]:
		s.waitLocked()
	}
}

// prompt hears that the shell gave sign: it is at its prompt.
func (s *starter) prompt(sign vt.PromptSign) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if int(sign) <= 0 || int(sign) >= len(s.seen) {
		return
	}
	s.seen[sign], s.stands[sign] = true, true
	if len(s.lines) == 0 || sign != s.sign {
		return
	}
	// A sign late is a sign all the same, and a surer one than quiet.
	s.quiet = false
	s.stopWaitLocked()
	s.settleLocked(promptSettle)
}

// started hears that the shell started a command, which its prompt no
// longer stands for.
func (s *starter) started() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stands[vt.PromptMark] = false
	if len(s.lines) > 0 && !s.quiet && s.sign == vt.PromptMark {
		s.stopLocked()
	}
}

// wrote hears that the program wrote something.
func (s *starter) wrote() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.said = true
	switch {
	case len(s.lines) == 0:
	case s.quiet:
		s.settleLocked(quietFor)
	case s.stands[s.sign]:
		// The prompt is still being drawn.
		s.settleLocked(promptSettle)
	case !s.seen[s.sign] && s.stopWait == nil:
		s.waitLocked()
	}
}

// took hears that the user typed or pasted into the program: the lines
// left are dropped, unless the shell is asking for a secret, which is
// what the user answers.
func (s *starter) took() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.lines) == 0 || s.asking {
		return
	}
	s.dropLocked()
}

// drop drops the lines left, as the program ends.
func (s *starter) drop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropLocked()
}

func (s *starter) dropLocked() {
	s.lines = nil
	s.asking = false
	s.stopLocked()
	s.stopWaitLocked()
}

// settleLocked types the next line once d has passed with nothing else
// heard in between that sets it again or stops it.
func (s *starter) settleLocked(d time.Duration) {
	s.stopLocked()
	s.gen++
	gen := s.gen
	s.stop = s.after(d, func() { s.fire(gen) })
}

func (s *starter) stopLocked() {
	if s.stop != nil {
		s.stop()
		s.stop = nil
	}
	// A timer that has fired already and waits for the lock is let go.
	s.gen++
}

// waitLocked waits for the first sign, and types by quiet once it has not
// come in time.
func (s *starter) waitLocked() {
	s.stopWait = s.after(marksWait, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if len(s.lines) == 0 || s.seen[s.sign] || s.quiet {
			return
		}
		s.quiet = true
		s.settleLocked(quietFor)
	})
}

func (s *starter) stopWaitLocked() {
	if s.stopWait != nil {
		s.stopWait()
		s.stopWait = nil
	}
}

// fire types the next line, for the timer set as gen, unless the shell
// asks for a secret: then it waits for what the shell writes next.
func (s *starter) fire(gen int) {
	s.mu.Lock()
	if gen != s.gen || len(s.lines) == 0 {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	// Read without the lock: it takes the emulator's, which the reader
	// holds while it tells this one what it heard.
	asked := s.asked()
	s.mu.Lock()
	defer s.mu.Unlock()
	if gen != s.gen || len(s.lines) == 0 {
		return
	}
	s.stop = nil
	if asked {
		s.asking = true
		return
	}
	s.asking = false
	line := s.lines[0]
	s.lines = s.lines[1:]
	// The next line waits for the next prompt, which this one's own
	// leaves standing until the shell has read it.
	s.stands[s.sign] = false
	s.send([]byte(line + "\r"))
	if len(s.lines) == 0 {
		s.dropLocked()
	}
}

// asksForSecret reports whether line, the text before the cursor, is a
// question asking for a password, a passphrase, a PIN or whether to trust
// a host key, as ssh, sudo and the like ask them: one that ends in a
// colon or a question mark, and names one of them.
func asksForSecret(line string) bool {
	l := strings.ToLower(strings.TrimSpace(line))
	if !strings.HasSuffix(l, ":") && !strings.HasSuffix(l, "?") {
		return false
	}
	for _, word := range []string{"password", "passphrase", "passcode", "pin ", "pin:", "yes/no"} {
		if strings.Contains(l, word) {
			return true
		}
	}
	return false
}

// cursorLine is the text on the cursor's row of the live screen, up to
// the cursor.
func (t *Terminal) cursorLine() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	scr := t.term.Screen()
	col, row := scr.CursorPos()
	cells := scr.LineAt(int(scr.LineNumber(row)))
	var b strings.Builder
	for _, c := range cells[:min(col, len(cells))] {
		if c.Width == 0 {
			continue
		}
		if c.Rune == 0 {
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(c.Rune)
	}
	return b.String()
}
