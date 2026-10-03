// Package seal encrypts small JSON payloads into URL-safe strings with
// AES-256-GCM. It is how the OAuth layer stays stateless: client IDs,
// authorization requests, codes and tokens are sealed values rather than rows
// in a database. Several keys can be configured for rotation: the first seals,
// all of them open.
package seal

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrInvalid is returned for anything that does not open: wrong key, wrong
// purpose, tampering or garbage.
var ErrInvalid = errors.New("invalid sealed value")

// Box seals and opens values.
type Box struct {
	keys  [][]byte
	aeads []cipher.AEAD
}

// ParseKeys reads a comma-separated list of base64 (standard or URL, padded or
// not) 32-byte keys.
func ParseKeys(s string) ([][]byte, error) {
	var out [][]byte
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		var k []byte
		var err error
		for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
			if k, err = enc.DecodeString(part); err == nil {
				break
			}
		}
		if err != nil {
			return nil, fmt.Errorf("seal: key is not base64: %w", err)
		}
		if len(k) != 32 {
			return nil, fmt.Errorf("seal: key must be 32 bytes, got %d", len(k))
		}
		out = append(out, k)
	}
	if len(out) == 0 {
		return nil, errors.New("seal: no keys")
	}
	return out, nil
}

// New builds a Box from raw 32-byte keys.
func New(keys [][]byte) (*Box, error) {
	b := &Box{keys: keys}
	for _, k := range keys {
		blk, err := aes.NewCipher(k)
		if err != nil {
			return nil, err
		}
		g, err := cipher.NewGCM(blk)
		if err != nil {
			return nil, err
		}
		b.aeads = append(b.aeads, g)
	}
	if len(b.aeads) == 0 {
		return nil, errors.New("seal: no keys")
	}
	return b, nil
}

// FromString is ParseKeys + New.
func FromString(s string) (*Box, error) {
	keys, err := ParseKeys(s)
	if err != nil {
		return nil, err
	}
	return New(keys)
}

// Seal encrypts v as JSON. purpose is bound as additional authenticated data,
// so a value sealed for one purpose never opens as another.
// Output: base64url(nonce || ciphertext || tag), unpadded.
func (b *Box) Seal(purpose string, v any) (string, error) {
	pt, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	g := b.aeads[0]
	nonce := make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := g.Seal(nonce, nonce, pt, []byte(purpose))
	return base64.RawURLEncoding.EncodeToString(out), nil
}

// Open decrypts a value sealed for purpose into v.
func (b *Box) Open(purpose, s string, v any) error {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
	if err != nil {
		return ErrInvalid
	}
	for _, g := range b.aeads {
		ns := g.NonceSize()
		if len(raw) < ns+g.Overhead() {
			return ErrInvalid
		}
		pt, err := g.Open(nil, raw[:ns], raw[ns:], []byte(purpose))
		if err != nil {
			continue
		}
		if err := json.Unmarshal(pt, v); err != nil {
			return ErrInvalid
		}
		return nil
	}
	return ErrInvalid
}

// MAC returns an HMAC-SHA256 of data under the primary key, base64url encoded.
func (b *Box) MAC(purpose, data string) string {
	return mac(b.keys[0], purpose, data)
}

// VerifyMAC checks a MAC against every configured key, in constant time.
func (b *Box) VerifyMAC(purpose, data, got string) bool {
	for _, k := range b.keys {
		if hmac.Equal([]byte(mac(k, purpose, data)), []byte(got)) {
			return true
		}
	}
	return false
}

func mac(key []byte, purpose, data string) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(purpose))
	m.Write([]byte{0})
	m.Write([]byte(data))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// Hash is base64url(sha256(s)), used to bind one sealed value to another.
func Hash(s string) string {
	h := sha256.Sum256([]byte(s))
	return base64.RawURLEncoding.EncodeToString(h[:])
}
