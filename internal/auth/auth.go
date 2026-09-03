// Package auth implements session login and role-based access. Passwords are
// stored as bcrypt hashes (or plaintext during bootstrap). A session is an
// opaque random token carried in a cookie; its value is looked up in a signed
// store. Viewer and admin roles protect the write endpoints.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"sync"
	"time"

	"github.com/hedchr/fanctrl/internal/config"
	"golang.org/x/crypto/bcrypt"
)

// Session represents a validated login session.
type Session struct {
	User    string
	Role    string
	Expires time.Time
}

// Store manages users and active sessions.
type Store struct {
	users  map[string]config.User
	secret []byte
	ttl    time.Duration
	tokens map[string]Session // token -> session
	mu     sync.RWMutex
	signer *tokenSigner
}

// NewStore builds an auth store from the configured users and a signing secret.
// If no secret is provided, a random one is generated (sessions won't survive a
// restart, which is acceptable for a local dev flow).
func NewStore(users []config.User, secret []byte, ttl time.Duration) *Store {
	s := &Store{
		users:  map[string]config.User{},
		secret: secret,
		ttl:    ttl,
		tokens: map[string]Session{},
	}
	if len(secret) == 0 {
		secret = make([]byte, 32)
		_, _ = rand.Read(secret)
	}
	s.signer = newTokenSigner(secret)
	for _, u := range users {
		s.users[u.Name] = u
	}
	return s
}

// ReplaceUsers hot-replaces the user list (used when settings change).
// Existing sessions remain valid as long as the user still exists.
func (s *Store) ReplaceUsers(users []config.User) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users = map[string]config.User{}
	for _, u := range users {
		s.users[u.Name] = u
	}
}

// Authenticate validates credentials and issues a session token.
func (s *Store) Authenticate(user, password string) (string, config.User, error) {
	u, ok := s.users[user]
	if !ok {
		// Constant-ish time to avoid user enumeration.
		_ = bcrypt.CompareHashAndPassword(bcryptHashStub, []byte(password))
		return "", config.User{}, errors.New("invalid credentials")
	}
	if !checkPassword(u, password) {
		return "", config.User{}, errors.New("invalid credentials")
	}
	token, err := s.signer.Sign(s.userID(u))
	if err != nil {
		return "", config.User{}, err
	}
	s.mu.Lock()
	s.tokens[token] = Session{User: u.Name, Role: u.Role, Expires: time.Now().Add(s.ttl)}
	s.mu.Unlock()
	return token, u, nil
}

// Validate checks a token and returns the session, refreshing the expiry.
func (s *Store) Validate(token string) (Session, bool) {
	uid, err := s.signer.Verify(token)
	if err != nil {
		return Session{}, false
	}
	sess, ok := s.tokens[token]
	if !ok {
		return Session{}, false
	}
	if time.Now().After(sess.Expires) {
		s.mu.Lock()
		delete(s.tokens, token)
		s.mu.Unlock()
		return Session{}, false
	}
	if sess.User != uid {
		return Session{}, false
	}
	s.mu.Lock()
	s.tokens[token] = Session{User: sess.User, Role: sess.Role, Expires: time.Now().Add(s.ttl)}
	s.mu.Unlock()
	return sess, true
}

// Revoke removes a token (logout).
func (s *Store) Revoke(token string) {
	s.mu.Lock()
	delete(s.tokens, token)
	s.mu.Unlock()
}

func (s *Store) userID(u config.User) string {
	return u.Name
}

// checkPassword compares a plaintext against a user's stored password. A user
// config Hash flag controls whether the stored value is a bcrypt hash.
func checkPassword(u config.User, password string) bool {
	if u.Hash {
		return bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(password)) == nil
	}
	// Plaintext comparison uses subtle constant-time.
	return subtle.ConstantTimeCompare([]byte(u.Password), []byte(password)) == 1
}

// bcryptHashStub is compared against a fixed hash to amortize timing across
// user/enumeration attempts (not cryptographically meaningful).
var bcryptHashStub = func() []byte {
	h, _ := bcrypt.GenerateFromPassword([]byte("stub"), bcrypt.DefaultCost)
	return h
}()

// GenerateToken is a helper for tests.
func GenerateToken() string { return randomToken() }
