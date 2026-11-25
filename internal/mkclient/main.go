// Command mkclient builds kakel's web client into the bundle a host
// gives a relay, where package webbundle embeds it from. A kakel built
// with the tag webclient has it in it.
//
//	go run ./internal/mkclient
//
// writes into webbundle/client the bundle, client.zip, and its manifest,
// client.manifest, which is what is signed for a release: gunimsign signs
// it as it does any file. mkclient never has the private key. It is a
// large program, and whatever it is built of would have the key too.
//
//	go run ./internal/mkclient -signed
//
// says how the bundle there is signed, and
//
//	go run ./internal/mkclient -check dist/kakel_*
//
// checks the web client that is in the programs of a release. See
// RELEASING.md.
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/marrasen/kakel/internal/build"
	"github.com/marrasen/kakel/internal/clientbuild"
	"github.com/marrasen/kakel/relay"
	"github.com/marrasen/kakel/webbundle/names"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "mkclient:", err)
		os.Exit(1)
	}
}

func run() error {
	dir := flag.String("o", "", "the folder to write to; "+names.Dir+" in kakel's source when empty")
	signed := flag.Bool("signed", false, "say how the bundle in the folder is signed")
	check := flag.Bool("check", false, "check the web client in the programs, or the archives of them, named after it")
	flag.Parse()
	key, err := base64.StdEncoding.DecodeString(build.ReleaseKey)
	if err != nil {
		return err
	}
	if *check {
		return checkPrograms(flag.Args(), key)
	}
	if flag.NArg() > 0 {
		return fmt.Errorf("%q is nothing it takes", flag.Arg(0))
	}
	if *dir == "" {
		root, err := clientbuild.SourceRoot()
		if err != nil {
			return err
		}
		*dir = filepath.Join(root, filepath.FromSlash(names.Dir))
	}
	if *signed {
		return saySigned(*dir, key)
	}
	files, err := clientbuild.Build()
	if err != nil {
		return err
	}
	b, err := bundleOf(files)
	if err != nil {
		return err
	}
	if err := b.write(*dir); err != nil {
		return err
	}
	fmt.Printf("%s: web client %s, %d bytes, not signed yet\n", filepath.Join(*dir, names.Zip), relay.BundleHash(b.zipped), len(b.zipped))
	return nil
}

// bundle is a bundle as it is made.
type bundle struct{ zipped, manifest []byte }

// bundleOf makes a bundle of a client's files.
func bundleOf(files map[string][]byte) (*bundle, error) {
	packed, err := clientbuild.Packed(files)
	if err != nil {
		return nil, err
	}
	zipped, manifest, err := relay.MakeBundle(packed)
	if err != nil {
		return nil, err
	}
	return &bundle{zipped: zipped, manifest: manifest}, nil
}

// write writes the bundle into dir, in place of the one there.
func (b *bundle) write(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// The signature of the one before goes first, so a half done write
	// leaves none that is of another.
	if err := os.Remove(filepath.Join(dir, names.Sig)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, names.Zip), b.zipped, 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, names.Manifest), b.manifest, 0o644)
}

// saySigned says how the bundle in dir is signed, and warns loudly of any
// way but with key.
func saySigned(dir string, key ed25519.PublicKey) error {
	zipped, err := os.ReadFile(filepath.Join(dir, names.Zip))
	if err != nil {
		return err
	}
	sig, err := os.ReadFile(filepath.Join(dir, names.Sig))
	switch {
	case errors.Is(err, os.ErrNotExist):
		fmt.Fprintln(os.Stderr, "mkclient: WARNING: the web client is NOT SIGNED. Only a relay run with -allow-unsigned takes it")
		return nil
	case err != nil:
		return err
	}
	if _, err := relay.CheckBundle(zipped, string(sig), key); err != nil {
		fmt.Fprintln(os.Stderr, "mkclient: WARNING: the web client is signed with ANOTHER KEY than kakel's release key. Only a relay run with -allow-unsigned takes it")
		return nil
	}
	fmt.Println("the web client is signed with kakel's release key")
	return nil
}
