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
// exports the complete event as UTF-8 JSON, and Envoy emits that value as one raw access-log line.
func BuildEnvoyFilterOCSFAccessLogPatch() (*istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch, error) {
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
								"text_format_source": map[string]any{
									"inline_string": "%FILTER_STATE(" + ocsfEventFilterStateKey + ":PLAIN)%\n",
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
