package istio

import (
	"encoding/json"
	"fmt"

	istioapinetworkingv1alpha3 "istio.io/api/networking/v1alpha3"
)

const (
	httpConnectionManagerFilterName = "envoy.filters.network.http_connection_manager"
	ocsfStdoutAccessLoggerName      = "envoy.access_loggers.stdout"
	ocsfCELFormatterName            = "envoy.formatter.cel"
	ocsfCELAccessLogFilterName      = "envoy.access_loggers.extension_filters.cel"
	ocsfCELAccessLogFilterType      = "type.googleapis.com/envoy.extensions.access_loggers.filters.cel.v3.ExpressionFilter"
	ocsfResourceTypeFilterStateKey  = "wasm.kuadrant.ocsf.resource.type"
	ocsfActorNameFilterStateKey     = "wasm.kuadrant.ocsf.actor.user.name"
	ocsfActorUIDFilterStateKey      = "wasm.kuadrant.ocsf.actor.user.uid"
	ocsfActorGroupsFilterStateKey   = "wasm.kuadrant.ocsf.actor.user.groups"
)

// versionString is set by the Makefile linker flags for release builds.
var versionString = "unknown"

// BuildEnvoyFilterOCSFAccessLogPatch returns an EnvoyFilter patch that appends one OCSF 1.9.0
// API Activity record to the gateway's stdout access log when a request completes. Authentication
// and target values are scalar filter-state properties exported by the Kuadrant wasm pipeline.
func BuildEnvoyFilterOCSFAccessLogPatch() (*istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch, error) {
	const (
		requestStartMillis = `(request.time - timestamp('1970-01-01T00:00:00Z')).getMilliseconds()`
		requestEndMillis   = requestStartMillis + ` + request.duration.getMilliseconds()`
	)
	actorPresent := fmt.Sprintf(`%q in filter_state && filter_state[%q] != ""`, ocsfActorNameFilterStateKey, ocsfActorNameFilterStateKey)
	actorName := fmt.Sprintf(`%s ? filter_state[%q] : "anonymous"`, actorPresent, ocsfActorNameFilterStateKey)
	actorTypeID := fmt.Sprintf(`%s ? (filter_state[%q].startsWith("system:serviceaccount:") ? 4 : 1) : 0`, actorPresent, ocsfActorNameFilterStateKey)
	actorUID := fmt.Sprintf(`%q in filter_state ? filter_state[%q] : ""`, ocsfActorUIDFilterStateKey, ocsfActorUIDFilterStateKey)
	actorGroups := fmt.Sprintf(`%q in filter_state ? filter_state[%q].map(group, {"name": group}) : []`, ocsfActorGroupsFilterStateKey, ocsfActorGroupsFilterStateKey)
	httpVersion := `request.protocol == "HTTP/2" ? "2.0" : request.protocol == "HTTP/3" ? "3.0" : request.protocol.startsWith("HTTP/") ? request.protocol.substring(5) : request.protocol`

	jsonFormat := map[string]any{
		"activity_id":   99,
		"activity_name": "Inference",
		"category_uid":  6,
		"category_name": "Application Activity",
		"class_uid":     6003,
		"class_name":    "API Activity",
		"type_uid":      600399,
		"severity_id":   1,
		"severity":      "Informational",
		"time":          "%TYPED_CEL(" + requestStartMillis + ")%",
		"metadata": map[string]any{
			"version":  "1.9.0",
			"profiles": []any{"ai_operation"},
			"product": map[string]any{
				"name":    "kuadrant-gateway",
				"version": versionString,
			},
			"logged_time": "%TYPED_CEL(" + requestEndMillis + ")%",
		},
		"actor": map[string]any{
			"user": map[string]any{
				"name":    "%TYPED_CEL(" + actorName + ")%",
				"uid":     "%TYPED_CEL(" + actorUID + ")%",
				"type_id": "%TYPED_CEL(" + actorTypeID + ")%",
				"groups":  "%TYPED_CEL(" + actorGroups + ")%",
			},
		},
		"api": map[string]any{
			"operation": "%REQ(:METHOD)% %REQ_WITHOUT_QUERY(:PATH)%",
		},
		"http_request": map[string]any{
			"http_method": "%REQ(:METHOD)%",
			"version":     "%TYPED_CEL(" + httpVersion + ")%",
			"url": map[string]any{
				"path": "%REQ_WITHOUT_QUERY(:PATH)%",
			},
		},
		"http_response": map[string]any{
			"code":        "%TYPED_CEL(response.code)%",
			"body_length": "%TYPED_CEL(response.size)%",
			"latency":     "%TYPED_CEL(request.duration.getMilliseconds())%",
		},
		"src_endpoint": map[string]any{
			"ip":   "%DOWNSTREAM_DIRECT_REMOTE_ADDRESS_WITHOUT_PORT%",
			"port": "%TYPED_CEL(source.port)%",
		},
		"dst_endpoint": "%TYPED_CEL(has(upstream.address) ? {'ip': upstream.address, 'port': upstream.port} : null)%",
		"status_id":    "%TYPED_CEL(response.code < 400 ? 1 : 2)%",
		"status":       "%TYPED_CEL(response.code < 400 ? 'Success' : 'Failure')%",
		"status_code":  "%TYPED_CEL(string(response.code))%",
		"resources": []any{
			map[string]any{
				"name":      "%FILTER_STATE(wasm.kuadrant.ocsf.resource.name:TYPED)%",
				"namespace": "%FILTER_STATE(wasm.kuadrant.ocsf.resource.namespace:TYPED)%",
				"type":      "%FILTER_STATE(wasm.kuadrant.ocsf.resource.type:TYPED)%",
				"role_id":   1,
				"role":      "Target",
			},
		},
		"ai_model": map[string]any{
			"name":        "%FILTER_STATE(wasm.kuadrant.ocsf.resource.name:TYPED)%",
			"ai_provider": "%FILTER_STATE(wasm.kuadrant.ocsf.ai_model.ai_provider:TYPED)%",
		},
	}

	patchRaw, err := json.Marshal(map[string]any{
		"operation": "MERGE",
		"value": map[string]any{
			"name": httpConnectionManagerFilterName,
			"typed_config": map[string]any{
				"@type": "type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager",
				"access_log": []any{
					map[string]any{
						"name": ocsfStdoutAccessLoggerName,
						"filter": map[string]any{
							"extension_filter": map[string]any{
								"name": ocsfCELAccessLogFilterName,
								"typed_config": map[string]any{
									"@type":      ocsfCELAccessLogFilterType,
									"expression": fmt.Sprintf("%q in filter_state", ocsfResourceTypeFilterStateKey),
								},
							},
						},
						"typed_config": map[string]any{
							"@type": "type.googleapis.com/envoy.extensions.access_loggers.stream.v3.StdoutAccessLog",
							"log_format": map[string]any{
								"typed_json_format": jsonFormat,
								"omit_empty_values": true,
								"formatters": []any{
									map[string]any{
										"name": ocsfCELFormatterName,
										"typed_config": map[string]any{
											"@type": "type.googleapis.com/envoy.extensions.formatter.cel.v3.Cel",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	})
	if err != nil {
		return nil, err
	}
	patch := &istioapinetworkingv1alpha3.EnvoyFilter_Patch{}
	if err := patch.UnmarshalJSON(patchRaw); err != nil {
		return nil, err
	}

	return &istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch{
		ApplyTo: istioapinetworkingv1alpha3.EnvoyFilter_NETWORK_FILTER,
		Match: &istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch{
			Context: istioapinetworkingv1alpha3.EnvoyFilter_GATEWAY,
			ObjectTypes: &istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
				Listener: &istioapinetworkingv1alpha3.EnvoyFilter_ListenerMatch{
					FilterChain: &istioapinetworkingv1alpha3.EnvoyFilter_ListenerMatch_FilterChainMatch{
						Filter: &istioapinetworkingv1alpha3.EnvoyFilter_ListenerMatch_FilterMatch{
							Name: httpConnectionManagerFilterName,
						},
					},
				},
			},
		},
		Patch: patch,
	}, nil
}
