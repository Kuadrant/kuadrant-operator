package extension

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

	"github.com/go-logr/logr"
)

const minCredentialLength = 32

var (
	ErrUnknownExtension   = errors.New("unknown extension")
	ErrInvalidCredential  = errors.New("invalid credential")
	ErrAlreadyConnected   = errors.New("extension already connected")
	ErrPolicyKindRequired = errors.New("policy_kind is required")
	ErrPolicyKindTaken    = errors.New("policy kind already registered by another extension")
)

type SessionStore struct {
	mu          sync.RWMutex
	credentials map[string][]byte
	sessions    map[string]string
	extensions  map[string]string
	policyKinds map[string]string // policy kind -> extension name
	logger      logr.Logger
}

func NewSessionStore(logger logr.Logger) *SessionStore {
	return &SessionStore{
		credentials: make(map[string][]byte),
		sessions:    make(map[string]string),
		extensions:  make(map[string]string),
		policyKinds: make(map[string]string),
		logger:      logger,
	}
}

func (s *SessionStore) SetCredential(name string, credential []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	stored := make([]byte, len(credential))
	copy(stored, credential)

	if existing, ok := s.credentials[name]; ok {
		if subtle.ConstantTimeCompare(existing, stored) == 1 {
			return
		}
		s.revokeByNameLocked(name)
	}

	s.credentials[name] = stored
}

func (s *SessionStore) RemoveCredential(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.credentials, name)
	return s.revokeByNameLocked(name)
}

func (s *SessionStore) Authenticate(name string, credential []byte, policyKind string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	expected, ok := s.credentials[name]
	if !ok {
		return "", ErrUnknownExtension
	}

	if subtle.ConstantTimeCompare(credential, expected) != 1 || len(credential) < minCredentialLength {
		return "", ErrInvalidCredential
	}

	if policyKind == "" {
		return "", ErrPolicyKindRequired
	}

	if _, connected := s.extensions[name]; connected {
		return "", ErrAlreadyConnected
	}

	if owner, taken := s.policyKinds[policyKind]; taken {
		return "", fmt.Errorf("%w: %q is owned by %q", ErrPolicyKindTaken, policyKind, owner)
	}

	token, err := generateSessionToken()
	if err != nil {
		return "", fmt.Errorf("failed to generate session token: %w", err)
	}

	s.sessions[token] = name
	s.extensions[name] = token
	s.policyKinds[policyKind] = name
	return token, nil
}

func (s *SessionStore) ValidateSession(token string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	name, ok := s.sessions[token]
	return name, ok
}

func (s *SessionStore) RevokeByName(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.revokeByNameLocked(name)
}

func (s *SessionStore) revokeByNameLocked(name string) bool {
	token, ok := s.extensions[name]
	if !ok {
		return false
	}
	delete(s.sessions, token)
	delete(s.extensions, name)
	for kind, owner := range s.policyKinds {
		if owner == name {
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
