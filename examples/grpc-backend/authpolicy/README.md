# AuthPolicy for GRPCRoute

Examples demonstrating how to secure gRPC services using Kuadrant's AuthPolicy attached to a GRPCRoute.

## Prerequisites

- [grpcurl](https://github.com/fullstorydev/grpcurl) - Command-line tool for interacting with gRPC servers
- A Gateway must exist for the GRPCRoute to attach to. The GRPCRoute in this example references the `kuadrant-ingressgateway` Gateway in the `gateway-system` namespace, which is created by `make local-setup`.

Deploy the grpcbin backend and GRPCRoute first:

```bash
kubectl apply -f examples/grpc-backend/grpcbin.yaml
kubectl apply -f examples/grpc-backend/grpcroute.yaml
```

Obtain the Gateway IP:

```bash
export GATEWAY_IP=$(kubectl get svc -n gateway-system kuadrant-ingressgateway-istio -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
```

## Example 1: Basic API Key Authentication

### Deploy

```bash
kubectl apply -f examples/grpc-backend/authpolicy/kuadrant.yaml
kubectl apply -f examples/grpc-backend/authpolicy/api-key-secret.yaml
kubectl apply -f examples/grpc-backend/authpolicy/authpolicy.yaml
```

### Verify Policy Status

```bash
kubectl get authpolicy grpcbin -o wide
```

Wait for `Accepted` and `Enforced` conditions to be `True`.

### Test

**Without authentication (should fail):**

```bash
grpcurl -plaintext -authority grpcbin.local $GATEWAY_IP:80 list
```

Expected: `Code: Unauthenticated`

**With valid API key (should succeed):**

```bash
grpcurl -plaintext -authority grpcbin.local \
  -H "authorization: APIKEY GRPCBINKEY123" \
  $GATEWAY_IP:80 list
```

Expected: Lists available gRPC services

**Call a specific method:**

```bash
grpcurl -plaintext -authority grpcbin.local \
  -H "authorization: APIKEY GRPCBINKEY123" \
  -d '{"f_string": "hello"}' \
  $GATEWAY_IP:80 grpcbin.GRPCBin/DummyUnary
```

Expected: Echoes back the request

## Example 2: Conditional Authentication with 'when' Predicates

This example demonstrates using CEL predicates to selectively protect different gRPC methods. Unlike the basic example, which protects all methods equally, this policy applies **different authentication requirements** to different methods.

**Key concept:** Authentication rules only apply when their 'when' predicates match. If no rule matches a request, it's **denied**.

### Deploy

```bash
kubectl apply -f examples/grpc-backend/authpolicy/kuadrant.yaml
kubectl apply -f examples/grpc-backend/authpolicy/api-key-secret.yaml
kubectl apply -f examples/grpc-backend/authpolicy/admin-key-secret.yaml
kubectl apply -f examples/grpc-backend/authpolicy/authpolicy-when.yaml
```

### What This Policy Does

The `authpolicy-when.yaml` contains five rules with different requirements:

| Method(s) | Predicate Pattern | Required Credentials |
|-----------|-------------------|---------------------|
| **Reflection** (`/grpc.reflection.*`) | Service prefix | Standard **or** Admin key |
| **Index** | Method regex | Standard key only |
| **DummyUnary** | Exact method path | Admin key only |
| **HeadersUnary** | Method + header check | Standard key **and** `x-grpc-test: true` header |
| **Other methods** (e.g., Empty) | *No matching rule* | ❌ **Denied** |

### Test Success and Failure Scenarios

**1. Reflection with standard key (✓ succeeds):**

```bash
grpcurl -plaintext -authority grpcbin.local \
  -H "authorization: APIKEY GRPCBINKEY123" \
  $GATEWAY_IP:80 list
```

**2. Index method with standard key (✓ succeeds):**

```bash
grpcurl -plaintext -authority grpcbin.local \
  -H "authorization: APIKEY GRPCBINKEY123" \
  $GATEWAY_IP:80 grpcbin.GRPCBin/Index
```

**3. DummyUnary with standard key (✗ fails - needs admin key):**

```bash
grpcurl -plaintext -authority grpcbin.local \
  -H "authorization: APIKEY GRPCBINKEY123" \
  -d '{"f_string": "test"}' \
  $GATEWAY_IP:80 grpcbin.GRPCBin/DummyUnary
```

Expected: `Code: Unauthenticated`

**4. DummyUnary with admin key (✓ succeeds):**

```bash
grpcurl -plaintext -authority grpcbin.local \
  -H "authorization: ADMIN ADMINKEY456" \
  -d '{"f_string": "test"}' \
  $GATEWAY_IP:80 grpcbin.GRPCBin/DummyUnary
```

**5. HeadersUnary without required header (✗ fails):**

```bash
grpcurl -plaintext -authority grpcbin.local \
  -H "authorization: APIKEY GRPCBINKEY123" \
  $GATEWAY_IP:80 grpcbin.GRPCBin/HeadersUnary
```

Expected: `Code: Unauthenticated` (predicate requires both method AND header)

**6. HeadersUnary with required header (✓ succeeds):**

```bash
grpcurl -plaintext -authority grpcbin.local \
  -H "authorization: APIKEY GRPCBINKEY123" \
  -H "x-grpc-test: true" \
  $GATEWAY_IP:80 grpcbin.GRPCBin/HeadersUnary
```

**7. Unprotected method (✗ fails - no matching rule):**

```bash
grpcurl -plaintext -authority grpcbin.local \
  -H "authorization: APIKEY GRPCBINKEY123" \
  $GATEWAY_IP:80 grpcbin.GRPCBin/Empty
```

Expected: `Code: Unauthenticated` (no rule matches this method)

### CEL Predicate Patterns Demonstrated

These predicates use `request.url_path` to match gRPC method paths, which follow the format `/<service>/<method>`:

| Pattern Type | Example Predicate | Matches |
|--------------|-------------------|---------|
| **Service-level** | `request.url_path.startsWith('/grpc.reflection.')` | All methods on the reflection service |
| **Method-only** | `request.url_path.matches('^/[^/]+/Index$')` | Index method on any service |
| **Service+Method** | `request.url_path == '/grpcbin.GRPCBin/DummyUnary'` | Specific method on specific service |
| **Combined conditions** | Method match AND header check | Both predicates must be true |

**Note:** These predicates currently use `request.url_path` (the HTTP/2 `:path` pseudo-header). In the future, Authorino will support gRPC-specific well-known attributes like `request.grpc.service` and `request.grpc.method` for more natural matching. See [Kuadrant/authorino#584](https://github.com/Kuadrant/authorino/issues/584) for details.

## Cleanup

```bash
kubectl delete -f examples/grpc-backend/authpolicy/authpolicy.yaml
kubectl delete -f examples/grpc-backend/authpolicy/authpolicy-when.yaml
kubectl delete -f examples/grpc-backend/authpolicy/api-key-secret.yaml
kubectl delete -f examples/grpc-backend/authpolicy/admin-key-secret.yaml
kubectl delete -f examples/grpc-backend/authpolicy/kuadrant.yaml
```
