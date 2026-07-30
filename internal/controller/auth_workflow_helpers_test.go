//go:build unit

package controllers

import (
	"encoding/json"
	"strings"
	"testing"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	"github.com/kuadrant/kuadrant-operator/internal/wasm"
)

func TestBuildWasmTypedActionsForAuth(t *testing.T) {
	pathID := "test-gateway#test-listener#test-route#rule-0"
	policy := EffectiveAuthPolicy{
		SourcePolicies: []string{"AuthPolicy/test-ns/test-policy"},
		Spec:           kuadrantv1.AuthPolicy{},
	}

	actions := buildWasmTypedActionsForAuth(pathID, policy)

	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}

	action := actions[0]

	if action.Type != "grpc" {
		t.Errorf("expected type 'grpc', got %q", action.Type)
	}
	if action.Service != wasm.AuthServiceName {
		t.Errorf("expected service %q, got %q", wasm.AuthServiceName, action.Service)
	}
	if action.Var != "auth_response" {
		t.Errorf("expected var 'auth_response', got %q", action.Var)
	}
	if action.Predicate != "true" {
		t.Errorf("expected predicate 'true' when no when-predicates, got %q", action.Predicate)
	}
	if action.Terminal {
		t.Error("expected terminal to be false")
	}

	expectedScope := AuthConfigNameForPath(pathID)
	if !strings.Contains(action.MessageBuilder, expectedScope) {
		t.Errorf("expected message builder to contain scope %q", expectedScope)
	}
	if !strings.Contains(action.MessageBuilder, "envoy.service.auth.v3.CheckRequest") {
		t.Error("expected message builder to contain CheckRequest type")
	}
	if !strings.Contains(action.MessageBuilder, "context_extensions") {
		t.Error("expected message builder to contain context_extensions")
	}

	if len(action.SourcePolicyLocators) != 1 || action.SourcePolicyLocators[0] != "AuthPolicy/test-ns/test-policy" {
		t.Errorf("unexpected source policy locators: %v", action.SourcePolicyLocators)
	}
}

func TestBuildWasmTypedActionsForAuth_WithPredicates(t *testing.T) {
	pathID := "test-gateway#test-listener#test-route#rule-0"
	policy := EffectiveAuthPolicy{
		SourcePolicies: []string{"AuthPolicy/test-ns/test-policy"},
		Spec: kuadrantv1.AuthPolicy{
			Spec: kuadrantv1.AuthPolicySpec{
				AuthPolicySpecProper: kuadrantv1.AuthPolicySpecProper{
					MergeableWhenPredicates: kuadrantv1.MergeableWhenPredicates{
						Predicates: kuadrantv1.WhenPredicates{
							{Predicate: "request.method == 'GET'"},
							{Predicate: "request.path.startsWith('/api')"},
						},
					},
				},
			},
		},
	}

	actions := buildWasmTypedActionsForAuth(pathID, policy)
	action := actions[0]

	if action.Predicate != "(request.method == 'GET') && (request.path.startsWith('/api'))" {
		t.Errorf("unexpected predicate: %q", action.Predicate)
	}
}

func TestBuildAuthOnReply(t *testing.T) {
	onReply := buildAuthOnReply("auth_response")

	if len(onReply) != 5 {
		t.Fatalf("expected 5 onReply actions, got %d", len(onReply))
	}

	// denied response → deny
	if onReply[0].Type != "deny" {
		t.Errorf("onReply[0]: expected type 'deny', got %q", onReply[0].Type)
	}
	if !onReply[0].Terminal {
		t.Error("onReply[0]: expected terminal")
	}
	if onReply[0].Predicate != "has(auth_response.denied_response)" {
		t.Errorf("onReply[0]: unexpected predicate: %q", onReply[0].Predicate)
	}
	if !strings.Contains(onReply[0].DenyWith, "403u") {
		t.Error("onReply[0]: expected DenyWith to contain 403u default")
	}

	// unsupported fields → fail
	if onReply[1].Type != "fail" {
		t.Errorf("onReply[1]: expected type 'fail', got %q", onReply[1].Type)
	}
	if !onReply[1].Terminal {
		t.Error("onReply[1]: expected terminal")
	}
	if onReply[1].LogMessage != "Unsupported field in OkHttpResponse" {
		t.Errorf("onReply[1]: unexpected log message: %q", onReply[1].LogMessage)
	}

	// dynamic metadata → store
	if onReply[2].Type != "store" {
		t.Errorf("onReply[2]: expected type 'store', got %q", onReply[2].Type)
	}
	if onReply[2].Terminal {
		t.Error("onReply[2]: expected non-terminal")
	}
	if onReply[2].Path != "auth" {
		t.Errorf("onReply[2]: expected path 'auth', got %q", onReply[2].Path)
	}
	if onReply[2].Value != "auth_response.dynamic_metadata" {
		t.Errorf("onReply[2]: unexpected value: %q", onReply[2].Value)
	}

	// ok response → headers
	if onReply[3].Type != "headers" {
		t.Errorf("onReply[3]: expected type 'headers', got %q", onReply[3].Type)
	}
	if onReply[3].Target != "request" {
		t.Errorf("onReply[3]: expected target 'request', got %q", onReply[3].Target)
	}

	// fallback → fail
	if onReply[4].Type != "fail" {
		t.Errorf("onReply[4]: expected type 'fail', got %q", onReply[4].Type)
	}
	if !onReply[4].Terminal {
		t.Error("onReply[4]: expected terminal")
	}
}

func TestJoinPredicates(t *testing.T) {
	tests := []struct {
		name       string
		predicates []string
		expected   string
	}{
		{"empty", nil, ""},
		{"single", []string{"a == b"}, "a == b"},
		{"multiple", []string{"a == b", "c == d"}, "(a == b) && (c == d)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := joinPredicates(tt.predicates)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestBuildAuthMessageBuilder(t *testing.T) {
	scope := "abc123hash"
	mb := buildAuthMessageBuilder(scope)

	if !strings.Contains(mb, `"host": "abc123hash"`) {
		t.Errorf("expected scope in context_extensions, got:\n%s", mb)
	}
	if !strings.Contains(mb, "envoy.service.auth.v3.CheckRequest") {
		t.Error("expected CheckRequest message type")
	}
	if !strings.Contains(mb, "request.time") {
		t.Error("expected request.time in message builder")
	}
	if !strings.Contains(mb, "destination.address") {
		t.Error("expected destination.address in message builder")
	}
	if !strings.Contains(mb, "source.address") {
		t.Error("expected source.address in message builder")
	}
	if !strings.Contains(mb, "envoy.config.core.v3.Metadata{}") {
		t.Error("expected empty metadata_context")
	}
}

func TestAuthTypedActionJSON(t *testing.T) {
	pathID := "test-gateway#test-listener#test-route#rule-0"
	policy := EffectiveAuthPolicy{
		SourcePolicies: []string{"AuthPolicy/test-ns/test-policy"},
		Spec:           kuadrantv1.AuthPolicy{},
	}

	actions := buildWasmTypedActionsForAuth(pathID, policy)
	action := actions[0]

	data, err := json.Marshal(action)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if parsed["type"] != "grpc" {
		t.Errorf("expected type 'grpc' in JSON, got %v", parsed["type"])
	}
	if parsed["service"] != wasm.AuthServiceName {
		t.Errorf("expected service %q in JSON, got %v", wasm.AuthServiceName, parsed["service"])
	}
	if parsed["var"] != "auth_response" {
		t.Errorf("expected var 'auth_response' in JSON, got %v", parsed["var"])
	}

	onReply, ok := parsed["onReply"].([]any)
	if !ok || len(onReply) != 5 {
		t.Fatalf("expected 5 onReply items in JSON, got %v", parsed["onReply"])
	}

	storeAction := onReply[2].(map[string]any)
	if storeAction["type"] != "store" {
		t.Errorf("expected onReply[2] type 'store', got %v", storeAction["type"])
	}
	if storeAction["path"] != "auth" {
		t.Errorf("expected store path 'auth', got %v", storeAction["path"])
	}
	if storeAction["value"] != "auth_response.dynamic_metadata" {
		t.Errorf("expected store value, got %v", storeAction["value"])
	}
}
