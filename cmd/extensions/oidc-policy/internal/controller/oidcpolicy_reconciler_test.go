//go:build unit

package controller

import (
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/google/cel-go/cel"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// evalClaimPredicate compiles the predicate produced by claimPredicate and
// evaluates it against a JWT identity, mirroring what authorino does at runtime.
func evalClaimPredicate(t *testing.T, k, v string, identity map[string]interface{}) bool {
	t.Helper()
	env, err := cel.NewEnv(cel.Variable("auth", cel.DynType))
	if err != nil {
		t.Fatalf("cel.NewEnv failed: %v", err)
	}
	ast, iss := env.Compile(claimPredicate(k, v))
	if iss != nil && iss.Err() != nil {
		t.Fatalf("compile failed for predicate %q: %v", claimPredicate(k, v), iss.Err())
	}
	prg, err := env.Program(ast)
	if err != nil {
		t.Fatalf("program failed: %v", err)
	}
	out, _, err := prg.Eval(map[string]interface{}{
		"auth": map[string]interface{}{"identity": identity},
	})
	if err != nil {
		t.Fatalf("eval failed: %v", err)
	}
	got, ok := out.Value().(bool)
	if !ok {
		t.Fatalf("predicate did not return a bool, got %T", out.Value())
	}
	return got
}

func TestClaimPredicate(t *testing.T) {
	cases := []struct {
		name     string
		claim    string
		value    string
		identity map[string]interface{}
		want     bool
	}{
		{
			name:     "scalar string match",
			claim:    "email",
			value:    "user@example.com",
			identity: map[string]interface{}{"email": "user@example.com"},
			want:     true,
		},
		{
			name:     "scalar string mismatch",
			claim:    "email",
			value:    "user@example.com",
			identity: map[string]interface{}{"email": "other@example.com"},
			want:     false,
		},
		{
			name:     "scalar boolean-as-string match",
			claim:    "email_verified",
			value:    "true",
			identity: map[string]interface{}{"email_verified": "true"},
			want:     true,
		},
		{
			name:     "list claim contains value",
			claim:    "groups",
			value:    "admin",
			identity: map[string]interface{}{"groups": []interface{}{"dev", "admin"}},
			want:     true,
		},
		{
			name:     "list claim missing value",
			claim:    "groups",
			value:    "admin",
			identity: map[string]interface{}{"groups": []interface{}{"dev", "ops"}},
			want:     false,
		},
		{
			name:     "claim absent from identity",
			claim:    "email",
			value:    "user@example.com",
			identity: map[string]interface{}{"sub": "1234567890"},
			want:     false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := evalClaimPredicate(t, tc.claim, tc.value, tc.identity); got != tc.want {
				t.Errorf("claimPredicate(%q, %q) over %v = %v, want %v", tc.claim, tc.value, tc.identity, got, tc.want)
			}
		})
	}
}

func TestBuildOpaAuthorizationRule(t *testing.T) {
	baseURL, err := url.Parse("https://gateway.example.com:8443")
	if err != nil {
		t.Fatal(err)
	}
	igwURL, err := url.Parse("https://gateway.example.com:8443")
	if err != nil {
		t.Fatal(err)
	}
	authorizeURL := "https://issuer.com/authorize?client_id=test"

	rule := buildOpaAuthorizationRule(baseURL, igwURL, authorizeURL)
	fmt.Println(rule)

	// Verify the rule contains the correct cookie parser that handles JWT tokens
	if !strings.Contains(rule, "eq_idx := indexof(trimmed, \"=\")") {
		t.Error("OPA rule should use indexof to find first = character")
	}
	if !strings.Contains(rule, "substring(trimmed, 0, eq_idx)") {
		t.Error("OPA rule should use substring to extract cookie name")
	}
	if !strings.Contains(rule, "substring(trimmed, eq_idx + 1, -1)") {
		t.Error("OPA rule should use substring to extract cookie value")
	}

	// Verify the rule does NOT use the broken split/count pattern
	if strings.Contains(rule, "count(kv) == 2") {
		t.Error("OPA rule should not use count check that breaks with = characters in values")
	}

	// Verify URLs are correctly embedded
	if !strings.Contains(rule, igwURL.String()) {
		t.Errorf("OPA rule should contain gateway URL: %s", igwURL.String())
	}
	if !strings.Contains(rule, authorizeURL) {
		t.Errorf("OPA rule should contain authorize URL: %s", authorizeURL)
	}
}

func TestBuildOpaAuthorizationRule_CookieParserPattern(t *testing.T) {
	baseURL, _ := url.Parse("http://example.com")
	igwURL, _ := url.Parse("http://example.com")
	authorizeURL := "http://issuer.com/auth"

	rule := buildOpaAuthorizationRule(baseURL, igwURL, authorizeURL)

	// The cookie parser should handle JWT tokens with = padding
	// Example JWT: eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIn0.Signature==
	// Cookie: jwt=eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIn0.Signature==

	// The pattern should:
	// 1. Find the index of first = character
	expectedPatterns := []string{
		"trimmed := trim(part, \" \")",       // trim the cookie part
		"eq_idx := indexof(trimmed, \"=\")",  // find first =
		"eq_idx != -1",                       // ensure = was found
		"substring(trimmed, 0, eq_idx)",      // extract name (before =)
		"substring(trimmed, eq_idx + 1, -1)", // extract value (after =, including any additional =)
	}

	for _, pattern := range expectedPatterns {
		if !strings.Contains(rule, pattern) {
			t.Errorf("OPA rule missing expected pattern: %s", pattern)
		}
	}

	// Verify the location logic is present
	expectedLocationLogic := []string{
		"location := concat",
		"cookies.target",
		"input.auth.metadata.token.id_token",
		"allow = true",
	}

	for _, logic := range expectedLocationLogic {
		if !strings.Contains(rule, logic) {
			t.Errorf("OPA rule missing expected logic: %s", logic)
		}
	}
}

func TestBuildOpaAuthorizationRule_JWTScenarios(t *testing.T) {
	tests := []struct {
		name        string
		description string
		jwtExample  string
	}{
		{
			name:        "JWT with single = padding",
			description: "JWT token ending with single = character",
			jwtExample:  "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.signature=",
		},
		{
			name:        "JWT with double = padding",
			description: "JWT token ending with double == characters",
			jwtExample:  "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIn0.sig==",
		},
		{
			name:        "JWT with no padding",
			description: "JWT token with no = padding",
			jwtExample:  "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.signature",
		},
	}

	baseURL, _ := url.Parse("http://example.com")
	igwURL, _ := url.Parse("http://example.com")
	authorizeURL := "http://issuer.com/auth"
	rule := buildOpaAuthorizationRule(baseURL, igwURL, authorizeURL)

	// Document that the cookie parser pattern can handle all these scenarios
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The rule uses substring(trimmed, eq_idx + 1, -1) which extracts everything after the first =
			// This correctly handles JWTs with any number of = characters
			if !strings.Contains(rule, "substring(trimmed, eq_idx + 1, -1)") {
				t.Errorf("Cookie parser should handle %s: %s", tt.description, tt.jwtExample)
			}
		})
	}
}

func TestBuildTargetCookieExpression(t *testing.T) {
	tests := []struct {
		name     string
		hostname string
		protocol gatewayapiv1.ProtocolType
		want     []string
	}{
		{
			name:     "HTTP protocol",
			hostname: "example.com",
			protocol: gatewayapiv1.HTTPProtocolType,
			want: []string{
				`"target=" + request.path`,
				`request.query != ""`,
				`"?" + request.query`,
				`domain=example.com`,
				`HttpOnly`,
				`SameSite=Lax`,
				`Path=/`,
				`Max-Age=3600`,
			},
		},
		{
			name:     "HTTPS protocol with Secure flag",
			hostname: "secure.example.com",
			protocol: gatewayapiv1.HTTPSProtocolType,
			want: []string{
				`"target=" + request.path`,
				`request.query != ""`,
				`"?" + request.query`,
				`domain=secure.example.com`,
				`HttpOnly`,
				`Secure`,
				`SameSite=Lax`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildTargetCookieExpression(tt.hostname, tt.protocol)

			for _, expected := range tt.want {
				if !strings.Contains(result, expected) {
					t.Errorf("buildTargetCookieExpression() missing expected pattern: %s\nGot: %s", expected, result)
				}
			}
		})
	}
}

func TestBuildTargetCookieExpression_QueryStringHandling(t *testing.T) {
	hostname := "example.com"
	protocol := gatewayapiv1.HTTPProtocolType

	expression := buildTargetCookieExpression(hostname, protocol)

	// Verify the expression includes query string handling
	requiredPatterns := []string{
		// CEL ternary operator to conditionally add query string
		`request.query != ""`,
		`"?" + request.query`,
		// The pattern should be: path + (query != "" ? "?" + query : "")
		`request.path + (request.query != "" ? "?" + request.query : "")`,
	}

	for _, pattern := range requiredPatterns {
		if !strings.Contains(expression, pattern) {
			t.Errorf("Expression missing query string handling pattern: %s", pattern)
		}
	}

	// Verify it does NOT use the broken pattern that only stores the path
	if strings.Contains(expression, `"target=" + request.path + "; domain=`) {
		t.Error("Expression should not directly concatenate path with cookie attributes (missing query string logic)")
	}
}

func TestBuildTargetCookieExpression_Examples(t *testing.T) {
	expression := buildTargetCookieExpression("example.com", gatewayapiv1.HTTPSProtocolType)

	// Document the expected behavior with examples
	examples := []struct {
		scenario    string
		requestPath string
		query       string
		expected    string
	}{
		{
			scenario:    "Path with query parameters",
			requestPath: "/dashboard",
			query:       "elicitation_id=123&user=456",
			expected:    "/dashboard?elicitation_id=123&user=456",
		},
		{
			scenario:    "Path without query parameters",
			requestPath: "/home",
			query:       "",
			expected:    "/home",
		},
		{
			scenario:    "Path with complex query string",
			requestPath: "/api/v1/resource",
			query:       "filter=active&sort=desc&limit=50",
			expected:    "/api/v1/resource?filter=active&sort=desc&limit=50",
		},
	}

	for _, ex := range examples {
		t.Run(ex.scenario, func(t *testing.T) {
			// The CEL expression uses a ternary: request.path + (request.query != "" ? "?" + request.query : "")
			// This should construct the full path with query when query is present
			if !strings.Contains(expression, `request.path + (request.query != "" ? "?" + request.query : "")`) {
				t.Errorf("Expression should handle scenario: %s\nExpected to preserve: %s", ex.scenario, ex.expected)
			}
		})
	}
}

func TestIngressGatewayInfo_GetURL(t *testing.T) {
	tests := []struct {
		name            string
		hostname        string
		protocol        gatewayapiv1.ProtocolType
		port            int32
		expectedScheme  string
		expectedHost    string
		expectedFullURL string
	}{
		{
			name:            "HTTP standard port 80",
			hostname:        "example.com",
			protocol:        gatewayapiv1.HTTPProtocolType,
			port:            80,
			expectedScheme:  "http",
			expectedHost:    "example.com",
			expectedFullURL: "http://example.com",
		},
		{
			name:            "HTTPS standard port 443",
			hostname:        "secure.example.com",
			protocol:        gatewayapiv1.HTTPSProtocolType,
			port:            443,
			expectedScheme:  "https",
			expectedHost:    "secure.example.com",
			expectedFullURL: "https://secure.example.com",
		},
		{
			name:            "HTTP non-standard port 8080",
			hostname:        "example.com",
			protocol:        gatewayapiv1.HTTPProtocolType,
			port:            8080,
			expectedScheme:  "http",
			expectedHost:    "example.com:8080",
			expectedFullURL: "http://example.com:8080",
		},
		{
			name:            "HTTP non-standard port 8001",
			hostname:        "example.com",
			protocol:        gatewayapiv1.HTTPProtocolType,
			port:            8001,
			expectedScheme:  "http",
			expectedHost:    "example.com:8001",
			expectedFullURL: "http://example.com:8001",
		},
		{
			name:            "HTTPS non-standard port 8443",
			hostname:        "secure.example.com",
			protocol:        gatewayapiv1.HTTPSProtocolType,
			port:            8443,
			expectedScheme:  "https",
			expectedHost:    "secure.example.com:8443",
			expectedFullURL: "https://secure.example.com:8443",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			igw := &ingressGatewayInfo{
				Hostname:  tt.hostname,
				Protocol:  tt.protocol,
				Port:      tt.port,
				Name:      "test-gateway",
				Namespace: "default",
			}

			url := igw.GetURL()

			if url.Scheme != tt.expectedScheme {
				t.Errorf("GetURL() scheme = %v, want %v", url.Scheme, tt.expectedScheme)
			}
			if url.Host != tt.expectedHost {
				t.Errorf("GetURL() host = %v, want %v", url.Host, tt.expectedHost)
			}
			if url.String() != tt.expectedFullURL {
				t.Errorf("GetURL() = %v, want %v", url.String(), tt.expectedFullURL)
			}
		})
	}
}

func TestIngressGatewayInfo_GetURL_CachesResult(t *testing.T) {
	igw := &ingressGatewayInfo{
		Hostname:  "example.com",
		Protocol:  gatewayapiv1.HTTPProtocolType,
		Port:      8080,
		Name:      "test-gateway",
		Namespace: "default",
	}

	// Call GetURL multiple times
	url1 := igw.GetURL()
	url2 := igw.GetURL()

	// Should return the same cached instance
	if url1 != url2 {
		t.Error("GetURL() should cache and return the same URL instance")
	}

	// Verify the URL is correct
	expectedURL := "http://example.com:8080"
	if url1.String() != expectedURL {
		t.Errorf("GetURL() = %v, want %v", url1.String(), expectedURL)
	}
}

func TestIngressGatewayInfo_GetURL_PortInCookieDomain(t *testing.T) {
	// Test that demonstrates Bug 1 is fixed: port is preserved in URL construction
	igw := &ingressGatewayInfo{
		Hostname:  "example.com",
		Protocol:  gatewayapiv1.HTTPProtocolType,
		Port:      8001,
		Name:      "test-gateway",
		Namespace: "default",
	}

	url := igw.GetURL()

	// The URL should include the port
	if url.Host != "example.com:8001" {
		t.Errorf("Expected Host to include port: got %v, want example.com:8001", url.Host)
	}

	// When this URL is used for redirect URI construction, the port will be preserved
	redirectURI := url.String() + "/auth/callback"
	expectedRedirectURI := "http://example.com:8001/auth/callback"
	if redirectURI != expectedRedirectURI {
		t.Errorf("Redirect URI = %v, want %v", redirectURI, expectedRedirectURI)
	}

	// Cookie domain uses igw.Hostname (without port), which is correct
	cookieExpr := buildTargetCookieExpression(igw.Hostname, igw.Protocol)
	if !strings.Contains(cookieExpr, "domain=example.com") {
		t.Error("Cookie domain should use hostname without port")
	}
}

func TestBuildOpaAuthorizationRule_UsesCorrectBaseURL(t *testing.T) {
	tests := []struct {
		name              string
		baseURL           string
		igwURL            string
		authorizeURL      string
		expectedInRule    []string
		notExpectedInRule []string
		description       string
	}{
		{
			name:         "No custom redirectURI - both URLs are the same",
			baseURL:      "http://gateway.example.com:8001",
			igwURL:       "http://gateway.example.com:8001",
			authorizeURL: "https://issuer.com/authorize",
			expectedInRule: []string{
				"http://gateway.example.com:8001",
				"https://issuer.com/authorize",
			},
			description: "When no custom redirectURI is set, baseURL and igwURL are identical",
		},
		{
			name:         "Custom redirect URI - baseURL differs from igwURL",
			baseURL:      "https://public.example.com:8443",
			igwURL:       "http://gateway.internal:8080",
			authorizeURL: "https://issuer.com/authorize",
			expectedInRule: []string{
				// Location 1: with cookies.target uses baseURL
				`concat("", ["https://public.example.com:8443", cookies.target])`,
				// Location 2: without cookies.target uses igwURL
				`location := "http://gateway.internal:8080"`,
				// Location 3: no auth uses authorizeURL
				`location := "https://issuer.com/authorize"`,
			},
			description: "When custom redirectURI is set, location 1 uses baseURL, location 2 uses igwURL",
		},
		{
			name:         "Custom redirect URI without port",
			baseURL:      "https://public.example.com",
			igwURL:       "http://gateway.example.com:8001",
			authorizeURL: "https://issuer.com/authorize?client_id=test",
			expectedInRule: []string{
				`concat("", ["https://public.example.com", cookies.target])`,
				`location := "http://gateway.example.com:8001"`,
				`location := "https://issuer.com/authorize?client_id=test"`,
			},
			description: "Handles custom redirectURI with standard port, igwURL with non-standard port",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseURL, err := url.Parse(tt.baseURL)
			if err != nil {
				t.Fatal(err)
			}
			igwURL, err := url.Parse(tt.igwURL)
			if err != nil {
				t.Fatal(err)
			}

			rule := buildOpaAuthorizationRule(baseURL, igwURL, tt.authorizeURL)

			// Verify expected strings are in the rule
			for _, expected := range tt.expectedInRule {
				if !strings.Contains(rule, expected) {
					t.Errorf("OPA rule missing expected pattern: %s\nDescription: %s\nRule: %s",
						expected, tt.description, rule)
				}
			}

			// Verify unexpected strings are NOT in the rule
			for _, notExpected := range tt.notExpectedInRule {
				if strings.Contains(rule, notExpected) {
					t.Errorf("OPA rule should not contain: %s\nDescription: %s\nRule: %s",
						notExpected, tt.description, rule)
				}
			}
		})
	}
}

func TestBuildOpaAuthorizationRule_LocationRedirects(t *testing.T) {
	baseURL, _ := url.Parse("https://public.example.com:8443")
	igwURL, _ := url.Parse("http://gateway.internal:8080")
	authorizeURL := "https://issuer.com/authorize?client_id=test"

	rule := buildOpaAuthorizationRule(baseURL, igwURL, authorizeURL)

	// The rule should have three location assignments:
	// 1. Successful auth with target cookie: concat baseURL with cookies.target (uses custom redirectURI base)
	// 2. Successful auth without target cookie: redirect to igwURL (uses gateway URL as default)
	// 3. Failed auth: redirect to authorizeURL

	expectedPatterns := []string{
		// Pattern 1: successful auth with target - uses baseURL
		`location := concat("", ["https://public.example.com:8443", cookies.target])`,
		`input.auth.metadata.token.id_token`,
		`cookies.target`,

		// Pattern 2: successful auth without target - uses igwURL
		`location := "http://gateway.internal:8080"`,
		`not cookies.target`,

		// Pattern 3: failed auth
		`location := "https://issuer.com/authorize?client_id=test"`,
		`not input.auth.metadata.token.id_token`,

		// Allow statement
		`allow = true`,
	}

	for _, pattern := range expectedPatterns {
		if !strings.Contains(rule, pattern) {
			t.Errorf("OPA rule missing expected pattern: %s", pattern)
		}
	}
}

func TestBuildOpaAuthorizationRule_CustomRedirectURI_Scenario(t *testing.T) {
	// Real-world scenario: LoadBalancer exposes gateway on public URL,
	// but internal gateway uses different host/port
	baseURL, _ := url.Parse("https://app.example.com")     // Custom redirectURI base
	igwURL, _ := url.Parse("http://gateway.internal:8080") // Internal gateway URL
	authorizeURL := "https://auth.example.com/authorize"

	rule := buildOpaAuthorizationRule(baseURL, igwURL, authorizeURL)

	// Scenario 1: User tried to access /dashboard?tab=settings
	// After auth, they should be redirected to: https://app.example.com/dashboard?tab=settings
	if !strings.Contains(rule, `concat("", ["https://app.example.com", cookies.target])`) {
		t.Error("Location 1 should use custom baseURL for user's intended destination")
	}

	// Scenario 2: User accessed callback directly (no target cookie)
	// They should be redirected to the internal gateway URL as default: http://gateway.internal:8080
	if !strings.Contains(rule, `location := "http://gateway.internal:8080" { input.auth.metadata.token.id_token; not cookies.target }`) {
		t.Error("Location 2 should use igwURL as default when no target cookie exists")
	}

	// Scenario 3: Auth failed (no token)
	// Redirect to authorize URL for re-authentication
	if !strings.Contains(rule, `location := "https://auth.example.com/authorize" { not input.auth.metadata.token.id_token }`) {
		t.Error("Location 3 should redirect to authorize URL when auth fails")
	}
}
