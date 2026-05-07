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
		{"allow", ActionType_ACTION_TYPE_ALLOW, "ACTION_TYPE_ALLOW"},
		{"add_headers", ActionType_ACTION_TYPE_ADD_HEADERS, "ACTION_TYPE_ADD_HEADERS"},
		{"with_response_code", ActionType_ACTION_TYPE_WITH_RESPONSE_CODE, "ACTION_TYPE_WITH_RESPONSE_CODE"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value.String() != tt.wantName {
				t.Errorf("ActionType(%d).String() = %q, want %q", tt.value, tt.value.String(), tt.wantName)
			}
		})
	}
}

func TestRequestActionEntry_FieldAccessors(t *testing.T) {
	entry := &RequestActionEntry{
		ActionType: ActionType_ACTION_TYPE_GRPC_METHOD,
		Predicate:  "request.headers['check'] == '1'",
		Intention:  "checkThreatResponse.HeatLevel == 5",
		Method:     "checkThreatLevel",
	}

	if entry.GetActionType() != ActionType_ACTION_TYPE_GRPC_METHOD {
		t.Errorf("GetActionType() = %v, want %v", entry.GetActionType(), ActionType_ACTION_TYPE_GRPC_METHOD)
	}
	if entry.GetPredicate() != "request.headers['check'] == '1'" {
		t.Errorf("GetPredicate() = %q, unexpected", entry.GetPredicate())
	}
	if entry.GetIntention() != "checkThreatResponse.HeatLevel == 5" {
		t.Errorf("GetIntention() = %q, unexpected", entry.GetIntention())
	}
	if entry.GetMethod() != "checkThreatLevel" {
		t.Errorf("GetMethod() = %q, unexpected", entry.GetMethod())
	}
}

func TestRequestActionEntry_NilSafeGetters(t *testing.T) {
	var entry *RequestActionEntry

	if entry.GetActionType() != ActionType_ACTION_TYPE_UNSPECIFIED {
		t.Errorf("GetActionType() on nil should return UNSPECIFIED")
	}
	if entry.GetPredicate() != "" {
		t.Errorf("GetPredicate() on nil should return empty string")
	}
	if entry.GetIntention() != "" {
		t.Errorf("GetIntention() on nil should return empty string")
	}
	if entry.GetMethod() != "" {
		t.Errorf("GetMethod() on nil should return empty string")
	}
}

func TestResponseActionEntry_FieldAccessors(t *testing.T) {
	entry := &ResponseActionEntry{
		ActionType:      ActionType_ACTION_TYPE_ADD_HEADERS,
		Predicate:       "response.code == 200",
		HeadersToAdd:    "{'x-threat-checked': 'true'}",
		NewResponseCode: 0,
	}

	if entry.GetActionType() != ActionType_ACTION_TYPE_ADD_HEADERS {
		t.Errorf("GetActionType() = %v, want %v", entry.GetActionType(), ActionType_ACTION_TYPE_ADD_HEADERS)
	}
	if entry.GetPredicate() != "response.code == 200" {
		t.Errorf("GetPredicate() = %q, unexpected", entry.GetPredicate())
	}
	if entry.GetHeadersToAdd() != "{'x-threat-checked': 'true'}" {
		t.Errorf("GetHeadersToAdd() = %q, unexpected", entry.GetHeadersToAdd())
	}
	if entry.GetNewResponseCode() != 0 {
		t.Errorf("GetNewResponseCode() = %d, want 0", entry.GetNewResponseCode())
	}

	// Test with_response_code type
	codeEntry := &ResponseActionEntry{
		ActionType:      ActionType_ACTION_TYPE_WITH_RESPONSE_CODE,
		NewResponseCode: 403,
	}
	if codeEntry.GetNewResponseCode() != 403 {
		t.Errorf("GetNewResponseCode() = %d, want 403", codeEntry.GetNewResponseCode())
	}
}

func TestResponseActionEntry_NilSafeGetters(t *testing.T) {
	var entry *ResponseActionEntry

	if entry.GetActionType() != ActionType_ACTION_TYPE_UNSPECIFIED {
		t.Errorf("GetActionType() on nil should return UNSPECIFIED")
	}
	if entry.GetPredicate() != "" {
		t.Errorf("GetPredicate() on nil should return empty string")
	}
	if entry.GetHeadersToAdd() != "" {
		t.Errorf("GetHeadersToAdd() on nil should return empty string")
	}
	if entry.GetNewResponseCode() != 0 {
		t.Errorf("GetNewResponseCode() on nil should return 0")
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
	requestActions := []*RequestActionEntry{
		{
			ActionType: ActionType_ACTION_TYPE_GRPC_METHOD,
			Method:     "checkThreatLevel",
			Intention:  "checkThreatLevelResponse.HeatLevel == 5",
		},
		{
			ActionType: ActionType_ACTION_TYPE_ALLOW,
			Intention:  "request.auth.identity.admin == true",
		},
	}
	responseActions := []*ResponseActionEntry{
		{
			ActionType:   ActionType_ACTION_TYPE_ADD_HEADERS,
			HeadersToAdd: "{'x-threat-checked': 'true'}",
		},
		{
			ActionType:      ActionType_ACTION_TYPE_WITH_RESPONSE_CODE,
			NewResponseCode: 429,
		},
	}

	req := &PipelineCommitRequest{
		Policy:          policy,
		RequestActions:  requestActions,
		ResponseActions: responseActions,
	}

	if req.GetPolicy() != policy {
		t.Errorf("GetPolicy() returned unexpected value")
	}
	if len(req.GetRequestActions()) != 2 {
		t.Fatalf("GetRequestActions() length = %d, want 2", len(req.GetRequestActions()))
	}
	if req.GetRequestActions()[0].GetMethod() != "checkThreatLevel" {
		t.Errorf("first request action Method = %q, want %q", req.GetRequestActions()[0].GetMethod(), "checkThreatLevel")
	}
	if req.GetRequestActions()[1].GetActionType() != ActionType_ACTION_TYPE_ALLOW {
		t.Errorf("second request action type = %v, want ALLOW", req.GetRequestActions()[1].GetActionType())
	}
	if len(req.GetResponseActions()) != 2 {
		t.Fatalf("GetResponseActions() length = %d, want 2", len(req.GetResponseActions()))
	}
	if req.GetResponseActions()[0].GetHeadersToAdd() != "{'x-threat-checked': 'true'}" {
		t.Errorf("first response action HeadersToAdd = %q, unexpected", req.GetResponseActions()[0].GetHeadersToAdd())
	}
	if req.GetResponseActions()[1].GetNewResponseCode() != 429 {
		t.Errorf("second response action NewResponseCode = %d, want 429", req.GetResponseActions()[1].GetNewResponseCode())
	}
}

func TestPipelineCommitRequest_NilSafeGetters(t *testing.T) {
	var req *PipelineCommitRequest

	if req.GetPolicy() != nil {
		t.Errorf("GetPolicy() on nil should return nil")
	}
	if req.GetRequestActions() != nil {
		t.Errorf("GetRequestActions() on nil should return nil")
	}
	if req.GetResponseActions() != nil {
		t.Errorf("GetResponseActions() on nil should return nil")
	}
}

func TestPipelineCommit_FullMethodName(t *testing.T) {
	expected := "/kuadrant.v1.ExtensionService/PipelineCommit"
	if ExtensionService_PipelineCommit_FullMethodName != expected {
		t.Errorf("FullMethodName = %q, want %q", ExtensionService_PipelineCommit_FullMethodName, expected)
	}
}
