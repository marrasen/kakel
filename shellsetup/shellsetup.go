// Package shellsetup holds the lines that teach a shell to say what it
// is doing.
//
// A shell is a separate program, and kakel only sees the bytes it
// prints. So it cannot know which directory the shell is in, or where
// one command's output ends and the next begins, unless the shell says
// so. The shell says so by printing escape sequences nobody sees: OSC 7
// for the directory, OSC 133 around each command.
//
// The lines here are typed into the shell as soon as it starts, the way
// a user would type them. That is the one way that works everywhere: on
// a server the login shell is started by sshd and kakel never sees a
// command line to add to.
package shellsetup

import (
	"strings"

	"github.com/marrasen/kakel/shells"
)

// Route is the kind of shell the lines are written for.
type Route int

const (
	// NoRoute is a shell nothing is typed into.
	NoRoute Route = iota

	// Posix is bash, zsh and anything close enough to them, which is
	// what a server's login shell is taken to be.
	Posix

	// PowerShell is Windows PowerShell and PowerShell.
	PowerShell

	// Cmd is the Command Prompt.
	Cmd
)

// RouteFor is the kind of shell an argv runs.
//
// An empty argv means a login shell on a machine at the far end, which
// is taken to be POSIX: sshd starts whatever the account is set to, and
// nothing in the protocol says what that is.
func RouteFor(argv []string) Route {
	if len(argv) == 0 {
		return Posix
	}
	switch strings.ToLower(shells.CommandBase(argv[0])) {
	case "cmd", "cmd.exe":
		return Cmd
	case "powershell", "powershell.exe", "pwsh", "pwsh.exe":
		return PowerShell
	}
	return Posix
}

// Knows reports whether argv runs a shell the lines here are written
// for. A program it does not know, such as nushell, python or ssh,
// would take them as its own input, so a profile running one is left
// alone.
//
// A shell handed a command or a script, as by bash -c, cmd /c or
// pwsh -File, runs that instead of reading its terminal, and the lines
// would go to the command. So it is not known either.
//
// wsl is a shell only when it starts the distribution's login shell. A
// command it is given, as in wsl -e nu or wsl htop, is the program the
// lines would reach, so that is the one asked about.
func Knows(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	args := argv[1:]
	switch strings.TrimSuffix(strings.ToLower(shells.CommandBase(argv[0])), ".exe") {
	case "bash", "zsh", "sh", "ksh", "dash":
		return !posixScripted(args)
	case "cmd":
		return !cmdScripted(args)
	case "powershell", "pwsh":
		return !powerShellScripted(args)
	case "wsl":
		if run := wslRuns(args); len(run) > 0 {
			return Knows(run)
		}
		return true
	}
	return false
}

// posixScripted reports whether a POSIX shell's arguments give it
// something to run: -c, alone or among other letters as in -lc, or a
// script, the first word that is not an option.
func posixScripted(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			return i+1 < len(args)
		case arg == "-o" || arg == "+o" || arg == "-O" || arg == "+O" || arg == "--rcfile" || arg == "--init-file":
			// Each takes the word after it as its value.
			i++
		case strings.HasPrefix(arg, "--") || strings.HasPrefix(arg, "+"):
		case strings.HasPrefix(arg, "-"):
			if strings.Contains(arg, "c") {
				return true
			}
		default:
			return true
		}
	}
	return false
}

// cmdScripted reports whether cmd's arguments give it a command to run
// and end with, /c. A command given with /k leaves the prompt open, and
// the words after /k are that command, not cmd's own.
func cmdScripted(args []string) bool {
	for _, arg := range args {
		if len(arg) < 2 || arg[0] != '/' {
			continue
		}
		switch arg[1] | 0x20 {
		case 'c':
			return true
		case 'k':
			return false
		}
	}
	return false
}

// powerShellScripted reports whether PowerShell's arguments give it a
// command, a script or an encoded command to run, unless -NoExit before
// it keeps the prompt open. PowerShell takes a parameter cut short, as
// -c or -com for -Command, so any such start counts.
func powerShellScripted(args []string) bool {
	stays := false
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			continue
		}
		name := strings.ToLower(strings.TrimLeft(arg, "-"))
		switch {
		case len(name) >= 3 && strings.HasPrefix("noexit", name):
			stays = true
		case name == "ec" || name != "" && (strings.HasPrefix("command", name) || strings.HasPrefix("file", name) || strings.HasPrefix("encodedcommand", name)):
			return !stays
		}
	}
	return false
}

// wslRuns is the command wsl's arguments run in the distribution, nil
// for its login shell: what follows -e, --exec or --, or else the first
// word that is not an option, which wsl runs through that shell.
func wslRuns(args []string) []string {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-e", "--exec", "--":
			return args[i+1:]
		case "-d", "--distribution", "--distribution-id", "-u", "--user", "--cd", "--shell-type":
			// Each takes the word after it as its value.
			i++
		case "~":
			// Starts the shell in the home folder, as Windows Terminal's
			// wsl.exe ~ -d Ubuntu does.
		default:
			if !strings.HasPrefix(args[i], "-") {
				return args[i:]
			}
		}
	}
	return nil
}

// Lines are what to type into a shell of that kind, one line each,
// ending with the one that clears the pane.
//
// The pane is cleared because the shell echoes what is typed into it,
// and a pane that opens on a screenful of setup is worse than one that
// opens empty. A shell's own banner goes with it.
func Lines(r Route) []string {
	switch r {
	case Posix:
		// The leading space keeps it out of the history of a shell set
		// to ignore lines that start with one.
		return []string{" " + posixLine, "clear"}
	case PowerShell:
		return []string{powerShellLine}
	case Cmd:
		// prompt takes the rest of its line literally, so the clear
		// cannot be joined onto it.
		return []string{cmdPrompt, "cls"}
	}
	return nil
}

// Prompts is how many prompts a shell taught as r shows, marked, while
// it runs the lines: one after each line, as the first prompt comes
// before the first line has taught it to mark one. The last of them is
// the first prompt at which the shell is ready for what the user types.
func Prompts(r Route) int { return len(Lines(r)) }

// OwnPrompt is the option Shell Setup puts on the OSC 133 A mark it
// teaches a shell to send, so a prompt kakel's own lines mark is told
// from one a shell's own prompt marks as well. Other terminals ignore
// an option they do not know.
const OwnPrompt = "kakel=1"

// posixLine tells bash and zsh to mark the directory and each command.
//
// __gt_d runs before each prompt: it reports how the last command went,
// from $? or the status it is handed, says where the shell is, and marks
// the prompt. __gt_c marks where a command's output starts, once per
// command line; __gt_r says it has. zsh has hooks for both. sh, dash and
// ksh get the functions and nothing calls them.
//
// bash 4.4 and later print PS0 once for each command line they read, and
// never at an empty one, so the C mark goes at the front of it. There is
// no hook before a command in older bash, and the debug trap stands in:
// it fires for every command bash runs, those in PROMPT_COMMAND too, so
// __gt_p sets __gt_r as the prompt's commands start and the last one
// clears it. The trap that was there before runs after __gt_c, with $_ as
// it was, and neither runs for kakel's own commands or, with extdebug,
// for those inside kakel's functions.
//
// __gt_d goes first in PROMPT_COMMAND, as a hook after the one that ends
// bash-preexec's own list would make it think the next command is part of
// the prompt. From bash 5.1 PROMPT_COMMAND is an array, each element of
// which sees the status and PIPESTATUS as the command left them. Before
// it, PROMPT_COMMAND is one string, and __gt_p hands the status back for
// what follows it; PIPESTATUS is not to be had. __gt_p is called through
// ${__gt_k-}, which in a bash started inside the pane, which inherits an
// exported PROMPT_COMMAND and none of the functions, is nothing at all.
//
// No command here leaves a failing status where set -e or an ERR trap
// sees it: a status handed back is the first of an && list. __gt_v is
// bash's version as major*100+minor, and a test sets it to try older ones.
const posixLine = `__gt_d(){ local c=${1:-$?}; __gt_r=; printf '\033]133;D;%s\a\033]7;file://%s%s\a\033]133;A;` + OwnPrompt + `\a' "$c" "${HOSTNAME:-${HOST:-localhost}}" "$PWD"; }; ` +
	`__gt_c(){ [ -n "${__gt_r-}" ] || { __gt_r=1; printf '\033]133;C\a'; }; }; ` +
	`if [ -n "${ZSH_VERSION-}" ]; then autoload -Uz add-zsh-hook; add-zsh-hook precmd __gt_d; add-zsh-hook preexec __gt_c; ` +
	`elif [ -n "${BASH_VERSION-}" ]; then __gt_v=${__gt_v:-$((BASH_VERSINFO[0]*100+BASH_VERSINFO[1]))}; ` +
	`if [ "$__gt_v" -ge 501 ]; then eval 'PROMPT_COMMAND=(__gt_d "${PROMPT_COMMAND[@]}")'; ` +
	`else __gt_p(){ local c=$?; __gt_d "$c"; __gt_r=1; return $c; }; __gt_k=__gt_p; PROMPT_COMMAND='${__gt_k-} && :'$'\n'"${PROMPT_COMMAND-}"; fi; ` +
	`if [ "$__gt_v" -ge 404 ]; then PS0=$'\033]133;C\a'"${PS0-}"; ` +
	`else PROMPT_COMMAND="$PROMPT_COMMAND"$'\n''__gt_r='; __gt_o=$(trap -p DEBUG); __gt_o=${__gt_o#trap -- }; eval "__gt_o=${__gt_o% DEBUG}"; ` +
	`trap -- 'case $BASH_COMMAND${FUNCNAME-} in *__gt_*) ;; *) __gt_c "$_"'$'\n'"$__gt_o"$'\n'';; esac' DEBUG; fi; fi`

// powerShellLine wraps the prompt that is already there rather than
// replacing it, so a profile that sets one keeps it.
//
// The command mark goes on the function PSReadLine calls to read a
// line. Without PSReadLine the host reads the line itself and never
// calls it, so there is no mark and the rest still works.
const powerShellLine = `$global:GtPrompt=$function:prompt; ` +
	`function global:prompt { $ok=$?; $e=[char]27; $b=[char]7; $c=0; ` +
	`if(-not $ok){ if($global:LASTEXITCODE){$c=$global:LASTEXITCODE}else{$c=1} }; ` +
	`$h=(Get-Location).Path; ` +
	`Write-Host -NoNewline "$e]133;D;$c$b$e]7;file://localhost/$([uri]::EscapeUriString(($h -replace '\\','/')))$b$e]133;A;` + OwnPrompt + `$b"; ` +
	`$t=if($global:GtPrompt){& $global:GtPrompt}else{"PS $h> "}; "$t$e]133;B$b" }; ` +
	`if($function:PSConsoleHostReadLine){ $global:GtRead=$function:PSConsoleHostReadLine; ` +
	`function global:PSConsoleHostReadLine { $l=& $global:GtRead; Write-Host -NoNewline "$([char]27)]133;C$([char]7)"; $l } }; ` +
	`Clear-Host`

// cmdPrompt is the Command Prompt saying where it is.
//
// OSC 9;9 rather than OSC 7, because the prompt is built from what
// cmd.exe substitutes and it has no way to make a URL out of a path.
// $e is an escape, $p the directory and $g a greater-than sign.
const cmdPrompt = `prompt $e]9;9;$p$e\$p$g`

// Typed is the bytes to write to a shell's input, with a return after
// each line.
//
// A return rather than a newline: this goes in where the keyboard goes
// in, and the key sends a return.
func Typed(r Route) []byte {
	lines := Lines(r)
	if len(lines) == 0 {
		return nil
	}
	return []byte(strings.Join(lines, "\r") + "\r")
}
