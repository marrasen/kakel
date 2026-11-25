package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/marrasen/kakel/agent"
	"github.com/marrasen/kakel/steps"
)

// tool is one thing an agent can ask for.
type tool struct {
	Name        string      `json:"name"`
	Title       string      `json:"title"`
	Description string      `json:"description"`
	InputSchema schema      `json:"inputSchema"`
	Annotations annotations `json:"annotations"`
}

// annotations tell a client what a tool does to the panes, so it can let reads through without asking.
type annotations struct {
	ReadOnly    bool  `json:"readOnlyHint"`
	Destructive *bool `json:"destructiveHint,omitempty"`
	Idempotent  bool  `json:"idempotentHint,omitempty"`
	OpenWorld   bool  `json:"openWorldHint"`
}

// reads is the annotation for a tool that only looks.
var reads = annotations{ReadOnly: true, Idempotent: true}

// types is the annotation for a tool that types into a real shell, which can do anything.
var types = annotations{OpenWorld: true}

// harmless is the annotation for a tool that changes a pane without being able to undo anything there.
var harmless = annotations{Destructive: new(false)}

// schema says what a tool's arguments look like.
type schema struct {
	Type       string           `json:"type"`
	Properties map[string]field `json:"properties"`
	Required   []string         `json:"required,omitempty"`
}

// field is one argument.
type field struct {
	Type        string `json:"type"`
	Description string `json:"description"`

	// Items says what an array argument holds, and is nil for the rest.
	Items *items `json:"items,omitempty"`
}

// items is what an array argument holds.
type items struct {
	Type string `json:"type"`
}

// result is what a tool call gives back: text, because what an agent is
// given is a screen.
type result struct {
	Content []content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

type content struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// toolList is what this server offers.
func toolList() []tool {
	pane := field{Type: "string", Description: "pane name"}
	lines := field{Type: "integer", Description: linesArg}
	return []tool{
		{
			Name:        "use_session_code",
			Title:       "Use a session code",
			Description: "Open the share a session code names. Call this first. The answer lists the panes, and every other tool takes a pane name.",
			InputSchema: schema{
				Type:       "object",
				Properties: map[string]field{"code": {Type: "string", Description: "the session code the user gave you"}},
				Required:   []string{"code"},
			},
			Annotations: reads,
		},
		{
			Name:        "list_panes",
			Title:       "List the panes you have",
			Description: "The panes in your share now: name, size and what each allows. The user adds and removes panes while you work.",
			InputSchema: schema{Type: "object", Properties: map[string]field{}},
			Annotations: reads,
		},
		{
			Name:        "read_pane",
			Title:       "Read a pane",
			Description: "The pane's screen now, as plain text. To get a command's output, use the answer from send_keys or wait_for instead.",
			InputSchema: schema{
				Type:       "object",
				Properties: map[string]field{"pane": pane, "lines": lines},
				Required:   []string{"pane"},
			},
			Annotations: reads,
		},
		{
			Name:  "send_keys",
			Title: "Type into a pane",
			Description: "Type, press keys and wait in a pane, in order, in one call." +
				` ["type:make test", "key:Enter", "until"] runs a command and returns what it printed and its exit status.` +
				" Several commands in one list each get their own output and exit status." +
				" A step that fails stops the list, and the answer names the step and shows the pane.",
			InputSchema: schema{
				Type: "object",
				Properties: map[string]field{
					"pane":       pane,
					"steps":      {Type: "array", Items: &items{Type: "string"}, Description: stepsArg},
					"lines":      lines,
					"timeout_ms": {Type: "integer", Description: fmt.Sprintf("ms each until may wait. Default and max: what is left of the list's %d.", longestCallMS)},
				},
				Required: []string{"pane", "steps"},
			},
			Annotations: types,
		},
		{
			Name:  "ask_for_secret",
			Title: "Ask the user to type a secret",
			Description: fmt.Sprintf("Ask the user to type a secret into the pane: a password, a passphrase, a one-time code."+
				" It goes to the program there, and you learn only that they typed."+
				" One call waits up to %d seconds. Call again with the same pane and what to keep waiting."+
				" A program that echoes input shows it on the screen.", int(agent.LongestCall.Seconds())),
			InputSchema: schema{
				Type: "object",
				Properties: map[string]field{
					"pane":    pane,
					"what":    {Type: "string", Description: `what to ask for, shown to the user, like "the sudo password for prod"`},
					"wait_ms": {Type: "integer", Description: fmt.Sprintf("ms this call waits. Default and max %d.", longestCallMS)},
				},
				Required: []string{"pane", "what"},
			},
			Annotations: harmless,
		},
		{
			Name:  "restart_pane",
			Title: "Start a pane's program again",
			Description: "Start the pane's program again after it has exited. The pane keeps its name and its earlier output." +
				" Needs the user's \"" + agent.BoxRestart + "\" box, which list_panes shows.",
			InputSchema: schema{
				Type:       "object",
				Properties: map[string]field{"pane": pane},
				Required:   []string{"pane"},
			},
			Annotations: harmless,
		},
		{
			Name:  "open_pane",
			Title: agent.BoxOpenMore,
			Description: "Open another shell on the machine a pane of yours is on. The answer names the new pane." +
				" kakel must already be connected to that machine." +
				" Needs the user's \"" + agent.BoxOpenMore + "\" box, which list_panes shows.",
			InputSchema: schema{
				Type:       "object",
				Properties: map[string]field{"pane": pane},
				Required:   []string{"pane"},
			},
			Annotations: harmless,
		},
		{
			Name:  "wait_for",
			Title: "Wait for a pane",
			Description: "Wait for a pane, then return what happened." +
				" With shell integration it ends when your command finishes and returns that command's output and exit status." +
				" Otherwise it ends when the prompt returns or the pane goes quiet, and returns the screen. The answer says which." +
				" For a program that keeps drawing (top, a progress bar, tail -f), give contains.",
			InputSchema: schema{
				Type: "object",
				Properties: map[string]field{
					"pane": pane,
					"contains": {Type: "string", Description: "end when the screen has this text. Text already there counts." +
						" Quiet no longer ends the wait."},
					"since_keys": {Type: "boolean", Description: "with contains: count only text that appears after this call starts." +
						" Use it when the text is a word you typed, because the pane echoes it."},
					"lines":      lines,
					"quiet_ms":   {Type: "integer", Description: "ms of silence that ends the wait. Default 700."},
					"timeout_ms": {Type: "integer", Description: fmt.Sprintf("ms before giving up. Default %d, max %d.", giveUpAfterMS, longestCallMS)},
					"screen":     {Type: "boolean", Description: "return the screen even when a command's output is available"},
				},
				Required: []string{"pane"},
			},
			Annotations: reads,
		},
	}
}

// runTool does one tool call.
//
// A tool that ran and could not do what it was asked says so in its
// answer. A call this server cannot make sense of at all -- a tool it
// does not have, arguments it cannot read, a call that does not say
// which pane -- is a failure of the message, which is what the protocol
// asks for.
func (s *server) runTool(ctx context.Context, name string, args json.RawMessage) (result, *rpcError) {
	var in struct {
		Code      string   `json:"code"`
		Pane      string   `json:"pane"`
		Text      string   `json:"text"`
		Keys      []string `json:"keys"`
		Steps     []string `json:"steps"`
		SinceKeys bool     `json:"since_keys"`
		Lines     int      `json:"lines"`
		Contains  string   `json:"contains"`
		QuietMS   int      `json:"quiet_ms"`
		TimeoutMS int      `json:"timeout_ms"`
		What      string   `json:"what"`
		WaitMS    int      `json:"wait_ms"`
		Screen    bool     `json:"screen"`
	}
	if len(args) > 0 {
		if why := readArgs(args, &in); why != "" {
			return result{}, &rpcError{Code: codeInvalidParams, Message: why}
		}
	}

	switch name {
	case "use_session_code":
		if in.Code == "" {
			return missing("code")
		}
		panes, err := s.panes.Use(in.Code)
		if err != nil {
			return wrong(failed(err))
		}
		return say(sharedWithYou(panes))

	case "list_panes":
		panes, err := s.panes.List()
		if err != nil && len(panes) == 0 {
			return wrong(failed(err))
		}
		if len(panes) == 0 {
			return say("You have no panes. If you have a session code, call use_session_code with it again;" +
				" if not, ask the user for one.")
		}
		var out strings.Builder
		for _, p := range panes {
			out.WriteString(paneLine(p) + "\n")
		}
		if err != nil {
			// The panes that did answer, and then what went wrong.
			out.WriteString("\nA window could not be asked, so this may not be all of them: " + failed(err))
		}
		return say(out.String())

	case "read_pane":
		if in.Pane == "" {
			return missing("pane")
		}
		lines, clamped := atMostLines(in.Lines)
		screen, err := s.panes.Read(in.Pane, lines)
		if err != nil {
			return wrong(failed(err))
		}
		return say(showScreen(screen, Ending{}, clamped))

	case "send_keys":
		if in.Pane == "" {
			return missing("pane")
		}
		if len(in.Steps) > 0 {
			if in.Text != "" || len(in.Keys) > 0 {
				return wrong("that call carries steps and text or keys as well." +
					" A list of steps says the whole of what to do, in order, so put" +
					" the text and the keys in it as type: and key: steps.")
			}
			list, err := steps.ParseAll(in.Steps)
			if err != nil {
				// Refused before anything is typed: a list that stopped
				// halfway is worse than one that never started.
				return wrong(failed(err))
			}
			lines, clamped := atMostLines(in.Lines)
			return s.runSteps(ctx, in.Pane, list, lines, clamped, in.TimeoutMS)
		}
		if in.Text == "" && len(in.Keys) == 0 {
			return missing("steps")
		}
		// Refused before anything is typed, so an agent that spelled a
		// key wrong is told which names there are and has sent nothing.
		if err := agent.CheckKeys(in.Keys); err != nil {
			return wrong(failed(err))
		}
		if why := escapedEnding(in.Text); why != "" {
			return wrong(why)
		}
		if err := s.panes.Send(in.Pane, in.Text, in.Keys); err != nil {
			return wrong(failed(err))
		}
		return say("Sent. Nothing waited, so call wait_for.")

	case "ask_for_secret":
		if in.Pane == "" {
			return missing("pane")
		}
		if strings.TrimSpace(in.What) == "" {
			return missing("what to ask the user for")
		}
		typed, err := s.panes.Secret(in.Pane, in.What,
			time.Duration(in.WaitMS)*time.Millisecond)
		if err != nil {
			return wrong(failed(err))
		}
		if !typed {
			return say(fmt.Sprintf("Not typed yet. The ask stays on the pane until %d minutes after you first asked."+
				" Call again with the same pane and what. After a few tries, ask the user yourself.",
				int(agent.LongestSecretWait.Minutes())))
		}
		return say("The user typed it. Call wait_for to see what the program does.")

	case "restart_pane":
		if in.Pane == "" {
			return missing("pane")
		}
		pane, err := s.panes.Restart(in.Pane)
		if err != nil {
			return wrong(failed(err))
		}
		return say(fmt.Sprintf("Restarted %s. Same pane name, earlier output still above.", pane.ID))

	case "open_pane":
		if in.Pane == "" {
			return missing("pane")
		}
		pane, err := s.panes.Open(in.Pane)
		if err != nil {
			return wrong(failed(err))
		}
		return say(fmt.Sprintf("Opened %s: a new shell on %s, %dx%d.", pane.ID, pane.Label, pane.Cols, pane.Rows))

	case "wait_for":
		if in.Pane == "" {
			return missing("pane")
		}
		lines, clamped := atMostLines(in.Lines)
		screen, ended, err := s.panes.Wait(in.Pane, lines, Until{
			Contains:  in.Contains,
			SinceKeys: in.SinceKeys,
			QuietMS:   in.QuietMS,
			TimeoutMS: min(in.TimeoutMS, longestCallMS),
		})
		if err != nil {
			return wrong(failed(err))
		}
		return say(s.afterWaiting(in.Pane, screen, ended, lines, clamped, in.Screen))
	}
	return result{}, &rpcError{
		Code:    codeInvalidParams,
		Message: fmt.Sprintf("%q is not a tool this server has", name),
	}
}

// sharedWithYou is what an agent is told when it uses a code: every pane in the share, then the rule, which a tool
// answer carries to every client.
func sharedWithYou(panes []Pane) string {
	if len(panes) == 0 {
		return "That share has no panes yet. Ask the user to add one, then call list_panes."
	}
	var out strings.Builder
	for _, p := range panes {
		out.WriteString(paneLine(p) + "\n")
	}
	return out.String() + "\nThe user is watching. Ask before anything you cannot undo."
}

// paneLine is one pane as an agent is told of it: its name, what it is, its size, and only what differs from a
// pane it may read and type into.
func paneLine(p Pane) string {
	var also []string
	switch {
	case p.Ended:
		also = append(also, "program has exited")
	case p.May.ReadOnly:
		also = append(also, "read only")
	}
	if p.May.Restart {
		also = append(also, "may restart_pane")
	}
	if p.May.OpenMore {
		also = append(also, "may open_pane")
	}
	if p.May.ReadBack {
		also = append(also, "may read above a clear")
	}
	line := fmt.Sprintf("%s: %s, %dx%d", p.ID, p.Label, p.Cols, p.Rows)
	if len(also) > 0 {
		line += " (" + strings.Join(also, ", ") + ")"
	}
	return line
}

// mostLines caps how many lines one read may ask for. The window's own
// limit, because a read renders a screenful at a time under the pane's
// lock and a long one keeps that window from drawing.
const mostLines = agent.MostLines

// atMostLines is how many lines to read, bounded by what one read gives,
// and whether that bound cut the number asked for. None asked for is the
// screen.
func atMostLines(n int) (lines int, clamped bool) {
	if n <= 0 {
		return 0, false
	}
	return min(n, mostLines), n > mostLines
}

// linesArg says what the lines argument does.
var linesArg = fmt.Sprintf("lines to return, counted up from the bottom, scrollback included."+
	" Default: the screen. Max %d.", mostLines)

// stepsArg says what a list of steps may hold.
var stepsArg = fmt.Sprintf(`Each step is "word:rest". The rest is literal, with no escapes.
type:<text> types it. Enter is a key, not a newline.
key:<name> presses one key: %s.
until waits for the running command to finish.
until:<text> waits for that text to appear after the last thing typed. If the command ends without it, the list stops.
wait:<ms> sleeps.
A whole list may take %[4]d seconds; then it stops, and wait_for waits on.
require:<text> stops the list unless the output since you typed has that text. fail:<text> stops it if it has.
Max %[2]d steps and %[3]d typed characters.`, agent.KeyNames(), steps.MostSteps, steps.MostText, int(agent.LongestCall.Seconds()))

// giveUpAfterMS is how long the window gives a wait that asked for no timeout, in milliseconds.
var giveUpAfterMS = agent.GiveUpAfter.Milliseconds()

// longestCallMS is agent.LongestCall in the milliseconds the tools take.
var longestCallMS = int(agent.LongestCall.Milliseconds())

// readArgs reads a tool call's arguments into in, and says which argument is wrong when one is.
func readArgs(args json.RawMessage, in any) string {
	d := json.NewDecoder(bytes.NewReader(args))
	d.DisallowUnknownFields()
	err := d.Decode(in)
	var typeErr *json.UnmarshalTypeError
	switch {
	case err == nil:
		return ""
	case errors.As(err, &typeErr) && typeErr.Field != "":
		return fmt.Sprintf("argument %q has to be %s, not %s", typeErr.Field, kindOf(typeErr.Type.Kind()), typeErr.Value)
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		return "no tool here takes the argument " + strings.TrimPrefix(err.Error(), "json: unknown field ")
	}
	return "the arguments are not a JSON object of this tool's arguments"
}

// kindOf names a Go kind the way a JSON schema does.
func kindOf(k reflect.Kind) string {
	switch k {
	case reflect.Int, reflect.Int64:
		return "an integer"
	case reflect.Bool:
		return "true or false"
	case reflect.String:
		return "a string"
	case reflect.Slice:
		return "an array"
	}
	return k.String()
}

// missing says a call left out something it had to carry.
func missing(what string) (result, *rpcError) {
	return result{}, &rpcError{
		Code:    codeInvalidParams,
		Message: "that call carried no " + what,
	}
}

// afterWaiting is what a wait answers with: what the command printed when the shell said it finished or the prompt
// came back, and the screen otherwise. A list of steps answers the same way.
func (s *server) afterWaiting(pane string, screen Screen, ended Ending,
	lines int, clamped, wantScreen bool) string {

	return showScreen(s.printedOrScreen(pane, screen, ended, lines, wantScreen), ended, clamped)
}

// printedOrScreen is what a wait's answer should carry: what the command
// printed where the shell said one finished, and the screen otherwise.
func (s *server) printedOrScreen(pane string, screen Screen, ended Ending,
	lines int, wantScreen bool) Screen {

	if wantScreen || (ended.Because != agent.EndedOnMarks && ended.Because != agent.EndedOnPrompt) {
		return screen
	}
	most := lines
	if most == 0 {
		most = mostLines
	}
	out, err := s.panes.Output(pane, most)
	if err == nil {
		return out
	}
	// A shell can say a command finished without ever having said where
	// its output began: the prompt coming back counts as finished too.
	// The screen is still the answer then, and the reason goes with it
	// rather than the call failing over a better answer that was not
	// available.
	screen.Note = withNote(screen.Note,
		"What that command printed could not be picked out: "+
			err.Error()+". This is the screen instead.")
	return screen
}

// showScreen is a screen as an agent reads it, with kakel's notes after notesMarker.
func showScreen(s Screen, ended Ending, clamped bool) string {
	var notes []string
	if ended.GaveUp {
		notes = append(notes, "timed out before that happened; call wait_for to keep waiting")
	} else if why := endedNote(ended.Because, s); why != "" {
		notes = append(notes, why)
	}
	if s.Gone {
		notes = append(notes, "the program has exited")
	}
	if note := commandNote(s); note != "" {
		notes = append(notes, note)
	}
	if clamped {
		notes = append(notes, fmt.Sprintf("capped at %d lines", mostLines))
	}
	if s.All && !s.Alt {
		notes = append(notes, "nothing above this")
	}
	notes = append(notes, imageNotes(s.Images)...)
	if s.Note != "" {
		notes = append(notes, s.Note)
	}
	if s.Alt {
		notes = append(notes, fmt.Sprintf("full-screen program: the screen is all there is. Cursor at row %d, column %d", s.Row, s.Col))
	}
	if len(notes) == 0 {
		return s.Screen
	}
	// Behind a marker, so the pane's own text stays apart from these.
	return s.Screen + "\n\n" + notesMarker + "\n" + strings.Join(notes, "\n")
}

// endedNote is how a wait ended, left out where the exit status already says it.
func endedNote(because string, s Screen) string {
	switch because {
	case "":
		return ""
	case agent.EndedOnMarks:
		if s.Marks && !s.Running && s.HasStatus {
			return ""
		}
		return "the command finished"
	case agent.EndedOnPrompt:
		return "the prompt came back"
	case agent.EndedOnQuiet:
		return "the pane went quiet, which does not mean a command finished"
	case agent.EndedOnStuck:
		return "the pane went quiet, but the shell says a command is still running"
	case agent.EndedOnText:
		return "the text is on the screen"
	case agent.EndedOnGone, agent.EndedOnTime:
		return ""
	}
	return because
}

// commandNote is what the shell said about the command line, or what was guessed when it says nothing.
func commandNote(s Screen) string {
	if s.Alt {
		return ""
	}
	if s.Gone {
		if s.Marks && s.Running {
			return "the shell was part way through a command"
		}
		return ""
	}
	if !s.Marks {
		return watchedNote(s)
	}
	if s.Running {
		return "a command is still running: call wait_for"
	}
	if !s.HasStatus {
		return "no command running, and no exit status for the last one"
	}
	if s.Yours {
		return fmt.Sprintf("exit %d", s.Status)
	}
	return fmt.Sprintf("exit %d, from a command that is not yours", s.Status)
}

// watchedNote is what an agent is told about a pane whose shell says nothing about its commands.
func watchedNote(s Screen) string {
	switch {
	case !s.Watching:
		return "no shell integration, and nothing typed here yet: judge from the screen"
	case s.Back:
		return "no shell integration: the prompt is back, so it probably finished"
	}
	return "no shell integration: the prompt is not back, so it is probably still running"
}

// notesMarker is the line between the pane's own text and what kakel
// has to say about it. The tools name it, so an agent knows where the
// screen ends.
const notesMarker = "-- kakel --"

// say is a tool answer.
func say(text string) (result, *rpcError) {
	return result{Content: []content{{Type: "text", Text: text}}}, nil
}

// wrong is a tool answer from a tool that ran and could not do what it
// was asked.
func wrong(why string) (result, *rpcError) {
	return result{Content: []content{{Type: "text", Text: why}}, IsError: true}, nil
}

// escapedEnding is why text that ends in an escape nobody meant is
// refused, and empty for text that is fine.
//
// A caller writing a command with a return at the end has to escape
// the backslash for JSON, and escaping it twice is easy: what arrives
// is the two characters rather than the control code, and they go
// into a live shell as a backslash and a letter on the end of the
// command. Nothing in the pane would say so; the command would simply
// run wrong.
//
// So it is refused rather than typed, which costs the caller one
// retry. Only at the end, because that is where a line ending was
// meant and anywhere else it is as likely to be a path on Windows.
func escapedEnding(text string) string {
	for _, end := range []string{`\r`, `\n`} {
		if !strings.HasSuffix(text, end) {
			continue
		}
		return "the text ends with " + end + ", which is a backslash and a letter" +
			" rather than a line ending: escaped twice on the way here. Nothing was" +
			" typed. Press Enter with keys [\"Enter\"] and leave it off the text." +
			" If those two characters really are what you want typed, put something" +
			" after them."
	}
	return ""
}

// imageNotes say where the screen holds images, whose cells read as blank.
func imageNotes(on []Image) []string {
	out := make([]string, 0, len(on))
	for _, p := range on {
		which := "OSC 1337"
		if p.Wire {
			which = "OSC 1338"
		}
		out = append(out, fmt.Sprintf("rows %d to %d: a %dx%d image (%s), blank here",
			p.Top, p.Top+p.Rows-1, p.Width, p.Height, which))
	}
	return out
}

// withNote adds a line to whatever the window already had to say.
func withNote(had, add string) string {
	if had == "" {
		return add
	}
	return had + "\n" + add
}

// failed is what an agent is told about an error, with how to get the panes back when the window stopped answering.
func failed(err error) string {
	if errors.Is(err, agent.ErrGone) {
		return err.Error() + ". Call use_session_code again with the same code."
	}
	return err.Error()
}
