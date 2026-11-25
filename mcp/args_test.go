package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestABadArgumentIsNamed(t *testing.T) {
	for _, tc := range []struct {
		args string
		says string
	}{
		{`{"pane":"1.1","lines":"50"}`, `"lines" has to be an integer`},
		{`{"pane":"1.1","steps":"until"}`, `"steps" has to be an array`},
		{`{"pane":"1.1","timeout":120000}`, `"timeout"`},
	} {
		answers := talk(t, &fakePanes{code: "gt1-2222-abc", screen: "$ "}, useCode,
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"send_keys","arguments":`+tc.args+`}}`)
		got := answers[1]
		if got.Error == nil || !strings.Contains(got.Error.Message, tc.says) {
			raw, _ := json.Marshal(got)
			t.Errorf("%s was answered %s, want it to say %s", tc.args, raw, tc.says)
		}
	}
}

func TestTheNewTextIsSmall(t *testing.T) {
	tools, err := json.Marshal(toolList())
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, tl := range toolList() {
		total += len(tl.Description)
		for _, f := range tl.InputSchema.Properties {
			total += len(f.Description)
		}
	}
	t.Logf("instructions %d chars, descriptions %d chars, tools/list %d chars", len(instructions), total, len(tools))
}
