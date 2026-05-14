//go:build unit

package types

import "testing"

func TestGRPCMethodAction_ImplementsAction(t *testing.T) {
	var _ Action = GRPCMethodAction{}
}

func TestDenyAction_ImplementsAction(t *testing.T) {
	var _ Action = DenyAction{}
}

func TestFailureAction_ImplementsAction(t *testing.T) {
	var _ Action = FailureAction{}
}

func TestAddHeadersAction_ImplementsAction(t *testing.T) {
	var _ Action = AddHeadersAction{}
}

func TestGRPCMethodAction_ActionType(t *testing.T) {
	a := GRPCMethodAction{
		Predicate: "request.headers['check'] == '1'",
		Method:    "checkThreatLevel",
		Var:       "threatResponse",
	}
	if a.actionType() != ActionTypeGRPCMethod {
		t.Errorf("actionType() = %q, want %q", a.actionType(), ActionTypeGRPCMethod)
	}
}

func TestDenyAction_ActionType(t *testing.T) {
	a := DenyAction{
		Predicate: "request.url_path == '/blocked'",
		DenyWith:  "403",
	}
	if a.actionType() != ActionTypeDeny {
		t.Errorf("actionType() = %q, want %q", a.actionType(), ActionTypeDeny)
	}
}

func TestFailureAction_ActionType(t *testing.T) {
	a := FailureAction{
		Predicate:      "request.headers['x-debug'] == 'true'",
		FailureMessage: "Request blocked by threat policy",
		FailureCode:    "500",
	}
	if a.actionType() != ActionTypeFailure {
		t.Errorf("actionType() = %q, want %q", a.actionType(), ActionTypeFailure)
	}
}

func TestAddHeadersAction_ActionType(t *testing.T) {
	a := AddHeadersAction{
		Predicate:    "true",
		HeadersToAdd: `{"x-threat-checked": "true"}`,
	}
	if a.actionType() != ActionTypeAddHeaders {
		t.Errorf("actionType() = %q, want %q", a.actionType(), ActionTypeAddHeaders)
	}
}
