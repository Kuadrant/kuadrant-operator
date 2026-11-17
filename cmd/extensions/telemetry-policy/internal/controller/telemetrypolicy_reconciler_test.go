//go:build unit

package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/google/cel-go/cel"
	celtypes "github.com/google/cel-go/common/types"
	celref "github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
	celext "github.com/google/cel-go/ext"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/kuadrant/kuadrant-operator/cmd/extensions/telemetry-policy/api/v1alpha1"
	wasmtypes "github.com/kuadrant/kuadrant-operator/internal/wasm"
	"github.com/kuadrant/kuadrant-operator/pkg/extension/types"
)

type addDataCall struct {
	domain     types.Domain
	binding    string
	expression string
}

type mockKuadrantCtx struct {
	calls             []addDataCall
	failOn            string // return error when binding matches this value
	failErr           error
	gatewayClassNames []string
	resolveErr        error
	resolveCalls      []resolveCall
	pipeline          *mockPipeline
}

type resolveCall struct {
	expression string
	subscribe  bool
}

type mockPipeline struct {
	preludeActions  []types.Action
	requestActions  []types.Action
	responseActions []types.Action
	committed       bool
	commitErr       error
}

func (m *mockPipeline) OnHTTPRequestPrelude(actions ...types.Action) error {
	m.preludeActions = append(m.preludeActions, actions...)
	return nil
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

func (m *mockKuadrantCtx) Resolve(_ context.Context, _ types.Policy, expression string, subscribe bool) (celref.Val, error) {
	m.resolveCalls = append(m.resolveCalls, resolveCall{expression: expression, subscribe: subscribe})
	if m.resolveErr != nil {
		return nil, m.resolveErr
	}
	return celtypes.DefaultTypeAdapter.NativeToValue(m.gatewayClassNames), nil
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
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-gw",
					Namespace: "default",
				},
				Spec: gatewayapiv1.GatewaySpec{GatewayClassName: "test-class"},
			},
			&gatewayapiv1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{Name: "test-class"},
				Spec:       gatewayapiv1.GatewayClassSpec{ControllerName: controllerName},
			},
		).
		Build()
}

func setTestPolicyEnvoyMinVersion(t *testing.T, policy *v1alpha1.TelemetryPolicy, version string) {
	t.Helper()
	if policy.Annotations == nil {
		policy.Annotations = make(map[string]string)
	}
	if version == "" {
		delete(policy.Annotations, ocsfEnvoyMinVersionAnnotation)
	} else {
		policy.Annotations[ocsfEnvoyMinVersionAnnotation] = version
	}
}

func testOCSFLoggingConfig() *v1alpha1.OCSFLoggingConfig {
	return &v1alpha1.OCSFLoggingConfig{
		Profile: v1alpha1.OCSFProfileAIOperation,
		Actor: v1alpha1.OCSFActorConfig{
			Identity: "auth.identity.user.username",
			UID:      "auth.identity.user.uid",
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
	}
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
	if !mock.pipeline.committed {
		t.Error("expected an empty pipeline commit to clear stale OCSF actions")
	}
	if len(mock.pipeline.preludeActions) != 0 {
		t.Errorf("expected 0 request prelude actions, got %d", len(mock.pipeline.preludeActions))
	}
	if len(mock.pipeline.requestActions) != 0 {
		t.Errorf("expected 0 request actions, got %d", len(mock.pipeline.requestActions))
	}
}

func TestReconcileSpec_OCSFExportsOnlyApprovedMetadata(t *testing.T) {
	mock := &mockKuadrantCtx{gatewayClassNames: []string{"test-class"}}
	r := &TelemetryPolicyReconciler{
		ExtensionBase: types.ExtensionBase{
			Logger: logr.Discard(),
			Client: newTestGatewayClient(t, istioGatewayControllerName),
		},
	}
	pol := newTestPolicy(nil, nil)
	setTestPolicyEnvoyMinVersion(t, pol, "1.36.0")
	pol.Spec.Logging = &v1alpha1.LoggingSpec{Default: v1alpha1.LoggingConfig{
		OCSF: &v1alpha1.OCSFLoggingConfig{
			Profile: v1alpha1.OCSFProfileAIOperation,
			Actor: v1alpha1.OCSFActorConfig{
				Identity: "auth.identity.user.username",
				UID:      "auth.identity.user.uid",
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
	if got, want := len(mock.pipeline.preludeActions), 4; got != want {
		t.Fatalf("request prelude actions = %d, want %d (three metadata stores and the fallback OCSF event)", got, want)
	}
	if got := len(mock.pipeline.requestActions); got != 0 {
		t.Fatalf("ordinary request actions = %d, want 0", got)
	}
	if got, want := len(mock.pipeline.responseActions), 1; got != want {
		t.Fatalf("response actions = %d, want %d", got, want)
	}

	env, err := cel.NewEnv(cel.Variable("request", cel.DynType), celext.Strings())
	if err != nil {
		t.Fatalf("cel.NewEnv failed: %v", err)
	}
	metadataPaths := []string{wasmtypes.OCSFResourcesStorePath, wasmtypes.OCSFAIModelStorePath, wasmtypes.OCSFURLPathStorePath}
	for index, path := range metadataPaths {
		metadata, ok := mock.pipeline.preludeActions[index].(types.StoreAction)
		if !ok {
			t.Fatalf("request prelude action %d has type %T, want types.StoreAction", index, mock.pipeline.preludeActions[index])
		}
		if metadata.Path != path || metadata.ExportToHost || metadata.ExportFormat != "" {
			t.Fatalf("request metadata store action = %#v, want internal prelude store at %q", metadata, path)
		}
		var required []string
		switch path {
		case wasmtypes.OCSFResourcesStorePath:
			required = []string{"name", "namespace", "LLMInferenceService"}
		case wasmtypes.OCSFAIModelStorePath:
			required = []string{"ai_provider", "KServe"}
		case wasmtypes.OCSFURLPathStorePath:
			required = []string{"request.url_path"}
		}
		for _, want := range required {
			if !strings.Contains(metadata.Value, want) {
				t.Errorf("request metadata value %q does not contain %q", metadata.Value, want)
			}
		}
		if strings.Contains(metadata.Value, "request.headers") || strings.Contains(metadata.Value, "Authorization") || strings.Contains(metadata.Value, "request.query") {
			t.Errorf("request metadata value must not export headers, credentials, or query data: %q", metadata.Value)
		}
		_, issues := env.Compile(metadata.Value)
		if issues != nil && issues.Err() != nil {
			t.Errorf("request metadata expression is invalid CEL: %v", issues.Err())
		}
	}

	preludeEvent, ok := mock.pipeline.preludeActions[3].(types.StoreAction)
	if !ok {
		t.Fatalf("request prelude event action type = %T, want types.StoreAction", mock.pipeline.preludeActions[3])
	}
	if preludeEvent.Path != wasmtypes.OCSFEventStorePath || preludeEvent.Predicate != "" || !preludeEvent.ExportToHost ||
		preludeEvent.ExportFormat != types.StoreExportFormatProtobufValue {
		t.Fatalf("request prelude event action = %#v, want one protobufValue fallback event export", preludeEvent)
	}

	event, ok := mock.pipeline.responseActions[0].(types.StoreAction)
	if !ok {
		t.Fatalf("response action type = %T, want types.StoreAction", mock.pipeline.responseActions[0])
	}
	if strings.Contains(preludeEvent.Value, "auth.identity") || !strings.Contains(preludeEvent.Value, `"actor": {"user": {"type_id": 0}}`) {
		t.Fatalf("request prelude event must use the unauthenticated fallback actor: %s", preludeEvent.Value)
	}
	if !strings.Contains(event.Value, "auth.identity.user.username") {
		t.Fatalf("response event must include the configured authenticated actor: %s", event.Value)
	}
	if event.Path != wasmtypes.OCSFEventStorePath || event.Predicate != "" || !event.ExportToHost ||
		event.ExportFormat != types.StoreExportFormatProtobufValue {
		t.Fatalf("response event action = %#v, want one protobufValue OCSF event export", event)
	}
	for _, want := range []string{
		`"class_uid": 6003`,
		`"type_uid": 600399`,
		`"version": "1.9.0"`,
		`request.method : "") + " "`,
		`request.protocol`,
		`ocsf.http_request.url.path`,
		`auth.identity.user.username`,
		`{"type_id": 0}`,
		`ocsf.resources`,
		`ocsf.ai_model`,
	} {
		if !strings.Contains(event.Value, want) {
			t.Errorf("OCSF event value does not contain %q: %s", want, event.Value)
		}
	}
	for _, streamField := range []string{"request.time", "request.duration", "response.code", "response.size"} {
		if strings.Contains(event.Value, streamField) {
			t.Errorf("OCSF StoreAction must leave final stream field %q to Envoy access logging: %s", streamField, event.Value)
		}
	}
	if strings.Contains(event.Value, "request.headers") || strings.Contains(event.Value, "Authorization") || strings.Contains(event.Value, "request.query") {
		t.Errorf("OCSF event value must not export headers, credentials, or query data: %q", event.Value)
	}

	if len(mock.resolveCalls) != 1 {
		t.Fatalf("Resolve calls = %d, want 1", len(mock.resolveCalls))
	}
	wantResolveExpression := fmt.Sprintf(`self.findGateways().map(gateway, %s)`, ocsfGatewayClassExpression("gateway"))
	if !mock.resolveCalls[0].subscribe || mock.resolveCalls[0].expression != wantResolveExpression {
		t.Fatalf("Resolve call = %#v, want subscribed Gateway class field", mock.resolveCalls[0])
	}
}

func TestReconcileSpec_OCSFRejectsUnsupportedGatewayProvider(t *testing.T) {
	mock := &mockKuadrantCtx{gatewayClassNames: []string{"test-class"}}
	r := &TelemetryPolicyReconciler{
		ExtensionBase: types.ExtensionBase{
			Logger: logr.Discard(),
			Client: newTestGatewayClient(t, gatewayapiv1.GatewayController("gateway.envoyproxy.io/gatewayclass-controller")),
		},
	}
	pol := newTestPolicy(
		map[string]string{"metric_label": "request.path"},
		map[string]string{"custom_field": "request.path"},
	)
	pol.Spec.Logging.Default.OCSF = &v1alpha1.OCSFLoggingConfig{}

	status, err := r.reconcileSpec(context.Background(), pol, mock)
	if err != nil {
		t.Fatalf("unsupported Gateway provider should be reported in policy status without requeueing: %v", err)
	}
	if len(mock.calls) != 2 {
		t.Fatalf("metrics and custom logging should still be reconciled, got calls %#v", mock.calls)
	}
	if mock.pipeline == nil || !mock.pipeline.committed {
		t.Fatal("unsupported Gateway provider should commit the remaining policy pipeline to clear stale OCSF actions")
	}
	if len(mock.pipeline.requestActions) != 0 || len(mock.pipeline.responseActions) != 0 {
		t.Fatalf("unsupported Gateway provider should not install OCSF actions: prelude=%#v request=%#v response=%#v", mock.pipeline.preludeActions, mock.pipeline.requestActions, mock.pipeline.responseActions)
	}

	accepted := false
	enforced := false
	for _, condition := range status.Conditions {
		switch condition.Type {
		case string(gatewayapiv1alpha2.PolicyConditionAccepted):
			accepted = condition.Status == metav1.ConditionTrue
		case string(types.PolicyConditionEnforced):
			enforced = true
			if condition.Status != metav1.ConditionFalse || !strings.Contains(condition.Message, "only supported by Istio") {
				t.Fatalf("Enforced condition = %#v, want False with unsupported provider detail", condition)
			}
		}
	}
	if !accepted {
		t.Fatal("unsupported provider should remain Accepted=True")
	}
	if !enforced {
		t.Fatal("unsupported provider should set Enforced=False")
	}
}

func TestReconcileSpec_OCSFMissingGatewayPreservesOtherBindings(t *testing.T) {
	mock := &mockKuadrantCtx{gatewayClassNames: []string{}}
	r := &TelemetryPolicyReconciler{
		ExtensionBase: types.ExtensionBase{
			Logger: logr.Discard(),
			Client: newTestGatewayClient(t, istioGatewayControllerName),
		},
	}
	pol := newTestPolicy(
		map[string]string{"metric_label": "request.path"},
		map[string]string{"custom_field": "request.path"},
	)
	pol.Spec.Logging.Default.OCSF = &v1alpha1.OCSFLoggingConfig{}

	status, err := r.reconcileSpec(context.Background(), pol, mock)
	if err != nil {
		t.Fatalf("missing Gateway should update status without requeueing: %v", err)
	}
	if len(mock.calls) != 2 {
		t.Fatalf("metrics and custom logging should still be reconciled, got calls %#v", mock.calls)
	}
	if mock.pipeline == nil || !mock.pipeline.committed {
		t.Fatal("missing Gateway should commit the remaining policy pipeline")
	}
	if len(mock.pipeline.preludeActions) != 0 || len(mock.pipeline.requestActions) != 0 || len(mock.pipeline.responseActions) != 0 {
		t.Fatal("missing Gateway must not install OCSF actions")
	}
	if len(mock.resolveCalls) != 1 || !mock.resolveCalls[0].subscribe {
		t.Fatalf("missing Gateway lookup must subscribe to topology changes, got %#v", mock.resolveCalls)
	}

	accepted := false
	for _, condition := range status.Conditions {
		if condition.Type == string(gatewayapiv1alpha2.PolicyConditionAccepted) {
			accepted = condition.Status == metav1.ConditionFalse
		}
	}
	if !accepted {
		t.Fatal("missing Gateway should report Accepted=False")
	}
}

func TestReconcileSpec_OCSFEnvoyVersionGate(t *testing.T) {
	tests := []struct {
		name           string
		minimumVersion string
		wantSupported  bool
		wantReason     string
	}{
		{name: "below minimum", minimumVersion: "1.35.0", wantReason: "EnvoyVersionUnsupported"},
		{name: "mixed replicas use lowest version", minimumVersion: "1.35.13", wantReason: "EnvoyVersionUnsupported"},
		{name: "unknown version", minimumVersion: "", wantReason: "EnvoyVersionNotVerified"},
		{name: "invalid version", minimumVersion: "unknown", wantReason: "EnvoyVersionNotVerified"},
		{name: "prerelease below minimum", minimumVersion: "1.36.0-rc.1", wantReason: "EnvoyVersionUnsupported"},
		{name: "minimum version", minimumVersion: "1.36.0", wantSupported: true},
		{name: "later patch", minimumVersion: "1.36.13", wantSupported: true},
		{name: "later minor", minimumVersion: "1.38.0", wantSupported: true},
		{name: "attested development build above minimum", minimumVersion: "1.38.0-dev", wantSupported: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockKuadrantCtx{gatewayClassNames: []string{"test-class"}}
			kubeClient := newTestGatewayClient(t, istioGatewayControllerName)
			r := &TelemetryPolicyReconciler{ExtensionBase: types.ExtensionBase{Logger: logr.Discard(), Client: kubeClient}}
			pol := newTestPolicy(map[string]string{"metric_label": "request.path"}, map[string]string{"custom_field": "request.path"})
			setTestPolicyEnvoyMinVersion(t, pol, tt.minimumVersion)
			pol.Spec.Logging.Default.OCSF = testOCSFLoggingConfig()

			status, err := r.reconcileSpec(context.Background(), pol, mock)
			if err != nil {
				t.Fatalf("reconcileSpec returned error: %v", err)
			}
			if len(mock.calls) != 2 {
				t.Fatalf("metrics and logging fields should still be reconciled, calls = %#v", mock.calls)
			}
			if mock.pipeline == nil || !mock.pipeline.committed {
				t.Fatal("the policy pipeline should be committed regardless of OCSF version support")
			}
			if status == nil {
				t.Fatal("expected a policy status")
			}

			var enforced *metav1.Condition
			for i := range status.Conditions {
				if status.Conditions[i].Type == string(types.PolicyConditionEnforced) {
					enforced = &status.Conditions[i]
					break
				}
			}
			if enforced == nil {
				t.Fatalf("missing Enforced condition: %#v", status.Conditions)
			}
			if tt.wantSupported {
				if enforced.Status != metav1.ConditionTrue || enforced.Reason != "Enforced" {
					t.Fatalf("Enforced condition = %#v, want True/Enforced", enforced)
				}
				if len(mock.pipeline.preludeActions) != 4 || len(mock.pipeline.responseActions) != 1 {
					t.Fatalf("qualifying Gateway actions = prelude:%d response:%d, want 4 and 1", len(mock.pipeline.preludeActions), len(mock.pipeline.responseActions))
				}
				return
			}

			if enforced.Status != metav1.ConditionFalse || string(enforced.Reason) != tt.wantReason || !strings.Contains(enforced.Message, "requires Envoy >= 1.36.0") {
				t.Fatalf("Enforced condition = %#v, want False/%s with the version requirement", enforced, tt.wantReason)
			}
			if len(mock.pipeline.preludeActions) != 0 || len(mock.pipeline.responseActions) != 0 {
				t.Fatalf("unqualified Gateway must not receive OCSF actions: prelude=%#v response=%#v", mock.pipeline.preludeActions, mock.pipeline.responseActions)
			}
		})
	}
}

func TestOCSFVersionSubscriptionExpressionUsesSupportedGatewayFields(t *testing.T) {
	expression := ocsfGatewayClassExpression("gateway")
	env, err := cel.NewEnv(cel.Variable("gateway", cel.DynType))
	if err != nil {
		t.Fatalf("cel.NewEnv failed: %v", err)
	}
	ast, issues := env.Compile(expression)
	if issues != nil && issues.Err() != nil {
		t.Fatalf("Gateway subscription expression is invalid CEL: %v", issues.Err())
	}
	program, err := env.Program(ast)
	if err != nil {
		t.Fatalf("env.Program failed: %v", err)
	}

	activation := map[string]any{"gateway": map[string]any{
		"spec": map[string]any{"gatewayClassName": "test-class"},
	}}
	got, _, err := program.Eval(activation)
	if err != nil {
		t.Fatalf("subscription expression evaluation failed: %v", err)
	}
	if got.Value() != "test-class" {
		t.Fatalf("subscription expression = %v, want test-class", got.Value())
	}
}

func TestReconcileSpec_OCSFEnvoyVersionUpgradeAndDowngrade(t *testing.T) {
	kubeClient := newTestGatewayClient(t, istioGatewayControllerName)
	r := &TelemetryPolicyReconciler{ExtensionBase: types.ExtensionBase{Logger: logr.Discard(), Client: kubeClient}}
	pol := newTestPolicy(map[string]string{"metric_label": "request.path"}, map[string]string{"custom_field": "request.path"})
	pol.Spec.Logging.Default.OCSF = testOCSFLoggingConfig()

	reconcileAt := func(version string) *mockPipeline {
		t.Helper()
		setTestPolicyEnvoyMinVersion(t, pol, version)
		mock := &mockKuadrantCtx{gatewayClassNames: []string{"test-class"}}
		if _, err := r.reconcileSpec(context.Background(), pol, mock); err != nil {
			t.Fatalf("reconcileSpec at minimum Envoy version %q returned error: %v", version, err)
		}
		if mock.pipeline == nil || !mock.pipeline.committed {
			t.Fatalf("pipeline at minimum Envoy version %q was not committed", version)
		}
		return mock.pipeline
	}

	if pipeline := reconcileAt("1.35.13"); len(pipeline.preludeActions) != 0 || len(pipeline.responseActions) != 0 {
		t.Fatalf("below-minimum version installed OCSF actions: %#v", pipeline)
	}
	if pipeline := reconcileAt("1.36.0"); len(pipeline.preludeActions) != 4 || len(pipeline.responseActions) != 1 {
		t.Fatalf("upgrade to 1.36.0 did not install OCSF actions: %#v", pipeline)
	}
	if pipeline := reconcileAt("1.35.13"); len(pipeline.preludeActions) != 0 || len(pipeline.responseActions) != 0 {
		t.Fatalf("downgrade below 1.36.0 did not remove OCSF actions: %#v", pipeline)
	}
}

func TestOCSFActorUserValueGuardsOptionalSelectorsAndOmitsMissingFields(t *testing.T) {
	actor := v1alpha1.OCSFActorConfig{
		Identity: "auth.identity.user.username",
		UID:      "auth.identity.user.uid",
		Groups:   "auth.identity.user.groups",
	}
	expression := ocsfActorUserValue(actor)
	for _, expected := range []string{
		"has(auth.identity.user)",
		"has(auth.identity.user.uid) && auth.identity.user.uid != null",
		"has(auth.identity.user.groups) && auth.identity.user.groups != null",
		`auth.identity.user.groups.map(group, {"name": group})`,
		`"type_id": auth.identity.user.username.startsWith("system:serviceaccount:") ? 4 : 1`,
	} {
		if !strings.Contains(expression, expected) {
			t.Errorf("actor expression %q does not contain %q", expression, expected)
		}
	}
	if strings.Contains(expression, "type(") {
		t.Fatalf("actor expression must not use unsupported CEL type(): %s", expression)
	}
	if strings.Contains(expression, ": null") {
		t.Fatalf("optional actor fields must be omitted from grouped maps, not exported as null: %s", expression)
	}

	requiredOnly := ocsfActorUserValue(v1alpha1.OCSFActorConfig{Identity: actor.Identity})
	if strings.Contains(requiredOnly, actor.UID) || strings.Contains(requiredOnly, actor.Groups) {
		t.Fatalf("unconfigured optional actor fields must be absent from the value: %s", requiredOnly)
	}
}

func TestOCSFStoreActionsExportProtobufValueEventBeforeAndAfterAuthorization(t *testing.T) {
	config := testOCSFLoggingConfig()

	requestActions, responseActions := ocsfStoreActions(config)
	wantMetadataPaths := []string{
		wasmtypes.OCSFResourcesStorePath,
		wasmtypes.OCSFAIModelStorePath,
		wasmtypes.OCSFURLPathStorePath,
	}
	if len(requestActions) != len(wantMetadataPaths)+1 {
		t.Fatalf("request actions = %d, want three metadata stores and one fallback event", len(requestActions))
	}
	for index, path := range wantMetadataPaths {
		store, ok := requestActions[index].(types.StoreAction)
		if !ok {
			t.Fatalf("request action %d = %T, want StoreAction", index, requestActions[index])
		}
		if store.Path != path || store.ExportToHost || store.ExportFormat != "" {
			t.Fatalf("request action %d = %#v, want internal store to %q", index, store, path)
		}
		if strings.Contains(store.Value, "request_metadata") {
			t.Fatalf("request action %d must store directly readable filter-state data, got %q", index, store.Value)
		}
	}

	preludeEvent, ok := requestActions[len(wantMetadataPaths)].(types.StoreAction)
	if !ok || preludeEvent.Path != wasmtypes.OCSFEventStorePath || !preludeEvent.ExportToHost ||
		preludeEvent.ExportFormat != types.StoreExportFormatProtobufValue {
		t.Fatalf("prelude event action = %#v, want one protobufValue host export", requestActions[len(wantMetadataPaths)])
	}
	if len(responseActions) != 1 {
		t.Fatalf("response actions = %d, want one grouped actor action", len(responseActions))
	}
	responseEvent, ok := responseActions[0].(types.StoreAction)
	if !ok || responseEvent.Path != wasmtypes.OCSFEventStorePath || !responseEvent.ExportToHost ||
		responseEvent.ExportFormat != types.StoreExportFormatProtobufValue {
		t.Fatalf("response event action = %#v, want one protobufValue host export", responseActions[0])
	}
	if strings.Contains(preludeEvent.Value, "auth.identity") || !strings.Contains(preludeEvent.Value, `"actor": {"user": {"type_id": 0}}`) {
		t.Fatalf("prelude event must use the unauthenticated fallback actor: %s", preludeEvent.Value)
	}
	if !strings.Contains(responseEvent.Value, "auth.identity.user.username") {
		t.Fatalf("response event must include the configured authenticated actor: %s", responseEvent.Value)
	}
	for _, want := range []string{
		"activity_id", "actor", "resources", "ai_model", "request.method", "request.protocol", "ocsf.http_request.url.path",
	} {
		if !strings.Contains(responseEvent.Value, want) {
			t.Fatalf("response event value does not contain %q: %s", want, responseEvent.Value)
		}
	}
	for _, unsupported := range []string{
		"isPresent(", "timestampToEpochMillis(", "durationToMillis(", "intToString(",
		"request.time", "request.duration", "response.code", "response.size", "int(", "string(",
	} {
		if strings.Contains(responseEvent.Value, unsupported) {
			t.Fatalf("OCSF StoreAction must not evaluate final stream fields or shim CEL conversions %q: %s", unsupported, responseEvent.Value)
		}
	}
	if strings.Contains(responseEvent.Value, "request.headers") || strings.Contains(responseEvent.Value, "Authorization") || strings.Contains(responseEvent.Value, "request.query") {
		t.Fatalf("OCSF event value must not export headers, credentials, or query data: %q", responseEvent.Value)
	}
}

func TestOCSFEventValueCELCompilesWithShimContract(t *testing.T) {
	config := &v1alpha1.OCSFLoggingConfig{
		Actor: v1alpha1.OCSFActorConfig{
			Identity: "auth.identity.user.username",
			UID:      "auth.identity.user.uid",
			Groups:   "auth.identity.user.groups",
		},
	}
	env, err := cel.NewEnv(
		cel.Variable("request", cel.DynType),
		cel.Variable("response", cel.DynType),
		cel.Variable("auth", cel.DynType),
		cel.Variable("ocsf", cel.DynType),
		celext.Strings(),
	)
	if err != nil {
		t.Fatalf("cel.NewEnv failed: %v", err)
	}
	_, issues := env.Compile(ocsfEventValue(config))
	if issues != nil && issues.Err() != nil {
		t.Fatalf("response OCSF event does not compile against standard CEL functions: %v", issues.Err())
	}

	preludeEnv, err := cel.NewEnv(
		cel.Variable("request", cel.DynType),
		cel.Variable("ocsf", cel.DynType),
		celext.Strings(),
	)
	if err != nil {
		t.Fatalf("cel.NewEnv for prelude failed: %v", err)
	}
	preludeEvent := ocsfEventValueWithActor(config, `{"type_id": 0}`)
	if strings.Contains(preludeEvent, "auth.identity") {
		t.Fatalf("prelude OCSF event must not depend on authorization metadata: %s", preludeEvent)
	}
	_, issues = preludeEnv.Compile(preludeEvent)
	if issues != nil && issues.Err() != nil {
		t.Fatalf("fallback OCSF event does not compile before authorization metadata exists: %v", issues.Err())
	}
}

func TestOCSFActorUserValueHandlesMissingParentsAndOptionalFields(t *testing.T) {
	actor := v1alpha1.OCSFActorConfig{
		Identity: "auth.identity.user.username",
		UID:      "auth.identity.user.uid",
		Groups:   "auth.identity.user.groups",
	}
	envOptions := []cel.EnvOption{cel.Variable("auth", cel.DynType), celext.Strings()}
	env, err := cel.NewEnv(envOptions...)
	if err != nil {
		t.Fatalf("cel.NewEnv failed: %v", err)
	}
	evaluate := func(expression string, auth map[string]any) celref.Val {
		t.Helper()
		ast, issues := env.Compile(expression)
		if issues != nil && issues.Err() != nil {
			t.Fatalf("failed to compile CEL expression %q: %v", expression, issues.Err())
		}
		program, err := env.Program(ast)
		if err != nil {
			t.Fatalf("failed to create CEL program for %q: %v", expression, err)
		}
		value, _, err := program.Eval(map[string]any{"auth": auth})
		if err != nil {
			t.Fatalf("failed to evaluate CEL expression %q: %v", expression, err)
		}
		return value
	}

	t.Run("missing parent returns false without error", func(t *testing.T) {
		predicate := evaluate(ocsfSelectorPresentPredicate(actor.Identity), map[string]any{
			"identity": map[string]any{},
		})
		if !celtypes.IsBool(predicate) || predicate != celtypes.False {
			t.Fatalf("identity predicate = %v (%T), want false", predicate, predicate)
		}
	})

	t.Run("missing optional selectors are omitted without error", func(t *testing.T) {
		auth := map[string]any{
			"identity": map[string]any{
				"user": map[string]any{"username": "alice"},
			},
		}
		predicate := evaluate(ocsfSelectorPresentPredicate(actor.Identity), auth)
		if !celtypes.IsBool(predicate) || predicate != celtypes.True {
			t.Fatalf("identity predicate = %v (%T), want true", predicate, predicate)
		}

		value := evaluate(ocsfActorUserValue(actor), auth).Value()
		user, ok := value.(map[celref.Val]celref.Val)
		if !ok {
			t.Fatalf("actor user value = %T, want CEL map", value)
		}
		field := func(key string) (any, bool) {
			value, exists := user[celtypes.String(key)]
			if !exists {
				return nil, false
			}
			return value.Value(), true
		}
		name, hasName := field("name")
		typeID, hasTypeID := field("type_id")
		if !hasName || !hasTypeID || name != "alice" || typeID != int64(1) {
			t.Fatalf("actor user name=%v type_id=%v, want alice and 1", name, typeID)
		}
		if _, ok := field("uid"); ok {
			t.Fatal("missing UID must be omitted")
		}
		if _, ok := field("groups"); ok {
			t.Fatal("missing groups must be omitted")
		}
	})

	t.Run("null identity and optional selectors are treated as absent", func(t *testing.T) {
		auth := map[string]any{
			"identity": map[string]any{
				"user": map[string]any{"username": nil, "uid": nil, "groups": nil},
			},
		}
		value := evaluate(ocsfActorUserValueOrUnknown(actor), auth).Value()
		user, ok := value.(map[celref.Val]celref.Val)
		if !ok {
			t.Fatalf("actor user value = %T, want CEL map", value)
		}
		if typeID, ok := user[celtypes.String("type_id")]; !ok || typeID.Value() != int64(0) {
			t.Fatalf("unknown actor type_id = %v, want 0", typeID)
		}
		if _, ok := user[celtypes.String("name")]; ok {
			t.Fatal("null identity must use the unknown-user representation")
		}

		auth["identity"].(map[string]any)["user"].(map[string]any)["username"] = "alice"
		value = evaluate(ocsfActorUserValue(actor), auth).Value()
		user, ok = value.(map[celref.Val]celref.Val)
		if !ok {
			t.Fatalf("actor user value = %T, want CEL map", value)
		}
		if _, ok := user[celtypes.String("uid")]; ok {
			t.Fatal("null UID must be omitted")
		}
		if _, ok := user[celtypes.String("groups")]; ok {
			t.Fatal("null groups must be omitted")
		}
	})

	t.Run("group names are mapped to OCSF group objects", func(t *testing.T) {
		auth := map[string]any{
			"identity": map[string]any{
				"user": map[string]any{
					"username": "alice",
					"groups":   []any{"admins", "operators"},
				},
			},
		}
		value := evaluate(ocsfActorUserValue(actor), auth).Value()
		user, ok := value.(map[celref.Val]celref.Val)
		if !ok {
			t.Fatalf("actor user value = %T, want CEL map", value)
		}
		groups, ok := user[celtypes.String("groups")].(traits.Lister)
		if !ok {
			t.Fatalf("groups value = %T, want CEL list", user[celtypes.String("groups")])
		}
		if groups.Size().Value() != int64(2) {
			t.Fatalf("groups size = %v, want 2", groups.Size())
		}
		first, ok := groups.Get(celtypes.Int(0)).Value().(map[celref.Val]celref.Val)
		if !ok {
			t.Fatalf("first group = %T, want CEL map", groups.Get(celtypes.Int(0)).Value())
		}
		if name, ok := first[celtypes.String("name")]; !ok || name.Value() != "admins" {
			t.Fatalf("first group name = %v, want admins", name)
		}
	})
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
