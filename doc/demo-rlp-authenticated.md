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

Create API key secrets for Authorino

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

Check `toystore` HTTPRoute requires API key

```
curl -v -H 'Authorization: APIKEY IAMBOB' -H 'Host: api.toystore.com' http://localhost:9080/toy
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
    - rules:
      - paths: ["/toy"]
        methods: ["GET"]
      configurations:
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
          seconds: 30
          variables: []
        - conditions:
            - "user-id == alice"
          maxValue: 4
          seconds: 30
          variables: []
EOF
```

Check the authenticated rate limit policy works: 2 requests every 30 secs.

```
curl -v -H 'Authorization: APIKEY IAMBOB' -H 'Host: api.toystore.com' http://localhost:9080/toy
```

```
curl -v -H 'Authorization: APIKEY IAMALICE' -H 'Host: api.toystore.com' http://localhost:9080/toy
```
