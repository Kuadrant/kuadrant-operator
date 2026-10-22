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
	ocsfURLPathFilterStateKey       = "wasm.kuadrant.ocsf.http_request.url.path"
)

// versionString is set by the Makefile linker flags for release builds.
var versionString = "unknown"

// BuildEnvoyFilterOCSFAccessLogPatch returns an EnvoyFilter patch that appends one OCSF 1.9.0
// API Activity record to the gateway's stdout access log when a request completes. Filter-state
// values are passed intact to Envoy's typed CEL formatter; missing optional values are guarded.
func BuildEnvoyFilterOCSFAccessLogPatch() (*istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch, error) {
	const requestStartDuration = `(request.time - timestamp('1970-01-01T00:00:00Z'))`
	requestStartMillis := `(` + requestStartDuration + `.getSeconds() * 1000 + ` + requestStartDuration + `.getMilliseconds())`
	requestDurationMillis := `(request.duration.getSeconds() * 1000 + request.duration.getMilliseconds())`
	requestEndMillis := requestStartMillis + ` + ` + requestDurationMillis
	actorName := ocsfTypedFilterStateValue(ocsfActorNameFilterStateKey)
	actorTypeID := fmt.Sprintf(
		`%q in filter_state ? (filter_state[%q].startsWith("system:serviceaccount:") ? 4 : 1) : null`,
		ocsfActorNameFilterStateKey,
		ocsfActorNameFilterStateKey,
	)
	actorUID := ocsfTypedFilterStateValue(ocsfActorUIDFilterStateKey)
	urlPath := ocsfTypedFilterStateValue(ocsfURLPathFilterStateKey)
	actorGroups := ocsfTypedFilterStateValue(ocsfActorGroupsFilterStateKey)
	httpVersion := `request.protocol == "HTTP/1.0" ? "1.0" : request.protocol == "HTTP/1.1" ? "1.1" : request.protocol == "HTTP/2" ? "2.0" : request.protocol == "HTTP/3" ? "3.0" : request.protocol`

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
				"name":    actorName,
				"uid":     actorUID,
				"type_id": ocsfTypedCEL(actorTypeID),
				"groups":  actorGroups,
			},
		},
		"api": map[string]any{
			"operation": "%REQ(:METHOD)% " + urlPath,
		},
		"http_request": map[string]any{
			"http_method": "%REQ(:METHOD)%",
			"version":     "%TYPED_CEL(" + httpVersion + ")%",
			"url": map[string]any{
				"path": urlPath,
			},
		},
		"http_response": map[string]any{
			"code":        "%TYPED_CEL(response.code)%",
			"body_length": "%TYPED_CEL(response.size)%",
			"latency":     "%TYPED_CEL(" + requestDurationMillis + ")%",
		},
		"src_endpoint": map[string]any{
			"ip":   "%DOWNSTREAM_DIRECT_REMOTE_ADDRESS_WITHOUT_PORT%",
			"port": "%TYPED_CEL(source.port)%",
		},
		"dst_endpoint": map[string]any{
			"ip":   "%UPSTREAM_REMOTE_ADDRESS_WITHOUT_PORT%",
			"port": "%TYPED_CEL(has(upstream.address) ? upstream.port : null)%",
		},
		"status_id":   "%TYPED_CEL(response.code < 400 ? 1 : 2)%",
		"status":      "%TYPED_CEL(response.code < 400 ? 'Success' : 'Failure')%",
		"status_code": "%RESPONSE_CODE%",
		"resources": []any{
			map[string]any{
				"name":      ocsfTypedFilterStateValue("wasm.kuadrant.ocsf.resource.name"),
				"namespace": ocsfTypedFilterStateValue("wasm.kuadrant.ocsf.resource.namespace"),
				"type":      ocsfTypedFilterStateValue("wasm.kuadrant.ocsf.resource.type"),
				"role_id":   1,
				"role":      "Target",
			},
		},
		"ai_model": map[string]any{
			"name":        ocsfTypedFilterStateValue("wasm.kuadrant.ocsf.resource.name"),
			"ai_provider": ocsfTypedFilterStateValue("wasm.kuadrant.ocsf.ai_model.ai_provider"),
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
									"expression": fmt.Sprintf(`%q in filter_state && filter_state[%q] != ""`, ocsfResourceTypeFilterStateKey, ocsfResourceTypeFilterStateKey),
								},
							},
						},
						"typed_config": map[string]any{
							"@type": "type.googleapis.com/envoy.extensions.access_loggers.stream.v3.StdoutAccessLog",
							"log_format": map[string]any{
								"json_format":       jsonFormat,
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

func ocsfOptionalFilterStateValue(key string) string {
	return fmt.Sprintf(`%q in filter_state ? filter_state[%q] : null`, key, key)
}

func ocsfTypedFilterStateValue(key string) string {
	return ocsfTypedCEL(ocsfOptionalFilterStateValue(key))
}

func ocsfTypedCEL(expression string) string {
	return "%TYPED_CEL(" + expression + ")%"
}
