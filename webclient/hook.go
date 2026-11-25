//go:build js && wasm

package main

import (
	"context"
	"encoding/hex"
	"image"
	"image/color"
	"io"
	"strings"
	"syscall/js"
	"time"

	"github.com/marrasen/gunim"

	"github.com/marrasen/kakel/grid"
	"github.com/marrasen/kakel/relay"
	"github.com/marrasen/kakel/screen"
	"github.com/marrasen/kakel/webclient/demo"
)

// expose puts window.kakelDemo on the page, for the tests to ask what
// the terminal shows and what its program was sent. It is there only
// for a demo, which holds nothing of anybody's.
func expose(c gunim.Client, p *page, sess *demo.Session, sh *screen.Shell) {
	fns := map[string]any{
		// sent is everything the program was sent, in hex.
		"sent": js.FuncOf(func(js.Value, []js.Value) any { return hex.EncodeToString(sess.Sent()) }),
		// screen is what is on the screen, a line each.
		"screen": js.FuncOf(func(js.Value, []js.Value) any {
			s, ok := screenText(sh)
			if !ok {
				return nil
			}
			return s
		}),
		// size is the size the program has, and where its cells are.
		"size": js.FuncOf(func(js.Value, []js.Value) any {
			cols, rows := sess.Size()
			at, cell := p.cells()
			return map[string]any{"cols": cols, "rows": rows, "x": at.X, "y": at.Y, "w": cell.W, "h": cell.H}
		}),
		// burst has the program print so many bytes, and returns a promise
		// of how many milliseconds the terminal took to take them.
		"burst": js.FuncOf(func(_ js.Value, a []js.Value) any {
			taken, start := sess.Burst(a[0].Int()), time.Now()
			return promise(func() (any, error) { <-taken; return time.Since(start).Seconds() * 1000, nil })
		}),
		// reset starts the program over.
		"reset": js.FuncOf(func(js.Value, []js.Value) any { sess.Reset(); return nil }),
		// colours is a promise of the colours the window's next frame has
		// at places, in CSS pixels one after the other, each a number
		// 0xRRGGBB.
		"colours": js.FuncOf(func(_ js.Value, a []js.Value) any {
			at := make([]float64, a[0].Length())
			for i := range at {
				at[i] = a[0].Index(i).Float()
			}
			return promise(func() (any, error) { return colours(c, at) })
		}),
		// print has the program print text.
		"print": js.FuncOf(func(_ js.Value, a []js.Value) any { sess.Print(a[0].String()); return nil }),
		// fit gives the screen a size, as a host would, or none for 0.
		"fit": js.FuncOf(func(_ js.Value, a []js.Value) any {
			_ = c.Publish(pageTopic, pageState{Cols: a[0].Int(), Rows: a[1].Int()})
			return nil
		}),
		// echo joins a relay by a link, sends text through it and returns
		// a promise of as many bytes back.
		"echo": js.FuncOf(func(_ js.Value, a []js.Value) any {
			return promise(func() (any, error) { return echo(a[0].String(), a[1].String()) })
		}),
	}
	js.Global().Set("kakelDemo", js.ValueOf(fns))
}

// colours is what the window's next frame has at places in CSS pixels,
// an x and a y each: 0xRRGGBB, or -1 outside the window.
func colours(c gunim.Client, at []float64) (any, error) {
	img, err := c.Shot(context.Background())
	if err != nil {
		return nil, err
	}
	scale := js.Global().Get("devicePixelRatio").Float()
	var out []any
	for i := 0; i+1 < len(at); i += 2 {
		p := image.Pt(int(at[i]*scale), int(at[i+1]*scale))
		if !p.In(img.Bounds()) {
			out = append(out, -1)
			continue
		}
		c := img.RGBAAt(p.X, p.Y)
		out = append(out, int(c.R)<<16|int(c.G)<<8|int(c.B))
	}
	return out, nil
}

// screenText is what is on the screen, and false while it is written.
func screenText(sh *screen.Shell) (string, bool) {
	g := grid.New(1, 1, color.RGBA{}, color.RGBA{})
	if !sh.Snapshot(g) {
		return "", false
	}
	var b strings.Builder
	cols, rows := g.Size()
	for y := range rows {
		for x := range cols {
			if c := g.At(x, y); c.Width > 0 {
				b.WriteRune(max(c.Rune, ' '))
			}
		}
		b.WriteByte('\n')
	}
	return b.String(), true
}

// echo sends text to the other end of the relay link names, and returns
// as many bytes as it sent.
func echo(link, text string) (string, error) {
	l, err := relay.ParseLink(link)
	if err != nil {
		return "", err
	}
	conn, err := relay.Join(context.Background(), l, relay.Dialer{})
	if err != nil {
		return "", err
	}
	defer func() { _ = conn.Close() }()
	// As ssh does: written and read at once.
	wrote := make(chan error, 1)
	go func() { _, err := io.WriteString(conn, text); wrote <- err }()
	back := make([]byte, len(text))
	if _, err := io.ReadFull(conn, back); err != nil {
		return "", err
	}
	return string(back), <-wrote
}

// promise runs f on a goroutine of its own, as a JavaScript promise.
func promise(f func() (any, error)) js.Value {
	var start js.Func
	start = js.FuncOf(func(_ js.Value, a []js.Value) any {
		resolve, reject := a[0], a[1]
		go func() {
			defer start.Release()
			v, err := f()
			if err != nil {
				reject.Invoke(err.Error())
				return
			}
			resolve.Invoke(v)
		}()
		return nil
	})
	return js.Global().Get("Promise").New(start)
}
