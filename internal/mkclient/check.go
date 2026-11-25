package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/marrasen/kakel/relay"
)

// program is a finished program, by the name it is told of as.
type program struct {
	name string
	data []byte
}

// checkPrograms checks that the programs at paths, each a program or an
// archive of them, hold one and the same web client, signed with key. It
// looks in the programs themselves, as the folder a client is built into
// may hold another by now.
func checkPrograms(paths []string, key ed25519.PublicKey) error {
	if len(paths) == 0 {
		return errors.New("name the programs to check, or the archives of them")
	}
	// hash is the client's, and "" for none.
	hash, first := "", ""
	for _, path := range paths {
		programs, err := programsAt(path)
		if err != nil {
			return err
		}
		if len(programs) == 0 {
			return fmt.Errorf("%s has no program in it", path)
		}
		for _, p := range programs {
			got, err := heldIn(p.data, key)
			if err != nil {
				return fmt.Errorf("%s: %w", p.name, err)
			}
			if first == "" {
				hash, first = got, p.name
			}
			switch {
			case got != hash:
				return fmt.Errorf("%s and %s do not hold the same web client", first, p.name)
			case got == "":
				return fmt.Errorf("%s holds no web client, and can share no window with a link: it is built with the tag webclient", p.name)
			default:
				fmt.Printf("%s: web client %s, signed with kakel's release key\n", p.name, got)
			}
		}
	}
	return nil
}

// programsAt are the programs at path: the files of a zip or a .tar.gz,
// or the file itself.
func programsAt(path string) ([]program, error) {
	switch {
	case strings.HasSuffix(path, ".zip"):
		return zippedPrograms(path)
	case strings.HasSuffix(path, ".tar.gz"):
		return tarredPrograms(path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return []program{{path, data}}, nil
}

// zippedPrograms are the files of the zip at path.
func zippedPrograms(path string) ([]program, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	defer func() { _ = zr.Close() }()
	var out []program
	for _, f := range zr.File {
		if !f.Mode().IsRegular() {
			continue
		}
		r, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		data, err := io.ReadAll(r)
		if err := errors.Join(err, r.Close()); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		out = append(out, program{f.Name + " in " + path, data})
	}
	return out, nil
}

// tarredPrograms are the files of the .tar.gz at path.
func tarredPrograms(path string) ([]program, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	z, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	var out []program
	for tr := tar.NewReader(z); ; {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		out = append(out, program{h.Name + " in " + path, data})
	}
}

// heldIn is the hash of the web client embedded in a program, and "" for
// a program with none. Every one in it has to be signed with key, by a
// signature that is in the program too. Go embeds a file as it is.
func heldIn(data []byte, key ed25519.PublicKey) (string, error) {
	hash := ""
	for at := 0; ; at++ {
		n := bytes.Index(data[at:], bundleStart)
		if n < 0 {
			return hash, nil
		}
		at += n
		if !startsBundle(data[at:]) {
			continue
		}
		b, err := relay.OpenBundle(bundleAt(data[at:]))
		if err != nil {
			return "", err
		}
		if !signedIn(data, b, key) {
			return "", errors.New("the web client in it is not signed with kakel's release key")
		}
		if hash != "" && hash != b.Hash {
			return "", errors.New("there are two web clients in it")
		}
		hash = b.Hash
	}
}

// bundleStart is what the zip of a bundle begins with, and bundleFirst
// the name of the first file in it, after the rest of that file's header.
var (
	bundleStart = []byte("PK\x03\x04")
	bundleFirst = []byte("MANIFEST")
)

// startsBundle reports whether data begins as a bundle does.
func startsBundle(data []byte) bool {
	const header = 30
	return len(data) >= header+len(bundleFirst) && int(binary.LittleEndian.Uint16(data[26:])) == len(bundleFirst) &&
		bytes.HasPrefix(data[header:], bundleFirst)
}

// bundleAt is the bundle data begins with, to the end of the zip, which
// says itself where that starts. It is nothing when that is not found.
func bundleAt(data []byte) []byte {
	const end = 22
	most := data[:min(len(data), relay.MaxBundle)]
	for at := 0; ; at++ {
		n := bytes.Index(most[at:], []byte("PK\x05\x06"))
		if n < 0 || at+n+end > len(most) {
			return nil
		}
		at += n
		size := uint64(binary.LittleEndian.Uint32(most[at+12:]))
		from := uint64(binary.LittleEndian.Uint32(most[at+16:]))
		if from+size == uint64(at) {
			return most[:at+end]
		}
	}
}

// signedIn reports whether a signature of the bundle b with key is in a
// program's data, as gunimsign writes one.
func signedIn(data []byte, b *relay.Bundle, key ed25519.PublicKey) bool {
	// 64 bytes in base64, and a line's end.
	const digits = 86
	tail := []byte("==\n")
	for at := 0; ; at += len(tail) {
		n := bytes.Index(data[at:], tail)
		if n < 0 {
			return false
		}
		at += n
		if at < digits {
			continue
		}
		sig := make([]byte, ed25519.SignatureSize)
		if _, err := base64.StdEncoding.Strict().Decode(sig, data[at-digits:at+2]); err == nil && ed25519.Verify(key, b.Manifest, sig) {
			return true
		}
	}
}
