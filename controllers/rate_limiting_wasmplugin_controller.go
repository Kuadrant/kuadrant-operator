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
	"sort"

	"github.com/go-logr/logr"
	istioextensionsv1alpha1 "istio.io/api/extensions/v1alpha1"
	istioclientgoextensionv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/reconcilers"
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools"
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools/wasm"
)

const (
	HTTPRouteParents = ".metadata.route.parent"
)

// RateLimitingWASMPluginReconciler reconciles a WASMPlugin object for rate limiting
type RateLimitingWASMPluginReconciler struct {
	reconcilers.TargetRefReconciler
}

//+kubebuilder:rbac:groups=extensions.istio.io,resources=wasmplugins,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=kuadrant.io,resources=ratelimitpolicies,verbs=get;list;watch;update;patch

// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *RateLimitingWASMPluginReconciler) Reconcile(eventCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger().WithValues("Gateway", req.NamespacedName)
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

	err = r.ReconcileResource(ctx, &istioclientgoextensionv1alpha1.WasmPlugin{}, desired, rlptools.WASMPluginMutator)
	if err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("Rate limiting WASMPlugin reconciled successfully")
	return ctrl.Result{}, nil
}

func (r *RateLimitingWASMPluginReconciler) desiredRateLimitingWASMPlugin(ctx context.Context, gw *gatewayapiv1.Gateway) (*istioclientgoextensionv1alpha1.WasmPlugin, error) {
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
			Name:      rlptools.WASMPluginName(gw),
			Namespace: gw.Namespace,
		},
		Spec: istioextensionsv1alpha1.WasmPlugin{
			Selector:     common.IstioWorkloadSelectorFromGateway(ctx, r.Client(), gw),
			Url:          rlptools.WASMFilterImageURL,
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
		common.TagObjectToDelete(wasmPlugin)
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

func (r *RateLimitingWASMPluginReconciler) wasmPluginConfig(ctx context.Context, gw *gatewayapiv1.Gateway) (*wasm.Plugin, error) {
	logger, err := logr.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	wasmPlugin := &wasm.Plugin{
		FailureMode:       wasm.FailureModeDeny,
		RateLimitPolicies: make([]wasm.RateLimitPolicy, 0),
	}

	gatewayAPITopology, err := r.gatewayAPITopologyFromGateway(ctx, gw)
	if err != nil {
		return nil, err
	}

	rateLimitPolicies := gatewayAPITopology.PoliciesFromGateway(gw)

	logger.V(1).Info("wasmPluginConfig", "#RLPS", len(rateLimitPolicies))

	// Shallow copy by assignment
	rateLimitPoliciesSorted := rateLimitPolicies

	// Sort RLPs for consistent comparison with existing objects
	sort.Sort(common.PolicyByKey(rateLimitPoliciesSorted))

	for _, policy := range rateLimitPolicies {
		rlp := policy.(*kuadrantv1beta2.RateLimitPolicy)
		wasmRLP, err := r.WASMRateLimitPolicy(ctx, gatewayAPITopology, rlp, gw)
		if err != nil {
			return nil, err
		}

		if wasmRLP == nil {
			// skip this RLP
			continue
		}

		wasmPlugin.RateLimitPolicies = append(wasmPlugin.RateLimitPolicies, *wasmRLP)
	}

	return wasmPlugin, nil
}

func (r *RateLimitingWASMPluginReconciler) gatewayAPITopologyFromGateway(ctx context.Context, gw *gatewayapiv1.Gateway) (*common.KuadrantTopology, error) {
	logger, err := logr.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	routeList := &gatewayapiv1.HTTPRouteList{}
	// Get all the routes having the gateway as parent
	err = r.Client().List(ctx, routeList, client.MatchingFields{HTTPRouteParents: client.ObjectKeyFromObject(gw).String()})
	logger.V(1).Info("gatewayAPITopologyFromGateway: list httproutes from gateway", "err", err)
	if err != nil {
		return nil, err
	}

	rlpList := &kuadrantv1beta2.RateLimitPolicyList{}
	// Get all the rate limit policies
	// TODO(eastizle): Add index field??
	err = r.Client().List(ctx, rlpList)
	logger.V(1).Info("gatewayAPITopologyFromGateway: list rate limit policies", "err", err)
	if err != nil {
		return nil, err
	}

	return common.NewKuadrantTopology(
		[]*gatewayapiv1.Gateway{gw},
		common.Map(routeList.Items, func(r gatewayapiv1.HTTPRoute) *gatewayapiv1.HTTPRoute { return &r }),
		common.Map(rlpList.Items, func(p kuadrantv1beta2.RateLimitPolicy) common.KuadrantPolicy { return &p }),
	), nil
}

func (r *RateLimitingWASMPluginReconciler) WASMRateLimitPolicy(ctx context.Context, t *common.KuadrantTopology, rlp *kuadrantv1beta2.RateLimitPolicy, gw *gatewayapiv1.Gateway) (*wasm.RateLimitPolicy, error) {
	gwHostnamesTmp := common.TargetHostnames(gw)
	gwHostnames := common.Map(gwHostnamesTmp, func(str string) gatewayapiv1.Hostname { return gatewayapiv1.Hostname(str) })

	route, err := r.RouteFromRLP(ctx, t, rlp, gw)
	if err != nil {
		return nil, err
	}
	if route == nil {
		// no need to add the policy if there are no routes;
		// a rlp can return no rules if all its limits fail to match any route rule
		// or targeting a gateway with no "free" routes. "free" meaning no route with policies targeting it
		return nil, nil
	}

	rules := rlptools.WasmRules(rlp, route)
	if len(rules) == 0 {
		// no need to add the policy if there are no rules; a rlp can return no rules if all its limits fail to match any route rule
		return nil, nil
	}

	// narrow the list of hostnames specified in the route so we don't generate wasm rules that only apply to other gateways
	// this is a no-op for the gateway rlp
	hostnames := common.FilterValidSubdomains(gwHostnames, route.Spec.Hostnames)
	if len(hostnames) == 0 { // it should only happen when the route specifies no hostnames
		hostnames = gwHostnames
	}

	return &wasm.RateLimitPolicy{
		Name:      client.ObjectKeyFromObject(rlp).String(),
		Domain:    rlptools.LimitsNamespaceFromRLP(rlp),
		Hostnames: common.HostnamesToStrings(hostnames), // we might be listing more hostnames than needed due to route selectors hostnames possibly being more restrictive
		Service:   common.KuadrantRateLimitClusterName,
		Rules:     rules,
	}, nil
}

func (r *RateLimitingWASMPluginReconciler) RouteFromRLP(ctx context.Context, t *common.KuadrantTopology, rlp *kuadrantv1beta2.RateLimitPolicy, gw *gatewayapiv1.Gateway) (*gatewayapiv1.HTTPRoute, error) {
	logger, err := logr.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	route := t.GetPolicyHTTPRoute(rlp)

	if route == nil {
		// The policy is targeting a gateway
		// This gateway policy will be enforced into all HTTPRoutes that do not have a policy attached to it

		// Build imaginary route with all the routes not having a RLP targeting it
		freeRoutes := t.GetFreeRoutes(gw)

		if len(freeRoutes) == 0 {
			// For policies targeting a gateway, when no httproutes is attached to the gateway, skip wasm config
			// test wasm config when no http routes attached to the gateway
			logger.V(1).Info("no free httproutes attached to the targeted gateway, skipping wasm config for the gateway rlp", "ratelimitpolicy", client.ObjectKeyFromObject(rlp))
			return nil, nil
		}

		freeRules := make([]gatewayapiv1.HTTPRouteRule, 0)
		for idx := range freeRoutes {
			freeroute := freeRoutes[idx]
			freeRules = append(freeRules, freeroute.Spec.Rules...)
		}

		gwHostnamesTmp := common.TargetHostnames(gw)
		gwHostnames := common.Map(gwHostnamesTmp, func(str string) gatewayapiv1.Hostname { return gatewayapiv1.Hostname(str) })
		route = &gatewayapiv1.HTTPRoute{
			Spec: gatewayapiv1.HTTPRouteSpec{
				Hostnames: gwHostnames,
				Rules:     freeRules,
			},
		}
	}

	return route, nil
}

// addHTTPRouteByGatewayIndexer declares an index key that we can later use with the client as a pseudo-field name,
// allowing to query all the routes parenting a given gateway
// to prevent creating the same index field multiple times, the function is declared private to be
// called only by this controller
func addHTTPRouteByGatewayIndexer(mgr ctrl.Manager, baseLogger logr.Logger) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &gatewayapiv1.HTTPRoute{}, HTTPRouteParents, func(rawObj client.Object) []string {
		// grab the route object, extract the parents
		route, assertionOk := rawObj.(*gatewayapiv1.HTTPRoute)
		if !assertionOk {
			return nil
		}

		logger := baseLogger.WithValues("route", client.ObjectKeyFromObject(route).String())

		keys := make([]string, 0)

		for _, parentRef := range route.Spec.CommonRouteSpec.ParentRefs {
			if !common.IsParentGateway(parentRef) {
				logger.V(1).Info("parent is not gateway", "ParentRefs", parentRef)
				continue
			}

			key := client.ObjectKey{
				Name:      string(parentRef.Name),
				Namespace: string(ptr.Deref(parentRef.Namespace, gatewayapiv1.Namespace(route.Namespace))),
			}

			logger.V(1).Info("new gateway added", "key", key.String())

			keys = append(keys, key.String())
		}

		return keys
	}); err != nil {
		return err
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RateLimitingWASMPluginReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Add custom indexer
	err := addHTTPRouteByGatewayIndexer(mgr, r.Logger().WithName("routeByGatewayIndexer"))
	if err != nil {
		return err
	}

	httpRouteToParentGatewaysEventMapper := &common.HTTPRouteToParentGatewaysEventMapper{
		Logger: r.Logger().WithName("httpRouteToParentGatewaysEventMapper"),
	}

	rlpToParentGatewaysEventMapper := &common.KuadrantPolicyToParentGatewaysEventMapper{
		Logger: r.Logger().WithName("ratelimitpolicyToParentGatewaysEventMapper"),
		Client: r.Client(),
	}

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
