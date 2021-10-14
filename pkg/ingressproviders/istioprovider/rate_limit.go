/*
 Copyright 2021 Red Hat, Inc.

 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package istioprovider

import (
	"context"
	"fmt"
	"reflect"

	istioapiv1alpha3 "istio.io/api/networking/v1alpha3"
	istionetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	"k8s.io/apimachinery/pkg/util/json"
	"sigs.k8s.io/controller-runtime/pkg/client"

	networkingv1beta1 "github.com/kuadrant/kuadrant-controller/apis/networking/v1beta1"
	"github.com/kuadrant/kuadrant-controller/pkg/common"
	"github.com/kuadrant/kuadrant-controller/pkg/reconcilers"
)

type HTTPFilterStage uint32

const (
	PreAuthStage HTTPFilterStage = iota
	PostAuthStage
)

func (is *IstioProvider) reconcileRateLimit(ctx context.Context, apip *networkingv1beta1.APIProduct) error {
	desiredEF := clusterEnvoyFilter(apip)
	err := is.ReconcileIstioEnvoyFilter(ctx, desiredEF, envoyFilterBasicMutator)
	if err != nil {
		return err
	}

	desiredEF = preAuthEnvoyFilter(apip)
	err = is.ReconcileIstioEnvoyFilter(ctx, desiredEF, envoyFilterBasicMutator)
	if err != nil {
		return err
	}

	desiredEF = postAuthEnvoyFilter(apip)
	err = is.ReconcileIstioEnvoyFilter(ctx, desiredEF, envoyFilterBasicMutator)
	if err != nil {
		return err
	}

	return nil
}

func clusterEnvoyFilter(apip *networkingv1beta1.APIProduct) *istionetworkingv1alpha3.EnvoyFilter {
	factory := EnvoyFilterFactory{
		ObjectName: fmt.Sprintf("%s.%s-rate-limit-cluster", apip.Name, apip.Namespace),
		Namespace:  common.KuadrantNamespace,
		Patches:    make([]*istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch, 0),
	}

	if !apip.IsRateLimitEnabled() {
		envoyFilter := factory.EnvoyFilter()
		common.TagObjectToDelete(envoyFilter)
		return envoyFilter
	}

	factory.Patches = append(factory.Patches, clusterEnvoyPatch())
	return factory.EnvoyFilter()
}

func preAuthEnvoyFilter(apip *networkingv1beta1.APIProduct) *istionetworkingv1alpha3.EnvoyFilter {
	factory := EnvoyFilterFactory{
		ObjectName: fmt.Sprintf("%s.%s-rate-limit-preauth", apip.Name, apip.Namespace),
		Namespace:  common.KuadrantNamespace,
		Patches:    make([]*istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch, 0),
	}

	if !apip.IsPreAuthRateLimitEnabled() {
		envoyFilter := factory.EnvoyFilter()
		common.TagObjectToDelete(envoyFilter)
		return envoyFilter
	}

	factory.Patches = append(factory.Patches, preAuthHTTPFilterEnvoyPatch(apip))

	for _, host := range apip.Spec.Hosts {
		factory.Patches = append(factory.Patches, preAuthActionsEnvoyPatch(apip, host))
	}

	return factory.EnvoyFilter()
}

func postAuthEnvoyFilter(apip *networkingv1beta1.APIProduct) *istionetworkingv1alpha3.EnvoyFilter {
	factory := EnvoyFilterFactory{
		ObjectName: fmt.Sprintf("%s.%s-rate-limit-postauth", apip.Name, apip.Namespace),
		Namespace:  common.KuadrantNamespace,
		Patches:    make([]*istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch, 0),
	}

	if apip.AuthRateLimit() == nil {
		envoyFilter := factory.EnvoyFilter()
		common.TagObjectToDelete(envoyFilter)
		return envoyFilter
	}

	factory.Patches = append(factory.Patches, postAuthHTTPFilterEnvoyPatch(apip))

	for _, host := range apip.Spec.Hosts {
		factory.Patches = append(factory.Patches, postAuthActionsEnvoyPatch(host))
	}

	return factory.EnvoyFilter()
}

func preAuthActionsEnvoyPatch(apip *networkingv1beta1.APIProduct, host string) *istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch {
	actions := []map[string]interface{}{}
	if apip.GlobalRateLimit() != nil {
		action := map[string]interface{}{
			"generic_key": map[string]string{"descriptor_value": "kuadrant"},
		}
		actions = append(actions, action)
	}

	if apip.PerRemoteIPRateLimit() != nil {
		action := map[string]interface{}{
			"remote_address": map[string]interface{}{},
		}
		actions = append(actions, action)
	}

	// defines the route configuration on which to rate limit
	patchUnstructured := map[string]interface{}{
		"operation": "MERGE",
		"value": map[string]interface{}{
			"rate_limits": []map[string]interface{}{
				{
					"stage":   PreAuthStage,
					"actions": actions,
				},
			},
		},
	}

	patchRaw, err := json.Marshal(patchUnstructured)
	if err != nil {
		panic(err)
	}

	patch := &istioapiv1alpha3.EnvoyFilter_Patch{}
	err = patch.UnmarshalJSON(patchRaw)
	if err != nil {
		panic(err)
	}

	return &istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch{
		ApplyTo: istioapiv1alpha3.EnvoyFilter_VIRTUAL_HOST,
		Match: &istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch{
			Context: istioapiv1alpha3.EnvoyFilter_GATEWAY,
			ObjectTypes: &istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
				RouteConfiguration: &istioapiv1alpha3.EnvoyFilter_RouteConfigurationMatch{
					Vhost: &istioapiv1alpha3.EnvoyFilter_RouteConfigurationMatch_VirtualHostMatch{
						Name: fmt.Sprintf("%s:80", host),
						Route: &istioapiv1alpha3.EnvoyFilter_RouteConfigurationMatch_RouteMatch{
							Action: istioapiv1alpha3.EnvoyFilter_RouteConfigurationMatch_RouteMatch_ANY,
						},
					},
				},
			},
		},
		Patch: patch,
	}
}

func postAuthActionsEnvoyPatch(host string) *istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch {
	// defines the route configuration on which to rate limit
	patchUnstructured := map[string]interface{}{
		"operation": "MERGE",
		"value": map[string]interface{}{
			"rate_limits": []map[string]interface{}{
				{
					"stage": PostAuthStage,
					"actions": []map[string]interface{}{
						{
							"metadata": map[string]interface{}{
								"metadata_key": map[string]interface{}{
									"key": "envoy.filters.http.ext_authz",
									"path": []map[string]string{
										{
											"key": "ext_auth_data",
										},
										{
											"key": "user-id",
										},
									},
								},
								"descriptor_key": "user_id",
							},
						},
					},
				},
			},
		},
	}

	patchRaw, err := json.Marshal(patchUnstructured)
	if err != nil {
		panic(err)
	}

	patch := &istioapiv1alpha3.EnvoyFilter_Patch{}
	err = patch.UnmarshalJSON(patchRaw)
	if err != nil {
		panic(err)
	}

	return &istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch{
		ApplyTo: istioapiv1alpha3.EnvoyFilter_VIRTUAL_HOST,
		Match: &istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch{
			Context: istioapiv1alpha3.EnvoyFilter_GATEWAY,
			ObjectTypes: &istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
				RouteConfiguration: &istioapiv1alpha3.EnvoyFilter_RouteConfigurationMatch{
					Vhost: &istioapiv1alpha3.EnvoyFilter_RouteConfigurationMatch_VirtualHostMatch{
						Name: fmt.Sprintf("%s:80", host),
						Route: &istioapiv1alpha3.EnvoyFilter_RouteConfigurationMatch_RouteMatch{
							Action: istioapiv1alpha3.EnvoyFilter_RouteConfigurationMatch_RouteMatch_ANY,
						},
					},
				},
			},
		},
		Patch: patch,
	}
}

func preAuthHTTPFilterEnvoyPatch(apip *networkingv1beta1.APIProduct) *istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch {
	// The patch inserts the envoy.filters.http.preauth.ratelimit HTTP filter into the HTTP_FILTER chain,
	// BEFORE the external AUTH HTTP filter.
	// The rate_limit_service field specifies the external rate limit service, rate_limit_cluster in this case.
	// https://www.envoyproxy.io/docs/envoy/latest/api-v3/extensions/filters/http/ratelimit/v3/rate_limit.proto.html
	patchUnstructured := map[string]interface{}{
		"operation": "INSERT_BEFORE",
		"value": map[string]interface{}{
			"name": "envoy.filters.http.preauth.ratelimit",
			"typed_config": map[string]interface{}{
				"@type":             "type.googleapis.com/envoy.extensions.filters.http.ratelimit.v3.RateLimit",
				"domain":            apip.RateLimitDomainName(),
				"stage":             PreAuthStage,
				"failure_mode_deny": true,
				"rate_limit_service": map[string]interface{}{
					"transport_api_version": "V3",
					"grpc_service": map[string]interface{}{
						"timeout": "3s",
						"envoy_grpc": map[string]string{
							"cluster_name": "rate_limit_cluster",
						},
					},
				},
			},
		},
	}

	patchRaw, err := json.Marshal(patchUnstructured)
	if err != nil {
		panic(err)
	}

	patch := &istioapiv1alpha3.EnvoyFilter_Patch{}
	err = patch.UnmarshalJSON(patchRaw)
	if err != nil {
		panic(err)
	}

	// insert global rate limit http filter before external AUTH
	return &istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch{
		ApplyTo: istioapiv1alpha3.EnvoyFilter_HTTP_FILTER,
		Match: &istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch{
			Context: istioapiv1alpha3.EnvoyFilter_GATEWAY,
			ObjectTypes: &istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
				Listener: &istioapiv1alpha3.EnvoyFilter_ListenerMatch{
					FilterChain: &istioapiv1alpha3.EnvoyFilter_ListenerMatch_FilterChainMatch{
						Filter: &istioapiv1alpha3.EnvoyFilter_ListenerMatch_FilterMatch{
							Name: "envoy.filters.network.http_connection_manager",
							SubFilter: &istioapiv1alpha3.EnvoyFilter_ListenerMatch_SubFilterMatch{
								Name: "envoy.filters.http.ext_authz",
							},
						},
					},
				},
			},
		},
		Patch: patch,
	}
}

func postAuthHTTPFilterEnvoyPatch(apip *networkingv1beta1.APIProduct) *istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch {
	// The patch inserts the envoy.filters.http.postauth.ratelimit HTTP filter into the HTTP_FILTER chain,
	// AFTER the external AUTH HTTP filter.
	// The rate_limit_service field specifies the external rate limit service, rate_limit_cluster in this case.
	patchUnstructured := map[string]interface{}{
		"operation": "INSERT_AFTER",
		"value": map[string]interface{}{
			"name": "envoy.filters.http.postauth.ratelimit",
			"typed_config": map[string]interface{}{
				"@type":             "type.googleapis.com/envoy.extensions.filters.http.ratelimit.v3.RateLimit",
				"domain":            apip.RateLimitDomainName(),
				"stage":             PostAuthStage,
				"failure_mode_deny": true,
				"rate_limit_service": map[string]interface{}{
					"transport_api_version": "V3",
					"grpc_service": map[string]interface{}{
						"timeout": "3s",
						"envoy_grpc": map[string]string{
							"cluster_name": "rate_limit_cluster",
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

	// insert global rate limit http filter before external AUTH
	return &istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch{
		ApplyTo: istioapiv1alpha3.EnvoyFilter_HTTP_FILTER,
		Match: &istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch{
			Context: istioapiv1alpha3.EnvoyFilter_GATEWAY,
			ObjectTypes: &istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
				Listener: &istioapiv1alpha3.EnvoyFilter_ListenerMatch{
					FilterChain: &istioapiv1alpha3.EnvoyFilter_ListenerMatch_FilterChainMatch{
						Filter: &istioapiv1alpha3.EnvoyFilter_ListenerMatch_FilterMatch{
							Name: "envoy.filters.network.http_connection_manager",
							SubFilter: &istioapiv1alpha3.EnvoyFilter_ListenerMatch_SubFilterMatch{
								Name: "envoy.filters.http.ext_authz",
							},
						},
					},
				},
			},
		},
		Patch: patch,
	}
}

func clusterEnvoyPatch() *istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch {
	// The patch defines the rate_limit_cluster, which provides the endpoint location of the external rate limit service.

	patchUnstructured := map[string]interface{}{
		"operation": "ADD",
		"value": map[string]interface{}{
			"name":                   "rate_limit_cluster",
			"type":                   "STRICT_DNS",
			"connect_timeout":        "1s",
			"lb_policy":              "ROUND_ROBIN",
			"http2_protocol_options": map[string]interface{}{},
			"load_assignment": map[string]interface{}{
				"cluster_name": "rate_limit_cluster",
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

// TODO(eastizle): EnvoyFilter does not exist in "istio.io/client-go/pkg/apis/networking/v1alpha3". Alternative in v1beta1???
func (is *IstioProvider) ReconcileIstioEnvoyFilter(ctx context.Context, desired *istionetworkingv1alpha3.EnvoyFilter, mutatefn reconcilers.MutateFn) error {
	return is.ReconcileResource(ctx, &istionetworkingv1alpha3.EnvoyFilter{}, desired, mutatefn)
}

func envoyFilterBasicMutator(existingObj, desiredObj client.Object) (bool, error) {
	existing, ok := existingObj.(*istionetworkingv1alpha3.EnvoyFilter)
	if !ok {
		return false, fmt.Errorf("%T is not a *istionetworkingv1alpha3.EnvoyFilter", existingObj)
	}
	desired, ok := desiredObj.(*istionetworkingv1alpha3.EnvoyFilter)
	if !ok {
		return false, fmt.Errorf("%T is not a *istionetworkingv1alpha3.EnvoyFilter", desiredObj)
	}

	updated := false
	if !reflect.DeepEqual(existing.Spec, desired.Spec) {
		existing.Spec = desired.Spec
		updated = true
	}

	return updated, nil
}
