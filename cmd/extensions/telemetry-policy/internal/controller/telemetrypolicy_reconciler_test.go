//go:build unit

package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	celref "github.com/google/cel-go/common/types/ref"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/kuadrant/kuadrant-operator/cmd/extensions/telemetry-policy/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/pkg/extension/types"
)

type addDataCall struct {
	domain     types.Domain
	binding    string
	expression string
}

type mockKuadrantCtx struct {
	calls   []addDataCall
	failOn  string // return error when binding matches this value
	failErr error
}

func (m *mockKuadrantCtx) AddDataTo(_ context.Context, _ types.Policy, domain types.Domain, binding, expression string) error {
	m.calls = append(m.calls, addDataCall{domain: domain, binding: binding, expression: expression})
	if m.failOn != "" && binding == m.failOn {
		return m.failErr
	}
	return nil
}

func (m *mockKuadrantCtx) Resolve(context.Context, types.Policy, string, bool) (celref.Val, error) {
	return nil, nil
}
func (m *mockKuadrantCtx) ResolvePolicy(context.Context, types.Policy, string, bool) (types.Policy, error) {
	return nil, nil
}
func (m *mockKuadrantCtx) ReconcileObject(context.Context, client.Object, client.Object, types.MutateFn) (client.Object, error) {
	return nil, nil
}
func (m *mockKuadrantCtx) RegisterActionMethod(_ context.Context, _ types.Policy, _ types.ActionMethodConfig) error {
	return nil
}
func (m *mockKuadrantCtx) NewPipeline(types.Policy) types.Pipeline { return nil }

func newTestPolicy(metrics map[string]string, loggingFields map[string]string) *v1alpha1.TelemetryPolicy {
	pol := &v1alpha1.TelemetryPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-policy",
			Namespace:  "default",
			Generation: 1,
		},
		Spec: v1alpha1.TelemetryPolicySpec{
			TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
				LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
					Group: "gateway.networking.k8s.io",
					Kind:  "Gateway",
					Name:  "my-gw",
				},
			},
		},
	}
	if metrics != nil {
		pol.Spec.Metrics = &v1alpha1.MetricsSpec{Default: v1alpha1.MetricsConfig{Labels: metrics}}
	}
	if loggingFields != nil {
		pol.Spec.Logging = &v1alpha1.LoggingSpec{Default: v1alpha1.LoggingConfig{Fields: loggingFields}}
	}
	return pol
}

func TestReconcileSpec_LoggingFieldsOnly(t *testing.T) {
	mock := &mockKuadrantCtx{}
	r := &TelemetryPolicyReconciler{
		ExtensionBase: types.ExtensionBase{Logger: logr.Discard()},
	}
	pol := newTestPolicy(nil, map[string]string{
		"client_identity": "auth.identity.sub",
		"request_path":    "request.path",
	})

	status, err := r.reconcileSpec(context.Background(), pol, mock)
	if err != nil {
		t.Fatalf("reconcileSpec returned error: %v", err)
	}
	if status == nil {
		t.Fatal("reconcileSpec returned nil status")
	}

	if len(mock.calls) != 2 {
		t.Fatalf("expected 2 AddDataTo calls, got %d", len(mock.calls))
	}

	callMap := make(map[string]addDataCall, len(mock.calls))
	for _, c := range mock.calls {
		callMap[c.binding] = c
	}

	if c, ok := callMap["logging.fields.client_identity"]; !ok {
		t.Error("missing AddDataTo call for logging.fields.client_identity")
	} else {
		if c.expression != "auth.identity.sub" {
			t.Errorf("client_identity expression = %q, want %q", c.expression, "auth.identity.sub")
		}
		if c.domain != types.DomainRequest {
			t.Errorf("client_identity domain = %v, want DomainRequest", c.domain)
		}
	}

	if c, ok := callMap["logging.fields.request_path"]; !ok {
		t.Error("missing AddDataTo call for logging.fields.request_path")
	} else {
		if c.expression != "request.path" {
			t.Errorf("request_path expression = %q, want %q", c.expression, "request.path")
		}
	}
}

func TestReconcileSpec_MetricsAndLogging(t *testing.T) {
	mock := &mockKuadrantCtx{}
	r := &TelemetryPolicyReconciler{
		ExtensionBase: types.ExtensionBase{Logger: logr.Discard()},
	}
	pol := newTestPolicy(
		map[string]string{"model": "responseBodyJSON('/model')"},
		map[string]string{"client_identity": "auth.identity.sub"},
	)

	_, err := r.reconcileSpec(context.Background(), pol, mock)
	if err != nil {
		t.Fatalf("reconcileSpec returned error: %v", err)
	}

	if len(mock.calls) != 2 {
		t.Fatalf("expected 2 AddDataTo calls, got %d", len(mock.calls))
	}

	callMap := make(map[string]addDataCall, len(mock.calls))
	for _, c := range mock.calls {
		callMap[c.binding] = c
	}

	if _, ok := callMap["metrics.labels.model"]; !ok {
		t.Error("missing AddDataTo call for metrics.labels.model")
	}
	if _, ok := callMap["logging.fields.client_identity"]; !ok {
		t.Error("missing AddDataTo call for logging.fields.client_identity")
	}
}

func TestReconcileSpec_LoggingFieldError(t *testing.T) {
	expectedErr := fmt.Errorf("binding failed")
	mock := &mockKuadrantCtx{
		failOn:  "logging.fields.bad_field",
		failErr: expectedErr,
	}
	r := &TelemetryPolicyReconciler{
		ExtensionBase: types.ExtensionBase{Logger: logr.Discard()},
	}
	pol := newTestPolicy(nil, map[string]string{
		"bad_field": "invalid.expression",
	})

	status, err := r.reconcileSpec(context.Background(), pol, mock)
	if err != expectedErr {
		t.Fatalf("expected error %v, got %v", expectedErr, err)
	}
	if status == nil {
		t.Fatal("expected error status, got nil")
	}
}

// assertRejected checks the outcome of a reconcileSpec call for a policy whose expression was
// rejected: no error (the rejection is terminal until the spec changes), Accepted=False with the
// Invalid reason and a message naming the offending path, no Enforced condition, and no binding
// registered.
func assertRejected(t *testing.T, mock *mockKuadrantCtx, status *v1alpha1.TelemetryPolicyStatus, err error, wantPath, wantDetail string) {
	t.Helper()
	if err != nil {
		t.Fatalf("reconcileSpec returned error %v, want nil for a rejected policy", err)
	}
	if status == nil {
		t.Fatal("reconcileSpec returned nil status")
	}
	if len(mock.calls) != 0 {
		t.Errorf("expected 0 AddDataTo calls for a rejected policy, got %d: %+v", len(mock.calls), mock.calls)
	}

	accepted := meta.FindStatusCondition(status.Conditions, string(gatewayapiv1alpha2.PolicyConditionAccepted))
	if accepted == nil {
		t.Fatal("missing Accepted condition")
	}
	if accepted.Status != metav1.ConditionFalse {
		t.Errorf("Accepted status = %s, want False", accepted.Status)
	}
	if accepted.Reason != string(gatewayapiv1alpha2.PolicyReasonInvalid) {
		t.Errorf("Accepted reason = %s, want %s", accepted.Reason, gatewayapiv1alpha2.PolicyReasonInvalid)
	}
	if !strings.Contains(accepted.Message, wantPath) {
		t.Errorf("Accepted message = %q, want it to contain %q", accepted.Message, wantPath)
	}
	if !strings.Contains(accepted.Message, wantDetail) {
		t.Errorf("Accepted message = %q, want it to contain %q", accepted.Message, wantDetail)
	}

	if enforced := meta.FindStatusCondition(status.Conditions, string(types.PolicyConditionEnforced)); enforced != nil {
		t.Errorf("unexpected Enforced condition on a rejected policy: %+v", enforced)
	}
}

func TestReconcileSpec_RejectsSensitiveHeaderLoggingField(t *testing.T) {
	mock := &mockKuadrantCtx{}
	r := &TelemetryPolicyReconciler{
		ExtensionBase: types.ExtensionBase{Logger: logr.Discard()},
	}
	pol := newTestPolicy(nil, map[string]string{
		"request_path": "request.path",
		"probe":        "request.headers.authorization",
	})

	status, err := r.reconcileSpec(context.Background(), pol, mock)
	assertRejected(t, mock, status, err, `spec.logging.default.fields["probe"]`, `sensitive request header "authorization"`)
}

func TestReconcileSpec_RejectsSensitiveHeaderMetricLabel(t *testing.T) {
	mock := &mockKuadrantCtx{}
	r := &TelemetryPolicyReconciler{
		ExtensionBase: types.ExtensionBase{Logger: logr.Discard()},
	}
	pol := newTestPolicy(
		map[string]string{"key": `request.headers["x-api-key"]`},
		map[string]string{"request_path": "request.path"},
	)

	status, err := r.reconcileSpec(context.Background(), pol, mock)
	assertRejected(t, mock, status, err, `spec.metrics.default.labels["key"]`, `sensitive request header "x-api-key"`)
}

func TestReconcileSpec_RejectsWholeHeadersMap(t *testing.T) {
	mock := &mockKuadrantCtx{}
	r := &TelemetryPolicyReconciler{
		ExtensionBase: types.ExtensionBase{Logger: logr.Discard()},
	}
	pol := newTestPolicy(nil, map[string]string{
		"all_headers": "string(request.headers)",
	})

	status, err := r.reconcileSpec(context.Background(), pol, mock)
	assertRejected(t, mock, status, err, `spec.logging.default.fields["all_headers"]`, errWholeHeaders.Error())
}

func TestReconcileSpec_RejectsUnparseableExpression(t *testing.T) {
	mock := &mockKuadrantCtx{}
	r := &TelemetryPolicyReconciler{
		ExtensionBase: types.ExtensionBase{Logger: logr.Discard()},
	}
	pol := newTestPolicy(nil, map[string]string{
		"broken": "request.",
	})

	status, err := r.reconcileSpec(context.Background(), pol, mock)
	assertRejected(t, mock, status, err, `spec.logging.default.fields["broken"]`, "invalid CEL expression")
}

func TestReconcileSpec_RejectionReportsFirstFieldInKeyOrder(t *testing.T) {
	mock := &mockKuadrantCtx{}
	r := &TelemetryPolicyReconciler{
		ExtensionBase: types.ExtensionBase{Logger: logr.Discard()},
	}
	pol := newTestPolicy(nil, map[string]string{
		"b_probe": "request.headers.authorization",
		"a_probe": "request.headers.cookie",
	})

	for range 10 {
		mock.calls = nil
		status, err := r.reconcileSpec(context.Background(), pol, mock)
		assertRejected(t, mock, status, err, `spec.logging.default.fields["a_probe"]`, `sensitive request header "cookie"`)
	}
}

func TestReconcileSpec_AcceptsPresenceTestOfSensitiveHeader(t *testing.T) {
	mock := &mockKuadrantCtx{}
	r := &TelemetryPolicyReconciler{
		ExtensionBase: types.ExtensionBase{Logger: logr.Discard()},
	}
	pol := newTestPolicy(nil, map[string]string{
		"auth_scheme": `has(request.headers.authorization) ? "bearer" : "anonymous"`,
	})

	status, err := r.reconcileSpec(context.Background(), pol, mock)
	if err != nil {
		t.Fatalf("reconcileSpec returned error: %v", err)
	}
	if len(mock.calls) != 1 {
		t.Fatalf("expected 1 AddDataTo call, got %d", len(mock.calls))
	}
	accepted := meta.FindStatusCondition(status.Conditions, string(gatewayapiv1alpha2.PolicyConditionAccepted))
	if accepted == nil || accepted.Status != metav1.ConditionTrue {
		t.Fatalf("Accepted condition = %+v, want True", accepted)
	}
}

func TestErrInvalidExpression_IsPolicyError(t *testing.T) {
	err := errInvalidExpression{path: `spec.logging.default.fields["x"]`, err: errWholeHeaders}

	var policyErr interface {
		error
		Reason() gatewayapiv1alpha2.PolicyConditionReason
	}
	if !errors.As(err, &policyErr) {
		t.Fatal("errInvalidExpression does not satisfy the PolicyError contract")
	}
	if policyErr.Reason() != gatewayapiv1alpha2.PolicyReasonInvalid {
		t.Errorf("Reason() = %s, want %s", policyErr.Reason(), gatewayapiv1alpha2.PolicyReasonInvalid)
	}
	if !errors.Is(err, errWholeHeaders) {
		t.Error("errInvalidExpression does not unwrap to the underlying error")
	}
}

func TestReconcileSpec_EmptySpec(t *testing.T) {
	mock := &mockKuadrantCtx{}
	r := &TelemetryPolicyReconciler{
		ExtensionBase: types.ExtensionBase{Logger: logr.Discard()},
	}
	pol := newTestPolicy(nil, nil)

	status, err := r.reconcileSpec(context.Background(), pol, mock)
	if err != nil {
		t.Fatalf("reconcileSpec returned error: %v", err)
	}
	if status == nil {
		t.Fatal("reconcileSpec returned nil status")
	}
	if len(mock.calls) != 0 {
		t.Errorf("expected 0 AddDataTo calls for empty spec, got %d", len(mock.calls))
	}
}
