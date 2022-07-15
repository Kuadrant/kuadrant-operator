package istio

import (
	"encoding/json"
	"fmt"

	istioapiv1alpha3 "istio.io/api/networking/v1alpha3"
	istionetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kuadrant/kuadrant-controller/pkg/common"
	"github.com/kuadrant/kuadrant-controller/pkg/rlptools"
)

// LimitadorClusterPatch returns an EnvoyFilter patch that adds a custom cluster entry to compensate for kuadrant/limitador#53.
// Note: This should be removed once the mentioned issue is fixed but that will take some time.
func LimitadorClusterPatch(limitadorSvc string, limitadorGRPCPort int) ([]*istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch, error) {
	// The patch defines the rate_limit_cluster, which provides the endpoint location of the external rate limit service.
	patchUnstructured := map[string]interface{}{
		"operation": "ADD",
		"value": map[string]interface{}{
			"name":                   common.KuadrantRateLimitClusterName,
			"type":                   "STRICT_DNS",
			"connect_timeout":        "1s",
			"lb_policy":              "ROUND_ROBIN",
			"http2_protocol_options": map[string]interface{}{},
			"load_assignment": map[string]interface{}{
				"cluster_name": common.KuadrantRateLimitClusterName,
				"endpoints": []map[string]interface{}{
					{
						"lb_endpoints": []map[string]interface{}{
							{
								"endpoint": map[string]interface{}{
									"address": map[string]interface{}{
										"socket_address": map[string]interface{}{
											"address":    rlptools.LimitadorServiceClusterHost(limitadorSvc),
											"port_value": limitadorGRPCPort,
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	patchRaw, _ := json.Marshal(patchUnstructured)

	patch := &istioapiv1alpha3.EnvoyFilter_Patch{}
	err := patch.UnmarshalJSON(patchRaw)
	if err != nil {
		return nil, err
	}

	return []*istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch{
		{
			ApplyTo: istioapiv1alpha3.EnvoyFilter_CLUSTER,
			Match: &istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch{
				ObjectTypes: &istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
					Cluster: &istioapiv1alpha3.EnvoyFilter_ClusterMatch{
						Service: rlptools.LimitadorServiceClusterHost(limitadorSvc),
					},
				},
			},
			Patch: patch,
		},
	}, nil
}

func AlwaysUpdateEnvoyFilter(existingObj, desiredObj client.Object) (bool, error) {
	existing, ok := existingObj.(*istionetworkingv1alpha3.EnvoyFilter)
	if !ok {
		return false, fmt.Errorf("%T is not a *istionetworkingv1alpha3.EnvoyFilter", existingObj)
	}
	desired, ok := desiredObj.(*istionetworkingv1alpha3.EnvoyFilter)
	if !ok {
		return false, fmt.Errorf("%T is not a *istionetworkingv1alpha3.EnvoyFilter", desiredObj)
	}
	existing.Spec = istioapiv1alpha3.EnvoyFilter{
		WorkloadSelector: desired.Spec.WorkloadSelector,
		ConfigPatches:    desired.Spec.ConfigPatches,
		Priority:         desired.Spec.Priority,
	}
	return true, nil
}
