package guestlook

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/marrasen/gunim/widget/dock"
)

// The most of a layout there is, at either end.
const (
	mostLayout = 256 << 10
	mostNodes  = 1024
	mostDepth  = 64
	mostID     = 128
)

// LayoutWindow is the window a layout read by ReadLayout stands in.
const LayoutWindow = 0

// Layout writes down how the panes of a window stand: the tree of one of
// a layout's windows, by the panes' IDs, and nothing else of the window,
// as where it is on the screen. The panes shown says nothing of are left
// out, with what that leaves empty. A nil root is a window with nothing
// in it.
func Layout(root *dock.Node, shown func(pane string) bool) (json.RawMessage, error) {
	l := dock.Layout{Grouped: true, Windows: []dock.Window{{ID: LayoutWindow, Keep: true, Root: root.Clone()}}}
	// Without what is not shown, and whole again.
	l.Mend(shown)
	if err := sound(l.Windows[0].Root, 0, new(int)); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(l.Windows[0].Root)
	if err != nil {
		return nil, fmt.Errorf("write the window's layout: %w", err)
	}
	if len(raw) > mostLayout {
		return nil, fmt.Errorf("the window's layout is %d bytes, over the %d it may be", len(raw), mostLayout)
	}
	return raw, nil
}

// ReadLayout reads what Layout wrote, for a dock of the window
// LayoutWindow. Whatever it is given, what it returns is whole: each pane
// stands once, and every group has a tab in front.
func ReadLayout(raw json.RawMessage) (dock.Layout, error) {
	empty := dock.Layout{Grouped: true, Windows: []dock.Window{{ID: LayoutWindow, Keep: true}}}
	if len(raw) == 0 {
		return empty, nil
	}
	if len(raw) > mostLayout {
		return empty, fmt.Errorf("the window's layout is %d bytes, over the %d it may be", len(raw), mostLayout)
	}
	var root *dock.Node
	if err := json.Unmarshal(raw, &root); err != nil {
		return empty, fmt.Errorf("read the window's layout: %w", err)
	}
	if err := sound(root, 0, new(int)); err != nil {
		return empty, err
	}
	l := empty
	l.Windows = []dock.Window{{ID: LayoutWindow, Keep: true, Root: root}}
	l.Mend(nil)
	return l, nil
}

// sound checks that a tree is none too large or deep, and puts right what
// Mend leaves as it is.
func sound(n *dock.Node, depth int, count *int) error {
	if n == nil {
		return nil
	}
	*count++
	switch {
	case *count > mostNodes:
		return fmt.Errorf("the window's layout has more than %d parts", mostNodes)
	case depth > mostDepth:
		return errors.New("the window's layout is too deep")
	case len(n.ID) > mostID || len(n.Front) > mostID:
		return errors.New("the window's layout has a name too long")
	}
	if !(n.Share > 0 && n.Share < 1) {
		n.Share = 0
	}
	if n.Kind != dock.Item && strings.Trim(strings.TrimPrefix(n.ID, "#"), "0123456789") != "" {
		// Not as a layout names its parts: it is named anew.
		n.ID = ""
	}
	for _, k := range n.Kids {
		if err := sound(k, depth+1, count); err != nil {
			return err
		}
	}
	return nil
}
