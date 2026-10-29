# Rules for working on kakel

## Animation first

kakel is built on gunim, and gunim is animation first. So is kakel.
Everything that changes on screen moves to its new state: a selection
glides to the row picked, a page slides or fades in as the one before
leaves, a row grows in and shrinks out, a colour fades. Nothing pops.
A change with no animation is a bug, whatever else it gets right.

## gunim's widgets first

Build UI from gunim's widgets (`github.com/marrasen/gunim/widget` and
its other packages). They bring the animation, the keyboard, the focus
ring and the screen reader with them.

When no widget does what is needed, add it to gunim, or extend the one
nearest, before writing a widget of kakel's own. A custom widget in
kakel is the last resort, for something only kakel will ever need, and
it is animated like gunim's.

A widget added to gunim follows gunim's `WIDGETS.md`, and goes through
the checklist at its end: settings as fields, callbacks named `On…` of
the one shape, state set through a setter taking the UI, every look a
token, springs from the theme, long content scrolled by gunim's own
scroller, and tests on every frame. Run `go test ./widget/...` in gunim
before pushing it, not only `./widget`.

Before calling UI work done, look at it moving, not only at a still:
screenshots on a private Xvfb (`view/shots_test.go`, `kakel -shot`),
never on the desktop's own display.
