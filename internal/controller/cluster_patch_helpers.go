package controllers

// buildClusterPatch creates an Envoy cluster configuration patch with optional mTLS support.
// HTTP/2 is always enabled since all current callers require it (gRPC upstreams).
func buildClusterPatch(clusterName, host string, port int, mTLS bool) map[string]any {
	base := map[string]any{
		"name":            clusterName,
		"type":            "STRICT_DNS",
		"connect_timeout": "1s",
		"lb_policy":       "ROUND_ROBIN",
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

	base["http2_protocol_options"] = map[string]any{}

	// Add mTLS configuration if needed
	if mTLS {
		base["transport_socket"] = buildMTLSTransportSocket()
	}

	return base
}

// grpcOutlierDetection returns an Envoy outlier detection configuration for pure gRPC
// upstreams. It ejects pod endpoints that accumulate consecutive gateway failures
// (gRPC UNAVAILABLE / connection errors) between DNS refresh cycles, reducing the
// window where traffic is routed to a dead pod after a rolling update or restart.
// Enabling this block also activates the consecutive_5xx detector (Envoy default:
// 5 errors, 100% enforcing). For connection-level failures (the pod-restart scenario
// we protect against), both detectors increment on the same events and eject at the
// same threshold — 5xx does not fire sooner. For application-level gRPC errors
// (non-zero gRPC status in trailers), HTTP/2 status is 200, so 5xx does not fire at
// all; only gateway_failure applies.
// Only non-default values are set here:
//   - enforcing_consecutive_gateway_failure: defaults to 0 (unenforced); set to 100
//     so consecutive gateway failures actually trigger ejection.
//   - max_ejection_percent: defaults to 10; set to 100 to allow ejecting all
//     endpoints when all pods are simultaneously unhealthy.
func grpcOutlierDetection() map[string]any {
	return map[string]any{
		// Default is 0 (unenforced) — must be 100 to actually eject on gateway failures.
		"enforcing_consecutive_gateway_failure": 100,
		// Default is 10% — set to 100 to allow all endpoints to be ejected if all are unhealthy.
		"max_ejection_percent": 100,
	}
}

// buildMTLSTransportSocket creates the mTLS transport socket configuration using SDS
func buildMTLSTransportSocket() map[string]interface{} {
	return map[string]interface{}{
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
