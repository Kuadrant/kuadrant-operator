# PlanPolicy Demo

> [!CAUTION]
> The current implementation for the PlanPolicy is not the desired end-goal solution and only intended as a PoC

Kind cluster already configured with Kuadrant and toystore deployed

```sh
curl -H 'Host: api.toystore.com' http://$KUADRANT_GATEWAY_URL/toy -i
```

### AuthPolicy

```yaml
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1
kind: AuthPolicy
metadata:
  name: toystore
  namespace: ${KUADRANT_DEVELOPER_NS}
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: toystore
  rules:
    authentication:
      "api-key-plan":
        apiKey:
          selector:
            matchLabels:
              app: toystore
          allNamespaces: true
        credentials:
          authorizationHeader:
            prefix: APIKEY
EOF
```


### API Key secrets
```yaml
kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: gold-key
  labels:
    authorino.kuadrant.io/managed-by: authorino
    app: toystore
  annotations:
    secret.kuadrant.io/plan-id: gold
stringData:
  api_key: IAMGOLD
type: Opaque
---
apiVersion: v1
kind: Secret
metadata:
  name: silver-key
  labels:
    authorino.kuadrant.io/managed-by: authorino
    app: toystore
  annotations:
    secret.kuadrant.io/plan-id: silver
stringData:
  api_key: IAMSILVER
type: Opaque
---
apiVersion: v1
kind: Secret
metadata:
  name: bronze-key
  labels:
    authorino.kuadrant.io/managed-by: authorino
    app: toystore
  annotations:
    secret.kuadrant.io/plan-id: bronze
stringData:
  api_key: IAMBRONZE
type: Opaque
EOF
```

### PlanPolicy

```yaml
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1alpha1
kind: PlanPolicy
metadata:
  name: my-toystore-plan
  namespace: ${KUADRANT_DEVELOPER_NS}
spec:
  targetRef:
    group: kuadrant.io
    kind: AuthPolicy
    name: toystore
  plans:
    - tier: gold
      predicate: |
        has(auth.identity) && auth.identity.metadata.annotations["secret.kuadrant.io/plan-id"] == "gold"
      limits:
        daily: 5
    - tier: silver
      predicate: |
        has(auth.identity) && auth.identity.metadata.annotations["secret.kuadrant.io/plan-id"] == "silver"
      limits:
        daily: 2
    - tier: bronze
      predicate: |
        has(auth.identity)
      limits:
        daily: 1
EOF
```

### Test Bronze Limit
```sh
while :; do curl --write-out '%{http_code}\n' --silent --output /dev/null -H 'Authorization: APIKEY IAMBRONZE' -H 'Host: api.toystore.com' http://$KUADRANT_GATEWAY_URL/toy | grep -E --color "\b(429)\b|$"; sleep 1; done
```

### Test Silver Limit
```sh
while :; do curl --write-out '%{http_code}\n' --silent --output /dev/null -H 'Authorization: APIKEY IAMSILVER' -H 'Host: api.toystore.com' http://$KUADRANT_GATEWAY_URL/toy | grep -E --color "\b(429)\b|$"; sleep 1; done
```

### Test Gold Limit
```sh
while :; do curl --write-out '%{http_code}\n' --silent --output /dev/null -H 'Authorization: APIKEY IAMGOLD' -H 'Host: api.toystore.com' http://$KUADRANT_GATEWAY_URL/toy | grep -E --color "\b(429)\b|$"; sleep 1; done
```

### Check RateLimitPolicy
```yaml
kubectl get ratelimitpolicy -n toystore my-toystore-plan -o yaml
```

### Check AuthPolicy
```yaml
kubectl get authpolicy -n toystore toystore -o yaml
```

### PlanPolicy (updated)

```yaml
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1alpha1
kind: PlanPolicy
metadata:
  name: my-toystore-plan
  namespace: ${KUADRANT_DEVELOPER_NS}
spec:
  targetRef:
    group: kuadrant.io
    kind: AuthPolicy
    name: toystore
  plans:
    - tier: platinum
      predicate: |
        has(auth.identity) && auth.identity.metadata.annotations["secret.kuadrant.io/plan-id"] == "platinum"
      limits:
        daily: 10
    - tier: gold
      predicate: |
        has(auth.identity) && auth.identity.metadata.annotations["secret.kuadrant.io/plan-id"] == "gold"
      limits:
        daily: 5
    - tier: silver
      predicate: |
        has(auth.identity) && auth.identity.metadata.annotations["secret.kuadrant.io/plan-id"] == "silver"
      limits:
        daily: 2
    - tier: bronze
      predicate: |
        has(auth.identity)
      limits:
        daily: 1
---
apiVersion: v1
kind: Secret
metadata:
  name: platinum-key
  labels:
    authorino.kuadrant.io/managed-by: authorino
    app: toystore
  annotations:
    secret.kuadrant.io/plan-id: platinum
stringData:
  api_key: IAMPLATINUM
type: Opaque
EOF
```

### Test Platinum Limit
```sh
while :; do curl --write-out '%{http_code}\n' --silent --output /dev/null -H 'Authorization: APIKEY IAMPLATINUM' -H 'Host: api.toystore.com' http://$KUADRANT_GATEWAY_URL/toy | grep -E --color "\b(429)\b|$"; sleep 1; done
```

### Check RateLimitPolicy
```yaml
kubectl get ratelimitpolicy -n toystore my-toystore-plan -o yaml
```

### Check AuthPolicy
```yaml
kubectl get authpolicy -n toystore toystore -o yaml
```
