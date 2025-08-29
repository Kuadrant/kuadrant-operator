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
	istioextensionsv1alpha1 "istio.io/api/extensions/v1alpha1"
	istiov1beta1 "istio.io/api/type/v1beta1"
	istioclientgoextensionv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	celvalidator "github.com/kuadrant/kuadrant-operator/internal/cel"
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
			{Kind: &kuadrantv1.RateLimitPolicyGroupKind},
			{Kind: &kuadrantv1alpha1.TokenRateLimitPolicyGroupKind},
			{Kind: &kuadrantv1.AuthPolicyGroupKind},
			{Kind: &kuadrantistio.WasmPluginGroupKind},
		},
	}
}

func (r *IstioExtensionReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("IstioExtensionReconciler")

	logger.V(1).Info("building istio extension ", "image url", WASMFilterImageURL)
	defer logger.V(1).Info("finished building istio extension")

	// build wasm plugin configs for each gateway
	wasmConfigs, err := r.buildWasmConfigs(ctx, state)
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

		desiredWasmPlugin := buildIstioWasmPluginForGateway(gateway, wasmConfigs[gateway.GetLocator()], ProtectedRegistry, WASMFilterImageURL)

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

	return nil
}

func mergeAndVerify(actions []wasm.Action) ([]wasm.Action, error) {
	if len(actions) == 0 {
		return nil, nil
	}

	result := []wasm.Action{actions[0]}
	for _, currentAction := range actions[1:] {
		lastAction := &result[len(result)-1]

		if lastAction.Scope == currentAction.Scope &&
			lastAction.ServiceName == currentAction.ServiceName && lastAction.ServiceName != wasm.AuthServiceName {
			lastAction.ConditionalData = append(lastAction.ConditionalData, currentAction.ConditionalData...)
		} else {
			result = append(result, currentAction)
		}
	}

	for i := range result {
		keyValueMap := make(map[string]string)
		for _, conditionalData := range result[i].ConditionalData {
			for _, data := range conditionalData.Data {
				var key, value string

				switch val := data.Value.(type) {
				case *wasm.Static:
					key = val.Static.Key
					value = val.Static.Value
				case *wasm.Expression:
					key = val.ExpressionItem.Key
					value = val.ExpressionItem.Value
				}

				if existingValue, exists := keyValueMap[key]; exists {
					if existingValue != value {
						return nil, fmt.Errorf("duplicate key '%s' with different values found in action", key)
					}
				} else {
					keyValueMap[key] = value
				}
			}
		}
	}

	return result, nil
}

// buildWasmConfigs returns a map of istio gateway locators to an ordered list of corresponding wasm policies
func (r *IstioExtensionReconciler) buildWasmConfigs(ctx context.Context, state *sync.Map) (map[string]wasm.Config, error) {
	logger := controller.LoggerFromContext(ctx).WithName("IstioExtensionReconciler").WithName("buildWasmConfigs")
	logger.Info("build Wasm configuration", "status", "started")
	logger.Info("build Wasm configuration", "status", "completed")

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

	wasmActionSets := kuadrantgatewayapi.GrouppedHTTPRouteMatchConfigs{}
	validatorBuilder := celvalidator.NewRootValidatorBuilder()
	celValidationIssues := celvalidator.NewIssueCollection()

	// build the wasm policies for each topological path that contains an effective rate limit policy affecting an istio gateway
	for i := range paths {
		pathID := paths[i].Key
		path := paths[i].Value

		gatewayClass, gateway, _, _, _, _ := kuadrantpolicymachinery.ObjectsInRequestPath(path)

		// ignore if not an istio gateway
		if !lo.Contains(istioGatewayControllerNames, gatewayClass.Spec.ControllerName) {
			continue
		}

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

		actions, err := mergeAndVerify(actions)
		if err != nil {
			return nil, fmt.Errorf("failed to merge/verify actions for path %s: %w", pathID, err)
		}

		if len(actions) == 0 {
			continue
		}

		validator, err := validatorBuilder.Build()
		if err != nil {
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

		if len(validatedActions) == 0 {
			continue
		}

		wasmActionSetsForPath, err := wasm.BuildActionSetsForPath(pathID, path, validatedActions)
		if err != nil {
			if errors.As(err, &kuadrantpolicymachinery.ErrInvalidPath{}) {
				logger.V(1).Info("ingoring invalid paths", "error", err.Error(), "status", "skipping", "pathID", pathID)
				continue
			}
			logger.Error(err, "failed to build wasm policies for path", "pathID", pathID, "status", "error")
			continue
		}
		wasmActionSets.Add(gateway.GetLocator(), wasmActionSetsForPath...)
	}

	if !celValidationIssues.IsEmpty() {
		state.Store(celvalidator.StateCELValidationErrors, celValidationIssues)
	}

	wasmConfigs := lo.MapValues(wasmActionSets.Sorted(), func(configs kuadrantgatewayapi.SortableHTTPRouteMatchConfigs, _ string) wasm.Config {
		return wasm.BuildConfigForActionSet(lo.Map(configs, func(c kuadrantgatewayapi.HTTPRouteMatchConfig, _ int) wasm.ActionSet {
			return c.Config.(wasm.ActionSet)
		}), &logger)
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
