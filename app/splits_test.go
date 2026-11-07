package app

import (
	"testing"

	"github.com/marrasen/kakel/screen"

	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
)

// A pane open on a stage of its own moves into a split beside another.
func TestAPaneMovesIntoASplit(t *testing.T) {
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	a := newApp(w.Client(), screen.NewShells())
	a.addPane(Pane{ID: "p1", Title: "one", Kind: KindFileManager}, nil, Placement{})
	a.addPane(Pane{ID: "p2", Title: "two", Kind: KindFileManager}, nil, Placement{})
	if !sameTabs(a, "p1", "p2") {
		t.Fatal("the panes began apart")
	}
	a.handle(MovePane{Pane: "p2", Beside: "p1"})
	b := boxOf(a.layout.Window(a.frontID()).Root)
	if b.A == nil || b.A.Pane != "p1" || b.B == nil || b.B.Pane != "p2" || b.Vertical {
		t.Fatalf("moved, the window shows %+v", b)
	}
	if a.st.Focus != "p2" {
		t.Fatalf("moved, the focus is on %s", a.st.Focus)
	}
}

// A split puts a chooser beside the pane at once. A pane picked in it
// moves into the chooser's place, and the chooser goes.
func TestAPanePickedInASplitsChooserTakesItsPlace(t *testing.T) {
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	a := newApp(w.Client(), screen.NewShells())
	a.addPane(Pane{ID: "f1", Title: "one", Kind: KindFileManager}, nil, Placement{})
	a.addPane(Pane{ID: "f2", Title: "two", Kind: KindFileManager}, nil, Placement{})
	a.focus("f1")
	a.handle(ChooseSplit{Vertical: true})
	chooser := a.st.Focus
	a.keepFocusShown()
	root := func() *Box { return boxOf(a.layout.Window(a.frontID()).Root) }
	if b := root(); a.kindOfPane(chooser) != KindChooser || b.B == nil || b.B.Pane != chooser || !b.Vertical {
		t.Fatalf("split, the window shows %+v and the focus on %s", b, chooser)
	}
	a.handle(MovePane{Pane: "f2", Instead: chooser})
	a.keepFocusShown()
	if b := root(); b.A.Pane != "f1" || b.B.Pane != "f2" || !b.Vertical {
		t.Fatalf("picked, the window shows %+v", b)
	}
	if a.has(chooser) || len(a.st.Panes) != 2 || a.st.Focus != "f2" {
		t.Fatalf("picked, the panes are %+v, the focus on %s", a.st.Panes, a.st.Focus)
	}
}

// A new shell picked in a split's chooser opens in its place, and a
// chooser closed gives its half back.
func TestANewShellPickedInASplitsChooserTakesItsPlace(t *testing.T) {
	a, _ := agentApp(t)
	first := a.st.Panes[0].ID
	a.handle(ChooseSplit{})
	chooser := a.st.Focus
	a.handle(SplitPane{Instead: chooser})
	waitFor(t, a, "the new shell", func() bool { return !a.has(chooser) })
	if b := boxOf(a.layout.Window(a.frontID()).Root); b.A == nil || b.A.Pane != first || b.B == nil || a.kindOfPane(b.B.Pane) != KindTerminal {
		t.Fatalf("picked, the window shows %+v", b)
	}
	a.handle(ChooseSplit{})
	chooser = a.st.Focus
	a.remove(chooser)
	if _, kept := a.choosers[chooser]; a.has(chooser) || kept {
		t.Fatalf("closed, the chooser stays: %v", a.choosers)
	}
}

// A second pick in a chooser already gone to the first lands beside the
// pane it was split from, in the same split, not on a stage of its own.
func TestASecondPickInAChooserLandsBesideItsPane(t *testing.T) {
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	a := newApp(w.Client(), screen.NewShells())
	a.addPane(Pane{ID: "f1", Title: "one", Kind: KindFileManager}, nil, Placement{})
	a.addPane(Pane{ID: "f2", Title: "two", Kind: KindFileManager}, nil, Placement{})
	a.addPane(Pane{ID: "f3", Title: "three", Kind: KindFileManager}, nil, Placement{})
	a.focus("f1")
	a.handle(ChooseSplit{})
	chooser := a.st.Focus
	a.handle(MovePane{Pane: "f2", Instead: chooser})
	a.addPane(Pane{ID: "f4", Title: "four", Kind: KindFileManager}, nil, Placement{Instead: chooser})
	if !inSplit(a, "f4", "f1") {
		t.Fatalf("the second pick stands with %v, not beside f1", tabsWith(a, "f4"))
	}
}

// Every window's layout is told to each, so a tab dragged in from
// another is known.
func TestEveryWindowsLayoutIsPublished(t *testing.T) {
	a, one, two := twoWindowApp(t)
	st := a.stateFor(one, a.st)
	st.Layout = a.layout.Clone()
	if _, ok := st.Layout.Where("p3"); !ok || a.ownerOf("p3") != two {
		t.Fatalf("the state's layout does not hold p3: %+v", st.Layout)
	}
}
