//go:build !js

package webbundle_test

import (
	"os"
	"os/exec"
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
