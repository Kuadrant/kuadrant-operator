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
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/mappers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/reconcilers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
)

// RateLimitingLimitsReconciler reconciles Limitador CR's "spec.Limits" slice for rate limiting
type RateLimitingLimitsReconciler struct {
	*reconcilers.BaseReconciler
}

//+kubebuilder:rbac:groups=limitador.kuadrant.io,resources=limitadors,verbs=get;list;watch;create;update;patch;delete

// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *RateLimitingLimitsReconciler) Reconcile(eventCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger().WithValues("Gateway", req.NamespacedName)
	logger.Info("Reconciling rate limiting limits")
	ctx := logr.NewContext(eventCtx, logger)

	limitadorCR := &limitadorv1alpha1.Limitador{}
	if err := r.Client().Get(ctx, req.NamespacedName, limitadorCR); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("no limitador found")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "failed to get limitador")
		return ctrl.Result{}, err
	}

	if logger.V(1).Enabled() {
		jsonData, err := json.MarshalIndent(limitadorCR, "", "  ")
		if err != nil {
			return ctrl.Result{}, err
		}
		logger.V(1).Info(string(jsonData))
	}

	//
	// Check that Limitador CR is the resource managed by kuadrant
	// This validation relies on the kuadrant management of the limitador instance
	// Which is currently driven by some hardcoded name and same namespacepace as the kuadrant CR
	//
	if limitadorCR.GetName() != common.LimitadorName {
		logger.Info("this limitador resource is not managed by kuadrant, unexpected name")
		return ctrl.Result{}, nil
	}

	kuadrantList := &kuadrantv1beta1.KuadrantList{}
	if err := r.Client().List(ctx, kuadrantList); err != nil {
		return ctrl.Result{}, err
	}

	if len(kuadrantList.Items) == 0 {
		logger.Info("this limitador resource is not managed by kuadrant, kuadrant instance could not be found")
		return ctrl.Result{}, nil
	}

	if limitadorCR.GetNamespace() != kuadrantList.Items[0].Namespace {
		logger.Info("this limitador resource is not managed by kuadrant, unexpected namespace")
		return ctrl.Result{}, nil
	}

	rlps, err := r.readRLPs(ctx, limitadorCR.GetNamespace())
	if err != nil {
		return ctrl.Result{}, err
	}

	rateLimitIndex := rlptools.NewRateLimitIndex()

	for _, rlp := range rlps {
		if _, ok := rateLimitIndex.Get(client.ObjectKeyFromObject(rlp)); ok {
			continue
		}

		rateLimitIndex.Set(client.ObjectKeyFromObject(rlp), rlptools.LimitadorRateLimitsFromRLP(rlp))
	}

	// return if limitador is up to date
	if rlptools.Equal(rateLimitIndex.ToRateLimits(), limitadorCR.Spec.Limits) {
		logger.V(1).Info("limitador is up to date, skipping update")
		return ctrl.Result{}, nil
	}

	// update limitador
	limitadorCR.Spec.Limits = rateLimitIndex.ToRateLimits()
	err = r.UpdateResource(ctx, limitadorCR)
	logger.V(1).Info("update limitador", "limitador", client.ObjectKeyFromObject(limitadorCR), "err", err)
	if err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("Rate limiting limits reconciled successfully")
	return ctrl.Result{}, nil
}

// Rate limit policies targeting programmed gateways or routes accepted by parent gateways.
func (r *RateLimitingLimitsReconciler) readRLPs(ctx context.Context, kuadrantNS string) ([]*kuadrantv1beta2.RateLimitPolicy, error) {
	logger, err := logr.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	// When multiple kuadrant instances are supported, this fetching could be targeted to all
	// gateways for the same kuadrant instance of the reconciled limitador instance
	gwList := &gatewayapiv1.GatewayList{}
	// Get all the routes having the gateway as parent
	err = r.Client().List(ctx, gwList)
	logger.V(1).Info("topology: list gateways", "#Gateways", len(gwList.Items), "err", err)
	if err != nil {
		return nil, err
	}

	routeList := &gatewayapiv1.HTTPRouteList{}
	// Get all the routes having the gateway as parent
	err = r.Client().List(ctx, routeList)
	logger.V(1).Info("topology: list httproutes", "#HTTPRoutes", len(routeList.Items), "err", err)
	if err != nil {
		return nil, err
	}

	rlpList := &kuadrantv1beta2.RateLimitPolicyList{}
	// Get all the rate limit policies
	err = r.Client().List(ctx, rlpList)
	logger.V(1).Info("topology: list rate limit policies", "#RLPS", len(rlpList.Items), "err", err)
	if err != nil {
		return nil, err
	}

	policies := utils.Map(rlpList.Items, func(p kuadrantv1beta2.RateLimitPolicy) kuadrantgatewayapi.Policy { return &p })

	t, err := kuadrantgatewayapi.NewTopology(
		kuadrantgatewayapi.WithGateways(utils.Map(gwList.Items, ptr.To)),
		kuadrantgatewayapi.WithRoutes(utils.Map(routeList.Items, ptr.To)),
		kuadrantgatewayapi.WithPolicies(policies),
		kuadrantgatewayapi.WithLogger(logger),
	)
	if err != nil {
		return nil, err
	}

	topologyindexes := kuadrantgatewayapi.NewTopologyIndexes(t)

	validRLPs := make([]*kuadrantv1beta2.RateLimitPolicy, 0)

	// Valid gateways are those assigned to the kuadrant instance where the current
	// limitador CR is living
	validGateways := utils.Filter(t.Gateways(), func(gwNode kuadrantgatewayapi.GatewayNode) bool {
		val, err := kuadrant.GetKuadrantNamespace(gwNode.Gateway)
		if err != nil {
			logger.V(1).Info("readRLPs: skipping gateway not assigned to kuadrant",
				"gateway", client.ObjectKeyFromObject(gwNode.Gateway))
			return false
		}

		if val != kuadrantNS {
			logger.V(1).Info("readRLPs: skipping gateway assigned to a different kuadrant instance",
				"gateway", client.ObjectKeyFromObject(gwNode.Gateway))
			return false
		}

		// valid gateway
		return true
	})

	for _, gwNode := range validGateways {
		gwPolicies := topologyindexes.PoliciesFromGateway(gwNode.Gateway)

		// filter out those policies that do not have any effect:
		// * targeting a gateway without any route
		// * targeting a gateway when all the route already have another policy attached
		numUntargetedRoutes := len(topologyindexes.GetUntargetedRoutes(gwNode.Gateway))
		validGwPolicies := utils.Filter(gwPolicies, func(p kuadrantgatewayapi.Policy) bool {
			// topologyindexes.GetPolicyHTTPRoute(p) != nil => the policy is targeting a route so it is effective
			// topologyindexes.GetPolicyHTTPRoute(p) == nil && numUntargetedRoutes == 0 => the policy is not effective
			return topologyindexes.GetPolicyHTTPRoute(p) != nil || numUntargetedRoutes > 0
		})

		validGwRLPs := utils.Map(validGwPolicies,
			func(p kuadrantgatewayapi.Policy) *kuadrantv1beta2.RateLimitPolicy {
				return p.(*kuadrantv1beta2.RateLimitPolicy)
			})

		validRLPs = append(validRLPs, validGwRLPs...)
	}

	logger.V(1).Info("readRLPs: valid rate limit policies", "#RLPS", len(validRLPs))
	return validRLPs, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RateLimitingLimitsReconciler) SetupWithManager(mgr ctrl.Manager) error {
	rlpToLimitadorEventMapper := mappers.NewRLPToLimitadorEventMapper(
		mappers.WithLogger(r.Logger().WithName("ratelimitpolicyToLimitadorEventMapper")),
		mappers.WithClient(r.Client()),
	)

	gatewayToLimitadorEventMapper := mappers.NewGatewayToLimitadorEventMapper(
		mappers.WithLogger(r.Logger().WithName("gatewayToLimitadorEventMapper")),
	)

	routeToLimitadorEventMapper := mappers.NewHTTPRouteToLimitadorEventMapper(
		mappers.WithLogger(r.Logger().WithName("routeToLimitadorEventMapper")),
		mappers.WithClient(r.Client()),
	)

	return ctrl.NewControllerManagedBy(mgr).
		For(&limitadorv1alpha1.Limitador{}).
		Watches(
			&kuadrantv1beta2.RateLimitPolicy{},
			handler.EnqueueRequestsFromMapFunc(rlpToLimitadorEventMapper.Map),
		).
		Watches(
			&gatewayapiv1.HTTPRoute{},
			handler.EnqueueRequestsFromMapFunc(routeToLimitadorEventMapper.Map),
		).
		Watches(
			&gatewayapiv1.Gateway{},
			handler.EnqueueRequestsFromMapFunc(gatewayToLimitadorEventMapper.Map),
		).
		Complete(r)
}
