package term

import (
	"bytes"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

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
// teaches them. Shell Setup's own lines show prompts of their own first,
// and the caller says how many: the first line waits for the last of
// them. Only the A marks Shell Setup taught, which carry its option,
// count for those, as a shell's own prompt may mark itself as well. Each
// sign after a line was typed is the shell back from it.
//
// A shell that gives no sign, or none in time, is typed into once it has
// printed something and then been quiet for a moment. A shell that gave
// a sign, with no command started since, and has been quiet for some
// seconds with the cursor after text and the line not typed, gets the
// line too: one that shows fewer prompts while it is set up than it was
// taken to. Until Shell Setup's prompts are all seen, that takes longer,
// and its first prompt must have come.
//
// Anything typed or pasted into the pane ends the typing: the user, or
// whoever else types there, has taken over, and the lines left are
// dropped. Not the answer to a password question, which is asked of the
// user, unless it is Ctrl+C or Ctrl+D. The program ending drops them
// too.

// The delays the lines are typed after.
var (
	// promptSettle is how long a prompt has to stand, with nothing
	// written and no command started, before a line is typed at it: long
	// enough to see the prompt drawn, and a command started from input
	// that was waiting.
	promptSettle = 250 * time.Millisecond

	// quietFor is how long a shell that gives no sign of its prompt has
	// to be quiet, after printing something, before a line is typed.
	quietFor = 500 * time.Millisecond

	// marksWait is how long a shell that was taught to give a sign is
	// waited for to give the first, after it first printed something,
	// before it is typed into as one that gives none: one whose setup
	// failed, as fish on a server set up as a POSIX shell.
	marksWait = 5 * time.Second

	// netWait is how long a shell that gave a sign, with a line waiting
	// that was not typed at it, has to be quiet before the line is typed
	// all the same, once Shell Setup's prompts are past, and setupNetWait
	// how long while they are not all seen: one that shows fewer prompts
	// while it is set up than it was taken to. Longer, as a prompt slow to
	// work out stands quiet while it is set up too.
	netWait      = 3 * time.Second
	setupNetWait = 10 * time.Second
)

// shortNets, when set, are the net's two waits in place of netWait and
// setupNetWait, for a test of the whole app; see SetStartLineNets.
var shortNets atomic.Pointer[[2]time.Duration]

// SetStartLineNets has the net under start lines wait net once Shell
// Setup's prompts are past, and setup while they are not all seen, and
// gives back what puts the usual waits back. It is for a test of a whole
// app with real shells, which has to see the net act, or not act, in
// less than the seconds it waits.
func SetStartLineNets(net, setup time.Duration) (restore func()) {
	shortNets.Store(&[2]time.Duration{net, setup})
	return func() { shortNets.Store(nil) }
}

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

	// said says the program has printed something, text and not only
	// escapes. ended says it has ended, and no lines are taken until it
	// starts again.
	said  bool
	ended bool

	// signs counts the signs of each kind that have come. fresh says one
	// has come since the last line was typed, and stands that it has and
	// no command has started since. own counts the A marks Shell Setup
	// taught, and ownStands says one has come and no command has started
	// since: the shell stands at the prompt kakel's own lines marked.
	signs     [3]int
	fresh     [3]bool
	stands    [3]bool
	own       int
	ownStands bool

	// lines are the lines still to type, and sign the sign a prompt is
	// known by, NoSign for none. setup is how many prompts Shell Setup's
	// own lines show, and typed says a line has been typed, after which
	// they are past.
	lines []string
	sign  vt.PromptSign
	setup int
	typed bool
	// quiet says a line is typed when the shell has been quiet: it gives
	// no sign, or gave none in time.
	quiet bool
	// answering says the user is answering a password question that was
	// on screen as they began: the keys up to the return are the answer.
	answering bool

	// gen counts the timers set, and a timer that fires for an older one
	// does nothing. stop stops the one set, stopWait the wait for the
	// first sign, and stopNet the net under a sign not answered.
	gen      int
	stop     func() bool
	stopWait func() bool
	stopNet  func() bool

	// send types bytes into the program. line is the text before the
	// cursor, the whole line however many rows it wraps onto, and
	// printed reports whether the screen holds any text.
	send    func([]byte)
	line    func() string
	printed func() bool
}

// TypeAtPrompts types lines into the program, each followed by a
// return, as start lines are: the first at the first prompt the shell is
// ready at, and each after it at the next. sign is how the shell says it
// is at a prompt, and vt.NoSign for a shell that says nothing, which is
// typed into once quiet. setup is how many prompts Shell Setup's lines
// show before the shell is ready, the last of them the one it is ready
// at. Lines handed while others wait take their place. A program that
// has ended is typed nothing into.
func (t *Terminal) TypeAtPrompts(lines []string, sign vt.PromptSign, setup int) {
	t.start.begin(lines, sign, setup)
}

// LinesLeft is how many start lines are still waiting to be typed.
func (t *Terminal) LinesLeft() int {
	t.start.mu.Lock()
	defer t.start.mu.Unlock()
	return len(t.start.lines)
}

// newStarter is a starter typing with send, reading the screen with
// line and printed.
func newStarter(send func([]byte), line func() string, printed func() bool) *starter {
	return &starter{
		send:    send,
		line:    line,
		printed: printed,
		after:   func(d time.Duration, f func()) func() bool { return time.AfterFunc(d, f).Stop },
	}
}

// begin starts typing lines, by sign.
func (s *starter) begin(lines []string, sign vt.PromptSign, setup int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropLocked()
	if len(lines) == 0 || s.ended {
		return
	}
	if int(sign) < 0 || int(sign) >= len(s.signs) {
		sign = vt.NoSign
	}
	s.lines = append([]string(nil), lines...)
	s.sign, s.setup, s.typed = sign, setup, false
	s.quiet = sign == vt.NoSign
	if s.quiet {
		if s.said {
			s.settleLocked(quietFor)
		}
		return
	}
	s.considerLocked()
	if s.said && s.countLocked() == 0 {
		s.waitLocked()
	}
}

// countLocked is how many of the signs the lines wait for have come:
// for marks, only those Shell Setup taught.
func (s *starter) countLocked() int {
	if s.sign == vt.PromptMark {
		return s.own
	}
	return s.signs[s.sign]
}

// readyLocked says Shell Setup's prompts are past: a line may be typed at
// the next that stands.
func (s *starter) readyLocked() bool { return s.typed || s.countLocked() >= s.setup }

// considerLocked sets the timers for a shell known by its signs, as the
// sign last heard left it: a line typed once a prompt it is ready at has
// stood, and the net under any sign not answered.
func (s *starter) considerLocked() {
	if !s.stands[s.sign] {
		return
	}
	if s.readyLocked() {
		s.settleLocked(promptSettle)
	}
	if s.fresh[s.sign] && s.countLocked() > 0 {
		s.netLocked()
	}
}

// prompt hears that the shell gave sign: it is at its prompt. own says
// it is an A mark Shell Setup taught.
func (s *starter) prompt(sign vt.PromptSign, own bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if int(sign) <= 0 || int(sign) >= len(s.signs) {
		return
	}
	s.signs[sign]++
	s.fresh[sign], s.stands[sign] = true, true
	own = own && sign == vt.PromptMark
	if own {
		s.own++
		s.ownStands = true
	}
	if len(s.lines) == 0 || sign != s.sign {
		return
	}
	if sign == vt.PromptMark && !own && s.countLocked() == 0 {
		// A shell's own marks, with none of Shell Setup's, are no sign
		// that it was set up: as fish, which marks its prompts itself.
		return
	}
	// A sign late is a sign all the same, and a surer one than quiet.
	s.quiet = false
	s.stopWaitLocked()
	s.stopLocked()
	s.considerLocked()
}

// started hears that the shell started a command, which its prompt no
// longer stands for.
func (s *starter) started() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stands[vt.PromptMark] = false
	s.ownStands = false
	if len(s.lines) > 0 && !s.quiet && s.sign == vt.PromptMark {
		s.stopLocked()
		s.stopNetLocked()
	}
}

// wrote hears that the program wrote something.
func (s *starter) wrote() {
	s.mu.Lock()
	said := s.said
	s.mu.Unlock()
	if !said {
		// Escapes alone, as ConPTY's when it starts, are no shell that has
		// printed anything. Read without the lock: it takes the emulator's.
		said = s.printed()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.said = s.said || said
	switch {
	case len(s.lines) == 0:
	case s.quiet:
		if s.said {
			s.settleLocked(quietFor)
		}
	default:
		// The prompt is still being drawn, or the shell is busy: each
		// wait starts again.
		s.considerLocked()
		if s.said && s.countLocked() == 0 && s.stopWait == nil {
			s.waitLocked()
		}
	}
}

// took hears that b was typed or pasted into the program: the lines left
// are dropped, unless it answers a password question the shell is
// asking, which is the user's to answer. Ctrl+C and Ctrl+D drop them
// whatever was asked.
func (s *starter) took(b []byte) {
	s.mu.Lock()
	if len(s.lines) == 0 {
		s.mu.Unlock()
		return
	}
	if bytes.ContainsAny(b, "\x03\x04") {
		s.dropLocked()
		s.mu.Unlock()
		return
	}
	ends := bytes.ContainsAny(b, "\r\n")
	if s.answering {
		s.answering = !ends
		s.mu.Unlock()
		return
	}
	// A shell at the prompt Shell Setup marked asks nothing, whatever the
	// prompt says.
	marked := s.ownStands
	s.mu.Unlock()
	// Read without the lock: it takes the emulator's.
	asked := !marked && asksForSecret(s.line())
	s.mu.Lock()
	defer s.mu.Unlock()
	if asked {
		s.answering = !ends
		return
	}
	s.dropLocked()
}

// end drops the lines left as the program ends or the pane closes, and
// takes none until a program starts again.
func (s *starter) end() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropLocked()
	s.ended = true
}

// renew starts afresh for a program started again in the pane, which
// has printed nothing and given no sign.
func (s *starter) renew() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropLocked()
	s.said, s.ended = false, false
	s.signs, s.fresh, s.stands = [3]int{}, [3]bool{}, [3]bool{}
	s.own, s.ownStands = 0, false
}

func (s *starter) dropLocked() {
	s.lines = nil
	s.answering = false
	s.stopLocked()
	s.stopWaitLocked()
	s.stopNetLocked()
}

// settleLocked types the next line once d has passed with nothing else
// heard in between that sets it again or stops it.
func (s *starter) settleLocked(d time.Duration) {
	s.stopLocked()
	s.gen++
	gen := s.gen
	s.stop = s.after(d, func() { s.fire(gen, false) })
}

func (s *starter) stopLocked() {
	if s.stop != nil {
		s.stop()
		s.stop = nil
	}
	// A timer that has fired already and waits for the lock is let go.
	s.gen++
}

// netLocked types the next line once the shell has been quiet for
// netWait since a sign it was not typed at, or setupNetWait while Shell
// Setup's prompts are not all seen, as long as no line is typed and no
// command started in between.
func (s *starter) netLocked() {
	s.stopNetLocked()
	wait := netWait
	if !s.readyLocked() {
		wait = setupNetWait
	}
	if short := shortNets.Load(); short != nil {
		wait = short[0]
		if !s.readyLocked() {
			wait = short[1]
		}
	}
	s.stopNet = s.after(wait, func() {
		s.mu.Lock()
		if len(s.lines) == 0 || s.quiet || !s.fresh[s.sign] || !s.stands[s.sign] {
			s.mu.Unlock()
			return
		}
		s.stopNet = nil
		s.stopLocked()
		gen := s.gen
		s.mu.Unlock()
		s.fire(gen, true)
	})
}

func (s *starter) stopNetLocked() {
	if s.stopNet != nil {
		s.stopNet()
		s.stopNet = nil
	}
}

// waitLocked waits for the first sign, and types by quiet once it has not
// come in time.
func (s *starter) waitLocked() {
	s.stopWait = s.after(marksWait, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if len(s.lines) == 0 || s.countLocked() > 0 || s.quiet {
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
// asks for a secret, or, for the Command Prompt, the cursor stands
// elsewhere than after its prompt: then it waits for what the shell
// writes next. net says the net fired it, which types only with the
// cursor after text, as a prompt leaves it: a cursor at the start of a
// line is a prompt not yet drawn, or a program's output.
func (s *starter) fire(gen int, net bool) {
	s.mu.Lock()
	if gen != s.gen || len(s.lines) == 0 {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	// Read without the lock: it takes the emulator's, which the reader
	// holds while it tells this one what it heard.
	line := s.line()
	s.mu.Lock()
	defer s.mu.Unlock()
	if gen != s.gen || len(s.lines) == 0 {
		return
	}
	s.stop = nil
	if net && strings.TrimSpace(line) == "" {
		return
	}
	if !(s.sign == vt.PromptMark && s.ownStands) && asksForSecret(line) {
		// A shell at the prompt Shell Setup marked asks nothing, whatever
		// the prompt says.
		return
	}
	if !s.quiet && s.sign == vt.PromptFolder && !strings.HasSuffix(strings.TrimRight(line, " "), ">") {
		// A batch file with echo on prints the prompt before each command
		// it runs, and the command after it: a prompt the cursor has left
		// is one of those.
		if net {
			s.netLocked()
		}
		return
	}
	next := s.lines[0]
	s.lines = s.lines[1:]
	s.typed = true
	// The next line waits for the next prompt, which this one's own
	// leaves standing until the shell has read it. Nor does the prompt
	// Shell Setup marked stand any longer: a question the line asks, with
	// no C mark before it, is a question all the same.
	s.fresh[s.sign], s.stands[s.sign] = false, false
	s.ownStands = false
	s.stopNetLocked()
	s.send([]byte(next + "\r"))
	if len(s.lines) == 0 {
		s.dropLocked()
	}
}

// secretWords are what a question for a secret names, as ssh, sudo, su,
// gpg, a PIN, a one-time code and the languages they are asked in put it: in
// English, Swedish, German, French, Spanish, Norwegian, Danish, Finnish,
// Dutch, Italian, Portuguese, Polish, Czech and Russian.
var secretWords = []string{
	"password", "passphrase", "passcode", "pass phrase",
	"yes/no", "'yes', 'no'", "fingerprint",
	"verification code", "one-time", "one time", "otp", "authenticator",
	"security code", "token code", "2fa",
	"lösenord", "lösenfras", "passwort", "kennwort", "mot de passe",
	"phrase secrète", "phrase de passe", "contraseña", "passord",
	"adgangskode", "kodeord", "salasana", "tunnuslause", "wachtwoord",
	"senha", "palavra-passe", "hasło", "heslo", "пароль", "pin",
}

// asksForSecret reports whether line, the text before the cursor, is a
// question asking for a password, a passphrase, a PIN, a one-time code
// or whether to trust a host key, as ssh, sudo and the like ask them:
// one that ends in a colon or a question mark, and names one of them as
// a word of its own, so a host called hotplug or a folder called
// otp-service in a prompt asks nothing.
func asksForSecret(line string) bool {
	l := strings.ToLower(strings.TrimSpace(line))
	if !strings.HasSuffix(l, ":") && !strings.HasSuffix(l, "?") && !strings.HasSuffix(l, "：") {
		return false
	}
	for _, word := range secretWords {
		if hasWord(l, word) {
			return true
		}
	}
	return false
}

// hasWord reports whether s holds word with nothing either side that is
// part of a word or a name: a letter, a digit, or one of _ . / \. A
// hyphen parts words, as in PIN-kod.
func hasWord(s, word string) bool {
	for i := 0; ; {
		at := strings.Index(s[i:], word)
		if at < 0 {
			return false
		}
		at += i
		end := at + len(word)
		before, _ := utf8.DecodeLastRuneInString(s[:at])
		after, _ := utf8.DecodeRuneInString(s[end:])
		if (at == 0 || !inName(before)) && (end == len(s) || !inName(after)) {
			return true
		}
		i = at + 1
	}
}

// inName says r is part of a word or a name: a letter, a digit, or one
// of _ . / \.
func inName(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune(`_./\`, r)
}

// cursorLine is the text before the cursor on the live screen, the
// whole line the cursor's row is part of, from the row it starts on
// however many rows it wraps onto.
func (t *Terminal) cursorLine() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	scr := t.term.Screen()
	col, row := scr.CursorPos()
	at := int(scr.LineNumber(row))
	first := at
	for first > scr.OldestLine() {
		above := scr.LineAt(first - 1)
		if len(above) == 0 || !above[len(above)-1].Wrapped {
			break
		}
		first--
	}
	var b strings.Builder
	for n := first; n <= at; n++ {
		cells := scr.LineAt(n)
		if n == at {
			cells = cells[:min(col, len(cells))]
		}
		for _, c := range cells {
			if c.Width == 0 {
				continue
			}
			if c.Rune == 0 {
				b.WriteByte(' ')
				continue
			}
			b.WriteRune(c.Rune)
		}
	}
	return b.String()
}

// hasText reports whether the live screen, or what has scrolled off it,
// holds anything other than blanks.
func (t *Terminal) hasText() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	scr := t.term.Screen()
	if scr.History() > 0 {
		return true
	}
	_, rows := scr.Size()
	for row := range rows {
		for _, c := range scr.LineAt(int(scr.LineNumber(row))) {
			if c.Rune != 0 && c.Rune != ' ' {
				return true
			}
		}
	}
	return false
}
