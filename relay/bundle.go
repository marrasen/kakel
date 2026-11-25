//go:build !js

package relay

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"
)

// A bundle is kakel's web client as a host gives it to a relay: a zip of
// a few files, nothing in it packed, with a manifest of them that is
// what is signed. The zip is known by its SHA-256.
const (
	// manifestName is the manifest's file in the zip.
	manifestName = "MANIFEST"
	// bundlePurpose is the manifest's first line. The same key signs
	// other things: with this line first, a signature made for one of
	// them is never a bundle's, and a bundle's is of nothing else, as no
	// line of a manifest reads as one of a SHA256SUMS.
	bundlePurpose = "kakel web client 1"

	// wasmName is the client, gzipped, as it is in the zip. It is served
	// without its .gz.
	wasmName = "client.wasm.gz"
	// execName is what runs Go's wasm in a browser.
	execName = "wasm_exec.js"
)

// The limits of a bundle.
const (
	// MaxBundle is how large a bundle's zip may be.
	MaxBundle = 32 << 20
	// maxFiles is how many files it may have, with its manifest.
	maxFiles = 33
	// maxManifest is how large its manifest may be, and maxDirectory its
	// zip's own list of its files.
	maxManifest  = 16 << 10
	maxDirectory = 16 << 10
	// maxName is how long a file's name may be.
	maxName = 64
)

// maxUnpacked is how large a gzipped file in a bundle may be unpacked.
var maxUnpacked int64 = 128 << 20

// fileKinds are the files a bundle may hold, by their ending, and what
// they are served as.
var fileKinds = map[string]string{
	".html":  "text/html; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".txt":   "text/plain; charset=utf-8",
	".wasm":  "application/wasm",
	".png":   "image/png",
	".svg":   "image/svg+xml",
	".ico":   "image/x-icon",
	".woff2": "font/woff2",
}

// fileName is how a file of a bundle may be named: flat, in lower case,
// and with nothing a path or a shell reads as more than a name.
var fileName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*(\.[a-z0-9_-]+)+$`)

// Bundle is a bundle found sound.
type Bundle struct {
	// Hash is the SHA-256 of its zip, in hexadecimal: what it is known
	// by.
	Hash string
	// Manifest is what is signed.
	Manifest []byte
	// files are its files by the name they are served under.
	files map[string]*bundleFile
	// size is how much of them is kept.
	size int
}

// bundleFile is a file of a bundle.
type bundleFile struct {
	// kind is its Content-Type.
	kind string
	data []byte
	// hash is data's SHA-256, in hexadecimal.
	hash string
	// gzipped says data is gzipped.
	gzipped bool
}

// BundleHash is what a bundle's zip is known by.
func BundleHash(zipped []byte) string {
	sum := sha256.Sum256(zipped)
	return hex.EncodeToString(sum[:])
}

// isHash reports whether s is written as BundleHash writes one.
func isHash(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == sha256.Size && s == strings.ToLower(s)
}

// MakeBundle makes a bundle of files, by their names, and its manifest,
// which is what is signed: gunimsign signs it as it does any file. The
// same files are always the same zip.
func MakeBundle(files map[string][]byte) (zipped, manifest []byte, err error) {
	manifest = manifestOf(files)
	var out bytes.Buffer
	w := zip.NewWriter(&out)
	add := func(name string, data []byte) error {
		// Stored as it is: the client is gzipped already, and what a
		// compressor writes differs from one version to the next.
		f, err := w.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		if err == nil {
			_, err = f.Write(data)
		}
		return err
	}
	if err := add(manifestName, manifest); err != nil {
		return nil, nil, err
	}
	for _, name := range slices.Sorted(maps.Keys(files)) {
		if err := add(name, files[name]); err != nil {
			return nil, nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, nil, err
	}
	// Anything a relay would turn away is turned away here.
	if _, err := OpenBundle(out.Bytes()); err != nil {
		return nil, nil, err
	}
	return out.Bytes(), manifest, nil
}

// manifestOf is the manifest of files: the purpose, and then a line for
// each file, by name, of its SHA-256, its size and its name.
func manifestOf(files map[string][]byte) []byte {
	var b strings.Builder
	b.WriteString(bundlePurpose + "\n")
	for _, name := range slices.Sorted(maps.Keys(files)) {
		fmt.Fprintf(&b, "%x %d %s\n", sha256.Sum256(files[name]), len(files[name]), name)
	}
	return []byte(b.String())
}

// CheckBundle opens a bundle and checks that sig, as gunimsign writes it
// in a .sig, is key's signature of it.
func CheckBundle(zipped []byte, sig string, key ed25519.PublicKey) (*Bundle, error) {
	b, err := OpenBundle(zipped)
	if err != nil {
		return nil, err
	}
	if !b.signedWith(key, sig) {
		return nil, errors.New("the web client is not signed as a released kakel's is")
	}
	return b, nil
}

// signedWith reports whether sig is key's signature of the bundle's
// manifest.
func (b *Bundle) signedWith(key ed25519.PublicKey, sig string) bool {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sig))
	return err == nil && len(key) == ed25519.PublicKeySize && len(raw) == ed25519.SignatureSize &&
		ed25519.Verify(key, b.Manifest, raw)
}

// OpenBundle reads a bundle, and turns away anything that is not just
// what MakeBundle makes. It does not check who made it.
func OpenBundle(zipped []byte) (*Bundle, error) {
	if len(zipped) > MaxBundle {
		return nil, fmt.Errorf("the web client is %d bytes, and %d is the most it may be", len(zipped), MaxBundle)
	}
	if err := checkEnd(zipped); err != nil {
		return nil, err
	}
	zr, err := zip.NewReader(bytes.NewReader(zipped), int64(len(zipped)))
	if err != nil {
		return nil, fmt.Errorf("the web client is not a sound zip: %w", err)
	}
	if len(zr.File) > maxFiles {
		return nil, fmt.Errorf("the web client has %d files, and %d is the most it may have", len(zr.File), maxFiles)
	}
	held := make(map[string][]byte, len(zr.File))
	var total uint64
	for _, f := range zr.File {
		data, err := storedFile(f, &total, uint64(len(zipped)))
		if err != nil {
			return nil, err
		}
		if _, twice := held[f.Name]; twice {
			return nil, fmt.Errorf("the web client has %q twice", f.Name)
		}
		held[f.Name] = data
	}
	manifest, ok := held[manifestName]
	if !ok {
		return nil, errors.New("the web client has no manifest")
	}
	delete(held, manifestName)
	// Written again from the files and compared, so there is one way
	// alone to write it, and nothing in it but the files there are.
	if !bytes.Equal(manifest, manifestOf(held)) {
		return nil, errors.New("the web client's manifest is not of its files")
	}
	b := &Bundle{Hash: BundleHash(zipped), Manifest: manifest, files: make(map[string]*bundleFile, len(held))}
	for name, data := range held {
		f, err := fileOf(name, data)
		if err != nil {
			return nil, err
		}
		b.files[strings.TrimSuffix(name, ".gz")] = f
		b.size += len(data)
	}
	for _, name := range []string{indexName, execName, strings.TrimSuffix(wasmName, ".gz")} {
		if b.files[name] == nil {
			return nil, fmt.Errorf("the web client has no %s", name)
		}
	}
	return b, nil
}

// checkEnd checks how a zip ends, before anything is read of it: with a
// short list of its files, and nothing after. A zip can name as many
// files as its list has room for, whatever number it gives of them, and
// reading them takes many times the room the list does.
func checkEnd(zipped []byte) error {
	const endLen = 22
	at := len(zipped) - endLen
	if at < 0 || !bytes.HasPrefix(zipped[at:], []byte("PK\x05\x06")) {
		return errors.New("the web client is not a zip")
	}
	size := uint64(binary.LittleEndian.Uint32(zipped[at+12:]))
	from := uint64(binary.LittleEndian.Uint32(zipped[at+16:]))
	if size > maxDirectory || from+size != uint64(at) {
		return errors.New("the web client is not a sound zip")
	}
	return nil
}

// storedFile is what a file of a bundle's zip holds, with its size added
// to total, which may come to no more than most.
func storedFile(f *zip.File, total *uint64, most uint64) ([]byte, error) {
	if f.Name != manifestName {
		if len(f.Name) > maxName || !fileName.MatchString(f.Name) {
			return nil, fmt.Errorf("the web client has a file named %q", f.Name)
		}
	} else if f.UncompressedSize64 > maxManifest {
		return nil, errors.New("the web client's manifest is too long")
	}
	// Nothing packed, so nothing can be larger than the zip itself, and
	// no two files the same bytes of it.
	*total += f.UncompressedSize64
	switch {
	case f.Method != zip.Store || f.CompressedSize64 != f.UncompressedSize64 || f.Flags&1 != 0:
		return nil, fmt.Errorf("the web client's %s is packed", f.Name)
	case f.UncompressedSize64 > most || *total > most:
		return nil, fmt.Errorf("the web client's %s is larger than its zip", f.Name)
	case f.Mode()&fs.ModeType != 0:
		return nil, fmt.Errorf("the web client's %s is not a file", f.Name)
	}
	r, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("the web client's %s: %w", f.Name, err)
	}
	defer func() { _ = r.Close() }()
	data, err := io.ReadAll(io.LimitReader(r, int64(f.UncompressedSize64)+1))
	if err == nil && uint64(len(data)) != f.UncompressedSize64 {
		err = io.ErrUnexpectedEOF
	}
	if err != nil {
		return nil, fmt.Errorf("the web client's %s: %w", f.Name, err)
	}
	return data, nil
}

// fileOf is a file of a bundle as it is served.
func fileOf(name string, data []byte) (*bundleFile, error) {
	served, gzipped := strings.CutSuffix(name, ".gz")
	kind, ok := fileKinds[path.Ext(served)]
	// Wasm is gzipped, and nothing else is.
	if !ok || gzipped != (path.Ext(served) == ".wasm") {
		return nil, fmt.Errorf("the web client has a file named %q", name)
	}
	f := &bundleFile{kind: kind, data: data, hash: BundleHash(data), gzipped: gzipped}
	if !gzipped {
		return f, nil
	}
	// Unpacked once here, so a browser is not sent what it cannot unpack
	// or what has no end.
	var unpacked int64
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err == nil {
		unpacked, err = io.Copy(io.Discard, io.LimitReader(zr, maxUnpacked+1))
	}
	switch {
	case err != nil:
		return nil, fmt.Errorf("the web client's %s: %w", name, err)
	case unpacked > maxUnpacked:
		return nil, fmt.Errorf("the web client's %s is too large unpacked", name)
	}
	return f, nil
}
