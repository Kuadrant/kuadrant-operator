# gRPC Backend (grpcbin)

This directory contains a simple gRPC backend deployment for testing GRPCRoute examples and verifying Kuadrant policy enforcement with gRPC traffic.

## Overview

**grpcbin** is a gRPC echo/testing service that provides multiple test methods and built-in gRPC reflection support. It's the gRPC equivalent of httpbin, making it ideal for demonstrations and testing.

- **Image:** `quay.io/kuadrant/grpcbin:latest`
- **Port:** 9000 (plain gRPC, no TLS)
- **Reflection:** Enabled by default

## Deployment

Deploy the gRPC backend to your cluster. These resources are deployed to the `default` namespace:

```bash
kubectl apply -f examples/grpc-backend/grpcbin.yaml -n default
```

Verify the deployment is ready:

```bash
kubectl get pods -l app=grpcbin -n default
kubectl get service grpcbin -n default
```

## Verification with grpcurl

[grpcurl](https://github.com/fullstorydev/grpcurl) is a command-line tool for interacting with gRPC servers. Install it to test the backend.

### List available services (using reflection)

```bash
kubectl run grpcurl --rm -it --image=mirror.gcr.io/fullstorydev/grpcurl:latest --restart=Never -- \
  -plaintext grpcbin.default.svc.cluster.local:9000 list
```

Expected output:
```text
grpc.reflection.v1alpha.ServerReflection
grpcbin.GRPCBin
```

### Test DummyUnary method

This method echoes back your request payload:

```bash
kubectl run grpcurl --rm -it --image=mirror.gcr.io/fullstorydev/grpcurl:latest --restart=Never -- \
  -plaintext -d '{"f_string": "hello"}' \
  grpcbin.default.svc.cluster.local:9000 \
  grpcbin.GRPCBin/DummyUnary
```

Expected output:
```json
{
  "f_string": "hello"
}
```

### Test HeadersUnary method

This method returns all request metadata (useful for verifying header injection from policies):

```bash
kubectl run grpcurl --rm -it --image=mirror.gcr.io/fullstorydev/grpcurl:latest --restart=Never -- \
  -plaintext -H 'x-custom-header: test-value' \
  grpcbin.default.svc.cluster.local:9000 \
  grpcbin.GRPCBin/HeadersUnary
```

## Available Methods

grpcbin provides several useful methods for testing:

| Method | Purpose |
|--------|---------|
| **DummyUnary** | Echoes back request payload - useful for basic connectivity testing |
| **HeadersUnary** | Returns all request metadata as a map - useful for verifying header injection from AuthPolicy |
| **SpecificError** | Returns a caller-specified gRPC status code - useful for testing error handling |

For a complete list of available methods, see [grpcb.in](https://grpcb.in/).

## Service Information

The grpcbin service exposes:

- **Service name:** `grpcbin.GRPCBin`
- **Methods:** Multiple test methods (see grpcb.in for full list)
- **Port:** 9000 (gRPC)
- **Protocol:** HTTP/2 (gRPC standard)

## Exposing via Gateway API

### Prerequisites

A Gateway must exist for the GRPCRoute to attach to. The GRPCRoute in this example references the `kuadrant-ingressgateway` Gateway in the `gateway-system` namespace, which is created by `make local-setup`.

### Deploy GRPCRoute

After deploying the grpcbin backend, expose it via Gateway API:

```bash
kubectl apply -f examples/grpc-backend/grpcroute.yaml -n default
```

This creates a GRPCRoute in the `default` namespace that routes traffic from `grpcbin.local` to the grpcbin service.

### Verify GRPCRoute is accepted

```bash
kubectl get grpcroute grpcbin -n default
```

Expected output:
```text
NAME      HOSTNAMES           AGE
grpcbin   ["grpcbin.local"]   10s
```

### Access via Gateway

Get the Gateway's LoadBalancer IP assigned by MetalLB and test grpcbin:

```bash
# Get the Gateway IP
export GATEWAY_IP=$(kubectl get svc -n gateway-system kuadrant-ingressgateway-istio -o jsonpath='{.status.loadBalancer.ingress[0].ip}')

# List available services
grpcurl -plaintext -authority grpcbin.local ${GATEWAY_IP}:80 list

# Call DummyUnary method
grpcurl -plaintext -authority grpcbin.local -d '{"f_string":"hello"}' ${GATEWAY_IP}:80 grpcbin.GRPCBin/DummyUnary
```

Expected output:
```json
{
  "f_string": "hello"
}
```

## Rate Limiting gRPC Traffic

Kuadrant's `RateLimitPolicy` can be applied to GRPCRoutes to rate limit gRPC traffic. See the [ratelimitpolicy directory](./ratelimitpolicy/README.md) for examples of:

- Basic rate limiting for all gRPC methods
- Method-specific rate limits using CEL expressions
- Combining rate limiting with authentication

Example:

```bash
kubectl apply -f examples/grpc-backend/ratelimitpolicy/ratelimitpolicy.yaml -n default
```

This applies a rate limit of 10 requests per 10 seconds to all traffic on the grpcbin GRPCRoute.

