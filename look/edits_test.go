package look

import (
	"encoding/json"
	"testing"

	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/themes"
)

// The theme editor's edits lie over the theme, and over what the panes
// wear, while Plain keeps the theme as it was. A value for a token gone
// is left out, and the rest still apply.
func TestEditsLieOverTheTheme(t *testing.T) {
	dark, _ := themes.Named(themes.Built(), "Dark")
	dark.Edits = json.RawMessage(`{"motion.caret": {"response": 0, "damping": 1}, "no.such.token": 3}`)
	got, err := Of(dark)
	if err != nil {
		t.Fatal(err)
	}
	jump := anim.Spring{Response: 0, Damping: 1}
	for name, th := range map[string]interface{ Value(string) (any, bool) }{"the theme": got.Theme, "the panes' theme": got.Content} {
		if v, ok := th.Value(widget.Caret.Key()); !ok || v != jump {
			t.Fatalf("in %s the cursor moves with %v", name, v)
		}
	}
	if _, ok := got.Plain.Value(widget.Caret.Key()); ok {
		t.Fatal("the theme without its edits has the edit")
	}
	if got.Theme.Name != "Dark" {
		t.Fatalf("edited, the theme is called %q", got.Theme.Name)
	}
}
