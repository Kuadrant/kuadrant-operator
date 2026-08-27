package extension

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	extensionTokenAudience = "kuadrant-extensions" //nolint:gosec

	registrationAPIGroup = "extensions.kuadrant.io"
	registrationResource = "policyregistrations"
	registrationVerb     = "register"
)

//+kubebuilder:rbac:groups=authentication.k8s.io,resources=tokenreviews,verbs=create
//+kubebuilder:rbac:groups=authorization.k8s.io,resources=subjectaccessreviews,verbs=create

// tokenAuthenticator validates standalone extension identities against the
// Kubernetes API using TokenReview (authentication) and SubjectAccessReview (authorization)
type tokenAuthenticator struct {
	client kubernetes.Interface
	logger logr.Logger
}

func newTokenAuthenticator(client kubernetes.Interface, logger logr.Logger) *tokenAuthenticator {
	return &tokenAuthenticator{client: client, logger: logger}
}

// reviewToken validates the presented token and returns the authenticated identity
func (a *tokenAuthenticator) reviewToken(ctx context.Context, token []byte) (authenticationv1.UserInfo, error) {
	review := &authenticationv1.TokenReview{
		Spec: authenticationv1.TokenReviewSpec{
			Token:     string(token),
			Audiences: []string{extensionTokenAudience},
		},
	}

	result, err := a.client.AuthenticationV1().TokenReviews().Create(ctx, review, metav1.CreateOptions{})
	if err != nil {
		return authenticationv1.UserInfo{}, fmt.Errorf("token review request failed: %w", err)
	}
	if result.Status.Error != "" {
		return authenticationv1.UserInfo{}, fmt.Errorf("token review error: %s", result.Status.Error)
	}
	if !result.Status.Authenticated {
		return authenticationv1.UserInfo{}, errors.New("token not authenticated")
	}
	if !audienceGranted(result.Status.Audiences) {
		return authenticationv1.UserInfo{}, fmt.Errorf("token audience %v does not include %q", result.Status.Audiences, extensionTokenAudience)
	}

	return result.Status.User, nil
}

// authorize checks whether the identity may register the given policy kind via
// a SubjectAccessReview against the virtual policyregistrations resource
func (a *tokenAuthenticator) authorize(ctx context.Context, user authenticationv1.UserInfo, policyKind string) (bool, error) {
	review := &authorizationv1.SubjectAccessReview{
		Spec: authorizationv1.SubjectAccessReviewSpec{
			User:   user.Username,
			UID:    user.UID,
			Groups: user.Groups,
			Extra:  convertExtra(user.Extra),
			ResourceAttributes: &authorizationv1.ResourceAttributes{
				Group:    registrationAPIGroup,
				Resource: registrationResource,
				Verb:     registrationVerb,
				Name:     policyKind,
			},
		},
	}

	result, err := a.client.AuthorizationV1().SubjectAccessReviews().Create(ctx, review, metav1.CreateOptions{})
	if err != nil {
		return false, fmt.Errorf("subject access review request failed: %w", err)
	}
	return result.Status.Allowed, nil
}

// convertExtra adapts the TokenReview extra attributes to the type expected by
// SubjectAccessReview
func convertExtra(extra map[string]authenticationv1.ExtraValue) map[string]authorizationv1.ExtraValue {
	if extra == nil {
		return nil
	}
	converted := make(map[string]authorizationv1.ExtraValue, len(extra))
	for key, value := range extra {
		converted[key] = authorizationv1.ExtraValue(value)
	}
	return converted
}

func audienceGranted(granted []string) bool {
	for _, audience := range granted {
		if audience == extensionTokenAudience {
			return true
		}
	}
	return false
}
