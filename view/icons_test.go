package view

import (
	"testing"
	"time"

	"github.com/marrasen/kakel/app"

	"github.com/marrasen/gunim/access"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/icon"
	"github.com/marrasen/gunim/paint"
)

// A question names its icon as Lucide does, so the names in askIcons
// must be the icons' own.
func TestAskIconsAreKeyedByTheirLucideNames(t *testing.T) {
	for name, ic := range askIcons {
		if ic.Name != name {
			t.Errorf("askIcons[%q] is %q", name, ic.Name)
		}
	}
}

// Every command in a menu or the palette shows an icon.
func TestEveryCommandInTheMenusAndThePaletteHasAnIcon(t *testing.T) {
	for _, m := range menus {
		for _, it := range m.items {
			if it.sub != "" {
				if submenus[it.sub].icon == nil {
					t.Errorf("%s → %s opens a submenu with no icon", m.title, it.title)
				}
				continue
			}
			if !it.caption && commandIcons[it.id] == nil {
				t.Errorf("%s → %s (%s) has no icon", m.title, it.title, it.id)
			}
		}
	}
	for _, c := range commands {
		if commandIcons[c.id] == nil {
			t.Errorf("the palette's %s (%s) has no icon", c.title, c.id)
		}
	}
}

// A failure shows as an error toast, with its icon.
func TestAFailureShowsAnErrorToast(t *testing.T) {
	_, _, publish := windowStage(t)
	st := twoPanes("p2", nil)
	st.Notices = []app.Notice{{ID: 1, Title: "Couldn't open the files on web1", Body: "no route to host", Kind: app.NoticeFailed}}
	publish(st)
	for range 60 {
		lastWindow.Frame(time.Second / 60)
	}
	for _, op := range lastWindow.Offscreen().Ops() {
		if m, ok := op.(*paint.MaskOp); ok {
			if s, ok := m.Shape.(icon.Stroke); ok && s.Icon == icon.CircleAlert {
				return
			}
		}
	}
	t.Fatal("the failure's toast shows no error icon")
}

// A notice with buttons shows them on its toast, which stays past a
// toast's time until one is clicked, and each sends its intent. One
// with the key of one showing takes its place.
func TestANoticesButtonsSendTheirIntents(t *testing.T) {
	win, _, publish := windowStage(t)
	st := twoPanes("p2", nil)
	ready := func(id uint64, version string) app.Notice {
		return app.Notice{ID: id, Title: "kakel " + version + " is ready", Key: "update", Icon: "refresh-cw",
			// A link beside buttons is not shown: the buttons ask.
			Action: "Undo", On: app.ShowAbout{},
			Buttons: []app.NoticeButton{
				{Label: "Restart Now", On: app.RestartIntoUpdate{}},
				{Label: "What's New", On: app.ShowReadyUpdate{}},
			}}
	}
	// frames runs the window for d.
	frames := func(d time.Duration) {
		for range int(d / (time.Second / 60)) {
			lastWindow.Frame(time.Second / 60)
		}
	}
	// named is the node a screen reader calls name, or nil.
	named := func(name string) *access.Node {
		var at *access.Node
		var walk func(n *access.Node)
		walk = func(n *access.Node) {
			if n.Name == name {
				at = n
			}
			for _, k := range n.Children {
				walk(k)
			}
		}
		walk(lastWindow.Access().Root)
		return at
	}
	// button is where the toasts' button named name is.
	button := func(name string) geom.Point {
		t.Helper()
		at := named(name)
		if at == nil || at.Role != access.RoleButton {
			t.Fatalf("no button %q", name)
		}
		return at.Bounds.Center()
	}
	st.Notices = []app.Notice{ready(1, "v9.9.9")}
	publish(st)
	// Longer than a toast without buttons stays.
	frames(7 * time.Second)
	if n := win.toasts.Len(); n != 1 {
		t.Fatalf("%d toasts after a toast's time, want the one that asks", n)
	}
	if named("Undo") != nil {
		t.Fatal("the toast shows the link beside its buttons")
	}
	// The next release's notice takes its place.
	st.Notices = append(st.Notices, ready(2, "v9.9.10"))
	publish(st)
	frames(time.Second)
	if n := win.toasts.Len(); n != 1 {
		t.Fatalf("%d toasts with the next release's, want 1", n)
	}
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	clickAt(button("Restart Now"))
	if in, ok := nextIntent(t).(app.RestartIntoUpdate); !ok {
		t.Fatalf("Restart Now sent %#v", in)
	}
	frames(time.Second)
	if n := win.toasts.Len(); n != 0 {
		t.Fatalf("%d toasts after Restart Now, want none", n)
	}
	// Offered again, What's New sends its own.
	st.Notices = append(st.Notices, ready(3, "v9.9.10"))
	publish(st)
	frames(time.Second)
	clickAt(button("What's New"))
	if in, ok := nextIntent(t).(app.ShowReadyUpdate); !ok {
		t.Fatalf("What's New sent %#v", in)
	}
}
