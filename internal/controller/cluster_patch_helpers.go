package controllers

// buildClusterPatch creates an Envoy cluster configuration patch with optional mTLS and HTTP/2 support
func buildClusterPatch(clusterName, host string, port int, mTLS bool, useHTTP2 bool) map[string]any {
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

	// Add HTTP/2 protocol options if needed
	if useHTTP2 {
		base["http2_protocol_options"] = map[string]any{}
	}

	// Add mTLS configuration if needed
	if mTLS {
		base["transport_socket"] = buildMTLSTransportSocket()
	}

	return base
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
