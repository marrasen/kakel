package app

import (
	"testing"

	"github.com/marrasen/kakel/settings"
)

// A layout open in two windows, each with a pane through another kakel
// window, removed and then saved again just as it was from the first
// window, is back already when Undo is pressed: Undo says nothing, and
// puts back what the second window had written down.
func TestUndoAfterTheLayoutWasSavedAgainAsItWas(t *testing.T) {
	a, b, opened := windowLayoutApps(t)
	id := keep(t, b, alone("Desk", throughDesk(settings.SavedPane{Kind: settings.LayoutTerminal}))).ID
	w := openLanded(t, a, b, opened, id)
	// Saved from its window, the layout is as that window saves it.
	if _, _, _, err := b.saveLayout(SaveLayout{Window: w.id, Name: "Desk", Replace: true}); err != nil {
		t.Fatal(err)
	}
	b.st.Notices = nil
	openLanded(t, a, b, opened, id)
	if recs := b.recordsOf(id); len(recs) != 2 {
		t.Fatalf("written down %+v", recs)
	}
	b.handle(RemoveLayout{ID: id})
	undo := b.st.Notices[len(b.st.Notices)-1].On.(RestoreLayout)
	if _, _, _, err := b.saveLayout(SaveLayout{Window: w.id, Name: "Desk"}); err != nil {
		t.Fatal(err)
	}
	if ls := b.settings.Layouts(); len(ls) != 1 || ls[0].ID != id {
		t.Fatalf("saved again as %+v", ls)
	}
	b.st.Notices = nil
	b.handle(undo)
	if len(b.st.Notices) > 0 {
		t.Fatalf("Undo said %+v", b.st.Notices)
	}
	if recs := b.recordsOf(id); len(recs) != 2 {
		t.Fatalf("after Undo, written down %+v", recs)
	}
}
