package clientbuild

import (
	"bytes"
	"compress/gzip"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/marrasen/gunim/driver/web/saveworker"
)

// The page as it stands in the source is told how large the program is,
// and has nothing inline, which no relay's policy lets run.
func TestThePageIsToldTheSize(t *testing.T) {
	root, err := SourceRoot()
	if err != nil {
		t.Fatal(err)
	}
	files, err := page(root, 12345)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(files["index.html"], []byte(`content="12345"`)) {
		t.Error("the index does not say the size")
	}
	if !bytes.HasPrefix(files[iconName], []byte("\x89PNG")) {
		t.Error("the icon is no PNG")
	}
	// gunim's save worker, as the page names it.
	if !bytes.Equal(files[saveworker.Name], saveworker.Script) ||
		!bytes.Contains(files["index.html"], []byte(`<meta name="gunim-save-worker" content="`+saveworker.Name+`">`)) {
		t.Error("the page has no save worker")
	}
	for name, data := range files {
		if filepath.Ext(name) != ".png" && bytes.ContainsRune(data, '\r') {
			t.Errorf("%s has the line ends of this machine", name)
		}
	}
	for _, index := range []string{"", sizeUnknown + sizeUnknown} {
		if _, err := sized([]byte(index), 1); err == nil {
			t.Errorf("an index of %q was told the size", index)
		}
	}
}

// What is gzipped says nothing of when and where, and is what it was.
func TestThePackedProgramSaysNothingOfItsMaking(t *testing.T) {
	wasm := bytes.Repeat([]byte("kakel"), 1000)
	got, err := Packed(map[string][]byte{WasmName: wasm, "index.html": []byte("x")})
	if err != nil {
		t.Fatal(err)
	}
	if string(got["index.html"]) != "x" || got[WasmName] != nil {
		t.Errorf("packed as %v", slices.Sorted(maps.Keys(got)))
	}
	z, err := gzip.NewReader(bytes.NewReader(got[WasmName+".gz"]))
	if err != nil {
		t.Fatal(err)
	}
	back, err := io.ReadAll(z)
	if err != nil || !bytes.Equal(back, wasm) {
		t.Errorf("unpacked to %d bytes, %v", len(back), err)
	}
	if !z.ModTime.IsZero() || z.Name != "" || z.Comment != "" || len(z.Extra) > 0 || z.OS != 255 {
		t.Errorf("its header says %+v", z.Header)
	}
}

func TestFlagsABrowsersBuildHasNoSupportForAreLeftOut(t *testing.T) {
	if got := forWasm("-race -trimpath --covermode=atomic -mod=readonly"); got != "-trimpath -mod=readonly" {
		t.Errorf("left with %q", got)
	}
}

// The same source is the same client, however often it is built, and the
// one that is released has nothing of the demo.
func TestTheClientIsTheSameBytesEachTime(t *testing.T) {
	if testing.Short() {
		t.Skip("building the client takes a while")
	}
	// Flags for every build here are not for this one.
	a, err := Build()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOFLAGS", strings.TrimSpace(os.Getenv("GOFLAGS")+" -tags="+Demo+",webclient"))
	b, err := Build()
	if err != nil {
		t.Fatal(err)
	}
	if !maps.EqualFunc(a, b, bytes.Equal) {
		for name := range a {
			if !bytes.Equal(a[name], b[name]) {
				t.Errorf("%s is %d bytes and then %d others", name, len(a[name]), len(b[name]))
			}
		}
	}
	root, err := SourceRoot()
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range []string{root, "kakelDemo"} {
		if bytes.Contains(a[WasmName], []byte(in)) {
			t.Errorf("%q is in the program", in)
		}
	}
}
