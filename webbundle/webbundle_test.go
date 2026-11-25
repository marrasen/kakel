//go:build !js

package webbundle_test

import (
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/marrasen/kakel/relay"
	"github.com/marrasen/kakel/webbundle"
)

// What is embedded is a sound bundle. A build without the tag webclient
// has none, and checks nothing here.
func TestTheEmbeddedBundleIsSound(t *testing.T) {
	zipped, sig, ok := webbundle.Bundle()
	if !ok {
		t.Skip("no web client is embedded in this build")
	}
	b, err := relay.OpenBundle(zipped)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("web client %s, %d bytes, with a signature of %d", b.Hash, len(zipped), len(sig))
}

// The web client never embeds one: it would be one in the other.
func TestABuildForABrowserEmbedsNothing(t *testing.T) {
	cmd := exec.Command("go", "list", "-tags", "webclient", "-f", "{{.GoFiles}} {{.EmbedPatterns}}", ".")
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", "CGO_ENABLED=0", "GOFLAGS=")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != "[none.go webbundle.go] []" {
		t.Errorf("it is built of %s", got)
	}
}

// What a release ships has the bundle in it: kakel itself is built of
// this package, on each of its systems, with the tag and without. With
// the tag, and no bundle built here, go list says what it can, with -e.
func TestKakelIsBuiltOfIt(t *testing.T) {
	const self = "github.com/marrasen/kakel/webbundle"
	for _, goos := range []string{"linux", "windows"} {
		for _, tags := range []string{"", "webclient"} {
			cmd := exec.Command("go", "list", "-e", "-deps", "-tags", tags, "github.com/marrasen/kakel")
			cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH=amd64", "CGO_ENABLED=0", "GOFLAGS=")
			out, err := cmd.Output()
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(strings.Fields(string(out)), self) {
				t.Errorf("kakel for %s with the tags %q has no web client to share a window with", goos, tags)
			}
		}
	}
}

// It reports whether there is one without reading it.
func TestBuiltSaysWhetherThereIsABundle(t *testing.T) {
	if _, _, ok := webbundle.Bundle(); ok != webbundle.Built() {
		t.Errorf("there is one: %v, and it says %v", ok, webbundle.Built())
	}
}
