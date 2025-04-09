package controllers

import (
	"context"
	"fmt"
	"sync"

	authorinooperatorv1beta1 "github.com/kuadrant/authorino-operator/api/v1beta1"
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
	kuadrantistio "github.com/kuadrant/kuadrant-operator/internal/istio"
	kuadrantpolicymachinery "github.com/kuadrant/kuadrant-operator/internal/policymachinery"
)

//+kubebuilder:rbac:groups=networking.istio.io,resources=envoyfilters,verbs=get;list;watch;create;update;patch;delete

// IstioAuthClusterReconciler reconciles Istio EnvoyFilter custom resources for auth
type IstioAuthClusterReconciler struct {
	client *dynamic.DynamicClient
}

// IstioAuthClusterReconciler subscribes to events with potential impact on the Istio EnvoyFilter custom resources for auth
func (r *IstioAuthClusterReconciler) Subscription() controller.Subscription {
	return controller.Subscription{
		ReconcileFunc: r.Reconcile,
		Events: []controller.ResourceEventMatcher{
			{Kind: &kuadrantv1beta1.KuadrantGroupKind},
			{Kind: &machinery.GatewayClassGroupKind},
			{Kind: &machinery.GatewayGroupKind},
			{Kind: &machinery.HTTPRouteGroupKind},
			{Kind: &kuadrantv1.AuthPolicyGroupKind},
			{Kind: &kuadrantistio.EnvoyFilterGroupKind},
		},
	}
}

func (r *IstioAuthClusterReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("IstioAuthClusterReconciler")

	logger.V(1).Info("building istio auth clusters")
	defer logger.V(1).Info("finished building istio auth clusters")

	kuadrant := GetKuadrantFromTopology(topology)
	if kuadrant == nil {
		return nil
	}

	authorinoObj, found := lo.Find(topology.Objects().Children(kuadrant), func(child machinery.Object) bool {
		return child.GroupVersionKind().GroupKind() == kuadrantv1beta1.AuthorinoGroupKind
	})
	if !found {
		logger.V(1).Info(ErrMissingAuthorino.Error())
		return nil
	}
	authorino := authorinoObj.(*controller.RuntimeObject).Object.(*authorinooperatorv1beta1.Authorino)

	effectivePolicies, ok := state.Load(StateEffectiveAuthPolicies)
	if !ok {
		logger.Error(ErrMissingStateEffectiveAuthPolicies, "failed to get effective auth policies from state")
		return nil
	}

	gateways := lo.UniqBy(lo.FilterMap(lo.Values(effectivePolicies.(EffectiveAuthPolicies)), func(effectivePolicy EffectiveAuthPolicy, _ int) (*machinery.Gateway, bool) {
		gatewayClass, gateway, _, _, _, _ := kuadrantpolicymachinery.ObjectsInRequestPath(effectivePolicy.Path)
		return gateway, lo.Contains(istioGatewayControllerNames, gatewayClass.Spec.ControllerName)
	}), func(gateway *machinery.Gateway) string {
		return gateway.GetLocator()
	})

	desiredEnvoyFilters := make(map[k8stypes.NamespacedName]struct{})
	var modifiedGateways []string

	// reconcile istio cluster for gateway
	for _, gateway := range gateways {
		gatewayKey := k8stypes.NamespacedName{Name: gateway.GetName(), Namespace: gateway.GetNamespace()}

		desiredEnvoyFilter, err := r.buildDesiredEnvoyFilter(authorino, gateway, kuadrant.IsMTLSEnabled())
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

	state.Store(StateIstioAuthClustersModified, modifiedGateways)

	// cleanup istio clusters for gateways that are not in the effective policies
	staleEnvoyFilters := topology.Objects().Items(func(o machinery.Object) bool {
		_, desired := desiredEnvoyFilters[k8stypes.NamespacedName{Name: o.GetName(), Namespace: o.GetNamespace()}]
		return o.GroupVersionKind().GroupKind() == kuadrantistio.EnvoyFilterGroupKind && labels.Set(o.(*controller.RuntimeObject).GetLabels()).AsSelector().Matches(AuthObjectLabels()) && !desired
	})
	for _, envoyFilter := range staleEnvoyFilters {
		if err := r.client.Resource(kuadrantistio.EnvoyFiltersResource).Namespace(envoyFilter.GetNamespace()).Delete(ctx, envoyFilter.GetName(), metav1.DeleteOptions{}); err != nil {
			logger.Error(err, "failed to delete envoyfilter object", "envoyfilter", fmt.Sprintf("%s/%s", envoyFilter.GetNamespace(), envoyFilter.GetName()))
			// TODO: handle error
		}
	}

	return nil
}

func (r *IstioAuthClusterReconciler) buildDesiredEnvoyFilter(authorino *authorinooperatorv1beta1.Authorino, gateway *machinery.Gateway, mtls bool) (*istioclientgonetworkingv1alpha3.EnvoyFilter, error) {
	envoyFilter := &istioclientgonetworkingv1alpha3.EnvoyFilter{
		TypeMeta: metav1.TypeMeta{
			Kind:       kuadrantistio.EnvoyFilterGroupKind.Kind,
			APIVersion: istioclientgonetworkingv1alpha3.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      AuthClusterName(gateway.GetName()),
			Namespace: gateway.GetNamespace(),
			Labels:    AuthObjectLabels(),
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

	authorinoServiceInfo := authorinoServiceInfoFromAuthorino(authorino)
	configPatches, err := kuadrantistio.BuildEnvoyFilterClusterPatch(authorinoServiceInfo.Host, int(authorinoServiceInfo.Port), mtls, authClusterPatch)
	if err != nil {
		return nil, err
	}
	envoyFilter.Spec.ConfigPatches = configPatches

	return envoyFilter, nil
}
