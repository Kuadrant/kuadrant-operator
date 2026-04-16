# Egress Gateway Setup and Policies

This guide covers setting up an Istio egress gateway with Kuadrant and applying rate limiting, workload identity, and authentication policies to outbound traffic. For routing approaches (internal hostname, CoreDNS), see the [DNS Routing](dns-routing.md) guide. For credential injection via Vault, see the [Credential Injection](credential-injection.md) guide.

## Prerequisites

- Kubernetes cluster with Kuadrant operator and Istio installed. See the [Getting Started](/latest/getting-started) guide.

Set up the egress gateway infrastructure:

```sh
curl -sL https://raw.githubusercontent.com/Kuadrant/kuadrant-operator/refs/heads/main/hack/setup-egress.sh | bash
```

This deploys a Gateway (`kuadrant-egressgateway`), ServiceEntry and DestinationRule for httpbin.org with TLS origination, an HTTPRoute, a test-client pod, and a dev Vault instance with Kubernetes auth. The examples in this guide use:

| Resource | Value |
|----------|-------|
| Gateway namespace | `gateway-system` |
| Gateway name | `kuadrant-egressgateway` |
| External service | `httpbin.org` |

Export the gateway address for use in examples:

```sh
export EGRESS_IP=$(kubectl get gtw kuadrant-egressgateway -n gateway-system \
    -o jsonpath='{.status.addresses[0].value}')
```

## Egress Gateway Resources

The setup script deployed the following resources.

### Gateway

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: kuadrant-egressgateway
  namespace: gateway-system
spec:
  gatewayClassName: istio
  listeners:
    - name: http
      port: 80
      protocol: HTTP
      allowedRoutes:
        namespaces:
          from: All
```

### ServiceEntry

Registers the external service in Istio's service registry, making the hostname routable:

```yaml
apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: httpbin-external
  namespace: gateway-system
spec:
  hosts:
    - httpbin.org
  ports:
    - number: 443
      name: https
      protocol: HTTPS
  location: MESH_EXTERNAL
  resolution: DNS
```

### DestinationRule

Configures TLS origination - the gateway establishes TLS to the external service:

```yaml
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: httpbin-external
  namespace: gateway-system
spec:
  host: httpbin.org
  trafficPolicy:
    tls:
      mode: SIMPLE
      sni: httpbin.org
```

### HTTPRoute

Routes traffic matching the external hostname through the gateway to the external service:

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: httpbin-external
  namespace: gateway-system
spec:
  parentRefs:
    - name: kuadrant-egressgateway
      namespace: gateway-system
  hostnames:
    - httpbin.org
  rules:
    - filters:
        - type: URLRewrite
          urlRewrite:
            hostname: httpbin.org
      backendRefs:
        - group: networking.istio.io
          kind: Hostname
          name: httpbin.org
          port: 443
```

The `Hostname` backend is provided by the ServiceEntry. The `URLRewrite` filter ensures the correct `Host` header reaches the external service.

## Rate Limiting

RateLimitPolicy works on egress using the same attachment model as ingress - the same `targetRef`, the same Limitador limits, the same WasmPlugin enforcement. One caveat: `source.address` cannot be used as a counter expression because it includes the ephemeral port, giving each connection its own bucket. Use [workload identity](#workload-identity) with `auth.identity.username` for per-workload limiting instead.

### Basic Egress Rate Limiting

A simple rate limit on all egress traffic through a route:

```sh
kubectl apply -f - <<'EOF'
apiVersion: kuadrant.io/v1
kind: RateLimitPolicy
metadata:
  name: egress-ratelimit
  namespace: gateway-system
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: httpbin-external
  limits:
    global:
      rates:
        - limit: 100
          window: 1m
EOF
```

This limits all traffic through the `httpbin-external` route to 100 requests per minute, regardless of which workload is sending.

Verify:

```sh
# Send requests until rate limited
kubectl exec test-client -n egress-test -- \
    curl -s -o /dev/null -w "%{http_code}" -H "Host: httpbin.org" http://${EGRESS_IP}/get
# 200 (until limit reached, then 429)
```

### Other Rate Limiting Patterns

Standard RateLimitPolicy patterns (per-route, gateway-level, conditional with `when` predicates, defaults/overrides) all work on egress. For other configurations, see:

| Pattern | Guide |
|---------|-------|
| Basic per-route rate limiting | [Simple RL for App Developers](../ratelimiting/simple-rl-for-app-developers.md) |
| Gateway-level rate limiting | [Gateway RL for Cluster Operators](../ratelimiting/gateway-rl-for-cluster-operators.md) |
| Per-identity with API keys | [Authenticated RL for App Developers](../ratelimiting/authenticated-rl-for-app-developers.md) |
| Per-identity with JWT + K8s RBAC | [Authenticated RL with JWTs and K8s AuthNZ](../ratelimiting/authenticated-rl-with-jwt-and-k8s-authnz.md) |

## Workload Identity

In egress, the clients are internal workloads. To identify which workload is making a request, use Kubernetes [TokenReview](https://kubernetes.io/docs/reference/kubernetes-api/authentication-resources/token-review-v1/) via AuthPolicy. By default, every pod has a ServiceAccount token mounted automatically - no API keys to distribute.

The workload sends its SA token in the `Authorization` header:

```sh
curl -H "Authorization: Bearer $(cat /var/run/secrets/kubernetes.io/serviceaccount/token)" \
     -H "Host: httpbin.org" http://${EGRESS_IP}/get
```

AuthPolicy validates the token and exposes the workload's identity (namespace, service account name) for use by other policies. Requests without a valid SA token are rejected with 401.

The following AuthPolicy authenticates workloads and restricts egress access to specific namespaces:

```sh
kubectl apply -f - <<'EOF'
apiVersion: kuadrant.io/v1
kind: AuthPolicy
metadata:
  name: workload-identity
  namespace: gateway-system
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: httpbin-external
  rules:
    authentication:
      "workload-sa":
        kubernetesTokenReview:
          audiences:
            - "https://kubernetes.default.svc.cluster.local"
    authorization:
      "allowed-namespaces":
        patternMatching:
          patterns:
            - predicate: auth.identity.user.username.startsWith('system:serviceaccount:egress-test:')
EOF
```

- `authentication.workload-sa` - validates the SA token via TokenReview. Unauthenticated requests are rejected (401).
- `authorization.allowed-namespaces` - restricts access to workloads from the `egress-test` namespace. Workloads from other namespaces are rejected (403). Add additional patterns with `||` to allow more namespaces.

Verify access control:

```sh
# Authorized workload - 200
kubectl exec test-client -n egress-test -- sh -c '
curl -s -o /dev/null -w "%{http_code}" \
    -H "Host: httpbin.org" \
    -H "Authorization: Bearer $(cat /var/run/secrets/kubernetes.io/serviceaccount/token)" \
    http://'"${EGRESS_IP}"'/get
'

# No token - 401
kubectl exec test-client -n egress-test -- \
    curl -s -o /dev/null -w "%{http_code}" -H "Host: httpbin.org" http://${EGRESS_IP}/get

# Unauthorized namespace - 403
kubectl run bad-client --image=curlimages/curl:latest -n default --restart=Never \
    --command -- sleep infinity
kubectl wait --for=condition=Ready pod/bad-client -n default --timeout=30s
kubectl exec bad-client -n default -- sh -c '
curl -s -o /dev/null -w "%{http_code}" -H "Host: httpbin.org" \
    -H "Authorization: Bearer $(cat /var/run/secrets/kubernetes.io/serviceaccount/token)" \
    http://'"${EGRESS_IP}"'/get
'
kubectl delete pod bad-client -n default
```

### Per-Workload Rate Limiting

With workload identity established, you can give each workload its own rate limit bucket by adding an RLP that uses the SA username as a counter:

```sh
kubectl apply -f - <<'EOF'
apiVersion: kuadrant.io/v1
kind: RateLimitPolicy
metadata:
  name: egress-per-workload
  namespace: gateway-system
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: httpbin-external
  limits:
    per-workload:
      rates:
        - limit: 10
          window: 1m
      counters:
        - expression: "auth.identity.username"
EOF
```

Each ServiceAccount gets an independent 10 req/min bucket. `system:serviceaccount:team-a:default` and `system:serviceaccount:team-b:default` are tracked separately. The identity is stable across pod restarts and reschedules.

Workloads must include their SA token in requests:

```sh
kubectl exec test-client -n egress-test -- sh -c '
curl -s -o /dev/null -w "%{http_code}" \
    -H "Host: httpbin.org" \
    -H "Authorization: Bearer $(cat /var/run/secrets/kubernetes.io/serviceaccount/token)" \
    http://'"${EGRESS_IP}"'/get
'
```

The resolved identity contains:

| Field | Example value | Access path (AuthPolicy selectors) | Access path (RLP expressions) |
|-------|--------------|-----------------------------------|-------------------------------|
| Username | `system:serviceaccount:egress-test:default` | `auth.identity.user.username` | `auth.identity.username` |
| UID | `425214f1-cf45-...` | `auth.identity.user.uid` | `auth.identity.uid` |
| Groups | `[system:serviceaccounts, system:serviceaccounts:egress-test]` | `auth.identity.user.groups` | `auth.identity.groups` |

> **Note:** AuthPolicy selectors and RateLimitPolicy expressions resolve identity paths differently. AuthPolicy uses the full TokenReview structure (`auth.identity.user.username`), while RLP counter expressions use a flattened representation (`auth.identity.username`). This is by design - the two are evaluated by different engines (Authorino vs. wasm-shim).

## Credential Injection

Beyond the access control pattern above, the primary egress use case for AuthPolicy is **credential injection** - fetching external API credentials from a secret store (e.g., Vault) and injecting them into outbound requests.

See the [Credential Injection](credential-injection.md) guide for a full walkthrough covering:
- Vault integration with Kubernetes auth method
- Per-identity credential paths (`secret/egress/<namespace>/<sa-name>`)
- Two-layer security model (TokenReview + Vault authorization)

## Considerations

### ClusterIP vs LoadBalancer

By default, Istio provisions a **LoadBalancer** service for a Gateway. For egress gateways, this is typically unnecessary since traffic originates inside the cluster.

To use **ClusterIP** instead, add the `networking.istio.io/service-type` annotation to the Gateway:

```yaml
metadata:
  annotations:
    networking.istio.io/service-type: ClusterIP
```

| Service Type | Use Case |
|-------------|----------|
| ClusterIP | Standard egress - traffic originates within the cluster. |
| LoadBalancer | Multi-cluster egress - workloads in other clusters need to reach this gateway. |

### Application Must Send HTTP

For Kuadrant policies to inspect request headers and apply rate limiting, the application must send plain HTTP to the egress gateway. The DestinationRule handles TLS origination to the external service. If the application sends HTTPS, the gateway cannot inspect the encrypted traffic.

### Resource Ownership

| Resource | Managed by | Purpose |
|----------|-----------|---------|
| Gateway | User | Egress gateway infrastructure |
| ServiceEntry | User | Registers external service in Istio |
| DestinationRule | User | TLS origination configuration |
| HTTPRoute | User | Routes traffic to external service |
| AuthPolicy, RateLimitPolicy | User | Policy definitions |
| WasmPlugin, EnvoyFilter | Kuadrant (auto-generated) | Policy enforcement in the data plane |
| AuthConfig, Limitador Limits | Kuadrant (auto-generated) | Policy decisions consumed by Authorino and Limitador |

### Limitations

- **Istio only** - Egress gateway support targets Istio as the Gateway API provider. Envoy Gateway is not supported for egress at this time.
- **ServiceEntry and DestinationRule are user-managed** - Kuadrant does not create or manage these Istio resources.

## References

- [RFC 0016: Egress Gateway](https://github.com/Kuadrant/architecture/blob/main/rfcs/0016-egress-gateway.md)
- [DNS Routing Guide](dns-routing.md)
- [Credential Injection Guide](credential-injection.md)
- [RateLimitPolicy Overview](../../overviews/rate-limiting.md)
- [AuthPolicy Overview](../../overviews/auth.md)
- [Istio Egress Gateway](https://istio.io/latest/docs/tasks/traffic-management/egress/egress-gateway/)
