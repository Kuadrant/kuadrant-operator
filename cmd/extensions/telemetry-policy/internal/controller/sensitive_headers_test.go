//go:build unit

package controller

import (
	"strings"
	"testing"
)

func TestCheckSensitiveHeaders(t *testing.T) {
	const (
		sensitiveAuthorization = `expression reads sensitive request header "authorization"`
		sensitiveCookie        = `expression reads sensitive request header "cookie"`
		sensitiveXAPIKey       = `expression reads sensitive request header "x-api-key"`
		sensitiveAPIKey        = `expression reads sensitive request header "apikey"`
		parseError             = "invalid CEL expression"
	)

	tests := []struct {
		name       string
		expression string
		wantErr    string // substring of the expected error; empty means the expression is accepted
	}{
		// accepted
		{name: "request path", expression: "request.path"},
		{name: "request method", expression: "request.method"},
		{name: "request attribute by index", expression: `request["path"]`},
		{name: "auth attribute", expression: "auth.identity.user.username"},
		{name: "non-sensitive header by selection", expression: "request.headers.host"},
		{name: "non-sensitive header by index", expression: `request.headers["content-type"]`},
		{name: "sensitive name as prefix only", expression: "request.headers.authorization_scheme"},
		{name: "presence test of sensitive header", expression: "has(request.headers.authorization)"},
		{name: "presence test in ternary", expression: `has(request.headers.authorization) ? "bearer" : "anonymous"`},
		{name: "presence test of headers map", expression: "has(request.headers)"},
		{name: "shim-only function", expression: "requestBodyJSON('/model')"},
		{name: "shim-only function with list", expression: "responseBodyJSON(['/usage/total_tokens'])"},
		{name: "string literal", expression: `"static"`},
		{name: "compound without headers", expression: `request.method + " " + request.path`},
		{name: "other map variable named headers", expression: "metadata.headers.authorization"},

		// rejected: sensitive header by name
		{name: "authorization by selection", expression: "request.headers.authorization", wantErr: sensitiveAuthorization},
		{name: "authorization mixed case", expression: "request.headers.Authorization", wantErr: sensitiveAuthorization},
		{name: "authorization by index", expression: `request.headers["authorization"]`, wantErr: sensitiveAuthorization},
		{name: "cookie by selection", expression: "request.headers.cookie", wantErr: sensitiveCookie},
		{name: "x-api-key by index mixed case", expression: `request.headers["X-API-Key"]`, wantErr: sensitiveXAPIKey},
		{name: "apikey by selection", expression: "request.headers.apikey", wantErr: sensitiveAPIKey},
		{name: "headers by index then selection", expression: `request["headers"].authorization`, wantErr: sensitiveAuthorization},
		{name: "headers by index then index", expression: `request["headers"]["cookie"]`, wantErr: sensitiveCookie},
		{name: "sensitive header inside a call", expression: `request.headers.authorization.startsWith("Bearer ")`, wantErr: sensitiveAuthorization},
		{name: "sensitive header inside string conversion", expression: "string(request.headers.authorization)", wantErr: sensitiveAuthorization},
		{name: "presence test guarding a value read", expression: `has(request.headers.authorization) ? request.headers.authorization : "absent"`, wantErr: sensitiveAuthorization},
		{name: "sensitive header inside a list", expression: "[request.path, request.headers.cookie]", wantErr: sensitiveCookie},
		{name: "sensitive header inside a map", expression: `{"k": request.headers.apikey}`, wantErr: sensitiveAPIKey},
		{name: "sensitive header as index key", expression: "request.headers[request.headers.authorization]", wantErr: errNonLiteralHeader.Error()},

		// rejected: headers map as a whole
		{name: "bare headers", expression: "request.headers", wantErr: errWholeHeaders.Error()},
		{name: "headers string conversion", expression: "string(request.headers)", wantErr: errWholeHeaders.Error()},
		{name: "headers size", expression: "request.headers.size()", wantErr: errWholeHeaders.Error()},
		{name: "headers map macro", expression: "request.headers.map(k, v, v)", wantErr: errWholeHeaders.Error()},
		{name: "headers exists macro", expression: `request.headers.exists(k, k == "authorization")`, wantErr: errWholeHeaders.Error()},
		{name: "headers in a list", expression: "[request.headers]", wantErr: errWholeHeaders.Error()},
		{name: "headers as index key", expression: "metadata[request.headers]", wantErr: errWholeHeaders.Error()},

		// rejected: non-literal header key
		{name: "header key from attribute", expression: "request.headers[request.path]", wantErr: errNonLiteralHeader.Error()},
		{name: "header key from concatenation", expression: `request.headers["auth" + "orization"]`, wantErr: errNonLiteralHeader.Error()},

		// rejected: request as a whole
		{name: "bare request", expression: "request", wantErr: errWholeRequest.Error()},
		{name: "request string conversion", expression: "string(request)", wantErr: errWholeRequest.Error()},
		{name: "request with non-literal key", expression: "request[request.path]", wantErr: errWholeRequest.Error()},
		{name: "request in a list", expression: "[request]", wantErr: errWholeRequest.Error()},

		// rejected: does not parse
		{name: "trailing dot", expression: "request.", wantErr: parseError},
		{name: "presence test with index", expression: `has(request.headers["authorization"])`, wantErr: parseError},
		{name: "optional field syntax", expression: "request.headers.?authorization", wantErr: parseError},
		{name: "empty expression", expression: "", wantErr: parseError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkSensitiveHeaders(tt.expression)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("checkSensitiveHeaders(%q) = %v, want nil", tt.expression, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("checkSensitiveHeaders(%q) = nil, want error containing %q", tt.expression, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("checkSensitiveHeaders(%q) = %q, want error containing %q", tt.expression, err, tt.wantErr)
			}
		})
	}
}
