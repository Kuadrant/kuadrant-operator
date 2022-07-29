## Authenticated Rate Limit For API Owners

This user guide shows how to configure authenticated rate limiting.
Authenticated rate limiting allows to specify rate limiting configurations
based on the traffic owners, i.e. ID of the user owning the request.
Authentication method used will be the API key.

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

### Create API keys for user `Bob` and `Alice`

```yaml
kubectl apply -f -<<EOF
---
apiVersion: v1
kind: Secret
metadata:
  annotations:
    secret.kuadrant.io/user-id: bob
  name: bob-key
  labels:
    authorino.kuadrant.io/managed-by: authorino
    app: toystore
stringData:
  api_key: IAMBOB
type: Opaque
---
apiVersion: v1
kind: Secret
metadata:
  annotations:
    secret.kuadrant.io/user-id: alice
  name: alice-key
  labels:
    authorino.kuadrant.io/managed-by: authorino
    app: toystore
stringData:
  api_key: IAMALICE
type: Opaque
EOF
```

### Create Kuadrant's `AuthPolicy` to configure API key based authentication

```yaml
kubectl apply -f - <<EOF
---
apiVersion: apim.kuadrant.io/v1alpha1
kind: AuthPolicy
metadata:
  name: toystore
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: toystore
  rules:
  - hosts: ["*.toystore.com"]
    paths: ["/toy*"]
  authScheme:
    hosts: ["api.toystore.com"]
    identity:
    - name: friends
      apiKey:
        labelSelectors:
          app: toystore
      credentials:
        in: authorization_header
        keySelector: APIKEY
    response:
    - json:
        properties:
          - name: user-id
            value: null
            valueFrom:
              authJSON: auth.identity.metadata.annotations.secret\.kuadrant\.io/user-id
      name: rate-limit-apikey
      wrapper: envoyDynamicMetadata
      wrapperKey: ext_auth_data
EOF
```

### Create RateLimitPolicy to rate limit per API key basis

![](https://i.imgur.com/2A9sXXs.png)

| User | Rate Limits |
| ------------- | -----: |
| `Bob` | **2** reqs / **10** secs (0.2 rps) |
| `Alice` | **5** reqs / **10** secs (0.5 rps) |

```yaml
kubectl apply -f -<<EOF
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
  - configurations:
      - actions:
          - metadata:
              descriptor_key: "user-id"
              default_value: "no-user"
              metadata_key:
                key: "envoy.filters.http.ext_authz"
                path:
                  - segment:
                      key: "ext_auth_data"
                  - segment:
                      key: "user-id"
    limits:
      - conditions:
          - "user-id == bob"
        maxValue: 2
        seconds: 10
        variables: []
      - conditions:
          - "user-id == alice"
        maxValue: 5
        seconds: 10
        variables: []
EOF
```

### Validating the rate limit policy

Only 2 requests every 10 allowed for Bob.

```
curl -v -H 'Authorization: APIKEY IAMBOB' -H 'Host: api.toystore.com' http://localhost:9080/toy
```

Only 5 requests every 10 allowed for Alice.

```
curl -v -H 'Authorization: APIKEY IAMALICE' -H 'Host: api.toystore.com' http://localhost:9080/toy
```
