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
	"github.com/google/uuid"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/mappers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/reconcilers"
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools"
)

// RateLimitPolicyReconciler reconciles a RateLimitPolicy object
type RateLimitPolicyReconciler struct {
	*reconcilers.BaseReconciler
	TargetRefReconciler reconcilers.TargetRefReconciler
}

//+kubebuilder:rbac:groups=kuadrant.io,resources=ratelimitpolicies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=kuadrant.io,resources=ratelimitpolicies/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=kuadrant.io,resources=ratelimitpolicies/finalizers,verbs=update
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch;update;patch

// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *RateLimitPolicyReconciler) Reconcile(eventCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger().WithValues("RateLimitPolicy", req.NamespacedName, "request id", uuid.NewString())
	logger.Info("Reconciling RateLimitPolicy instances")
	ctx := logr.NewContext(eventCtx, logger)

	kuadrantCR := &kuadrantv1beta1.Kuadrant{}
	if err := r.Client().Get(ctx, req.NamespacedName, kuadrantCR); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("no kuadrant instance found")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "failed to get kuadrant")
		return ctrl.Result{}, err
	}

	if logger.V(1).Enabled() {
		jsonData, err := json.MarshalIndent(kuadrantCR, "", "  ")
		if err != nil {
			return ctrl.Result{}, err
		}
		logger.V(1).Info(string(jsonData))
	}

	topology, err := rlptools.Topology(ctx, r.Client())
	if err != nil {
		return ctrl.Result{}, err
	}

	// reconcile ratelimitpolicy status
	err = r.reconcileStatus(ctx, topology)
	if err != nil {
		// Ignore conflicts, resource might just be outdated.
		if apierrors.IsConflict(err) {
			logger.V(1).Info("Failed to update status: resource might just be outdated", "error", err)
			return reconcile.Result{Requeue: true}, nil
		}

		return ctrl.Result{}, err
	}

	// reconcile network object
	// set direct back ref - i.e. claim the target network object
	err = r.reconcileDirectBackReferences(ctx, topology)
	if err != nil {
		// Ignore conflicts, resource might just be outdated.
		if apierrors.IsConflict(err) {
			logger.V(1).Info("Failed to update status: resource might just be outdated", "error", err)
			return reconcile.Result{Requeue: true}, nil
		}

		return ctrl.Result{}, err
	}

	logger.Info("RateLimitPolicy reconciled successfully")
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RateLimitPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	ok, err := kuadrantgatewayapi.IsGatewayAPIInstalled(mgr.GetRESTMapper())
	if err != nil {
		return err
	}
	if !ok {
		r.Logger().Info("Ratelimitpolicy controller disabled. GatewayAPI was not found")
		return nil
	}

	rlpToKuadrantEventMapper := mappers.NewPolicyToKuadrantEventMapper(
		mappers.WithLogger(r.Logger().WithName("ratelimitpolicyToKuadrantEventMapper")),
		mappers.WithClient(r.Client()),
	)

	gatewayToKuadrantEventMapper := mappers.NewGatewayToKuadrantEventMapper(
		mappers.WithLogger(r.Logger().WithName("gatewayToKuadrantEventMapper")),
	)

	routeToKuadrantEventMapper := mappers.NewHTTPRouteToKuadrantEventMapper(
		mappers.WithLogger(r.Logger().WithName("routeToKuadrantEventMapper")),
		mappers.WithClient(r.Client()),
	)

	return ctrl.NewControllerManagedBy(mgr).
		For(&kuadrantv1beta1.Kuadrant{}).
		Watches(
			&kuadrantv1beta2.RateLimitPolicy{},
			handler.EnqueueRequestsFromMapFunc(rlpToKuadrantEventMapper.Map),
		).
		Watches(
			&gatewayapiv1.HTTPRoute{},
			handler.EnqueueRequestsFromMapFunc(routeToKuadrantEventMapper.Map),
		).
		Watches(
			&gatewayapiv1.Gateway{},
			handler.EnqueueRequestsFromMapFunc(gatewayToKuadrantEventMapper.Map),
		).
		Complete(r)
}
