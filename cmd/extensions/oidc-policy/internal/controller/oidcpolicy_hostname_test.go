//go:build unit

package controller

import (
	"errors"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kuadrant/kuadrant-operator/cmd/extensions/oidc-policy/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
	extcontroller "github.com/kuadrant/kuadrant-operator/pkg/extension/controller"
)

// Tests for https://github.com/Kuadrant/kuadrant-operator/issues/1504:
// the OIDCPolicy must never put a wildcard (or empty) listener hostname into
// cookie domains, redirect URLs or the callback route.

func hostnameTestPolicy(redirectURI string) *v1alpha1.OIDCPolicy {
	return &v1alpha1.OIDCPolicy{
		TypeMeta:   metav1.TypeMeta{Kind: "OIDCPolicy", APIVersion: v1alpha1.GroupVersion.String()},
		ObjectMeta: metav1.ObjectMeta{Name: "baker-auth", Namespace: "bakery"},
		Spec: v1alpha1.OIDCPolicySpec{
			OIDCPolicySpecProper: v1alpha1.OIDCPolicySpecProper{
				Provider: &v1alpha1.Provider{
					IssuerURL:   "https://idp.example.com/realms/demo",
					ClientID:    "oidc-demo",
					RedirectURI: redirectURI,
				},
			},
		},
	}
}

func hostnameTestGateway(hostname string, protocol gatewayapiv1.ProtocolType, port int32) *ingressGatewayInfo {
	return &ingressGatewayInfo{
		Hostname:  hostname,
		Name:      "ingressgateway",
		Namespace: "api-gateway",
		Protocol:  protocol,
		Port:      port,
	}
}

func mainSetCookieExpression(t *testing.T, pol *v1alpha1.OIDCPolicy, igw *ingressGatewayInfo) string {
	t.Helper()
	ap, err := buildMainAuthPolicy(pol, igw)
	if err != nil {
		t.Fatalf("buildMainAuthPolicy returned an unexpected error: %v", err)
	}
	return string(ap.Spec.Overrides.AuthScheme.Response.Unauthenticated.Headers["set-cookie"].Expression)
}

func callbackSetCookieAndRule(t *testing.T, pol *v1alpha1.OIDCPolicy, igw *ingressGatewayInfo) (string, string) {
	t.Helper()
	ap, err := buildCallbackAuthPolicy(pol, igw)
	if err != nil {
		t.Fatalf("buildCallbackAuthPolicy returned an unexpected error: %v", err)
	}
	cookie := string(ap.Spec.Overrides.AuthScheme.Response.Unauthorized.Headers["set-cookie"].Expression)
	rule := ap.Spec.Overrides.AuthScheme.Authorization["location"].Opa.Rego
	return cookie, rule
}

func TestCookieDomain_UsesExactHost(t *testing.T) {
	tests := []struct {
		name         string
		listenerHost string
		redirectURI  string
		wantDomain   string
	}{
		{
			name:         "exact listener hostname, no redirectURI keeps today's behaviour",
			listenerHost: "bakery.example.com",
			wantDomain:   "domain=bakery.example.com;",
		},
		{
			name:         "wildcard listener hostname uses the redirectURI host",
			listenerHost: "*.example.com",
			redirectURI:  "https://bakery.example.com/auth/callback",
			wantDomain:   "domain=bakery.example.com;",
		},
		{
			name:         "empty listener hostname uses the redirectURI host",
			listenerHost: "",
			redirectURI:  "https://bakery.example.com/auth/callback",
			wantDomain:   "domain=bakery.example.com;",
		},
		{
			name:         "redirectURI port is not part of the cookie domain",
			listenerHost: "*.example.com",
			redirectURI:  "https://bakery.example.com:8443/auth/callback",
			wantDomain:   "domain=bakery.example.com;",
		},
		{
			name:         "exact listener hostname is kept when redirectURI uses another host",
			listenerHost: "gateway.internal",
			redirectURI:  "https://public.example.com/auth/callback",
			wantDomain:   "domain=gateway.internal;",
		},
		{
			name:         "redirectURI host is lowercased",
			listenerHost: "*.example.com",
			redirectURI:  "https://Bakery.Example.COM/auth/callback",
			wantDomain:   "domain=bakery.example.com;",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pol := hostnameTestPolicy(tt.redirectURI)
			igw := hostnameTestGateway(tt.listenerHost, gatewayapiv1.HTTPSProtocolType, 443)

			targetCookie := mainSetCookieExpression(t, pol, igw)
			jwtCookie, _ := callbackSetCookieAndRule(t, pol, igw)

			for name, expr := range map[string]string{"target cookie": targetCookie, "jwt cookie": jwtCookie} {
				if !strings.Contains(expr, tt.wantDomain) {
					t.Errorf("%s: want %q in\n%s", name, tt.wantDomain, expr)
				}
				if strings.Contains(expr, "*") {
					t.Errorf("%s: must not contain a wildcard domain:\n%s", name, expr)
				}
				if strings.Contains(expr, "domain=;") {
					t.Errorf("%s: must not contain an empty domain:\n%s", name, expr)
				}
			}
		})
	}
}

func TestCallbackRule_FallbackLocationIsNeverAWildcard(t *testing.T) {
	pol := hostnameTestPolicy("https://bakery.example.com/auth/callback")
	igw := hostnameTestGateway("*.example.com", gatewayapiv1.HTTPSProtocolType, 443)

	_, rule := callbackSetCookieAndRule(t, pol, igw)

	if strings.Contains(rule, "*.example.com") {
		t.Errorf("OPA rule redirects to a wildcard host:\n%s", rule)
	}
	want := `location := "https://bakery.example.com" { input.auth.metadata.token.id_token; not cookies.target }`
	if !strings.Contains(rule, want) {
		t.Errorf("want fallback %q in OPA rule:\n%s", want, rule)
	}
}

func TestCallbackRule_ExactListenerKeepsGatewayFallback(t *testing.T) {
	// The fallback to the gateway URL is on purpose when the listener host is exact.
	// See TestBuildOpaAuthorizationRule_CustomRedirectURI_Scenario.
	pol := hostnameTestPolicy("https://public.example.com/auth/callback")
	igw := hostnameTestGateway("gateway.internal", gatewayapiv1.HTTPProtocolType, 8080)

	_, rule := callbackSetCookieAndRule(t, pol, igw)

	want := `location := "http://gateway.internal:8080" { input.auth.metadata.token.id_token; not cookies.target }`
	if !strings.Contains(rule, want) {
		t.Errorf("want fallback %q in OPA rule:\n%s", want, rule)
	}
}

func TestCallbackHTTPRoute_Hostnames(t *testing.T) {
	tests := []struct {
		name          string
		listenerHost  string
		redirectURI   string
		wantHostnames []gatewayapiv1.Hostname
	}{
		{
			name:          "wildcard listener gets the redirectURI host so it lands next to the app route",
			listenerHost:  "*.example.com",
			redirectURI:   "https://bakery.example.com/auth/callback",
			wantHostnames: []gatewayapiv1.Hostname{"bakery.example.com"},
		},
		{
			name:          "empty listener gets the redirectURI host",
			listenerHost:  "",
			redirectURI:   "https://bakery.example.com/auth/callback",
			wantHostnames: []gatewayapiv1.Hostname{"bakery.example.com"},
		},
		{
			name:          "redirectURI host is lowercased",
			listenerHost:  "*.example.com",
			redirectURI:   "https://Bakery.Example.COM/auth/callback",
			wantHostnames: []gatewayapiv1.Hostname{"bakery.example.com"},
		},
		{
			name:         "exact listener keeps inheriting the listener hostname",
			listenerHost: "bakery.example.com",
		},
		{
			name:         "exact listener with a different public redirectURI is left alone",
			listenerHost: "gateway.internal",
			redirectURI:  "https://public.example.com/auth/callback",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			route := buildCallbackHTTPRoute(hostnameTestPolicy(tt.redirectURI), hostnameTestGateway(tt.listenerHost, gatewayapiv1.HTTPSProtocolType, 443))
			got := route.Spec.Hostnames
			if len(got) != len(tt.wantHostnames) {
				t.Fatalf("hostnames: got %v, want %v", got, tt.wantHostnames)
			}
			for i := range got {
				if got[i] != tt.wantHostnames[i] {
					t.Errorf("hostnames[%d]: got %q, want %q", i, got[i], tt.wantHostnames[i])
				}
			}
		})
	}
}

func TestNoExactHost_PolicyIsInvalid(t *testing.T) {
	tests := []struct {
		name         string
		listenerHost string
		redirectURI  string
	}{
		{name: "wildcard listener without redirectURI", listenerHost: "*.example.com"},
		{name: "empty listener without redirectURI", listenerHost: ""},
		{name: "wildcard redirectURI", listenerHost: "*.example.com", redirectURI: "https://*.example.com/auth/callback"},
		{name: "trailing dot redirectURI host", listenerHost: "", redirectURI: "https://bakery.example.com./auth/callback"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pol := hostnameTestPolicy(tt.redirectURI)
			igw := hostnameTestGateway(tt.listenerHost, gatewayapiv1.HTTPSProtocolType, 443)

			_, mainErr := buildMainAuthPolicy(pol, igw)
			_, callbackErr := buildCallbackAuthPolicy(pol, igw)

			for name, err := range map[string]error{"main AuthPolicy": mainErr, "callback AuthPolicy": callbackErr} {
				if err == nil {
					t.Errorf("%s: want an error, got nil", name)
					continue
				}
				var invalid kuadrant.ErrInvalid
				if !errors.As(err, &invalid) {
					t.Errorf("%s: want a kuadrant.ErrInvalid so the status reason is Invalid, got %T: %v", name, err, err)
				}
				if !strings.Contains(err.Error(), "redirectURI") {
					t.Errorf("%s: error should tell the user to set redirectURI, got: %v", name, err)
				}
			}

			if mainErr != nil {
				cond := extcontroller.AcceptedCondition(pol, mainErr)
				if cond.Status != metav1.ConditionFalse || cond.Reason != "Invalid" {
					t.Errorf("Accepted condition: got status=%s reason=%s, want False/Invalid", cond.Status, cond.Reason)
				}
			}
		})
	}
}
