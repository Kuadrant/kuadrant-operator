## Gateway Rate Limit For Cluster Operators

This user guide shows how the kuadrant's control plane applies rate limit policy at
[Gateway API's Gateway](https://gateway-api.sigs.k8s.io/v1alpha2/references/spec/#gateway.networking.k8s.io/v1alpha2.Gateway)
level.

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
    - name: kuadrant-gwapi-gateway
      namespace: kuadrant-system
  hostnames: ["*.toystore.com"]
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: "/toy"
          method: GET
        - path:
            type: PathPrefix
            value: "/free"
          method: GET
        - path:
            type: Exact
            value: "/admin/toy"
          method: POST
      backendRefs:
        - name: toystore
          port: 80
EOF
```

### Check `toystore` HTTPRoute works

`GET /toy`: no rate limiting
```
while :; do curl --write-out '%{http_code}' --silent --output /dev/null -H "Host: api.toystore.com" http://localhost:9080/toy | egrep --color "\b(429)\b|$"; sleep 1; done
```

`POST /admin/toy`: no rate limiting
```
while :; do curl --write-out '%{http_code}' --silent --output /dev/null -H "Host: api.toystore.com" -X POST http://localhost:9080/admin/toy | egrep --color "\b(429)\b|$"; sleep 1; done
```

**Note**: This only works out of the box on linux environments. If not on linux,
you may need to forward ports

```bash
kubectl port-forward -n kuadrant-system service/kuadrant-gateway 9080:80
```

### Rate limiting `toystore` HTTPRoute traffic

![](https://i.imgur.com/2A9sXXs.png)

RateLimitPolicy applied for the `toystore` HTTPRoute.

| Endpoints | Rate Limits |
| ------------- | -----: |
| `POST /admin/toy` | **5** reqs / **10** secs (0.5 rps) |
| `GET /toy` | **8** reqs / **10** secs (0.8 rps) |
| `*` | **30** reqs / **10** secs (3.0 rps) |

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
        - paths: ["/admin/toy"]
          methods: ["POST"]
      configurations:
        - actions:
            - generic_key:
                descriptor_key: admin-operation
                descriptor_value: "1"
      limits:
        - conditions:
            - "admin-operation == 1"
          maxValue: 5
          seconds: 10
          variables: []
    - rules:
        - paths: ["/toy"]
          methods: ["GET"]
      configurations:
        - actions:
            - generic_key:
                descriptor_key: get-operation
                descriptor_value: "1"
      limits:
        - conditions:
            - "get-operation == 1"
          maxValue: 8
          seconds: 10
          variables: []
    - configurations:
        - actions:
            - generic_key:
                descriptor_key: toystore
                descriptor_value: "1"
      limits:
        - conditions: ["toystore == 1"]
          maxValue: 30
          seconds: 10
          variables: []
EOF
```

Validating the rate limit policy.

`GET /toy` @ **1** rps (expected to be rate limited @ **8** reqs / **10** secs (0.8 rps))
```
while :; do curl --write-out '%{http_code}' --silent --output /dev/null -H "Host: api.toystore.com" http://localhost:9080/toy | egrep --color "\b(429)\b|$"; sleep 1; done
```

`POST /admin/toy` @ **1** rps (expected to be rate limited @ **5** reqs / **10** secs (0.5 rps))
```
while :; do curl --write-out '%{http_code}' --silent --output /dev/null -H "Host: api.toystore.com" -X POST http://localhost:9080/admin/toy | egrep --color "\b(429)\b|$"; sleep 1; done
```

### Rate limiting Gateway traffic

![](https://i.imgur.com/0o3yQzP.png)

RateLimitPolicy applied for the Gateway.

| Policy | Rate Limits |
| ------------- | -----: |
| `POST /*` | **2** reqs / **10** secs (0.2 rps) |
| Per remote IP | **25** reqs / **10** secs (2.5 rps) |

```yaml
kubectl apply -f - <<EOF
---
apiVersion: apim.kuadrant.io/v1alpha1
kind: RateLimitPolicy
metadata:
  name: kuadrant-gw
  namespace: kuadrant-system
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: kuadrant-gwapi-gateway
  rateLimits:
    - rules:
      - methods: ["POST"]
      configurations:
        - actions:
            - generic_key:
                descriptor_key: expensive-op
                descriptor_value: "1"
      limits:
        - conditions: ["expensive-op == 1"]
          maxValue: 2
          seconds: 10
          variables: []
    - configurations:
        - actions:
            - remote_address: {}
      limits:
        - conditions: []
          maxValue: 25
          seconds: 10
          variables: ["remote_address"]
EOF
```

### Validating the rate limit policies (HTTPRoute and Gateway).

`GET /toy` @ **1** rps (expected to be rate limited @ **8** reqs / **10** secs (0.8 rps))
```
while :; do curl --write-out '%{http_code}' --silent --output /dev/null -H "Host: api.toystore.com" http://localhost:9080/toy | egrep --color "\b(429)\b|$"; sleep 1; done
```

`POST /admin/toy` @ **1** rps (expected to be rate limited @ **2** reqs / **10** secs (0.2 rps))
```
while :; do curl --write-out '%{http_code}' --silent --output /dev/null -H "Host: api.toystore.com" -X POST http://localhost:9080/admin/toy | egrep --color "\b(429)\b|$"; sleep 1; done
```

### Validating Gateway "Per Remote IP" policy

Stop all traffic.

`GET /free` @ **3** rps (expected to be rate limited @ **25** reqs / **10** secs (2.5 rps))

```
while :; do curl --write-out '%{http_code}\n' --silent --output /dev/null -H "Host: api.toystore.com" http://localhost:9080/free -: --write-out '%{http_code}\n' --silent --output /dev/null -H "Host: api.toystore.com" http://localhost:9080/free -: --write-out '%{http_code}\n' --silent --output /dev/null -H "Host: api.toystore.com" http://localhost:9080/free | egrep --color "\b(429)\b|$"; sleep 1; done
```
