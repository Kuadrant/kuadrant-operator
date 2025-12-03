package controllers

import (
	"context"
	"fmt"
	"sync"

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

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantistio "github.com/kuadrant/kuadrant-operator/internal/istio"
)

//+kubebuilder:rbac:groups=networking.istio.io,resources=envoyfilters,verbs=get;list;watch;create;update;patch;delete

// IstioTracingClusterReconciler reconciles Istio EnvoyFilter custom resources for tracing
type IstioTracingClusterReconciler struct {
	client *dynamic.DynamicClient
}

// Subscription subscribes to events with potential impact on the Istio EnvoyFilter custom resources for tracing
func (r *IstioTracingClusterReconciler) Subscription() controller.Subscription {
	return controller.Subscription{
		ReconcileFunc: r.Reconcile,
		Events: []controller.ResourceEventMatcher{
			{Kind: &kuadrantv1beta1.KuadrantGroupKind},
			{Kind: &machinery.GatewayClassGroupKind},
			{Kind: &machinery.GatewayGroupKind},
			{Kind: &kuadrantistio.EnvoyFilterGroupKind},
		},
	}
}

func (r *IstioTracingClusterReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("IstioTracingClusterReconciler").WithValues("context", ctx)

	logger.V(1).Info("building istio tracing clusters")
	defer logger.V(1).Info("finished building istio tracing clusters")

	kuadrant := GetKuadrantFromTopology(topology)
	if kuadrant == nil {
		logger.V(1).Info("kuadrant CR not found")
		return nil
	}

	var gateways []*machinery.Gateway

	// Only build tracing clusters if tracing is configured
	if kuadrant.Spec.Observability.Tracing != nil && kuadrant.Spec.Observability.Tracing.DefaultEndpoint != "" {
		// Get all istio gateways
		gateways = lo.FilterMap(
			topology.Targetables().Items(func(o machinery.Object) bool {
				return o.GroupVersionKind().GroupKind() == machinery.GatewayGroupKind
			}),
			func(t machinery.Targetable, _ int) (*machinery.Gateway, bool) {
				gateway := t.(*machinery.Gateway)
				gatewayClass, found := lo.Find(topology.Targetables().Parents(gateway), func(t machinery.Targetable) bool {
					return t.GroupVersionKind().GroupKind() == machinery.GatewayClassGroupKind
				})
				if !found {
					return nil, false
				}
				return gateway, lo.Contains(istioGatewayControllerNames, gatewayClass.(*machinery.GatewayClass).Spec.ControllerName)
			},
		)
	} else {
		logger.V(1).Info("tracing not configured")
	}

	desiredEnvoyFilters := make(map[k8stypes.NamespacedName]struct{})
	var modifiedGateways []string

	if len(gateways) == 0 {
		logger.V(1).Info("no istio gateways found")
	}

	// Reconcile tracing cluster for each gateway
	for _, gateway := range gateways {
		gatewayKey := k8stypes.NamespacedName{Name: gateway.GetName(), Namespace: gateway.GetNamespace()}

		desiredEnvoyFilter, err := r.buildDesiredEnvoyFilter(kuadrant, gateway)
		if err != nil {
			logger.Error(err, "failed to build desired envoy filter", "gateway", gatewayKey.String())
			continue
		}
		desiredEnvoyFilters[k8stypes.NamespacedName{Name: desiredEnvoyFilter.GetName(), Namespace: desiredEnvoyFilter.GetNamespace()}] = struct{}{}

		resource := r.client.Resource(kuadrantistio.EnvoyFiltersResource).Namespace(desiredEnvoyFilter.GetNamespace())

		existingEnvoyFilterObj, found := lo.Find(topology.Objects().Children(gateway), func(child machinery.Object) bool {
			return child.GroupVersionKind().GroupKind() == kuadrantistio.EnvoyFilterGroupKind &&
				child.GetName() == desiredEnvoyFilter.GetName() &&
				child.GetNamespace() == desiredEnvoyFilter.GetNamespace() &&
				labels.Set(child.(*controller.RuntimeObject).GetLabels()).AsSelector().Matches(labels.Set(desiredEnvoyFilter.GetLabels()))
		})

		// Create
		if !found {
			modifiedGateways = append(modifiedGateways, gateway.GetLocator())
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

		// Update
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

	state.Store(StateIstioTracingClustersModified, modifiedGateways)

	// Cleanup tracing clusters for gateways that no longer need them
	staleEnvoyFilters := topology.Objects().Items(func(o machinery.Object) bool {
		_, desired := desiredEnvoyFilters[k8stypes.NamespacedName{Name: o.GetName(), Namespace: o.GetNamespace()}]
		return o.GroupVersionKind().GroupKind() == kuadrantistio.EnvoyFilterGroupKind &&
			labels.Set(o.(*controller.RuntimeObject).GetLabels()).AsSelector().Matches(TracingObjectLabels()) &&
			!desired
	})

	for _, envoyFilter := range staleEnvoyFilters {
		if err := r.client.Resource(kuadrantistio.EnvoyFiltersResource).Namespace(envoyFilter.GetNamespace()).Delete(ctx, envoyFilter.GetName(), metav1.DeleteOptions{}); err != nil {
			logger.Error(err, "failed to delete envoyfilter object", "envoyfilter", fmt.Sprintf("%s/%s", envoyFilter.GetNamespace(), envoyFilter.GetName()))
			// TODO: handle error
		}
	}

	return nil
}

func (r *IstioTracingClusterReconciler) buildDesiredEnvoyFilter(kuadrant *kuadrantv1beta1.Kuadrant, gateway *machinery.Gateway) (*istioclientgonetworkingv1alpha3.EnvoyFilter, error) {
	envoyFilter := &istioclientgonetworkingv1alpha3.EnvoyFilter{
		TypeMeta: metav1.TypeMeta{
			Kind:       kuadrantistio.EnvoyFilterGroupKind.Kind,
			APIVersion: istioclientgonetworkingv1alpha3.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      TracingClusterName(gateway.GetName()),
			Namespace: gateway.GetNamespace(),
			Labels:    TracingObjectLabels(),
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

	tracingEndpoint := kuadrant.Spec.Observability.Tracing.DefaultEndpoint

	// Parse the tracing endpoint to extract host and port
	host, port, err := parseTracingEndpoint(tracingEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to parse tracing endpoint: %w", err)
	}

	// Use mTLS unless explicitly set to insecure
	mTLS := !kuadrant.Spec.Observability.Tracing.Insecure

	configPatches, err := kuadrantistio.BuildEnvoyFilterClusterPatch(host, port, mTLS, tracingClusterPatch)
	if err != nil {
		return nil, err
	}
	envoyFilter.Spec.ConfigPatches = configPatches

	return envoyFilter, nil
}
