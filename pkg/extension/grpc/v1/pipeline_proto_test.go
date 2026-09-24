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

func TestPhase_EnumValues(t *testing.T) {
	tests := []struct {
		name     string
		value    Phase
		wantName string
	}{
		{"unspecified", Phase_PHASE_UNSPECIFIED, "PHASE_UNSPECIFIED"},
		{"request", Phase_PHASE_REQUEST, "PHASE_REQUEST"},
		{"response", Phase_PHASE_RESPONSE, "PHASE_RESPONSE"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value.String() != tt.wantName {
				t.Errorf("Phase(%d).String() = %q, want %q", tt.value, tt.value.String(), tt.wantName)
			}
		})
	}
}

func TestActionEntry_FieldAccessors(t *testing.T) {
	entry := &ActionEntry{
		Phase:     Phase_PHASE_REQUEST,
		Predicate: "request.headers['check'] == '1'",
		Action: &ActionEntry_Grpc{Grpc: &GrpcAction{
			Method: "checkThreatLevel",
			Var:    "threatResponse",
		}},
	}

	if entry.GetPhase() != Phase_PHASE_REQUEST {
		t.Errorf("GetPhase() = %v, want %v", entry.GetPhase(), Phase_PHASE_REQUEST)
	}
	if entry.GetPredicate() != "request.headers['check'] == '1'" {
		t.Errorf("GetPredicate() = %q, unexpected", entry.GetPredicate())
	}
	if entry.GetGrpc() == nil {
		t.Fatal("Expected Grpc action to be set")
	}
	if entry.GetGrpc().GetMethod() != "checkThreatLevel" {
		t.Errorf("GetMethod() = %q, unexpected", entry.GetGrpc().GetMethod())
	}
	if entry.GetGrpc().GetVar() != "threatResponse" {
		t.Errorf("GetVar() = %q, unexpected", entry.GetGrpc().GetVar())
	}

	denyEntry := &ActionEntry{
		Phase:     Phase_PHASE_RESPONSE,
		Predicate: "threatResponse.threat_level >= 5",
		Action: &ActionEntry_Deny{Deny: &DenyAction{
			WithStatus:  403,
			WithHeaders: `[["x-threat-assessed", "true"]]`,
			WithBody:    "Blocked by threat policy",
		}},
	}
	if denyEntry.GetPhase() != Phase_PHASE_RESPONSE {
		t.Errorf("GetPhase() = %v, want %v", denyEntry.GetPhase(), Phase_PHASE_RESPONSE)
	}
	if denyEntry.GetDeny() == nil {
		t.Fatal("Expected Deny action to be set")
	}
	if denyEntry.GetDeny().GetWithStatus() != 403 {
		t.Errorf("GetWithStatus() = %d, want %d", denyEntry.GetDeny().GetWithStatus(), 403)
	}
	if denyEntry.GetDeny().GetWithHeaders() != `[["x-threat-assessed", "true"]]` {
		t.Errorf("GetWithHeaders() = %q, unexpected", denyEntry.GetDeny().GetWithHeaders())
	}
	if denyEntry.GetDeny().GetWithBody() != "Blocked by threat policy" {
		t.Errorf("GetWithBody() = %q, unexpected", denyEntry.GetDeny().GetWithBody())
	}

	headersEntry := &ActionEntry{
		Phase: Phase_PHASE_RESPONSE,
		Action: &ActionEntry_AddHeaders{AddHeaders: &AddHeadersAction{
			HeadersToAdd: `{"x-threat-checked": "true"}`,
		}},
	}
	if headersEntry.GetAddHeaders() == nil {
		t.Fatal("Expected AddHeaders action to be set")
	}
	if headersEntry.GetAddHeaders().GetHeadersToAdd() != `{"x-threat-checked": "true"}` {
		t.Errorf("GetHeadersToAdd() = %q, unexpected", headersEntry.GetAddHeaders().GetHeadersToAdd())
	}

	failEntry := &ActionEntry{
		Phase:     Phase_PHASE_RESPONSE,
		Predicate: `threatResponse.error_code != 0`,
		Action: &ActionEntry_Fail{Fail: &FailAction{
			LogMessage: "Threat service returned unexpected error",
		}},
	}
	if failEntry.GetFail() == nil {
		t.Fatal("Expected Fail action to be set")
	}
	if failEntry.GetFail().GetLogMessage() != "Threat service returned unexpected error" {
		t.Errorf("GetLogMessage() = %q, unexpected", failEntry.GetFail().GetLogMessage())
	}
}

func TestActionEntry_NilSafeGetters(t *testing.T) {
	var entry *ActionEntry

	if entry.GetPhase() != Phase_PHASE_UNSPECIFIED {
		t.Errorf("GetPhase() on nil should return PHASE_UNSPECIFIED")
	}
	if entry.GetPredicate() != "" {
		t.Errorf("GetPredicate() on nil should return empty string")
	}
	if entry.GetGrpc() != nil {
		t.Errorf("GetGrpc() on nil should return nil")
	}
	if entry.GetDeny() != nil {
		t.Errorf("GetDeny() on nil should return nil")
	}
	if entry.GetAddHeaders() != nil {
		t.Errorf("GetAddHeaders() on nil should return nil")
	}
	if entry.GetFail() != nil {
		t.Errorf("GetFail() on nil should return nil")
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
			Phase:     Phase_PHASE_REQUEST,
			Predicate: `request.url_path == "/blocked"`,
			Action: &ActionEntry_Deny{Deny: &DenyAction{
				WithStatus: 403,
			}},
		},
		{
			Phase: Phase_PHASE_REQUEST,
			Action: &ActionEntry_Grpc{Grpc: &GrpcAction{
				Method: "checkThreatLevel",
				Var:    "threatResponse",
			}},
		},
		{
			Phase:     Phase_PHASE_RESPONSE,
			Predicate: "threatResponse.threat_level >= 5",
			Action: &ActionEntry_Deny{Deny: &DenyAction{
				WithStatus: 403,
			}},
		},
		{
			Phase: Phase_PHASE_RESPONSE,
			Action: &ActionEntry_AddHeaders{AddHeaders: &AddHeadersAction{
				HeadersToAdd: `{"x-threat-checked": "true"}`,
			}},
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
	if req.GetActions()[0].GetDeny().GetWithStatus() != 403 {
		t.Errorf("first action WithStatus = %d, want %d", req.GetActions()[0].GetDeny().GetWithStatus(), 403)
	}
	if req.GetActions()[1].GetGrpc().GetMethod() != "checkThreatLevel" {
		t.Errorf("second action Method = %q, want %q", req.GetActions()[1].GetGrpc().GetMethod(), "checkThreatLevel")
	}
	if req.GetActions()[2].GetPhase() != Phase_PHASE_RESPONSE {
		t.Errorf("third action Phase = %v, want %v", req.GetActions()[2].GetPhase(), Phase_PHASE_RESPONSE)
	}
	if req.GetActions()[3].GetAddHeaders().GetHeadersToAdd() != `{"x-threat-checked": "true"}` {
		t.Errorf("fourth action HeadersToAdd = %q, unexpected", req.GetActions()[3].GetAddHeaders().GetHeadersToAdd())
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
