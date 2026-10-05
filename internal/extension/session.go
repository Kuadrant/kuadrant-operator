package extension

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
)

var (
	ErrAlreadyConnected   = errors.New("extension already connected")
	ErrPolicyKindRequired = errors.New("policy_kind is required")
	ErrPolicyKindTaken    = errors.New("policy kind already registered by another extension")
)

const defaultSessionTTL = 45 * time.Second

type session struct {
	identity   string
	token      string
	policyKind string
	lastSeen   time.Time
}

type SessionStore struct {
	mu          sync.RWMutex
	credentials map[string][]byte   // built-in name -> ephemeral credential
	sessions    map[string]*session // session token -> session
	logger      logr.Logger

	sessionTTL time.Duration
	now        func() time.Time

	builtinNames   map[string]struct{}
	warmupComplete bool
}

func NewSessionStore(logger logr.Logger) *SessionStore {
	return &SessionStore{
		credentials:    make(map[string][]byte),
		sessions:       make(map[string]*session),
		logger:         logger,
		sessionTTL:     defaultSessionTTL,
		now:            time.Now,
		builtinNames:   make(map[string]struct{}),
		warmupComplete: true,
	}
}

func (s *SessionStore) SetSessionTTL(ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessionTTL = ttl
}

func (s *SessionStore) BeginWarmup(builtinNames []string, timeout time.Duration) {
	s.mu.Lock()
	for _, name := range builtinNames {
		s.builtinNames[name] = struct{}{}
	}
	s.warmupComplete = s.allBuiltinsRegisteredLocked()
	pending := !s.warmupComplete
	s.mu.Unlock()

	if !pending {
		return
	}

	time.AfterFunc(timeout, func() {
		s.mu.Lock()
		elapsed := !s.warmupComplete
		s.warmupComplete = true
		s.mu.Unlock()
		if elapsed {
			s.logger.Info("warmup timeout elapsed, admitting standalone extensions", "timeout", timeout)
		}
	})
}

func (s *SessionStore) isWarmupComplete() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.warmupComplete
}

func (s *SessionStore) allBuiltinsRegisteredLocked() bool {
	for name := range s.builtinNames {
		if s.sessionByIdentityLocked(name) == nil {
			return false
		}
	}
	return true
}

func (s *SessionStore) SetCredential(name string, credential []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setCredentialLocked(name, credential)
}

func (s *SessionStore) setCredentialLocked(name string, credential []byte) {
	stored := make([]byte, len(credential))
	copy(stored, credential)

	if existing, ok := s.credentials[name]; ok {
		if subtle.ConstantTimeCompare(existing, stored) == 1 {
			return
		}
		s.revokeIdentityLocked(name)
	}

	s.credentials[name] = stored
}

func (s *SessionStore) matchBuiltin(token []byte) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	name := ""
	found := false
	for candidate, credential := range s.credentials {
		if subtle.ConstantTimeCompare(token, credential) == 1 {
			name = candidate
			found = true
		}
	}
	return name, found
}

func (s *SessionStore) CreateSession(identity, policyKind string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if policyKind == "" {
		return "", ErrPolicyKindRequired
	}

	// A live session is never hijacked; a stale one is revoked here so a returning
	// or replacement extension can take over without waiting for the reaper.
	if existing := s.sessionByIdentityLocked(identity); existing != nil {
		if !s.isStaleLocked(existing) {
			return "", ErrAlreadyConnected
		}
		delete(s.sessions, existing.token)
	}

	if owner := s.sessionByPolicyKindLocked(policyKind); owner != nil {
		if !s.isStaleLocked(owner) {
			return "", fmt.Errorf("%w: %q is owned by %q", ErrPolicyKindTaken, policyKind, owner.identity)
		}
		delete(s.sessions, owner.token)
	}

	token, err := generateSessionToken()
	if err != nil {
		return "", fmt.Errorf("failed to generate session token: %w", err)
	}

	s.sessions[token] = &session{
		identity:   identity,
		token:      token,
		policyKind: policyKind,
		lastSeen:   s.now(),
	}

	if !s.warmupComplete && s.allBuiltinsRegisteredLocked() {
		s.warmupComplete = true
	}

	return token, nil
}

func (s *SessionStore) Touch(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[token]; ok {
		sess.lastSeen = s.now()
	}
}

func (s *SessionStore) ReapStale() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	var revoked []string
	for token, sess := range s.sessions {
		if s.isStaleLocked(sess) {
			delete(s.sessions, token)
			revoked = append(revoked, sess.identity)
		}
	}
	return revoked
}

func (s *SessionStore) isStaleLocked(sess *session) bool {
	return s.now().Sub(sess.lastSeen) > s.sessionTTL
}

func (s *SessionStore) sessionByIdentityLocked(identity string) *session {
	for _, sess := range s.sessions {
		if sess.identity == identity {
			return sess
		}
	}
	return nil
}

func (s *SessionStore) sessionByPolicyKindLocked(policyKind string) *session {
	for _, sess := range s.sessions {
		if sess.policyKind == policyKind {
			return sess
		}
	}
	return nil
}

func (s *SessionStore) ValidateSession(token string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if sess, ok := s.sessions[token]; ok {
		return sess.identity, true
	}
	return "", false
}

func (s *SessionStore) RevokeByName(identity string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.revokeIdentityLocked(identity)
}

func (s *SessionStore) revokeIdentityLocked(identity string) bool {
	sess := s.sessionByIdentityLocked(identity)
	if sess == nil {
		return false
	}
	delete(s.sessions, sess.token)
	return true
}

func generateSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
