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

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/mappers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/reconcilers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools"
)

// RateLimitingLimitsReconciler reconciles Limitador CR's "spec.Limits" slice for rate limiting
type RateLimitingLimitsReconciler struct {
	*reconcilers.BaseReconciler
}

//+kubebuilder:rbac:groups=limitador.kuadrant.io,resources=limitadors,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=kuadrant.io,resources=kuadrants,verbs=get;list;watch;create;update;patch;delete

// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *RateLimitingLimitsReconciler) Reconcile(eventCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger().WithValues("kuadrant", req.NamespacedName)
	logger.Info("Reconciling rate limiting limits")
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

	rateLimitIndex, err := r.buildRateLimitIndexFromTopology(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	// get the current limitador cr for the kuadrant instance so we can compare if it needs to be updated
	limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: kuadrantCR.GetNamespace()}
	limitador := &limitadorv1alpha1.Limitador{}
	err = r.Client().Get(ctx, limitadorKey, limitador)
	logger.V(1).Info("get limitador", "limitador", limitadorKey, "err", err)
	if err != nil {
		return ctrl.Result{}, err
	}

	if !rlptools.Equal(rateLimitIndex.ToRateLimits(), limitador.Spec.Limits) {
		// update limitador
		limitador.Spec.Limits = rateLimitIndex.ToRateLimits()
		err = r.UpdateResource(ctx, limitador)
		logger.V(1).Info("update limitador", "limitador", client.ObjectKeyFromObject(limitador), "err", err)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	logger.Info("Rate limiting limits reconciled successfully")
	return ctrl.Result{}, nil
}

func (r *RateLimitingLimitsReconciler) buildRateLimitIndexFromTopology(ctx context.Context) (*rlptools.RateLimitIndex, error) {
	logger, err := logr.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	topology, err := rlptools.Topology(ctx, r.Client())
	if err != nil {
		return nil, err
	}

	rateLimitIndex := rlptools.NewRateLimitIndex()

	// filter out those policies that do not have any effect:
	// * targeting a gateway without any route
	// * targeting a gateway when all the routes already have another policy attached
	for _, gateway := range topology.Gateways() {
		gwLogger := logger.WithValues("gateway", client.ObjectKeyFromObject(gateway))
		gwLogger.V(1).Info("gateway numRoutes", "#Routes", len(gateway.Routes()))
		gwLogger.V(1).Info("gateway numPolicies", "#Policies", len(gateway.AttachedPolicies()))
		for _, route := range gateway.Routes() {
			routeLogger := gwLogger.WithValues("route", client.ObjectKeyFromObject(route))
			routeLogger.V(1).Info("route numPolicies", "#Policies", len(route.AttachedPolicies()))

			routePolicies := route.AttachedPolicies()

			// gateway policies are default ones when missing at the route level
			if len(routePolicies) == 0 {
				routePolicies = gateway.AttachedPolicies()
			}

			for _, policy := range routePolicies {
				policyLogger := routeLogger.WithValues("policy", client.ObjectKeyFromObject(policy))

				gwRLPs := utils.Map(gateway.AttachedPolicies(), func(p kuadrantgatewayapi.Policy) *kuadrantv1beta2.RateLimitPolicy {
					return p.(*kuadrantv1beta2.RateLimitPolicy)
				})

				routePolicy := policy.(*kuadrantv1beta2.RateLimitPolicy)

				effectivePolicy := rlptools.ApplyOverrides(routePolicy, gwRLPs, policyLogger)

				if effectivePolicy == nil {
					policyLogger.V(1).Info("effective policy is null")
					continue
				}

				key := rlptools.RateLimitIndexKey{
					RateLimitPolicyKey: client.ObjectKeyFromObject(effectivePolicy),
					GatewayKey:         client.ObjectKeyFromObject(gateway.Gateway),
				}
				if _, ok := rateLimitIndex.Get(key); ok {
					// multiple routes without attached policies can get the same effective policy
					continue
				}
				policyLogger.V(1).Info("adding effective policy")
				rateLimitIndex.Set(key, rlptools.LimitadorRateLimitsFromRLP(effectivePolicy))
			}
		}
	}

	return rateLimitIndex, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RateLimitingLimitsReconciler) SetupWithManager(mgr ctrl.Manager) error {
	ok, err := kuadrantgatewayapi.IsGatewayAPIInstalled(mgr.GetRESTMapper())
	if err != nil {
		return err
	}
	if !ok {
		r.Logger().Info("Kuadrant controller disabled. GatewayAPI was not found")
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
		Owns(&limitadorv1alpha1.Limitador{}).
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
