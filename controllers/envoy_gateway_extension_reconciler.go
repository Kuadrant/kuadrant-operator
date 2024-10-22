package controllers

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"

	envoygatewayv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantv1beta3 "github.com/kuadrant/kuadrant-operator/api/v1beta3"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	kuadrantenvoygateway "github.com/kuadrant/kuadrant-operator/pkg/envoygateway"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
	"github.com/kuadrant/kuadrant-operator/pkg/wasm"
)

// envoyGatewayExtensionReconciler reconciles Envoy Gateway EnvoyExtensionPolicy custom resources
type envoyGatewayExtensionReconciler struct {
	client *dynamic.DynamicClient
}

func (r *envoyGatewayExtensionReconciler) Subscription() controller.Subscription {
	return controller.Subscription{
		ReconcileFunc: r.Reconcile,
		Events: []controller.ResourceEventMatcher{ // matches reconciliation events that change the rate limit definitions or status of rate limit policies
			{Kind: &kuadrantv1beta1.KuadrantGroupKind},
			{Kind: &machinery.GatewayClassGroupKind},
			{Kind: &machinery.GatewayGroupKind},
			{Kind: &machinery.HTTPRouteGroupKind},
			{Kind: &kuadrantv1beta3.RateLimitPolicyGroupKind},
			{Kind: &kuadrantenvoygateway.EnvoyExtensionPolicyGroupKind},
		},
	}
}

func (r *envoyGatewayExtensionReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("envoyGatewayExtensionReconciler")

	logger.V(1).Info("building envoy gateway extension")
	defer logger.V(1).Info("finished building envoy gateway extension")

	// build wasm plugin configs for each gateway
	wasmConfigs, err := r.buildWasmConfigs(ctx, state)
	if err != nil {
		if errors.Is(err, ErrMissingStateEffectiveRateLimitPolicies) {
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

		desiredEnvoyExtensionPolicy := buildEnvoyExtensionPolicyForGateway(gateway, wasmConfigs[gateway.GetLocator()])

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
func (r *envoyGatewayExtensionReconciler) buildWasmConfigs(ctx context.Context, state *sync.Map) (map[string]wasm.Config, error) {
	logger := controller.LoggerFromContext(ctx).WithName("envoyGatewayExtensionReconciler").WithName("buildWasmConfigs")

	effectivePolicies, ok := state.Load(StateEffectiveRateLimitPolicies)
	if !ok {
		return nil, ErrMissingStateEffectiveRateLimitPolicies
	}

	logger.V(1).Info("building wasm configs for envoy gateway extension", "effectivePolicies", len(effectivePolicies.(EffectiveRateLimitPolicies)))

	wasmActionSets := kuadrantgatewayapi.GrouppedHTTPRouteMatchConfigs{}

	// build the wasm policies for each topological path that contains an effective rate limit policy affecting an envoy gateway gateway
	for pathID, effectivePolicy := range effectivePolicies.(EffectiveRateLimitPolicies) {
		gatewayClass, gateway, _, _, _, _ := common.ObjectsInRequestPath(effectivePolicy.Path)

		// ignore if not an envoy gateway gateway
		if gatewayClass.Spec.ControllerName != envoyGatewayGatewayControllerName {
			continue
		}

		wasmActionSetsForPath, err := wasm.BuildActionSetsForPath(pathID, effectivePolicy.Path, effectivePolicy.Spec.Rules(), rateLimitWasmActionBuilder(pathID, effectivePolicy, state))
		if err != nil {
			logger.Error(err, "failed to build wasm policies for path", "pathID", pathID)
			continue
		}
		wasmActionSets.Add(gateway.GetLocator(), wasmActionSetsForPath...)
	}

	wasmConfigs := lo.MapValues(wasmActionSets.Sorted(), func(configs kuadrantgatewayapi.SortableHTTPRouteMatchConfigs, _ string) wasm.Config {
		return wasm.BuildConfigForActionSet(lo.Map(configs, func(c kuadrantgatewayapi.HTTPRouteMatchConfig, _ int) wasm.ActionSet {
			return c.Config.(wasm.ActionSet)
		}))
	})

	return wasmConfigs, nil
}

// buildEnvoyExtensionPolicyForGateway builds a desired EnvoyExtensionPolicy custom resource for a given gateway and corresponding wasm config
func buildEnvoyExtensionPolicyForGateway(gateway *machinery.Gateway, wasmConfig wasm.Config) *envoygatewayv1alpha1.EnvoyExtensionPolicy {
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
							URL: WASMFilterImageURL,
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

	return len(aWasms) == len(bWasms) && !lo.EveryBy(aWasms, func(aWasm envoygatewayv1alpha1.Wasm) bool {
		return lo.SomeBy(bWasms, func(bWasm envoygatewayv1alpha1.Wasm) bool {
			if ptr.Deref(aWasm.Name, "") != ptr.Deref(bWasm.Name, "") || ptr.Deref(aWasm.RootID, "") != ptr.Deref(bWasm.RootID, "") || ptr.Deref(aWasm.FailOpen, false) != ptr.Deref(bWasm.FailOpen, false) || aWasm.Code.Type != bWasm.Code.Type || aWasm.Code.Image.URL != bWasm.Code.Image.URL {
				return false
			}
			aWasmConfigJSON, err := wasm.ConfigFromJSON(aWasm.Config)
			if err != nil {
				return false
			}
			bWasmConfigJSON, err := wasm.ConfigFromJSON(bWasm.Config)
			if err != nil {
				return false
			}
			return reflect.DeepEqual(aWasmConfigJSON, bWasmConfigJSON)
		})
	})
}
