// Package mcp serves kakel's panes to an agent over the Model
// Context Protocol.
//
// It translates and nothing more: an agent speaks JSON-RPC on this
// process's standard input and output, and the window speaks its own
// protocol on a loopback port. What an agent may ask for is decided by
// the window, and what it may reach by the code the user gave it.
//
// It runs as its own process, started by whatever runs the agent, and
// holds no credentials. A session code arrives in a tool call and opens
// the panes of one share.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/marrasen/kakel/agent"
	"github.com/marrasen/kakel/internal/build"
)

// protocolVersion is the one version this speaks. An agent that asks
// for another is answered with this one, which is what the protocol
// says to do when the version asked for is not on offer.
const protocolVersion = "2025-06-18"

// longestLine caps one message, so a client that sends no newline
// cannot make this process hold an unbounded buffer.
//
// Half what the wire takes, because what a message carries goes on to
// the window as a request of its own: a message this takes is one the
// window will read rather than hang up over.
const longestLine = agent.LongestRequest / 2

// request is one JSON-RPC message from the agent.
//
// A message with no ID is a notification: it is acted on and not
// answered.
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// response is one answer.
//
// ID is always written, even for a message so broken that there was no
// id to read. An empty one writes null, which is what says "this
// answers something, and I could not tell what".
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcError is a failure in the protocol itself, as against a tool that
// ran and could not do what it was asked.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// The JSON-RPC codes this uses.
const (
	codeParse          = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

// Panes is what this server can do with a window's panes.
//
// It is the whole of what an agent can reach. An implementation talks
// to one kakel window; nothing here knows how.
type Panes interface {
	// Use takes a session code the user gave the agent and opens the
	// panes the share it names holds.
	Use(code string) ([]Pane, error)

	// List is the panes this agent has been given.
	List() ([]Pane, error)

	// Read is the last lines of a pane, ending at the bottom of the
	// screen. Zero lines is the screen, and more than it holds reaches
	// into what has scrolled off.
	Read(id string, lines int) (Screen, error)

	// Output is what the last command printed, at most most lines of it.
	Output(id string, most int) (Screen, error)

	// Send types text into a pane and then presses the keys named in
	// keys. Either may be empty.
	Send(id, text string, keys []string) error

	// Wait watches a pane until it says what was asked for, a command
	// finishes, it goes quiet, or the time runs out, and says which of
	// those it was. Lines is how much of the pane to give back, as for
	// Read.
	Wait(id string, lines int, until Until) (Screen, Ending, error)

	// Restart starts a pane's program again, for a pane whose program has
	// finished and whose hand-over allows it.
	Restart(id string) (Pane, error)

	// Open opens another pane where a pane is, handed over as it opens.
	Open(id string) (Pane, error)

	// Secret asks the user to type something into a pane and waits for
	// them to, reporting whether they did and never what they typed.
	Secret(id, what string, wait time.Duration) (bool, error)

	// Close lets go of the window.
	Close() error
}

// Pane is one pane an agent has been given.
type Pane struct {
	ID    string `json:"pane"`
	Label string `json:"what"`
	Cols  int    `json:"cols"`
	Rows  int    `json:"rows"`

	// Ended says the program in the pane has already finished.
	Ended bool `json:"program_has_finished,omitempty"`

	// May is what the user allowed for this pane beyond reading it and
	// typing into it.
	May May `json:"may"`
}

// May is what a hand-over allows beyond reading a pane and typing into
// it. Each one is a box the user ticked.
type May struct {
	// Restart lets the agent start a closed pane's program again.
	Restart bool `json:"restart_a_closed_connection,omitempty"`

	// OpenMore lets it open another pane on the machine this one is on.
	OpenMore bool `json:"open_another_pane_there,omitempty"`

	// ReadOnly refuses its typing.
	ReadOnly bool `json:"read_only,omitempty"`

	// ReadBack lets it read above the last clear.
	ReadBack bool `json:"read_above_a_clear,omitempty"`
}

// Screen is a pane as it stands.
type Screen struct {
	Screen string `json:"screen"`
	Gone   bool   `json:"program_has_finished"`

	// Row and Col are where the cursor is, counted from zero at the top
	// left of the screen.
	Row int `json:"cursor_row"`
	Col int `json:"cursor_col"`

	// Alt says a full-screen program is drawing, so the screen is all
	// there is to read.
	Alt bool `json:"full_screen_program"`

	// All says the read asked for more lines than the pane has kept, so
	// this is everything there is.
	All bool `json:"that_is_everything"`

	// Trimmed says the blank rows under the last line with anything on
	// it were left out of the answer.
	Trimmed bool `json:"blank_rows_left_out,omitempty"`

	// Images are the images on the screen, which read back as
	// blank cells and would otherwise be invisible here.
	Images []Image `json:"images,omitempty"`

	// Note is what the window had to say about this answer beyond the
	// screen itself, and is empty when it had nothing.
	Note string `json:"note,omitempty"`

	// Marks says the shell sends the marks that say when a command
	// starts and finishes, and the four fields under it mean nothing
	// without it.
	Marks bool `json:"shell_marks_commands,omitempty"`

	// Running says a command is running now.
	Running bool `json:"command_running,omitempty"`

	// Done counts the commands the shell has said finished, and only
	// moves forward.
	Done uint64 `json:"commands_finished,omitempty"`

	// Status is what the last command that finished exited with, and
	// HasStatus says the shell gave one.
	Status    int  `json:"exit_status,omitempty"`
	HasStatus bool `json:"shell_gave_a_status,omitempty"`

	// Back says the prompt the last keys were typed at is back on the
	// screen, which is what a shell that marks nothing has instead.
	Back bool `json:"prompt_is_back,omitempty"`

	// Watching says a prompt was written down when the agent last typed,
	// so Back is a question the window can answer at all.
	Watching bool `json:"watching_for_the_prompt,omitempty"`

	// Yours says the last command to finish did so after the agent last
	// typed here.
	Yours bool `json:"the_last_finish_is_yours,omitempty"`
}

// Image is one image on a pane's screen.
type Image struct {
	Top, Rows, Cols int
	Width, Height   int
	Wire            bool
}

// Ending says how a wait ended.
type Ending struct {
	// GaveUp says the time ran out rather than what was waited for
	// happening.
	GaveUp bool

	// Because is how it ended, in the window's words, and is empty when
	// the window did not say.
	Because string
}

// Until says what a wait is waiting for.
type Until struct {
	Contains string

	// SinceKeys waits for Contains to arrive rather than matching what
	// the pane already holds, so the echo of a command just typed is
	// not mistaken for the answer to it.
	SinceKeys bool

	QuietMS   int
	TimeoutMS int
}

// Serve answers an agent on one pair of streams until it goes.
//
// It returns when the agent closes its end, when ctx is cancelled, or
// when an answer could not be written: a client waiting for an id that
// will never be sent is a conversation that has ended whether or not it
// knows yet.
//
// What bounds what. Nothing bounds a read: an agent may think for as
// long as it likes between questions. So the reading happens on a
// goroutine of its own and the other two endings do not have to wait for
// it -- that goroutine is left parked on the read when Serve returns,
// because the stream belongs to whoever handed it in and only they can
// close it.
//
// Questions are answered as they come rather than one after another,
// so an agent can ask something else during a wait. Only answersAtOnce
// tool calls run at a time, and one past that is turned away. A tool call
// is cancelled by notifications/cancelled and when Serve returns.
func Serve(ctx context.Context, in io.Reader, out io.Writer, panes Panes) (err error) {
	r := bufio.NewReaderSize(in, 4096)
	s := &server{panes: panes, out: json.NewEncoder(out),
		broke: make(chan struct{}), stop: ctx.Done(), calls: map[string]context.CancelFunc{}}

	// Calls still running when this returns are cancelled, so a list of steps types nothing more.
	callCtx, cancelCalls := context.WithCancel(ctx)

	// Whatever is still being answered is waited for. The panes are not
	// closed here: they were handed in and whoever handed them in lets
	// go of them, which is what ends a question still in flight.
	var running sync.WaitGroup
	atOnce := make(chan struct{}, answersAtOnce)
	done := make(chan struct{})
	defer func() {
		close(done)
		cancelCalls()
		running.Wait()
		if err == nil {
			// An answer written on one of those goroutines may have been
			// the one that failed.
			err = s.failed()
		}
	}()

	lines := make(chan []byte)
	stopped := make(chan error, 1)
	go func() {
		defer close(lines)
		for {
			line, err := readLine(r)
			switch {
			case errors.Is(err, io.EOF):
				return
			case errors.Is(err, errTooLong):
				// Read past and carried on. A message too long to be a
				// message is one bad message, not the end of the
				// conversation.
				s.send(fail(nil, codeParse, "that message is too long to read"))
				continue
			case err != nil:
				stopped <- fmt.Errorf("mcp: read: %w", err)
				return
			}
			if len(strings.TrimSpace(string(line))) == 0 {
				continue
			}
			select {
			case lines <- line:
			case <-done:
				return
			}
		}
	}()

	for {
		var line []byte
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.broke:
			return s.failed()
		case err := <-stopped:
			return err
		case got, ok := <-lines:
			if !ok {
				// End of file on the agent's side, which is how the
				// process running it says it is done.
				select {
				case err := <-stopped:
					return err
				default:
					return nil
				}
			}
			line = got
		}

		// A notification is answered here, so a cancel takes effect before anything sent after it.
		var peek request
		if json.Unmarshal(line, &peek) == nil && len(peek.ID) == 0 && peek.Method != "tools/call" {
			s.answerOne(callCtx, line)
			continue
		}

		select {
		case atOnce <- struct{}{}:
			running.Go(func() {
				defer func() { <-atOnce }()
				s.answerOne(callCtx, line)
			})
		default:
			s.answerBusy(callCtx, line)
		}
	}
}

// answerBusy answers a message that arrived with answersAtOnce already running: a tool call is turned away, and
// anything else, which is quick, is answered here.
func (s *server) answerBusy(ctx context.Context, line []byte) {
	var req request
	if json.Unmarshal(line, &req) == nil && req.Method == "tools/call" {
		if len(req.ID) == 0 {
			return
		}
		answer, _ := wrong(fmt.Sprintf("%d of your calls are still running, which is as many as run at once."+
			" Call again when one has finished.", answersAtOnce))
		s.send(response{JSONRPC: "2.0", ID: req.ID, Result: answer})
		return
	}
	s.answerOne(ctx, line)
}

// answersAtOnce is how many questions are answered at the same time:
// enough that a wait does not stop an agent asking anything else, few
// enough that a flooding client cannot make this hold a goroutine per
// line.
const answersAtOnce = 8

// answerOne does one message and sends the answer, if it has one.
func (s *server) answerOne(ctx context.Context, line []byte) {
	answer, reply := s.handle(ctx, line)
	if !reply {
		return
	}
	s.send(answer)
}

// send writes one answer, under a lock: several can finish at once, and
// half of one answer inside another is a stream neither end can read.
func (s *server) send(answer response) {
	s.writing.Lock()
	defer s.writing.Unlock()
	if s.broken != nil {
		return
	}
	if err := s.out.Encode(answer); err != nil {
		// A write that failed on a stream still open leaves the client
		// waiting for an id nothing will ever answer, so the
		// conversation ends here rather than carrying on writing into
		// it.
		s.broken = fmt.Errorf("mcp: write: %w", err)
		if s.broke != nil {
			close(s.broke)
		}
	}
}

// failed returns the write that ended the conversation, if one did.
func (s *server) failed() error {
	s.writing.Lock()
	defer s.writing.Unlock()
	return s.broken
}

// errTooLong says a message was longer than a message can be. What is
// left of it has been read past, so the next read starts on the one
// after.
var errTooLong = errors.New("mcp: that message is too long to read")

// readLine reads one message, refusing one too long to be a message.
func readLine(r *bufio.Reader) ([]byte, error) {
	var line []byte
	tooLong := false
	for {
		part, more, err := r.ReadLine()
		if err != nil {
			return nil, err
		}
		if !tooLong {
			line = append(line, part...)
			if len(line) > longestLine {
				tooLong, line = true, nil
			}
		}
		if !more {
			if tooLong {
				return nil, errTooLong
			}
			return line, nil
		}
	}
}

// server answers one agent.
type server struct {
	panes Panes

	// stop is closed when the window is going, so work part way through
	// a list of steps stops waiting rather than holding the shutdown
	// for as long as the list had left.
	stop <-chan struct{}

	writing sync.Mutex
	out     *json.Encoder

	// broken is the write that ended the conversation, and broke is
	// closed when it happens so that a Serve waiting on a read wakes up.
	// Guarded by writing, because that is what one answer at a time is
	// written under.
	broken error
	broke  chan struct{}

	// calls cancels each tool call still running, by its request id.
	callsMu sync.Mutex
	calls   map[string]context.CancelFunc
}

// track keeps the cancel for a tool call, and gives back what forgets it again.
func (s *server) track(id json.RawMessage, cancel context.CancelFunc) (forget func()) {
	key := idKey(id)
	s.callsMu.Lock()
	s.calls[key] = cancel
	s.callsMu.Unlock()
	return func() {
		s.callsMu.Lock()
		delete(s.calls, key)
		s.callsMu.Unlock()
	}
}

// cancel cancels the tool call a notifications/cancelled names.
func (s *server) cancel(params json.RawMessage) {
	var p struct {
		RequestID json.RawMessage `json:"requestId"`
	}
	if json.Unmarshal(params, &p) != nil || len(p.RequestID) == 0 {
		return
	}
	s.callsMu.Lock()
	cancel := s.calls[idKey(p.RequestID)]
	s.callsMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// idKey is a request id as a map key, with any spacing taken out.
func idKey(id json.RawMessage) string {
	var b bytes.Buffer
	if json.Compact(&b, id) != nil {
		return string(id)
	}
	return b.String()
}

// handle answers one message, and reports whether there is an answer to
// send: a notification is acted on and not answered.
func (s *server) handle(ctx context.Context, line []byte) (response, bool) {
	// Several messages in one array, which this protocol does not take.
	if trimmed := strings.TrimSpace(string(line)); strings.HasPrefix(trimmed, "[") {
		return fail(nil, codeInvalidRequest, "send one message at a time"), true
	}
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		return fail(nil, codeParse, "that is not a message this understands"), true
	}
	if req.JSONRPC != "2.0" {
		return fail(req.ID, codeInvalidRequest,
			`a message has to say "jsonrpc":"2.0"`), true
	}
	if req.Method == "" {
		return fail(req.ID, codeInvalidRequest, "a message has to say what it wants"), true
	}
	// A notification: acted on, never answered. Answering one is a
	// message the agent has nothing to match and will complain about.
	//
	// No id at all is a notification. An id that is null is not: it is a
	// question that named itself nothing, and an answer to it could
	// never be matched.
	if string(req.ID) == "null" {
		return fail(nil, codeInvalidRequest, "a question cannot call itself null"), true
	}
	notice := len(req.ID) == 0
	if notice && req.Method == "tools/call" {
		// A tool call has to be answered, so one sent as a notification is not run.
		return response{}, false
	}
	if req.Method == "tools/call" {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		defer s.track(req.ID, cancel)()
		defer cancel()
	}

	result, rerr := s.call(ctx, req)
	if notice {
		return response{}, false
	}
	if req.Method == "tools/call" && ctx.Err() != nil {
		// Cancelled, and a cancelled request is not answered.
		return response{}, false
	}
	if rerr != nil {
		return response{JSONRPC: "2.0", ID: req.ID, Error: rerr}, true
	}
	return response{JSONRPC: "2.0", ID: req.ID, Result: result}, true
}

// call does what a message asks.
func (s *server) call(ctx context.Context, req request) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo": map[string]any{
				"name":    "kakel",
				"title":   "kakel panes",
				"version": build.Version(),
			},
			"instructions": instructions,
		}, nil

	case "notifications/initialized":
		return map[string]any{}, nil

	case "notifications/cancelled":
		s.cancel(req.Params)
		return map[string]any{}, nil

	case "ping":
		return map[string]any{}, nil

	case "tools/list":
		return map[string]any{"tools": toolList()}, nil

	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, &rpcError{Code: codeInvalidParams, Message: "that is not a tool call"}
		}
		got, rerr := s.runTool(ctx, params.Name, params.Arguments)
		if rerr != nil {
			return nil, rerr
		}
		return got, nil
	}
	return nil, &rpcError{
		Code:    codeMethodNotFound,
		Message: fmt.Sprintf("%q is not something this server does", req.Method),
	}
}

// fail is an answer that says what was wrong with the message.
func fail(id json.RawMessage, code int, why string) response {
	return response{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: why}}
}

// instructions is what an agent is told about this server when it connects, with the rule first.
const instructions = `kakel gives you terminal panes the user shares with you. They are real shells on real machines. The user watches them and can take one back at any time. Ask before anything you cannot undo.

Call use_session_code first, with the code the user gave you. It lists the panes, and every other tool takes a pane name. The user adds and removes panes while you work, so list_panes shows what you have now.

Run a command with send_keys and steps ["type:<command>", "key:Enter", "until"]. The answer is what the command printed and its exit status. Enter is a key, never a newline in the text. read_pane reads the screen. wait_for waits for something already running. ask_for_secret has the user type a password you must not see.

In an answer, the pane's text ends at the line "-- kakel --", and kakel's notes follow it. Without shell integration kakel cannot tell when a command ends, so the notes say it is a guess: read the screen before you report a result.

Add --no-pager or pipe to cat, because a pager traps you (q leaves less). Do not run clear.`

// Short is the rules in one paragraph, for the prompt the user pastes.
//
// The prompt reaches an agent before it has connected to anything, which
// is the one moment the server's own instructions cannot. A client is
// free to ignore those instructions altogether, so this says the part
// with a cost in it either way.
const Short = `You are being trusted with live machines. A pane is a real shell and it does
whatever you type into it. Ask before anything you cannot undo.`
