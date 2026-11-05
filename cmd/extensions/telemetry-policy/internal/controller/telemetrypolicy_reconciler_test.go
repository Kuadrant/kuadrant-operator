//go:build unit

package controller

import (
	"context"
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
	if got, want := len(mock.pipeline.preludeActions), 3; got != want {
		t.Fatalf("request prelude actions = %d, want %d", got, want)
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
		if metadata.Path != path || !metadata.ExportToHost || metadata.ExportFormat != types.StoreExportFormatProtobufValue {
			t.Fatalf("request metadata store action = %#v, want typed host export at %q", metadata, path)
		}
		var required []string
		switch path {
		case wasmtypes.OCSFResourcesStorePath:
			required = []string{"name", "namespace", "LLMInferenceService"}
		case wasmtypes.OCSFAIModelStorePath:
			required = []string{"ai_provider", "KServe"}
		case wasmtypes.OCSFURLPathStorePath:
			required = []string{"request.url_path", "has(request.url_path) ? request.url_path"}
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

	actor, ok := mock.pipeline.responseActions[0].(types.StoreAction)
	if !ok {
		t.Fatalf("response action type = %T, want types.StoreAction", mock.pipeline.responseActions[0])
	}
	if actor.Path != ocsfActorUserStorePath || actor.Predicate != ocsfSelectorPresentPredicate("auth.identity.user.username") ||
		actor.Value != ocsfActorUserValue(pol.Spec.Logging.Default.OCSF.Actor) || !actor.ExportToHost ||
		actor.ExportFormat != types.StoreExportFormatProtobufValue {
		t.Fatalf("response actor action = %#v, want configured typed actor projection", actor)
	}

	if len(mock.resolveCalls) != 1 {
		t.Fatalf("Resolve calls = %d, want 1", len(mock.resolveCalls))
	}
	if !mock.resolveCalls[0].subscribe || mock.resolveCalls[0].expression != "self.findGateways().map(gateway, gateway.spec.gatewayClassName)" {
		t.Fatalf("Resolve call = %#v, want a subscribed Gateway topology lookup", mock.resolveCalls[0])
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

func TestOCSFActorUserValueGuardsOptionalSelectorsAndOmitsMissingFields(t *testing.T) {
	actor := v1alpha1.OCSFActorConfig{
		Identity: "auth.identity.user.username",
		UID:      "auth.identity.user.uid",
		Groups:   "auth.identity.user.groups",
	}
	expression := ocsfActorUserValue(actor)
	for _, expected := range []string{
		"has(auth.identity.user)",
		"has(auth.identity.user.uid)",
		"has(auth.identity.user.groups)",
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
	if strings.Contains(expression, "null") {
		t.Fatalf("optional actor fields must be omitted from grouped maps, not exported as null: %s", expression)
	}

	requiredOnly := ocsfActorUserValue(v1alpha1.OCSFActorConfig{Identity: actor.Identity})
	if strings.Contains(requiredOnly, actor.UID) || strings.Contains(requiredOnly, actor.Groups) {
		t.Fatalf("unconfigured optional actor fields must be absent from the value: %s", requiredOnly)
	}
}

func TestOCSFStoreActionsExportDirectTypedValues(t *testing.T) {
	config := &v1alpha1.OCSFLoggingConfig{
		Actor: v1alpha1.OCSFActorConfig{
			Identity: "auth.identity.user.username",
			UID:      "auth.identity.user.uid",
			Groups:   "auth.identity.user.groups",
		},
		Target: v1alpha1.OCSFTargetConfig{
			ResourceType: "model",
			RequestPath: v1alpha1.OCSFRequestPathTarget{
				NamespaceSegment: 0,
				NameSegment:      1,
			},
			AIProvider: "KServe",
		},
	}

	requestActions, responseActions := ocsfStoreActions(config)
	wantPaths := []string{
		wasmtypes.OCSFResourcesStorePath,
		wasmtypes.OCSFAIModelStorePath,
		wasmtypes.OCSFURLPathStorePath,
	}
	if len(requestActions) != len(wantPaths) {
		t.Fatalf("request actions = %d, want %d", len(requestActions), len(wantPaths))
	}
	for index, action := range requestActions {
		store, ok := action.(types.StoreAction)
		if !ok {
			t.Fatalf("request action %d = %T, want StoreAction", index, action)
		}
		if store.Path != wantPaths[index] || !store.ExportToHost || store.ExportFormat != types.StoreExportFormatProtobufValue {
			t.Fatalf("request action %d = %#v, want typed host export to %q", index, store, wantPaths[index])
		}
		if strings.Contains(store.Value, "request_metadata") {
			t.Fatalf("request action %d must export a directly readable filter-state value, got %q", index, store.Value)
		}
	}
	if len(responseActions) != 1 {
		t.Fatalf("response actions = %d, want one grouped actor action", len(responseActions))
	}
	actor, ok := responseActions[0].(types.StoreAction)
	if !ok || actor.Path != wasmtypes.OCSFActorUserStorePath || !actor.ExportToHost || actor.ExportFormat != types.StoreExportFormatProtobufValue {
		t.Fatalf("response actor action = %#v, want one grouped typed host export", responseActions[0])
	}
}

func TestOCSFActorUserValueHandlesMissingParentsAndOptionalFields(t *testing.T) {
	actor := v1alpha1.OCSFActorConfig{
		Identity: "auth.identity.user.username",
		UID:      "auth.identity.user.uid",
		Groups:   "auth.identity.user.groups",
	}
	env, err := cel.NewEnv(cel.Variable("auth", cel.DynType))
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
