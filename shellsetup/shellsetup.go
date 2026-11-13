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
		// The leading space keeps them out of the history of a shell set
		// to ignore lines that start with one.
		return []string{" " + posixLine, " " + posixCommandLine, "clear"}
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
//
// So the first line is the one that hooks the prompt, in every shell.
func Prompts(r Route) int { return len(Lines(r)) }

// OwnPrompt is the option Shell Setup puts on the OSC 133 A mark it
// teaches a shell to send, so a prompt kakel's own lines mark is told
// from one a shell's own prompt marks as well. Other terminals ignore
// an option they do not know.
const OwnPrompt = "kakel=1"

// posixLine tells bash and zsh to mark the directory and each prompt, and
// zsh each command. posixCommandLine, typed after it, tells bash to mark
// each command. They are two lines so that each stays well under the
// 1024 bytes a terminal on macOS or BSD takes in one line while the shell
// is still starting.
//
// __gt_d runs before each prompt: it reports how the last command went,
// from $? or the status it is handed, says where the shell is, and marks
// the prompt. __gt_c marks where a command's output starts, once until
// the next prompt; __gt_r says it has. zsh has hooks for both. sh, dash
// and ksh get the functions and nothing calls them, and only parse the
// rest, so nothing in it is bash's own syntax outside eval.
//
// A bash where bash-preexec hooked the prompt, with __bp_precmd in
// PROMPT_COMMAND, has hooks for both too, and they are used: bash-preexec
// tells its own commands from the user's by the pieces of PROMPT_COMMAND,
// and an entry of kakel's there would make it take the user's last
// command for a new one at every empty prompt. bash-preexec loaded but
// not hooked, as when a later line sets PROMPT_COMMAND afresh, calls no
// hooks, so that bash gets kakel's own entry.
//
// That entry is __gt_p, first in PROMPT_COMMAND, which stays a string, or
// an array when the user made it one, with the entry at the front of its
// first element: a tool that checks whether it is in PROMPT_COMMAND
// already still finds itself, and a child bash, which an exported
// PROMPT_COMMAND reaches, still gets it. __gt_p hands back the status for
// what follows, and $_ as it was. It is called through ${__gt_k-}, so a
// child bash, which has none of these functions, runs $(exit $?) in its
// place, which keeps the status for the user's part. A semicolon parts it
// from the user's part, so a tool looking for ;its_hook; finds it, and
// none follows it alone, so one that adds ;its_hook makes no ;;.
//
// __gt_h says the prompt is hooked, and how: 2 through bash-preexec, 1
// through PROMPT_COMMAND. A prompt hooked already is left as it is, so the
// lines typed twice mark each prompt once. __gt_h stays empty where
// PROMPT_COMMAND is read only, and then nothing marks a command either, as
// a command marked with no prompt after it would be taken to run for good.
//
// No command here leaves a failing status where set -e or an ERR trap
// sees it: a status handed back is the first of an && list. __gt_v is
// bash's version as major*100+minor, and a test sets it to try older ones.
const posixLine = `__gt_d(){ local c=${1:-$?}; __gt_r=; printf '\033]133;D;%s\a\033]7;file://%s%s\a\033]133;A;` + OwnPrompt + `\a' "$c" "${HOSTNAME:-${HOST:-localhost}}" "$PWD"; }; ` +
	`__gt_c(){ [ -n "${__gt_r-}" ] || { __gt_r=1; printf '\033]133;C\a'; }; }; ` +
	`if [ -n "${ZSH_VERSION-}" ]; then autoload -Uz add-zsh-hook; add-zsh-hook precmd __gt_d; add-zsh-hook preexec __gt_c; ` +
	`elif [ -n "${BASH_VERSION-}" ]; then __gt_v=${__gt_v:-$((BASH_VERSINFO[0]*100+BASH_VERSINFO[1]))}; __gt_h=; ` +
	`case ${bash_preexec_imported:+y}${__bp_imported:+y}:${__gt_k-}${PROMPT_COMMAND-} in *:__gt_p*__gt_k*) __gt_h=1;; ` +
	`y*:*__bp_precmd*) eval '[[ ${precmd_functions[*]-} = *__gt_d* ]] || precmd_functions+=(__gt_d)'; __gt_h=2;; ` +
	`*) __gt_p(){ local c=$?; __gt_d "$c"; __gt_r=1; return $c; }; __gt_k=__gt_p; ` +
	`PROMPT_COMMAND='${__gt_k-$(exit $?)} ${__gt_k+"$_"} && : __gt_ "$_"'"${PROMPT_COMMAND:+;$PROMPT_COMMAND}" && __gt_h=1;; esac; fi`

// posixCommandLine tells bash to mark each command, once posixLine has
// hooked its prompt.
//
// bash 4.4 and later print PS0 before each command they read from the
// prompt, and never at an empty one, so the C mark goes at the front of
// it, as the bytes themselves: they mean the same whether promptvars is
// on or off. A block of lines pasted at once, or an alias holding more
// than one line, is read as one line and gets one PROMPT_COMMAND after
// it, so each of its commands is marked C and one D ends them all. A PS0
// that marks C already is left as it is.
//
// There is no hook before a command in older bash, and the debug trap
// stands in: it fires for every command bash runs, those in
// PROMPT_COMMAND too, so __gt_p sets __gt_r as the prompt's commands
// start and the last one clears it. The trap that was there before runs
// after __gt_c, with $_ as it was, and neither runs for kakel's own
// commands or, with extdebug, for those inside kakel's functions. The
// trap is one call, to __gt_t, so a tool that keeps a trap it finds by
// running its first word, or all of it, keeps this one. A trap that is
// kakel's already is left as it is. With bash-preexec, its preexec hook
// stands in.
const posixCommandLine = `if [ -n "${__gt_h-}" ]; then if [ "$__gt_v" -ge 404 ]; then case ${PS0-} in *'133;C'*) ;; *) PS0=$'\033]133;C\a'${PS0-};; esac; ` +
	`elif [ "$__gt_h" = 2 ]; then eval 'preexec_functions+=(__gt_c)'; ` +
	`else __gt_x=$(trap -p DEBUG); __gt_x=${__gt_x#trap -- }; eval "__gt_x=${__gt_x% DEBUG}"; case $__gt_x in *__gt_*) ;; *) __gt_o=$__gt_x; ` +
	`__gt_t(){ case $BASH_COMMAND${FUNCNAME[1]-} in *__gt_*) return 0;; esac; __gt_c; : "$1"; eval "$__gt_o"; }; trap '__gt_t "$_"' DEBUG; ` +
	`PROMPT_COMMAND="$PROMPT_COMMAND"$'\n''__gt_r=';; esac; fi; fi`

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
