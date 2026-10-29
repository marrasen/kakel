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

Before calling UI work done, look at it moving, not only at a still:
screenshots on a private Xvfb (`view/shots_test.go`, `kakel -shot`),
never on the desktop's own display.
