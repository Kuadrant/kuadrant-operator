# Simple Rate Limit For API Owners

This user guide shows how to configure rate limiting for one of the subdomains.

### Clone the project

```
git clone https://github.com/Kuadrant/kuadrant-controller
```

### Setup environment

This step creates a containerized Kubernetes server locally using [Kind](https://kind.sigs.k8s.io),
then it installs Istio, Kubernetes Gateway API and kuadrant.

```
make local-setup
```

### Deploy toystore example deployment

```
kubectl apply -f examples/toystore/toystore.yaml
```

### Create HTTPRoute to configure routing to the toystore service

![](https://i.imgur.com/rdN8lo3.png)

```yaml
kubectl apply -f - <<EOF
---
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: HTTPRoute
metadata:
  name: toystore
  labels:
    app: toystore
spec:
  parentRefs:
    - name: istio-ingressgateway
      namespace: istio-system
  hostnames: ["*.toystore.com"]
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: "/toy"
          method: GET
      backendRefs:
        - name: toystore
          port: 80
EOF
```

### Check `toystore` HTTPRoute works

```
curl -v -H 'Host: api.toystore.com' http://localhost:9080/toy
```

It should return `200 OK`.

**Note**: This only works out of the box on linux environments. If not on linux,
you may need to forward ports

```bash
kubectl port-forward -n kuadrant-system service/kuadrant-gateway 9080:80
```

### Create RateLimitPolicy for ratelimiting only for specific subdomain

![](https://i.imgur.com/2A9sXXs.png)


RateLimitPolicy applied for the `toystore` HTTPRoute.

| Hostname | Rate Limits |
| ------------- | -----: |
| `rate-limited.toystore.com` | **5** reqs / **10** secs (0.5 rps) |
| `*.toystore.com` | not rate limited |

```yaml
kubectl apply -f - <<EOF
---
apiVersion: apim.kuadrant.io/v1alpha1
kind: RateLimitPolicy
metadata:
  name: toystore
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: toystore
  rateLimits:
    - rules:
        - hosts: ["rate-limited.toystore.com"]
      configurations:
        - actions:
            - generic_key:
                descriptor_key: "limited"
                descriptor_value: "1"
      limits:
        - conditions:
            - "limited == 1"
          maxValue: 5
          seconds: 10
          variables: []
EOF
```

### Validating the rate limit policy

Only 5 requests every 10 secs on `rate-limited.toystore.com` allowed.

```
curl -v -H 'Host: rate-limited.toystore.com' http://localhost:9080/toy
```

Whereas `other.toystore.com` is not rate limited.

```
curl -v -H 'Host: other.toystore.com' http://localhost:9080/toy
```
