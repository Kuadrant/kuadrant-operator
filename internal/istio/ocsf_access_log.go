package istio

import (
	"encoding/json"
	"fmt"

	istioapinetworkingv1alpha3 "istio.io/api/networking/v1alpha3"

	wasmtypes "github.com/kuadrant/kuadrant-operator/internal/wasm"
)

const (
	httpConnectionManagerFilterName = "envoy.filters.network.http_connection_manager"
	ocsfStdoutAccessLoggerName      = "envoy.access_loggers.stdout"
	ocsfCELAccessLogFilterName      = "envoy.access_loggers.extension_filters.cel"
	ocsfCELAccessLogFilterType      = "type.googleapis.com/envoy.extensions.access_loggers.filters.cel.v3.ExpressionFilter"

	ocsfEventFilterStateKey = wasmtypes.OCSFEventFilterStateKey
)

// BuildEnvoyFilterOCSFAccessLogPatch returns an EnvoyFilter patch that appends one OCSF 1.9.0
// API Activity record to the gateway's stdout access log when a request completes. The WASM shim
// exports the event as a google.protobuf.Value; Envoy projects its typed fields through TYPED_CEL.
func ocsfEventFieldPath(fields ...string) string {
	path := fmt.Sprintf("filter_state[%q]", ocsfEventFilterStateKey)
	for _, field := range fields {
		path += fmt.Sprintf("[%q]", field)
	}
	return path
}

func ocsfTypedCEL(expression string) string {
	return "%TYPED_CEL(" + expression + ")%"
}

func BuildEnvoyFilterOCSFAccessLogPatch() (*istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch, error) {
	field := func(parts ...string) string {
		return ocsfTypedCEL(ocsfEventFieldPath(parts...))
	}
	requestStartMillis := `(request.time != null ? int(request.time) * 1000 + request.time.getMilliseconds() : 0)`
	requestEndMillis := `(request.time != null && request.duration != null ? int(request.time + request.duration) * 1000 + (request.time + request.duration).getMilliseconds() : 0)`
	requestDurationMillis := `(request.duration != null ? request.duration.getMilliseconds() : 0)`
	responseCode := `(response.code != null ? response.code : 0)`
	responseSize := `(response.size != null ? response.size : 0)`
	statusSuccess := `(response.code != null && response.code >= 200 && response.code < 300)`
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
									"expression": fmt.Sprintf(`%q in filter_state`, ocsfEventFilterStateKey),
								},
							},
						},
						"typed_config": map[string]any{
							"@type": "type.googleapis.com/envoy.extensions.access_loggers.stream.v3.StdoutAccessLog",
							"log_format": map[string]any{
								"json_format": map[string]any{
									"activity_id":   field("activity_id"),
									"activity_name": field("activity_name"),
									"category_uid":  field("category_uid"),
									"category_name": field("category_name"),
									"class_uid":     field("class_uid"),
									"class_name":    field("class_name"),
									"type_uid":      field("type_uid"),
									"severity_id":   field("severity_id"),
									"severity":      field("severity"),
									"time":          ocsfTypedCEL(requestStartMillis),
									"metadata": map[string]any{
										"version":  field("metadata", "version"),
										"profiles": []any{"ai_operation"},
										"product": map[string]any{
											"name":    field("metadata", "product", "name"),
											"version": field("metadata", "product", "version"),
										},
										"logged_time": ocsfTypedCEL(requestEndMillis),
									},
									"actor": map[string]any{
										"user": field("actor", "user"),
									},
									"api": map[string]any{
										"operation": field("api", "operation"),
									},
									"http_request": map[string]any{
										"http_method": field("http_request", "http_method"),
										"version":     field("http_request", "version"),
										"url": map[string]any{
											"path": field("http_request", "url", "path"),
										},
									},
									"http_response": map[string]any{
										"code":        ocsfTypedCEL(responseCode),
										"body_length": ocsfTypedCEL(responseSize),
										"latency":     ocsfTypedCEL(requestDurationMillis),
									},
									"status_id":   ocsfTypedCEL(fmt.Sprintf(`(%s) ? 1 : 2`, statusSuccess)),
									"status":      ocsfTypedCEL(fmt.Sprintf(`(%s) ? "Success" : "Failure"`, statusSuccess)),
									"status_code": ocsfTypedCEL(`response.headers[":status"]`),
									"resources":   field("resources"),
									"ai_model":    field("ai_model"),
									"unmapped": map[string]any{
										"kuadrant_response_succeeded": ocsfTypedCEL(statusSuccess),
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
