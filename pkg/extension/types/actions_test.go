//go:build unit

package types

import "testing"

func TestGRPCMethodAction_ImplementsAction(t *testing.T) {
	var _ Action = GRPCMethodAction{}
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
		Predicate:  "request.url_path == '/blocked'",
		WithStatus: 403,
		WithBody:   "Forbidden",
	}
	if a.actionType() != ActionTypeDeny {
		t.Errorf("actionType() = %q, want %q", a.actionType(), ActionTypeDeny)
	}
}

func TestFailAction_ActionType(t *testing.T) {
	a := FailAction{
		Predicate:  "threatResponse.error_code != 0",
		LogMessage: "Threat service returned unexpected error",
	}
	if a.actionType() != ActionTypeFail {
		t.Errorf("actionType() = %q, want %q", a.actionType(), ActionTypeFail)
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
