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
	// Warmup gate
	builtinNames   map[string]struct{}
	warmupComplete bool
	secretNames    map[string]struct{}
}

func NewSessionStore(logger logr.Logger) *SessionStore {
	return &SessionStore{
		credentials:    make(map[string][]byte),
		sessions:       make(map[string]string),
		extensions:     make(map[string]string),
		policyKinds:    make(map[string]string),
		logger:         logger,
		builtinNames:   make(map[string]struct{}),
		warmupComplete: true,
		secretNames:    make(map[string]struct{}),
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

// handshakeAdmitted reports whether extension may handshake now
func (s *SessionStore) handshakeAdmitted(name string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.builtinNames[name]; ok {
		return true
	}
	return s.warmupComplete
}

func (s *SessionStore) allBuiltinsRegisteredLocked() bool {
	for name := range s.builtinNames {
		if _, ok := s.extensions[name]; !ok {
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
		s.revokeByNameLocked(name)
	}

	s.credentials[name] = stored
}

type CredentialSyncResult struct {
	Added   int
	Rotated int
	Removed int
}

func (r CredentialSyncResult) Changed() bool {
	return r.Added > 0 || r.Rotated > 0 || r.Removed > 0
}

func (s *SessionStore) SyncSecretCredentials(credentials map[string][]byte) CredentialSyncResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	var result CredentialSyncResult
	for name, credential := range credentials {
		if _, isBuiltin := s.builtinNames[name]; isBuiltin {
			continue
		}
		existing, existed := s.credentials[name]
		unchanged := existed && subtle.ConstantTimeCompare(existing, credential) == 1
		s.setCredentialLocked(name, credential)
		s.secretNames[name] = struct{}{}
		switch {
		case !existed:
			result.Added++
		case !unchanged:
			result.Rotated++
		}
	}

	for name := range s.secretNames {
		if _, present := credentials[name]; present {
			continue
		}
		delete(s.credentials, name)
		s.revokeByNameLocked(name)
		delete(s.secretNames, name)
		result.Removed++
	}

	return result
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

	if !s.warmupComplete && s.allBuiltinsRegisteredLocked() {
		s.warmupComplete = true
	}

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
