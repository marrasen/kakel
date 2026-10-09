package app

import (
	"testing"

	"github.com/marrasen/kakel/screen"

	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
)

// A scrollback's reader says so once its pane has closed.
func TestAScrollbackSaysWhenItsPaneHasClosed(t *testing.T) {
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	a := newApp(w.Client(), screen.NewShells())
	a.addPane(Pane{ID: "p1", Title: "Terminal 1"}, nil, Placement{})
	a.addPane(Pane{ID: "p2", Title: "Scrollback of Terminal 1", Kind: KindReader}, nil, Placement{})
	a.setReader("p2", Reader{Path: "Scrollback of Terminal 1", Lines: []string{"x"}, Seq: 1, Of: "p1"})
	a.remove("p1")
	if r := a.st.Readers["p2"]; r.Gone == "" {
		t.Fatalf("its pane closed, the scrollback reads %+v", r)
	}
}
