//go:build unit

package types

import (
	"testing"

	extpb "github.com/kuadrant/kuadrant-operator/pkg/extension/grpc/v1"
)

func TestGRPCAction_ImplementsAction(t *testing.T) {
	var _ Action = GRPCAction{}
}

func TestDenyAction_ImplementsAction(t *testing.T) {
	var _ Action = DenyAction{}
}

func TestFailAction_ImplementsAction(t *testing.T) {
	var _ Action = FailAction{}
}

func TestAddHeadersAction_ImplementsAction(t *testing.T) {
	var _ Action = AddHeadersAction{}
}

func TestGRPCAction_PopulateProtobuf(t *testing.T) {
	a := GRPCAction{
		Predicate: "request.headers['check'] == '1'",
		Method:    "checkThreatLevel",
		Var:       "threatResponse",
	}
	entry := &extpb.ActionEntry{}
	a.PopulateProtobuf(entry)

	if entry.Predicate != "request.headers['check'] == '1'" {
		t.Errorf("Predicate = %q, want %q", entry.Predicate, "request.headers['check'] == '1'")
	}
	if entry.GetGrpc() == nil {
		t.Fatal("Expected Grpc action to be set")
	}
	if entry.GetGrpc().Method != "checkThreatLevel" {
		t.Errorf("Method = %q, want %q", entry.GetGrpc().Method, "checkThreatLevel")
	}
	if entry.GetGrpc().Var != "threatResponse" {
		t.Errorf("Var = %q, want %q", entry.GetGrpc().Var, "threatResponse")
	}
}

func TestDenyAction_PopulateProtobuf(t *testing.T) {
	a := DenyAction{
		Predicate:  "request.url_path == '/blocked'",
		WithStatus: 403,
		WithBody:   "Forbidden",
	}
	entry := &extpb.ActionEntry{}
	a.PopulateProtobuf(entry)

	if entry.Predicate != "request.url_path == '/blocked'" {
		t.Errorf("Predicate = %q, want %q", entry.Predicate, "request.url_path == '/blocked'")
	}
	if entry.GetDeny() == nil {
		t.Fatal("Expected Deny action to be set")
	}
	if entry.GetDeny().WithStatus != 403 {
		t.Errorf("WithStatus = %d, want %d", entry.GetDeny().WithStatus, 403)
	}
	if entry.GetDeny().WithBody != "Forbidden" {
		t.Errorf("WithBody = %q, want %q", entry.GetDeny().WithBody, "Forbidden")
	}
}

func TestFailAction_PopulateProtobuf(t *testing.T) {
	a := FailAction{
		Predicate:  "threatResponse.error_code != 0",
		LogMessage: "Threat service returned unexpected error",
	}
	entry := &extpb.ActionEntry{}
	a.PopulateProtobuf(entry)

	if entry.Predicate != "threatResponse.error_code != 0" {
		t.Errorf("Predicate = %q, want %q", entry.Predicate, "threatResponse.error_code != 0")
	}
	if entry.GetFail() == nil {
		t.Fatal("Expected Fail action to be set")
	}
	if entry.GetFail().LogMessage != "Threat service returned unexpected error" {
		t.Errorf("LogMessage = %q, want %q", entry.GetFail().LogMessage, "Threat service returned unexpected error")
	}
}

func TestAddHeadersAction_PopulateProtobuf(t *testing.T) {
	a := AddHeadersAction{
		Predicate:    "true",
		HeadersToAdd: `{"x-threat-checked": "true"}`,
	}
	entry := &extpb.ActionEntry{}
	a.PopulateProtobuf(entry)

	if entry.Predicate != "true" {
		t.Errorf("Predicate = %q, want %q", entry.Predicate, "true")
	}
	if entry.GetAddHeaders() == nil {
		t.Fatal("Expected AddHeaders action to be set")
	}
	if entry.GetAddHeaders().HeadersToAdd != `{"x-threat-checked": "true"}` {
		t.Errorf("HeadersToAdd = %q, want %q", entry.GetAddHeaders().HeadersToAdd, `{"x-threat-checked": "true"}`)
	}
}
