package controllers

import (
	"context"
	"errors"
	"fmt"
	"sync"

	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	istioapinetworkingv1alpha3 "istio.io/api/networking/v1alpha3"
	istiov1beta1 "istio.io/api/type/v1beta1"
	istioclientgonetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	kuadrantistio "github.com/kuadrant/kuadrant-operator/pkg/istio"
)

// IstioRateLimitClusterReconciler reconciles Istio EnvoyFilter custom resources for rate limiting
type IstioRateLimitClusterReconciler struct {
	client *dynamic.DynamicClient
}

// IstioRateLimitClusterReconciler subscribes to events with potential impact on the Istio EnvoyFilter custom resources for rate limiting
func (r *IstioRateLimitClusterReconciler) Subscription() controller.Subscription {
	return controller.Subscription{
		ReconcileFunc: r.Reconcile,
		Events: []controller.ResourceEventMatcher{
			{Kind: &kuadrantv1beta1.KuadrantGroupKind},
			{Kind: &machinery.GatewayClassGroupKind},
			{Kind: &machinery.GatewayGroupKind},
			{Kind: &machinery.HTTPRouteGroupKind},
			{Kind: &kuadrantv1.RateLimitPolicyGroupKind},
			{Kind: &kuadrantistio.EnvoyFilterGroupKind},
		},
	}
}

func (r *IstioRateLimitClusterReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("IstioRateLimitClusterReconciler")

	logger.V(1).Info("building istio rate limit clusters")
	defer logger.V(1).Info("finished building istio rate limit clusters")

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
		return gateway, gatewayClass.Spec.ControllerName == istioGatewayControllerName
	}), func(gateway *machinery.Gateway) string {
		return gateway.GetLocator()
	})

	desiredEnvoyFilters := make(map[k8stypes.NamespacedName]struct{})
	var modifiedGateways []string

	// reconcile istio cluster for gateway
	for _, gateway := range gateways {
		gatewayKey := k8stypes.NamespacedName{Name: gateway.GetName(), Namespace: gateway.GetNamespace()}

		desiredEnvoyFilter, err := r.buildDesiredEnvoyFilter(limitador, gateway)
		if err != nil {
			logger.Error(err, "failed to build desired envoy filter")
			continue
		}
		desiredEnvoyFilters[k8stypes.NamespacedName{Name: desiredEnvoyFilter.GetName(), Namespace: desiredEnvoyFilter.GetNamespace()}] = struct{}{}

		resource := r.client.Resource(kuadrantistio.EnvoyFiltersResource).Namespace(desiredEnvoyFilter.GetNamespace())

		existingEnvoyFilterObj, found := lo.Find(topology.Objects().Children(gateway), func(child machinery.Object) bool {
			return child.GroupVersionKind().GroupKind() == kuadrantistio.EnvoyFilterGroupKind && child.GetName() == desiredEnvoyFilter.GetName() && child.GetNamespace() == desiredEnvoyFilter.GetNamespace() && labels.Set(child.(*controller.RuntimeObject).GetLabels()).AsSelector().Matches(labels.Set(desiredEnvoyFilter.GetLabels()))
		})

		// create
		if !found {
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

		existingEnvoyFilter := existingEnvoyFilterObj.(*controller.RuntimeObject).Object.(*istioclientgonetworkingv1alpha3.EnvoyFilter)

		if kuadrantistio.EqualEnvoyFilters(existingEnvoyFilter, desiredEnvoyFilter) {
			logger.V(1).Info("envoyfilter object is up to date, nothing to do")
			continue
		}

		// update
		existingEnvoyFilter.Spec = istioapinetworkingv1alpha3.EnvoyFilter{
			TargetRefs:    desiredEnvoyFilter.Spec.TargetRefs,
			ConfigPatches: desiredEnvoyFilter.Spec.ConfigPatches,
			Priority:      desiredEnvoyFilter.Spec.Priority,
		}

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

	state.Store(StateIstioRateLimitClustersModified, modifiedGateways)

	// cleanup istio clusters for gateways that are not in the effective policies
	staleEnvoyFilters := topology.Objects().Items(func(o machinery.Object) bool {
		_, desired := desiredEnvoyFilters[k8stypes.NamespacedName{Name: o.GetName(), Namespace: o.GetNamespace()}]
		return o.GroupVersionKind().GroupKind() == kuadrantistio.EnvoyFilterGroupKind && labels.Set(o.(*controller.RuntimeObject).GetLabels()).AsSelector().Matches(RateLimitObjectLabels()) && !desired
	})
	for _, envoyFilter := range staleEnvoyFilters {
		if err := r.client.Resource(kuadrantistio.EnvoyFiltersResource).Namespace(envoyFilter.GetNamespace()).Delete(ctx, envoyFilter.GetName(), metav1.DeleteOptions{}); err != nil {
			logger.Error(err, "failed to delete envoyfilter object", "envoyfilter", fmt.Sprintf("%s/%s", envoyFilter.GetNamespace(), envoyFilter.GetName()))
			// TODO: handle error
		}
	}

	return nil
}

func (r *IstioRateLimitClusterReconciler) buildDesiredEnvoyFilter(limitador *limitadorv1alpha1.Limitador, gateway *machinery.Gateway) (*istioclientgonetworkingv1alpha3.EnvoyFilter, error) {
	envoyFilter := &istioclientgonetworkingv1alpha3.EnvoyFilter{
		TypeMeta: metav1.TypeMeta{
			Kind:       kuadrantistio.EnvoyFilterGroupKind.Kind,
			APIVersion: istioclientgonetworkingv1alpha3.SchemeGroupVersion.String(),
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

	limitadorService := limitador.Status.Service
	if limitadorService == nil {
		return nil, ErrMissingLimitadorServiceInfo
	}
	configPatches, err := kuadrantistio.BuildEnvoyFilterClusterPatch(limitador.Status.Service.Host, int(limitador.Status.Service.Ports.GRPC), rateLimitClusterPatch)
	if err != nil {
		return nil, err
	}
	envoyFilter.Spec.ConfigPatches = configPatches

	return envoyFilter, nil
}
