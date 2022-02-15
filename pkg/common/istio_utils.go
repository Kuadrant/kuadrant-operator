package common

import (
	"encoding/json"

	istioapiv1alpha3 "istio.io/api/networking/v1alpha3"
	istionetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apimv1alpha1 "github.com/kuadrant/kuadrant-controller/apis/apim/v1alpha1"
)

type HTTPFilterStage uint32

const (
	PreAuthStage HTTPFilterStage = iota
	PostAuthStage

	PatchedLimitadorClusterName = "rate-limit-cluster"
)

type EnvoyFilterFactory struct {
	ObjectName string
	Namespace  string
	Patches    []*istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch
	Labels     map[string]string
}

func (v *EnvoyFilterFactory) EnvoyFilter() *istionetworkingv1alpha3.EnvoyFilter {
	if len(v.Labels) == 0 {
		// default to kuadrant labels to avoid replication where it's already used.
		v.Labels = map[string]string{"istio": "kuadrant-system"}
	}
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

func LimitadorClusterEnvoyPatch() *istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch {
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
											"address":    LimitadorServiceClusterHost,
											"port_value": LimitadorServiceGrpcPort,
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

	return &istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch{
		ApplyTo: istioapiv1alpha3.EnvoyFilter_CLUSTER,
		Match: &istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch{
			ObjectTypes: &istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
				Cluster: &istioapiv1alpha3.EnvoyFilter_ClusterMatch{
					Service: LimitadorServiceClusterHost,
				},
			},
		},
		Patch: patch,
	}
}

// EnvoyFilterRatelimitsUnstructured returns "rate_limits" envoy filter patch format from kuadrant rate limits
func EnvoyFilterRatelimitsUnstructured(rateLimits []*apimv1alpha1.RateLimit) []map[string]interface{} {
	envoyRateLimits := make([]map[string]interface{}, 0)
	for _, rateLimit := range rateLimits {
		if rateLimit.Stage == apimv1alpha1.RateLimitStageBOTH {
			// Apply same actions to both stages
			stages := []apimv1alpha1.RateLimitStage{
				apimv1alpha1.RateLimitStagePREAUTH,
				apimv1alpha1.RateLimitStagePOSTAUTH,
			}

			for _, stage := range stages {
				envoyRateLimit := map[string]interface{}{
					"stage":   apimv1alpha1.RateLimitStageValue[stage],
					"actions": rateLimit.Actions,
				}
				envoyRateLimits = append(envoyRateLimits, envoyRateLimit)
			}
		} else {
			envoyRateLimit := map[string]interface{}{
				"stage":   apimv1alpha1.RateLimitStageValue[rateLimit.Stage],
				"actions": rateLimit.Actions,
			}
			envoyRateLimits = append(envoyRateLimits, envoyRateLimit)
		}
	}

	return envoyRateLimits
}
