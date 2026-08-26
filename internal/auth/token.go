package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// tokenSigner produces stateless HMAC-signed tokens (payload + signature),
// so that O(N) tokens don't need a server-side store to be validated. We still
// keep a store for expiration/revocation, but verification doesn't require it.
type tokenSigner struct {
	key []byte
}

func newTokenSigner(key []byte) *tokenSigner {
	return &tokenSigner{key: key}
}

type tokenPayload struct {
	Auth   string `json:"a"`
	User   string `json:"u"`
	Expiry int64  `json:"e"`
}

// Sign creates a token for a user id.
func (t *tokenSigner) Sign(user string) (string, error) {
	p := tokenPayload{
		Auth:   randomToken(),
		User:   user,
		Expiry: time.Now().Add(24 * time.Hour).Unix(),
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	b64 := base64.RawURLEncoding.EncodeToString(raw)
	sig := t.sign(b64)
	return b64 + "." + sig, nil
}

// Verify parses and authenticates a token, returning its user id.
func (t *tokenSigner) Verify(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return "", errors.New("malformed token")
	}
	payloadB64, sig := parts[0], parts[1]
	if !hmac.Equal([]byte(t.sign(payloadB64)), []byte(sig)) {
		return "", errors.New("invalid signature")
	}
	raw, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		return "", err
	}
	var p tokenPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", err
	}
	if time.Now().Unix() > p.Expiry {
		return "", errors.New("token expired")
	}
	return p.User, nil
}

func (t *tokenSigner) sign(data string) string {
	m := hmac.New(sha256.New, t.key)
	m.Write([]byte(data))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// randomToken returns a URL-safe random token (auth nonce).
func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
