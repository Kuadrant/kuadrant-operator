//go:build unit

package controller

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	celref "github.com/google/cel-go/common/types/ref"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
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
	calls    []addDataCall
	failOn   string // return error when binding matches this value
	failErr  error
	pipeline *mockPipeline
}

type mockPipeline struct {
	requestActions  []types.Action
	responseActions []types.Action
	committed       bool
	commitErr       error
}

func (m *mockPipeline) OnHTTPRequest(actions ...types.Action) error {
	m.requestActions = append(m.requestActions, actions...)
	return nil
}

func (m *mockPipeline) OnHTTPResponse(actions ...types.Action) error {
	m.responseActions = append(m.responseActions, actions...)
	return nil
}

func (m *mockPipeline) Commit(context.Context) error {
	m.committed = true
	return m.commitErr
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
func (m *mockKuadrantCtx) NewPipeline(types.Policy) types.Pipeline {
	if m.pipeline == nil {
		m.pipeline = &mockPipeline{}
	}
	return m.pipeline
}

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

func newTestGatewayClient(t *testing.T, controllerName gatewayapiv1.GatewayController) client.Client {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := gatewayapiv1.Install(scheme); err != nil {
		t.Fatalf("failed to install Gateway API scheme: %v", err)
	}

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(
			&gatewayapiv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{Name: "my-gw", Namespace: "default"},
				Spec:       gatewayapiv1.GatewaySpec{GatewayClassName: "test-class"},
			},
			&gatewayapiv1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{Name: "test-class"},
				Spec:       gatewayapiv1.GatewayClassSpec{ControllerName: controllerName},
			},
		).
		Build()
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
	if !mock.pipeline.committed {
		t.Error("expected an empty pipeline commit to clear stale OCSF actions")
	}
	if len(mock.pipeline.requestActions) != 0 {
		t.Errorf("expected 0 request actions, got %d", len(mock.pipeline.requestActions))
	}
}

func TestReconcileSpec_OCSFExportsOnlyApprovedMetadata(t *testing.T) {
	mock := &mockKuadrantCtx{}
	r := &TelemetryPolicyReconciler{
		ExtensionBase: types.ExtensionBase{
			Logger: logr.Discard(),
			Client: newTestGatewayClient(t, istioGatewayControllerName),
		},
	}
	pol := newTestPolicy(nil, nil)
	pol.Spec.Logging = &v1alpha1.LoggingSpec{Default: v1alpha1.LoggingConfig{
		OCSF: &v1alpha1.OCSFLoggingConfig{
			Profile: v1alpha1.OCSFProfileAIOperation,
			Actor: v1alpha1.OCSFActorConfig{
				Identity: "auth.identity.user.username",
				Groups:   "auth.identity.user.groups",
			},
			Target: v1alpha1.OCSFTargetConfig{
				ResourceType: "LLMInferenceService",
				RequestPath: v1alpha1.OCSFRequestPathTarget{
					NamespaceSegment: 0,
					NameSegment:      1,
				},
				AIProvider: "KServe",
			},
		},
	}}

	_, err := r.reconcileSpec(context.Background(), pol, mock)
	if err != nil {
		t.Fatalf("reconcileSpec returned error: %v", err)
	}
	if !mock.pipeline.committed {
		t.Fatal("expected OCSF pipeline to be committed")
	}
	if got, want := len(mock.pipeline.requestActions), 7; got != want {
		t.Fatalf("request actions = %d, want %d", got, want)
	}

	want := map[string]types.StoreAction{
		ocsfActorUserNameStorePath: {
			Predicate:    "has(auth.identity.user.username)",
			Path:         ocsfActorUserNameStorePath,
			Value:        "auth.identity.user.username",
			ExportToHost: true,
			ExportFormat: types.StoreExportFormatProtobufValue,
		},
		ocsfActorUserGroupsStorePath: {
			Predicate:    "has(auth.identity.user.groups)",
			Path:         ocsfActorUserGroupsStorePath,
			Value:        `auth.identity.user.groups.map(group, {"name": group})`,
			ExportToHost: true,
			ExportFormat: types.StoreExportFormatProtobufValue,
		},
		ocsfURLPathStorePath: {
			Path:         ocsfURLPathStorePath,
			Value:        "request.url_path",
			ExportToHost: true,
			ExportFormat: types.StoreExportFormatProtobufValue,
		},
		ocsfResourceNamespaceStorePath: {
			Predicate:    "request.url_path.split('/').size() > 1",
			Path:         ocsfResourceNamespaceStorePath,
			Value:        "request.url_path.split('/')[1]",
			ExportToHost: true,
			ExportFormat: types.StoreExportFormatProtobufValue,
		},
		ocsfResourceNameStorePath: {
			Predicate:    "request.url_path.split('/').size() > 2",
			Path:         ocsfResourceNameStorePath,
			Value:        "request.url_path.split('/')[2]",
			ExportToHost: true,
			ExportFormat: types.StoreExportFormatProtobufValue,
		},
		ocsfResourceTypeStorePath: {
			Path:         ocsfResourceTypeStorePath,
			Value:        `"LLMInferenceService"`,
			ExportToHost: true,
			ExportFormat: types.StoreExportFormatProtobufValue,
		},
		ocsfAIProviderStorePath: {
			Path:         ocsfAIProviderStorePath,
			Value:        `"KServe"`,
			ExportToHost: true,
			ExportFormat: types.StoreExportFormatProtobufValue,
		},
	}

	for _, action := range mock.pipeline.requestActions {
		store, ok := action.(types.StoreAction)
		if !ok {
			t.Fatalf("action type = %T, want types.StoreAction", action)
		}
		expected, ok := want[store.Path]
		if !ok {
			t.Errorf("unexpected store path %q", store.Path)
			continue
		}
		if store != expected {
			t.Errorf("store action for %s = %#v, want %#v", store.Path, store, expected)
		}
		delete(want, store.Path)
	}
	if len(want) != 0 {
		t.Errorf("missing store actions: %#v", want)
	}
}

func TestReconcileSpec_OCSFRejectsUnsupportedGatewayProvider(t *testing.T) {
	mock := &mockKuadrantCtx{}
	r := &TelemetryPolicyReconciler{
		ExtensionBase: types.ExtensionBase{
			Logger: logr.Discard(),
			Client: newTestGatewayClient(t, gatewayapiv1.GatewayController("gateway.envoyproxy.io/gatewayclass-controller")),
		},
	}
	pol := newTestPolicy(nil, nil)
	pol.Spec.Logging = &v1alpha1.LoggingSpec{Default: v1alpha1.LoggingConfig{OCSF: &v1alpha1.OCSFLoggingConfig{}}}

	status, err := r.reconcileSpec(context.Background(), pol, mock)
	if err == nil || !strings.Contains(err.Error(), "only supported by Istio") {
		t.Fatalf("reconcileSpec error = %v, want unsupported Gateway provider error", err)
	}
	if len(mock.calls) != 0 {
		t.Fatalf("unexpected binding calls before provider validation: %#v", mock.calls)
	}
	if mock.pipeline != nil {
		t.Fatal("unsupported Gateway provider must not commit an OCSF pipeline")
	}

	accepted := false
	enforced := false
	for _, condition := range status.Conditions {
		switch condition.Type {
		case string(gatewayapiv1alpha2.PolicyConditionAccepted):
			accepted = condition.Status == metav1.ConditionTrue
		case string(types.PolicyConditionEnforced):
			enforced = true
			if condition.Status != metav1.ConditionFalse {
				t.Fatalf("Enforced condition status = %s, want False", condition.Status)
			}
		}
	}
	if !accepted {
		t.Fatal("unsupported provider should remain Accepted=True with a clear Enforced=False condition")
	}
	if !enforced {
		t.Fatal("unsupported provider should set Enforced=False")
	}
}

func TestReconcileSpec_PipelineCommitError(t *testing.T) {
	expectedErr := fmt.Errorf("commit failed")
	mock := &mockKuadrantCtx{pipeline: &mockPipeline{commitErr: expectedErr}}
	r := &TelemetryPolicyReconciler{
		ExtensionBase: types.ExtensionBase{Logger: logr.Discard()},
	}

	status, err := r.reconcileSpec(context.Background(), newTestPolicy(nil, nil), mock)
	if err != expectedErr {
		t.Fatalf("expected error %v, got %v", expectedErr, err)
	}
	if status == nil {
		t.Fatal("expected error status, got nil")
	}
}
