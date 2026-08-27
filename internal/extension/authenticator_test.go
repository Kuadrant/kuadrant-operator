//go:build unit

package extension

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

func newTestAuthenticator(client *fake.Clientset) *tokenAuthenticator {
	return newTokenAuthenticator(client, logr.Discard())
}

func TestReviewToken_Success(t *testing.T) {
	client := fake.NewSimpleClientset()
	client.PrependReactor("create", "tokenreviews", func(action clienttesting.Action) (bool, runtime.Object, error) {
		review := action.(clienttesting.CreateAction).GetObject().(*authenticationv1.TokenReview)
		review.Status = authenticationv1.TokenReviewStatus{
			Authenticated: true,
			Audiences:     []string{extensionTokenAudience},
			User: authenticationv1.UserInfo{
				Username: "system:serviceaccount:ns:standalone",
				UID:      "uid-123",
				Groups:   []string{"system:authenticated", "kuadrant:extensions"},
				Extra:    map[string]authenticationv1.ExtraValue{"scope": {"a", "b"}},
			},
		}
		return true, review, nil
	})

	user, err := newTestAuthenticator(client).reviewToken(context.Background(), []byte("token"))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if user.Username != "system:serviceaccount:ns:standalone" {
		t.Fatalf("unexpected username: %q", user.Username)
	}
	if user.UID != "uid-123" {
		t.Fatalf("unexpected uid: %q", user.UID)
	}
	if !reflect.DeepEqual(user.Groups, []string{"system:authenticated", "kuadrant:extensions"}) {
		t.Fatalf("unexpected groups: %v", user.Groups)
	}
	if !reflect.DeepEqual(user.Extra, map[string]authenticationv1.ExtraValue{"scope": {"a", "b"}}) {
		t.Fatalf("unexpected extra: %v", user.Extra)
	}
}

func TestReviewToken_SendsTokenAndAudience(t *testing.T) {
	var captured *authenticationv1.TokenReview
	client := fake.NewSimpleClientset()
	client.PrependReactor("create", "tokenreviews", func(action clienttesting.Action) (bool, runtime.Object, error) {
		captured = action.(clienttesting.CreateAction).GetObject().(*authenticationv1.TokenReview)
		captured.Status = authenticationv1.TokenReviewStatus{
			Authenticated: true,
			Audiences:     []string{extensionTokenAudience},
			User:          authenticationv1.UserInfo{Username: "identity"},
		}
		return true, captured, nil
	})

	if _, err := newTestAuthenticator(client).reviewToken(context.Background(), []byte("the-token")); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if captured.Spec.Token != "the-token" {
		t.Fatalf("expected token %q to be forwarded, got %q", "the-token", captured.Spec.Token)
	}
	if !reflect.DeepEqual(captured.Spec.Audiences, []string{extensionTokenAudience}) {
		t.Fatalf("expected audience %q to be requested, got %v", extensionTokenAudience, captured.Spec.Audiences)
	}
}

func TestReviewToken_APIError(t *testing.T) {
	client := fake.NewSimpleClientset()
	client.PrependReactor("create", "tokenreviews", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("connection refused")
	})

	_, err := newTestAuthenticator(client).reviewToken(context.Background(), []byte("token"))
	if err == nil || !strings.Contains(err.Error(), "token review request failed") {
		t.Fatalf("expected token review request failure, got: %v", err)
	}
}

func TestReviewToken_StatusError(t *testing.T) {
	client := fake.NewSimpleClientset()
	client.PrependReactor("create", "tokenreviews", func(action clienttesting.Action) (bool, runtime.Object, error) {
		review := action.(clienttesting.CreateAction).GetObject().(*authenticationv1.TokenReview)
		review.Status = authenticationv1.TokenReviewStatus{Error: "token expired"}
		return true, review, nil
	})

	_, err := newTestAuthenticator(client).reviewToken(context.Background(), []byte("token"))
	if err == nil || !strings.Contains(err.Error(), "token review error: token expired") {
		t.Fatalf("expected token review status error, got: %v", err)
	}
}

func TestReviewToken_NotAuthenticated(t *testing.T) {
	client := fake.NewSimpleClientset()
	client.PrependReactor("create", "tokenreviews", func(action clienttesting.Action) (bool, runtime.Object, error) {
		review := action.(clienttesting.CreateAction).GetObject().(*authenticationv1.TokenReview)
		review.Status = authenticationv1.TokenReviewStatus{Authenticated: false}
		return true, review, nil
	})

	_, err := newTestAuthenticator(client).reviewToken(context.Background(), []byte("token"))
	if err == nil || !strings.Contains(err.Error(), "token not authenticated") {
		t.Fatalf("expected not-authenticated error, got: %v", err)
	}
}

func TestReviewToken_AudienceMismatch(t *testing.T) {
	client := fake.NewSimpleClientset()
	client.PrependReactor("create", "tokenreviews", func(action clienttesting.Action) (bool, runtime.Object, error) {
		review := action.(clienttesting.CreateAction).GetObject().(*authenticationv1.TokenReview)
		review.Status = authenticationv1.TokenReviewStatus{
			Authenticated: true,
			Audiences:     []string{"some-other-audience"},
			User:          authenticationv1.UserInfo{Username: "identity"},
		}
		return true, review, nil
	})

	_, err := newTestAuthenticator(client).reviewToken(context.Background(), []byte("token"))
	if err == nil || !strings.Contains(err.Error(), "does not include") {
		t.Fatalf("expected audience mismatch error, got: %v", err)
	}
}

func TestAuthorize_ForwardsFullIdentity(t *testing.T) {
	var captured *authorizationv1.SubjectAccessReview
	client := fake.NewSimpleClientset()
	client.PrependReactor("create", "subjectaccessreviews", func(action clienttesting.Action) (bool, runtime.Object, error) {
		captured = action.(clienttesting.CreateAction).GetObject().(*authorizationv1.SubjectAccessReview)
		captured.Status.Allowed = true
		return true, captured, nil
	})

	user := authenticationv1.UserInfo{
		Username: "system:serviceaccount:ns:standalone",
		UID:      "uid-123",
		Groups:   []string{"system:authenticated", "kuadrant:extensions"},
		Extra:    map[string]authenticationv1.ExtraValue{"scope": {"a", "b"}},
	}

	allowed, err := newTestAuthenticator(client).authorize(context.Background(), user, "ThreatPolicy")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !allowed {
		t.Fatal("expected authorization to be allowed")
	}

	spec := captured.Spec
	if spec.User != user.Username {
		t.Fatalf("expected user %q, got %q", user.Username, spec.User)
	}
	if spec.UID != user.UID {
		t.Fatalf("expected uid %q, got %q", user.UID, spec.UID)
	}
	if !reflect.DeepEqual(spec.Groups, user.Groups) {
		t.Fatalf("expected groups %v, got %v", user.Groups, spec.Groups)
	}
	wantExtra := map[string]authorizationv1.ExtraValue{"scope": {"a", "b"}}
	if !reflect.DeepEqual(spec.Extra, wantExtra) {
		t.Fatalf("expected extra %v, got %v", wantExtra, spec.Extra)
	}

	attrs := spec.ResourceAttributes
	if attrs == nil {
		t.Fatal("expected resource attributes to be set")
	}
	if attrs.Group != registrationAPIGroup || attrs.Resource != registrationResource ||
		attrs.Verb != registrationVerb || attrs.Name != "ThreatPolicy" {
		t.Fatalf("unexpected resource attributes: %+v", attrs)
	}
}

func TestAuthorize_Denied(t *testing.T) {
	client := fake.NewSimpleClientset()
	client.PrependReactor("create", "subjectaccessreviews", func(action clienttesting.Action) (bool, runtime.Object, error) {
		review := action.(clienttesting.CreateAction).GetObject().(*authorizationv1.SubjectAccessReview)
		review.Status.Allowed = false
		return true, review, nil
	})

	allowed, err := newTestAuthenticator(client).authorize(context.Background(), authenticationv1.UserInfo{Username: "identity"}, "ThreatPolicy")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if allowed {
		t.Fatal("expected authorization to be denied")
	}
}

func TestAuthorize_APIError(t *testing.T) {
	client := fake.NewSimpleClientset()
	client.PrependReactor("create", "subjectaccessreviews", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("connection refused")
	})

	_, err := newTestAuthenticator(client).authorize(context.Background(), authenticationv1.UserInfo{Username: "identity"}, "ThreatPolicy")
	if err == nil || !strings.Contains(err.Error(), "subject access review request failed") {
		t.Fatalf("expected subject access review failure, got: %v", err)
	}
}

func TestConvertExtra_Nil(t *testing.T) {
	if got := convertExtra(nil); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestConvertExtra_PreservesEntries(t *testing.T) {
	in := map[string]authenticationv1.ExtraValue{
		"scope":     {"read", "write"},
		"namespace": {"ns"},
	}
	want := map[string]authorizationv1.ExtraValue{
		"scope":     {"read", "write"},
		"namespace": {"ns"},
	}
	if got := convertExtra(in); !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}
