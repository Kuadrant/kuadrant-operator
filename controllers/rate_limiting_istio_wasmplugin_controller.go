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
	"sort"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	istioextensionsv1alpha1 "istio.io/api/extensions/v1alpha1"
	istioclientgoextensionv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/env"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	kuadrantistioutils "github.com/kuadrant/kuadrant-operator/pkg/istio"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/mappers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/reconcilers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools"
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools/wasm"
)

var (
	WASMFilterImageURL = env.GetString("RELATED_IMAGE_WASMSHIM", "oci://quay.io/kuadrant/wasm-shim:latest")
)

func WASMPluginName(gw *gatewayapiv1.Gateway) string {
	return fmt.Sprintf("kuadrant-%s", gw.Name)
}

// RateLimitingIstioWASMPluginReconciler reconciles a WASMPlugin object for rate limiting
type RateLimitingIstioWASMPluginReconciler struct {
	*reconcilers.BaseReconciler
}

//+kubebuilder:rbac:groups=extensions.istio.io,resources=wasmplugins,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=kuadrant.io,resources=ratelimitpolicies,verbs=get;list;watch;update;patch

// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *RateLimitingIstioWASMPluginReconciler) Reconcile(eventCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger().WithValues("Gateway", req.NamespacedName, "request id", uuid.NewString())
	logger.Info("Reconciling rate limiting WASMPlugin")
	ctx := logr.NewContext(eventCtx, logger)

	gw := &gatewayapiv1.Gateway{}
	if err := r.Client().Get(ctx, req.NamespacedName, gw); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("no gateway found")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "failed to get gateway")
		return ctrl.Result{}, err
	}

	if logger.V(1).Enabled() {
		jsonData, err := json.MarshalIndent(gw, "", "  ")
		if err != nil {
			return ctrl.Result{}, err
		}
		logger.V(1).Info(string(jsonData))
	}

	desired, err := r.desiredRateLimitingWASMPlugin(ctx, gw)
	if err != nil {
		return ctrl.Result{}, err
	}

	err = r.ReconcileResource(ctx, &istioclientgoextensionv1alpha1.WasmPlugin{}, desired, kuadrantistioutils.WASMPluginMutator)
	if err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("Rate limiting WASMPlugin reconciled successfully")
	return ctrl.Result{}, nil
}

func (r *RateLimitingIstioWASMPluginReconciler) desiredRateLimitingWASMPlugin(ctx context.Context, gw *gatewayapiv1.Gateway) (*istioclientgoextensionv1alpha1.WasmPlugin, error) {
	baseLogger, err := logr.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	wasmPlugin := &istioclientgoextensionv1alpha1.WasmPlugin{
		TypeMeta: metav1.TypeMeta{
			Kind:       "WasmPlugin",
			APIVersion: "extensions.istio.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      WASMPluginName(gw),
			Namespace: gw.Namespace,
		},
		Spec: istioextensionsv1alpha1.WasmPlugin{
			TargetRef:    kuadrantistioutils.PolicyTargetRefFromGateway(gw),
			Url:          WASMFilterImageURL,
			PluginConfig: nil,
			// Insert plugin before Istio stats filters and after Istio authorization filters.
			Phase: istioextensionsv1alpha1.PluginPhase_STATS,
		},
	}

	logger := baseLogger.WithValues("wasmplugin", client.ObjectKeyFromObject(wasmPlugin))

	pluginConfig, err := r.wasmPluginConfig(ctx, gw)
	if err != nil {
		return nil, err
	}

	if pluginConfig == nil || len(pluginConfig.RateLimitPolicies) == 0 {
		logger.V(1).Info("pluginConfig is empty. Wasmplugin will be deleted if it exists")
		utils.TagObjectToDelete(wasmPlugin)
		return wasmPlugin, nil
	}

	pluginConfigStruct, err := pluginConfig.ToStruct()
	if err != nil {
		return nil, err
	}

	wasmPlugin.Spec.PluginConfig = pluginConfigStruct

	// controller reference
	if err := r.SetOwnerReference(gw, wasmPlugin); err != nil {
		return nil, err
	}

	return wasmPlugin, nil
}

func (r *RateLimitingIstioWASMPluginReconciler) wasmPluginConfig(ctx context.Context, gw *gatewayapiv1.Gateway) (*wasm.Config, error) {
	logger, err := logr.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	config := &wasm.Config{
		FailureMode:       wasm.FailureModeDeny,
		RateLimitPolicies: make([]wasm.RateLimitPolicy, 0),
	}

	t, err := rlptools.TopologyIndexesFromGateway(ctx, r.Client(), gw)
	if err != nil {
		return nil, err
	}

	rateLimitPolicies := t.PoliciesFromGateway(gw)

	logger.V(1).Info("wasmPluginConfig", "#RLPS", len(rateLimitPolicies))

	// Sort RLPs for consistent comparison with existing objects
	sort.Sort(kuadrantgatewayapi.PolicyByTargetRefKindAndCreationTimeStamp(rateLimitPolicies))

	for _, policy := range rateLimitPolicies {
		rlp := policy.(*kuadrantv1beta2.RateLimitPolicy)
		wasmRLP, err := r.wasmRateLimitPolicy(ctx, t, rlp, gw)
		if err != nil {
			return nil, err
		}

		if wasmRLP == nil {
			// skip this RLP
			continue
		}

		config.RateLimitPolicies = append(config.RateLimitPolicies, *wasmRLP)
	}

	return config, nil
}

func (r *RateLimitingIstioWASMPluginReconciler) wasmRateLimitPolicy(ctx context.Context, t *kuadrantgatewayapi.TopologyIndexes, rlp *kuadrantv1beta2.RateLimitPolicy, gw *gatewayapiv1.Gateway) (*wasm.RateLimitPolicy, error) {
	route, err := r.routeFromRLP(ctx, t, rlp, gw)
	if err != nil {
		return nil, err
	}
	if route == nil {
		// no need to add the policy if there are no routes;
		// a rlp can return no rules if all its limits fail to match any route rule
		// or targeting a gateway with no "free" routes. "free" meaning no route with policies targeting it
		return nil, nil
	}

	// narrow the list of hostnames specified in the route so we don't generate wasm rules that only apply to other gateways
	// this is a no-op for the gateway rlp
	gwHostnames := kuadrantgatewayapi.GatewayHostnames(gw)
	if len(gwHostnames) == 0 {
		gwHostnames = []gatewayapiv1.Hostname{"*"}
	}
	hostnames := kuadrantgatewayapi.FilterValidSubdomains(gwHostnames, route.Spec.Hostnames)
	if len(hostnames) == 0 { // it should only happen when the route specifies no hostnames
		hostnames = gwHostnames
	}

	//
	// The route selectors logic rely on the "hostnames" field of the route object.
	// However, routes effective hostname can be inherited from parent gateway,
	// hence it depends on the context as multiple gateways can be targeted by a route
	// The route selectors logic needs to be refactored
	// or just deleted as soon as the HTTPRoute has name in the route object
	//
	routeWithEffectiveHostnames := route.DeepCopy()
	routeWithEffectiveHostnames.Spec.Hostnames = hostnames

	rules := wasm.Rules(rlp, routeWithEffectiveHostnames)
	if len(rules) == 0 {
		// no need to add the policy if there are no rules; a rlp can return no rules if all its limits fail to match any route rule
		return nil, nil
	}

	return &wasm.RateLimitPolicy{
		Name:      client.ObjectKeyFromObject(rlp).String(),
		Domain:    rlptools.LimitsNamespaceFromRLP(rlp),
		Hostnames: utils.HostnamesToStrings(hostnames), // we might be listing more hostnames than needed due to route selectors hostnames possibly being more restrictive
		Service:   common.KuadrantRateLimitClusterName,
		Rules:     rules,
	}, nil
}

func (r *RateLimitingIstioWASMPluginReconciler) routeFromRLP(ctx context.Context, t *kuadrantgatewayapi.TopologyIndexes, rlp *kuadrantv1beta2.RateLimitPolicy, gw *gatewayapiv1.Gateway) (*gatewayapiv1.HTTPRoute, error) {
	logger, err := logr.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	route := t.GetPolicyHTTPRoute(rlp)

	if route == nil {
		// The policy is targeting a gateway
		// This gateway policy will be enforced into all HTTPRoutes that do not have a policy attached to it

		// Build imaginary route with all the routes not having a RLP targeting it
		untargetedRoutes := t.GetUntargetedRoutes(gw)

		if len(untargetedRoutes) == 0 {
			// For policies targeting a gateway, when no httproutes is attached to the gateway, skip wasm config
			// test wasm config when no http routes attached to the gateway
			logger.V(1).Info("no untargeted httproutes attached to the targeted gateway, skipping wasm config for the gateway rlp", "ratelimitpolicy", client.ObjectKeyFromObject(rlp))
			return nil, nil
		}

		untargetedRules := make([]gatewayapiv1.HTTPRouteRule, 0)
		for idx := range untargetedRoutes {
			untargetedRules = append(untargetedRules, untargetedRoutes[idx].Spec.Rules...)
		}
		gwHostnamesTmp := kuadrantgatewayapi.TargetHostnames(gw)
		gwHostnames := utils.Map(gwHostnamesTmp, func(str string) gatewayapiv1.Hostname { return gatewayapiv1.Hostname(str) })
		route = &gatewayapiv1.HTTPRoute{
			Spec: gatewayapiv1.HTTPRouteSpec{
				Hostnames: gwHostnames,
				Rules:     untargetedRules,
			},
		}
	}

	return route, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RateLimitingIstioWASMPluginReconciler) SetupWithManager(mgr ctrl.Manager) error {
	ok, err := kuadrantistioutils.IsWASMPluginInstalled(mgr.GetRESTMapper())
	if err != nil {
		return err
	}
	if !ok {
		r.Logger().Info("Istio WasmPlugin controller disabled. Istio was not found")
		return nil
	}

	ok, err = kuadrantgatewayapi.IsGatewayAPIInstalled(mgr.GetRESTMapper())
	if err != nil {
		return err
	}
	if !ok {
		r.Logger().Info("Istio WasmPlugin controller disabled. GatewayAPI was not found")
		return nil
	}

	httpRouteToParentGatewaysEventMapper := mappers.NewHTTPRouteToParentGatewaysEventMapper(
		mappers.WithLogger(r.Logger().WithName("httpRouteToParentGatewaysEventMapper")),
	)

	rlpToParentGatewaysEventMapper := mappers.NewPolicyToParentGatewaysEventMapper(
		mappers.WithLogger(r.Logger().WithName("ratelimitpolicyToParentGatewaysEventMapper")),
		mappers.WithClient(r.Client()),
	)

	return ctrl.NewControllerManagedBy(mgr).
		// Rate limiting WASMPlugin controller only cares about
		// Gateway API Gateway
		// Gateway API HTTPRoutes
		// Kuadrant RateLimitPolicies

		// The type of object being *reconciled* is the Gateway.
		// TODO(eguzki): consider having the WasmPlugin as the type of object being *reconciled*
		For(&gatewayapiv1.Gateway{}).
		Owns(&istioclientgoextensionv1alpha1.WasmPlugin{}).
		Watches(
			&gatewayapiv1.HTTPRoute{},
			handler.EnqueueRequestsFromMapFunc(httpRouteToParentGatewaysEventMapper.Map),
		).
		Watches(
			&kuadrantv1beta2.RateLimitPolicy{},
			handler.EnqueueRequestsFromMapFunc(rlpToParentGatewaysEventMapper.Map),
		).
		Complete(r)
}
