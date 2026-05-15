//go:build unit

package controller

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"

	celtypes "github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"gotest.tools/assert"
	"gotest.tools/assert/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	extpb "github.com/kuadrant/kuadrant-operator/pkg/extension/grpc/v1"
	exttypes "github.com/kuadrant/kuadrant-operator/pkg/extension/types"
)

type mockKuadrantCtx struct {
	resolveFn              func(ctx context.Context, policy exttypes.Policy, expression string, subscribe bool) (ref.Val, error)
	resolvePolicyFn        func(ctx context.Context, policy exttypes.Policy, expression string, subscribe bool) (exttypes.Policy, error)
	addDataToFn            func(ctx context.Context, policy exttypes.Policy, domain exttypes.Domain, binding string, expression string) error
	registerActionMethodFn func(ctx context.Context, policy exttypes.Policy, svc exttypes.ActionMethodConfig) error
}

type mockPolicy struct {
	name      string
	namespace string
}

func (m *mockPolicy) GetName() string                  { return m.name }
func (m *mockPolicy) GetNamespace() string             { return m.namespace }
func (m *mockPolicy) GetObjectKind() schema.ObjectKind { return &mockObjectKind{} }
func (m *mockPolicy) GetTargetRefs() []gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName {
	return nil
}

type mockObjectKind struct{}

func (m *mockObjectKind) SetGroupVersionKind(gvk schema.GroupVersionKind) {}
func (m *mockObjectKind) GroupVersionKind() schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: "kuadrant.io", Kind: "TestPolicy"}
}

type mockCelValue struct {
	pbPolicy *extpb.Policy
}

func (m *mockCelValue) ConvertToNative(typeDesc reflect.Type) (interface{}, error) {
	if typeDesc == reflect.TypeOf((*extpb.Policy)(nil)).Elem() {
		return m.pbPolicy, nil
	}
	return nil, fmt.Errorf("unsupported conversion to %v", typeDesc)
}

func (m *mockCelValue) ConvertToType(typeVal ref.Type) ref.Val { return m }
func (m *mockCelValue) Equal(other ref.Val) ref.Val            { return celtypes.Bool(false) }
func (m *mockCelValue) Type() ref.Type                         { return celtypes.TypeType }
func (m *mockCelValue) Value() interface{}                     { return m.pbPolicy }

func (m *mockKuadrantCtx) Resolve(ctx context.Context, policy exttypes.Policy, expression string, subscribe bool) (ref.Val, error) {
	return m.resolveFn(ctx, policy, expression, subscribe)
}

func (m *mockKuadrantCtx) ResolvePolicy(ctx context.Context, policy exttypes.Policy, expression string, subscribe bool) (exttypes.Policy, error) {
	if m.resolvePolicyFn != nil {
		return m.resolvePolicyFn(ctx, policy, expression, subscribe)
	}
	return nil, fmt.Errorf("resolvePolicyFn not implemented in mock")
}

func (m *mockKuadrantCtx) AddDataTo(ctx context.Context, policy exttypes.Policy, domain exttypes.Domain, binding string, expression string) error {
	return m.addDataToFn(ctx, policy, domain, binding, expression)
}

func (m *mockKuadrantCtx) GetClient() client.Client {
	return nil
}

func (m *mockKuadrantCtx) GetScheme() *runtime.Scheme {
	return &runtime.Scheme{}
}

func (m *mockKuadrantCtx) ReconcileObject(ctx context.Context, obj, desired client.Object, mutateFn exttypes.MutateFn) (client.Object, error) {
	return nil, nil
}

func (m *mockKuadrantCtx) RegisterActionMethod(ctx context.Context, policy exttypes.Policy, svc exttypes.ActionMethodConfig) error {
	if m.registerActionMethodFn != nil {
		return m.registerActionMethodFn(ctx, policy, svc)
	}
	return nil
}

func (m *mockKuadrantCtx) NewPipeline(policy exttypes.Policy) exttypes.Pipeline {
	return nil
}

func TestGenericResolveSuccess(t *testing.T) {
	mockCtx := &mockKuadrantCtx{
		resolveFn: func(ctx context.Context, policy exttypes.Policy, expression string, subscribe bool) (ref.Val, error) {
			return celtypes.Int(42), nil
		},
	}

	result, err := Resolve[int](context.Background(), mockCtx, nil, "some.expression", false)
	assert.NilError(t, err)
	assert.Equal(t, 42, result)
}

func TestGenericResolveTypeMismatch(t *testing.T) {
	mockCtx := &mockKuadrantCtx{
		resolveFn: func(ctx context.Context, policy exttypes.Policy, expression string, subscribe bool) (ref.Val, error) {
			return celtypes.String("not-an-int"), nil
		},
	}

	_, err := Resolve[int](context.Background(), mockCtx, nil, "some.expression", false)
	assert.ErrorContains(t, err, "unsupported native conversion")
}

func TestGenericResolveError(t *testing.T) {
	expectedErr := errors.New("resolve error")
	mockCtx := &mockKuadrantCtx{
		resolveFn: func(ctx context.Context, policy exttypes.Policy, expression string, subscribe bool) (ref.Val, error) {
			return nil, expectedErr
		},
	}

	_, err := Resolve[int](context.Background(), mockCtx, &mockPolicy{}, "expression", false)
	assert.Assert(t, err != nil)
	assert.Assert(t, cmp.Contains(err.Error(), expectedErr.Error()))
}

func TestResolvePolicy(t *testing.T) {
	expectedPbPolicy := &extpb.Policy{
		Metadata: &extpb.Metadata{
			Name:      "test-policy",
			Namespace: "test-namespace",
			Group:     "kuadrant.io",
			Kind:      "AuthPolicy",
		},
		TargetRefs: []*extpb.TargetRef{
			{
				Group:       "gateway.networking.k8s.io",
				Kind:        "Gateway",
				Name:        "test-gateway",
				Namespace:   "test-namespace",
				SectionName: "http",
			},
		},
	}

	mockCtx := &mockKuadrantCtx{
		resolveFn: func(ctx context.Context, policy exttypes.Policy, expression string, subscribe bool) (ref.Val, error) {
			return &mockCelValue{pbPolicy: expectedPbPolicy}, nil
		},
		resolvePolicyFn: func(ctx context.Context, policy exttypes.Policy, expression string, subscribe bool) (exttypes.Policy, error) {
			return extpb.NewPolicyAdapter(expectedPbPolicy), nil
		},
	}

	// returns types.Policy interface
	policy, err := mockCtx.ResolvePolicy(context.Background(), &mockPolicy{}, "self.findAuthPolicies()[0]", true)
	if err != nil {
		t.Logf("ResolvePolicy error: %v", err)
	}
	assert.Assert(t, err == nil)
	assert.Assert(t, policy != nil)

	assert.Equal(t, policy.GetName(), "test-policy")
	assert.Equal(t, policy.GetNamespace(), "test-namespace")
	assert.Equal(t, policy.GetObjectKind().GroupVersionKind().Group, "kuadrant.io")
	assert.Equal(t, policy.GetObjectKind().GroupVersionKind().Kind, "AuthPolicy")

	targetRefs := policy.GetTargetRefs()
	assert.Assert(t, cmp.Len(targetRefs, 1))
	assert.Equal(t, string(targetRefs[0].Group), "gateway.networking.k8s.io")
	assert.Equal(t, string(targetRefs[0].Kind), "Gateway")
	assert.Equal(t, string(targetRefs[0].Name), "test-gateway")
	assert.Equal(t, string(*targetRefs[0].SectionName), "http")
}

func TestPolicyAdapterMethods(t *testing.T) {
	pbPolicy := &extpb.Policy{
		Metadata: &extpb.Metadata{
			Name:      "auth-policy",
			Namespace: "default",
			Group:     "kuadrant.io",
			Kind:      "AuthPolicy",
		},
		TargetRefs: []*extpb.TargetRef{
			{
				Group:     "gateway.networking.k8s.io",
				Kind:      "HTTPRoute",
				Name:      "test-route",
				Namespace: "default",
			},
		},
	}

	adapter := extpb.NewPolicyAdapter(pbPolicy)

	assert.Equal(t, adapter.GetName(), "auth-policy")
	assert.Equal(t, adapter.GetNamespace(), "default")
	gvk := adapter.GetObjectKind().GroupVersionKind()
	assert.Equal(t, gvk.Group, "kuadrant.io")
	assert.Equal(t, gvk.Kind, "AuthPolicy")

	targetRefs := adapter.GetTargetRefs()
	assert.Assert(t, cmp.Len(targetRefs, 1))
	assert.Equal(t, string(targetRefs[0].Group), "gateway.networking.k8s.io")
	assert.Equal(t, string(targetRefs[0].Kind), "HTTPRoute")
	assert.Equal(t, string(targetRefs[0].Name), "test-route")
	assert.Assert(t, targetRefs[0].SectionName == nil)
}

func TestPolicyAdapterNilSafety(t *testing.T) {
	adapter := extpb.NewPolicyAdapter(nil)
	assert.Equal(t, adapter.GetName(), "")
	assert.Equal(t, adapter.GetNamespace(), "")
	assert.Assert(t, adapter.GetTargetRefs() == nil)

	pbPolicy := &extpb.Policy{
		Metadata: nil,
	}
	adapter = extpb.NewPolicyAdapter(pbPolicy)
	assert.Equal(t, adapter.GetName(), "")
	assert.Equal(t, adapter.GetNamespace(), "")
}

func mockReconcile(_ context.Context, _ reconcile.Request, _ exttypes.KuadrantCtx) (reconcile.Result, error) {
	return reconcile.Result{}, nil
}

func setupFakeArgs(t *testing.T, socketPath string) func() {
	t.Helper()
	originalArgs := os.Args
	os.Args = []string{"my-extension", socketPath}
	return func() { os.Args = originalArgs }
}

func TestBuilderMissingName(t *testing.T) {
	builder := &Builder{}
	_, err := builder.Build()
	assert.ErrorContains(t, err, "controller name must be set")
}

func TestBuilderMissingScheme(t *testing.T) {
	builder, _ := NewBuilder("test-controller")
	_, err := builder.Build()
	assert.ErrorContains(t, err, "scheme must be set")
}

func TestBuilderBuildMissingReconcile(t *testing.T) {
	builder, _ := NewBuilder("test-controller")
	_, err := builder.
		WithScheme(runtime.NewScheme()).
		Build()
	assert.ErrorContains(t, err, "reconcile function must be set")
}

func TestBuilderMissingForType(t *testing.T) {
	builder, _ := NewBuilder("test-controller")
	_, err := builder.
		WithScheme(runtime.NewScheme()).
		WithReconciler(mockReconcile).
		Build()
	assert.ErrorContains(t, err, "for type must be set")
}

func TestBuilderMissingSocketPath(t *testing.T) {
	originalArgs := os.Args
	defer func() { os.Args = originalArgs }()
	os.Args = []string{"my-extension"} // no socket path

	builder, _ := NewBuilder("test-controller")
	_, err := builder.
		WithScheme(runtime.NewScheme()).
		WithReconciler(mockReconcile).
		For(&corev1.Pod{}).
		Build()

	assert.ErrorContains(t, err, "missing socket path")
}

// mockExtensionServiceClient implements extpb.ExtensionServiceClient for testing.
type mockExtensionServiceClient struct {
	registerActionMethodFn func(ctx context.Context, in *extpb.RegisterActionMethodRequest, opts ...grpc.CallOption) (*emptypb.Empty, error)
	pipelineCommitFn       func(ctx context.Context, in *extpb.PipelineCommitRequest, opts ...grpc.CallOption) (*emptypb.Empty, error)
}

func (m *mockExtensionServiceClient) Ping(_ context.Context, _ *extpb.PingRequest, _ ...grpc.CallOption) (*extpb.PongResponse, error) {
	return nil, nil
}
func (m *mockExtensionServiceClient) Subscribe(_ context.Context, _ *extpb.SubscribeRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[extpb.SubscribeResponse], error) {
	return nil, nil
}
func (m *mockExtensionServiceClient) Resolve(_ context.Context, _ *extpb.ResolveRequest, _ ...grpc.CallOption) (*extpb.ResolveResponse, error) {
	return nil, nil
}
func (m *mockExtensionServiceClient) RegisterMutator(_ context.Context, _ *extpb.RegisterMutatorRequest, _ ...grpc.CallOption) (*emptypb.Empty, error) {
	return nil, nil
}
func (m *mockExtensionServiceClient) ClearPolicy(_ context.Context, _ *extpb.ClearPolicyRequest, _ ...grpc.CallOption) (*extpb.ClearPolicyResponse, error) {
	return nil, nil
}
func (m *mockExtensionServiceClient) RegisterActionMethod(ctx context.Context, in *extpb.RegisterActionMethodRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	if m.registerActionMethodFn != nil {
		return m.registerActionMethodFn(ctx, in, opts...)
	}
	return &emptypb.Empty{}, nil
}
func (m *mockExtensionServiceClient) PipelineCommit(ctx context.Context, in *extpb.PipelineCommitRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	if m.pipelineCommitFn != nil {
		return m.pipelineCommitFn(ctx, in, opts...)
	}
	return &emptypb.Empty{}, nil
}

func newTestExtensionController(mockClient *mockExtensionServiceClient) *ExtensionController {
	return &ExtensionController{
		extensionClient: &extensionClient{
			client: mockClient,
		},
	}
}

func TestRegisterActionMethod_Success(t *testing.T) {
	var capturedReq *extpb.RegisterActionMethodRequest
	mock := &mockExtensionServiceClient{
		registerActionMethodFn: func(_ context.Context, in *extpb.RegisterActionMethodRequest, _ ...grpc.CallOption) (*emptypb.Empty, error) {
			capturedReq = in
			return &emptypb.Empty{}, nil
		},
	}

	ec := newTestExtensionController(mock)
	policy := &mockPolicy{name: "my-policy", namespace: "default"}
	svc := exttypes.ActionMethodConfig{
		Name:            "assess-threat",
		URL:             "grpc://my-service:8081",
		Service:         "my.Service",
		Method:          "DoSomething",
		MessageTemplate: `ThreatRequest { uri: request.path }`,
	}

	err := ec.RegisterActionMethod(context.Background(), policy, svc)
	assert.NilError(t, err)
	assert.Equal(t, capturedReq.Name, "assess-threat")
	assert.Equal(t, capturedReq.Url, "grpc://my-service:8081")
	assert.Equal(t, capturedReq.Service, "my.Service")
	assert.Equal(t, capturedReq.Method, "DoSomething")
	assert.Equal(t, capturedReq.MessageTemplate, `ThreatRequest { uri: request.path }`)
	assert.Equal(t, capturedReq.Policy.Metadata.Name, "my-policy")
	assert.Equal(t, capturedReq.Policy.Metadata.Namespace, "default")
}

func TestRegisterActionMethod_Unavailable(t *testing.T) {
	mock := &mockExtensionServiceClient{
		registerActionMethodFn: func(_ context.Context, _ *extpb.RegisterActionMethodRequest, _ ...grpc.CallOption) (*emptypb.Empty, error) {
			return nil, status.Error(codes.Unavailable, "connection refused to grpc://my-service:8081")
		},
	}

	ec := newTestExtensionController(mock)
	policy := &mockPolicy{name: "my-policy", namespace: "default"}
	svc := exttypes.ActionMethodConfig{URL: "grpc://my-service:8081"}

	err := ec.RegisterActionMethod(context.Background(), policy, svc)
	assert.Assert(t, err != nil)
	assert.Assert(t, errors.Is(err, exttypes.ErrUpstreamUnreachable))
	assert.Assert(t, cmp.Contains(err.Error(), "connection refused"))
}

func TestRegisterActionMethod_OtherGRPCError(t *testing.T) {
	mock := &mockExtensionServiceClient{
		registerActionMethodFn: func(_ context.Context, _ *extpb.RegisterActionMethodRequest, _ ...grpc.CallOption) (*emptypb.Empty, error) {
			return nil, status.Error(codes.InvalidArgument, "bad request")
		},
	}

	ec := newTestExtensionController(mock)
	policy := &mockPolicy{name: "my-policy", namespace: "default"}
	svc := exttypes.ActionMethodConfig{URL: "grpc://my-service:8081"}

	err := ec.RegisterActionMethod(context.Background(), policy, svc)
	assert.Assert(t, err != nil)
	assert.Assert(t, !errors.Is(err, exttypes.ErrUpstreamUnreachable))
	// Should be the original gRPC error, not wrapped as ErrUpstreamUnreachable
	st, ok := status.FromError(err)
	assert.Assert(t, ok)
	assert.Equal(t, st.Code(), codes.InvalidArgument)
}

func TestNewPipeline_ReturnsNonNil(t *testing.T) {
	mock := &mockExtensionServiceClient{}
	ec := newTestExtensionController(mock)
	policy := &mockPolicy{name: "my-policy", namespace: "default"}

	pipeline := ec.NewPipeline(policy)
	assert.Assert(t, pipeline != nil)
}

func TestPipeline_AccumulatesBothPhases(t *testing.T) {
	p := &PipelineImpl{populatedVars: make(map[string]bool)}

	err := p.OnHTTPRequest(
		exttypes.GRPCMethodAction{
			Predicate: "true",
			Method:    "assess-threat",
			Var:       "threatResponse",
		},
		exttypes.DenyAction{
			Predicate: `request.url_path == "/blocked"`,
			DenyWith:  "403",
		},
	)
	assert.NilError(t, err)

	err = p.OnHTTPResponse(
		exttypes.DenyAction{
			Predicate: "threatResponse.threat_level >= 5",
			DenyWith:  "403",
		},
		exttypes.AddHeadersAction{
			HeadersToAdd: `{"x-threat-checked": "true"}`,
		},
	)
	assert.NilError(t, err)

	assert.Equal(t, len(p.actions), 4)
	assert.Equal(t, p.actions[0].phase, "request")
	assert.Equal(t, p.actions[1].phase, "request")
	assert.Equal(t, p.actions[2].phase, "response")
	assert.Equal(t, p.actions[3].phase, "response")
}

func TestPipeline_PhaseOrdering_RequestAfterResponse(t *testing.T) {
	p := &PipelineImpl{populatedVars: make(map[string]bool)}

	err := p.OnHTTPResponse(exttypes.AddHeadersAction{
		HeadersToAdd: `{"x-checked": "true"}`,
	})
	assert.NilError(t, err)

	err = p.OnHTTPRequest(exttypes.DenyAction{
		Predicate: "true",
		DenyWith:  "403",
	})
	assert.Assert(t, err != nil)
	assert.Assert(t, cmp.Contains(err.Error(), "cannot add request actions after response actions"))
}

func TestPipeline_VarAvailability_ForwardReference(t *testing.T) {
	p := &PipelineImpl{populatedVars: make(map[string]bool)}

	err := p.OnHTTPRequest(
		exttypes.DenyAction{
			Predicate: "threatResponse.threat_level >= 5",
			DenyWith:  "403",
		},
		exttypes.GRPCMethodAction{
			Method: "assess-threat",
			Var:    "threatResponse",
		},
	)
	assert.Assert(t, err != nil)
	assert.Assert(t, cmp.Contains(err.Error(), "references variable \"threatResponse\" before it is populated"))
}

func TestPipeline_VarAvailability_WithinCallValid(t *testing.T) {
	p := &PipelineImpl{populatedVars: make(map[string]bool)}

	err := p.OnHTTPRequest(
		exttypes.GRPCMethodAction{
			Method: "assess-threat",
			Var:    "threatResponse",
		},
		exttypes.DenyAction{
			Predicate: "threatResponse.threat_level >= 5",
			DenyWith:  "403",
		},
	)
	assert.NilError(t, err)
}

func TestPipeline_VarAvailability_CrossCallValid(t *testing.T) {
	p := &PipelineImpl{populatedVars: make(map[string]bool)}

	err := p.OnHTTPRequest(exttypes.GRPCMethodAction{
		Method: "assess-threat",
		Var:    "threatResponse",
	})
	assert.NilError(t, err)

	err = p.OnHTTPResponse(exttypes.DenyAction{
		Predicate: "threatResponse.threat_level >= 5",
		DenyWith:  "403",
	})
	assert.NilError(t, err)
}

func TestPipeline_DuplicateVarName(t *testing.T) {
	p := &PipelineImpl{populatedVars: make(map[string]bool)}

	err := p.OnHTTPRequest(
		exttypes.GRPCMethodAction{Method: "method-a", Var: "myVar"},
		exttypes.GRPCMethodAction{Method: "method-b", Var: "myVar"},
	)
	assert.Assert(t, err != nil)
	assert.Assert(t, cmp.Contains(err.Error(), "duplicate variable name \"myVar\""))
}

func TestPipeline_DuplicateVarName_AcrossCalls(t *testing.T) {
	p := &PipelineImpl{populatedVars: make(map[string]bool)}

	err := p.OnHTTPRequest(exttypes.GRPCMethodAction{Method: "method-a", Var: "myVar"})
	assert.NilError(t, err)

	err = p.OnHTTPRequest(exttypes.GRPCMethodAction{Method: "method-b", Var: "myVar"})
	assert.Assert(t, err != nil)
	assert.Assert(t, cmp.Contains(err.Error(), "duplicate variable name \"myVar\""))
}

func TestPipeline_NoVarReference_NoError(t *testing.T) {
	p := &PipelineImpl{populatedVars: make(map[string]bool)}

	err := p.OnHTTPRequest(
		exttypes.DenyAction{
			Predicate: `request.url_path == "/blocked"`,
			DenyWith:  "403",
		},
		exttypes.FailureAction{
			Predicate:      `request.headers["x-debug"] == "true"`,
			FailureMessage: "debug failure",
			FailureCode:    "500",
		},
	)
	assert.NilError(t, err)

	err = p.OnHTTPResponse(exttypes.AddHeadersAction{
		HeadersToAdd: `{"x-checked": "true"}`,
	})
	assert.NilError(t, err)
}

func TestPipeline_MultipleOnHTTPRequestCalls(t *testing.T) {
	p := &PipelineImpl{populatedVars: make(map[string]bool)}

	err := p.OnHTTPRequest(exttypes.DenyAction{Predicate: "true", DenyWith: "403"})
	assert.NilError(t, err)

	err = p.OnHTTPRequest(exttypes.DenyAction{Predicate: "false", DenyWith: "404"})
	assert.NilError(t, err)

	assert.Equal(t, len(p.actions), 2)
}

func TestPipeline_VarInHeadersToAdd(t *testing.T) {
	p := &PipelineImpl{populatedVars: make(map[string]bool)}

	err := p.OnHTTPRequest(exttypes.GRPCMethodAction{
		Method: "assess-threat",
		Var:    "threatResponse",
	})
	assert.NilError(t, err)

	err = p.OnHTTPResponse(exttypes.AddHeadersAction{
		HeadersToAdd: `{"x-threat-level": string(threatResponse.threat_level)}`,
	})
	assert.NilError(t, err)
}

func TestPipeline_VarInHeadersToAdd_ForwardReference(t *testing.T) {
	p := &PipelineImpl{populatedVars: make(map[string]bool)}

	err := p.OnHTTPRequest(
		exttypes.AddHeadersAction{
			HeadersToAdd: `{"x-threat-level": string(threatResponse.threat_level)}`,
		},
		exttypes.GRPCMethodAction{
			Method: "assess-threat",
			Var:    "threatResponse",
		},
	)
	assert.Assert(t, err != nil)
	assert.Assert(t, cmp.Contains(err.Error(), "references variable \"threatResponse\" before it is populated"))
}

func TestPipelineCommit_SendsAllActions(t *testing.T) {
	var capturedReq *extpb.PipelineCommitRequest
	mock := &mockExtensionServiceClient{
		pipelineCommitFn: func(_ context.Context, in *extpb.PipelineCommitRequest, _ ...grpc.CallOption) (*emptypb.Empty, error) {
			capturedReq = in
			return &emptypb.Empty{}, nil
		},
	}

	ec := newTestExtensionController(mock)
	pipeline := ec.NewPipeline(&mockPolicy{name: "my-policy", namespace: "default"})

	err := pipeline.OnHTTPRequest(
		exttypes.DenyAction{
			Predicate: `request.url_path == "/blocked"`,
			DenyWith:  "403",
		},
		exttypes.GRPCMethodAction{
			Predicate: "true",
			Method:    "assess-threat",
			Var:       "threatResponse",
		},
		exttypes.FailureAction{
			Predicate:      `request.headers["x-debug"] == "true"`,
			FailureMessage: "Request blocked",
			FailureCode:    "500",
		},
	)
	assert.NilError(t, err)

	err = pipeline.OnHTTPResponse(
		exttypes.DenyAction{
			Predicate: "threatResponse.threat_level >= 5",
			DenyWith:  "403",
		},
		exttypes.AddHeadersAction{
			HeadersToAdd: `{"x-threat-checked": "true"}`,
		},
	)
	assert.NilError(t, err)

	err = pipeline.Commit(context.Background())
	assert.NilError(t, err)
	assert.Assert(t, capturedReq != nil)
	assert.Equal(t, capturedReq.Policy.Metadata.Name, "my-policy")
	assert.Equal(t, capturedReq.Policy.Metadata.Namespace, "default")

	assert.Assert(t, cmp.Len(capturedReq.Actions, 5))

	assert.Equal(t, capturedReq.Actions[0].ActionType, extpb.ActionType_ACTION_TYPE_DENY)
	assert.Equal(t, capturedReq.Actions[0].Phase, "request")
	assert.Equal(t, capturedReq.Actions[0].DenyWith, "403")

	assert.Equal(t, capturedReq.Actions[1].ActionType, extpb.ActionType_ACTION_TYPE_GRPC_METHOD)
	assert.Equal(t, capturedReq.Actions[1].Phase, "request")
	assert.Equal(t, capturedReq.Actions[1].Method, "assess-threat")
	assert.Equal(t, capturedReq.Actions[1].Var, "threatResponse")

	assert.Equal(t, capturedReq.Actions[2].ActionType, extpb.ActionType_ACTION_TYPE_FAILURE)
	assert.Equal(t, capturedReq.Actions[2].Phase, "request")
	assert.Equal(t, capturedReq.Actions[2].FailureMessage, "Request blocked")
	assert.Equal(t, capturedReq.Actions[2].FailureCode, "500")

	assert.Equal(t, capturedReq.Actions[3].ActionType, extpb.ActionType_ACTION_TYPE_DENY)
	assert.Equal(t, capturedReq.Actions[3].Phase, "response")

	assert.Equal(t, capturedReq.Actions[4].ActionType, extpb.ActionType_ACTION_TYPE_ADD_HEADERS)
	assert.Equal(t, capturedReq.Actions[4].Phase, "response")
	assert.Equal(t, capturedReq.Actions[4].HeadersToAdd, `{"x-threat-checked": "true"}`)
}

func TestPipelineCommit_EmptyPipeline(t *testing.T) {
	var capturedReq *extpb.PipelineCommitRequest
	mock := &mockExtensionServiceClient{
		pipelineCommitFn: func(_ context.Context, in *extpb.PipelineCommitRequest, _ ...grpc.CallOption) (*emptypb.Empty, error) {
			capturedReq = in
			return &emptypb.Empty{}, nil
		},
	}

	ec := newTestExtensionController(mock)
	pipeline := ec.NewPipeline(&mockPolicy{name: "p", namespace: "ns"})

	err := pipeline.Commit(context.Background())
	assert.NilError(t, err)
	assert.Assert(t, capturedReq != nil)
	assert.Assert(t, cmp.Len(capturedReq.Actions, 0))
}

func TestPipelineCommit_PropagatesError(t *testing.T) {
	mock := &mockExtensionServiceClient{
		pipelineCommitFn: func(_ context.Context, _ *extpb.PipelineCommitRequest, _ ...grpc.CallOption) (*emptypb.Empty, error) {
			return nil, status.Error(codes.InvalidArgument, "bad action")
		},
	}

	ec := newTestExtensionController(mock)
	pipeline := ec.NewPipeline(&mockPolicy{name: "p", namespace: "ns"})
	_ = pipeline.OnHTTPRequest(exttypes.DenyAction{Predicate: "true", DenyWith: "403"})

	err := pipeline.Commit(context.Background())
	assert.Assert(t, err != nil)
	assert.Assert(t, cmp.Contains(err.Error(), "bad action"))
}
