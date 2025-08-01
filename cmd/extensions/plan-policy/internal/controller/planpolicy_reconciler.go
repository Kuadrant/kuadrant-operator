package controller

import (
	"context"
	"fmt"
	"reflect"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/pkg/extension/types"
)

// +kubebuilder:rbac:groups=kuadrant.io,resources=planpolicies,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=kuadrant.io,resources=planpolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kuadrant.io,resources=planpolicies/finalizers,verbs=update

// +kubebuilder:rbac:groups=kuadrant.io,resources=ratelimitpolicies,verbs=create;delete

type PlanPolicyReconciler struct {
	types.ExtensionBase
}

func NewPlanPolicyReconciler() *PlanPolicyReconciler {
	return &PlanPolicyReconciler{}
}

func (r *PlanPolicyReconciler) Reconcile(ctx context.Context, request reconcile.Request, kuadrantCtx types.KuadrantCtx) (reconcile.Result, error) {
	if err := r.Configure(ctx); err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to configure extension: %w", err)
	}
	r.Logger.Info("reconciling planpolicies started")
	defer r.Logger.Info("reconciling planpolicies completed")

	planPolicy := &kuadrantv1alpha1.PlanPolicy{}
	if err := r.Client.Get(ctx, request.NamespacedName, planPolicy); err != nil {
		if errors.IsNotFound(err) {
			r.Logger.Error(err, "planpolicy not found")
			return reconcile.Result{}, nil
		}
		r.Logger.Error(err, "failed to retrieve planpolicy")
		return reconcile.Result{}, err
	}

	if planPolicy.GetDeletionTimestamp() != nil {
		r.Logger.Info("planpolicy marked for deletion")
		return reconcile.Result{}, nil
	}

	authPolicy, err := kuadrantCtx.ResolvePolicy(ctx, planPolicy,
		`self.findAuthPolicies()[0]`, true)
	if err != nil {
		r.Logger.Error(err, "failed to resolve policy")
		return reconcile.Result{}, err
	}

	desiredRateLimitPolicy := r.buildDesiredRateLimitPolicy(planPolicy, authPolicy.GetTargetRefs()[0])
	if err := controllerutil.SetControllerReference(planPolicy, desiredRateLimitPolicy, r.Scheme); err != nil {
		r.Logger.Error(err, "failed to set controller reference")
		return reconcile.Result{}, err
	}
	if err := kuadrantCtx.ReconcileObject(ctx, &kuadrantv1.RateLimitPolicy{}, desiredRateLimitPolicy, rlpSpecMutator); err != nil {
		r.Logger.Error(err, "failed to reconcile desired ratelimitpolicy")
		return reconcile.Result{}, err
	}

	r.Logger.Info("cel expression", "expression", planPolicy.BuildCelExpression())

	err = kuadrantCtx.AddDataTo(ctx, planPolicy, authPolicy, "plan", planPolicy.BuildCelExpression())
	if err != nil {
		r.Logger.Error(err, "failed to add data to policy", "policy", authPolicy)
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func (r *PlanPolicyReconciler) buildDesiredRateLimitPolicy(planPolicy *kuadrantv1alpha1.PlanPolicy, targetRef gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName) *kuadrantv1.RateLimitPolicy {
	return &kuadrantv1.RateLimitPolicy{
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

func rlpSpecMutator(existingObj, desiredObj client.Object) (bool, error) {
	var update bool
	existing, ok := existingObj.(*kuadrantv1.RateLimitPolicy)
	if !ok {
		return false, fmt.Errorf("%T is not a *kuadrantv1.RateLimitPolicy", existingObj)
	}
	desired, ok := desiredObj.(*kuadrantv1.RateLimitPolicy)
	if !ok {
		return false, fmt.Errorf("%T is not a *kuadrantv1.RateLimitPolicy", desiredObj)
	}
	if !reflect.DeepEqual(desired.Spec.TargetRef, existing.Spec.TargetRef) {
		existing.Spec.TargetRef = desired.Spec.TargetRef
		update = true
	}
	if !reflect.DeepEqual(desired.Spec.RateLimitPolicySpecProper, existing.Spec.RateLimitPolicySpecProper) {
		existing.Spec.RateLimitPolicySpecProper = desired.Spec.RateLimitPolicySpecProper
		update = true
	}
	return update, nil
}
