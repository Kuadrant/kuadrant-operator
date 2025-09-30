//go:build unit

package v1alpha1

import (
	"net/url"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

func TestGetIssuerTokenExchangeURL(t *testing.T) {
	policy := mockMinimalOIDCPolicy()

	actual, err := policy.GetIssuerTokenExchangeURL()
	if err != nil {
		t.Fatal(err)
	}
	expected := "https://issuer.com/oauth/token"

	if strings.Compare(actual, expected) != 0 {
		t.Errorf("incorrect issuer token URL, actual = %v, expected %v", actual, expected)
	}

	policy.Spec.Provider.TokenEndpoint = "https://example.com/token?foo=bar"

	actual, err = policy.GetIssuerTokenExchangeURL()
	if err != nil {
		t.Fatal(err)
	}
	expected = "https://example.com/token?foo=bar"
	if strings.Compare(actual, expected) != 0 {
		t.Errorf("incorrect issuer token URL, actual = %v, expected %v", actual, expected)
	}
}

func TestGetIssuerTokenExchangeBodyCelExpression(t *testing.T) {
	policy := mockMinimalOIDCPolicy()
	gwURL, err := url.Parse("https://gateway.example.com")
	if err != nil {
		t.Fatal(err)
	}

	celExpression, err := policy.GetIssuerTokenExchangeBodyCelExpression(gwURL, nil)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(celExpression, `"code=" + request.query.split("&").map(entry, entry.split("=")).filter(pair, pair[0] == "code").map(pair, pair[1])[0] + `) {
		t.Errorf("incorrect token exchange body cel expression %s", celExpression)
	}

	if !strings.Contains(celExpression, "&client_id=client123") {
		t.Errorf("incorrect token exchange body cel expression %s", celExpression)
	}
	if !strings.Contains(celExpression, "&grant_type=authorization_code") {
		t.Errorf("incorrect token exchange body cel expression %s", celExpression)
	}
	if !strings.Contains(celExpression, "&redirect_uri=https%3A%2F%2Fgateway.example.com%2Fauth%2Fcallback") {
		t.Errorf("incorrect token exchange body cel expression %s", celExpression)
	}

	celExpression, err = policy.GetIssuerTokenExchangeBodyCelExpression(gwURL, map[string]string{"client_secret": "secret123"})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(celExpression, "&client_secret=secret123") {
		t.Errorf("incorrect token exchange body cel expression %s", celExpression)
	}

}

func TestGetAuthorizeURL(t *testing.T) {
	policy := mockMinimalOIDCPolicy()

	baseURL, err := url.Parse("https://gateway.example.com")
	if err != nil {
		t.Fatal(err)
	}

	actual, err := policy.GetAuthorizeURL(baseURL)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(actual, "client_id=client123") {
		t.Errorf("missing client_id parameter")
	}
	if !strings.Contains(actual, "redirect_uri=https%3A%2F%2Fgateway.example.com%2Fauth%2Fcallback") {
		t.Errorf("incorrect redirect_uri parameter")
	}
	if !strings.Contains(actual, "response_type=code") {
		t.Errorf("missing response_type parameter")
	}
	if !strings.Contains(actual, "scope=openid") {
		t.Errorf("missing scope parameter")
	}

	policy.Spec.Provider.AuthorizationEndpoint = "https://customidp.com/authorize?custom=true&client_id=client321&redirect_uri=https%3A%2F%2Fgateway.example.com%2Fauth%2Fcustomcallback"
	actual, err = policy.GetAuthorizeURL(baseURL)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(actual, "client_id=client321") {
		t.Errorf("incorrect client_id parameter")
	}
	if !strings.Contains(actual, "redirect_uri=https%3A%2F%2Fgateway.example.com%2Fauth%2Fcustomcallback") {
		t.Errorf("incorrect redirect_uri parameter")
	}
	if !strings.Contains(actual, "response_type=code") {
		t.Errorf("missing response_type parameter")
	}
	if !strings.Contains(actual, "scope=openid") {
		t.Errorf("missing scope parameter")
	}
	if !strings.Contains(actual, "custom=true") {
		t.Errorf("missing custom parameter")
	}
}

func TestGetTargetRefs(t *testing.T) {
	policy := mockMinimalOIDCPolicy()

	targetRefs := policy.GetTargetRefs()
	if len(targetRefs) != 1 {
		t.Errorf("GetTargetRefs() returned %d references, expected 1", len(targetRefs))
	}
	if targetRefs[0].LocalPolicyTargetReference.Group != "gateway.networking.k8s.io" {
		t.Errorf("incorrect group: actual %q, expected %q", targetRefs[0].LocalPolicyTargetReference.Group, "gateway.networking.k8s.io")
	}
}

func TestOIDCPolicyStatus_Equals(t *testing.T) {
	var (
		conditions = []metav1.Condition{
			{
				Type: StatusConditionReady,
			},
		}
		status = &OIDCPolicyStatus{
			ObservedGeneration: 0,
			Conditions:         conditions,
		}
	)
	s := OIDCPolicyStatus{ObservedGeneration: int64(1)}
	if s.Equals(status, logr.Logger{}) {
		t.Errorf("observed generation should be different")
	}

	s = OIDCPolicyStatus{ObservedGeneration: status.ObservedGeneration}
	if s.Equals(status, logr.Logger{}) {
		t.Errorf("conditions should be different")
	}

	s = OIDCPolicyStatus{ObservedGeneration: status.ObservedGeneration, Conditions: status.Conditions}
	if !s.Equals(status, logr.Logger{}) {
		t.Errorf("status should be the same")
	}
}

func mockMinimalOIDCPolicy() *OIDCPolicy {
	return &OIDCPolicy{
		TypeMeta:   metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{},
		Spec: OIDCPolicySpec{
			TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
				LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
					Group: "gateway.networking.k8s.io",
					Kind:  "HTTPRoute",
					Name:  "test-route",
				},
			},
			OIDCPolicySpecProper: OIDCPolicySpecProper{
				Provider: &Provider{
					IssuerURL: "https://issuer.com",
					ClientID:  "client123",
				},
			},
		},
	}
}
