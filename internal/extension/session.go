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

type SessionStore struct {
	mu          sync.RWMutex
	credentials map[string][]byte // built-in name -> ephemeral credential
	sessions    map[string]string // session token -> identity
	connections map[string]string // identity -> session token
	policyKinds map[string]string // policy kind -> identity
	logger      logr.Logger

	builtinNames   map[string]struct{}
	warmupComplete bool
}

func NewSessionStore(logger logr.Logger) *SessionStore {
	return &SessionStore{
		credentials:    make(map[string][]byte),
		sessions:       make(map[string]string),
		connections:    make(map[string]string),
		policyKinds:    make(map[string]string),
		logger:         logger,
		builtinNames:   make(map[string]struct{}),
		warmupComplete: true,
	}
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
		if _, ok := s.connections[name]; !ok {
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

	if _, connected := s.connections[identity]; connected {
		return "", ErrAlreadyConnected
	}

	if owner, taken := s.policyKinds[policyKind]; taken {
		return "", fmt.Errorf("%w: %q is owned by %q", ErrPolicyKindTaken, policyKind, owner)
	}

	token, err := generateSessionToken()
	if err != nil {
		return "", fmt.Errorf("failed to generate session token: %w", err)
	}

	s.sessions[token] = identity
	s.connections[identity] = token
	s.policyKinds[policyKind] = identity

	if !s.warmupComplete && s.allBuiltinsRegisteredLocked() {
		s.warmupComplete = true
	}

	return token, nil
}

func (s *SessionStore) ValidateSession(token string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	identity, ok := s.sessions[token]
	return identity, ok
}

func (s *SessionStore) RevokeByName(identity string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.revokeIdentityLocked(identity)
}

func (s *SessionStore) revokeIdentityLocked(identity string) bool {
	token, ok := s.connections[identity]
	if !ok {
		return false
	}
	delete(s.sessions, token)
	delete(s.connections, identity)
	for kind, owner := range s.policyKinds {
		if owner == identity {
			delete(s.policyKinds, kind)
			break
		}
	}
	return true
}

func generateSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
