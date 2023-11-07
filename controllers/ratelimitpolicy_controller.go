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

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	gatewayapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
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
//+kubebuilder:rbac:groups=extensions.istio.io,resources=wasmplugins,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch;update;patch

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
	rlp := &kuadrantv1beta2.RateLimitPolicy{}
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
	targetNetworkObject, err := r.FetchValidTargetRef(ctx, rlp.GetTargetRef(), rlp.Namespace)
	if err != nil {
		if !markedForDeletion {
			if apierrors.IsNotFound(err) {
				logger.V(1).Info("Network object not found. Cleaning up")
				delResErr := r.deleteResources(ctx, rlp, nil)
				if delResErr == nil {
					delResErr = err
				}
				return r.reconcileStatus(ctx, rlp, delResErr)
			}
			return ctrl.Result{}, err
		}
		targetNetworkObject = nil // we need the object set to nil when there's an error, otherwise deleting the resources (when marked for deletion) will panic
	}

	// handle authpolicy marked for deletion
	if markedForDeletion {
		if controllerutil.ContainsFinalizer(rlp, rateLimitPolicyFinalizer) {
			logger.V(1).Info("Handling removal of ratelimitpolicy object")

			if err := r.deleteResources(ctx, rlp, targetNetworkObject); err != nil {
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
	specErr := r.reconcileResources(ctx, rlp, targetNetworkObject)

	// reconcile ratelimitpolicy status
	statusResult, statusErr := r.reconcileStatus(ctx, rlp, specErr)

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

func (r *RateLimitPolicyReconciler) reconcileResources(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy, targetNetworkObject client.Object) error {
	// validate
	err := rlp.Validate()
	if err != nil {
		return err
	}

	err = common.ValidateHierarchicalRules(rlp, targetNetworkObject)
	if err != nil {
		return err
	}

	// reconcile based on gateway diffs
	gatewayDiffObj, err := r.ComputeGatewayDiffs(ctx, rlp, targetNetworkObject, &common.KuadrantRateLimitPolicyRefsConfig{})
	if err != nil {
		return err
	}

	if err := r.reconcileLimits(ctx, rlp); err != nil {
		return err
	}

	if err := r.reconcileWASMPluginConf(ctx, rlp, gatewayDiffObj); err != nil {
		return err
	}

	// set direct back ref - i.e. claim the target network object as taken asap
	if err := r.reconcileNetworkResourceDirectBackReference(ctx, rlp, targetNetworkObject); err != nil {
		return err
	}

	// set annotation of policies afftecting the gateway - should be the last step, only when all the reconciliation steps succeed
	return r.ReconcileGatewayPolicyReferences(ctx, rlp, gatewayDiffObj)
}

func (r *RateLimitPolicyReconciler) deleteResources(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy, targetNetworkObject client.Object) error {
	// delete based on gateway diffs
	gatewayDiffObj, err := r.ComputeGatewayDiffs(ctx, rlp, targetNetworkObject, &common.KuadrantRateLimitPolicyRefsConfig{})
	if err != nil {
		return err
	}

	if err := r.reconcileWASMPluginConf(ctx, rlp, gatewayDiffObj); err != nil {
		return err
	}

	if err := r.deleteLimits(ctx, rlp); err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	// remove direct back ref
	if targetNetworkObject != nil {
		if err := r.deleteNetworkResourceDirectBackReference(ctx, targetNetworkObject); err != nil {
			return err
		}
	}

	// update annotation of policies afftecting the gateway
	return r.ReconcileGatewayPolicyReferences(ctx, rlp, gatewayDiffObj)
}

// Ensures only one RLP targets the network resource
func (r *RateLimitPolicyReconciler) reconcileNetworkResourceDirectBackReference(ctx context.Context, policy common.KuadrantPolicy, targetNetworkObject client.Object) error {
	return r.ReconcileTargetBackReference(ctx, client.ObjectKeyFromObject(policy), targetNetworkObject, common.RateLimitPolicyBackRefAnnotation)
}

func (r *RateLimitPolicyReconciler) deleteNetworkResourceDirectBackReference(ctx context.Context, targetNetworkObject client.Object) error {
	return r.DeleteTargetBackReference(ctx, targetNetworkObject, common.RateLimitPolicyBackRefAnnotation)
}

// SetupWithManager sets up the controller with the Manager.
func (r *RateLimitPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	httpRouteEventMapper := &HTTPRouteEventMapper{
		Logger: r.Logger().WithName("httpRouteEventMapper"),
	}
	gatewayEventMapper := &GatewayEventMapper{
		Logger: r.Logger().WithName("gatewayEventMapper"),
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&kuadrantv1beta2.RateLimitPolicy{}).
		Watches(
			&gatewayapiv1beta1.HTTPRoute{},
			handler.EnqueueRequestsFromMapFunc(httpRouteEventMapper.MapToRateLimitPolicy),
		).
		// Currently the purpose is to generate events when rlp references change in gateways
		// so the status of the rlps targeting a route can be keep in sync
		Watches(
			&gatewayapiv1beta1.Gateway{},
			handler.EnqueueRequestsFromMapFunc(gatewayEventMapper.MapToRateLimitPolicy),
		).
		Complete(r)
}
