package controllers

import (
	"context"
	"reflect"
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

	// todo(adam-cattermole): either calculate name of authpolicy linked to ident
	// 	or find the link in the topology

	policies := lo.FilterMap(topology.Policies().Items(), func(item machinery.Policy, _ int) (*kuadrantv1alpha1.PlanPolicy, bool) {
		p, ok := item.(*kuadrantv1alpha1.PlanPolicy)
		return p, ok
	})

	for _, policy := range policies {
		pLogger := logger.WithValues("policy", policy.GetLocator())

		// Find existing AP
		authPolicyObj, found := lo.Find(topology.Policies().Children(policy), func(child machinery.Policy) bool {
			return child.GroupVersionKind().GroupKind() == kuadrantv1.AuthPolicyGroupKind
		})
		if !found {
			pLogger.V(1).Info("targeted authpolicy not present, skipping")
			continue
		}
		existingAuthPolicy := authPolicyObj.(*kuadrantv1.AuthPolicy)

		// Update AP
		desiredAuthPolicy := r.buildDesiredAuthPolicy(policy, existingAuthPolicy)
		//todo(adam-cattermole): if planpolicy is deleted we should revert the change to the AP
		if reflect.DeepEqual(existingAuthPolicy.Spec, desiredAuthPolicy.Spec) {
			pLogger.Info("authpolicy is up to date, nothing to do")
		} else {
			apResource := r.Client.Resource(kuadrantv1.AuthPoliciesResource).Namespace(desiredAuthPolicy.GetNamespace())
			desiredApUnstructured, err := controller.Destruct(desiredAuthPolicy)
			if err != nil {
				pLogger.Error(err, "failed to destruct authpolicy object", "authpolicy", desiredApUnstructured)
				continue
			}
			if _, err = apResource.Update(ctx, desiredApUnstructured, metav1.UpdateOptions{}); err != nil {
				pLogger.Error(err, "failed to update authpolicy object", "authpolicy", desiredApUnstructured.Object)
			}
		}

		// Find existing RLP
		rateLimitPolicyObj, found := lo.Find(topology.Policies().Children(policy), func(child machinery.Policy) bool {
			return child.GroupVersionKind().GroupKind() == kuadrantv1.RateLimitPolicyGroupKind
		})

		desiredRateLimitPolicy := r.buildDesiredRateLimitPolicy(policy, existingAuthPolicy.Spec.TargetRef)
		rlpResource := r.Client.Resource(kuadrantv1.RateLimitPoliciesResource).Namespace(desiredRateLimitPolicy.GetNamespace())

		// Update RLP
		if found {
			existingRateLimitPolicy := rateLimitPolicyObj.(*kuadrantv1.RateLimitPolicy)
			if reflect.DeepEqual(existingRateLimitPolicy.Spec, desiredRateLimitPolicy.Spec) {
				pLogger.Info("ratelimitpolicy is up to date, nothing to do")
				continue
			}
			existingRateLimitPolicy.Spec = desiredRateLimitPolicy.Spec
			existingRlpUnstructured, err := controller.Destruct(existingRateLimitPolicy)
			if err != nil {
				pLogger.Error(err, "failed to destruct ratelimitpolicy object", "ratelimitpolicy", existingRateLimitPolicy)
				continue
			}
			if _, err = rlpResource.Update(ctx, existingRlpUnstructured, metav1.UpdateOptions{}); err != nil {
				pLogger.Error(err, "failed to update ratelimitpolicy object", "ratelimitpolicy", existingRlpUnstructured.Object)
			}
			continue
		}

		// Create RLP
		if err := controllerutil.SetControllerReference(policy, desiredRateLimitPolicy, r.scheme); err != nil {
			pLogger.Error(err, "failed to set owner reference on desired ratelimitpolicy")
			continue
		}
		desiredRlpUnstructured, err := controller.Destruct(desiredRateLimitPolicy)
		if err != nil {
			pLogger.Error(err, "failed to destruct ratelimitpolicy object", "ratelimitpolicy", desiredRlpUnstructured)
			continue
		}
		if _, err = rlpResource.Create(ctx, desiredRlpUnstructured, metav1.CreateOptions{}); err != nil {
			pLogger.Error(err, "failed to create ratelimitpolicy object", "ratelimitpolicy", desiredRlpUnstructured.Object)
		}
		// todo(adam-cattermole): status
	}

	return nil
}

func (r *PlanPolicyReconciler) buildDesiredAuthPolicy(planPolicy *kuadrantv1alpha1.PlanPolicy, existing *kuadrantv1.AuthPolicy) *kuadrantv1.AuthPolicy {
	desired := existing.DeepCopy()

	ensureAuthPolicyInitialized(desired)
	desired.Spec.AuthPolicySpecProper.AuthScheme.Response.Success.DynamicMetadata["kuadrant"] = kuadrantv1.MergeableSuccessResponseSpec{
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
	return desired
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
