// Package wasm provides types and utilities for building WebAssembly plugin
// configurations for Envoy-based data plane integration.
//
// This package translates Kuadrant policies (AuthPolicy, RateLimitPolicy, etc.)
// into WASM plugin configurations deployed to Envoy proxies via Istio or Envoy Gateway.
//
// # Core Types
//
//   - Config: Top-level WASM configuration with services, action sets, and observability
//   - Service: External services the plugin calls (Authorino, Limitador, tracing)
//   - ActionSet: Actions triggered when route conditions match
//   - Action: Individual enforcement steps (auth checks, rate limiting)
//
// # Gateway API Integration
//
// Route matches from HTTPRoute and GRPCRoute are converted to CEL predicates
// for runtime evaluation. gRPC routes are represented as HTTP/2 paths since
// gRPC methods map to paths like "/{service}/{method}".
//
// # Semantic Equality
//
// EqualTo methods implement semantic equality - configs are equal if functionally
// equivalent, ignoring collection order where it doesn't affect behavior.
// This prevents unnecessary data plane updates.
//
// # Serialization
//
// Configs serialize to JSON (ToJSON) for Kubernetes resources and Protobuf Struct
// (ToStruct) for Istio/Envoy Gateway integration.
package wasm
