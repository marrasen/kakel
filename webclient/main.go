//go:build js && wasm

// Command webclient is kakel in a web browser: the page a guest opens a
// window shared with a link in. guestview draws it and works it, and this
// is what it needs of a browser: the address it was opened at, what the
// tab keeps, and the way to the host through the relay.
//
// Built with the tag demo, as the tests build it and a release never is,
// it shows one terminal with a made-up program in it when the page's
// address says ?demo=1, and tells the tests what it shows.
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

	"github.com/marrasen/kakel/guest"
	"github.com/marrasen/kakel/guestlook"
	"github.com/marrasen/kakel/guestview"
	"github.com/marrasen/kakel/internal/build"
	"github.com/marrasen/kakel/look"
	"github.com/marrasen/kakel/relay"
	"github.com/marrasen/kakel/themes"
)

// show is what the window shows, until it ends.
var show = showShare

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	// kakel's own look, light or dark as the browser's user has it, until
	// the host's is known.
	th, err := ownTheme(prefers("color-scheme", "light"))
	if err != nil {
		return fmt.Errorf("the theme: %w", err)
	}
	if still() {
		th = guestview.Still(th)
	}
	return gunim.Main(ctx, func(a *gunim.App) error {
		w, err := a.NewWindow(gunim.WindowOptions{
			Title: build.Name, Size: geom.Sz(900, 600), Theme: guestview.Own(th.Theme, coarse()), SystemFrame: true,
		})
		if err != nil {
			return fmt.Errorf("the window: %w", err)
		}
		return show(ctx, w, th)
	})
}

// ownTheme is the theme a page is drawn in with no host's: the one a
// window opens on first, or the first light one.
func ownTheme(light bool) (look.Themed, error) {
	all := themes.Built()
	for _, t := range all {
		made, err := look.Of(t)
		if err != nil {
			return look.Themed{}, err
		}
		if !light || !(guestlook.Theme{Look: made}).Dark() {
			return made, nil
		}
	}
	return look.Of(all[0])
}

// showShare shows the window the page's address shares, until the page
// closes.
func showShare(ctx context.Context, w *gunim.Window, th look.Themed) error {
	link, secret, err := opened()
	k := &kept{id: link.ID, secret: secret}
	if err != nil {
		// A reload of a page that took the secret off its address.
		secret, err = k.loadSecret()
	}
	yield := yielding()
	// Nothing said of a link has any of it: what is logged is the share's
	// short name, and the relay.
	if err != nil {
		log.Printf("the page's address is no whole link")
	}
	opens(w)
	return guestview.Run(ctx, w, guestview.Config{
		Broken: err != nil,
		Ended:  err != nil && k.ended(),
		Dial: func(ctx context.Context, key guest.Key) (guestview.Session, error) {
			conn, err := relay.JoinShare(ctx, link, relay.Dialer{})
			if err != nil {
				return nil, err
			}
			s, err := guest.Dial(ctx, conn, guest.Options{Secret: secret, Key: key, Yield: yield})
			if err != nil {
				return nil, err
			}
			return guestview.Dialed(s), nil
		},
		Keep:  k,
		Name:  load("localStorage", nameKey),
		About: about(),
		SaveName: func(name string) {
			// Its own convenience, with nothing lost where it is not kept.
			_ = store("localStorage", nameKey, name)
		},
		Wake:    waking(ctx),
		Theme:   th,
		Still:   still(),
		CanFill: canFill(),
		Mac:     mac(),
		Touch:   coarse(),
		Drawn:   drawn,
		OnState: onState,
		OnShown: told,
		Shells:  shells,
	})
}
