package controller

import (
	"context"
	"fmt"
	"reflect"
	"slices"

	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	"github.com/kuadrant/kuadrant-operator/cmd/extensions/plan-policy/api/v1alpha1"
	extcontroller "github.com/kuadrant/kuadrant-operator/pkg/extension/controller"
	"github.com/kuadrant/kuadrant-operator/pkg/extension/types"
)

// +kubebuilder:rbac:groups=extensions.kuadrant.io,resources=planpolicies,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=extensions.kuadrant.io,resources=planpolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=extensions.kuadrant.io,resources=planpolicies/finalizers,verbs=update

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

	planPolicy := &v1alpha1.PlanPolicy{}
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

	_, specErr := r.reconcileSpec(ctx, planPolicy, kuadrantCtx)
	if specErr != nil {
		return reconcile.Result{}, specErr
	}

	return reconcile.Result{}, nil
}

func (r *PlanPolicyReconciler) reconcileSpec(ctx context.Context, planPolicy *v1alpha1.PlanPolicy, kuadrantCtx types.KuadrantCtx) (*v1alpha1.PlanPolicyStatus, error) {
	desiredRateLimitPolicy := r.buildDesiredRateLimitPolicy(planPolicy)
	if err := controllerutil.SetControllerReference(planPolicy, desiredRateLimitPolicy, r.Scheme); err != nil {
		r.Logger.Error(err, "failed to set controller reference")
		return calculateErrorStatus(planPolicy, err), err
	}
	rateLimitPolicy, err := kuadrantCtx.ReconcileObject(ctx, &kuadrantv1.RateLimitPolicy{}, desiredRateLimitPolicy, rlpSpecMutator)
	if err != nil {
		r.Logger.Error(err, "failed to reconcile desired ratelimitpolicy")
		return calculateErrorStatus(planPolicy, err), err
	}

	if err = kuadrantCtx.AddDataTo(ctx, planPolicy, types.DomainAuth, "plan", planPolicy.BuildCelExpression()); err != nil {
		r.Logger.Error(err, "failed to add data to auth domain")
		return calculateErrorStatus(planPolicy, err), err
	}

	if err = isRateLimitPolicyEnforced(rateLimitPolicy.(*kuadrantv1.RateLimitPolicy)); err != nil {
		return calculateErrorStatus(planPolicy, err), err
	}

	return calculateEnforcedStatus(planPolicy, nil), nil
}

func (r *PlanPolicyReconciler) buildDesiredRateLimitPolicy(planPolicy *v1alpha1.PlanPolicy) *kuadrantv1.RateLimitPolicy {
	return &kuadrantv1.RateLimitPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      planPolicy.GetName(),
			Namespace: planPolicy.GetNamespace(),
		},
		Spec: kuadrantv1.RateLimitPolicySpec{
			TargetRef: planPolicy.Spec.TargetRef,
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

func isRateLimitPolicyEnforced(rlPolicy *kuadrantv1.RateLimitPolicy) error {
	cond, found := lo.Find(rlPolicy.Status.GetConditions(), func(c metav1.Condition) bool {
		return c.Type == string(types.PolicyConditionEnforced)
	})
	if !found || cond.Status == metav1.ConditionFalse {
		return fmt.Errorf("RateLimitPolicy %s is not enforced", rlPolicy.Name)
	}
	return nil
}

func calculateErrorStatus(pol *v1alpha1.PlanPolicy, specErr error) *v1alpha1.PlanPolicyStatus {
	newStatus := &v1alpha1.PlanPolicyStatus{
		ObservedGeneration: pol.Generation,
		// Copy initial conditions. Otherwise, status will always be updated
		Conditions: slices.Clone(pol.Status.Conditions),
	}
	meta.SetStatusCondition(&newStatus.Conditions, *extcontroller.AcceptedCondition(pol, specErr))
	return newStatus
}

func calculateEnforcedStatus(pol *v1alpha1.PlanPolicy, enforcedErr error) *v1alpha1.PlanPolicyStatus {
	newStatus := &v1alpha1.PlanPolicyStatus{
		ObservedGeneration: pol.Generation,
		// Copy initial conditions. Otherwise, status will always be updated
		Conditions: slices.Clone(pol.Status.Conditions),
	}

	meta.SetStatusCondition(&newStatus.Conditions, *extcontroller.AcceptedCondition(pol, nil))
	meta.SetStatusCondition(&newStatus.Conditions, *extcontroller.EnforcedCondition(pol, enforcedErr, true))
	return newStatus
}
