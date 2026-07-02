package controllers

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/go-logr/logr"
	"github.com/google/cel-go/cel"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	istioapinetworkingv1alpha3 "istio.io/api/networking/v1alpha3"
	istiov1beta1 "istio.io/api/type/v1beta1"
	istioclientgonetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/env"
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

//+kubebuilder:rbac:groups=extensions.istio.io,resources=wasmplugins,verbs=delete
//+kubebuilder:rbac:groups=networking.istio.io,resources=envoyfilters,verbs=get;list;watch;create;update;patch;delete

// IstioExtensionReconciler reconciles Istio EnvoyFilter custom resources for wasm plugin injection
type IstioExtensionReconciler struct {
	client *dynamic.DynamicClient
}

// IstioExtensionReconciler subscribes to events with potential impact on the Istio EnvoyFilter custom resources
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
			{Kind: &kuadrantistio.EnvoyFilterGroupKind},
		},
	}
}

func (r *IstioExtensionReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("IstioExtensionReconciler").WithValues("context", ctx)

	operatorNamespace := env.GetString("OPERATOR_NAMESPACE", "kuadrant-system")
	wasmServerHost := fmt.Sprintf("kuadrant-operator-wasm.%s.svc.cluster.local", operatorNamespace)
	wasmServerPort, portErr := env.GetInt("WASM_SERVER_PORT", defaultWasmServerPort)
	if portErr != nil {
		wasmServerPort = defaultWasmServerPort
	}
	wasmURL := fmt.Sprintf("http://%s:%d/plugin.wasm", wasmServerHost, wasmServerPort)

	logger.V(1).Info("building istio extension", "wasm url", wasmURL)
	defer logger.V(1).Info("finished building istio extension")

	// reconcile for each gateway based on the desired wasm plugin policies calculated before
	gateways := lo.Map(topology.Targetables().Items(func(o machinery.Object) bool {
		return o.GroupVersionKind().GroupKind() == machinery.GatewayGroupKind
	}), func(g machinery.Targetable, _ int) *machinery.Gateway {
		return g.(*machinery.Gateway)
	})

	// Reconcile EnvoyFilter cluster patches for registered upstreams
	r.reconcileUpstreamClusters(ctx, topology, gateways)

	// build wasm plugin configs for each gateway
	wasmConfigs, err := r.buildWasmConfigs(ctx, topology, state)
	if err != nil {
		if errors.Is(err, ErrMissingStateEffectiveAuthPolicies) || errors.Is(err, ErrMissingStateEffectiveRateLimitPolicies) {
			logger.V(1).Info(err.Error())
		} else {
			return err
		}
	}

	modifiedGateways := make([]string, 0, len(gateways))

	for _, gateway := range gateways {
		gatewayKey := k8stypes.NamespacedName{Name: gateway.GetName(), Namespace: gateway.GetNamespace()}

		// Get the wasm config for this gateway and apply mutators
		wasmConfig := wasmConfigs[gateway.GetLocator()]
		if err := extension.ApplyWasmConfigMutators(&wasmConfig, gateway, topology); err != nil {
			logger.Error(err, "failed to apply wasm config mutators", "gateway", gatewayKey.String())
		}

		desiredEnvoyFilter := buildIstioEnvoyFilterForGateway(gateway, wasmConfig, wasmURL, wasmServerHost, wasmServerPort, WasmFileSHA256)

		resource := r.client.Resource(kuadrantistio.EnvoyFiltersResource).Namespace(desiredEnvoyFilter.GetNamespace())

		existingEnvoyFilterObj, found := lo.Find(topology.Objects().Children(gateway), func(child machinery.Object) bool {
			return child.GroupVersionKind().GroupKind() == kuadrantistio.EnvoyFilterGroupKind && child.GetName() == desiredEnvoyFilter.GetName() && child.GetNamespace() == desiredEnvoyFilter.GetNamespace() && labels.Set(child.(*controller.RuntimeObject).GetLabels()).AsSelector().Matches(labels.Set(desiredEnvoyFilter.GetLabels()))
		})

		// create
		if !found {
			if utils.IsObjectTaggedToDelete(desiredEnvoyFilter) {
				continue
			}
			modifiedGateways = append(modifiedGateways, gateway.GetLocator()) // we only signal the gateway as modified when an envoyfilter is created, because updates won't change the status
			desiredEnvoyFilterUnstructured, err := controller.Destruct(desiredEnvoyFilter)
			if err != nil {
				logger.Error(err, "failed to destruct envoyfilter object", "gateway", gatewayKey.String(), "envoyfilter", desiredEnvoyFilter)
				continue
			}
			if _, err = resource.Create(ctx, desiredEnvoyFilterUnstructured, metav1.CreateOptions{}); err != nil {
				logger.Error(err, "failed to create envoyfilter object", "gateway", gatewayKey.String(), "envoyfilter", desiredEnvoyFilterUnstructured.Object)
				// TODO: handle error
			}
			continue
		}

		// Clean up old WasmPlugin for this specific gateway - temporary to be removed
		wasmPluginName := wasm.ExtensionName(gateway.GetName())
		if err := r.client.Resource(kuadrantistio.WasmPluginsResource).Namespace(gateway.GetNamespace()).Delete(ctx, wasmPluginName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			logger.Error(err, "failed to delete old wasmplugin", "gateway", gatewayKey.String(), "wasmplugin", wasmPluginName)
		}

		existingEnvoyFilter := existingEnvoyFilterObj.(*controller.RuntimeObject).Object.(*istioclientgonetworkingv1alpha3.EnvoyFilter)

		// delete
		if utils.IsObjectTaggedToDelete(desiredEnvoyFilter) && !utils.IsObjectTaggedToDelete(existingEnvoyFilter) {
			if err := resource.Delete(ctx, existingEnvoyFilter.GetName(), metav1.DeleteOptions{}); err != nil {
				logger.Error(err, "failed to delete envoyfilter object", "gateway", gatewayKey.String(), "envoyfilter", fmt.Sprintf("%s/%s", existingEnvoyFilter.GetNamespace(), existingEnvoyFilter.GetName()))
				// TODO: handle error
			}
			continue
		}
		logger.V(1).Info("envoyfilter object ", "desired", desiredEnvoyFilter)
		if kuadrantistio.EqualEnvoyFilters(existingEnvoyFilter, desiredEnvoyFilter) {
			logger.V(1).Info("envoyfilter object is up to date, nothing to do")
			continue
		}

		// update
		existingEnvoyFilter.Spec.ConfigPatches = desiredEnvoyFilter.Spec.ConfigPatches
		existingEnvoyFilter.Spec.Priority = desiredEnvoyFilter.Spec.Priority
		existingEnvoyFilter.Spec.TargetRefs = desiredEnvoyFilter.Spec.TargetRefs

		existingEnvoyFilterUnstructured, err := controller.Destruct(existingEnvoyFilter)
		if err != nil {
			logger.Error(err, "failed to destruct envoyfilter object", "gateway", gatewayKey.String(), "envoyfilter", existingEnvoyFilter)
			continue
		}
		if _, err = resource.Update(ctx, existingEnvoyFilterUnstructured, metav1.UpdateOptions{}); err != nil {
			logger.Error(err, "failed to update envoyfilter object", "gateway", gatewayKey.String(), "envoyfilter", existingEnvoyFilterUnstructured.Object)
			// TODO: handle error
		}
	}

	state.Store(StateIstioExtensionsModified, modifiedGateways)

	return nil
}

func (r *IstioExtensionReconciler) reconcileUpstreamClusters(ctx context.Context, topology *machinery.Topology, gateways []*machinery.Gateway) {
	logger := controller.LoggerFromContext(ctx).WithName("IstioExtensionReconciler").WithName("reconcileUpstreamClusters")

	desiredEnvoyFilters := make(map[k8stypes.NamespacedName]struct{})

	for _, gateway := range gateways {
		// Skip non-Istio gateways
		gatewayClassObjs := topology.Targetables().Parents(gateway)
		gatewayClassObj, found := lo.Find(gatewayClassObjs, func(obj machinery.Targetable) bool {
			return obj.GroupVersionKind().GroupKind() == machinery.GatewayClassGroupKind
		})
		if found {
			gatewayClass := gatewayClassObj.(*machinery.GatewayClass)
			if !lo.Contains(istioGatewayControllerNames, gatewayClass.Spec.ControllerName) {
				continue
			}
		}

		gatewayKey := k8stypes.NamespacedName{Name: gateway.GetName(), Namespace: gateway.GetNamespace()}

		gatewayUpstreams := extension.GetRegisteredUpstreamsByTargetRef(extension.TargetRef{
			Group:     "gateway.networking.k8s.io",
			Kind:      "Gateway",
			Name:      gateway.GetName(),
			Namespace: gateway.GetNamespace(),
		})

		// Also collect upstreams registered for routes attached to this gateway
		gatewayUpstreams = append(gatewayUpstreams, extension.CollectRouteUpstreams(topology, gateway)...)

		if len(gatewayUpstreams) == 0 {
			continue
		}

		desiredEnvoyFilter, err := buildUpstreamEnvoyFilter(logger, gateway, gatewayUpstreams)
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

func buildUpstreamEnvoyFilter(logger logr.Logger, gateway *machinery.Gateway, upstreams []extension.RegisteredUpstreamEntry) (*istioclientgonetworkingv1alpha3.EnvoyFilter, error) {
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

	// Add descriptor service cluster (can not conflict with extension upstreams due to ext- prefix)
	operatorNamespace := env.GetString("OPERATOR_NAMESPACE", "kuadrant-system")
	descriptorServiceHost := fmt.Sprintf("kuadrant-operator-grpc.%s.svc.cluster.local", operatorNamespace)
	const defaultDescriptorPort = 50051
	descriptorServicePort, portErr := env.GetInt("EXTENSIONS_DESCRIPTOR_SERVICE_PORT", defaultDescriptorPort)
	if portErr != nil {
		logger.Error(portErr, "invalid EXTENSIONS_DESCRIPTOR_SERVICE_PORT, using default", "default", defaultDescriptorPort)
		descriptorServicePort = defaultDescriptorPort
	}

	descriptorPatches, err := kuadrantistio.BuildEnvoyFilterClusterPatch(descriptorServiceHost, descriptorServicePort, false, func(h string, p int, _ bool) map[string]any {
		return buildClusterPatch("kuadrant-operator-grpc", h, p, false)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to build cluster patch for descriptor service: %w", err)
	}
	allPatches = append(allPatches, descriptorPatches...)

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
	kObj := GetKuadrantFromTopology(topology, state)
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

	wasmActionSets := kuadrantgatewayapi.GroupedHTTPRouteMatchConfigs{}
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

		var specs []wasm.ActionSpec

		// auth
		if effectivePolicy, ok := effectiveAuthPoliciesMap[pathID]; ok {
			specs = append(specs, buildWasmActionSpecsForAuth(pathID, effectivePolicy)...)
			validatorBuilder.PushPolicyBinding(celvalidator.AuthPolicyKind, celvalidator.AuthPolicyName, cel.AnyType)
		}

		// rate limit
		if effectivePolicy, ok := effectiveRateLimitPoliciesMap[pathID]; ok {
			rlSpecs := buildWasmActionSpecsForRateLimit(effectivePolicy, isRateLimitPolicyAcceptedAndNotDeletedFunc(state))
			if specsHaveAuthAccess(rlSpecs) {
				specs = append(specs, rlSpecs...)
			} else {
				// pre auth rate limiting
				specs = append(rlSpecs, specs...)
			}
			validatorBuilder.PushPolicyBinding(celvalidator.RateLimitPolicyKind, celvalidator.RateLimitName, cel.AnyType)
		}

		if effectivePolicy, ok := effectiveTokenRateLimitPoliciesMap[pathID]; ok {
			trlSpecs := buildWasmActionSpecsForTokenRateLimit(effectivePolicy, isTokenRateLimitPolicyAcceptedAndNotDeletedFunc(state))
			if specsHaveAuthAccess(trlSpecs) {
				specs = append(specs, trlSpecs...)
			} else {
				// pre auth rate limiting
				specs = append(trlSpecs, specs...)
			}
			validatorBuilder.PushPolicyBinding(celvalidator.TokenRateLimitPolicyKind, celvalidator.RateLimitName, cel.AnyType)
		}

		pathSpan.SetAttributes(attribute.Int("specs.before_merge", len(specs)))

		// Extract and track source policies before merging
		sourcePolicies := lo.Uniq(lo.FlatMap(specs, func(s wasm.ActionSpec, _ int) []string {
			return s.Sources
		}))
		if len(sourcePolicies) > 0 {
			pathSpan.SetAttributes(attribute.StringSlice("source_policies", sourcePolicies))
		}

		specs, err := mergeAndVerifySpecs(pathCtx, specs)
		if err != nil {
			pathSpan.RecordError(err)
			pathSpan.SetStatus(codes.Error, "failed to merge/verify specs")
			pathSpan.End()
			return nil, fmt.Errorf("failed to merge/verify action specs for path %s: %w", pathID, err)
		}

		if len(specs) == 0 {
			pathSpan.SetStatus(codes.Ok, "no specs after merge")
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

		// Validate specs, then Build() validated ones into Actions
		var builtActions []wasm.Action
		invalidCount := 0

		for _, spec := range specs {
			if err := celvalidator.ValidateWasmActionSpec(spec, validator); err != nil {
				logger.V(1).Info("WASM action spec is invalid", "spec", spec, "path", pathID, "error", err)
				celValidationIssues.Add(celvalidator.NewIssue(spec, pathID, err))
				invalidCount++
			} else {
				builtActions = append(builtActions, spec.Build())
			}
		}

		pathSpan.SetAttributes(
			attribute.Int("specs.after_merge", len(specs)),
			attribute.Int("specs.validated", len(builtActions)),
			attribute.Int("specs.invalid", invalidCount),
		)

		if len(builtActions) == 0 {
			pathSpan.SetStatus(codes.Ok, "no validated actions")
			pathSpan.End()
			continue
		}

		wasmActionSetsForPath, err := wasm.BuildActionSetsForPath(pathCtx, pathID, path, builtActions)
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

func specsHaveAuthAccess(specs []wasm.ActionSpec) bool {
	for _, spec := range specs {
		if spec.HasAuthAccess() {
			return true
		}
	}
	return false
}

// buildIstioEnvoyFilterForGateway builds a desired EnvoyFilter custom resource for a given gateway and corresponding wasm config
func buildIstioEnvoyFilterForGateway(gateway *machinery.Gateway, wasmConfig wasm.Config, wasmURL, wasmServerHost string, wasmServerPort int, imageSHA string) *istioclientgonetworkingv1alpha3.EnvoyFilter {
	var configPatches []*istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch

	if len(wasmConfig.ActionSets) > 0 {
		pluginConfigStruct, err := wasmConfig.ToStruct()
		if err == nil {
			patches, err := kuadrantistio.BuildEnvoyFilterWasmPatch(wasmURL, "", imageSHA, WasmServerClusterName, pluginConfigStruct)
			if err == nil {
				configPatches = patches
			}
		}

		clusterPatches, err := kuadrantistio.BuildEnvoyFilterClusterPatch(wasmServerHost, wasmServerPort, false, func(h string, p int, _ bool) map[string]any {
			return buildClusterPatch(WasmServerClusterName, h, p, false)
		})
		if err == nil {
			configPatches = append(configPatches, clusterPatches...)
		}
	}

	envoyFilter := &istioclientgonetworkingv1alpha3.EnvoyFilter{
		TypeMeta: metav1.TypeMeta{
			Kind:       kuadrantistio.EnvoyFilterGroupKind.Kind,
			APIVersion: istioclientgonetworkingv1alpha3.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      wasm.ExtensionName(gateway.GetName()),
			Namespace: gateway.GetNamespace(),
			Labels:    labels.Set(map[string]string{kuadrantManagedLabelKey: "true", "kuadrant.io/wasm": "true"}),
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
			ConfigPatches: configPatches,
		},
	}

	if len(wasmConfig.ActionSets) == 0 {
		utils.TagObjectToDelete(envoyFilter)
	}

	return envoyFilter
}
