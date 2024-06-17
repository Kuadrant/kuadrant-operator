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
	"slices"
	"sort"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/mappers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/reconcilers"
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
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
	logger := r.Logger().WithValues("Gateway", req.NamespacedName)
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

	if logger.V(1).Enabled() {
		jsonData, err := json.MarshalIndent(kuadrantCR, "", "  ")
		if err != nil {
			return ctrl.Result{}, err
		}
		logger.V(1).Info(string(jsonData))
	}

	rateLimitIndexFromTopology, err := r.buildRateLimitIndexFromTopology(ctx)
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

	if !rlptools.Equal(rateLimitIndexFromTopology.ToRateLimits(), limitador.Spec.Limits) {
		// update limitador
		limitador.Spec.Limits = rateLimitIndexFromTopology.ToRateLimits()
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

	gateways := lo.KeyBy(topology.Gateways(), func(gateway kuadrantgatewayapi.GatewayNode) string {
		return client.ObjectKeyFromObject(gateway.Gateway).String()
	})

	// sort the gateways for deterministic output and consistent comparison against existing objects
	gatewayNames := lo.Keys(gateways)
	slices.Sort(gatewayNames)

	rateLimitIndex := rlptools.NewRateLimitIndex()

	for _, gatewayName := range gatewayNames {
		gateway := gateways[gatewayName].Gateway
		topologyWithOverrides, err := rlptools.ApplyOverrides(topology, gateway)
		if err != nil {
			logger.Error(err, "failed to apply overrides")
			return nil, err
		}

		// sort the policies for deterministic output and consistent comparison against existing objects
		indexes := kuadrantgatewayapi.NewTopologyIndexes(topologyWithOverrides)
		policies := indexes.PoliciesFromGateway(gateway)
		sort.Sort(kuadrantgatewayapi.PolicyByTargetRefKindAndCreationTimeStamp(policies))

		logger.V(1).Info("new rate limit index",
			"gateway", client.ObjectKeyFromObject(gateway),
			"policies", lo.Map(policies, func(p kuadrantgatewayapi.Policy, _ int) string {
				return client.ObjectKeyFromObject(p).String()
			}))

		for _, policy := range policies {
			rlpKey := client.ObjectKeyFromObject(policy)
			gatewayKey := client.ObjectKeyFromObject(gateway)
			key := rlptools.RateLimitIndexKey{
				RateLimitPolicyKey: rlpKey,
				GatewayKey:         gatewayKey,
			}
			if _, ok := rateLimitIndex.Get(key); ok { // should never happen
				logger.Error(fmt.Errorf("unexpected duplicate rate limit policy key found"),
					"failed do add rate limit policy to index",
					"RateLimitPolicy", rlpKey.String(), "Gateway", gatewayKey)
				continue
			}
			rlp := policy.(*kuadrantv1beta2.RateLimitPolicy)
			rateLimitIndex.Set(key, rlptools.LimitadorRateLimitsFromRLP(rlp))
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
