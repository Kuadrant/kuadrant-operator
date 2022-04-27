## Updating the RateLimitPolicy `targetRef` attribute

This demo shows how the kuadrant's controller applies the rate limit policy to the new HTTPRoute
object and cleans up rate limit configuration to the HTTPRoute object no longer referenced by the policy.

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

![](https://i.imgur.com/ykv86hV.png)

Rate limit `toystore` HTTPRoute

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
    - stage: PREAUTH
      actions:
        - generic_key:
            descriptor_key: vhaction
            descriptor_value: "yes"
  domain: toystore-app
  limits:
    - conditions: ["vhaction == yes"]
      max_value: 2
      namespace: toystore-app
      seconds: 5
      variables: []
EOF
```

Check the rate limit policy works: 2 requests every 5 secs.

```
curl -v -H 'Host: api.toystore.com' http://localhost:9080/toy
```

Add a second HTTPRoute: `carstore`

![](https://i.imgur.com/ruabBi3.png)

```yaml
kubectl apply -f - <<EOF
---
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: HTTPRoute
metadata:
  name: carstore
  labels:
    app: carstore
spec:
  parentRefs:
    - name: kuadrant-gwapi-gateway
      namespace: kuadrant-system
  hostnames: ["api.carstore.com"]
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: "/car"
          method: GET
      backendRefs:
        - name: toystore
          port: 80
EOF
```

Check `carstore` HTTPRoute works

```
curl -v -H 'Host: api.carstore.com' http://localhost:9080/car
```

Update RLP `targetRef` to the new HTTPRoute `carstore`

![](https://i.imgur.com/eu30Mry.png)

```
k edit ratelimitpolicy toystore
```

Check `toystore` HTTPRoute is no longer rate limited

```
curl -v -H 'Host: api.toystore.com' http://localhost:9080/toy
```

Check `carstore` HTTPRoute is rate limited

```
curl -v -H 'Host: api.carstore.com' http://localhost:9080/car
```

Remove the rate limit policy

```
k delete ratelimitpolicy toystore
```

Check `toystore` and `carstore` HTTPRoutes are no longer rate limited
