//go:build unit

package types

import "testing"

func TestGRPCMethodAction_ImplementsRequestAction(t *testing.T) {
	var _ RequestAction = GRPCMethodAction{}
}

func TestAllowAction_ImplementsRequestAction(t *testing.T) {
	var _ RequestAction = AllowAction{}
}

func TestAddHeadersAction_ImplementsResponseAction(t *testing.T) {
	var _ ResponseAction = AddHeadersAction{}
}

func TestWithResponseCodeAction_ImplementsResponseAction(t *testing.T) {
	var _ ResponseAction = WithResponseCodeAction{}
}

func TestGRPCMethodAction_RequestActionType(t *testing.T) {
	a := GRPCMethodAction{
		Predicate: "request.headers['check'] == '1'",
		Intention: "response.HeatLevel == 5",
		Method:    "checkThreatLevel",
	}
	if a.requestActionType() != ActionTypeGRPCMethod {
		t.Errorf("requestActionType() = %q, want %q", a.requestActionType(), ActionTypeGRPCMethod)
	}
}

func TestAllowAction_RequestActionType(t *testing.T) {
	a := AllowAction{
		Predicate: "request.headers['x-bypass'] == 'true'",
		Intention: "request.auth.identity.admin == true",
	}
	if a.requestActionType() != ActionTypeAllow {
		t.Errorf("requestActionType() = %q, want %q", a.requestActionType(), ActionTypeAllow)
	}
}

func TestAddHeadersAction_ResponseActionType(t *testing.T) {
	a := AddHeadersAction{
		HeadersToAdd: "{'x-threat-checked': 'true'}",
	}
	if a.responseActionType() != ActionTypeAddHeaders {
		t.Errorf("responseActionType() = %q, want %q", a.responseActionType(), ActionTypeAddHeaders)
	}
}

func TestWithResponseCodeAction_ResponseActionType(t *testing.T) {
	a := WithResponseCodeAction{
		NewResponseCode: 403,
	}
	if a.responseActionType() != ActionTypeWithResponseCode {
		t.Errorf("responseActionType() = %q, want %q", a.responseActionType(), ActionTypeWithResponseCode)
	}
}
