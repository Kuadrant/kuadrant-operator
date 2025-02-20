package controllers

import (
	"context"
	"sync"

	authorinov1beta3 "github.com/kuadrant/authorino/api/v1beta3"
	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

//+kubebuilder:rbac:groups=kuadrant.io,resources=planpolicies,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=kuadrant.io,resources=planpolicies/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=kuadrant.io,resources=planpolicies/finalizers,verbs=update

//+kubebuilder:rbac:groups=kuadrant.io,resources=ratelimitpolicies,verbs=create;delete

type PlanPolicyReconciler struct {
	Client *dynamic.DynamicClient
	scheme *runtime.Scheme
}

func NewPlanPolicyReconciler(client *dynamic.DynamicClient, scheme *runtime.Scheme) *PlanPolicyReconciler {

	return &PlanPolicyReconciler{Client: client, scheme: scheme}
}

func (r *PlanPolicyReconciler) Subscription() *controller.Subscription {
	return &controller.Subscription{
		ReconcileFunc: r.Reconcile,
		Events: []controller.ResourceEventMatcher{
			{Kind: &kuadrantv1alpha1.PlanPolicyGroupKind},
		},
	}
}

func (r *PlanPolicyReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, _ *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("PlanPolicyReconciler")
	logger.Info("generating rate limit policy from plan policy", "status", "started")
	defer logger.Info("generating rate limit policy from plan policy", "status", "completed")

	// generate name
	// "extract" targetref
	// "find auth/identpolicy"
	// update response field
	// status

	policies := lo.FilterMap(topology.Policies().Items(), func(item machinery.Policy, _ int) (*kuadrantv1alpha1.PlanPolicy, bool) {
		p, ok := item.(*kuadrantv1alpha1.PlanPolicy)
		return p, ok
	})

	for _, policy := range policies {
		pLogger := logger.WithValues("policy", policy.GetLocator())

		if policy.GetDeletionTimestamp() != nil {
			pLogger.V(1).Info("policy marked for deletion, skipping")
			continue
		}

		// what about if identpolicy

		var authPolicy *kuadrantv1.AuthPolicy
		for _, child := range topology.Policies().Children(policy) {
			ap, ok := child.(*kuadrantv1.AuthPolicy)
			if ok {
				authPolicy = ap
				break
			}
		}
		if authPolicy == nil {
			// we didn't find a child authpolicy?
			continue
		}

		// update authpolicy response predicate

		desiredAuthPolicy := r.buildDesiredAuthPolicy(policy, authPolicy)
		// compare to check if they're equal? otherwise update?

		apResource := r.Client.Resource(kuadrantv1.AuthPoliciesResource).Namespace(desiredAuthPolicy.GetNamespace())

		found := false
		if !found {
			desiredApUnstructured, err := controller.Destruct(desiredAuthPolicy)
			if err != nil {
				logger.Error(err, "failed to destruct ap object", "rlp", desiredApUnstructured)
				continue
			}
			if _, err = apResource.Update(ctx, desiredApUnstructured, metav1.UpdateOptions{}); err != nil {
				logger.Error(err, "failed to update ap object", "rlp", desiredApUnstructured.Object)
			}
		}

		rlp := r.buildDesiredRateLimitPolicy(policy, authPolicy.Spec.TargetRef)
		if err := controllerutil.SetControllerReference(policy, rlp, r.scheme); err != nil {
			pLogger.Error(err, "failed to set owner reference on desired ratelimitpolicy")
			continue
		}

		rlpResource := r.Client.Resource(kuadrantv1.RateLimitPoliciesResource).Namespace(rlp.GetNamespace())

		// get existing
		found = false

		if !found {

			desiredRlpUnstructured, err := controller.Destruct(rlp)
			if err != nil {
				logger.Error(err, "failed to destruct rlp object", "rlp", desiredRlpUnstructured)
				continue
			}
			if _, err = rlpResource.Create(ctx, desiredRlpUnstructured, metav1.CreateOptions{}); err != nil {
				logger.Error(err, "failed to create rlp object", "rlp", desiredRlpUnstructured.Object)
			}
		}
		// otherwise update?

	}

	return nil
}

func (r *PlanPolicyReconciler) buildDesiredAuthPolicy(planPolicy *kuadrantv1alpha1.PlanPolicy, existing *kuadrantv1.AuthPolicy) *kuadrantv1.AuthPolicy {
	ensureAuthPolicyInitialized(existing)

	existing.Spec.AuthPolicySpecProper.AuthScheme.Response.Success.DynamicMetadata["kuadrant"] = kuadrantv1.MergeableSuccessResponseSpec{
		SuccessResponseSpec: authorinov1beta3.SuccessResponseSpec{
			AuthResponseMethodSpec: authorinov1beta3.AuthResponseMethodSpec{
				Json: &authorinov1beta3.JsonAuthResponseSpec{
					Properties: map[string]authorinov1beta3.ValueOrSelector{
						"plan_tier": {
							Expression: planPolicy.ToCelExpression(),
						},
					},
				},
			},
		},
	}
	return existing
}

func ensureAuthPolicyInitialized(existing *kuadrantv1.AuthPolicy) {
	if existing.Spec.AuthPolicySpecProper.AuthScheme == nil {
		existing.Spec.AuthPolicySpecProper.AuthScheme = &kuadrantv1.AuthSchemeSpec{}
	}
	if existing.Spec.AuthPolicySpecProper.AuthScheme.Response == nil {
		existing.Spec.AuthPolicySpecProper.AuthScheme.Response = &kuadrantv1.MergeableResponseSpec{}
	}
	if existing.Spec.AuthPolicySpecProper.AuthScheme.Response.Success.DynamicMetadata == nil {
		existing.Spec.AuthPolicySpecProper.AuthScheme.Response.Success.DynamicMetadata = make(map[string]kuadrantv1.MergeableSuccessResponseSpec)
	}
}

func (r *PlanPolicyReconciler) buildDesiredRateLimitPolicy(planPolicy *kuadrantv1alpha1.PlanPolicy, targetRef gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName) *kuadrantv1.RateLimitPolicy {
	return &kuadrantv1.RateLimitPolicy{
		TypeMeta: metav1.TypeMeta{
			Kind:       "RateLimitPolicy",
			APIVersion: kuadrantv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      planPolicy.GetName(),
			Namespace: planPolicy.GetNamespace(),
		},
		Spec: kuadrantv1.RateLimitPolicySpec{
			TargetRef: targetRef,
			RateLimitPolicySpecProper: kuadrantv1.RateLimitPolicySpecProper{
				Limits: planPolicy.ToRateLimits(),
			},
		},
	}
}
