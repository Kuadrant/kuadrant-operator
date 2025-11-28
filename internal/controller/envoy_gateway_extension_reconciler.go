package controllers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	envoygatewayv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"github.com/google/cel-go/cel"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"
	v1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwapiv1b1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	celvalidator "github.com/kuadrant/kuadrant-operator/internal/cel"
	kuadrantenvoygateway "github.com/kuadrant/kuadrant-operator/internal/envoygateway"
	"github.com/kuadrant/kuadrant-operator/internal/extension"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/internal/gatewayapi"
	kuadrantpolicymachinery "github.com/kuadrant/kuadrant-operator/internal/policymachinery"
	"github.com/kuadrant/kuadrant-operator/internal/utils"
	"github.com/kuadrant/kuadrant-operator/internal/wasm"
)

//+kubebuilder:rbac:groups=gateway.envoyproxy.io,resources=envoyextensionpolicies,verbs=get;list;watch;create;update;patch;delete

// EnvoyGatewayExtensionReconciler reconciles Envoy Gateway EnvoyExtensionPolicy custom resources
type EnvoyGatewayExtensionReconciler struct {
	client *dynamic.DynamicClient
}

// EnvoyGatewayExtensionReconciler subscribes to events with potential impact on the Envoy Gateway EnvoyExtensionPolicy custom resources
func (r *EnvoyGatewayExtensionReconciler) Subscription() controller.Subscription {
	return controller.Subscription{
		ReconcileFunc: r.Reconcile,
		Events: []controller.ResourceEventMatcher{
			{Kind: &kuadrantv1beta1.KuadrantGroupKind},
			{Kind: &machinery.GatewayClassGroupKind},
			{Kind: &machinery.GatewayGroupKind},
			{Kind: &machinery.HTTPRouteGroupKind},
			{Kind: &kuadrantv1.AuthPolicyGroupKind},
			{Kind: &kuadrantv1.RateLimitPolicyGroupKind},
			{Kind: &kuadrantv1alpha1.TokenRateLimitPolicyGroupKind},
			{Kind: &kuadrantenvoygateway.EnvoyExtensionPolicyGroupKind},
		},
	}
}

func (r *EnvoyGatewayExtensionReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("EnvoyGatewayExtensionReconciler").WithValues("context", ctx)

	logger.V(1).Info("building envoy gateway extension", "image url", WASMFilterImageURL)
	defer logger.V(1).Info("finished building envoy gateway extension")

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

		desiredEnvoyExtensionPolicy := buildEnvoyExtensionPolicyForGateway(gateway, wasmConfig, ProtectedRegistry, WASMFilterImageURL)

		resource := r.client.Resource(kuadrantenvoygateway.EnvoyExtensionPoliciesResource).Namespace(desiredEnvoyExtensionPolicy.GetNamespace())

		existingEnvoyExtensionPolicyObj, found := lo.Find(topology.Objects().Children(gateway), func(child machinery.Object) bool {
			return child.GroupVersionKind().GroupKind() == kuadrantenvoygateway.EnvoyExtensionPolicyGroupKind && child.GetName() == desiredEnvoyExtensionPolicy.GetName() && child.GetNamespace() == desiredEnvoyExtensionPolicy.GetNamespace() && labels.Set(child.(*controller.RuntimeObject).GetLabels()).AsSelector().Matches(labels.Set(desiredEnvoyExtensionPolicy.GetLabels()))
		})

		// create
		if !found {
			if utils.IsObjectTaggedToDelete(desiredEnvoyExtensionPolicy) {
				continue
			}
			modifiedGateways = append(modifiedGateways, gateway.GetLocator()) // we only signal the gateway as modified when an envoyextensionpolicy is created, because updates won't change the status
			desiredEnvoyExtensionPolicyUnstructured, err := controller.Destruct(desiredEnvoyExtensionPolicy)
			if err != nil {
				logger.Error(err, "failed to destruct envoyextensionpolicy object", "gateway", gatewayKey.String(), "envoyextensionpolicy", desiredEnvoyExtensionPolicy)
				continue
			}
			if _, err = resource.Create(ctx, desiredEnvoyExtensionPolicyUnstructured, metav1.CreateOptions{}); err != nil {
				logger.Error(err, "failed to create envoyextensionpolicy object", "gateway", gatewayKey.String(), "envoyextensionpolicy", desiredEnvoyExtensionPolicyUnstructured.Object)
				// TODO: handle error
			}
			continue
		}

		existingEnvoyExtensionPolicy := existingEnvoyExtensionPolicyObj.(*controller.RuntimeObject).Object.(*envoygatewayv1alpha1.EnvoyExtensionPolicy)

		// delete
		if utils.IsObjectTaggedToDelete(desiredEnvoyExtensionPolicy) && !utils.IsObjectTaggedToDelete(existingEnvoyExtensionPolicy) {
			if err := resource.Delete(ctx, existingEnvoyExtensionPolicy.GetName(), metav1.DeleteOptions{}); err != nil {
				logger.Error(err, "failed to delete envoyextensionpolicy object", "gateway", gatewayKey.String(), "envoyextensionpolicy", fmt.Sprintf("%s/%s", existingEnvoyExtensionPolicy.GetNamespace(), existingEnvoyExtensionPolicy.GetName()))
				// TODO: handle error
			}
			continue
		}

		if equalEnvoyExtensionPolicies(existingEnvoyExtensionPolicy, desiredEnvoyExtensionPolicy) {
			logger.V(1).Info("envoyextensionpolicy object is up to date, nothing to do")
			continue
		}

		// update
		existingEnvoyExtensionPolicy.Spec.TargetRefs = desiredEnvoyExtensionPolicy.Spec.TargetRefs
		existingEnvoyExtensionPolicy.Spec.Wasm = desiredEnvoyExtensionPolicy.Spec.Wasm

		existingEnvoyExtensionPolicyUnstructured, err := controller.Destruct(existingEnvoyExtensionPolicy)
		if err != nil {
			logger.Error(err, "failed to destruct envoyextensionpolicy object", "gateway", gatewayKey.String(), "envoyextensionpolicy", existingEnvoyExtensionPolicy)
			continue
		}
		if _, err = resource.Update(ctx, existingEnvoyExtensionPolicyUnstructured, metav1.UpdateOptions{}); err != nil {
			logger.Error(err, "failed to update envoyextensionpolicy object", "gateway", gatewayKey.String(), "envoyextensionpolicy", existingEnvoyExtensionPolicyUnstructured.Object)
			// TODO: handle error
		}
	}

	state.Store(StateEnvoyGatewayExtensionsModified, modifiedGateways)

	return nil
}

// buildWasmConfigs returns a map of envoy gateway gateway locators to an ordered list of corresponding wasm policies
func (r *EnvoyGatewayExtensionReconciler) buildWasmConfigs(ctx context.Context, topology *machinery.Topology, state *sync.Map) (map[string]wasm.Config, error) {
	logger := controller.LoggerFromContext(ctx).WithName("EnvoyGatewayExtensionReconciler").WithName("buildWasmConfigs").WithValues("context", ctx)

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

	logger.V(1).Info("building wasm configs for envoy gateway extension", "effectiveAuthPolicies", len(effectiveAuthPoliciesMap), "effectiveRateLimitPolicies", len(effectiveRateLimitPoliciesMap), "effectiveTokenRateLimitPolicies", len(effectiveTokenRateLimitPoliciesMap))

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
	celValidationIssues := celvalidator.NewIssueCollection()

	// build the wasm policies for each topological path that contains an effective rate limit policy affecting an envoy gateway gateway
	for i := range paths {
		pathID := paths[i].Key
		path := paths[i].Value

		gatewayClass, gateway, _, _, _, _ := kuadrantpolicymachinery.ObjectsInRequestPath(path)

		validatorBuilder := celvalidator.NewRootValidatorBuilder()

		// ignore if not an envoy gateway gateway
		if !lo.Contains(envoyGatewayGatewayControllerNames, gatewayClass.Spec.ControllerName) {
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
			logger.Error(err, "failed to build wasm policies for path", "pathID", pathID)
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
		}), &logger, observability, serviceBuilder)
	})

	return wasmConfigs, nil
}

// buildEnvoyExtensionPolicyForGateway builds a desired EnvoyExtensionPolicy custom resource for a given gateway and corresponding wasm config
func buildEnvoyExtensionPolicyForGateway(gateway *machinery.Gateway, wasmConfig wasm.Config, protectedRegistry, imageURL string) *envoygatewayv1alpha1.EnvoyExtensionPolicy {
	envoyPolicy := &envoygatewayv1alpha1.EnvoyExtensionPolicy{
		TypeMeta: metav1.TypeMeta{
			Kind:       kuadrantenvoygateway.EnvoyExtensionPolicyGroupKind.Kind,
			APIVersion: envoygatewayv1alpha1.GroupVersion.String(),
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
		Spec: envoygatewayv1alpha1.EnvoyExtensionPolicySpec{
			PolicyTargetReferences: envoygatewayv1alpha1.PolicyTargetReferences{
				TargetRefs: []gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
					{
						LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
							Group: gatewayapiv1alpha2.Group(machinery.GatewayGroupKind.Group),
							Kind:  gatewayapiv1alpha2.Kind(machinery.GatewayGroupKind.Kind),
							Name:  gatewayapiv1alpha2.ObjectName(gateway.GetName()),
						},
					},
				},
			},
			Wasm: []envoygatewayv1alpha1.Wasm{
				{
					Name:   ptr.To("kuadrant-wasm-shim"),
					RootID: ptr.To("kuadrant_wasm_shim"),
					Code: envoygatewayv1alpha1.WasmCodeSource{
						Type: envoygatewayv1alpha1.ImageWasmCodeSourceType,
						Image: &envoygatewayv1alpha1.ImageWasmCodeSource{
							URL: imageURL,
						},
					},
					Config: nil,
					// When a fatal error accurs during the initialization or the execution of the
					// Wasm extension, if FailOpen is set to false the system blocks the traffic and returns
					// an HTTP 5xx error.
					FailOpen: ptr.To(false),
				},
			},
		},
	}
	for _, wasm := range envoyPolicy.Spec.Wasm {
		if wasm.Code.Image.PullSecretRef != nil {
			//reset it to empty this will remove it if the image is now public registry
			wasm.Code.Image.PullSecretRef = nil
		}
		// if we are in a protected registry set the object
		if protectedRegistry != "" && strings.Contains(imageURL, protectedRegistry) {
			wasm.Code.Image.PullSecretRef = &gwapiv1b1.SecretObjectReference{Name: v1.ObjectName(RegistryPullSecretName)}
		}
	}

	if len(wasmConfig.ActionSets) == 0 {
		utils.TagObjectToDelete(envoyPolicy)
	} else {
		pluginConfigJSON, err := wasmConfig.ToJSON()
		if err != nil {
			return nil
		}
		envoyPolicy.Spec.Wasm[0].Config = pluginConfigJSON
	}

	return envoyPolicy
}

func equalEnvoyExtensionPolicies(a, b *envoygatewayv1alpha1.EnvoyExtensionPolicy) bool {
	if !kuadrantgatewayapi.EqualLocalPolicyTargetReferencesWithSectionName(a.Spec.TargetRefs, b.Spec.TargetRefs) {
		return false
	}

	aWasms := a.Spec.Wasm
	bWasms := b.Spec.Wasm

	return len(aWasms) == len(bWasms) && lo.EveryBy(aWasms, func(aWasm envoygatewayv1alpha1.Wasm) bool {
		return lo.SomeBy(bWasms, func(bWasm envoygatewayv1alpha1.Wasm) bool {
			if ptr.Deref(aWasm.Name, "") != ptr.Deref(bWasm.Name, "") || ptr.Deref(aWasm.RootID, "") != ptr.Deref(bWasm.RootID, "") || ptr.Deref(aWasm.FailOpen, false) != ptr.Deref(bWasm.FailOpen, false) || aWasm.Code.Type != bWasm.Code.Type || aWasm.Code.Image.URL != bWasm.Code.Image.URL || ptr.Deref(aWasm.Code.Image.PullSecretRef, gwapiv1b1.SecretObjectReference{}) != ptr.Deref(bWasm.Code.Image.PullSecretRef, gwapiv1b1.SecretObjectReference{}) {
				return false
			}
			aConfig, err := wasm.ConfigFromJSON(aWasm.Config)
			if err != nil {
				return false
			}
			bConfig, err := wasm.ConfigFromJSON(bWasm.Config)
			if err != nil {
				return false
			}
			return aConfig != nil && bConfig != nil && aConfig.EqualTo(bConfig)
		})
	})
}
