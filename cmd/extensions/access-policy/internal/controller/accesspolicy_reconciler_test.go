//go:build unit

package controller

import (
	"context"
	"testing"

	celref "github.com/google/cel-go/common/types/ref"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	"github.com/kuadrant/kuadrant-operator/cmd/extensions/access-policy/api/v1alpha1"
	exttypes "github.com/kuadrant/kuadrant-operator/pkg/extension/types"
	extutils "github.com/kuadrant/kuadrant-operator/pkg/extension/utils"
)

type testKuadrantCtx struct {
	reconcileObjectFn func(ctx context.Context, sampleObj, desiredObj ctrlclient.Object, mutator exttypes.MutateFn) (ctrlclient.Object, error)
}

func (m *testKuadrantCtx) Resolve(ctx context.Context, policy exttypes.Policy, expr string, sub bool) (celref.Val, error) {
	return nil, nil
}
func (m *testKuadrantCtx) ResolvePolicy(ctx context.Context, policy exttypes.Policy, expr string, sub bool) (exttypes.Policy, error) {
	return nil, nil
}
func (m *testKuadrantCtx) AddDataTo(ctx context.Context, policy exttypes.Policy, domain exttypes.Domain, binding, expr string) error {
	return nil
}
func (m *testKuadrantCtx) ReconcileObject(ctx context.Context, sampleObj, desiredObj ctrlclient.Object, mutator exttypes.MutateFn) (ctrlclient.Object, error) {
	if m.reconcileObjectFn != nil {
		return m.reconcileObjectFn(ctx, sampleObj, desiredObj, mutator)
	}
	return nil, nil
}
func (m *testKuadrantCtx) RegisterActionMethod(ctx context.Context, policy exttypes.Policy, svc exttypes.ActionMethodConfig) error {
	return nil
}
func (m *testKuadrantCtx) NewPipeline(policy exttypes.Policy) exttypes.Pipeline {
	return nil
}

func setupTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := gatewayapiv1.Install(s); err != nil {
		t.Fatalf("failed to install gatewayapiv1 scheme: %v", err)
	}
	if err := gatewayapiv1alpha2.Install(s); err != nil {
		t.Fatalf("failed to install gatewayapiv1alpha2 scheme: %v", err)
	}
	if err := kuadrantv1.AddToScheme(s); err != nil {
		t.Fatalf("failed to add kuadrantv1 scheme: %v", err)
	}
	if err := v1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("failed to add v1alpha1 scheme: %v", err)
	}
	return s
}

func makeTestContext(ctx context.Context, c ctrlclient.Client, s *runtime.Scheme) context.Context {
	ctx = context.WithValue(ctx, extutils.ClientKey, c)
	ctx = context.WithValue(ctx, extutils.SchemeKey, s)
	return ctx
}

func TestReconcile_NoGateway(t *testing.T) {
	scheme := setupTestScheme(t)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	ctx := makeTestContext(context.Background(), fakeClient, scheme)

	r := NewAccessPolicyReconciler()

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Namespace: "default",
			Name:      "test-gateway",
		},
	}

	res, err := r.Reconcile(ctx, req, &testKuadrantCtx{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Requeue {
		t.Errorf("expected no requeue")
	}
}

func TestReconcile_GatewayNoPolicies_DeletesAuthPolicy(t *testing.T) {
	scheme := setupTestScheme(t)
	gw := &gatewayapiv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gateway",
			Namespace: "default",
		},
	}
	authPolicy := &kuadrantv1.AuthPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gateway-auth",
			Namespace: "default",
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gw, authPolicy).Build()
	ctx := makeTestContext(context.Background(), fakeClient, scheme)

	r := NewAccessPolicyReconciler()

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Namespace: "default",
			Name:      "test-gateway",
		},
	}

	_, err := r.Reconcile(ctx, req, &testKuadrantCtx{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var existingAuthPolicy kuadrantv1.AuthPolicy
	err = fakeClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "test-gateway-auth"}, &existingAuthPolicy)
	if err == nil {
		t.Errorf("expected AuthPolicy to be deleted, but it was found")
	}
}

func TestReconcile_ValidAccessPolicy_CreatesAuthPolicy(t *testing.T) {
	scheme := setupTestScheme(t)
	gw := &gatewayapiv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gateway",
			Namespace: "default",
		},
	}

	spiffeID := v1alpha1.AuthorizationSourceSPIFFE("spiffe://cluster.local/ns/default/sa/agent-sa")
	policy := &v1alpha1.AccessPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-access-policy",
			Namespace: "default",
		},
		Spec: v1alpha1.AccessPolicySpec{
			TargetRefs: []gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
				{
					LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
						Group: "gateway.networking.k8s.io",
						Kind:  "Gateway",
						Name:  "test-gateway",
					},
				},
			},
			Action: v1alpha1.ActionTypeAllow,
			Rules: []v1alpha1.AccessRule{
				{
					Name: "allow-sa-agent",
					Source: v1alpha1.AccessRuleSource{
						Type: v1alpha1.AuthorizationSourceTypeServiceAccount,
						ServiceAccount: &v1alpha1.AuthorizationSourceServiceAccount{
							Namespace: "default",
							Name:      "agent-sa",
						},
					},
					Authorization: &v1alpha1.AuthorizationRule{
						Type: v1alpha1.AuthorizationRuleTypeInline,
						MCP: v1alpha1.MCPAttributes{
							MCPBaseProtocolMethodsOption: v1alpha1.MCPBaseProtocolMethodsOptionMatch,
							Methods: []v1alpha1.MCPMethod{
								{
									Name:   "tools/call",
									Params: []v1alpha1.MCPMethodParam{"get-sum"},
								},
							},
						},
					},
				},
				{
					Name: "allow-spiffe-agent",
					Source: v1alpha1.AccessRuleSource{
						Type:   v1alpha1.AuthorizationSourceTypeSPIFFE,
						SPIFFE: &spiffeID,
					},
					Authorization: &v1alpha1.AuthorizationRule{
						Type: v1alpha1.AuthorizationRuleTypeCEL,
						CEL: &v1alpha1.AccessPolicyCELRule{
							Expression: "request.mcp.tool_name == 'get-sum'",
						},
					},
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(gw, policy).
		WithStatusSubresource(policy).
		Build()
	ctx := makeTestContext(context.Background(), fakeClient, scheme)

	r := NewAccessPolicyReconciler()

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Namespace: "default",
			Name:      "test-gateway",
		},
	}

	kuadrantCtx := &testKuadrantCtx{
		reconcileObjectFn: func(ctx context.Context, sampleObj, desiredObj ctrlclient.Object, mutator exttypes.MutateFn) (ctrlclient.Object, error) {
			desiredAuthPolicy, ok := desiredObj.(*kuadrantv1.AuthPolicy)
			if !ok {
				t.Fatalf("expected AuthPolicy")
			}
			existingAuthPolicy := &kuadrantv1.AuthPolicy{}
			err := fakeClient.Get(ctx, ctrlclient.ObjectKeyFromObject(desiredAuthPolicy), existingAuthPolicy)
			if err != nil {
				if err := fakeClient.Create(ctx, desiredAuthPolicy); err != nil {
					return nil, err
				}
				return desiredAuthPolicy, nil
			}
			update, err := mutator(existingAuthPolicy, desiredAuthPolicy)
			if err != nil {
				return nil, err
			}
			if update {
				if err := fakeClient.Update(ctx, existingAuthPolicy); err != nil {
					return nil, err
				}
			}
			return existingAuthPolicy, nil
		},
	}

	_, err := r.Reconcile(ctx, req, kuadrantCtx)
	if err != nil {
		t.Fatalf("unexpected reconcile error: %v", err)
	}

	var createdAuthPolicy kuadrantv1.AuthPolicy
	err = fakeClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "test-gateway-auth"}, &createdAuthPolicy)
	if err != nil {
		t.Fatalf("failed to get created AuthPolicy: %v", err)
	}

	if createdAuthPolicy.Spec.AuthScheme == nil {
		t.Fatalf("expected AuthScheme in AuthPolicy")
	}

	if _, ok := createdAuthPolicy.Spec.AuthScheme.Authentication["service-account"]; !ok {
		t.Errorf("expected service-account authentication spec")
	}
	if _, ok := createdAuthPolicy.Spec.AuthScheme.Authentication["spiffe"]; !ok {
		t.Errorf("expected spiffe authentication spec")
	}

	if _, ok := createdAuthPolicy.Spec.AuthScheme.Authorization["test-access-policy-allow-sa-agent"]; !ok {
		t.Errorf("expected allow-sa-agent authorization spec in AuthPolicy")
	}
	if _, ok := createdAuthPolicy.Spec.AuthScheme.Authorization["test-access-policy-allow-spiffe-agent"]; !ok {
		t.Errorf("expected allow-spiffe-agent authorization spec in AuthPolicy")
	}

	if _, ok := createdAuthPolicy.Spec.AuthScheme.Authorization["fail-close"]; !ok {
		t.Errorf("expected fail-close authorization spec in AuthPolicy")
	}

	var updatedPolicy v1alpha1.AccessPolicy
	err = fakeClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "test-access-policy"}, &updatedPolicy)
	if err != nil {
		t.Fatalf("failed to get updated AccessPolicy: %v", err)
	}

	if len(updatedPolicy.Status.Ancestors) == 0 {
		t.Fatalf("expected ancestors in AccessPolicy status")
	}
	cond := updatedPolicy.Status.Ancestors[0].Conditions[0]
	if cond.Status != metav1.ConditionTrue || cond.Reason != string(v1alpha1.PolicyReasonAccepted) {
		t.Errorf("expected status Accepted=True, got status=%s, reason=%s", cond.Status, cond.Reason)
	}
}

func TestReconcile_ExternalAuthAction_UpdatesStatusFalse(t *testing.T) {
	scheme := setupTestScheme(t)
	gw := &gatewayapiv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gateway",
			Namespace: "default",
		},
	}

	policy := &v1alpha1.AccessPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "external-auth-policy",
			Namespace: "default",
		},
		Spec: v1alpha1.AccessPolicySpec{
			TargetRefs: []gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
				{
					LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
						Group: "gateway.networking.k8s.io",
						Kind:  "Gateway",
						Name:  "test-gateway",
					},
				},
			},
			Action: v1alpha1.ActionTypeExternalAuth,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(gw, policy).
		WithStatusSubresource(policy).
		Build()
	ctx := makeTestContext(context.Background(), fakeClient, scheme)

	r := NewAccessPolicyReconciler()

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Namespace: "default",
			Name:      "test-gateway",
		},
	}

	kuadrantCtx := &testKuadrantCtx{}
	_, err := r.Reconcile(ctx, req, kuadrantCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var updatedPolicy v1alpha1.AccessPolicy
	err = fakeClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "external-auth-policy"}, &updatedPolicy)
	if err != nil {
		t.Fatalf("failed to get AccessPolicy: %v", err)
	}

	if len(updatedPolicy.Status.Ancestors) == 0 {
		t.Fatalf("expected ancestors status")
	}
	cond := updatedPolicy.Status.Ancestors[0].Conditions[0]
	if cond.Status != metav1.ConditionFalse || cond.Reason != "Invalid" {
		t.Errorf("expected status Accepted=False/Invalid for ExternalAuth, got status=%s, reason=%s", cond.Status, cond.Reason)
	}
}

func TestReconcile_InvalidCEL_UpdatesStatusFalse(t *testing.T) {
	scheme := setupTestScheme(t)
	gw := &gatewayapiv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gateway",
			Namespace: "default",
		},
	}

	policy := &v1alpha1.AccessPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "invalid-cel-policy",
			Namespace: "default",
		},
		Spec: v1alpha1.AccessPolicySpec{
			TargetRefs: []gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
				{
					LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
						Group: "gateway.networking.k8s.io",
						Kind:  "Gateway",
						Name:  "test-gateway",
					},
				},
			},
			Action: v1alpha1.ActionTypeAllow,
			Rules: []v1alpha1.AccessRule{
				{
					Name: "bad-cel-rule",
					Authorization: &v1alpha1.AuthorizationRule{
						Type: v1alpha1.AuthorizationRuleTypeCEL,
						CEL: &v1alpha1.AccessPolicyCELRule{
							Expression: "invalid syntax (((",
						},
					},
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(gw, policy).
		WithStatusSubresource(policy).
		Build()
	ctx := makeTestContext(context.Background(), fakeClient, scheme)

	r := NewAccessPolicyReconciler()

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Namespace: "default",
			Name:      "test-gateway",
		},
	}

	kuadrantCtx := &testKuadrantCtx{}
	_, err := r.Reconcile(ctx, req, kuadrantCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var updatedPolicy v1alpha1.AccessPolicy
	err = fakeClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "invalid-cel-policy"}, &updatedPolicy)
	if err != nil {
		t.Fatalf("failed to get AccessPolicy: %v", err)
	}

	if len(updatedPolicy.Status.Ancestors) == 0 {
		t.Fatalf("expected ancestors status")
	}
	cond := updatedPolicy.Status.Ancestors[0].Conditions[0]
	if cond.Status != metav1.ConditionFalse || cond.Reason != string(v1alpha1.PolicyReasonInvalidCEL) {
		t.Errorf("expected status Accepted=False/InvalidCEL, got status=%s, reason=%s", cond.Status, cond.Reason)
	}
}
