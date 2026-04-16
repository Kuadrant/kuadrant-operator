package controllers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/google/cel-go/cel"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	istioextensionsv1alpha1 "istio.io/api/extensions/v1alpha1"
	istioapinetworkingv1alpha3 "istio.io/api/networking/v1alpha3"
	istiov1beta1 "istio.io/api/type/v1beta1"
	istioclientgoextensionv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	istioclientgonetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	celvalidator "github.com/kuadrant/kuadrant-operator/internal/cel"
	"github.com/kuadrant/kuadrant-operator/internal/extension"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/internal/gatewayapi"
	kuadrantistio "github.com/kuadrant/kuadrant-operator/internal/istio"
	kuadrantpolicymachinery "github.com/kuadrant/kuadrant-operator/internal/policymachinery"
	"github.com/kuadrant/kuadrant-operator/internal/utils"
	"github.com/kuadrant/kuadrant-operator/internal/wasm"
)

//+kubebuilder:rbac:groups=extensions.istio.io,resources=wasmplugins,verbs=get;list;watch;create;update;patch;delete

// IstioExtensionReconciler reconciles Istio WasmPlugin custom resources
type IstioExtensionReconciler struct {
	client *dynamic.DynamicClient
}

// IstioExtensionReconciler subscribes to events with potential impact on the Istio WasmPlugin custom resources
func (r *IstioExtensionReconciler) Subscription() controller.Subscription {
	return controller.Subscription{
		ReconcileFunc: r.Reconcile,
		Events: []controller.ResourceEventMatcher{
			{Kind: &kuadrantv1beta1.KuadrantGroupKind},
			{Kind: &machinery.GatewayClassGroupKind},
			{Kind: &machinery.GatewayGroupKind},
			{Kind: &machinery.HTTPRouteGroupKind},
			{Kind: &machinery.GRPCRouteGroupKind},
			{Kind: &kuadrantv1.RateLimitPolicyGroupKind},
			{Kind: &kuadrantv1alpha1.TokenRateLimitPolicyGroupKind},
			{Kind: &kuadrantv1.AuthPolicyGroupKind},
			{Kind: &kuadrantistio.WasmPluginGroupKind},
		},
	}
}

func (r *IstioExtensionReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("IstioExtensionReconciler").WithValues("context", ctx)

	logger.V(1).Info("building istio extension ", "image url", WASMFilterImageURL)
	defer logger.V(1).Info("finished building istio extension")

	// build wasm plugin configs for each gateway
	wasmConfigs, err := r.buildWasmConfigs(ctx, topology, state)
	if err != nil {
		if errors.Is(err, ErrMissingStateEffectiveAuthPolicies) || errors.Is(err, ErrMissingStateEffectiveRateLimitPolicies) {
			logger.V(1).Info(err.Error())
		} else {
			return err
		}
	}

	// reconcile for each gateway based on the desired wasm plugin policies calculated before
	gateways := lo.Map(topology.Targetables().Items(func(o machinery.Object) bool {
		return o.GroupVersionKind().GroupKind() == machinery.GatewayGroupKind
	}), func(g machinery.Targetable, _ int) *machinery.Gateway {
		return g.(*machinery.Gateway)
	})

	var modifiedGateways []string

	for _, gateway := range gateways {
		gatewayKey := k8stypes.NamespacedName{Name: gateway.GetName(), Namespace: gateway.GetNamespace()}

		// Get the wasm config for this gateway and apply mutators
		wasmConfig := wasmConfigs[gateway.GetLocator()]
		if err := extension.ApplyWasmConfigMutators(&wasmConfig, gateway); err != nil {
			logger.Error(err, "failed to apply wasm config mutators", "gateway", gatewayKey.String())
		}

		desiredWasmPlugin := buildIstioWasmPluginForGateway(gateway, wasmConfig, ProtectedRegistry, WASMFilterImageURL)

		resource := r.client.Resource(kuadrantistio.WasmPluginsResource).Namespace(desiredWasmPlugin.GetNamespace())

		existingWasmPluginObj, found := lo.Find(topology.Objects().Children(gateway), func(child machinery.Object) bool {
			return child.GroupVersionKind().GroupKind() == kuadrantistio.WasmPluginGroupKind && child.GetName() == desiredWasmPlugin.GetName() && child.GetNamespace() == desiredWasmPlugin.GetNamespace() && labels.Set(child.(*controller.RuntimeObject).GetLabels()).AsSelector().Matches(labels.Set(desiredWasmPlugin.GetLabels()))
		})

		// create
		if !found {
			if utils.IsObjectTaggedToDelete(desiredWasmPlugin) {
				continue
			}
			modifiedGateways = append(modifiedGateways, gateway.GetLocator()) // we only signal the gateway as modified when a wasmplugin is created, because updates won't change the status
			desiredWasmPluginUnstructured, err := controller.Destruct(desiredWasmPlugin)
			if err != nil {
				logger.Error(err, "failed to destruct wasmplugin object", "gateway", gatewayKey.String(), "wasmplugin", desiredWasmPlugin)
				continue
			}
			if _, err = resource.Create(ctx, desiredWasmPluginUnstructured, metav1.CreateOptions{}); err != nil {
				logger.Error(err, "failed to create wasmplugin object", "gateway", gatewayKey.String(), "wasmplugin", desiredWasmPluginUnstructured.Object)
				// TODO: handle error
			}
			continue
		}

		existingWasmPlugin := existingWasmPluginObj.(*controller.RuntimeObject).Object.(*istioclientgoextensionv1alpha1.WasmPlugin)

		// delete
		if utils.IsObjectTaggedToDelete(desiredWasmPlugin) && !utils.IsObjectTaggedToDelete(existingWasmPlugin) {
			if err := resource.Delete(ctx, existingWasmPlugin.GetName(), metav1.DeleteOptions{}); err != nil {
				logger.Error(err, "failed to delete wasmplugin object", "gateway", gatewayKey.String(), "wasmplugin", fmt.Sprintf("%s/%s", existingWasmPlugin.GetNamespace(), existingWasmPlugin.GetName()))
				// TODO: handle error
			}
			continue
		}
		logger.V(1).Info("wasmplugin object ", "desired", desiredWasmPlugin)
		if equalWasmPlugins(existingWasmPlugin, desiredWasmPlugin) {
			logger.V(1).Info("wasmplugin object is up to date, nothing to do")
			continue
		}

		// update
		existingWasmPlugin.Spec.Url = desiredWasmPlugin.Spec.Url
		existingWasmPlugin.Spec.Phase = desiredWasmPlugin.Spec.Phase
		existingWasmPlugin.Spec.TargetRefs = desiredWasmPlugin.Spec.TargetRefs
		existingWasmPlugin.Spec.PluginConfig = desiredWasmPlugin.Spec.PluginConfig
		existingWasmPlugin.Spec.ImagePullSecret = desiredWasmPlugin.Spec.ImagePullSecret

		existingWasmPluginUnstructured, err := controller.Destruct(existingWasmPlugin)
		if err != nil {
			logger.Error(err, "failed to destruct wasmplugin object", "gateway", gatewayKey.String(), "wasmplugin", existingWasmPlugin)
			continue
		}
		if _, err = resource.Update(ctx, existingWasmPluginUnstructured, metav1.UpdateOptions{}); err != nil {
			logger.Error(err, "failed to update wasmplugin object", "gateway", gatewayKey.String(), "wasmplugin", existingWasmPluginUnstructured.Object)
			// TODO: handle error
		}
	}

	state.Store(StateIstioExtensionsModified, modifiedGateways)

	// Reconcile EnvoyFilter cluster patches for registered upstreams
	r.reconcileUpstreamClusters(ctx, topology, gateways)

	return nil
}

func (r *IstioExtensionReconciler) reconcileUpstreamClusters(ctx context.Context, topology *machinery.Topology, gateways []*machinery.Gateway) {
	logger := controller.LoggerFromContext(ctx).WithName("IstioExtensionReconciler").WithName("reconcileUpstreamClusters")

	desiredEnvoyFilters := make(map[k8stypes.NamespacedName]struct{})

	for _, gateway := range gateways {
		gatewayKey := k8stypes.NamespacedName{Name: gateway.GetName(), Namespace: gateway.GetNamespace()}

		gatewayUpstreams := extension.GetRegisteredUpstreamsByTargetRef(extension.TargetRef{
			Group:     "gateway.networking.k8s.io",
			Kind:      "Gateway",
			Name:      gateway.GetName(),
			Namespace: gateway.GetNamespace(),
		})

		// Also collect upstreams registered for HTTPRoutes attached to this gateway
		gatewayUpstreams = append(gatewayUpstreams, extension.CollectHTTPRouteUpstreams(topology, gateway)...)

		if len(gatewayUpstreams) == 0 {
			continue
		}

		desiredEnvoyFilter, err := buildUpstreamEnvoyFilter(gateway, gatewayUpstreams)
		if err != nil {
			logger.Error(err, "failed to build upstream envoy filter", "gateway", gatewayKey.String())
			continue
		}
		desiredEnvoyFilters[k8stypes.NamespacedName{Name: desiredEnvoyFilter.GetName(), Namespace: desiredEnvoyFilter.GetNamespace()}] = struct{}{}

		resource := r.client.Resource(kuadrantistio.EnvoyFiltersResource).Namespace(desiredEnvoyFilter.GetNamespace())

		existingObj, found := lo.Find(topology.Objects().Children(gateway), func(child machinery.Object) bool {
			return child.GroupVersionKind().GroupKind() == kuadrantistio.EnvoyFilterGroupKind &&
				child.GetName() == desiredEnvoyFilter.GetName() &&
				child.GetNamespace() == desiredEnvoyFilter.GetNamespace() &&
				labels.Set(child.(*controller.RuntimeObject).GetLabels()).AsSelector().Matches(labels.Set(desiredEnvoyFilter.GetLabels()))
		})

		if !found {
			unstructured, err := controller.Destruct(desiredEnvoyFilter)
			if err != nil {
				logger.Error(err, "failed to destruct envoyfilter", "gateway", gatewayKey.String())
				continue
			}
			if _, err = resource.Create(ctx, unstructured, metav1.CreateOptions{}); err != nil {
				logger.Error(err, "failed to create upstream envoyfilter", "gateway", gatewayKey.String())
			}
			continue
		}

		existing := existingObj.(*controller.RuntimeObject).Object.(*istioclientgonetworkingv1alpha3.EnvoyFilter)
		if kuadrantistio.EqualEnvoyFilters(existing, desiredEnvoyFilter) {
			continue
		}

		existing.Spec = istioapinetworkingv1alpha3.EnvoyFilter{
			TargetRefs:    desiredEnvoyFilter.Spec.TargetRefs,
			ConfigPatches: desiredEnvoyFilter.Spec.ConfigPatches,
		}
		unstructured, err := controller.Destruct(existing)
		if err != nil {
			logger.Error(err, "failed to destruct envoyfilter", "gateway", gatewayKey.String())
			continue
		}
		if _, err = resource.Update(ctx, unstructured, metav1.UpdateOptions{}); err != nil {
			logger.Error(err, "failed to update upstream envoyfilter", "gateway", gatewayKey.String())
		}
	}

	// Cleanup stale upstream EnvoyFilters
	stale := topology.Objects().Items(func(o machinery.Object) bool {
		_, desired := desiredEnvoyFilters[k8stypes.NamespacedName{Name: o.GetName(), Namespace: o.GetNamespace()}]
		return o.GroupVersionKind().GroupKind() == kuadrantistio.EnvoyFilterGroupKind &&
			labels.Set(o.(*controller.RuntimeObject).GetLabels()).AsSelector().Matches(UpstreamObjectLabels()) &&
			!desired
	})
	for _, ef := range stale {
		if err := r.client.Resource(kuadrantistio.EnvoyFiltersResource).Namespace(ef.GetNamespace()).Delete(ctx, ef.GetName(), metav1.DeleteOptions{}); err != nil {
			logger.Error(err, "failed to delete stale upstream envoyfilter", "envoyfilter", fmt.Sprintf("%s/%s", ef.GetNamespace(), ef.GetName()))
		}
	}
}

func buildUpstreamEnvoyFilter(gateway *machinery.Gateway, upstreams []extension.RegisteredUpstreamEntry) (*istioclientgonetworkingv1alpha3.EnvoyFilter, error) {
	envoyFilter := &istioclientgonetworkingv1alpha3.EnvoyFilter{
		TypeMeta: metav1.TypeMeta{
			Kind:       kuadrantistio.EnvoyFilterGroupKind.Kind,
			APIVersion: istioclientgonetworkingv1alpha3.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      UpstreamClusterName(gateway.GetName()),
			Namespace: gateway.GetNamespace(),
			Labels:    UpstreamObjectLabels(),
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         gateway.GroupVersionKind().GroupVersion().String(),
					Kind:               gateway.GroupVersionKind().Kind,
					Name:               gateway.Name,
					UID:                gateway.UID,
					BlockOwnerDeletion: ptr.To(true),
					Controller:         ptr.To(true),
				},
			},
		},
		Spec: istioapinetworkingv1alpha3.EnvoyFilter{
			TargetRefs: []*istiov1beta1.PolicyTargetReference{
				{
					Group: machinery.GatewayGroupKind.Group,
					Kind:  machinery.GatewayGroupKind.Kind,
					Name:  gateway.GetName(),
				},
			},
		},
	}

	var allPatches []*istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch
	seen := make(map[string]struct{})
	for _, entry := range upstreams {
		if _, exists := seen[entry.ClusterName]; exists {
			continue
		}
		seen[entry.ClusterName] = struct{}{}
		patches, err := kuadrantistio.BuildEnvoyFilterClusterPatch(entry.Host, entry.Port, false, func(h string, p int, _ bool) map[string]any {
			return buildClusterPatch(entry.ClusterName, h, p, false)
		})
		if err != nil {
			return nil, fmt.Errorf("failed to build cluster patch for %q: %w", entry.ClusterName, err)
		}
		allPatches = append(allPatches, patches...)
	}
	envoyFilter.Spec.ConfigPatches = allPatches

	return envoyFilter, nil
}

// buildWasmConfigs returns a map of istio gateway locators to an ordered list of corresponding wasm policies
func (r *IstioExtensionReconciler) buildWasmConfigs(ctx context.Context, topology *machinery.Topology, state *sync.Map) (map[string]wasm.Config, error) {
	logger := controller.LoggerFromContext(ctx).WithName("IstioExtensionReconciler").WithName("buildWasmConfigs").WithValues("context", ctx)
	logger.Info("build Wasm configuration", "status", "started")
	logger.Info("build Wasm configuration", "status", "completed")

	serviceBuilder := wasm.NewServiceBuilder(&logger)
	// Get Kuadrant CR to access observability settings
	kObj := GetKuadrantFromTopology(topology)
	var observability *wasm.Observability
	if kObj != nil {
		observability = wasm.BuildObservabilityConfig(serviceBuilder, &kObj.Spec.Observability)
	}

	effectiveAuthPolicies, ok := state.Load(StateEffectiveAuthPolicies)
	if !ok {
		return nil, ErrMissingStateEffectiveAuthPolicies
	}
	effectiveAuthPoliciesMap := effectiveAuthPolicies.(EffectiveAuthPolicies)

	var effectiveRateLimitPoliciesMap EffectiveRateLimitPolicies
	if effectiveRateLimitPolicies, ok := state.Load(StateEffectiveRateLimitPolicies); ok {
		effectiveRateLimitPoliciesMap = effectiveRateLimitPolicies.(EffectiveRateLimitPolicies)
	} else {
		logger.V(1).Info("no effective rate limit policies found in state, continuing with empty map")
	}

	var effectiveTokenRateLimitPoliciesMap EffectiveTokenRateLimitPolicies
	if effectiveTokenRateLimitPolicies, ok := state.Load(StateEffectiveTokenRateLimitPolicies); ok {
		effectiveTokenRateLimitPoliciesMap = effectiveTokenRateLimitPolicies.(EffectiveTokenRateLimitPolicies)
	} else {
		logger.V(1).Info("no effective token rate limit policies found in state, continuing with empty map")
	}

	logger.V(1).Info("building wasm configs for istio extension", "effectiveAuthPolicies", len(effectiveAuthPoliciesMap), "effectiveRateLimitPolicies", len(effectiveRateLimitPoliciesMap), "effectiveTokenRateLimitPolicies", len(effectiveTokenRateLimitPoliciesMap))

	// unique paths from different policy types
	var allPaths []lo.Entry[string, []machinery.Targetable]

	// paths from auth ratelimit and tokenratelimit policies
	authPaths := lo.Entries(lo.MapValues(effectiveAuthPoliciesMap, func(p EffectiveAuthPolicy, _ string) []machinery.Targetable { return p.Path }))
	allPaths = append(allPaths, authPaths...)
	rateLimitPaths := lo.Entries(lo.MapValues(effectiveRateLimitPoliciesMap, func(p EffectiveRateLimitPolicy, _ string) []machinery.Targetable { return p.Path }))
	allPaths = append(allPaths, rateLimitPaths...)
	tokenRateLimitPaths := lo.Entries(lo.MapValues(effectiveTokenRateLimitPoliciesMap, func(p EffectiveTokenRateLimitPolicy, _ string) []machinery.Targetable { return p.Path }))
	allPaths = append(allPaths, tokenRateLimitPaths...)

	// unique paths by key
	paths := lo.UniqBy(allPaths, func(e lo.Entry[string, []machinery.Targetable]) string { return e.Key })

	logger.V(1).Info("processing paths for wasm config", "totalPaths", len(paths))

	authPathIDs := lo.Keys(effectiveAuthPoliciesMap)
	logger.V(1).Info("effective auth policy pathIDs", "count", len(authPathIDs), "pathIDs", authPathIDs)

	wasmActionSets := kuadrantgatewayapi.GrouppedHTTPRouteMatchConfigs{}
	celValidationIssues := celvalidator.NewIssueCollection()

	tracer := controller.TracerFromContext(ctx)

	// build the wasm policies for each topological path that contains an effective rate limit policy affecting an istio gateway
	for i := range paths {
		pathID := paths[i].Key
		path := paths[i].Value

		logger.V(1).Info("processing path", "pathID", pathID, "pathLength", len(path))

		parsed, pathErr := kuadrantpolicymachinery.ParseTopologyPath(path)
		if pathErr != nil {
			logger.V(1).Info("skipping path - failed to parse", "pathID", pathID, "error", pathErr)
			continue
		}

		// ignore if not an istio gateway
		if !lo.Contains(istioGatewayControllerNames, parsed.GatewayClass.Spec.ControllerName) {
			continue
		}

		// Create a parent span for this entire path processing
		pathCtx, pathSpan := tracer.Start(ctx, "wasm.BuildConfigForPath")
		pathSpan.SetAttributes(
			attribute.String("path_id", pathID),
			attribute.String("route_type", parsed.RouteType.String()),
			attribute.String("gateway.name", parsed.Gateway.GetName()),
			attribute.String("gateway.namespace", parsed.Gateway.GetNamespace()),
			attribute.String("listener.name", string(parsed.Listener.Name)),
			attribute.String("route.name", parsed.GetRouteName()),
			attribute.String("route.namespace", parsed.GetRouteNamespace()),
		)

		validatorBuilder := celvalidator.NewRootValidatorBuilder()

		var actions []wasm.Action

		// auth
		if effectivePolicy, ok := effectiveAuthPoliciesMap[pathID]; ok {
			actions = append(actions, buildWasmActionsForAuth(pathID, effectivePolicy)...)
			validatorBuilder.PushPolicyBinding(celvalidator.AuthPolicyKind, celvalidator.AuthPolicyName, cel.AnyType)
		}

		// rate limit
		if effectivePolicy, ok := effectiveRateLimitPoliciesMap[pathID]; ok {
			rlAction := buildWasmActionsForRateLimit(effectivePolicy, isRateLimitPolicyAcceptedAndNotDeletedFunc(state))
			if hasAuthAccess(rlAction) {
				actions = append(actions, rlAction...)
			} else {
				// pre auth rate limiting
				actions = append(rlAction, actions...)
			}
			validatorBuilder.PushPolicyBinding(celvalidator.RateLimitPolicyKind, celvalidator.RateLimitName, cel.AnyType)
		}

		if effectivePolicy, ok := effectiveTokenRateLimitPoliciesMap[pathID]; ok {
			trlAction := buildWasmActionsForTokenRateLimit(effectivePolicy, isTokenRateLimitPolicyAcceptedAndNotDeletedFunc(state))
			if hasAuthAccess(trlAction) {
				actions = append(actions, trlAction...)
			} else {
				// pre auth rate limiting
				actions = append(trlAction, actions...)
			}
			validatorBuilder.PushPolicyBinding(celvalidator.TokenRateLimitPolicyKind, celvalidator.RateLimitName, cel.AnyType)
		}

		pathSpan.SetAttributes(attribute.Int("actions.before_merge", len(actions)))

		// Extract and track source policies before merging
		sourcePolicies := lo.Uniq(lo.FlatMap(actions, func(a wasm.Action, _ int) []string {
			return a.SourcePolicyLocators
		}))
		if len(sourcePolicies) > 0 {
			pathSpan.SetAttributes(attribute.StringSlice("source_policies", sourcePolicies))
		}

		actions, err := mergeAndVerify(pathCtx, actions)
		if err != nil {
			pathSpan.RecordError(err)
			pathSpan.SetStatus(codes.Error, "failed to merge/verify actions")
			pathSpan.End()
			return nil, fmt.Errorf("failed to merge/verify actions for path %s: %w", pathID, err)
		}

		if len(actions) == 0 {
			pathSpan.SetStatus(codes.Ok, "no actions after merge")
			pathSpan.End()
			continue
		}

		validator, err := validatorBuilder.Build()
		if err != nil {
			pathSpan.RecordError(err)
			pathSpan.SetStatus(codes.Error, "failed to build validator")
			pathSpan.End()
			return nil, fmt.Errorf("failed to build validator for path %s: %w", pathID, err)
		}
		var validatedActions []wasm.Action

		for _, action := range actions {
			if err := celvalidator.ValidateWasmAction(action, validator); err != nil {
				logger.V(1).Info("WASM action is invalid", "action", action, "path", pathID, "error", err)
				celValidationIssues.Add(celvalidator.NewIssue(action, pathID, err))
			} else {
				validatedActions = append(validatedActions, action)
			}
		}

		pathSpan.SetAttributes(
			attribute.Int("actions.after_merge", len(actions)),
			attribute.Int("actions.validated", len(validatedActions)),
			attribute.Int("actions.invalid", len(actions)-len(validatedActions)),
		)

		if len(validatedActions) == 0 {
			pathSpan.SetStatus(codes.Ok, "no validated actions")
			pathSpan.End()
			continue
		}

		wasmActionSetsForPath, err := wasm.BuildActionSetsForPath(pathCtx, pathID, path, validatedActions)
		if err != nil {
			if errors.As(err, &kuadrantpolicymachinery.ErrInvalidPath{}) {
				logger.V(1).Info("ingoring invalid paths", "error", err.Error(), "status", "skipping", "pathID", pathID)
				pathSpan.SetStatus(codes.Ok, "invalid path - skipped")
				pathSpan.End()
				continue
			}
			logger.Error(err, "failed to build wasm policies for path", "pathID", pathID, "status", "error")
			pathSpan.RecordError(err)
			pathSpan.SetStatus(codes.Error, "failed to build action sets")
			pathSpan.End()
			continue
		}

		pathSpan.SetAttributes(attribute.Int("actionsets.created", len(wasmActionSetsForPath)))
		pathSpan.SetStatus(codes.Ok, "")
		pathSpan.End()

		wasmActionSets.Add(parsed.Gateway.GetLocator(), wasmActionSetsForPath...)
	}

	if !celValidationIssues.IsEmpty() {
		state.Store(celvalidator.StateCELValidationErrors, celValidationIssues)
	}

	wasmConfigs := lo.MapValues(wasmActionSets.Sorted(), func(configs kuadrantgatewayapi.SortableHTTPRouteMatchConfigs, _ string) wasm.Config {
		return wasm.BuildConfigForActionSet(lo.Map(configs, func(c kuadrantgatewayapi.HTTPRouteMatchConfig, _ int) wasm.ActionSet {
			return c.Config.(wasm.ActionSet)
		}), &logger, observability, serviceBuilder)
	})

	return wasmConfigs, nil
}

func hasAuthAccess(actionSet []wasm.Action) bool {
	for _, action := range actionSet {
		if action.HasAuthAccess() {
			return true
		}
	}
	return false
}

// buildIstioWasmPluginForGateway builds a desired WasmPlugin custom resource for a given gateway and corresponding wasm config
func buildIstioWasmPluginForGateway(gateway *machinery.Gateway, wasmConfig wasm.Config, protectedRegistry, imageURL string) *istioclientgoextensionv1alpha1.WasmPlugin {
	wasmPlugin := &istioclientgoextensionv1alpha1.WasmPlugin{
		TypeMeta: metav1.TypeMeta{
			Kind:       kuadrantistio.WasmPluginGroupKind.Kind,
			APIVersion: istioclientgoextensionv1alpha1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      wasm.ExtensionName(gateway.GetName()),
			Namespace: gateway.GetNamespace(),
			Labels:    KuadrantManagedObjectLabels(),
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         gateway.GroupVersionKind().GroupVersion().String(),
					Kind:               gateway.GroupVersionKind().Kind,
					Name:               gateway.Name,
					UID:                gateway.UID,
					BlockOwnerDeletion: ptr.To(true),
					Controller:         ptr.To(true),
				},
			},
		},
		Spec: istioextensionsv1alpha1.WasmPlugin{
			TargetRefs: []*istiov1beta1.PolicyTargetReference{
				{
					Group: machinery.GatewayGroupKind.Group,
					Kind:  machinery.GatewayGroupKind.Kind,
					Name:  gateway.GetName(),
				},
			},
			Url:          imageURL,
			PluginConfig: nil,
			Phase:        istioextensionsv1alpha1.PluginPhase_STATS, // insert the plugin before Istio stats filters and after Istio authorization filters.
		},
	}
	// reset to empty to allow fo the image having moved to a public registry
	wasmPlugin.Spec.ImagePullSecret = ""
	// only set to pull secret if we are in a protected registry
	if protectedRegistry != "" && strings.Contains(imageURL, protectedRegistry) {
		wasmPlugin.Spec.ImagePullSecret = RegistryPullSecretName
	}

	if len(wasmConfig.ActionSets) == 0 {
		utils.TagObjectToDelete(wasmPlugin)
	} else {
		pluginConfigStruct, err := wasmConfig.ToStruct()
		if err != nil {
			return nil
		}
		wasmPlugin.Spec.PluginConfig = pluginConfigStruct
	}

	return wasmPlugin
}

func equalWasmPlugins(a, b *istioclientgoextensionv1alpha1.WasmPlugin) bool {
	if a.Spec.ImagePullSecret != b.Spec.ImagePullSecret || a.Spec.Url != b.Spec.Url || a.Spec.Phase != b.Spec.Phase || !kuadrantistio.EqualTargetRefs(a.Spec.TargetRefs, b.Spec.TargetRefs) {
		return false
	}

	if a.Spec.PluginConfig == nil && b.Spec.PluginConfig == nil {
		return true
	}

	var err error

	var aConfig *wasm.Config
	var bConfig *wasm.Config

	if a.Spec.PluginConfig != nil {
		aConfig, err = wasm.ConfigFromStruct(a.Spec.PluginConfig)
		if err != nil {
			return false
		}
	}

	if b.Spec.PluginConfig != nil {
		bConfig, err = wasm.ConfigFromStruct(b.Spec.PluginConfig)
		if err != nil {
			return false
		}
	}

	return aConfig != nil && bConfig != nil && aConfig.EqualTo(bConfig)
}
