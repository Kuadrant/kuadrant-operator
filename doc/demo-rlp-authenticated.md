## Authenticated rate limiting

This demo shows how to configure rate limiting after authentication stage and rate limit configuration
is per API key basis.

### Steps

Create local cluster and deploy kuadrant

```
make local-setup
```

Deploy toystore example deployment

```
kubectl apply -f examples/toystore/toystore.yaml
```

Create `toystore` HTTPRoute to configure routing to the toystore service

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

Check `toystore` HTTPRoute works

```
curl -v -H 'Host: api.toystore.com' http://localhost:9080/toy
```

Create AuthPolicy

```
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
EOF
```

Create API key secrets for Authorino

```yaml
kubectl apply -f -<<EOF
---
apiVersion: v1
kind: Secret
metadata:
  name: key-a
  labels:
    authorino.kuadrant.io/managed-by: authorino
    api: toystore
stringData:
  api_key: KEY-A
type: Opaque
---
apiVersion: v1
kind: Secret
metadata:
  name: key-b
  labels:
    authorino.kuadrant.io/managed-by: authorino
    api: toystore
stringData:
  api_key: KEY-B
type: Opaque
EOF
```
Check `toystore` HTTPRoute requires API key

```
curl -v -H 'Authorization: APIKEY KEY-A' -H 'Host: api.toystore.com' http://localhost:9080/toy
```

Add rate limit policy to protect per API key basis


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
    - stage: POSTAUTH
      actions:
        - request_headers:
            descriptor_key: key
            header_name: "Authorization"
            skip_if_absent: true
  domain: toystore-app
  limits:
    - conditions: []
      max_value: 2
      namespace: toystore-app
      seconds: 30
      variables: ["key"]
EOF
```

Check the authenticated rate limit policy works: 2 requests every 30 secs.

```
curl -v -H 'Authorization: APIKEY KEY-A' -H 'Host: api.toystore.com' http://localhost:9080/toy
```

```
curl -v -H 'Authorization: APIKEY KEY-B' -H 'Host: api.toystore.com' http://localhost:9080/toy
```
