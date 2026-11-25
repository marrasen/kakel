//go:build js && wasm

// Command webclient is kakel in a web browser: a terminal drawn as the
// desktop's window draws its own, in a page.
//
// For now it shows one terminal with a made-up program in it, when the
// page's address says ?demo=1.
package main

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"syscall/js"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"

	"github.com/marrasen/kakel/internal/build"
	"github.com/marrasen/kakel/look"
	"github.com/marrasen/kakel/screen"
	"github.com/marrasen/kakel/themes"
	"github.com/marrasen/kakel/webclient/demo"
)

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	query, err := url.ParseQuery(trimQuery(js.Global().Get("location").Get("search").String()))
	if err != nil {
		return fmt.Errorf("the page's address: %w", err)
	}
	if query.Get("demo") == "" {
		log.Print("nothing to show: the page's address names no session")
		return nil
	}
	// kakel's own look, the theme a window opens on first.
	th, err := look.Of(themes.Built()[0])
	if err != nil {
		return fmt.Errorf("the theme: %w", err)
	}
	return gunim.Main(ctx, func(a *gunim.App) error {
		w, err := a.NewWindow(gunim.WindowOptions{
			Title: build.Name, Size: geom.Sz(900, 600), Theme: th.Theme, SystemFrame: true,
		})
		if err != nil {
			return fmt.Errorf("the window: %w", err)
		}
		sess := demo.New(yielding())
		wrote := make(chan struct{}, 1)
		sh := screen.Open(sess, th.Palette, screen.Hooks{
			Output: func() {
				// One word is enough, however much was written.
				select {
				case wrote <- struct{}{}:
				default:
				}
			},
			Title: func(string) {}, Exit: func() {}, Clipboard: func(string) {},
		})
		c := w.Client()
		p := newPage(sh, th.Content)
		gunim.RegisterView(w, "page", func(pageState) *page { return p }, (*page).Update)
		gunim.RegisterPatch(w, "page", func(p *page, _ outputArrived, u *gunim.UI) { p.output.Arrived(u) })
		if err := c.Mount(gunim.Root, "page", "page", pageState{}, pageTopic); err != nil {
			return err
		}
		expose(c, p, sess, sh)
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-wrote:
				if err := c.Patch(pageTopic, outputArrived{}); err != nil {
					return err
				}
			case _, ok := <-c.Intents():
				if !ok {
					return nil
				}
			}
		}
	})
}

// yielding is a function that lets the browser have its thread, to draw
// and to hear of keys, and returns once it comes back. Go gives it back
// only when all its goroutines wait, and a sleep too short to wait for
// is none. A message to the page itself comes back as soon as it may,
// as a message from the network does.
func yielding() func() {
	ch := js.Global().Get("MessageChannel").New()
	back := make(chan struct{}, 1)
	ch.Get("port1").Set("onmessage", js.FuncOf(func(js.Value, []js.Value) any {
		back <- struct{}{}
		return nil
	}))
	return func() {
		ch.Get("port2").Call("postMessage", js.Null())
		<-back
	}
}

// trimQuery takes the question mark off a page's query.
func trimQuery(s string) string {
	if len(s) > 0 && s[0] == '?' {
		return s[1:]
	}
	return s
}
