# DNS Routing for Egress Gateway

This guide covers routing approaches for directing pod traffic through an Istio egress gateway to external services. For egress gateway setup and Kuadrant policies, see the [Egress Gateway Setup](egress-gateway.md) guide.

## Prerequisites

- Egress gateway infrastructure deployed (see guide above).

The examples in this guide use:

| Resource | Value |
|----------|-------|
| Gateway namespace | `gateway-system` |
| Gateway name | `kuadrant-egressgateway` |
| Istio-created Service | `kuadrant-egressgateway-istio.gateway-system.svc.cluster.local` |
| External service | `httpbin.org` |

---

## Recommended: Internal Hostname

The application uses the egress gateway's cluster-internal service name as the hostname. The gateway rewrites the host header and routes the request to the real external service with TLS origination.

This is the recommended approach because it requires zero DNS configuration, works on any platform, and has the simplest TLS model.

### How It Works

```text
Application: curl http://kuadrant-egressgateway-istio.gateway-system.svc.cluster.local/get
    -> Kubernetes DNS resolves to egress gateway ClusterIP
    -> Egress gateway receives plain HTTP request
    -> HTTPRoute matches, URLRewrite filter sets Host header to httpbin.org
    -> DestinationRule initiates TLS to httpbin.org
    -> External service receives the request over HTTPS and responds
```

### DNS Configuration

No additional DNS configuration is needed. When Istio provisions the egress gateway, it creates a Kubernetes Service named `kuadrant-egressgateway-istio` in the gateway's namespace. Kubernetes automatically adds this service to the cluster DNS, so any pod can resolve it without additional configuration:

```bash
kubectl exec test-client -n egress-test -- nslookup kuadrant-egressgateway-istio.gateway-system.svc.cluster.local
# Name:      kuadrant-egressgateway-istio.gateway-system.svc.cluster.local
# Address:   <gateway-clusterip>
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
  externalName: kuadrant-egressgateway-istio.gateway-system.svc.cluster.local
```

Applications in `app-namespace` can then use `http://httpbin-egress/get`.

### HTTPRoute with Host Rewrite

This approach replaces the HTTPRoute deployed by the setup script. Instead of matching the external hostname, it matches the gateway's internal service name and rewrites the `Host` header to the real external hostname:

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: httpbin-internal-hostname
  namespace: gateway-system
spec:
  parentRefs:
    - name: kuadrant-egressgateway
      namespace: gateway-system
  hostnames:
    - kuadrant-egressgateway-istio.gateway-system.svc.cluster.local
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

Tested by sending a request from a pod in the `egress-test` namespace:

```bash
kubectl exec test-client -n egress-test -- \
  curl -s http://kuadrant-egressgateway-istio.gateway-system.svc.cluster.local/get
```

Response confirms the flow works end-to-end:

```json
{
  "headers": {
    "Host": "httpbin.org",
    "X-Envoy-Original-Host": "kuadrant-egressgateway-istio.gateway-system.svc.cluster.local",
    "X-Envoy-Decorator-Operation": "httpbin.org:443/*"
  },
  "url": "https://httpbin.org/get"
}
```

- `Host: httpbin.org` - URLRewrite correctly rewrote the host header
- `X-Envoy-Original-Host` - preserves the original internal hostname
- `url: https://httpbin.org/get` - TLS origination happened (app sent HTTP, gateway upgraded to HTTPS)

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
- **Kuadrant CoreDNS** deployed - see the [CoreDNS integration guide](https://github.com/kuadrant/dns-operator/blob/main/docs/coredns/coredns-integration.md) for deployment instructions

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

The IP depends on your cluster's service subnet configuration - do not assume a default value.

The `httpbin.org` zone block serves egress records via the kuadrant plugin. The `.` block forwards all other queries (including `*.svc.cluster.local`) to the cluster DNS, so pods can still resolve cluster-internal services.

Add one zone block per external hostname that should be routed through the egress gateway.

### Gateway Listener Hostname

For this approach, the egress gateway listener must specify the external hostname. DNSPolicy reads listener hostnames to create DNS records:

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: kuadrant-egressgateway
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
    name: kuadrant-egressgateway
  providerRefs:
    - name: dns-provider-credentials-coredns
```

The dns-operator processes the resulting DNSRecord and publishes it to the kuadrant CoreDNS instance, which serves it to pods.

### HTTPRoute

This approach uses the HTTPRoute deployed by the setup script as-is - it already matches `httpbin.org` and routes to the external service. No changes needed.

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

Replace `<kuadrant-coredns-ip>` with the kuadrant CoreDNS service ClusterIP. The `searches` entries should match your cluster's DNS domain (typically `cluster.local` - check `/etc/resolv.conf` in an existing pod if unsure).

```bash
kubectl get svc kuadrant-coredns -n kuadrant-coredns -o jsonpath='{.spec.clusterIP}'
```

> **Important:** Use only the kuadrant CoreDNS IP as the nameserver. Do not add the cluster DNS as a second nameserver - some resolver implementations (notably musl libc, used by Alpine-based images) send queries to all nameservers simultaneously and use whichever responds first, which can bypass the egress routing. The kuadrant CoreDNS forwards non-egress queries to the cluster DNS via the `.` forward block, so all DNS resolution continues to work.

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
    "X-Envoy-Peer-Metadata-Id": "router~...kuadrant-egressgateway-istio...gateway-system..."
  },
  "url": "https://httpbin.org/get"
}
```

- `X-Envoy-Decorator-Operation: httpbin.org:443/*` - routed through the egress gateway
- `X-Envoy-Peer-Metadata-Id: ...kuadrant-egressgateway-istio...` - response came from the egress gateway pod
- `url: https://httpbin.org/get` - TLS origination happened at the gateway

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

If workloads are already enrolled in the Istio service mesh with sidecar injection, traffic can be routed through the egress gateway using a VirtualService with the `mesh` gateway. The sidecar proxy intercepts outbound connections and routes them to the egress gateway transparently - no DNS changes needed. This is a native Istio pattern but requires mesh membership, which adds resource overhead per pod and conflicts with the [RFC 0016](https://github.com/Kuadrant/architecture/blob/main/rfcs/0016-egress-gateway.md) goal of not requiring a service mesh. Consider this option only if your workloads already use sidecar injection.

## References

- [Egress Gateway Setup](egress-gateway.md)
- [Credential Injection](credential-injection.md)
- [RFC 0016: Egress Gateway](https://github.com/Kuadrant/architecture/blob/main/rfcs/0016-egress-gateway.md)
- [CoreDNS Integration Guide](https://github.com/kuadrant/dns-operator/blob/main/docs/coredns/coredns-integration.md)
