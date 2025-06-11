package controller

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/internal/reconcilers"
	"github.com/kuadrant/kuadrant-operator/pkg/extension/controller"
	"github.com/kuadrant/kuadrant-operator/pkg/extension/types"
	"github.com/kuadrant/kuadrant-operator/pkg/extension/utils"
)

// +kubebuilder:rbac:groups=kuadrant.io,resources=planpolicies,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=kuadrant.io,resources=planpolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kuadrant.io,resources=planpolicies/finalizers,verbs=update

// +kubebuilder:rbac:groups=kuadrant.io,resources=ratelimitpolicies,verbs=create;delete

type TargetRefData struct {
	Group       gatewayapiv1alpha2.Group       `json:"group"`
	Kind        gatewayapiv1alpha2.Kind        `json:"kind"`
	Name        gatewayapiv1alpha2.ObjectName  `json:"name"`
	SectionName gatewayapiv1alpha2.SectionName `json:"sectionName"`
}

type PlanPolicyReconciler struct {
	*reconcilers.BaseReconciler
	logger logr.Logger
}

func NewPlanPolicyReconciler() *PlanPolicyReconciler {
	return &PlanPolicyReconciler{
		BaseReconciler: reconcilers.NewLazyBaseReconciler(),
	}
}

func (r *PlanPolicyReconciler) WithLogger(logger logr.Logger) *PlanPolicyReconciler {
	r.logger = logger
	return r
}

func (r *PlanPolicyReconciler) Reconcile(ctx context.Context, request reconcile.Request, kuadrantCtx types.KuadrantCtx) (reconcile.Result, error) {
	r.WithLogger(utils.LoggerFromContext(ctx).WithName("PlanPolicyReconciler"))
	r.logger.Info("reconciling planpolicies started")
	defer r.logger.Info("reconciling planpolicies completed")

	planPolicy := &kuadrantv1alpha1.PlanPolicy{}
	if err := r.Client().Get(ctx, request.NamespacedName, planPolicy); err != nil {
		if errors.IsNotFound(err) {
			r.logger.Error(err, "planpolicy not found")
			return reconcile.Result{}, nil
		}
		r.logger.Error(err, "failed to retrieve planpolicy")
		return reconcile.Result{}, err
	}

	if planPolicy.GetDeletionTimestamp() != nil {
		r.logger.Info("planpolicy marked for deletion")
		// todo(adam-cattermole): handle deletion case
		return reconcile.Result{}, nil
	}

	targetRefsData, err := controller.Resolve[[]TargetRefData](ctx, kuadrantCtx, planPolicy,
		`self.findAuthPolicies()[0].targetRefs.map(ref, {"group": ref.group, "kind": ref.kind, "name": ref.name, "sectionName": ref.sectionName})`, true)
	if err != nil {
		r.logger.Error(err, "failed to resolve target references")
		return reconcile.Result{}, err
	}

	if len(targetRefsData) == 0 {
		r.logger.Info("no target references found")
		return reconcile.Result{}, nil
	}

	targetRefs := lo.Map(targetRefsData, func(tr TargetRefData, _ int) gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName {
		targetRef := gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
			LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
				Name:  tr.Name,
				Group: tr.Group,
				Kind:  tr.Kind,
			},
		}
		if tr.SectionName != "" {
			targetRef.SectionName = ptr.To(tr.SectionName)
		}
		return targetRef
	})

	desiredRateLimitPolicy := r.buildDesiredRateLimitPolicy(planPolicy, targetRefs[0])
	if err := controllerutil.SetControllerReference(planPolicy, desiredRateLimitPolicy, r.Scheme()); err != nil {
		r.logger.Error(err, "failed to set controller reference")
		return reconcile.Result{}, err
	}
	if err := r.ReconcileResource(ctx, &kuadrantv1.RateLimitPolicy{}, desiredRateLimitPolicy, reconcilers.CreateOnlyMutator); err != nil {
		r.logger.Error(err, "failed to reconcile desired ratelimitpolicy")
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
