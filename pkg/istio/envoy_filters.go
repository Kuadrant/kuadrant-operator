package istio

import (
	"encoding/json"
	"fmt"

	istioapiv1alpha3 "istio.io/api/networking/v1alpha3"
	istionetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kuadrant/kuadrant-controller/pkg/common"
)

const LimitadorClusterEnvoyFilterName = "limitador-cluster-patch"

type EnvoyFilterFactory struct {
	ObjectName string
	Namespace  string
	Patches    []*istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch
	Labels     map[string]string
}

func (v *EnvoyFilterFactory) EnvoyFilter() *istionetworkingv1alpha3.EnvoyFilter {
	return &istionetworkingv1alpha3.EnvoyFilter{
		TypeMeta: metav1.TypeMeta{
			Kind:       "EnvoyFilter",
			APIVersion: "networking.istio.io/v1alpha3",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      v.ObjectName,
			Namespace: v.Namespace,
		},
		Spec: istioapiv1alpha3.EnvoyFilter{
			WorkloadSelector: &istioapiv1alpha3.WorkloadSelector{
				Labels: v.Labels,
			},
			ConfigPatches: v.Patches,
		},
	}
}

// LimitadorClusterEnvoyFilter returns an EnvoyFilter that adds a custom cluster entry to compensate for kuadrant/limitador#53.
// Note: This should be removed once the mentioned issue is fixed but that will take some time.
func LimitadorClusterEnvoyFilter(gwKey client.ObjectKey, labels map[string]string) *istionetworkingv1alpha3.EnvoyFilter {
	// The patch defines the rate_limit_cluster, which provides the endpoint location of the external rate limit service.
	patchUnstructured := map[string]interface{}{
		"operation": "ADD",
		"value": map[string]interface{}{
			"name":                   PatchedLimitadorClusterName,
			"type":                   "STRICT_DNS",
			"connect_timeout":        "1s",
			"lb_policy":              "ROUND_ROBIN",
			"http2_protocol_options": map[string]interface{}{},
			"load_assignment": map[string]interface{}{
				"cluster_name": PatchedLimitadorClusterName,
				"endpoints": []map[string]interface{}{
					{
						"lb_endpoints": []map[string]interface{}{
							{
								"endpoint": map[string]interface{}{
									"address": map[string]interface{}{
										"socket_address": map[string]interface{}{
											"address":    common.LimitadorServiceClusterHost,
											"port_value": common.LimitadorServiceGrpcPort,
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
		panic(err)
	}

	envoyFilterFactory := EnvoyFilterFactory{
		ObjectName: LimitadorClusterEnvoyFilterName,
		Namespace:  gwKey.Namespace,
		Patches: []*istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch{{
			ApplyTo: istioapiv1alpha3.EnvoyFilter_CLUSTER,
			Match: &istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch{
				ObjectTypes: &istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
					Cluster: &istioapiv1alpha3.EnvoyFilter_ClusterMatch{
						Service: common.LimitadorServiceClusterHost,
					},
				},
			},
			Patch: patch,
		}},
	}
	return envoyFilterFactory.EnvoyFilter()
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
