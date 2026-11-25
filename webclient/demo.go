//go:build js && wasm && demo

package main

import (
	"context"
	"net/url"
	"strings"
	"syscall/js"

	"github.com/marrasen/gunim"

	"github.com/marrasen/kakel/look"
	"github.com/marrasen/kakel/screen"
	"github.com/marrasen/kakel/webclient/demo"
)

// A page whose address asks for the demo shows it.
func init() {
	query, err := url.ParseQuery(strings.TrimPrefix(js.Global().Get("location").Get("search").String(), "?"))
	if err == nil && query.Get("demo") != "" {
		show = showDemo
	}
}

// showDemo shows a terminal with a made-up program in it, until the
// window closes.
func showDemo(ctx context.Context, w *gunim.Window, _ look.Themed) error {
	// Its colours are the same whatever the browser's user likes.
	th, err := ownTheme(false)
	if err != nil {
		return err
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
}
