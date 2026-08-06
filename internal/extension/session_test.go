//go:build unit

package extension

import (
	"errors"
	"sync"
	"testing"

	"github.com/go-logr/logr"
)

func newTestSessionStore() *SessionStore {
	return NewSessionStore(logr.Discard())
}

func validCredential() []byte {
	return []byte("0123456789abcdef0123456789abcdef")
}

func TestSessionStore_SetCredential(t *testing.T) {
	store := newTestSessionStore()
	cred := validCredential()
	store.SetCredential("test-extension", cred)

	token, err := store.Authenticate("test-extension", cred, "TestPolicy")
	if err != nil {
		t.Fatalf("expected authentication to succeed after SetCredential, got: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty session token")
	}
}

func TestSessionStore_Authenticate(t *testing.T) {
	store := newTestSessionStore()
	cred := validCredential()
	store.SetCredential("test-extension", cred)

	token, err := store.Authenticate("test-extension", cred, "TestPolicy")
	if err != nil {
		t.Fatalf("expected successful authentication, got: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty session token")
	}
	if len(token) != 64 {
		t.Fatalf("expected 64-char hex token, got %d chars", len(token))
	}

	name, ok := store.ValidateSession(token)
	if !ok {
		t.Fatal("expected session to be valid after authentication")
	}
	if name != "test-extension" {
		t.Fatalf("expected extension name %q, got %q", "test-extension", name)
	}
}

func TestSessionStore_Authenticate_UnknownExtension(t *testing.T) {
	store := newTestSessionStore()

	_, err := store.Authenticate("unknown", validCredential(), "TestPolicy")
	if !errors.Is(err, ErrUnknownExtension) {
		t.Fatalf("expected ErrUnknownExtension, got: %v", err)
	}
}

func TestSessionStore_Authenticate_InvalidCredential(t *testing.T) {
	store := newTestSessionStore()
	store.SetCredential("test-extension", validCredential())

	_, err := store.Authenticate("test-extension", []byte("wrong-credential-that-is-long-enough-32"), "TestPolicy")
	if !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("expected ErrInvalidCredential, got: %v", err)
	}
}

func TestSessionStore_Authenticate_AlreadyConnected(t *testing.T) {
	store := newTestSessionStore()
	cred := validCredential()
	store.SetCredential("test-extension", cred)

	_, err := store.Authenticate("test-extension", cred, "TestPolicy")
	if err != nil {
		t.Fatalf("expected first authentication to succeed, got: %v", err)
	}

	_, err = store.Authenticate("test-extension", cred, "TestPolicy")
	if !errors.Is(err, ErrAlreadyConnected) {
		t.Fatalf("expected ErrAlreadyConnected, got: %v", err)
	}
}

func TestSessionStore_Authenticate_CredentialTooShort(t *testing.T) {
	store := newTestSessionStore()
	store.SetCredential("test-extension", validCredential())

	_, err := store.Authenticate("test-extension", []byte("short"), "TestPolicy")
	if !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("expected ErrInvalidCredential for short credential, got: %v", err)
	}
}

func TestSessionStore_Authenticate_PolicyKindRequired(t *testing.T) {
	store := newTestSessionStore()
	cred := validCredential()
	store.SetCredential("test-extension", cred)

	_, err := store.Authenticate("test-extension", cred, "")
	if !errors.Is(err, ErrPolicyKindRequired) {
		t.Fatalf("expected ErrPolicyKindRequired, got: %v", err)
	}
}

func TestSessionStore_Authenticate_PolicyKindTaken(t *testing.T) {
	store := newTestSessionStore()
	cred1 := validCredential()
	cred2 := []byte("abcdefghijklmnopqrstuvwxyz012345")
	store.SetCredential("extension-a", cred1)
	store.SetCredential("extension-b", cred2)

	_, err := store.Authenticate("extension-a", cred1, "SharedPolicy")
	if err != nil {
		t.Fatalf("expected first authentication to succeed, got: %v", err)
	}

	_, err = store.Authenticate("extension-b", cred2, "SharedPolicy")
	if err == nil {
		t.Fatal("expected error when policy kind is already taken")
	}
	if !errors.Is(err, ErrPolicyKindTaken) {
		t.Fatalf("expected ErrPolicyKindTaken, got: %v", err)
	}
}

func TestSessionStore_Authenticate_PolicyKindReleasedOnRevoke(t *testing.T) {
	store := newTestSessionStore()
	cred1 := validCredential()
	cred2 := []byte("abcdefghijklmnopqrstuvwxyz012345")
	store.SetCredential("extension-a", cred1)
	store.SetCredential("extension-b", cred2)

	_, err := store.Authenticate("extension-a", cred1, "SharedPolicy")
	if err != nil {
		t.Fatalf("expected first authentication to succeed, got: %v", err)
	}

	store.RevokeByName("extension-a")

	_, err = store.Authenticate("extension-b", cred2, "SharedPolicy")
	if err != nil {
		t.Fatalf("expected authentication to succeed after revocation, got: %v", err)
	}
}

func TestSessionStore_ValidateSession(t *testing.T) {
	store := newTestSessionStore()

	_, ok := store.ValidateSession("nonexistent-token")
	if ok {
		t.Fatal("expected invalid session for nonexistent token")
	}

	cred := validCredential()
	store.SetCredential("test-extension", cred)
	token, _ := store.Authenticate("test-extension", cred, "TestPolicy")

	name, ok := store.ValidateSession(token)
	if !ok {
		t.Fatal("expected valid session")
	}
	if name != "test-extension" {
		t.Fatalf("expected %q, got %q", "test-extension", name)
	}
}

func TestSessionStore_RevokeByName(t *testing.T) {
	store := newTestSessionStore()
	cred := validCredential()
	store.SetCredential("test-extension", cred)

	token, _ := store.Authenticate("test-extension", cred, "TestPolicy")

	revoked := store.RevokeByName("test-extension")
	if !revoked {
		t.Fatal("expected revocation to return true")
	}

	_, ok := store.ValidateSession(token)
	if ok {
		t.Fatal("expected session to be invalid after revocation")
	}

	revoked = store.RevokeByName("test-extension")
	if revoked {
		t.Fatal("expected second revocation to return false")
	}
}

func TestSessionStore_RemoveCredential(t *testing.T) {
	store := newTestSessionStore()
	cred := validCredential()
	store.SetCredential("test-extension", cred)

	token, _ := store.Authenticate("test-extension", cred, "TestPolicy")

	hadSession := store.RemoveCredential("test-extension")
	if !hadSession {
		t.Fatal("expected RemoveCredential to report revoked session")
	}

	_, ok := store.ValidateSession(token)
	if ok {
		t.Fatal("expected session to be invalid after credential removal")
	}

	_, err := store.Authenticate("test-extension", cred, "TestPolicy")
	if !errors.Is(err, ErrUnknownExtension) {
		t.Fatalf("expected ErrUnknownExtension after credential removal, got: %v", err)
	}
}

func TestSessionStore_SetCredential_ChangedCredentialRevokesSession(t *testing.T) {
	store := newTestSessionStore()
	cred := validCredential()
	store.SetCredential("test-extension", cred)

	token, err := store.Authenticate("test-extension", cred, "TestPolicy")
	if err != nil {
		t.Fatalf("expected authentication to succeed, got: %v", err)
	}

	newCred := []byte("abcdefghijklmnopqrstuvwxyz012345")
	store.SetCredential("test-extension", newCred)

	_, ok := store.ValidateSession(token)
	if ok {
		t.Fatal("expected old session to be revoked after credential change")
	}

	token2, err := store.Authenticate("test-extension", newCred, "TestPolicy")
	if err != nil {
		t.Fatalf("expected re-authentication with new credential to succeed, got: %v", err)
	}
	if token2 == "" {
		t.Fatal("expected non-empty session token")
	}
}

func TestSessionStore_SetCredential_SameCredentialPreservesSession(t *testing.T) {
	store := newTestSessionStore()
	cred := validCredential()
	store.SetCredential("test-extension", cred)

	token, err := store.Authenticate("test-extension", cred, "TestPolicy")
	if err != nil {
		t.Fatalf("expected authentication to succeed, got: %v", err)
	}

	sameCred := make([]byte, len(cred))
	copy(sameCred, cred)
	store.SetCredential("test-extension", sameCred)

	name, ok := store.ValidateSession(token)
	if !ok {
		t.Fatal("expected session to remain valid when credential is unchanged")
	}
	if name != "test-extension" {
		t.Fatalf("expected %q, got %q", "test-extension", name)
	}
}

func TestSessionStore_ConcurrentAccess(t *testing.T) {
	store := newTestSessionStore()
	concurrency := 10

	for i := range concurrency {
		cred := make([]byte, 32)
		for j := range cred {
			cred[j] = byte(i)
		}
		store.SetCredential(extensionName(i), cred)
	}

	var wg sync.WaitGroup
	tokens := make([]string, concurrency)

	for i := range concurrency {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			cred := make([]byte, 32)
			for j := range cred {
				cred[j] = byte(idx)
			}
			token, err := store.Authenticate(extensionName(idx), cred, "Policy"+extensionName(idx))
			if err != nil {
				t.Errorf("concurrent authenticate failed for %d: %v", idx, err)
				return
			}
			tokens[idx] = token
		}(i)
	}
	wg.Wait()

	for i := range concurrency {
		name, ok := store.ValidateSession(tokens[i])
		if !ok {
			t.Errorf("expected valid session for extension %d", i)
			continue
		}
		if name != extensionName(i) {
			t.Errorf("expected %q, got %q", extensionName(i), name)
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
		_, ok := store.ValidateSession(tokens[i])
		if ok {
			t.Errorf("expected invalid session for extension %d after revocation", i)
		}
	}
}

func extensionName(i int) string {
	return "ext-" + string(rune('a'+i))
}
