package view

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every toast the window shows goes into the Window Log, where it stays
// once the toast has gone: through Window.toast, or, for the program's
// notices, as the program posted them. Nothing else calls Show.
func TestEveryToastGoesThroughTheLog(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, "toasts.Show(") {
				found = append(found, fmt.Sprintf("%s:%d", f, i+1))
			}
		}
	}
	if len(found) != 2 {
		t.Fatalf("toasts.Show is called %d times, want twice, in Window.toast and for the program's notices: %q", len(found), found)
	}
}
