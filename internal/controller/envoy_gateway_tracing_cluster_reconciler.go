package controllers

import (
	"context"
	"fmt"
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
	kuadrantenvoygateway "github.com/kuadrant/kuadrant-operator/internal/envoygateway"
	"github.com/kuadrant/kuadrant-operator/internal/wasm"
)

//+kubebuilder:rbac:groups=gateway.envoyproxy.io,resources=envoypatchpolicies,verbs=get;list;watch;create;update;patch;delete

// EnvoyGatewayTracingClusterReconciler reconciles Envoy Gateway EnvoyPatchPolicy custom resources for tracing
type EnvoyGatewayTracingClusterReconciler struct {
	client *dynamic.DynamicClient
}

// Subscription subscribes to events with potential impact on the Envoy Gateway EnvoyPatchPolicy custom resources for tracing
func (r *EnvoyGatewayTracingClusterReconciler) Subscription() controller.Subscription {
	return controller.Subscription{
		ReconcileFunc: r.Reconcile,
		Events: []controller.ResourceEventMatcher{
			{Kind: &kuadrantv1beta1.KuadrantGroupKind},
			{Kind: &machinery.GatewayClassGroupKind},
			{Kind: &machinery.GatewayGroupKind},
			{Kind: &kuadrantenvoygateway.EnvoyPatchPolicyGroupKind},
		},
	}
}

func (r *EnvoyGatewayTracingClusterReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("EnvoyGatewayTracingClusterReconciler").WithValues("context", ctx)

	logger.V(1).Info("building envoy gateway tracing clusters")
	defer logger.V(1).Info("finished building envoy gateway tracing clusters")

	kuadrant := GetKuadrantFromTopology(topology)
	if kuadrant == nil {
		logger.V(1).Info("kuadrant CR not found")
		return nil
	}

	var gateways []*machinery.Gateway

	// Only build tracing clusters if tracing is configured
	if kuadrant.Spec.Observability.Tracing != nil && kuadrant.Spec.Observability.Tracing.DefaultEndpoint != "" {
		// Get all envoy gateway gateways
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
				return gateway, lo.Contains(envoyGatewayGatewayControllerNames, gatewayClass.(*machinery.GatewayClass).Spec.ControllerName)
			},
		)
	} else {
		logger.V(1).Info("tracing not configured")
	}

	desiredEnvoyPatchPolicies := make(map[k8stypes.NamespacedName]struct{})
	var modifiedGateways []string

	if len(gateways) == 0 {
		logger.V(1).Info("no envoy gateway gateways found")
	}

	// Reconcile tracing cluster for each gateway
	for _, gateway := range gateways {
		gatewayKey := k8stypes.NamespacedName{Name: gateway.GetName(), Namespace: gateway.GetNamespace()}

		desiredEnvoyPatchPolicy, err := r.buildDesiredEnvoyPatchPolicy(kuadrant, gateway)
		if err != nil {
			logger.Error(err, "failed to build desired envoy patch policy", "gateway", gatewayKey.String())
			continue
		}
		desiredEnvoyPatchPolicies[k8stypes.NamespacedName{Name: desiredEnvoyPatchPolicy.GetName(), Namespace: desiredEnvoyPatchPolicy.GetNamespace()}] = struct{}{}

		resource := r.client.Resource(kuadrantenvoygateway.EnvoyPatchPoliciesResource).Namespace(desiredEnvoyPatchPolicy.GetNamespace())

		existingEnvoyPatchPolicyObj, found := lo.Find(topology.Objects().Children(gateway), func(child machinery.Object) bool {
			return child.GroupVersionKind().GroupKind() == kuadrantenvoygateway.EnvoyPatchPolicyGroupKind &&
				child.GetName() == desiredEnvoyPatchPolicy.GetName() &&
				child.GetNamespace() == desiredEnvoyPatchPolicy.GetNamespace() &&
				labels.Set(child.(*controller.RuntimeObject).GetLabels()).AsSelector().Matches(labels.Set(desiredEnvoyPatchPolicy.GetLabels()))
		})

		// Create
		if !found {
			modifiedGateways = append(modifiedGateways, gateway.GetLocator())
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

		// Update
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

	state.Store(StateEnvoyGatewayTracingClustersModified, modifiedGateways)

	// Cleanup tracing clusters for gateways that no longer need them
	staleEnvoyPatchPolicies := topology.Objects().Items(func(o machinery.Object) bool {
		_, desired := desiredEnvoyPatchPolicies[k8stypes.NamespacedName{Name: o.GetName(), Namespace: o.GetNamespace()}]
		return o.GroupVersionKind().GroupKind() == kuadrantenvoygateway.EnvoyPatchPolicyGroupKind &&
			labels.Set(o.(*controller.RuntimeObject).GetLabels()).AsSelector().Matches(TracingObjectLabels()) &&
			!desired
	})

	for _, envoyPatchPolicy := range staleEnvoyPatchPolicies {
		if err := r.client.Resource(kuadrantenvoygateway.EnvoyPatchPoliciesResource).Namespace(envoyPatchPolicy.GetNamespace()).Delete(ctx, envoyPatchPolicy.GetName(), metav1.DeleteOptions{}); err != nil {
			logger.Error(err, "failed to delete envoypatchpolicy object", "envoypatchpolicy", fmt.Sprintf("%s/%s", envoyPatchPolicy.GetNamespace(), envoyPatchPolicy.GetName()))
			// TODO: handle error
		}
	}

	return nil
}

func (r *EnvoyGatewayTracingClusterReconciler) buildDesiredEnvoyPatchPolicy(kuadrant *kuadrantv1beta1.Kuadrant, gateway *machinery.Gateway) (*envoygatewayv1alpha1.EnvoyPatchPolicy, error) {
	envoyPatchPolicy := &envoygatewayv1alpha1.EnvoyPatchPolicy{
		TypeMeta: metav1.TypeMeta{
			Kind:       kuadrantenvoygateway.EnvoyPatchPolicyGroupKind.Kind,
			APIVersion: envoygatewayv1alpha1.GroupVersion.String(),
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
		Spec: envoygatewayv1alpha1.EnvoyPatchPolicySpec{
			TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReference{
				Group: gatewayapiv1alpha2.Group(machinery.GatewayGroupKind.Group),
				Kind:  gatewayapiv1alpha2.Kind(machinery.GatewayGroupKind.Kind),
				Name:  gatewayapiv1alpha2.ObjectName(gateway.GetName()),
			},
			Type: envoygatewayv1alpha1.JSONPatchEnvoyPatchType,
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

	jsonPatches, err := kuadrantenvoygateway.BuildEnvoyPatchPolicyClusterPatch(
		wasm.TracingServiceName,
		host,
		port,
		mTLS,
		tracingClusterPatch,
	)
	if err != nil {
		return nil, err
	}
	envoyPatchPolicy.Spec.JSONPatches = jsonPatches

	return envoyPatchPolicy, nil
}
