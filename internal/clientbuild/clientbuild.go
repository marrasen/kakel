// Package clientbuild builds kakel's web client out of its source: the
// program, for a browser, and the page it starts in. cmd mkclient makes
// the bundle a relay serves of it, and the tests build it to try it.
package clientbuild

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/marrasen/kakel/appicon"
)

// WasmName is the program's file, as the page asks for it. In a bundle
// it is gzipped, with .gz after its name.
const WasmName = "client.wasm"

// pageFiles are the files of the page, in the folder webclient. The
// page's scripts and styles are files of their own, as the policy a
// relay serves it under lets nothing inline run.
var pageFiles = []string{"index.html", "page.css", "start.js"}

// iconName is kakel's icon, which the page shows and is known by, and
// iconSide how large it is, in pixels a side.
const (
	iconName = "icon.png"
	iconSide = 192
)

// Demo is the build tag of a client with the demo, for the tests. What
// is released is built without.
const Demo = "demo"

// Build builds the client under tags, and returns its files by name as a
// browser is to have them: nothing is gzipped. The same source and the
// same Go always give the same bytes.
func Build(tags ...string) (map[string][]byte, error) {
	root, err := SourceRoot()
	if err != nil {
		return nil, err
	}
	wasm, err := program(root, tags)
	if err != nil {
		return nil, err
	}
	files, err := page(root, len(wasm))
	if err != nil {
		return nil, err
	}
	if files["wasm_exec.js"], err = runner(root); err != nil {
		return nil, err
	}
	files[WasmName] = wasm
	return files, nil
}

// Packed is files as they are put in a bundle: the program gzipped.
func Packed(files map[string][]byte) (map[string][]byte, error) {
	out := make(map[string][]byte, len(files))
	for name, data := range files {
		if name != WasmName {
			out[name] = data
			continue
		}
		packed, err := gzipped(data)
		if err != nil {
			return nil, err
		}
		out[name+".gz"] = packed
	}
	return out, nil
}

// gzipped is data gzipped as small as Go makes it, with nothing in it of
// when or where that was done.
func gzipped(data []byte) ([]byte, error) {
	var out bytes.Buffer
	w, err := gzip.NewWriterLevel(&out, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// SourceRoot is the folder of kakel's source.
func SourceRoot() (string, error) {
	mod, err := output("", "go", "env", "GOMOD")
	if err != nil {
		return "", err
	}
	if mod == "" || mod == os.DevNull {
		return "", errors.New("building the web client: not in kakel's source")
	}
	return filepath.Dir(mod), nil
}

// program builds the program under tags.
func program(root string, tags []string) ([]byte, error) {
	dir, err := os.MkdirTemp("", "kakel-client")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	out := filepath.Join(dir, WasmName)
	// Nothing of the machine or the checkout it is built from: no paths,
	// no commit, and no build id. And no names, which is a fifteenth of
	// it and shows in nothing but a debugger. The tags are always said,
	// so the ones of the build this is part of are not taken over.
	cmd := exec.Command("go", "build", "-tags", strings.Join(tags, ","),
		"-trimpath", "-buildvcs=false", "-ldflags", "-s -w -buildid=", "-o", out, "./webclient")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", "CGO_ENABLED=0", "GOFLAGS="+forWasm(os.Getenv("GOFLAGS")))
	if said, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("building the web client: %w\n%s", err, said)
	}
	return os.ReadFile(out)
}

// forWasm is flags for every go command, less those a build for a
// browser has no support for.
func forWasm(flags string) string {
	var out []string
	for _, f := range strings.Fields(flags) {
		name, _, _ := strings.Cut(strings.TrimLeft(f, "-"), "=")
		if !slices.Contains([]string{"race", "msan", "asan", "cover", "covermode", "coverpkg"}, name) {
			out = append(out, f)
		}
	}
	return strings.Join(out, " ")
}

// runner is the script that runs Go's programs in a browser, of the Go
// that builds them: one of another version does not run them.
func runner(root string) ([]byte, error) {
	goroot, err := output(root, "go", "env", "GOROOT")
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(filepath.Join(goroot, "lib", "wasm", "wasm_exec.js"))
	if errors.Is(err, os.ErrNotExist) {
		// Where it was before Go 1.24.
		b, err = os.ReadFile(filepath.Join(goroot, "misc", "wasm", "wasm_exec.js"))
	}
	if err != nil {
		return nil, fmt.Errorf("building the web client: %w", err)
	}
	return lines(b), nil
}

// page is the page's files, told how large the program is.
func page(root string, size int) (map[string][]byte, error) {
	files := map[string][]byte{}
	for _, name := range pageFiles {
		b, err := os.ReadFile(filepath.Join(root, "webclient", name))
		if err != nil {
			return nil, err
		}
		files[name] = lines(b)
	}
	index, err := sized(files["index.html"], size)
	if err != nil {
		return nil, err
	}
	files["index.html"] = index
	var icon bytes.Buffer
	if err := png.Encode(&icon, appicon.Draw(iconSide)); err != nil {
		return nil, err
	}
	files[iconName] = icon.Bytes()
	return files, nil
}

// sizeMeta is the part of the index that says how large the program is,
// and sizeUnknown the same as it stands in the source.
const (
	sizeMeta    = `<meta name="kakel-size" content="%d">`
	sizeUnknown = `<meta name="kakel-size" content="0">`
)

// sized is the index saying the program is size bytes. A browser counts
// what it unpacks, and is told only how large it is packed.
func sized(index []byte, size int) ([]byte, error) {
	if n := bytes.Count(index, []byte(sizeUnknown)); n != 1 {
		return nil, fmt.Errorf("building the web client: index.html has %s %s times", sizeUnknown, strconv.Itoa(n))
	}
	return bytes.Replace(index, []byte(sizeUnknown), fmt.Appendf(nil, sizeMeta, size), 1), nil
}

// lines is text with the line ends git gives it on Linux, whatever it
// gave it here, so the client is the same bytes wherever it is built.
func lines(text []byte) []byte { return bytes.ReplaceAll(text, []byte("\r\n"), []byte("\n")) }

// output is what a command run in dir prints, trimmed.
func output(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}
