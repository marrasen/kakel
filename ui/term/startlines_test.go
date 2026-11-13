package term

import (
	"slices"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/marrasen/kakel/input"
	"github.com/marrasen/kakel/vt"
)

// fakeClock is a clock a test moves on by hand, so what is typed after a
// delay is typed when the test says the delay has passed, and not when
// the machine running it gets round to it.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Duration
	timers []*fakeTimer
}

type fakeTimer struct {
	at  time.Duration
	f   func()
	off bool
}

// after is time.AfterFunc on this clock.
func (c *fakeClock) after(d time.Duration, f func()) func() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{at: c.now + d, f: f}
	c.timers = append(c.timers, t)
	return func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		was := !t.off
		t.off = true
		return was
	}
}

// advance moves the clock on by d, firing each timer that falls due on
// the way, in the order they fall due, at the time each does.
func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	end := c.now + d
	c.mu.Unlock()
	for {
		c.mu.Lock()
		due := slices.DeleteFunc(slices.Clone(c.timers), func(t *fakeTimer) bool { return t.off || t.at > end })
		if len(due) == 0 {
			c.now = end
			c.mu.Unlock()
			return
		}
		sort.SliceStable(due, func(i, j int) bool { return due[i].at < due[j].at })
		next := due[0]
		next.off = true
		c.now = next.at
		c.mu.Unlock()
		next.f()
	}
}

// typist is a terminal whose start lines go to typed, on a fake clock.
type typist struct {
	term  *Terminal
	f     *fakeSession
	clock *fakeClock
	mu    sync.Mutex
	typed []string
}

func newTypist(t *testing.T) *typist {
	t.Helper()
	term, f := newTestTerm(t, 40, 6, Config{})
	tp := &typist{term: term, f: f, clock: &fakeClock{}}
	term.start.after = tp.clock.after
	term.start.send = func(b []byte) {
		tp.mu.Lock()
		tp.typed = append(tp.typed, string(b))
		tp.mu.Unlock()
	}
	return tp
}

// says has the program write s, and waits for the terminal to take it.
func (tp *typist) says(t *testing.T, s string) {
	t.Helper()
	tp.f.feed(t, tp.term, s)
}

// want checks what has been typed so far.
func (tp *typist) want(t *testing.T, when string, want ...string) {
	t.Helper()
	tp.mu.Lock()
	defer tp.mu.Unlock()
	if !slices.Equal(tp.typed, want) && !(len(tp.typed) == 0 && len(want) == 0) {
		t.Fatalf("%s, typed %q, want %q", when, tp.typed, want)
	}
}

// The marks a POSIX shell taught by Shell Setup sends.
const (
	markPrompt = "\x1b]133;A\x07$ "
	markStart  = "\x1b]133;C\x07"
	markDone   = "\x1b]133;D;0\x07"
)

// Lines are typed one at each prompt, the first at the first, each once
// the prompt has stood a moment; output before any prompt types nothing.
func TestStartLinesAreTypedOneAtEachPrompt(t *testing.T) {
	tp := newTypist(t)
	tp.term.TypeAtPrompts([]string{"cd web", "vite dev", "never"}, vt.PromptMark)

	tp.says(t, "Welcome to the machine\r\n")
	tp.clock.advance(time.Second)
	tp.want(t, "before any prompt")

	tp.says(t, markPrompt)
	tp.clock.advance(promptSettle - time.Millisecond)
	tp.want(t, "before the prompt has stood")
	tp.clock.advance(time.Millisecond)
	tp.want(t, "at the first prompt", "cd web\r")

	// The shell echoes the line, runs it, and prompts again.
	tp.clock.advance(time.Minute)
	tp.want(t, "with no prompt since", "cd web\r")
	tp.says(t, "cd web\r\n"+markStart+markDone+markPrompt)
	tp.clock.advance(promptSettle)
	tp.want(t, "at the second prompt", "cd web\r", "vite dev\r")

	// vite dev runs for ever, and the line after it waits.
	tp.says(t, "vite dev\r\n"+markStart+"ready in 300 ms\r\n")
	tp.clock.advance(time.Hour)
	tp.want(t, "while vite runs", "cd web\r", "vite dev\r")
	if n := tp.term.LinesLeft(); n != 1 {
		t.Fatalf("%d lines left, want 1", n)
	}
}

// Shell Setup's own lines come first: the prompt after the first of them
// is followed at once by the next being run, and no line is typed until
// a prompt has stood with nothing run after it.
func TestStartLinesWaitForTheSetupToFinish(t *testing.T) {
	tp := newTypist(t)
	tp.term.TypeAtPrompts([]string{"ls"}, vt.PromptMark)
	tp.says(t, markDone+markPrompt)
	tp.says(t, markStart+"\x1b[H\x1b[2J")
	tp.clock.advance(time.Second)
	tp.want(t, "with clear run after the prompt")
	tp.says(t, markDone+markPrompt)
	tp.clock.advance(promptSettle)
	tp.want(t, "once the prompt after clear stood", "ls\r")
}

// A prompt that came before the lines were handed over counts: the pane
// is placed a moment after its shell starts.
func TestAPromptBeforeTheLinesCounts(t *testing.T) {
	tp := newTypist(t)
	tp.says(t, markPrompt)
	tp.term.TypeAtPrompts([]string{"ls"}, vt.PromptMark)
	tp.clock.advance(promptSettle)
	tp.want(t, "at the prompt that came first", "ls\r")
}

// A shell that gives no sign is typed into once it has written something
// and been quiet a moment, and each line after it the same way.
func TestAShellWithNoMarksIsTypedIntoOnceQuiet(t *testing.T) {
	tp := newTypist(t)
	tp.term.TypeAtPrompts([]string{"one", "two"}, vt.NoSign)
	tp.clock.advance(time.Minute)
	tp.want(t, "before the shell wrote anything")

	tp.says(t, "~> ")
	tp.clock.advance(quietFor / 2)
	tp.says(t, "\x1b[32m~>\x1b[0m ")
	tp.clock.advance(quietFor / 2)
	tp.want(t, "while the prompt was still drawn")
	tp.clock.advance(quietFor / 2)
	tp.want(t, "once quiet", "one\r")

	tp.says(t, "one\r\noutput\r\n~> ")
	tp.clock.advance(quietFor)
	tp.want(t, "once quiet again", "one\r", "two\r")
}

// A shell taught to give marks that gives none, as fish set up as a POSIX
// shell, is typed into once quiet, some seconds after it first wrote.
func TestMarksThatNeverComeFallBackToQuiet(t *testing.T) {
	tp := newTypist(t)
	tp.term.TypeAtPrompts([]string{"one", "two"}, vt.PromptMark)
	tp.says(t, "fish: Unknown command: __gt_d\r\n> ")
	tp.clock.advance(marksWait - time.Millisecond)
	tp.want(t, "while marks are waited for")
	tp.clock.advance(time.Millisecond + quietFor)
	tp.want(t, "once marks were given up on", "one\r")
	tp.says(t, "one\r\n> ")
	tp.clock.advance(quietFor)
	tp.want(t, "the next line, once quiet", "one\r", "two\r")
}

// The Command Prompt's sign is its folder; a mark from it means nothing.
func TestTheCommandPromptIsKnownByItsFolder(t *testing.T) {
	tp := newTypist(t)
	tp.term.TypeAtPrompts([]string{"dir"}, vt.PromptFolder)
	tp.says(t, markPrompt)
	tp.clock.advance(promptSettle)
	tp.want(t, "at a mark")
	tp.says(t, "\x1b]9;9;C:\\Users\x1b\\C:\\Users>")
	tp.clock.advance(promptSettle)
	tp.want(t, "at the folder", "dir\r")
}

// A key the user presses takes the pane over: the lines left are
// dropped. So does a paste, and someone typing from another window.
func TestTheUserTakesOverFromTheLines(t *testing.T) {
	for name, take := range map[string]func(*Terminal){
		"key": func(term *Terminal) {
			_, _ = term.HandleKey(input.Event{Kind: input.Text, Rune: 'x', NormalText: true})
		},
		"paste": func(term *Terminal) { term.Paste("x") },
		"watch": func(term *Terminal) { term.Send([]byte("x")) },
	} {
		t.Run(name, func(t *testing.T) {
			tp := newTypist(t)
			tp.term.TypeAtPrompts([]string{"one", "two"}, vt.PromptMark)
			tp.says(t, markPrompt)
			tp.clock.advance(promptSettle)
			tp.want(t, "at the first prompt", "one\r")
			take(tp.term)
			tp.says(t, "one\r\n"+markStart+markDone+markPrompt)
			tp.clock.advance(time.Minute)
			tp.want(t, "taken over", "one\r")
			if n := tp.term.LinesLeft(); n != 0 {
				t.Fatalf("%d lines left once taken over", n)
			}
		})
	}
}

// Nothing is typed into a question for a password: the user's answer to
// it is no take-over, and the lines are typed at the shell after it.
func TestNothingIsTypedIntoAPasswordQuestion(t *testing.T) {
	tp := newTypist(t)
	tp.term.TypeAtPrompts([]string{"ls"}, vt.NoSign)
	tp.says(t, "me@far's password: ")
	tp.clock.advance(time.Minute)
	tp.want(t, "at the question")
	_, _ = tp.term.HandleKey(input.Event{Kind: input.Text, Rune: 's', NormalText: true})
	tp.says(t, "\r\nWelcome\r\nme@far:~$ ")
	tp.clock.advance(quietFor)
	tp.want(t, "at the shell", "ls\r")
}

// The program ending drops the lines: the one started in its place is
// not the one they were for.
func TestTheProgramEndingDropsTheLines(t *testing.T) {
	tp := newTypist(t)
	tp.term.TypeAtPrompts([]string{"ls"}, vt.NoSign)
	endProgram(t, tp.term, tp.f)
	if n := tp.term.LinesLeft(); n != 0 {
		t.Fatalf("%d lines left once the program ended", n)
	}
	tp.clock.advance(time.Minute)
	tp.want(t, "once ended")
}

func TestAsksForSecret(t *testing.T) {
	for line, want := range map[string]bool{
		"Password:":                       true,
		"me@far's password: ":             true,
		"[sudo] password for me:":         true,
		"Enter passphrase for key '/k': ": true,
		"Enter PIN for 'yubikey':":        true,
		"Are you sure you want to continue connecting (yes/no/[fingerprint])? ": true,
		"me@far:~/passwords$ ":         false,
		"PS C:\\passwords> ":           false,
		"":                             false,
		"Type a password into the box": false,
	} {
		if got := asksForSecret(line); got != want {
			t.Errorf("asksForSecret(%q) = %v, want %v", line, got, want)
		}
	}
}
