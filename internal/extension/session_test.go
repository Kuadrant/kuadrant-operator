//go:build unit

package extension

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr"
)

func gateOpen(store *SessionStore) bool {
	return store.isWarmupComplete()
}

func newTestSessionStore() *SessionStore {
	return NewSessionStore(logr.Discard())
}

func validCredential() []byte {
	return []byte("0123456789abcdef0123456789abcdef")
}

func TestSessionStore_MatchBuiltin(t *testing.T) {
	store := newTestSessionStore()
	cred := validCredential()
	store.SetCredential("test-extension", cred)

	name, ok := store.matchBuiltin(cred)
	if !ok {
		t.Fatal("expected credential to match a built-in")
	}
	if name != "test-extension" {
		t.Fatalf("expected built-in name %q, got %q", "test-extension", name)
	}
}

func TestSessionStore_MatchBuiltin_Unknown(t *testing.T) {
	store := newTestSessionStore()
	store.SetCredential("test-extension", validCredential())

	if _, ok := store.matchBuiltin([]byte("some-other-token")); ok {
		t.Fatal("expected no match for an unknown token")
	}
}

func TestSessionStore_CreateSession(t *testing.T) {
	store := newTestSessionStore()

	token, err := store.CreateSession("test-extension", "TestPolicy")
	if err != nil {
		t.Fatalf("expected successful session creation, got: %v", err)
	}
	if len(token) != 64 {
		t.Fatalf("expected 64-char hex token, got %d chars", len(token))
	}

	identity, ok := store.ValidateSession(token)
	if !ok {
		t.Fatal("expected session to be valid after creation")
	}
	if identity != "test-extension" {
		t.Fatalf("expected identity %q, got %q", "test-extension", identity)
	}
}

func TestSessionStore_CreateSession_PolicyKindRequired(t *testing.T) {
	store := newTestSessionStore()

	_, err := store.CreateSession("test-extension", "")
	if !errors.Is(err, ErrPolicyKindRequired) {
		t.Fatalf("expected ErrPolicyKindRequired, got: %v", err)
	}
}

func TestSessionStore_CreateSession_AlreadyConnected(t *testing.T) {
	store := newTestSessionStore()

	if _, err := store.CreateSession("test-extension", "TestPolicy"); err != nil {
		t.Fatalf("expected first session creation to succeed, got: %v", err)
	}

	_, err := store.CreateSession("test-extension", "AnotherPolicy")
	if !errors.Is(err, ErrAlreadyConnected) {
		t.Fatalf("expected ErrAlreadyConnected, got: %v", err)
	}
}

func TestSessionStore_CreateSession_PolicyKindTaken(t *testing.T) {
	store := newTestSessionStore()

	if _, err := store.CreateSession("extension-a", "SharedPolicy"); err != nil {
		t.Fatalf("expected first session creation to succeed, got: %v", err)
	}

	_, err := store.CreateSession("extension-b", "SharedPolicy")
	if !errors.Is(err, ErrPolicyKindTaken) {
		t.Fatalf("expected ErrPolicyKindTaken, got: %v", err)
	}
}

func TestSessionStore_CreateSession_PolicyKindReleasedOnRevoke(t *testing.T) {
	store := newTestSessionStore()

	if _, err := store.CreateSession("extension-a", "SharedPolicy"); err != nil {
		t.Fatalf("expected first session creation to succeed, got: %v", err)
	}

	store.RevokeByName("extension-a")

	if _, err := store.CreateSession("extension-b", "SharedPolicy"); err != nil {
		t.Fatalf("expected session creation to succeed after revocation, got: %v", err)
	}
}

func TestSessionStore_ValidateSession(t *testing.T) {
	store := newTestSessionStore()

	if _, ok := store.ValidateSession("nonexistent-token"); ok {
		t.Fatal("expected invalid session for nonexistent token")
	}

	token, _ := store.CreateSession("test-extension", "TestPolicy")

	identity, ok := store.ValidateSession(token)
	if !ok {
		t.Fatal("expected valid session")
	}
	if identity != "test-extension" {
		t.Fatalf("expected %q, got %q", "test-extension", identity)
	}
}

func TestSessionStore_RevokeByName(t *testing.T) {
	store := newTestSessionStore()

	token, _ := store.CreateSession("test-extension", "TestPolicy")

	if !store.RevokeByName("test-extension") {
		t.Fatal("expected revocation to return true")
	}

	if _, ok := store.ValidateSession(token); ok {
		t.Fatal("expected session to be invalid after revocation")
	}

	if store.RevokeByName("test-extension") {
		t.Fatal("expected second revocation to return false")
	}
}

func TestSessionStore_SetCredential_ChangedCredentialRevokesSession(t *testing.T) {
	store := newTestSessionStore()
	cred := validCredential()
	store.SetCredential("test-extension", cred)

	token, err := store.CreateSession("test-extension", "TestPolicy")
	if err != nil {
		t.Fatalf("expected session creation to succeed, got: %v", err)
	}

	newCred := []byte("abcdefghijklmnopqrstuvwxyz012345")
	store.SetCredential("test-extension", newCred)

	if _, ok := store.ValidateSession(token); ok {
		t.Fatal("expected old session to be revoked after credential change")
	}

	if _, ok := store.matchBuiltin(newCred); !ok {
		t.Fatal("expected new credential to match after change")
	}
}

func TestSessionStore_SetCredential_SameCredentialPreservesSession(t *testing.T) {
	store := newTestSessionStore()
	cred := validCredential()
	store.SetCredential("test-extension", cred)

	token, err := store.CreateSession("test-extension", "TestPolicy")
	if err != nil {
		t.Fatalf("expected session creation to succeed, got: %v", err)
	}

	sameCred := make([]byte, len(cred))
	copy(sameCred, cred)
	store.SetCredential("test-extension", sameCred)

	identity, ok := store.ValidateSession(token)
	if !ok {
		t.Fatal("expected session to remain valid when credential is unchanged")
	}
	if identity != "test-extension" {
		t.Fatalf("expected %q, got %q", "test-extension", identity)
	}
}

func TestSessionStore_ConcurrentAccess(t *testing.T) {
	store := newTestSessionStore()
	concurrency := 10

	var wg sync.WaitGroup
	tokens := make([]string, concurrency)

	for i := range concurrency {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			token, err := store.CreateSession(extensionName(idx), "Policy"+extensionName(idx))
			if err != nil {
				t.Errorf("concurrent session creation failed for %d: %v", idx, err)
				return
			}
			tokens[idx] = token
		}(i)
	}
	wg.Wait()

	for i := range concurrency {
		identity, ok := store.ValidateSession(tokens[i])
		if !ok {
			t.Errorf("expected valid session for extension %d", i)
			continue
		}
		if identity != extensionName(i) {
			t.Errorf("expected %q, got %q", extensionName(i), identity)
		}
	}

	for i := range concurrency {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			store.RevokeByName(extensionName(idx))
		}(i)
	}
	wg.Wait()

	for i := range concurrency {
		if _, ok := store.ValidateSession(tokens[i]); ok {
			t.Errorf("expected invalid session for extension %d after revocation", i)
		}
	}
}

func extensionName(i int) string {
	return "ext-" + string(rune('a'+i))
}

// fakeClock is a controllable time source for session-liveness tests.
type fakeClock struct {
	t time.Time
}

func (c *fakeClock) now() time.Time { return c.t }

func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newClockedSessionStore(ttl time.Duration) (*SessionStore, *fakeClock) {
	store := newTestSessionStore()
	clock := &fakeClock{t: time.Unix(0, 0)}
	store.now = clock.now
	store.SetSessionTTL(ttl)
	return store, clock
}

func TestSessionStore_Touch_KeepsSessionFresh(t *testing.T) {
	store, clock := newClockedSessionStore(45 * time.Second)

	token, err := store.CreateSession("test-extension", "TestPolicy")
	if err != nil {
		t.Fatalf("expected session creation to succeed, got: %v", err)
	}

	clock.advance(40 * time.Second)
	store.Touch(token)
	clock.advance(40 * time.Second)

	if store.isStaleLocked(store.sessions[token]) {
		t.Fatal("expected session to be fresh after Touch within TTL")
	}
}

func TestSessionStore_Touch_UnknownTokenIgnored(t *testing.T) {
	store, _ := newClockedSessionStore(45 * time.Second)

	store.Touch("nonexistent-token")

	if _, ok := store.sessions["nonexistent-token"]; ok {
		t.Fatal("expected Touch to ignore an unknown token")
	}
}

func TestSessionStore_ReapStale(t *testing.T) {
	store, clock := newClockedSessionStore(45 * time.Second)

	staleToken, _ := store.CreateSession("stale-extension", "StalePolicy")
	freshToken, _ := store.CreateSession("fresh-extension", "FreshPolicy")

	clock.advance(46 * time.Second)
	store.Touch(freshToken)

	revoked := store.ReapStale()

	if len(revoked) != 1 || revoked[0] != "stale-extension" {
		t.Fatalf("expected only stale-extension to be reaped, got: %v", revoked)
	}
	if _, ok := store.ValidateSession(staleToken); ok {
		t.Fatal("expected stale session to be revoked")
	}
	if _, ok := store.ValidateSession(freshToken); !ok {
		t.Fatal("expected fresh session to survive reaping")
	}
}

func TestSessionStore_CreateSession_SupersedesStaleSameIdentity(t *testing.T) {
	store, clock := newClockedSessionStore(45 * time.Second)

	oldToken, err := store.CreateSession("test-extension", "TestPolicy")
	if err != nil {
		t.Fatalf("expected first session creation to succeed, got: %v", err)
	}

	clock.advance(46 * time.Second)

	newToken, err := store.CreateSession("test-extension", "TestPolicy")
	if err != nil {
		t.Fatalf("expected handshake to supersede a stale session, got: %v", err)
	}
	if newToken == oldToken {
		t.Fatal("expected a fresh session token after superseding")
	}
	if _, ok := store.ValidateSession(oldToken); ok {
		t.Fatal("expected the stale session token to be invalid")
	}
}

func TestSessionStore_CreateSession_RejectsFreshSameIdentity(t *testing.T) {
	store, clock := newClockedSessionStore(45 * time.Second)

	if _, err := store.CreateSession("test-extension", "TestPolicy"); err != nil {
		t.Fatalf("expected first session creation to succeed, got: %v", err)
	}

	clock.advance(44 * time.Second)

	_, err := store.CreateSession("test-extension", "TestPolicy")
	if !errors.Is(err, ErrAlreadyConnected) {
		t.Fatalf("expected ErrAlreadyConnected for a fresh session, got: %v", err)
	}
}

func TestSessionStore_CreateSession_SupersedesStalePolicyKindOwner(t *testing.T) {
	store, clock := newClockedSessionStore(45 * time.Second)

	if _, err := store.CreateSession("extension-a", "SharedPolicy"); err != nil {
		t.Fatalf("expected first session creation to succeed, got: %v", err)
	}

	clock.advance(46 * time.Second)

	if _, err := store.CreateSession("extension-b", "SharedPolicy"); err != nil {
		t.Fatalf("expected a new owner to supersede a stale policy-kind claim, got: %v", err)
	}
}

func TestSessionStore_CreateSession_RejectsFreshPolicyKindOwner(t *testing.T) {
	store, clock := newClockedSessionStore(45 * time.Second)

	if _, err := store.CreateSession("extension-a", "SharedPolicy"); err != nil {
		t.Fatalf("expected first session creation to succeed, got: %v", err)
	}

	clock.advance(44 * time.Second)

	_, err := store.CreateSession("extension-b", "SharedPolicy")
	if !errors.Is(err, ErrPolicyKindTaken) {
		t.Fatalf("expected ErrPolicyKindTaken for a fresh owner, got: %v", err)
	}
}

func TestSessionStore_Warmup_OpenByDefault(t *testing.T) {
	store := newTestSessionStore()

	if !gateOpen(store) {
		t.Fatal("expected warmup gate to be open before BeginWarmup is called")
	}
}

func TestSessionStore_Warmup_NoBuiltinsOpensImmediately(t *testing.T) {
	store := newTestSessionStore()

	store.BeginWarmup(nil, time.Minute)

	if !gateOpen(store) {
		t.Fatal("expected warmup gate to be open when there are no built-ins to wait for")
	}
}

func TestSessionStore_Warmup_HeldUntilBuiltinRegisters(t *testing.T) {
	store := newTestSessionStore()
	store.SetCredential("builtin", validCredential())

	store.BeginWarmup([]string{"builtin"}, time.Minute)

	if gateOpen(store) {
		t.Fatal("expected warmup gate to be closed while built-in is unregistered")
	}

	if _, err := store.CreateSession("builtin", "BuiltinPolicy"); err != nil {
		t.Fatalf("expected built-in session creation to succeed, got: %v", err)
	}

	if !gateOpen(store) {
		t.Fatal("expected warmup gate to open once all built-ins registered")
	}
}

func TestSessionStore_Warmup_HeldUntilAllBuiltinsRegister(t *testing.T) {
	store := newTestSessionStore()
	store.SetCredential("builtin-a", validCredential())
	store.SetCredential("builtin-b", []byte("abcdefghijklmnopqrstuvwxyz012345"))

	store.BeginWarmup([]string{"builtin-a", "builtin-b"}, time.Minute)

	if _, err := store.CreateSession("builtin-a", "PolicyA"); err != nil {
		t.Fatalf("expected built-in-a session creation to succeed, got: %v", err)
	}
	if gateOpen(store) {
		t.Fatal("expected warmup gate to remain closed until every built-in registers")
	}

	if _, err := store.CreateSession("builtin-b", "PolicyB"); err != nil {
		t.Fatalf("expected built-in-b session creation to succeed, got: %v", err)
	}
	if !gateOpen(store) {
		t.Fatal("expected warmup gate to open once every built-in registered")
	}
}

func TestSessionStore_Warmup_TimeoutOpensGate(t *testing.T) {
	store := newTestSessionStore()
	store.SetCredential("builtin", validCredential())

	store.BeginWarmup([]string{"builtin"}, 10*time.Millisecond)

	if gateOpen(store) {
		t.Fatal("expected warmup gate to be closed immediately after BeginWarmup")
	}

	deadline := time.After(time.Second)
	for !gateOpen(store) {
		select {
		case <-deadline:
			t.Fatal("expected warmup gate to open after timeout elapsed")
		case <-time.After(time.Millisecond):
		}
	}
}
