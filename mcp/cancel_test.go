package mcp

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

// blockingPanes is fakePanes whose waits each block until the test lets them all go.
type blockingPanes struct {
	*fakePanes
	started chan struct{}
	letGo   chan struct{}
}

func (b *blockingPanes) Wait(id string, lines int, until Until) (Screen, Ending, error) {
	b.started <- struct{}{}
	<-b.letGo
	return b.fakePanes.Wait(id, lines, until)
}

// conversation is a server on pipes, for tests that send before the last answer has come.
type conversation struct {
	t    *testing.T
	to   *io.PipeWriter
	from *bufio.Reader
	done chan error
}

func converse(t *testing.T, panes Panes) *conversation {
	t.Helper()
	return converseWith(t, panes, nil)
}

// converseWith is converse, with onCancelled run once the server has cancelled its calls.
func converseWith(t *testing.T, panes Panes, onCancelled func()) *conversation {
	t.Helper()
	toServer, fromTest := io.Pipe()
	fromServer, toTest := io.Pipe()
	c := &conversation{t: t, to: fromTest, from: bufio.NewReaderSize(fromServer, 4096), done: make(chan error, 1)}
	go func() { c.done <- serve(t.Context(), toServer, toTest, panes, onCancelled) }()
	t.Cleanup(func() { _ = fromTest.Close(); _ = fromServer.Close() })
	return c
}

func (c *conversation) say(m string) {
	c.t.Helper()
	if _, err := io.WriteString(c.to, m+"\n"); err != nil {
		c.t.Fatalf("say: %v", err)
	}
}

func (c *conversation) answer() response {
	c.t.Helper()
	line, _, err := c.from.ReadLine()
	if err != nil {
		c.t.Fatalf("read: %v", err)
	}
	var r response
	if err := json.Unmarshal(line, &r); err != nil {
		c.t.Fatalf("it said something unreadable: %q", line)
	}
	return r
}

const useCode = `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":` +
	`{"name":"use_session_code","arguments":{"code":"gt1-2222-abc"}}}`

func TestACancelledListTypesNothingMore(t *testing.T) {
	panes := &blockingPanes{fakePanes: &fakePanes{code: "gt1-2222-abc", screen: "$ "},
		started: make(chan struct{}, 1), letGo: make(chan struct{})}
	c := converse(t, panes)
	c.say(useCode)
	c.answer()

	c.say(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"send_keys",` +
		`"arguments":{"pane":"1.1","steps":["until","type:make deploy","key:Enter"]}}}`)
	<-panes.started
	c.say(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":2}}`)
	// A notification is handled before the next message is read, so the ping's answer means the cancel landed.
	c.say(`{"jsonrpc":"2.0","id":3,"method":"ping"}`)
	if got := c.answer(); string(got.ID) != "3" {
		t.Fatalf("the first answer after the cancel is for %s", got.ID)
	}
	close(panes.letGo)

	// The cancelled call is not answered, so the next answer is the second ping's.
	time.Sleep(50 * time.Millisecond)
	c.say(`{"jsonrpc":"2.0","id":4,"method":"ping"}`)
	if got := c.answer(); string(got.ID) != "4" {
		t.Fatalf("the cancelled call was answered: got %s", got.ID)
	}
	panes.mu.Lock()
	typed := panes.typed
	panes.mu.Unlock()
	if strings.Contains(typed, "make deploy") {
		t.Fatalf("a cancelled list typed %q", typed)
	}
}

func TestACancelSentRightAfterTheCallStopsIt(t *testing.T) {
	panes := &fakePanes{code: "gt1-2222-abc", screen: "$ "}
	c := converse(t, panes)
	c.say(useCode)
	c.answer()
	c.say(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"send_keys",` +
		`"arguments":{"pane":"1.1","steps":["wait:1500","type:make deploy"]}}}`)
	c.say(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":2}}`)
	c.say(`{"jsonrpc":"2.0","id":3,"method":"ping"}`)
	if got := c.answer(); string(got.ID) != "3" {
		t.Fatalf("the first answer after the cancel is for %s", got.ID)
	}
	// Past the list's own wait, which is when it would have typed.
	time.Sleep(2 * time.Second)
	panes.mu.Lock()
	typed := panes.typed
	panes.mu.Unlock()
	if typed != "" {
		t.Fatalf("a list cancelled as it was sent typed %q", typed)
	}
}

func TestTheAgentGoingStopsAListPartWay(t *testing.T) {
	panes := &blockingPanes{fakePanes: &fakePanes{code: "gt1-2222-abc", screen: "$ "},
		started: make(chan struct{}, 1), letGo: make(chan struct{})}
	// The wait comes back only once the server has cancelled the call.
	c := converseWith(t, panes, func() { close(panes.letGo) })
	c.say(useCode)
	c.answer()
	c.say(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"send_keys",` +
		`"arguments":{"pane":"1.1","steps":["until","type:make deploy"]}}}`)
	<-panes.started
	if err := c.to.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := <-c.done; err != nil {
		t.Fatalf("serve: %v", err)
	}
	if strings.Contains(panes.typed, "make deploy") {
		t.Fatalf("a list kept typing after the agent went: %q", panes.typed)
	}
}

func TestACallPastTheLimitIsTurnedAwayAndPingsStillAnswer(t *testing.T) {
	panes := &blockingPanes{fakePanes: &fakePanes{code: "gt1-2222-abc", screen: "$ "},
		started: make(chan struct{}, answersAtOnce), letGo: make(chan struct{})}
	c := converse(t, panes)
	c.say(useCode)
	c.answer()
	for i := range answersAtOnce {
		c.say(`{"jsonrpc":"2.0","id":` + itoa(10+i) + `,"method":"tools/call","params":` +
			`{"name":"wait_for","arguments":{"pane":"1.1"}}}`)
	}
	for range answersAtOnce {
		<-panes.started
	}

	c.say(`{"jsonrpc":"2.0","id":99,"method":"tools/call","params":{"name":"wait_for","arguments":{"pane":"1.1"}}}`)
	got := c.answer()
	if string(got.ID) != "99" {
		t.Fatalf("the first answer is for %s", got.ID)
	}
	text, failed := textOf(t, got)
	if !failed || !strings.Contains(text, "still running") {
		t.Fatalf("a call past the limit was answered %v: %s", failed, text)
	}
	c.say(`{"jsonrpc":"2.0","id":100,"method":"ping"}`)
	if got := c.answer(); string(got.ID) != "100" {
		t.Fatalf("the ping was not answered while the calls ran: got %s", got.ID)
	}
	close(panes.letGo)
	for range answersAtOnce {
		c.answer()
	}
}

func TestAToolCallSentAsANotificationIsNotRun(t *testing.T) {
	panes := &fakePanes{code: "gt1-2222-abc", screen: "$ "}
	talk(t, panes, useCode,
		`{"jsonrpc":"2.0","method":"tools/call","params":{"name":"send_keys",`+
			`"arguments":{"pane":"1.1","text":"rm -rf build"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"ping"}`)
	if panes.typed != "" {
		t.Fatalf("a tool call with no id typed %q", panes.typed)
	}
}

func TestAWaitIsHeldToOneCallsWorth(t *testing.T) {
	panes := &fakePanes{code: "gt1-2222-abc", screen: "$ "}
	talk(t, panes, useCode,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"wait_for",`+
			`"arguments":{"pane":"1.1","timeout_ms":600000}}}`)
	if got := panes.waited.TimeoutMS; got != longestCallMS {
		t.Fatalf("a wait asking for ten minutes was given %dms, want %d", got, longestCallMS)
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
