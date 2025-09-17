package controllers

import (
	"context"
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
	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantenvoygateway "github.com/kuadrant/kuadrant-operator/internal/envoygateway"
	kuadrantpolicymachinery "github.com/kuadrant/kuadrant-operator/internal/policymachinery"
)

//+kubebuilder:rbac:groups=gateway.envoyproxy.io,resources=envoypatchpolicies,verbs=get;list;watch;create;update;patch;delete

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
			{Kind: &kuadrantv1alpha1.TokenRateLimitPolicyGroupKind},
			{Kind: &kuadrantenvoygateway.EnvoyPatchPolicyGroupKind},
		},
	}
}

func (r *EnvoyGatewayRateLimitClusterReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("EnvoyGatewayRateLimitClusterReconciler")

	logger.V(1).Info("building envoy gateway rate limit clusters")
	defer logger.V(1).Info("finished building envoy gateway rate limit clusters")

	kuadrant := GetKuadrantFromTopology(topology)
	if kuadrant == nil {
		return nil
	}

	limitadorObj, found := lo.Find(topology.Objects().Children(kuadrant), func(child machinery.Object) bool {
		return child.GroupVersionKind().GroupKind() == kuadrantv1beta1.LimitadorGroupKind
	})
	if !found {
		logger.V(1).Info(ErrMissingLimitador.Error())
		return nil
	}
	limitador := limitadorObj.(*controller.RuntimeObject).Object.(*limitadorv1alpha1.Limitador)

	// Collect gateways from both RateLimitPolicies and TokenRateLimitPolicies
	var gateways []*machinery.Gateway

	// Get gateways from RateLimitPolicies
	effectiveRateLimitPolicies, rlpOk := state.Load(StateEffectiveRateLimitPolicies)
	if rlpOk && effectiveRateLimitPolicies != nil {
		rlpGateways := lo.FilterMap(lo.Values(effectiveRateLimitPolicies.(EffectiveRateLimitPolicies)), func(effectivePolicy EffectiveRateLimitPolicy, _ int) (*machinery.Gateway, bool) {
			gatewayClass, gateway, _, _, _, _ := kuadrantpolicymachinery.ObjectsInRequestPath(effectivePolicy.Path)
			return gateway, lo.Contains(envoyGatewayGatewayControllerNames, gatewayClass.Spec.ControllerName)
		})
		gateways = append(gateways, rlpGateways...)
	}

	// Get gateways from TokenRateLimitPolicies
	effectiveTokenRateLimitPolicies, trlpOk := state.Load(StateEffectiveTokenRateLimitPolicies)
	if trlpOk && effectiveTokenRateLimitPolicies != nil {
		trlpGateways := lo.FilterMap(lo.Values(effectiveTokenRateLimitPolicies.(EffectiveTokenRateLimitPolicies)), func(effectivePolicy EffectiveTokenRateLimitPolicy, _ int) (*machinery.Gateway, bool) {
			gatewayClass, gateway, _, _, _, _ := kuadrantpolicymachinery.ObjectsInRequestPath(effectivePolicy.Path)
			return gateway, lo.Contains(envoyGatewayGatewayControllerNames, gatewayClass.Spec.ControllerName)
		})
		gateways = append(gateways, trlpGateways...)
	}

	// Remove duplicates
	gateways = lo.UniqBy(gateways, func(gateway *machinery.Gateway) string {
		return gateway.GetLocator()
	})

	desiredEnvoyPatchPolicies := make(map[k8stypes.NamespacedName]struct{})
	var modifiedGateways []string

	if len(gateways) == 0 {
		logger.V(1).Info("no gateways with rate limiting policies found")
	}

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

	limitadorServiceInfo := ServiceSpecFromLimitador(limitador)
	jsonPatches, err := kuadrantenvoygateway.BuildEnvoyPatchPolicyClusterPatch(limitadorServiceInfo.ToClusterName(), limitadorServiceInfo.Host, int(limitadorServiceInfo.Port), false, rateLimitClusterPatch)
	if err != nil {
		return nil, err
	}
	envoyPatchPolicy.Spec.JSONPatches = jsonPatches

	return envoyPatchPolicy, nil
}

func rateLimitClusterPatch(clusterName, host string, port int, mTLS bool) map[string]any {
	base := map[string]any{
		"name":                   clusterName,
		"type":                   "STRICT_DNS",
		"connect_timeout":        "1s",
		"lb_policy":              "ROUND_ROBIN",
		"http2_protocol_options": map[string]any{},
		"load_assignment": map[string]any{
			"cluster_name": clusterName,
			"endpoints": []map[string]any{
				{
					"lb_endpoints": []map[string]any{
						{
							"endpoint": map[string]any{
								"address": map[string]any{
									"socket_address": map[string]any{
										"address":    host,
										"port_value": port,
									},
								},
							},
						},
					},
				},
			},
		},
	}
	if mTLS {
		base["transport_socket"] = map[string]interface{}{
			"name": "envoy.transport_sockets.tls",
			"typed_config": map[string]interface{}{
				"@type": "type.googleapis.com/envoy.extensions.transport_sockets.tls.v3.UpstreamTlsContext",
				"common_tls_context": map[string]interface{}{
					"tls_certificate_sds_secret_configs": []interface{}{
						map[string]interface{}{
							"name": "default",
							"sds_config": map[string]interface{}{
								"api_config_source": map[string]interface{}{
									"api_type": "GRPC",
									"grpc_services": []interface{}{
										map[string]interface{}{
											"envoy_grpc": map[string]interface{}{
												"cluster_name": "sds-grpc",
											},
										},
									},
								},
							},
						},
					},
					"validation_context_sds_secret_config": map[string]interface{}{
						"name": "ROOTCA",
						"sds_config": map[string]interface{}{
							"api_config_source": map[string]interface{}{
								"api_type": "GRPC",
								"grpc_services": []interface{}{
									map[string]interface{}{
										"envoy_grpc": map[string]interface{}{
											"cluster_name": "sds-grpc",
										},
									},
								},
							},
						},
					},
				},
			},
		}
	}
	return base
}
