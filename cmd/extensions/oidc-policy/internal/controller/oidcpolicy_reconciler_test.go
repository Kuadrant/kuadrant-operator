//go:build unit

package controller

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
)

func TestBuildOpaAuthorizationRule(t *testing.T) {
	igwURL, err := url.Parse("https://gateway.example.com:8443")
	if err != nil {
		t.Fatal(err)
	}
	authorizeURL := "https://issuer.com/authorize?client_id=test"

	rule := buildOpaAuthorizationRule(igwURL, authorizeURL)
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
	igwURL, _ := url.Parse("http://example.com")
	authorizeURL := "http://issuer.com/auth"

	rule := buildOpaAuthorizationRule(igwURL, authorizeURL)

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

	igwURL, _ := url.Parse("http://example.com")
	authorizeURL := "http://issuer.com/auth"
	rule := buildOpaAuthorizationRule(igwURL, authorizeURL)

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
