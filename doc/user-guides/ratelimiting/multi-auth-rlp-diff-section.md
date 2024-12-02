# Gateway Rate Limiting

This user guide walks you through an example of how to configure multiple rate limit polices for different listeners in an ingress gateway.

### Setup the environment

Follow this [setup doc](https://github.com/Kuadrant/kuadrant-operator/blob/main/doc/install/install-make-target.md) to set up your environment before continuing with this doc.

### Deploy the sample API:

```sh
kubectl apply -f examples/toystore/toystore.yaml
```

### Create the ingress gateways

```sh
kubectl -n kuadrant-system apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: environment
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
  - name: local
    port: 80
    protocol: HTTP
    hostname: '*.local'
    allowedRoutes:
      namespaces:
        from: All
EOF
```

### Route traffic to the API from both gateways listeners

```sh
kubectl apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: toystore
spec:
  parentRefs:
  - name: environment
    namespace: kuadrant-system
  hostnames:
  - "*.toystore.io"
  - "*.toystore.local"
  rules:
  - backendRefs:
    - name: toystore
      port: 80
EOF
```

### Create a Kuadrant `RateLimitPolicy` to configure rate limiting for the external listener:

```sh
kubectl apply -n kuadrant-system -f - <<EOF
apiVersion: kuadrant.io/v1beta3
kind: RateLimitPolicy
metadata:
  name: gw-rlp-external
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: environment
    sectionName: external
  defaults:
    strategy: merge  
    limits:
      "external":
        rates:
        - limit: 2
          window: 10s
EOF
```

### Create a Kuadrant `RateLimitPolicy` to configure rate limiting for the local listener:

```sh
kubectl apply -n kuadrant-system -f - <<EOF
apiVersion: kuadrant.io/v1beta3
kind: RateLimitPolicy
metadata:
  name: gw-rlp-local
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: environment
    sectionName: local
  defaults:
    strategy: merge    
    limits:
      "local":
        rates:
        - limit: 5
          window: 10s
EOF
```

> **Note:** It may take a couple of minutes for the RateLimitPolicy to be applied depending on your cluster.



### Verify the rate limiting works by sending requests in a loop

Expose the gateways, respectively at the port numbers `9081` and `9082` of the local host:

```sh
kubectl port-forward -n gateway-system service/environment-istio 9081:80 >/dev/null 2>&1 &
```

Up to 5 successful (`200 OK`) requests every 10 seconds through the `external` ingress gateway (`*.io`), then `429 Too Many Requests`:

```sh
while :; do curl --write-out '%{http_code}\n' --silent --output /dev/null -H 'Host: api.toystore.io' http://localhost:9081 | grep -E --color "\b(429)\b|$"; sleep 1; done
```

Unlimited successful (`200 OK`) through the `internal` ingress gateway (`*.local`):

```sh
while :; do curl --write-out '%{http_code}\n' --silent --output /dev/null -H 'Host: api.toystore.local' http://localhost:9081 | grep -E --color "\b(429)\b|$"; sleep 1; done
```

## Cleanup

```sh
make local-cleanup
```
