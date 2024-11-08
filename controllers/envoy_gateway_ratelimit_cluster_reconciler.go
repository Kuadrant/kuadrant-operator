package controllers

import (
	"context"
	"errors"
	"fmt"
	"sync"

	envoygatewayv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	kuadrantenvoygateway "github.com/kuadrant/kuadrant-operator/pkg/envoygateway"
)

// EnvoyGatewayRateLimitClusterReconciler reconciles Envoy Gateway EnvoyPatchPolicy custom resources for rate limiting
type EnvoyGatewayRateLimitClusterReconciler struct {
	client *dynamic.DynamicClient
}

// EnvoyGatewayRateLimitClusterReconciler subscribes to events with potential impact on the Envoy Gateway EnvoyPatchPolicy custom resources for rate limiting
func (r *EnvoyGatewayRateLimitClusterReconciler) Subscription() controller.Subscription {
	return controller.Subscription{
		ReconcileFunc: r.Reconcile,
		Events: []controller.ResourceEventMatcher{
			{Kind: &kuadrantv1beta1.KuadrantGroupKind},
			{Kind: &machinery.GatewayClassGroupKind},
			{Kind: &machinery.GatewayGroupKind},
			{Kind: &machinery.HTTPRouteGroupKind},
			{Kind: &kuadrantv1.RateLimitPolicyGroupKind},
			{Kind: &kuadrantenvoygateway.EnvoyPatchPolicyGroupKind},
		},
	}
}

func (r *EnvoyGatewayRateLimitClusterReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("EnvoyGatewayRateLimitClusterReconciler")

	logger.V(1).Info("building envoy gateway rate limit clusters")
	defer logger.V(1).Info("finished building envoy gateway rate limit clusters")

	kuadrant, err := GetKuadrantFromTopology(topology)
	if err != nil {
		if errors.Is(err, ErrMissingKuadrant) {
			logger.V(1).Info(err.Error())
			return nil
		}
		return err
	}

	limitadorObj, found := lo.Find(topology.Objects().Children(kuadrant), func(child machinery.Object) bool {
		return child.GroupVersionKind().GroupKind() == kuadrantv1beta1.LimitadorGroupKind
	})
	if !found {
		logger.V(1).Info(ErrMissingLimitador.Error())
		return nil
	}
	limitador := limitadorObj.(*controller.RuntimeObject).Object.(*limitadorv1alpha1.Limitador)

	effectivePolicies, ok := state.Load(StateEffectiveRateLimitPolicies)
	if !ok {
		logger.Error(ErrMissingStateEffectiveRateLimitPolicies, "failed to get effective rate limit policies from state")
		return nil
	}

	gateways := lo.UniqBy(lo.FilterMap(lo.Values(effectivePolicies.(EffectiveRateLimitPolicies)), func(effectivePolicy EffectiveRateLimitPolicy, _ int) (*machinery.Gateway, bool) {
		gatewayClass, gateway, _, _, _, _ := common.ObjectsInRequestPath(effectivePolicy.Path)
		return gateway, gatewayClass.Spec.ControllerName == envoyGatewayGatewayControllerName
	}), func(gateway *machinery.Gateway) string {
		return gateway.GetLocator()
	})

	desiredEnvoyPatchPolicies := make(map[k8stypes.NamespacedName]struct{})
	var modifiedGateways []string

	// reconcile envoy gateway cluster for gateway
	for _, gateway := range gateways {
		gatewayKey := k8stypes.NamespacedName{Name: gateway.GetName(), Namespace: gateway.GetNamespace()}

		desiredEnvoyPatchPolicy, err := r.buildDesiredEnvoyPatchPolicy(limitador, gateway)
		if err != nil {
			logger.Error(err, "failed to build desired envoy patch policy")
			continue
		}
		desiredEnvoyPatchPolicies[k8stypes.NamespacedName{Name: desiredEnvoyPatchPolicy.GetName(), Namespace: desiredEnvoyPatchPolicy.GetNamespace()}] = struct{}{}

		resource := r.client.Resource(kuadrantenvoygateway.EnvoyPatchPoliciesResource).Namespace(desiredEnvoyPatchPolicy.GetNamespace())

		existingEnvoyPatchPolicyObj, found := lo.Find(topology.Objects().Children(gateway), func(child machinery.Object) bool {
			return child.GroupVersionKind().GroupKind() == kuadrantenvoygateway.EnvoyPatchPolicyGroupKind && child.GetName() == desiredEnvoyPatchPolicy.GetName() && child.GetNamespace() == desiredEnvoyPatchPolicy.GetNamespace() && labels.Set(child.(*controller.RuntimeObject).GetLabels()).AsSelector().Matches(labels.Set(desiredEnvoyPatchPolicy.GetLabels()))
		})

		// create
		if !found {
			modifiedGateways = append(modifiedGateways, gateway.GetLocator()) // we only signal the gateway as modified when an envoypatchpolicy is created, because updates won't change the status
			desiredEnvoyPatchPolicyUnstructured, err := controller.Destruct(desiredEnvoyPatchPolicy)
			if err != nil {
				logger.Error(err, "failed to destruct envoypatchpolicy object", "gateway", gatewayKey.String(), "envoypatchpolicy", desiredEnvoyPatchPolicy)
				continue
			}
			if _, err = resource.Create(ctx, desiredEnvoyPatchPolicyUnstructured, metav1.CreateOptions{}); err != nil {
				logger.Error(err, "failed to create envoypatchpolicy object", "gateway", gatewayKey.String(), "envoypatchpolicy", desiredEnvoyPatchPolicyUnstructured.Object)
				// TODO: handle error
			}
			continue
		}

		existingEnvoyPatchPolicy := existingEnvoyPatchPolicyObj.(*controller.RuntimeObject).Object.(*envoygatewayv1alpha1.EnvoyPatchPolicy)

		if kuadrantenvoygateway.EqualEnvoyPatchPolicies(existingEnvoyPatchPolicy, desiredEnvoyPatchPolicy) {
			logger.V(1).Info("envoypatchpolicy object is up to date, nothing to do")
			continue
		}

		// update
		existingEnvoyPatchPolicy.Spec = envoygatewayv1alpha1.EnvoyPatchPolicySpec{
			TargetRef:   desiredEnvoyPatchPolicy.Spec.TargetRef,
			Type:        desiredEnvoyPatchPolicy.Spec.Type,
			JSONPatches: desiredEnvoyPatchPolicy.Spec.JSONPatches,
			Priority:    desiredEnvoyPatchPolicy.Spec.Priority,
		}

		existingEnvoyPatchPolicyUnstructured, err := controller.Destruct(existingEnvoyPatchPolicy)
		if err != nil {
			logger.Error(err, "failed to destruct envoypatchpolicy object", "gateway", gatewayKey.String(), "envoypatchpolicy", existingEnvoyPatchPolicy)
			continue
		}
		if _, err = resource.Update(ctx, existingEnvoyPatchPolicyUnstructured, metav1.UpdateOptions{}); err != nil {
			logger.Error(err, "failed to update envoypatchpolicy object", "gateway", gatewayKey.String(), "envoypatchpolicy", existingEnvoyPatchPolicyUnstructured.Object)
			// TODO: handle error
		}
	}

	state.Store(StateEnvoyGatewayRateLimitClustersModified, modifiedGateways)

	// cleanup envoy gateway clusters for gateways that are not in the effective policies
	staleEnvoyPatchPolicies := topology.Objects().Items(func(o machinery.Object) bool {
		_, desired := desiredEnvoyPatchPolicies[k8stypes.NamespacedName{Name: o.GetName(), Namespace: o.GetNamespace()}]
		return o.GroupVersionKind().GroupKind() == kuadrantenvoygateway.EnvoyPatchPolicyGroupKind && labels.Set(o.(*controller.RuntimeObject).GetLabels()).AsSelector().Matches(RateLimitObjectLabels()) && !desired
	})

	for _, envoyPatchPolicy := range staleEnvoyPatchPolicies {
		if err := r.client.Resource(kuadrantenvoygateway.EnvoyPatchPoliciesResource).Namespace(envoyPatchPolicy.GetNamespace()).Delete(ctx, envoyPatchPolicy.GetName(), metav1.DeleteOptions{}); err != nil {
			logger.Error(err, "failed to delete envoypatchpolicy object", "envoypatchpolicy", fmt.Sprintf("%s/%s", envoyPatchPolicy.GetNamespace(), envoyPatchPolicy.GetName()))
			// TODO: handle error
		}
	}

	return nil
}

func (r *EnvoyGatewayRateLimitClusterReconciler) buildDesiredEnvoyPatchPolicy(limitador *limitadorv1alpha1.Limitador, gateway *machinery.Gateway) (*envoygatewayv1alpha1.EnvoyPatchPolicy, error) {
	envoyPatchPolicy := &envoygatewayv1alpha1.EnvoyPatchPolicy{
		TypeMeta: metav1.TypeMeta{
			Kind:       kuadrantenvoygateway.EnvoyPatchPolicyGroupKind.Kind,
			APIVersion: envoygatewayv1alpha1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      RateLimitClusterName(gateway.GetName()),
			Namespace: gateway.GetNamespace(),
			Labels:    RateLimitObjectLabels(),
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
		Spec: envoygatewayv1alpha1.EnvoyPatchPolicySpec{
			TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReference{
				Group: gatewayapiv1alpha2.Group(machinery.GatewayGroupKind.Group),
				Kind:  gatewayapiv1alpha2.Kind(machinery.GatewayGroupKind.Kind),
				Name:  gatewayapiv1alpha2.ObjectName(gateway.GetName()),
			},
			Type: envoygatewayv1alpha1.JSONPatchEnvoyPatchType,
		},
	}

	jsonPatches, err := kuadrantenvoygateway.BuildEnvoyPatchPolicyClusterPatch(common.KuadrantRateLimitClusterName, limitador.Status.Service.Host, int(limitador.Status.Service.Ports.GRPC), rateLimitClusterPatch)
	if err != nil {
		return nil, err
	}
	envoyPatchPolicy.Spec.JSONPatches = jsonPatches

	return envoyPatchPolicy, nil
}
