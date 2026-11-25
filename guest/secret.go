package guest

import (
	"crypto/ed25519"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"

	"golang.org/x/crypto/ssh"
)

// Secret is what a link carries: the seed the guest's link key is made
// from, and the start of the hash of the share's host key, which the
// guest checks the host by. Whoever has it can ask to join, once.
type Secret struct {
	seed [seedLen]byte
	host [hashLen]byte
}

const (
	seedLen = 16
	hashLen = 16
)

// secretText is how a Secret is written: with no padding, and only one
// way to write each.
var secretText = base64.RawURLEncoding.Strict()

// String is the secret as a link carries it, 43 characters a URL's
// fragment holds as they are.
func (s Secret) String() string {
	return secretText.EncodeToString(append(s.seed[:], s.host[:]...))
}

// ParseSecret reads what String wrote.
func ParseSecret(text string) (Secret, error) {
	raw, ok := readText(text, seedLen+hashLen)
	if !ok {
		return Secret{}, errors.New("the link is not whole: it was cut short, or has something added")
	}
	var s Secret
	copy(s.seed[:], raw)
	copy(s.host[:], raw[seedLen:])
	return s, nil
}

// readText reads n bytes written as secretText writes them, and as nothing
// else does: it skips line breaks when it reads.
func readText(text string, n int) ([]byte, bool) {
	raw, err := secretText.DecodeString(text)
	return raw, err == nil && len(raw) == n && len(text) == secretText.EncodedLen(n)
}

// linkKey is the key a guest that has not joined yet logs in with.
func (s Secret) linkKey() (ssh.Signer, error) {
	seed, err := hkdf.Key(sha256.New, s.seed[:], nil, "kakel share link key v1", ed25519.SeedSize)
	if err != nil {
		return nil, fmt.Errorf("make the link's key: %w", err)
	}
	return ssh.NewSignerFromKey(ed25519.NewKeyFromSeed(seed))
}

// madeBy reports whether key is the host key the secret was made for.
func (s Secret) madeBy(key ssh.PublicKey) bool {
	sum := sha256.Sum256(key.Marshal())
	return key.Type() == ssh.KeyAlgoED25519 && subtle.ConstantTimeCompare(sum[:hashLen], s.host[:]) == 1
}

// Key is a guest's own key, made by its tab and kept by it, so that the
// same tab comes back as the same guest after a reload or a lost network.
type Key [ed25519.SeedSize]byte

// NewKey makes a key for a guest.
func NewKey() (Key, error) {
	var k Key
	if _, err := rand.Read(k[:]); err != nil {
		return Key{}, fmt.Errorf("make the guest's key: %w", err)
	}
	return k, nil
}

// String is the key as text, for a tab to keep.
func (k Key) String() string { return secretText.EncodeToString(k[:]) }

// ParseKey reads what String wrote.
func ParseKey(text string) (Key, error) {
	raw, ok := readText(text, len(Key{}))
	if !ok {
		return Key{}, errors.New("that is not a guest's key")
	}
	return Key(raw), nil
}

func (k Key) private() ed25519.PrivateKey { return ed25519.NewKeyFromSeed(k[:]) }

// newShare makes a share's host key and the secret that goes with it.
func newShare() (ssh.Signer, Secret, error) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, Secret{}, fmt.Errorf("make the share's key: %w", err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		return nil, Secret{}, fmt.Errorf("make the share's key: %w", err)
	}
	var s Secret
	if _, err := rand.Read(s.seed[:]); err != nil {
		return nil, Secret{}, fmt.Errorf("make the link's secret: %w", err)
	}
	sum := sha256.Sum256(signer.PublicKey().Marshal())
	copy(s.host[:], sum[:])
	return signer, s, nil
}

// algorithms are what both ends use, one build of one program: pinned, so
// nothing older can be asked for. The cipher is the one a browser, with
// no AES of its own to lend, is quickest at.
func algorithms() ssh.Config {
	return ssh.Config{
		KeyExchanges: []string{ssh.KeyExchangeMLKEM768X25519, ssh.KeyExchangeCurve25519},
		Ciphers:      []string{ssh.CipherChaCha20Poly1305},
		MACs:         []string{ssh.HMACSHA256ETM},
	}
}

// keyAlgorithms are the kinds of key either end shows the other.
var keyAlgorithms = []string{ssh.KeyAlgoED25519}

// version is what both ends call themselves.
const version = "SSH-2.0-kakel"
