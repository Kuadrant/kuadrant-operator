# DNS-Based Routing to an Egress Gateway

This guide explains how to route pod traffic through an Istio egress gateway to external services using DNS configuration. It presents a recommended approach and an alternative for different trade-offs.

> **Note:** This guide is part of the [Egress Gateway epic](https://github.com/Kuadrant/architecture/issues/145) and will evolve as related work progresses — including the [egress gateway test environment](https://github.com/Kuadrant/kuadrant-operator/issues/1799), [credential injection](https://github.com/Kuadrant/kuadrant-operator/issues/1800), and [end-to-end documentation](https://github.com/Kuadrant/kuadrant-operator/issues/1804).

**Related:**
- [RFC 0016 — Egress Gateway Support](https://github.com/Kuadrant/architecture/blob/main/rfcs/0016-egress-gateway.md)
- [Existing guide: Using Gateway API with APIs outside the cluster](../misc/external-api.md)

## Prerequisites

This guide assumes the following are already running in the cluster:

- **Istio** installed as the Gateway API provider
- **Kuadrant operator** installed and the `Kuadrant` CR created
- **Egress gateway** deployed — a Gateway API `Gateway` resource with `gatewayClassName: istio`

When Istio processes the Gateway, it creates a Deployment and a Service. The examples in this guide use:

| Resource | Value |
|----------|-------|
| Gateway namespace | `gateway-system` |
| Gateway name | `egress-gateway` |
| Istio-created Service | `egress-gateway-istio.gateway-system.svc.cluster.local` |
| External service | `httpbin.org` |

> For egress gateway environment setup, see [Egress Gateway Test Environment](https://github.com/Kuadrant/kuadrant-operator/issues/1799).

## Egress Gateway Setup

Both approaches share common Istio resources that register the external service and configure TLS origination. These are created by the user alongside the Gateway.

### Gateway

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: egress-gateway
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

Configures TLS origination — the gateway establishes TLS to the external service:

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

An HTTPRoute attached to the egress gateway routes traffic to the external service. The exact HTTPRoute configuration depends on the routing approach — see each approach section below for the specific HTTPRoute definition.

Kuadrant policies (AuthPolicy, RateLimitPolicy) attach to the Gateway or HTTPRoute in the same way as for ingress.

---

## Recommended: Internal Hostname

The application uses the egress gateway's cluster-internal service name as the hostname. The gateway rewrites the host header and routes the request to the real external service with TLS origination.

This is the recommended approach because it requires zero DNS configuration, works on any platform, and has the simplest TLS model.

### How It Works

```text
Application: curl http://egress-gateway-istio.gateway-system.svc.cluster.local/get
    -> Kubernetes DNS resolves to egress gateway ClusterIP
    -> Egress gateway receives plain HTTP request
    -> HTTPRoute matches, URLRewrite filter sets Host header to httpbin.org
    -> DestinationRule initiates TLS to httpbin.org
    -> External service receives the request over HTTPS and responds
```

### DNS Configuration

No additional DNS configuration is needed. When Istio provisions the egress gateway, it creates a Kubernetes Service named `egress-gateway-istio` in the gateway's namespace. This service is automatically resolvable by any pod in the cluster via Kubernetes DNS:

```text
egress-gateway-istio.gateway-system.svc.cluster.local
```

If a friendlier hostname is preferred (e.g., `httpbin-egress`), create an ExternalName Service that aliases the gateway service:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: httpbin-egress
  namespace: app-namespace
spec:
  type: ExternalName
  externalName: egress-gateway-istio.gateway-system.svc.cluster.local
```

Applications in `app-namespace` can then use `http://httpbin-egress/get`.

### HTTPRoute with Host Rewrite

The HTTPRoute matches on the internal gateway hostname and rewrites the `Host` header to the real external hostname before forwarding:

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: httpbin-internal-hostname
  namespace: gateway-system
spec:
  parentRefs:
    - name: egress-gateway
      namespace: gateway-system
  hostnames:
    - egress-gateway-istio.gateway-system.svc.cluster.local
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

The `URLRewrite` filter changes the `Host` header from the internal hostname to `httpbin.org`, and the `Hostname` backend (provided by Istio's ServiceEntry) routes the request to the real external service.

> **Note:** The Gateway listener does not restrict by hostname, so it accepts traffic on any host, including the internal service name.

### TLS Considerations

This approach has the simplest TLS model:

1. **App to gateway:** Plain HTTP. The application sends unencrypted HTTP to the egress gateway's ClusterIP. Traffic stays within the cluster network.
2. **Gateway to external service:** TLS origination. The DestinationRule establishes a TLS connection to the external service with the appropriate SNI.

The application does not need to handle TLS certificates for the external service at all.

### Validated Result

Tested by sending a request from a pod in the `default` namespace:

```bash
kubectl exec test-client -n default -- \
  curl -s http://egress-gateway-istio.gateway-system.svc.cluster.local/get
```

Response confirms the flow works end-to-end:

```json
{
  "headers": {
    "Host": "httpbin.org",
    "X-Envoy-Original-Host": "egress-gateway-istio.gateway-system.svc.cluster.local",
    "X-Envoy-Decorator-Operation": "httpbin.org:443/*"
  },
  "url": "https://httpbin.org/get"
}
```

- `Host: httpbin.org` — URLRewrite correctly rewrote the host header
- `X-Envoy-Original-Host` — preserves the original internal hostname
- `url: https://httpbin.org/get` — TLS origination happened (app sent HTTP, gateway upgraded to HTTPS)

---

## Alternative: Pod DNS with Kuadrant CoreDNS

The application uses the real external hostname (`httpbin.org`) transparently. Workload pods are configured with a custom `dnsConfig` that uses a kuadrant CoreDNS instance as their nameserver. A DNSPolicy on the egress gateway creates DNS records that resolve the external hostname to the gateway IP.

Use this approach when the application cannot be changed to use an internal hostname.

### How It Works

```text
Application: curl http://httpbin.org/get
    -> Pod dnsConfig directs query to kuadrant CoreDNS
    -> Kuadrant CoreDNS resolves httpbin.org to egress gateway IP
    -> Egress gateway receives HTTP request with Host: httpbin.org
    -> HTTPRoute matches on httpbin.org
    -> DestinationRule initiates TLS to httpbin.org
    -> External service responds
```

Split DNS is handled by design: workload pods use kuadrant CoreDNS (which returns the gateway IP), while the gateway pod uses standard cluster DNS (which returns real external IPs). These separate resolution paths prevent routing loops.

### Prerequisites

In addition to the [common prerequisites](#prerequisites), this approach requires:

- **dns-operator** installed (included with Kuadrant)
- **Kuadrant CoreDNS** deployed — see the [CoreDNS integration guide](https://github.com/kuadrant/dns-operator/blob/main/docs/coredns/coredns-integration.md) for deployment instructions

### CoreDNS Configuration

The kuadrant CoreDNS instance needs a zone block for the external hostname and a forward block for all other queries:

```text
httpbin.org {
    kuadrant
}
. {
    forward . <cluster-dns-ip>
}
```

Replace `<cluster-dns-ip>` with your cluster's DNS service IP:

- **Kubernetes:** `kubectl get svc kube-dns -n kube-system -o jsonpath='{.spec.clusterIP}'`
- **OpenShift:** `kubectl get svc dns-default -n openshift-dns -o jsonpath='{.spec.clusterIP}'`

The IP depends on your cluster's service subnet configuration — do not assume a default value.

The `httpbin.org` zone block serves egress records via the kuadrant plugin. The `.` block forwards all other queries (including `*.svc.cluster.local`) to the cluster DNS, so pods can still resolve cluster-internal services.

Add one zone block per external hostname that should be routed through the egress gateway.

### Gateway Listener Hostname

For this approach, the egress gateway listener must specify the external hostname. DNSPolicy reads listener hostnames to create DNS records:

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: egress-gateway
  namespace: gateway-system
spec:
  gatewayClassName: istio
  listeners:
    - name: httpbin
      port: 80
      protocol: HTTP
      hostname: httpbin.org
      allowedRoutes:
        namespaces:
          from: All
```

Add one listener per external hostname that should be routed through the egress gateway.

### Provider Secret and DNSPolicy

Create a CoreDNS provider secret with the external hostname as the zone:

```bash
kubectl create secret generic dns-provider-credentials-coredns \
  --namespace=gateway-system \
  --type=kuadrant.io/coredns \
  --from-literal=ZONES="httpbin.org"
```

Create a DNSPolicy targeting the egress gateway. DNSPolicy reads the listener hostnames and the gateway's status address, and automatically creates a DNSRecord mapping `httpbin.org` to the gateway IP:

```yaml
apiVersion: kuadrant.io/v1
kind: DNSPolicy
metadata:
  name: egress-dns
  namespace: gateway-system
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: egress-gateway
  providerRefs:
    - name: dns-provider-credentials-coredns
```

The dns-operator processes the resulting DNSRecord and publishes it to the kuadrant CoreDNS instance, which serves it to pods.

### HTTPRoute

The HTTPRoute matches on the real external hostname directly — no rewrite is needed since the app sends the correct `Host` header:

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: httpbin-egress-route
  namespace: gateway-system
spec:
  parentRefs:
    - name: egress-gateway
      namespace: gateway-system
  hostnames:
    - httpbin.org
  rules:
    - backendRefs:
        - group: networking.istio.io
          kind: Hostname
          name: httpbin.org
          port: 443
```

### Workload Pod Configuration

Configure workload pods with `dnsPolicy: None` and a `dnsConfig` pointing exclusively to the kuadrant CoreDNS instance:

```yaml
spec:
  dnsPolicy: None
  dnsConfig:
    nameservers:
      - "<kuadrant-coredns-ip>"
    searches:
      - <namespace>.svc.cluster.local
      - svc.cluster.local
      - cluster.local
```

Replace `<kuadrant-coredns-ip>` with the kuadrant CoreDNS service ClusterIP:

```bash
kubectl get svc kuadrant-coredns -n kuadrant-coredns -o jsonpath='{.spec.clusterIP}'
```

> **Important:** Use only the kuadrant CoreDNS IP as the nameserver. Do not add the cluster DNS as a second nameserver — some resolver implementations (notably musl libc, used by Alpine-based images) send queries to all nameservers simultaneously and use whichever responds first, which can bypass the egress routing. The kuadrant CoreDNS forwards non-egress queries to the cluster DNS via the `.` forward block, so all DNS resolution continues to work.

### TLS Considerations

This approach has the same TLS model as the internal hostname approach:

1. **App to gateway:** Plain HTTP. The application sends unencrypted HTTP. DNS resolves the external hostname to the gateway IP within the cluster.
2. **Gateway to external service:** TLS origination. The DestinationRule establishes a TLS connection to the external service.

The application must send HTTP (not HTTPS) for Kuadrant policies to inspect request headers. If the application sends HTTPS, the gateway cannot terminate TLS (it does not hold a certificate for the external hostname), and policies cannot inspect the encrypted traffic.

### Validated Result

Tested by creating a pod with custom dnsConfig and sending a request using the real hostname:

```bash
kubectl exec egress-dns-test -- curl -s http://httpbin.org/get
```

Response confirms the traffic went through the egress gateway:

```json
{
  "headers": {
    "Host": "httpbin.org",
    "X-Envoy-Decorator-Operation": "httpbin.org:443/*",
    "X-Envoy-Peer-Metadata-Id": "router~...egress-gateway-istio...gateway-system..."
  },
  "url": "https://httpbin.org/get"
}
```

- `X-Envoy-Decorator-Operation: httpbin.org:443/*` — routed through the egress gateway
- `X-Envoy-Peer-Metadata-Id: ...egress-gateway-istio...` — response came from the egress gateway pod
- `url: https://httpbin.org/get` — TLS origination happened at the gateway

Cluster DNS resolution continues to work through the forward block:

```bash
kubectl exec egress-dns-test -- nslookup kubernetes.default.svc.cluster.local
# Address: 10.96.0.1
```

---

## Other Alternatives

### hostAliases

Kubernetes pods support [`hostAliases`](https://kubernetes.io/docs/tasks/network/customize-hosts-file-for-pods/) to add entries to `/etc/hosts`. This can map an external hostname to the egress gateway IP per pod:

```yaml
spec:
  hostAliases:
  - ip: "<egress-gateway-ip>"
    hostnames:
    - "httpbin.org"
```

This is simple and works on any platform, but the gateway IP is hardcoded in every pod spec. If the gateway service is recreated and gets a new IP, all pods need redeployment. The kuadrant CoreDNS approach avoids this problem since DNSRecord updates propagate automatically.

### Sidecar-Based Routing

If workloads are already enrolled in the Istio service mesh with sidecar injection, traffic can be routed through the egress gateway using a VirtualService with the `mesh` gateway. The sidecar proxy intercepts outbound connections and routes them to the egress gateway transparently — no DNS changes needed. This is a native Istio pattern but requires mesh membership, which adds resource overhead per pod and conflicts with the [RFC 0016](https://github.com/Kuadrant/architecture/blob/main/rfcs/0016-egress-gateway.md) goal of not requiring a service mesh. Consider this option only if your workloads already use sidecar injection.

---

## Considerations

### ClusterIP vs LoadBalancer for Egress Gateway

By default, Istio provisions a **LoadBalancer** service for a Gateway, which allocates an external IP. For egress gateways, this is typically unnecessary since traffic originates inside the cluster.

To use **ClusterIP** instead, add the `networking.istio.io/service-type` annotation to the Gateway:

```yaml
metadata:
  annotations:
    networking.istio.io/service-type: ClusterIP
```

| Service Type | Use Case |
|-------------|----------|
| ClusterIP | Standard egress — traffic originates within the cluster. No external IP provisioned. |
| LoadBalancer | Multi-cluster egress — workloads in other clusters need to reach this egress gateway via an external IP. |

### Applying Kuadrant Policies

AuthPolicy and RateLimitPolicy attach to the egress Gateway or HTTPRoute the same way as for ingress. The policies are separate CRDs that reference the target resource via `targetRef` — they do not modify the HTTPRoute.

```yaml
apiVersion: kuadrant.io/v1
kind: RateLimitPolicy
metadata:
  name: httpbin-egress-ratelimit
  namespace: gateway-system
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: httpbin-egress-route
  limits:
    global:
      rates:
        - limit: 100
          window: 1m
```

```yaml
apiVersion: kuadrant.io/v1
kind: AuthPolicy
metadata:
  name: httpbin-egress-auth
  namespace: gateway-system
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: httpbin-egress-route
  rules:
    authorization:
      deny-all:
        opa:
          rego: "allow = false"
    response:
      success:
        headers:
          x-egress-authorized:
            plain:
              value: "true"
```

Policies can also target the Gateway directly to apply to all routes on the egress gateway.

> **Note:** For the internal hostname approach, the policy targets the HTTPRoute that matches the internal hostname. For the pod DNS approach, the policy targets the HTTPRoute that matches the external hostname. The policy attachment mechanism is the same in both cases.

### Resource Ownership

All routing resources in this guide are **user-managed**:

| Resource | Managed by | Purpose |
|----------|-----------|---------|
| Gateway | User | Egress gateway infrastructure |
| ServiceEntry | User | Registers external service in Istio |
| DestinationRule | User | TLS origination configuration |
| HTTPRoute | User | Routes traffic to external service |
| DNSPolicy | User | Creates DNS records mapping external hostname to gateway IP (pod DNS approach only) |
| AuthPolicy, RateLimitPolicy | User | Policy definitions attached to Gateway/HTTPRoute |
| WasmPlugin, EnvoyFilter | Kuadrant (auto-generated) | Policy enforcement in the data plane |
| AuthConfig, Limitador Limits | Kuadrant (auto-generated) | Policy decisions consumed by Authorino and Limitador |

### Limitations

- **Istio only** — Egress gateway support targets Istio as the Gateway API provider. Envoy Gateway is not supported for egress at this time.
- **ServiceEntry and DestinationRule are user-managed** — Kuadrant does not create or manage these Istio resources. Users must configure external service registration and TLS origination manually.
- **Credential injection is covered separately** — Injecting API keys or tokens into outbound requests via AuthPolicy is an investigation item tracked in [Kuadrant/kuadrant-operator#1800](https://github.com/Kuadrant/kuadrant-operator/issues/1800).
- **Application must send HTTP** — For Kuadrant policies to inspect request headers and apply rate limiting, the application must send plain HTTP to the egress gateway. If the application sends HTTPS, either TLS passthrough is needed (policies cannot inspect headers) or a more complex TLS re-origination setup is required.
