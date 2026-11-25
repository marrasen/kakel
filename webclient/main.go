//go:build js && wasm

// Command webclient is kakel in a web browser: a terminal drawn as the
// desktop's window draws its own, in a page.
//
// For now it shows a page with a name on it. Built with the tag demo, as
// the tests build it and a release never is, it shows one terminal with
// a made-up program in it when the page's address says ?demo=1.
//
// internal/clientbuild makes it and the page it starts in, index.html
// and what that loads, into the files a relay serves.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"

	"github.com/marrasen/kakel/internal/build"
	"github.com/marrasen/kakel/look"
	"github.com/marrasen/kakel/themes"
)

// show is what the window shows, until it ends.
var show = showName

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
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
		return show(ctx, w, th)
	})
}
