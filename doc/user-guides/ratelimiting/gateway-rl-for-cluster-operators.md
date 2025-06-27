# Gateway Rate Limiting for Cluster Operators

For more info on the different personas see [Gateway API](https://gateway-api.sigs.k8s.io/concepts/roles-and-personas/#key-roles-and-personas) 

This tutorial walks you through an example of how to configure rate limiting for all routes attached to a specific ingress gateway.

## Prerequisites

- Kubernetes cluster with Kuadrant operator installed. See our [Getting Started](/latest/getting-started) guide for more information.
- [kubectl](https://kubernetes.io/docs/tasks/tools/#kubectl) command line tool.

### Deploy the Toystore example API:

```sh
kubectl apply -f https://raw.githubusercontent.com/Kuadrant/Kuadrant-operator/main/examples/toystore/toystore.yaml

```

### Create the ingress gateways

```sh
kubectl -n gateway-system apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: external
  annotations:
    kuadrant.io/namespace: kuadrant-system
    networking.istio.io/service-type: ClusterIP
spec:
  gatewayClassName: istio
  listeners:
  - name: external
    port: 80
    protocol: HTTP
    hostname: '*.io'
    allowedRoutes:
      namespaces:
        from: All
---
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: internal
  annotations:
    kuadrant.io/namespace: kuadrant-system
    networking.istio.io/service-type: ClusterIP
spec:
  gatewayClassName: istio
  listeners:
  - name: local
    port: 80
    protocol: HTTP
    hostname: '*.local'
    allowedRoutes:
      namespaces:
        from: All
EOF
```

### Enforce rate limiting on requests incoming through the `external` gateway

```
    ┌───────────┐      ┌───────────┐
    │ (Gateway) │      │ (Gateway) │
    │  external │      │  internal │
    │           │      │           │
    │   *.io    │      │  *.local  │
    └───────────┘      └───────────┘
          ▲
          │
┌─────────┴─────────┐
│ (RateLimitPolicy) │
│       gw-rlp      │
└───────────────────┘
```

Create a Kuadrant `RateLimitPolicy` to configure rate limiting:

```sh
kubectl apply -n gateway-system -f - <<EOF
apiVersion: kuadrant.io/v1
kind: RateLimitPolicy
metadata:
  name: gw-rlp
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: external
  limits:
    "global":
      rates:
      - limit: 5
        window: 10s
EOF
```

> **Note:** It may take a couple of minutes for the RateLimitPolicy to be applied depending on your cluster.

### Deploy a sample API to test rate limiting enforced at the level of the gateway

```
                           ┌───────────┐      ┌───────────┐
┌───────────────────┐      │ (Gateway) │      │ (Gateway) │
│ (RateLimitPolicy) │      │  external │      │  internal │
│       gw-rlp      ├─────►│           │      │           │
└───────────────────┘      │   *.io    │      │  *.local  │
                           └─────┬─────┘      └─────┬─────┘
                                 │                  │
                                 └─────────┬────────┘
                                           │
                                 ┌─────────┴────────┐
                                 │   (HTTPRoute)    │
                                 │     toystore     │
                                 │                  │
                                 │ *.toystore.io    │
                                 │ *.toystore.local │
                                 └────────┬─────────┘
                                          │
                                   ┌──────┴───────┐
                                   │   (Service)  │
                                   │   toystore   │
                                   └──────────────┘
```

### Route traffic to the API from both gateways:

```sh
kubectl apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: toystore
spec:
  parentRefs:
  - name: external
    namespace: gateway-system
  - name: internal
    namespace: gateway-system
  hostnames:
  - "*.toystore.io"
  - "*.toystore.local"
  rules:
  - backendRefs:
    - name: toystore
      port: 80
EOF
```

### Verify the rate limiting works by sending requests in a loop

Expose the gateways, respectively at the port numbers `9081` and `9082` of the local host:

```sh
kubectl port-forward -n gateway-system service/external-istio 9081:80 >/dev/null 2>&1 &
kubectl port-forward -n gateway-system service/internal-istio 9082:80 >/dev/null 2>&1 &
```

Up to 5 successful (`200 OK`) requests every 10 seconds through the `external` ingress gateway (`*.io`), then `429 Too Many Requests`:

```sh
while :; do curl --write-out '%{http_code}\n' --silent --output /dev/null -H 'Host: api.toystore.io' http://localhost:9081 | grep -E --color "\b(429)\b|$"; sleep 1; done
```

Unlimited successful (`200 OK`) through the `internal` ingress gateway (`*.local`):

```sh
while :; do curl --write-out '%{http_code}\n' --silent --output /dev/null -H 'Host: api.toystore.local' http://localhost:9082 | grep -E --color "\b(429)\b|$"; sleep 1; done
```
