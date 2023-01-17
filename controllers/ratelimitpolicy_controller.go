/*
Copyright 2021 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/reconcilers"
)

const rateLimitPolicyFinalizer = "ratelimitpolicy.kuadrant.io/finalizer"

// RateLimitPolicyReconciler reconciles a RateLimitPolicy object
type RateLimitPolicyReconciler struct {
	reconcilers.TargetRefReconciler
}

//+kubebuilder:rbac:groups=kuadrant.io,resources=ratelimitpolicies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=kuadrant.io,resources=ratelimitpolicies/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=kuadrant.io,resources=ratelimitpolicies/finalizers,verbs=update
//+kubebuilder:rbac:groups=limitador.kuadrant.io,resources=limitadors,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.istio.io,resources=envoyfilters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=extensions.istio.io,resources=wasmplugins,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=create;get;list;watch;update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the RateLimitPolicy object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *RateLimitPolicyReconciler) Reconcile(eventCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger().WithValues("RateLimitPolicy", req.NamespacedName)
	logger.Info("Reconciling RateLimitPolicy")
	ctx := logr.NewContext(eventCtx, logger)

	// fetch the ratelimitpolicy
	rlp := &kuadrantv1beta1.RateLimitPolicy{}
	if err := r.Client().Get(ctx, req.NamespacedName, rlp); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("no RateLimitPolicy found")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "failed to get RateLimitPolicy")
		return ctrl.Result{}, err
	}

	if logger.V(1).Enabled() {
		jsonData, err := json.MarshalIndent(rlp, "", "  ")
		if err != nil {
			return ctrl.Result{}, err
		}
		logger.V(1).Info(string(jsonData))
	}

	markedForDeletion := rlp.GetDeletionTimestamp() != nil

	// fetch the target network object
	targetObj, err := r.FetchValidTargetRef(ctx, rlp.GetTargetRef(), rlp.Namespace)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.V(1).Info("Network object not found. Cleaning up")
			delResErr := r.deleteResources(ctx, rlp, nil)
			if markedForDeletion {
				return ctrl.Result{}, delResErr
			}
			if delResErr == nil {
				delResErr = err
			}
			return r.reconcileStatus(ctx, rlp, nil, delResErr)
		}
		return ctrl.Result{}, err
	}

	// handle authpolicy marked for deletion
	if markedForDeletion {
		if controllerutil.ContainsFinalizer(rlp, rateLimitPolicyFinalizer) {
			logger.V(1).Info("Handling removal of ratelimitpolicy object")

			if err := r.deleteResources(ctx, rlp, targetObj); err != nil {
				return ctrl.Result{}, err
			}

			logger.Info("removing finalizer")
			if err := r.RemoveFinalizer(ctx, rlp, rateLimitPolicyFinalizer); err != nil {
				return ctrl.Result{}, err
			}
		}

		return ctrl.Result{}, nil
	}

	// add finalizer to the ratelimitpolicy
	if !controllerutil.ContainsFinalizer(rlp, rateLimitPolicyFinalizer) {
		controllerutil.AddFinalizer(rlp, rateLimitPolicyFinalizer)
		if err := r.UpdateResource(ctx, rlp); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{Requeue: true}, err
		}
	}

	// reconcile the ratelimitpolicy spec
	specResult, specErr := r.reconcileResources(ctx, rlp, targetObj)
	if specErr == nil && specResult.Requeue {
		logger.V(1).Info("Reconciling spec not finished. Requeueing.")
		return specResult, nil
	}

	// reconcile ratelimitpolicy status
	statusResult, statusErr := r.reconcileStatus(ctx, rlp, targetObj, specErr)

	if specErr != nil {
		return ctrl.Result{}, specErr
	}

	if statusErr != nil {
		return ctrl.Result{}, statusErr
	}

	if statusResult.Requeue {
		logger.V(1).Info("Reconciling status not finished. Requeueing.")
		return statusResult, nil
	}

	logger.Info("RateLimitPolicy reconciled successfully")
	return ctrl.Result{}, nil
}

func (r *RateLimitPolicyReconciler) reconcileResources(ctx context.Context, rlp *kuadrantv1beta1.RateLimitPolicy, targetObj client.Object) (ctrl.Result, error) {
	err := rlp.Validate()
	if err != nil {
		return ctrl.Result{}, err
	}

	err = r.validateHierarchicalRules(ctx, rlp, targetObj)
	if err != nil {
		return ctrl.Result{}, err
	}

	err = r.reconcileGatewayDiffs(ctx, rlp, targetObj)
	if err != nil {
		return ctrl.Result{}, err
	}

	err = r.reconcileNetworkResourceDirectBackReference(ctx, rlp, targetObj)
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *RateLimitPolicyReconciler) deleteResources(ctx context.Context, rlp *kuadrantv1beta1.RateLimitPolicy, targetObj client.Object) error {
	logger, _ := logr.FromContext(ctx)
	logger.V(1).Info("Handling removal of ratelimitpolicy object")

	gatewayDiffObj, err := r.ComputeGatewayDiffs(ctx, rlp, targetObj, &common.KuadrantRateLimitPolicyRefsConfig{})
	if err != nil {
		return err
	}

	if err := r.reconcileWASMPluginConf(ctx, rlp, gatewayDiffObj); err != nil {
		return err
	}

	if err := r.reconcileRateLimitingClusterEnvoyFilter(ctx, rlp, gatewayDiffObj); err != nil {
		return err
	}

	if err := r.reconcileLimits(ctx, rlp, gatewayDiffObj); err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	if err := r.ReconcileGatewayPolicyReferences(ctx, rlp, gatewayDiffObj); err != nil {
		return err
	}

	if targetObj != nil {
		return r.deleteNetworkResourceDirectBackReference(ctx, rlp, targetObj)
	}

	return nil
}

func (r *RateLimitPolicyReconciler) validateHierarchicalRules(ctx context.Context, rlp *kuadrantv1beta1.RateLimitPolicy, targetObj client.Object) error {
	targetHostnames, err := r.TargetHostnames(ctx, targetObj)
	if err != nil {
		return err
	}

	ruleHosts := make([]string, 0)
	for idx := range rlp.Spec.RateLimits {
		for ruleIdx := range rlp.Spec.RateLimits[idx].Rules {
			ruleHosts = append(ruleHosts, rlp.Spec.RateLimits[idx].Rules[ruleIdx].Hosts...)
		}
	}

	if valid, invalidHost := common.ValidSubdomains(targetHostnames, ruleHosts); !valid {
		return fmt.Errorf("rule host (%s) not valid", invalidHost)
	}

	return nil
}

// Ensures only one RLP targets the network resource
func (r *RateLimitPolicyReconciler) reconcileNetworkResourceDirectBackReference(ctx context.Context, policy common.KuadrantPolicy, targetObj client.Object) error {
	return r.ReconcileTargetBackReference(ctx, client.ObjectKeyFromObject(policy), targetObj, common.RateLimitPolicyBackRefAnnotation)
}

func (r *RateLimitPolicyReconciler) deleteNetworkResourceDirectBackReference(ctx context.Context, rlp *kuadrantv1beta1.RateLimitPolicy, targetObj client.Object) error {
	return r.DeleteTargetBackReference(ctx, client.ObjectKeyFromObject(rlp), targetObj, common.RateLimitPolicyBackRefAnnotation)
}

func (r *RateLimitPolicyReconciler) reconcileGatewayDiffs(ctx context.Context, rlp *kuadrantv1beta1.RateLimitPolicy, targetObj client.Object) error {
	// Reconcile based on gateway diffs:
	// * Limits
	// * WASM Plugin configuration object
	// * EnvoyFilter
	// * Gateway rate limit policy annotations (last)
	logger, _ := logr.FromContext(ctx)

	gatewayDiffObj, err := r.ComputeGatewayDiffs(ctx, rlp, targetObj, &common.KuadrantRateLimitPolicyRefsConfig{})
	if err != nil {
		return err
	}
	if gatewayDiffObj == nil {
		logger.V(1).Info("gatewayDiffObj is nil")
		return nil
	}

	if err := r.reconcileLimits(ctx, rlp, gatewayDiffObj); err != nil {
		return err
	}

	if err := r.reconcileRateLimitingClusterEnvoyFilter(ctx, rlp, gatewayDiffObj); err != nil {
		return err
	}

	if err := r.reconcileWASMPluginConf(ctx, rlp, gatewayDiffObj); err != nil {
		return err
	}

	// should be the last step, only when all the reconciliation steps succeed
	if err := r.ReconcileGatewayPolicyReferences(ctx, rlp, gatewayDiffObj); err != nil {
		return err
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RateLimitPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	httpRouteEventMapper := &HTTPRouteEventMapper{
		Logger: r.Logger().WithName("httpRouteEventMapper"),
	}
	gatewayEventMapper := &GatewayEventMapper{
		Logger: r.Logger().WithName("gatewayEventMapper"),
	}
	gatewayRateLimtPolicyEventMapper := &GatewayRateLimitPolicyEventMapper{
		Logger: r.Logger().WithName("gatewayRateLimitPolicyEventMapper"),
		Client: r.Client(),
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&kuadrantv1beta1.RateLimitPolicy{}).
		Watches(
			&source.Kind{Type: &gatewayapiv1alpha2.HTTPRoute{}},
			handler.EnqueueRequestsFromMapFunc(httpRouteEventMapper.MapToRateLimitPolicy),
		).
		// Currently the purpose is to generate events when rlp references change in gateways
		// so the status of the rlps targeting a route can be keep in sync
		Watches(
			&source.Kind{Type: &gatewayapiv1alpha2.Gateway{}},
			handler.EnqueueRequestsFromMapFunc(gatewayEventMapper.MapToRateLimitPolicy),
		).
		// When gateway level RLP changes, notify route level RLP's
		Watches(
			&source.Kind{Type: &kuadrantv1beta1.RateLimitPolicy{}},
			handler.EnqueueRequestsFromMapFunc(gatewayRateLimtPolicyEventMapper.MapRouteRateLimitPolicy),
		).
		Complete(r)
}
