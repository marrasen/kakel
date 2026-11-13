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
	tp.term.TypeAtPrompts([]string{"cd web", "vite dev", "never"}, vt.PromptMark, 1)

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
// is not the one the shell is ready at, however long it stands, as when
// the prompt takes a while to work out; the line waits for the prompt
// after clear, and the next for the prompt after it.
func TestStartLinesWaitForTheSetupToFinish(t *testing.T) {
	tp := newTypist(t)
	tp.term.TypeAtPrompts([]string{"cat", "echo TWO"}, vt.PromptMark, 2)
	tp.says(t, "$ setup\r\n")
	// After Shell Setup's line: the mark, then a prompt slow to draw.
	tp.says(t, markDone+"\x1b]133;A\x07")
	tp.clock.advance(time.Second)
	tp.says(t, "$ ")
	tp.clock.advance(time.Second)
	tp.want(t, "at Shell Setup's first prompt")
	tp.says(t, "clear\r\n"+markStart+"\x1b[H\x1b[2J")
	tp.says(t, markDone+"\x1b]133;A\x07")
	tp.clock.advance(promptSettle)
	tp.want(t, "once the prompt after clear stood", "cat\r")
	// cat runs, quiet, for as long as it likes.
	tp.says(t, "cat\r\n"+markStart)
	tp.clock.advance(time.Hour)
	tp.want(t, "while cat runs", "cat\r")
	if n := tp.term.LinesLeft(); n != 1 {
		t.Fatalf("%d lines left while cat runs, want 1", n)
	}
	tp.says(t, "^C\r\n"+markDone+markPrompt)
	tp.clock.advance(promptSettle)
	tp.want(t, "once cat ended", "cat\r", "echo TWO\r")
}

// A prompt that came before the lines were handed over counts: the pane
// is placed a moment after its shell starts.
func TestAPromptBeforeTheLinesCounts(t *testing.T) {
	tp := newTypist(t)
	tp.says(t, markPrompt)
	tp.term.TypeAtPrompts([]string{"ls"}, vt.PromptMark, 1)
	tp.clock.advance(promptSettle)
	tp.want(t, "at the prompt that came first", "ls\r")
}

// A shell whose marks say a command started at each prompt, as bash did
// with a PROMPT_COMMAND of the user's own, has its line typed once it
// has been quiet some seconds at a prompt the line was not typed at; a
// line typed and running holds the next back all the same.
func TestASignNotAnsweredIsTypedAtOnceQuiet(t *testing.T) {
	tp := newTypist(t)
	tp.term.TypeAtPrompts([]string{"cat", "echo TWO"}, vt.PromptMark, 2)
	tp.says(t, markDone+markPrompt+markStart)
	tp.says(t, "clear\r\n"+markDone+markPrompt+markStart)
	tp.clock.advance(netWait - time.Millisecond)
	tp.want(t, "before the shell was quiet long enough")
	tp.clock.advance(time.Millisecond)
	tp.want(t, "once quiet at the prompt", "cat\r")
	tp.says(t, "cat\r\n")
	tp.clock.advance(time.Hour)
	tp.want(t, "while cat runs", "cat\r")
	tp.says(t, "^C\r\n"+markDone+markPrompt+markStart)
	tp.clock.advance(netWait)
	tp.want(t, "once quiet at the prompt after cat", "cat\r", "echo TWO\r")
}

// A shell that shows fewer prompts while it is set up than it was taken
// to has its lines typed all the same, once quiet at its last.
func TestTooFewSetupPromptsLoseNoLines(t *testing.T) {
	tp := newTypist(t)
	tp.term.TypeAtPrompts([]string{"ls"}, vt.PromptMark, 3)
	tp.says(t, markDone+markPrompt)
	tp.clock.advance(time.Second)
	tp.says(t, "x")
	tp.clock.advance(netWait)
	tp.want(t, "once quiet at the prompt", "ls\r")
}

// A shell that gives no sign is typed into once it has printed something
// and been quiet a moment, and each line after it the same way. Escapes
// alone, as ConPTY sends as it starts, are nothing printed.
func TestAShellWithNoMarksIsTypedIntoOnceQuiet(t *testing.T) {
	tp := newTypist(t)
	tp.term.TypeAtPrompts([]string{"one", "two"}, vt.NoSign, 0)
	tp.says(t, "\x1b[?9001h\x1b[?1004h\x1b[?25l\x1b[2J\x1b[m\x1b[H\x1b]0;C:\\Windows\\system32\\cmd.exe\x07\x1b[?25h")
	tp.clock.advance(time.Minute)
	tp.want(t, "before the shell printed anything")

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
	tp.term.TypeAtPrompts([]string{"one", "two"}, vt.PromptMark, 2)
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
	tp.term.TypeAtPrompts([]string{"dir"}, vt.PromptFolder, 1)
	tp.says(t, markPrompt)
	tp.clock.advance(promptSettle)
	tp.want(t, "at a mark")
	tp.says(t, "\x1b]9;9;C:\\Users\x1b\\C:\\Users>")
	tp.clock.advance(promptSettle)
	tp.want(t, "at the folder", "dir\r")
}

// A batch file with echo on prints the Command Prompt's prompt before
// each command it runs: a prompt the cursor has moved on from, to the
// command and its output, is no prompt to type at, even once quiet.
func TestABatchFilesEchoedPromptIsNoPrompt(t *testing.T) {
	tp := newTypist(t)
	tp.term.TypeAtPrompts([]string{"build.bat", "dir"}, vt.PromptFolder, 1)
	tp.says(t, "\x1b]9;9;C:\\x\x1b\\C:\\x>")
	tp.clock.advance(promptSettle)
	tp.want(t, "at the prompt", "build.bat\r")
	tp.says(t, "build.bat\r\n\r\n\x1b]9;9;C:\\x\x1b\\C:\\x>timeout 60\r\n")
	tp.clock.advance(time.Hour)
	tp.want(t, "while the batch file runs", "build.bat\r")
	tp.says(t, "\r\n\x1b]9;9;C:\\x\x1b\\C:\\x>")
	tp.clock.advance(promptSettle)
	tp.want(t, "at the prompt after it", "build.bat\r", "dir\r")
}

// Anything typed into the pane takes it over: the lines left are
// dropped. A key, a paste, someone typing from another window, an agent.
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
			tp.term.TypeAtPrompts([]string{"one", "two"}, vt.PromptMark, 1)
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

// key types one key into the terminal, as the keyboard does.
func key(term *Terminal, r rune) {
	_, _ = term.HandleKey(input.Event{Kind: input.Text, Rune: r, NormalText: true})
}

// enter presses Enter.
func enter(term *Terminal) {
	_, _ = term.HandleKey(input.Event{Kind: input.KeyPress, Key: input.KeyEnter})
}

// ctrlC presses Ctrl+C.
func ctrlC(term *Terminal) {
	_, _ = term.HandleKey(input.Event{Kind: input.KeyPress, Key: input.KeyC, Mods: input.ModCtrl})
}

// Nothing is typed into a question for a password: the user's answer to
// it is no take-over, and the lines are typed at the shell after it.
func TestNothingIsTypedIntoAPasswordQuestion(t *testing.T) {
	tp := newTypist(t)
	tp.term.TypeAtPrompts([]string{"ls"}, vt.NoSign, 0)
	tp.says(t, "me@far's password: ")
	tp.clock.advance(time.Minute)
	tp.want(t, "at the question")
	key(tp.term, 's')
	enter(tp.term)
	tp.says(t, "\r\nWelcome\r\nme@far:~$ ")
	tp.clock.advance(quietFor)
	tp.want(t, "at the shell", "ls\r")
}

// A question that wraps onto more rows than one, in a narrow pane, is
// known by the whole of it.
func TestAWrappedQuestionIsKnown(t *testing.T) {
	tp := newTypist(t)
	tp.term.TypeAtPrompts([]string{"ls"}, vt.NoSign, 0)
	tp.says(t, "Enter passphrase for key '/home/me/.ssh/id_ed25519': ")
	tp.clock.advance(time.Minute)
	tp.want(t, "at the wrapped question")
	if got := tp.term.cursorLine(); got != "Enter passphrase for key '/home/me/.ssh/id_ed25519': " {
		t.Fatalf("the line before the cursor reads %q", got)
	}
}

// In a shell that marks its prompts, a line that asks for a password, as
// sudo does, is answered by the user, and the next line typed at the
// prompt after it.
func TestALineAskingForAPasswordIsAnswered(t *testing.T) {
	tp := newTypist(t)
	tp.term.TypeAtPrompts([]string{"sudo true", "ls"}, vt.PromptMark, 1)
	tp.says(t, markPrompt)
	tp.clock.advance(promptSettle)
	tp.want(t, "at the prompt", "sudo true\r")
	tp.says(t, "sudo true\r\n"+markStart+"[sudo] password for me: ")
	for _, r := range "secret" {
		key(tp.term, r)
	}
	enter(tp.term)
	if n := tp.term.LinesLeft(); n != 1 {
		t.Fatalf("%d lines left once the password was typed, want 1", n)
	}
	tp.says(t, "\r\n"+markDone+markPrompt)
	tp.clock.advance(promptSettle)
	tp.want(t, "at the prompt after sudo", "sudo true\r", "ls\r")
}

// Ctrl+C at a password question cancels the lines left: the line after
// it was for what the question let in.
func TestCtrlCAtAPasswordQuestionDropsTheLines(t *testing.T) {
	for name, cancel := range map[string]func(*Terminal){
		"Ctrl+C":        ctrlC,
		"typed, Ctrl+C": func(term *Terminal) { key(term, 's'); ctrlC(term) },
		"Ctrl+D":        func(term *Terminal) { term.Send([]byte{4}) },
	} {
		t.Run(name, func(t *testing.T) {
			tp := newTypist(t)
			tp.term.TypeAtPrompts([]string{"ls"}, vt.NoSign, 0)
			tp.says(t, "me@prod's password: ")
			cancel(tp.term)
			tp.says(t, "^C\r\n$ ")
			tp.clock.advance(time.Minute)
			tp.want(t, "once cancelled")
			if n := tp.term.LinesLeft(); n != 0 {
				t.Fatalf("%d lines left", n)
			}
		})
	}
}

// Keys typed once the question is answered, and none is asked, take
// over.
func TestTypingAfterAnAnswerTakesOver(t *testing.T) {
	tp := newTypist(t)
	tp.term.TypeAtPrompts([]string{"ls"}, vt.NoSign, 0)
	tp.says(t, "me@far's password: ")
	key(tp.term, 's')
	enter(tp.term)
	tp.says(t, "\r\nme@far:~$ ")
	key(tp.term, 'x')
	if n := tp.term.LinesLeft(); n != 0 {
		t.Fatalf("%d lines left after typing at the shell", n)
	}
}

// The answer to ssh's host key question is echoed as it is typed, and
// is an answer all the same.
func TestAnEchoedAnswerIsAnAnswer(t *testing.T) {
	tp := newTypist(t)
	tp.term.TypeAtPrompts([]string{"ls"}, vt.NoSign, 0)
	tp.says(t, "Are you sure you want to continue connecting (yes/no/[fingerprint])? ")
	for _, r := range "yes" {
		key(tp.term, r)
		tp.says(t, string(r))
	}
	enter(tp.term)
	if n := tp.term.LinesLeft(); n != 1 {
		t.Fatalf("%d lines left after answering yes", n)
	}
}

// The program ending drops the lines: the one started in its place is
// not the one they were for. Lines handed over once it has ended are
// refused, and a program started again is typed nothing into.
func TestTheProgramEndingDropsTheLines(t *testing.T) {
	tp := newTypist(t)
	tp.term.TypeAtPrompts([]string{"ls"}, vt.NoSign, 0)
	endProgram(t, tp.term, tp.f)
	if n := tp.term.LinesLeft(); n != 0 {
		t.Fatalf("%d lines left once the program ended", n)
	}
	tp.term.TypeAtPrompts([]string{"late"}, vt.NoSign, 0)
	if n := tp.term.LinesLeft(); n != 0 {
		t.Fatalf("%d lines taken once the program ended", n)
	}
	next := restart(t, tp.term)
	next.feed(t, tp.term, "$ ")
	tp.clock.advance(time.Minute)
	tp.want(t, "in the program started again")
}

func TestAsksForSecret(t *testing.T) {
	for line, want := range map[string]bool{
		"Password:":                       true,
		"me@far's password: ":             true,
		"[sudo] password for me:":         true,
		"Enter passphrase for key '/k': ": true,
		"Enter PIN for 'yubikey':":        true,
		"Are you sure you want to continue connecting (yes/no/[fingerprint])? ": true,
		"Please type 'yes', 'no' or the fingerprint: ":                          true,
		"Verification code: ":          true,
		"[sudo] lösenord för me: ":     true,
		"Passwort: ":                   true,
		"Mot de passe : ":              true,
		"Contraseña: ":                 true,
		"Passord: ":                    true,
		"Adgangskode: ":                true,
		"Salasana: ":                   true,
		"Wachtwoord: ":                 true,
		"Senha: ":                      true,
		"me@far:~/passwords$ ":         false,
		"PS C:\\passwords> ":           false,
		"":                             false,
		"Type a password into the box": false,
		"Spinning up:":                 false,
	} {
		if got := asksForSecret(line); got != want {
			t.Errorf("asksForSecret(%q) = %v, want %v", line, got, want)
		}
	}
}
