package shellsetup

import (
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marrasen/kakel/session"
)

// The shell an argv runs decides what is typed into it.
func TestWhichShellAnArgvRuns(t *testing.T) {
	for _, tc := range []struct {
		argv []string
		want Route
	}{
		{[]string{`C:\Windows\System32\cmd.exe`}, Cmd},
		{[]string{`C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`}, PowerShell},
		{[]string{`C:\Program Files\PowerShell\7\pwsh.exe`}, PowerShell},
		{[]string{`C:\Windows\System32\wsl.exe`, "-d", "Ubuntu"}, Posix},
		{[]string{"/bin/bash"}, Posix},
		{[]string{"/usr/bin/zsh"}, Posix},
		// A login shell on a machine at the far end, which sshd picks
		// and the protocol never names.
		{nil, Posix},
	} {
		if got := RouteFor(tc.argv); got != tc.want {
			t.Errorf("%v came out as route %d, want %d", tc.argv, got, tc.want)
		}
	}
}

// The lines are typed only into a shell they are written for: a
// profile running anything else would take them as its own input.
func TestOnlyAShellTheLinesAreWrittenForIsKnown(t *testing.T) {
	for _, tc := range []struct {
		argv []string
		want bool
	}{
		{[]string{`C:\Program Files\Git\bin\bash.exe`, "-i", "-l"}, true},
		{[]string{`C:\msys64\usr\bin\ZSH.EXE`}, true},
		{[]string{"/bin/sh"}, true},
		{[]string{"ksh"}, true},
		{[]string{"dash"}, true},
		{[]string{`C:\Windows\System32\cmd.exe`, "/k", "echo hi"}, true},
		{[]string{"powershell.exe", "-NoLogo"}, true},
		{[]string{"pwsh"}, true},
		{[]string{`C:\Windows\System32\wsl.exe`, "-d", "Ubuntu"}, true},
		{[]string{"wsl.exe", "-d", "Ubuntu", "--cd", "/home/x"}, true},
		{[]string{"wsl", "-u", "root", "-e", "bash", "-l"}, true},
		{[]string{"wsl", "-e", "nu"}, false},
		{[]string{"wsl", "--exec", "htop"}, false},
		{[]string{"wsl", "--", "htop"}, false},
		{[]string{"wsl", "-d", "Ubuntu", "htop"}, false},
		{[]string{"wsl.exe", "~", "-d", "Ubuntu"}, true},
		{[]string{"wsl", "-e", "bash", "-c", "make"}, false},
		// A shell handed something to run runs that, not what is typed.
		{[]string{"bash", "-c", "make test"}, false},
		{[]string{"bash", "-lc", "make"}, false},
		{[]string{"zsh", "deploy.sh"}, false},
		{[]string{"bash", "-o", "vi", "--login"}, true},
		{[]string{"bash", "-l", "-Cl"}, true},
		{[]string{"cmd.exe", "/c", "build.bat"}, false},
		{[]string{"cmd.exe", "/D", "/C", "dir"}, false},
		{[]string{"cmd.exe", "/k", "echo /c"}, true},
		{[]string{"pwsh", "-Command", "Get-Date"}, false},
		{[]string{"pwsh", "-c", "Get-Date"}, false},
		{[]string{"powershell.exe", "-NoProfile", "-File", "x.ps1"}, false},
		{[]string{"powershell.exe", "-ec", "ZQBjAGgAbwA="}, false},
		{[]string{"pwsh", "-NoExit", "-Command", "cd ~"}, true},
		{[]string{"pwsh", "-ExecutionPolicy", "Bypass", "-NoLogo"}, true},
		{[]string{"nu"}, false},
		{[]string{`C:\Python312\python.exe`}, false},
		{[]string{"ssh", "-t", "box"}, false},
		{[]string{"bashful"}, false},
		{nil, false},
	} {
		if got := Knows(tc.argv); got != tc.want {
			t.Errorf("%q is known: %v, want %v", tc.argv, got, tc.want)
		}
	}
}

// Every shell gets lines to type, and the pane ends up cleared: what
// is typed is echoed, and a pane that opens on a screenful of setup is
// worse than one that opens empty.
func TestEveryShellIsLeftWithACleanPane(t *testing.T) {
	for _, tc := range []struct {
		route Route
		clear string
	}{
		{Posix, "clear"},
		{PowerShell, "Clear-Host"},
		{Cmd, "cls"},
	} {
		lines := Lines(tc.route)
		if len(lines) == 0 {
			t.Errorf("route %d has nothing to type", tc.route)
			continue
		}
		if last := lines[len(lines)-1]; !strings.HasSuffix(last, tc.clear) {
			t.Errorf("route %d ends with %q, want it to end by clearing the pane", tc.route, last)
		}
	}
}

// Nothing is typed into a shell nobody named.
func TestNoRouteTypesNothing(t *testing.T) {
	if got := Typed(NoRoute); len(got) != 0 {
		t.Errorf("%q was typed into no shell at all", got)
	}
}

// What is typed ends every line with a return, because this goes in
// where the keyboard goes in.
func TestEachLineEndsWithAReturn(t *testing.T) {
	got := string(Typed(Cmd))

	if strings.Contains(got, "\n") {
		t.Errorf("%q holds a newline, which is not what the enter key sends", got)
	}
	if !strings.HasSuffix(got, "\r") {
		t.Errorf("%q does not end with a return", got)
	}
	if n := strings.Count(got, "\r"); n != len(Lines(Cmd)) {
		t.Errorf("%d returns for %d lines", n, len(Lines(Cmd)))
	}
}

// The line for bash and zsh says where the shell is and marks each
// command, and starts with a space so a shell set to ignore those
// keeps it out of the history.
func TestTheLineForBashAndZsh(t *testing.T) {
	lines := Lines(Posix)
	setup := lines[0]

	if !strings.HasPrefix(setup, " ") {
		t.Error("it does not start with a space, so it goes into the history")
	}
	for _, want := range []string{`]7;file://`, `]133;A`, `]133;C`, `]133;D`} {
		if !strings.Contains(setup, want) {
			t.Errorf("it does not send %s", want)
		}
	}
	for _, want := range []string{"ZSH_VERSION", "PROMPT_COMMAND", "precmd", "preexec"} {
		if !strings.Contains(setup, want) {
			t.Errorf("it does not mention %s, so it does not cover both shells", want)
		}
	}
	if strings.Contains(setup, "\n") {
		t.Error("it is more than one line, and only one line can be typed in")
	}
}

// The PowerShell line keeps the prompt that was already there, so a
// profile that sets one is not thrown away.
func TestThePowerShellLineKeepsTheOldPrompt(t *testing.T) {
	setup := Lines(PowerShell)[0]

	if !strings.Contains(setup, "$function:prompt") {
		t.Error("it does not take hold of the prompt that was there")
	}
	if !strings.Contains(setup, "& $global:GtPrompt") {
		t.Error("it does not call the prompt that was there")
	}
	for _, want := range []string{`]7;file://`, `]133;A`, `]133;B`, `]133;C`, `]133;D`} {
		if !strings.Contains(setup, want) {
			t.Errorf("it does not send %s", want)
		}
	}
}

// The Command Prompt says where it is as a plain path. It builds its
// prompt out of what cmd.exe substitutes, and none of that makes a
// URL, so OSC 7 is not open to it.
func TestTheCommandPromptSaysWhereItIsAsAPath(t *testing.T) {
	setup := Lines(Cmd)[0]

	if !strings.Contains(setup, "]9;9;$p") {
		t.Errorf("%q does not send the directory as OSC 9;9", setup)
	}
	if strings.Contains(setup, "file://") {
		t.Errorf("%q tries to send a URL, which cmd.exe cannot build", setup)
	}
}

// shell is a shell running in a terminal of its own, for a test to type
// into and read.
type shell struct {
	t    *testing.T
	sess session.Session
	mu   sync.Mutex
	out  []byte
	done chan struct{}
}

// startShell runs argv in a terminal, with env added to its own.
func startShell(t *testing.T, argv, env []string) *shell {
	t.Helper()
	sess, err := session.StartLocal(session.LocalConfig{Command: argv, Env: env, Cols: 200, Rows: 50})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	sh := &shell{t: t, sess: sess, done: make(chan struct{})}
	go func() {
		defer close(sh.done)
		buf := make([]byte, 4096)
		for {
			n, err := sess.Read(buf)
			sh.mu.Lock()
			sh.out = append(sh.out, buf[:n]...)
			sh.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	return sh
}

// ownA is the A mark Shell Setup teaches a shell to send.
const ownA = "\x1b]133;A;" + OwnPrompt + "\a"

var mark = regexp.MustCompile("\x1b\\]133;([ACD])(;[^\a]*)?\a")

// marks are the marks the shell has sent, in order, as "A", "C" and
// "D;status", and A* for an A that is not Shell Setup's.
func (sh *shell) marks() []string {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	var got []string
	for _, m := range mark.FindAllSubmatch(sh.out, -1) {
		kind, opt := string(m[1]), string(m[2])
		switch {
		case kind == "A" && opt != ";"+OwnPrompt:
			got = append(got, "A*")
		case kind == "D":
			got = append(got, "D"+opt)
		default:
			got = append(got, kind)
		}
	}
	return got
}

// written is all the shell has written.
func (sh *shell) written() string {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	return string(sh.out)
}

// prompts is how many prompts Shell Setup's marks have marked.
func (sh *shell) prompts() int { return strings.Count(sh.written(), ownA) }

// waitFor waits for ok to hold.
func (sh *shell) waitFor(what string, ok func() bool) {
	sh.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			sh.t.Fatalf("waited for %s; the shell wrote %q", what, sh.written())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// waitPrompts waits for the nth prompt Shell Setup marks.
func (sh *shell) waitPrompts(n int) {
	sh.t.Helper()
	sh.waitFor(fmt.Sprintf("prompt %d", n), func() bool { return sh.prompts() >= n })
}

func (sh *shell) write(s string) {
	sh.t.Helper()
	if _, err := sh.sess.Write([]byte(s)); err != nil {
		sh.t.Fatal(err)
	}
}

// run types line and a return, and waits for the prompt after it.
func (sh *shell) run(line string) {
	sh.t.Helper()
	n := sh.prompts()
	sh.write(line + "\r")
	sh.waitPrompts(n + 1)
}

// setUp types before, if anything, then Shell Setup's lines, and waits
// for the prompt they leave the shell at.
func (sh *shell) setUp(before string) {
	sh.t.Helper()
	if before != "" {
		sh.write(before + "\r")
	}
	sh.write(string(Typed(Posix)))
	sh.waitPrompts(Prompts(Posix))
}

// exit types exit and waits for the shell to end.
func (sh *shell) exit() {
	sh.t.Helper()
	sh.write("exit\r")
	select {
	case <-sh.done:
	case <-time.After(10 * time.Second):
		sh.t.Fatalf("the shell did not exit; it wrote %q", sh.written())
	}
}

// marksOf runs a shell, argv, with env added to its own, types before
// into it, then Shell Setup's lines, then each of commands, each once
// the prompt before it is marked, and exit. It gives back the marks the
// shell sent from the first, in order, and all it wrote.
func marksOf(t *testing.T, argv, env []string, before string, commands ...string) ([]string, string) {
	t.Helper()
	sh := startShell(t, argv, env)
	sh.setUp(before)
	for _, c := range commands {
		sh.run(c)
	}
	sh.exit()
	return sh.marks(), sh.written()
}

// marksInOrder checks the marks a shell sent, got, and all it wrote,
// out: from Shell Setup's last prompt on, each prompt is D then A, each
// command C, and the D after a command has its status, as false, sh -c
// 'exit 3' and echo hi give them. And a C comes only once the line typed
// at the prompt before it has been echoed and ended: no C comes while
// the shell stands at its prompt.
func marksInOrder(t *testing.T, got []string, out string) {
	t.Helper()
	want := []string{"D;0", "A", "C", "D;1", "A", "C", "D;3", "A", "C", "D;0", "A", "C"}
	if len(got) < len(want) || !slices.Equal(got[len(got)-len(want):], want) {
		t.Fatalf("marks %q, want them to end %q", got, want)
	}
	if n := strings.Count(strings.Join(got, " "), "C"); n != 5 {
		t.Fatalf("marks %q: %d C, want one for clear, each command and exit", got, n)
	}
	noCAtAPrompt(t, out)
}

// noCAtAPrompt checks that no C mark came at a prompt Shell Setup
// marked before a line typed at it was echoed and ended.
func noCAtAPrompt(t *testing.T, out string) {
	t.Helper()
	const c = "\x1b]133;C\a"
	for i, part := range strings.Split(out, ownA)[1:] {
		at := strings.Index(part, c)
		if at >= 0 && !strings.Contains(part[:at], "\r\n") {
			t.Fatalf("prompt %d was marked C before a line was typed at it: %q", i+1, part)
		}
	}
}

// bashHere is the bash on this machine and its version as major*100 +
// minor, or skips the test.
func bashHere(t *testing.T) (string, int) {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash here")
	}
	v, err := exec.Command(bash, "-c", "echo $((BASH_VERSINFO[0]*100+BASH_VERSINFO[1]))").Output()
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(v)))
	if err != nil {
		t.Fatal(err)
	}
	return bash, n
}

// aBash is a kind of bash the line tells apart: the environment that
// makes the bash here take itself for one, and the version it takes.
type aBash struct {
	env     []string
	version int
}

// bashes are the kinds of bash the line tells apart: the bash here as it
// is, and, where it is newer, as a bash 4.4 to 5.0, with PS0 and no
// PROMPT_COMMAND array, and as a bash before 4.4, as macOS has, with
// neither.
func bashes(t *testing.T) map[string]aBash {
	t.Helper()
	_, here := bashHere(t)
	all := map[string]aBash{"as it is": {nil, here}}
	for name, v := range map[string]int{"as 5.0": 500, "as 4.3": 403} {
		if v < here {
			all[name] = aBash{[]string{"__gt_v=" + strconv.Itoa(v)}, v}
		}
	}
	return all
}

// plainBash is bash with none of the user's startup files.
func plainBash(t *testing.T) []string {
	t.Helper()
	bash, _ := bashHere(t)
	return []string{bash, "--norc", "--noprofile"}
}

// Taught by Shell Setup, bash marks each prompt D then A, and each
// command C before its output, with the command's own status in the D
// after it, whatever the user's own PROMPT_COMMAND runs, and with
// extdebug on. The user's PROMPT_COMMAND sees the command's status too.
func TestBashMarksEachPromptAndEachCommand(t *testing.T) {
	for kind, b := range bashes(t) {
		for name, tc := range map[string]struct {
			env    []string
			before string
		}{
			"plain":                   {},
			"history -a":              {env: []string{"PROMPT_COMMAND=history -a"}},
			"ending in a semicolon":   {env: []string{"PROMPT_COMMAND=history -a;"}},
			"failing":                 {env: []string{"PROMPT_COMMAND=false"}},
			"reading the status":      {env: []string{"PROMPT_COMMAND=printf '<%s>' $?"}},
			"an array, as bash 5.1's": {before: "PROMPT_COMMAND=('history -a' 'printf \"<%s>\" $?' false)"},
			"extdebug":                {before: "shopt -s extdebug"},
			"set -u":                  {before: "set -u"},
		} {
			if strings.Contains(name, "array") && b.version < 501 {
				// An older bash has no PROMPT_COMMAND array to run.
				continue
			}
			t.Run(kind+"/"+name, func(t *testing.T) {
				got, out := marksOf(t, plainBash(t), append(tc.env, b.env...), tc.before, "false", "sh -c 'exit 3'", "echo hi")
				marksInOrder(t, got, out)
				if strings.Contains(name, "status") || strings.Contains(name, "array") {
					if !strings.Contains(out, "<1>") || !strings.Contains(out, "<3>") {
						t.Fatalf("the user's PROMPT_COMMAND was not shown the status: %q", out)
					}
				}
				if strings.Contains(out, "unbound variable") {
					t.Fatalf("the line used a variable that was not set: %q", out)
				}
			})
		}
	}
}

// Shell Setup's line in bash keeps the user's own: a debug trap, as
// bash-preexec, atuin and starship set, still runs before each command,
// and a preexec hook kept the way bash-preexec keeps it, which takes a
// command run by PROMPT_COMMAND for none of the user's, still fires.
func TestBashKeepsTheUsersDebugTrap(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash is not the Windows shell")
	}
	// What bash-preexec does: its hook ends PROMPT_COMMAND, and its trap
	// fires for the first command after it that PROMPT_COMMAND does not
	// run.
	const preexec = `__m=; __in(){ case $'\n'"$PROMPT_COMMAND"$'\n' in *$'\n'"$1"$'\n'*) return 0;; esac; return 1; }; ` +
		`__pre(){ [ -n "$__m" ] || return 0; __m=; __in "$BASH_COMMAND" && return 0; printf '<preexec %s>' "$BASH_COMMAND"; }; ` +
		`trap '__pre' DEBUG; PROMPT_COMMAND="history -a"$'\n''__m=on'`
	for kind, b := range bashes(t) {
		t.Run(kind, func(t *testing.T) {
			sh := startShell(t, plainBash(t), b.env)
			sh.setUp(preexec)
			sh.run("echo hi")
			sh.run("true")
			sh.exit()
			if out := sh.written(); !strings.Contains(out, "<preexec echo hi>") || !strings.Contains(out, "<preexec true>") {
				t.Fatalf("the user's preexec hook did not run for each command: %q", out)
			}
		})
	}
}

// From bash 4.4 no C comes at a prompt standing empty, whatever runs
// there: a key bound with bind -x, as fzf's Ctrl+R is, and hooks added
// to the end and the front of PROMPT_COMMAND after Shell Setup, as a
// user or direnv adds them.
func TestBashMarksNoCommandAtAnEmptyPrompt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash is not the Windows shell")
	}
	for kind, b := range bashes(t) {
		if b.version < 404 {
			// Before 4.4 the debug trap stands in, and fires for these.
			continue
		}
		t.Run(kind, func(t *testing.T) {
			sh := startShell(t, plainBash(t), b.env)
			sh.setUp(`bind -x '"\C-t": printf "<key>"'`)
			sh.write("\x14")
			sh.waitFor("the bound key", func() bool { return strings.Contains(sh.written(), "<key>") })
			sh.run(`PROMPT_COMMAND="$PROMPT_COMMAND;printf '<end>'"`)
			sh.run(`PROMPT_COMMAND="printf '<front>';$PROMPT_COMMAND"`)
			sh.run("")
			sh.run("")
			sh.run("echo hi")
			sh.exit()
			out := sh.written()
			if !strings.Contains(out, "<end>") || !strings.Contains(out, "<front>") {
				t.Fatalf("the hooks did not run: %q", out)
			}
			noCAtAPrompt(t, out)
			// clear, the two hooks, echo hi and exit.
			if n := strings.Count(strings.Join(sh.marks(), " "), "C"); n != 5 {
				t.Fatalf("marks %q: %d C, want 5", sh.marks(), n)
			}
		})
	}
}

// Nothing Shell Setup's line runs fails where set -e or an ERR trap
// sees it: with set -e, Ctrl+C at an empty prompt and ! true leave the
// shell running, and an ERR trap fires once for false and never for an
// empty line, as it does in a shell with no Shell Setup.
func TestBashLeavesSetEAndTheErrTrapAlone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash is not the Windows shell")
	}
	for kind, b := range bashes(t) {
		t.Run(kind, func(t *testing.T) {
			sh := startShell(t, plainBash(t), b.env)
			sh.setUp(`trap 'printf "<E%s>" RR' ERR`)
			sh.run("false")
			sh.run("")
			sh.run("")
			sh.run("true")
			if n := strings.Count(sh.written(), "<ERR>"); n != 1 {
				t.Fatalf("the ERR trap fired %d times, want once: %q", n, sh.written())
			}
			sh.run("set -e")
			n := sh.prompts()
			sh.write("\x03")
			sh.waitPrompts(n + 1)
			sh.run("! true")
			sh.run("echo ALIVE")
			sh.exit()
			if !strings.Contains(sh.written(), "ALIVE") {
				t.Fatalf("the shell ended under set -e: %q", sh.written())
			}
		})
	}
}

// From bash 5.1, what the user's PROMPT_COMMAND runs sees PIPESTATUS as
// the command left it.
func TestBashShowsThePromptCommandThePipeStatus(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash is not the Windows shell")
	}
	if _, v := bashHere(t); v < 501 {
		t.Skip("bash before 5.1 has no PROMPT_COMMAND array")
	}
	_, out := marksOf(t, plainBash(t), nil, `PROMPT_COMMAND='printf "<%s>" "${PIPESTATUS[*]}"'`, "false | true | false")
	if !strings.Contains(out, "<1 0 1>") {
		t.Fatalf("the user's PROMPT_COMMAND did not see PIPESTATUS: %q", out)
	}
}

// A bash started inside the pane inherits an exported PROMPT_COMMAND
// and none of Shell Setup's functions, and says nothing of them.
func TestABashInsideThePaneSaysNothing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash is not the Windows shell")
	}
	for kind, b := range bashes(t) {
		t.Run(kind, func(t *testing.T) {
			sh := startShell(t, plainBash(t), append([]string{"PROMPT_COMMAND=history -a"}, b.env...))
			sh.setUp("export PROMPT_COMMAND")
			n := sh.prompts()
			sh.write(plainBash(t)[0] + " --norc --noprofile\r")
			sh.write("false\r")
			sh.write("echo INSIDE\r")
			sh.write("exit\r")
			sh.waitPrompts(n + 1)
			sh.exit()
			out := sh.written()
			if !strings.Contains(out, "INSIDE") {
				t.Fatalf("the bash inside did not run: %q", out)
			}
			if strings.Contains(out, "not found") {
				t.Fatalf("the bash inside called Shell Setup's functions: %q", out)
			}
		})
	}
}

// A PROMPT_COMMAND set over Shell Setup's, keeping __gt_d alone, reports
// each command's own status, not one saved earlier.
func TestBashReportsNoStaleStatus(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash is not the Windows shell")
	}
	for kind, b := range bashes(t) {
		t.Run(kind, func(t *testing.T) {
			got, _ := marksOf(t, plainBash(t), b.env, "", "false", "PROMPT_COMMAND=__gt_d", "false", "true")
			want := []string{"D;1", "A", "C", "D;0", "A"}
			if len(got) < len(want) || !slices.Equal(got[len(got)-len(want)-1:len(got)-1], want) {
				t.Fatalf("marks %q, want them to end %q and exit's C", got, want)
			}
		})
	}
}

// zsh, taught by the same line, marks the same way.
func TestZshMarksEachPromptAndEachCommand(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("no zsh here")
	}
	got, out := marksOf(t, []string{zsh, "-f"}, nil, "", "false", "sh -c 'exit 3'", "echo hi")
	marksInOrder(t, got, out)
}

// A shell's own OSC 133 marks are told from Shell Setup's: those it
// teaches carry kakel's option.
func TestShellSetupsPromptsAreItsOwn(t *testing.T) {
	sh := startShell(t, plainBash(t), nil)
	sh.setUp(`PS1='\[\e]133;A\a\]\$ '`)
	sh.run("true")
	sh.exit()
	got := sh.marks()
	if !slices.Contains(got, "A*") || !slices.Contains(got, "A") {
		t.Fatalf("marks %q, want the shell's own A* and Shell Setup's A apart", got)
	}
}
