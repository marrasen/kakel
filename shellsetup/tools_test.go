package shellsetup

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// Tools that hook bash's prompt, as a user's .bashrc loads them, and
// Shell Setup's lines typed after them.
//
// None of them is here: each is a stand-in for the parts of it that
// touch PROMPT_COMMAND, the DEBUG trap and PS0, written after its
// published text, with a function in place of its program.
// KAKEL_TEST_BASH_PREEXEC names a folder holding the real bash-preexec,
// as 0.5.0/bash-preexec.sh, 0.6.0/bash-preexec.sh and
// master/bash-preexec.sh, to run in place of the stand-in.

// bashPreexec is a stand-in for bash-preexec
// (github.com/rcaloras/bash-preexec): how it is installed at the first
// prompt, how it tells its own commands from the user's by the pieces of
// PROMPT_COMMAND, and when it runs precmd_functions and
// preexec_functions, as 0.5.0, 0.6.0 and master have it. __bp_sv names
// which. master's PS0 hook, for bash 5.3, is left out.
const bashPreexec = `
if [[ -n "${bash_preexec_imported:-}" ]] || [[ $__bp_sv != 0.5.0 && -n "${__bp_imported:-}" ]]; then return 0; fi
bash_preexec_imported=defined
__bp_imported=$bash_preexec_imported
__bp_last_ret_value=$?
__bp_inside_precmd=0
__bp_inside_preexec=0
__bp_preexec_interactive_mode=
if [[ $__bp_sv == master ]]; then
    __bp_install_string='__bp_install "$_"'
else
    __bp_install_string=$'__bp_trap_string="$(trap -p DEBUG)"\ntrap - DEBUG\n__bp_install'
fi
__bp_master() { [[ $__bp_sv == master ]]; }
__bp_use_array_prompt_command() {
    __bp_master && (( BASH_VERSINFO[0] > 5 || (BASH_VERSINFO[0] == 5 && BASH_VERSINFO[1] >= 1) ))
}
__bp_require_not_readonly() {
    local var
    for var; do
        if ! ( unset "$var" 2> /dev/null ); then
            echo "bash-preexec requires write access to ${var}" >&2
            return 1
        fi
    done
}
__bp_trim_whitespace() {
    local var=${1:?} text=${2:-}
    text="${text#"${text%%[![:space:]]*}"}"
    text="${text%"${text##*[![:space:]]}"}"
    printf -v "$var" '%s' "$text"
}
__bp_sanitize_string() {
    local var=${1:?} sanitized=${2:-}
    if __bp_master; then
        local unset_extglob=
        if ! shopt -q extglob; then unset_extglob=yes; shopt -s extglob; fi
        local tmp nl=$'\n'
        while
            tmp="${sanitized//?(+([[:blank:]]))[";$nl"]*([[:blank:]]):*([[:blank:]])[";$nl"]*([[:blank:]])/$nl}"
            [[ "$tmp" != "$sanitized" ]]
        do
            sanitized="$tmp"
        done
        sanitized="${sanitized#:*([[:blank:]])[";$nl"]}"
        sanitized="${sanitized%[";$nl"]*([[:blank:]]):}"
        [[ -n "$unset_extglob" ]] && shopt -u extglob
    fi
    __bp_trim_whitespace sanitized "$sanitized"
    sanitized=${sanitized%;}
    sanitized=${sanitized#;}
    __bp_trim_whitespace sanitized "$sanitized"
    if __bp_master && [[ "$sanitized" == ":" ]]; then sanitized=; fi
    printf -v "$var" '%s' "$sanitized"
}
__bp_remove_command_from_prompt_command() {
    local removed_command="${1-}" i sanitized_prompt_command
    if __bp_use_array_prompt_command; then
        for i in "${!PROMPT_COMMAND[@]}"; do
            sanitized_prompt_command="${PROMPT_COMMAND[i]:-}"
            sanitized_prompt_command="${sanitized_prompt_command//"$removed_command"/:}"
            __bp_sanitize_string sanitized_prompt_command "$sanitized_prompt_command"
            if [[ -n "$sanitized_prompt_command" ]]; then
                PROMPT_COMMAND[i]="$sanitized_prompt_command"
            else
                unset -v 'PROMPT_COMMAND[i]'
            fi
        done
    else
        sanitized_prompt_command="${PROMPT_COMMAND:-}"
        sanitized_prompt_command="${sanitized_prompt_command//"$removed_command"/:}"
        __bp_sanitize_string PROMPT_COMMAND "$sanitized_prompt_command"
    fi
}
__bp_interactive_mode() {
    if __bp_master && [[ "${1-}" != force ]] && (( ${#FUNCNAME[*]} > 1 )); then
        return 0
    fi
    __bp_preexec_interactive_mode="on"
}
__bp_precmd_invoke_cmd() {
    __bp_last_ret_value="$?" __bp_last_argument_prev_command="$_" BP_PIPESTATUS=("${PIPESTATUS[@]}")
    if (( __bp_inside_precmd > 0 )); then
        return "$__bp_last_ret_value"
    fi
    if __bp_master && ! __bp_install_prompt_command; then
        if [[ "${1-}" != force ]] && (( ${#FUNCNAME[*]} > 1 )); then
            return "$__bp_last_ret_value"
        fi
    fi
    local __bp_inside_precmd=1 precmd_function
    for precmd_function in "${precmd_functions[@]}"; do
        if type -t "$precmd_function" 1>/dev/null; then
            __bp_set_ret_value "$__bp_last_ret_value" "$__bp_last_argument_prev_command"
            "$precmd_function"
        fi
    done
    __bp_set_ret_value "$__bp_last_ret_value" "$__bp_last_argument_prev_command"
}
__bp_set_ret_value() {
    return ${1:+"$1"}
}
__bp_in_prompt_command() {
    local prompt_command_array IFS=$'\n;'
    if [[ $__bp_sv == 0.5.0 ]]; then
        read -rd '' -a prompt_command_array <<< "${PROMPT_COMMAND:-}"
    else
        read -rd '' -a prompt_command_array <<< "${PROMPT_COMMAND[*]:-}"
    fi
    local trimmed_arg
    __bp_trim_whitespace trimmed_arg "${1:-}"
    local command trimmed_command
    for command in "${prompt_command_array[@]:-}"; do
        __bp_trim_whitespace trimmed_command "$command"
        if [[ "$trimmed_command" == "$trimmed_arg" ]]; then
            return 0
        fi
    done
    return 1
}
__bp_preexec_invoke_exec() {
    __bp_last_argument_prev_command="${1:-}"
    if (( __bp_inside_preexec > 0 )); then
        return
    fi
    local __bp_inside_preexec=1
    if [[ ! -t 1 ]]; then
        return
    fi
    if [[ $__bp_sv == 0.5.0 ]]; then
        if [[ -n "${COMP_LINE:-}" ]]; then return; fi
    elif [[ -n "${COMP_POINT:-}" || -n "${READLINE_POINT:-}" ]]; then
        return
    fi
    if [[ -z "${__bp_preexec_interactive_mode:-}" ]]; then
        return
    elif [[ 0 -eq "${BASH_SUBSHELL:-}" ]]; then
        __bp_preexec_interactive_mode=""
    fi
    if __bp_in_prompt_command "${BASH_COMMAND:-}"; then
        __bp_preexec_interactive_mode=""
        return
    fi
    local this_command
    this_command=$(LC_ALL=C HISTTIMEFORMAT='' builtin history 1)
    this_command="${this_command#*[[:digit:]][* ] }"
    [[ -n "$this_command" ]] || return
    local preexec_function preexec_function_ret_value preexec_ret_value=0
    for preexec_function in "${preexec_functions[@]:-}"; do
        if type -t "$preexec_function" 1>/dev/null; then
            __bp_set_ret_value "${__bp_last_ret_value:-}" "$__bp_last_argument_prev_command"
            "$preexec_function" "$this_command"
            preexec_function_ret_value="$?"
            if [[ "$preexec_function_ret_value" != 0 ]]; then
                preexec_ret_value="$preexec_function_ret_value"
            fi
        fi
    done
    __bp_set_ret_value "$preexec_ret_value" "$__bp_last_argument_prev_command"
}
__bp_install_prompt_command() {
    local prompt_command="${PROMPT_COMMAND:-}"
    if __bp_use_array_prompt_command; then
        local IFS=$'\n'
        prompt_command="${PROMPT_COMMAND[*]:-}"
        IFS=$' \t\n'
    fi
    local prologue='__bp_precmd_invoke_cmd "$_"'
    local epilogue='__bp_interactive_mode'
    if [[ "$prompt_command" == "$prologue"$'\n'* && "$prompt_command" == *$'\n'"$epilogue" ]]; then
        return 1
    fi
    __bp_remove_command_from_prompt_command "$prologue"
    __bp_remove_command_from_prompt_command "$epilogue"
    PROMPT_COMMAND=$prologue${PROMPT_COMMAND:+$'\n'$PROMPT_COMMAND}
    if __bp_use_array_prompt_command; then
        PROMPT_COMMAND+=("$epilogue")
    else
        PROMPT_COMMAND+=$'\n'$epilogue
    fi
    return 0
}
__bp_install() {
    if [[ "${PROMPT_COMMAND[*]:-}" == *__bp_precmd_invoke_cmd* ]]; then
        return 1
    fi
    local trap_string
    if __bp_master; then trap_string=$(trap -p DEBUG); else trap_string=${__bp_trap_string:-}; fi
    trap '__bp_preexec_invoke_exec "$_"' DEBUG
    eval "local trap_argv=(${trap_string:-})"
    local prior_trap=${trap_argv[2]:-}
    unset __bp_trap_string
    if [[ -n "$prior_trap" ]]; then
        eval '__bp_original_debug_trap() {
            '"$prior_trap"'
        }'
        preexec_functions+=(__bp_original_debug_trap)
    fi
    if __bp_master; then
        __bp_remove_command_from_prompt_command "$__bp_install_string"
        __bp_install_prompt_command || true
    else
        local existing_prompt_command="${PROMPT_COMMAND:-}"
        existing_prompt_command="${existing_prompt_command//$__bp_install_string[;$'\n']}"
        existing_prompt_command="${existing_prompt_command//$__bp_install_string}"
        __bp_sanitize_string existing_prompt_command "$existing_prompt_command"
        PROMPT_COMMAND=$'__bp_precmd_invoke_cmd\n'
        if [[ -n "$existing_prompt_command" ]]; then
            PROMPT_COMMAND+=${existing_prompt_command}$'\n'
        fi
        PROMPT_COMMAND+='__bp_interactive_mode'
    fi
    precmd_functions+=(precmd)
    preexec_functions+=(preexec)
    __bp_precmd_invoke_cmd force
    __bp_interactive_mode force
}
declare -ft __bp_install
__bp_install_after_session_init() {
    __bp_require_not_readonly PROMPT_COMMAND HISTCONTROL HISTTIMEFORMAT || return
    if __bp_use_array_prompt_command; then
        PROMPT_COMMAND+=("${__bp_install_string}")
    else
        local sanitized_prompt_command
        __bp_sanitize_string sanitized_prompt_command "${PROMPT_COMMAND:-}"
        if [[ -n "$sanitized_prompt_command" ]]; then
            PROMPT_COMMAND=${sanitized_prompt_command}$'\n'
        fi
        PROMPT_COMMAND+=${__bp_install_string}
    fi
}
__bp_install_after_session_init
`

// bashPreexecs are the versions of bash-preexec the tests load.
var bashPreexecs = []string{"0.5.0", "0.6.0", "master"}

// bashPreexecFile is a file to source that loads bash-preexec as version
// v, the real one if KAKEL_TEST_BASH_PREEXEC has it and the stand-in
// otherwise.
func bashPreexecFile(t *testing.T, v string) string {
	t.Helper()
	if dir := os.Getenv("KAKEL_TEST_BASH_PREEXEC"); dir != "" {
		real := filepath.Join(dir, v, "bash-preexec.sh")
		if _, err := os.Stat(real); err != nil {
			t.Fatalf("KAKEL_TEST_BASH_PREEXEC has no %s: %v", v, err)
		}
		return real
	}
	return writeFile(t, "bash-preexec.sh", "__bp_sv="+v+"\n"+bashPreexec)
}

// writeFile writes text to a file of that name in a folder of the
// test's, and gives back its path.
func writeFile(t *testing.T, name, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// since is what the shell wrote from offset from on.
func (sh *shell) since(from int) string { return sh.written()[from:] }

// cMarks counts the C marks in s.
func cMarks(s string) int { return strings.Count(s, "\x1b]133;C\a") }

// countOf counts the marks in got that are m.
func countOf(got []string, m string) int {
	n := 0
	for _, g := range got {
		if g == m {
			n++
		}
	}
	return n
}

// With bash-preexec loaded, its preexec hook runs once for each command
// the user runs, and never at a prompt left empty, by Ctrl+C or Enter,
// or for a line that runs only a subshell; its precmd hook sees each
// command's status; and Shell Setup's marks are as they are without it.
func TestBashWithBashPreexec(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash is not the Windows shell")
	}
	for _, v := range bashPreexecs {
		for kind, b := range bashes(t) {
			t.Run(v+"/"+kind, func(t *testing.T) {
				rc := writeFile(t, "bashrc", "source "+bashPreexecFile(t, v)+"\n"+
					`preexec() { printf '<pre:%s>' "$1"; }`+"\n"+
					`precmd() { printf '<post:%s>' "$?"; }`+"\n")
				sh := startShell(t, plainBash(t), b.env)
				sh.setUp("source " + rc)
				sh.run("echo hi")
				sh.drawn()
				from := len(sh.written())
				sh.run("")
				sh.ctrlC()
				sh.run("(true)")
				between := sh.since(from)
				sh.run("false")
				sh.run("echo bye")
				sh.exit()
				out := sh.written()

				if n := strings.Count(out, "<pre:echo hi>"); n != 1 {
					t.Fatalf("preexec ran %d times for echo hi, want once: %q", n, out)
				}
				if strings.Contains(strings.ReplaceAll(between, "<pre:(true)>", ""), "<pre:") {
					t.Fatalf("preexec ran at an empty prompt: %q", between)
				}
				if !strings.Contains(out, "<pre:echo bye>") || !strings.Contains(out, "<post:1>") {
					t.Fatalf("the hooks did not run for the commands after: %q", out)
				}
				noCAtAPrompt(t, out)
				after := afterSetup(sh.marks())
				// echo hi, (true) where PS0 marks it, false, echo bye and exit.
				want := 4
				if b.version >= 404 {
					want = 5
				}
				if n := countOf(after, "C"); n != want {
					t.Fatalf("marks %q: %d C after setup, want %d", after, n, want)
				}
				if !slices.Contains(after, "D;1") {
					t.Fatalf("marks %q: no D;1 for false", after)
				}
			})
		}
	}
}

// A PROMPT_COMMAND that is read only, as a server that logs each command
// sets it, cannot be hooked, and then nothing marks a command either: a
// command marked with no prompt after it would be taken to run for good.
// With bash-preexec loaded, which refuses to install, the same.
func TestBashWithAReadOnlyPromptCommandMarksNothing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash is not the Windows shell")
	}
	for kind, b := range bashes(t) {
		for name, before := range map[string]string{
			"alone":             "readonly PROMPT_COMMAND='history -a'",
			"with bash-preexec": "readonly PROMPT_COMMAND='history -a'; source " + bashPreexecFile(t, "master") + " 2>/dev/null",
		} {
			t.Run(kind+"/"+name, func(t *testing.T) {
				sh := startShell(t, plainBash(t), b.env)
				sh.write(before + "\r")
				sh.write(string(Typed(Posix)))
				for _, c := range []string{"true", "false", "echo DONE"} {
					sh.write(c + "\r")
				}
				sh.waitFor("the commands", func() bool { return strings.Contains(sh.written(), "DONE\r\n") })
				sh.exit()
				if got := sh.marks(); len(got) > 0 {
					t.Fatalf("marks %q, want none", got)
				}
			})
		}
	}
}

// Tools' prompt hooks as their bash init texts set them, each with a
// function in place of its program that says it ran.
const (
	// starship init bash, without bash-preexec: its DEBUG trap, which
	// runs a trap that was there before from its own, and its
	// PROMPT_COMMAND, which keeps the one that was there to run itself.
	starshipInit = `
starship() { case $1 in prompt) printf '<sp:%s>' "$2" >&2; printf '$ ';; esac; }
starship_preexec() {
    local PREV_LAST_ARG=$1
    if [[ "${STARSHIP_PREEXEC_READY:-}" == "true" ]]; then
        STARSHIP_PREEXEC_READY=false
        STARSHIP_START_TIME=1
    fi
    : "$PREV_LAST_ARG"
}
starship_precmd() {
    STARSHIP_CMD_STATUS=$? STARSHIP_PIPE_STATUS=(${PIPESTATUS[@]})
    local NUM_JOBS=0
    eval "$_PRESERVED_PROMPT_COMMAND"
    PS1="$(starship prompt "$STARSHIP_CMD_STATUS")"
    unset STARSHIP_START_TIME
    STARSHIP_PREEXEC_READY=true
}
if [[ "${__bp_imported:-}" == "defined" || -n "${preexec_functions-}" || -n "${precmd_functions-}" ]]; then
    precmd_functions+=(starship_precmd)
    preexec_functions+=(starship_preexec)
else
    dbg_trap="$(trap -p DEBUG)"; dbg_trap=${dbg_trap#*\'}; dbg_trap=${dbg_trap%\'*}
    if [[ -z "$dbg_trap" ]]; then
        trap 'starship_preexec "$_"' DEBUG
    elif [[ "$dbg_trap" != 'starship_preexec "$_"' && "$dbg_trap" != 'starship_preexec_all "$_"' ]]; then
        starship_preexec_all() {
            local PREV_LAST_ARG=$1 ; eval -- "$dbg_trap" ; starship_preexec; : "$PREV_LAST_ARG";
        }
        trap 'starship_preexec_all "$_"' DEBUG
    fi
    if [[ -z "${PROMPT_COMMAND-}" ]]; then
        PROMPT_COMMAND="starship_precmd"
    elif [[ "$PROMPT_COMMAND" != *"starship_precmd"* ]]; then
        _PRESERVED_PROMPT_COMMAND="$PROMPT_COMMAND"
        PROMPT_COMMAND="starship_precmd"
    fi
fi
`

	// direnv hook bash.
	direnvInit = `
direnv() { printf '<dv>' >&2; }
_direnv_hook() {
  local previous_exit_status=$?;
  trap -- '' SIGINT;
  eval "$(direnv export bash)";
  trap - SIGINT;
  return $previous_exit_status;
};
if [[ ";${PROMPT_COMMAND[*]:-};" != *";_direnv_hook;"* ]]; then
  if [[ "$(declare -p PROMPT_COMMAND 2>&1)" == "declare -a"* ]]; then
    PROMPT_COMMAND=(_direnv_hook "${PROMPT_COMMAND[@]}")
  else
    PROMPT_COMMAND="_direnv_hook${PROMPT_COMMAND:+;$PROMPT_COMMAND}"
  fi
fi
`

	// zoxide init bash, its default hook. Its program is a script, as
	// the hook calls it through command.
	zoxideInit = `
PATH=$KAKEL_TEST_BIN:$PATH
function __zoxide_hook() {
    \builtin local -r retval="$?"
    \command zoxide add -- "$PWD"
    return "${retval}"
}
if [[ ${PROMPT_COMMAND:=} != *'__zoxide_hook'* ]]; then
    PROMPT_COMMAND="__zoxide_hook;${PROMPT_COMMAND#;}"
fi
`
)

// A .bashrc read again, as with source ~/.bashrc, with tools that hook
// the prompt and check first that they have not already: each still runs
// once at each prompt, Shell Setup's marks once, and the status each
// sees is the command's.
func TestBashAfterABashrcWithToolsIsReadAgain(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash is not the Windows shell")
	}
	for kind, b := range bashes(t) {
		for name, tc := range map[string]struct {
			init, ran string
		}{
			"starship": {starshipInit, "<sp:"},
			"direnv":   {direnvInit, "<dv>"},
			"zoxide":   {zoxideInit, "<zx>"},
		} {
			if name == "direnv" && b.version < 404 {
				// Shell Setup's entry at the end of PROMPT_COMMAND follows
				// direnv's after a newline, where direnv looks for a
				// semicolon, and direnv adds itself again.
				continue
			}
			t.Run(kind+"/"+name, func(t *testing.T) {
				rc := writeFile(t, "bashrc", tc.init)
				bin := t.TempDir()
				if err := os.WriteFile(filepath.Join(bin, "zoxide"), []byte("#!/bin/sh\nprintf '<zx>' >&2\n"), 0o700); err != nil {
					t.Fatal(err)
				}
				sh := startShell(t, plainBash(t), append([]string{"KAKEL_TEST_BIN=" + bin}, b.env...))
				sh.setUp("source " + rc)
				sh.run("false")
				sh.drawn()
				from := len(sh.written())
				sh.run("source " + rc)
				sh.run("false")
				sh.run("")
				sh.run("true")
				sh.exit()
				out := sh.since(from)

				prompts := strings.Count(out, ownA)
				if prompts != 4 {
					t.Fatalf("%d prompts marked, want 4: %q", prompts, out)
				}
				if n := strings.Count(out, tc.ran); n != prompts {
					t.Fatalf("the tool ran %d times at %d prompts: %q", n, prompts, out)
				}
				if name == "starship" && !strings.Contains(out, "<sp:1>") {
					t.Fatalf("starship was not shown false's status: %q", out)
				}
				got := afterSetup(sh.marks())
				want := []string{"C", "D;1", "A", "C", "D;0", "A", "C", "D;1", "A", "D;1", "A", "C", "D;0", "A", "C"}
				// Before 4.4, starship read again sets its own debug trap in
				// place of Shell Setup's where bash 4 and later cannot show it
				// the trap from inside .bashrc, and commands are no longer
				// marked. Prompts still are.
				lost := []string{"C", "D;1", "A", "C", "D;0", "A", "D;1", "A", "D;1", "A", "D;0", "A"}
				if !slices.Equal(got, want) && !(name == "starship" && b.version < 404 && slices.Equal(got, lost)) {
					t.Fatalf("marks %q after setup, want %q", got, want)
				}
			})
		}
	}
}

// A .bashrc read again that sets PROMPT_COMMAND afresh takes Shell
// Setup's hook away. The command that read it is the last marked as
// one: from 4.4 the next ends it with a D mark, so it is not taken to run
// for good, and before 4.4 nothing more is marked.
func TestBashThatLostItsHookMarksNoMoreCommands(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash is not the Windows shell")
	}
	for kind, b := range bashes(t) {
		t.Run(kind, func(t *testing.T) {
			rc := writeFile(t, "bashrc", "PROMPT_COMMAND='history -a'\n")
			sh := startShell(t, plainBash(t), b.env)
			sh.setUp("source " + rc)
			sh.run("true")
			sh.drawn()
			from := len(sh.written())
			sh.write("source " + rc + "\r")
			for _, c := range []string{"true", "false", "", "echo DONE"} {
				sh.write(c + "\r")
			}
			sh.waitFor("the commands", func() bool { return strings.Contains(sh.since(from), "DONE\r\n") })
			out := sh.since(from)
			sh.exit()

			if n := cMarks(out); n != 1 {
				t.Fatalf("%d C marks once the hook was lost, want only source's: %q", n, out)
			}
			if strings.Contains(out, ownA) {
				t.Fatalf("a prompt was marked with no hook: %q", out)
			}
			got := sh.marks()
			last := got[len(got)-1]
			if b.version >= 404 && last != "D" {
				t.Fatalf("marks %q, want them to end with a D for source", got)
			}
		})
	}
}

// A PROMPT_COMMAND keeps its form: a string stays a string, which a child
// bash still gets when it is exported, and an array the user made stays
// an array. Either way, what the user's part starts with is still where
// a tool looks for it.
func TestBashKeepsTheFormOfPromptCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash is not the Windows shell")
	}
	for kind, b := range bashes(t) {
		forms := map[string]string{
			"a string": "PROMPT_COMMAND='history -a'; export PROMPT_COMMAND",
			"none":     "unset PROMPT_COMMAND",
		}
		if b.version >= 501 {
			forms["an array"] = "PROMPT_COMMAND=('history -a' 'true')"
		}
		for name, before := range forms {
			t.Run(kind+"/"+name, func(t *testing.T) {
				sh := startShell(t, plainBash(t), b.env)
				sh.setUp(before)
				sh.run(`declare -p PROMPT_COMMAND | cut -c1-10 | tr ' ' _`)
				sh.run(`case $PROMPT_COMMAND in *'history -a'*) echo FIRST-KEPT;; esac`)
				// A hook added after, the way many add one.
				sh.run(`PROMPT_COMMAND="$PROMPT_COMMAND;printf '<%s>' late"`)
				sh.run("false")
				sh.exit()
				out := sh.written()
				want := map[string]string{"a string": "declare_-x", "none": "declare_--", "an array": "declare_-a"}[name]
				if !strings.Contains(out, want+"\r\n") {
					t.Fatalf("PROMPT_COMMAND is not %s still, want %s: %q", name, want, out)
				}
				if name != "none" && !strings.Contains(out, "FIRST-KEPT\r\n") {
					t.Fatalf("PROMPT_COMMAND no longer holds the user's: %q", out)
				}
				if strings.Contains(out, "syntax error") || !strings.Contains(out, "<late>") {
					t.Fatalf("the hook added after does not run: %q", out)
				}
				if !slices.Contains(afterSetup(sh.marks()), "D;1") {
					t.Fatalf("marks %q, want a D;1 for false", sh.marks())
				}
			})
		}
	}
}
