//go:build unit

/*
Copyright 2025 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	"testing"
)

func TestActionType_EnumValues(t *testing.T) {
	tests := []struct {
		name     string
		value    ActionType
		wantName string
	}{
		{"unspecified", ActionType_ACTION_TYPE_UNSPECIFIED, "ACTION_TYPE_UNSPECIFIED"},
		{"grpc_method", ActionType_ACTION_TYPE_GRPC_METHOD, "ACTION_TYPE_GRPC_METHOD"},
		{"deny", ActionType_ACTION_TYPE_DENY, "ACTION_TYPE_DENY"},
		{"add_headers", ActionType_ACTION_TYPE_ADD_HEADERS, "ACTION_TYPE_ADD_HEADERS"},
		{"failure", ActionType_ACTION_TYPE_FAILURE, "ACTION_TYPE_FAILURE"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value.String() != tt.wantName {
				t.Errorf("ActionType(%d).String() = %q, want %q", tt.value, tt.value.String(), tt.wantName)
			}
		})
	}
}

func TestActionEntry_FieldAccessors(t *testing.T) {
	entry := &ActionEntry{
		ActionType: ActionType_ACTION_TYPE_GRPC_METHOD,
		Predicate:  "request.headers['check'] == '1'",
		Phase:      "request",
		Method:     "checkThreatLevel",
		Var:        "threatResponse",
	}

	if entry.GetActionType() != ActionType_ACTION_TYPE_GRPC_METHOD {
		t.Errorf("GetActionType() = %v, want %v", entry.GetActionType(), ActionType_ACTION_TYPE_GRPC_METHOD)
	}
	if entry.GetPredicate() != "request.headers['check'] == '1'" {
		t.Errorf("GetPredicate() = %q, unexpected", entry.GetPredicate())
	}
	if entry.GetPhase() != "request" {
		t.Errorf("GetPhase() = %q, want %q", entry.GetPhase(), "request")
	}
	if entry.GetMethod() != "checkThreatLevel" {
		t.Errorf("GetMethod() = %q, unexpected", entry.GetMethod())
	}
	if entry.GetVar() != "threatResponse" {
		t.Errorf("GetVar() = %q, unexpected", entry.GetVar())
	}

	denyEntry := &ActionEntry{
		ActionType: ActionType_ACTION_TYPE_DENY,
		Predicate:  "threatResponse.threat_level >= 5",
		Phase:      "response",
		DenyWith:   "403",
	}
	if denyEntry.GetDenyWith() != "403" {
		t.Errorf("GetDenyWith() = %q, want %q", denyEntry.GetDenyWith(), "403")
	}
	if denyEntry.GetPhase() != "response" {
		t.Errorf("GetPhase() = %q, want %q", denyEntry.GetPhase(), "response")
	}

	headersEntry := &ActionEntry{
		ActionType:   ActionType_ACTION_TYPE_ADD_HEADERS,
		HeadersToAdd: `{"x-threat-checked": "true"}`,
		Phase:        "response",
	}
	if headersEntry.GetHeadersToAdd() != `{"x-threat-checked": "true"}` {
		t.Errorf("GetHeadersToAdd() = %q, unexpected", headersEntry.GetHeadersToAdd())
	}

	failureEntry := &ActionEntry{
		ActionType:     ActionType_ACTION_TYPE_FAILURE,
		Predicate:      `request.headers["x-debug"] == "true"`,
		Phase:          "request",
		FailureMessage: "Request blocked by threat policy",
		FailureCode:    "500",
	}
	if failureEntry.GetFailureMessage() != "Request blocked by threat policy" {
		t.Errorf("GetFailureMessage() = %q, unexpected", failureEntry.GetFailureMessage())
	}
	if failureEntry.GetFailureCode() != "500" {
		t.Errorf("GetFailureCode() = %q, want %q", failureEntry.GetFailureCode(), "500")
	}
}

func TestActionEntry_NilSafeGetters(t *testing.T) {
	var entry *ActionEntry

	if entry.GetActionType() != ActionType_ACTION_TYPE_UNSPECIFIED {
		t.Errorf("GetActionType() on nil should return UNSPECIFIED")
	}
	if entry.GetPredicate() != "" {
		t.Errorf("GetPredicate() on nil should return empty string")
	}
	if entry.GetPhase() != "" {
		t.Errorf("GetPhase() on nil should return empty string")
	}
	if entry.GetMethod() != "" {
		t.Errorf("GetMethod() on nil should return empty string")
	}
	if entry.GetVar() != "" {
		t.Errorf("GetVar() on nil should return empty string")
	}
	if entry.GetDenyWith() != "" {
		t.Errorf("GetDenyWith() on nil should return empty string")
	}
	if entry.GetHeadersToAdd() != "" {
		t.Errorf("GetHeadersToAdd() on nil should return empty string")
	}
	if entry.GetFailureMessage() != "" {
		t.Errorf("GetFailureMessage() on nil should return empty string")
	}
	if entry.GetFailureCode() != "" {
		t.Errorf("GetFailureCode() on nil should return empty string")
	}
}

func TestPipelineCommitRequest_FieldAccessors(t *testing.T) {
	policy := &Policy{
		Metadata: &Metadata{
			Kind:      "ThreatPolicy",
			Namespace: "default",
			Name:      "my-policy",
		},
	}
	actions := []*ActionEntry{
		{
			ActionType: ActionType_ACTION_TYPE_DENY,
			Predicate:  `request.url_path == "/blocked"`,
			Phase:      "request",
			DenyWith:   "403",
		},
		{
			ActionType: ActionType_ACTION_TYPE_GRPC_METHOD,
			Phase:      "request",
			Method:     "checkThreatLevel",
			Var:        "threatResponse",
		},
		{
			ActionType: ActionType_ACTION_TYPE_DENY,
			Predicate:  "threatResponse.threat_level >= 5",
			Phase:      "response",
			DenyWith:   "403",
		},
		{
			ActionType:   ActionType_ACTION_TYPE_ADD_HEADERS,
			Phase:        "response",
			HeadersToAdd: `{"x-threat-checked": "true"}`,
		},
	}

	req := &PipelineCommitRequest{
		Policy:  policy,
		Actions: actions,
	}

	if req.GetPolicy() != policy {
		t.Errorf("GetPolicy() returned unexpected value")
	}
	if len(req.GetActions()) != 4 {
		t.Fatalf("GetActions() length = %d, want 4", len(req.GetActions()))
	}
	if req.GetActions()[0].GetDenyWith() != "403" {
		t.Errorf("first action DenyWith = %q, want %q", req.GetActions()[0].GetDenyWith(), "403")
	}
	if req.GetActions()[1].GetMethod() != "checkThreatLevel" {
		t.Errorf("second action Method = %q, want %q", req.GetActions()[1].GetMethod(), "checkThreatLevel")
	}
	if req.GetActions()[2].GetPhase() != "response" {
		t.Errorf("third action Phase = %q, want %q", req.GetActions()[2].GetPhase(), "response")
	}
	if req.GetActions()[3].GetHeadersToAdd() != `{"x-threat-checked": "true"}` {
		t.Errorf("fourth action HeadersToAdd = %q, unexpected", req.GetActions()[3].GetHeadersToAdd())
	}
}

func TestPipelineCommitRequest_NilSafeGetters(t *testing.T) {
	var req *PipelineCommitRequest

	if req.GetPolicy() != nil {
		t.Errorf("GetPolicy() on nil should return nil")
	}
	if req.GetActions() != nil {
		t.Errorf("GetActions() on nil should return nil")
	}
}

func TestPipelineCommit_FullMethodName(t *testing.T) {
	expected := "/kuadrant.v1.ExtensionService/PipelineCommit"
	if ExtensionService_PipelineCommit_FullMethodName != expected {
		t.Errorf("FullMethodName = %q, want %q", ExtensionService_PipelineCommit_FullMethodName, expected)
	}
}
