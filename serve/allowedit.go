package serve

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

// Changing who may connect, from the window rather than by editing the
// file: a key pasted or read from a .pub file is added as one line, and
// a key taken away loses its lines, with every other line, comments
// too, kept as it was written.

// AllowedKey is a key that may connect, as the window lists it.
type AllowedKey struct {
	// Name is the key's comment, or its fingerprint when it has none,
	// Fingerprint its SHA256 fingerprint, and Type its algorithm.
	Name, Fingerprint, Type string
}

// Keys are the keys that may connect, in the file's order.
func (a *Allowed) Keys() []AllowedKey {
	if a == nil {
		return nil
	}
	names := a.Names()
	out := make([]AllowedKey, len(a.keys))
	for i, key := range a.keys {
		out[i] = AllowedKey{Name: names[i], Fingerprint: Fingerprint(key), Type: key.Type()}
	}
	return out
}

// ParseKey reads one public key, as pasted or read from a .pub file, by
// the rules the allowed keys are read by: one line of the authorized_keys
// format, with no options, and no certificate. It returns the key and
// its comment.
func ParseKey(text string) (ssh.PublicKey, string, error) {
	if strings.Contains(text, "PRIVATE KEY") {
		return nil, "", errors.New("that is a private key. Never share it: add the public key, from the .pub file beside it")
	}
	var line string
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		if line != "" {
			return nil, "", errors.New("that is more than one key: add them one at a time")
		}
		line = l
	}
	if line == "" {
		return nil, "", errors.New("there is no key there")
	}
	a, err := ParseAllowed([]byte(line), "the key")
	if err != nil {
		return nil, "", errors.New(strings.TrimPrefix(err.Error(), "serve: "))
	}
	return a.keys[0], a.names[0], nil
}

// Allow adds the key written as text, as [ParseKey] reads it, to the
// allowed keys in the file at path, and returns the name it is listed
// by. A key listed already is not listed twice, and a file that cannot
// be read is left as it is: a key added to it would not be honoured.
func Allow(path, text string) (string, error) {
	key, comment, err := ParseKey(text)
	if err != nil {
		return "", err
	}
	have, err := LoadAllowed(path)
	if err != nil {
		return "", err
	}
	if name, ok := have.Who(key); ok {
		return "", fmt.Errorf("that key may connect already, as %s", name)
	}
	raw, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("serve: read %s: %w", path, err)
	}
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
	if comment != "" {
		line += " " + comment
	}
	body := string(raw)
	if body != "" && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	if err := replace(path, []byte(body+line+"\n")); err != nil {
		return "", err
	}
	if comment == "" {
		return Fingerprint(key), nil
	}
	return comment, nil
}

// Disallow takes the key with the SHA256 fingerprint fp off the allowed
// keys in the file at path, every line that lists it, and returns the
// name it was listed by. Every other line stays as it was written.
func Disallow(path, fp string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("serve: read %s: %w", path, err)
	}
	// Read whole first, by the rules serving reads it by, so a file
	// that would not serve is not rewritten.
	if _, err := ParseAllowed(raw, path); err != nil {
		return "", err
	}
	var kept []string
	name := ""
	for _, line := range strings.SplitAfter(string(raw), "\n") {
		text := strings.TrimSpace(line)
		if text != "" && !strings.HasPrefix(text, "#") {
			key, comment, _, _, err := ssh.ParseAuthorizedKey([]byte(text))
			if err == nil && Fingerprint(key) == fp {
				if name == "" {
					name = Fingerprint(key)
					if c := strings.TrimSpace(comment); c != "" {
						name = c
					}
				}
				continue
			}
		}
		kept = append(kept, line)
	}
	if name == "" {
		return "", errors.New("that key was not among the keys that may connect")
	}
	return name, replace(path, []byte(strings.Join(kept, "")))
}

// replace puts body at path in one step, readable by its owner alone: a
// window reading the file meanwhile finds the old keys or the new, never
// half of them. A link at path is followed, so the file it names is the
// one that changes.
func replace(path string, body []byte) error {
	real, err := filepath.EvalSymlinks(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		real = path
	case err != nil:
		return fmt.Errorf("serve: write %s: %w", path, err)
	}
	dir := filepath.Dir(real)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("serve: write %s: %w", path, err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(real)+".*")
	if err != nil {
		return fmt.Errorf("serve: write %s: %w", path, err)
	}
	name := tmp.Name()
	fail := func(err error) error {
		_ = tmp.Close()
		_ = os.Remove(name)
		return fmt.Errorf("serve: write %s: %w", path, err)
	}
	if err := tmp.Chmod(0o600); err != nil && modesMeanSomething {
		return fail(err)
	}
	if _, err := tmp.Write(body); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("serve: write %s: %w", path, err)
	}
	if err := os.Rename(name, real); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("serve: write %s: %w", path, err)
	}
	return nil
}
