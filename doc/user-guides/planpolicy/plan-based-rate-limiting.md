# Plan-based Rate Limiting Tutorial

This tutorial demonstrates how to use the PlanPolicy extension to implement tiered service offerings with different rate limits based on user authentication plans.

## Overview

In this tutorial, you will:

1. Set up a basic Gateway and HTTPRoute
2. Configure authentication with different API keys for different plans
3. Deploy a PlanPolicy to automatically assign rate limits based on user plans
4. Test the plan-based rate limiting functionality

## Prerequisites

- Kubernetes cluster with Kuadrant operator installed. See our [Getting Started](/latest/getting-started) guide for more information.
- [kubectl](https://kubernetes.io/docs/tasks/tools/#kubectl) command line tool.

### Setup environment variables

Set environment variables for convenience:

```sh
export KUADRANT_GATEWAY_NS=api-gateway     # Namespace for the Gateway
export KUADRANT_GATEWAY_NAME=external      # Name for the Gateway
export KUADRANT_DEVELOPER_NS=toystore      # Namespace for the app
```

## Deploy Kuadrant and Gateway

Deploy the Kuadrant instance:

```sh
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1beta1
kind: Kuadrant
metadata:
  name: kuadrant
  namespace: kuadrant-system
EOF
```

Create the gateway namespace and deploy the Gateway:

```sh
kubectl create ns ${KUADRANT_GATEWAY_NS}
kubectl apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: ${KUADRANT_GATEWAY_NAME}
  namespace: ${KUADRANT_GATEWAY_NS}
  labels:
    kuadrant.io/gateway: "true"
spec:
  gatewayClassName: istio
  listeners:
    - name: http
      protocol: HTTP
      port: 80
      allowedRoutes:
        namespaces:
          from: All
EOF
```

## Deploy the Application

Deploy the toystore application and create an HTTPRoute:

```sh
kubectl create ns ${KUADRANT_DEVELOPER_NS}
kubectl apply -f https://raw.githubusercontent.com/Kuadrant/Kuadrant-operator/main/examples/toystore/toystore.yaml -n ${KUADRANT_DEVELOPER_NS}

kubectl apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: toystore
  namespace: ${KUADRANT_DEVELOPER_NS}
spec:
  parentRefs:
  - name: ${KUADRANT_GATEWAY_NAME}
    namespace: ${KUADRANT_GATEWAY_NS}
  hostnames:
  - api.toystore.com
  rules:
  - matches:
    - path:
        type: Exact
        value: "/toy"
      method: GET
    backendRefs:
    - name: toystore
      port: 80
EOF
```

Set up gateway connection variables:

```sh
export KUADRANT_INGRESS_HOST=$(kubectl get gtw ${KUADRANT_GATEWAY_NAME} -n ${KUADRANT_GATEWAY_NS} -o jsonpath='{.status.addresses[0].value}')
export KUADRANT_INGRESS_PORT=$(kubectl get gtw ${KUADRANT_GATEWAY_NAME} -n ${KUADRANT_GATEWAY_NS} -o jsonpath='{.spec.listeners[?(@.name=="http")].port}')
export KUADRANT_GATEWAY_URL=${KUADRANT_INGRESS_HOST}:${KUADRANT_INGRESS_PORT}

# Wait for the deployment to be ready
kubectl -n ${KUADRANT_DEVELOPER_NS} wait --for=condition=Available deployments toystore --timeout=90s
```

Test the basic connectivity:

```sh
curl -H 'Host: api.toystore.com' http://$KUADRANT_GATEWAY_URL/toy -i
# Expected: HTTP/1.1 200 OK
```

## Configure Authentication

Deploy an AuthPolicy targeting the Gateway with API key authentication:

```sh
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1
kind: AuthPolicy
metadata:
  name: toystore
  namespace: ${KUADRANT_GATEWAY_NS}
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: ${KUADRANT_GATEWAY_NAME}
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

Create API key secrets for different plans:

```sh
kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: gold-key
  namespace: ${KUADRANT_GATEWAY_NS}
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
  namespace: ${KUADRANT_GATEWAY_NS}
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
  namespace: ${KUADRANT_GATEWAY_NS}
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

Test that authentication is working:

```sh
# Without authentication - should fail
curl -H 'Host: api.toystore.com' http://$KUADRANT_GATEWAY_URL/toy -i
# Expected: HTTP/1.1 401 Unauthorized

# With authentication - should succeed
curl -H 'Host: api.toystore.com' -H 'Authorization: APIKEY IAMGOLD' http://$KUADRANT_GATEWAY_URL/toy -i
# Expected: HTTP/1.1 200 OK
```

## Deploy PlanPolicy

Now deploy the PlanPolicy to implement plan-based rate limiting:

```sh
kubectl apply -f - <<EOF
apiVersion: extensions.kuadrant.io/v1alpha1
kind: PlanPolicy
metadata:
  name: toystore-plans
  namespace: ${KUADRANT_DEVELOPER_NS}
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
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
        has(auth.identity) && auth.identity.metadata.annotations["secret.kuadrant.io/plan-id"] == "bronze"
      limits:
        daily: 1
EOF
```

## Verify Plan-based Rate Limiting

Check that the AuthConfig was updated with plan data:

```sh
kubectl get authconfig -n kuadrant-system -o yaml
```

You should see that the AuthConfig includes plan information in the response section.

## Test Different Plans

Test the bronze plan (1 request per day limit):

```sh
# First request should succeed
curl -H 'Host: api.toystore.com' -H 'Authorization: APIKEY IAMBRONZE' http://$KUADRANT_GATEWAY_URL/toy -i
# Expected: HTTP/1.1 200 OK

# Second request should be rate limited
curl -H 'Host: api.toystore.com' -H 'Authorization: APIKEY IAMBRONZE' http://$KUADRANT_GATEWAY_URL/toy -i
# Expected: HTTP/1.1 429 Too Many Requests (after rate limit is enforced)
```

Test the silver plan (2 requests per day limit):

```sh
# First request
curl -H 'Host: api.toystore.com' -H 'Authorization: APIKEY IAMSILVER' http://$KUADRANT_GATEWAY_URL/toy -i
# Expected: HTTP/1.1 200 OK

# Second request
curl -H 'Host: api.toystore.com' -H 'Authorization: APIKEY IAMSILVER' http://$KUADRANT_GATEWAY_URL/toy -i
# Expected: HTTP/1.1 200 OK

# Third request should be rate limited
curl -H 'Host: api.toystore.com' -H 'Authorization: APIKEY IAMSILVER' http://$KUADRANT_GATEWAY_URL/toy -i
# Expected: HTTP/1.1 429 Too Many Requests (after rate limit is enforced)
```

Test the gold plan (5 requests per day limit):

```sh
# Multiple requests should succeed up to the limit
for i in {1..5}; do
  curl -H 'Host: api.toystore.com' -H 'Authorization: APIKEY IAMGOLD' http://$KUADRANT_GATEWAY_URL/toy -i
done
# Expected: First 5 requests return HTTP/1.1 200 OK

# Sixth request should be rate limited
curl -H 'Host: api.toystore.com' -H 'Authorization: APIKEY IAMGOLD' http://$KUADRANT_GATEWAY_URL/toy -i
# Expected: HTTP/1.1 429 Too Many Requests (after rate limit is enforced)
```

## Understanding the PlanPolicy Configuration

The PlanPolicy works by:

1. **Plan Identification**: Each plan has a predicate that checks the `secret.kuadrant.io/plan-id` annotation in the authenticated user's secret
2. **Rate Limit Assignment**: Based on the identified plan, different rate limits are applied
3. **Automatic Enforcement**: The policy integrates with Kuadrant's rate limiting infrastructure

### Plan Predicates

The predicates use CEL expressions to identify which plan a user belongs to:

```yaml
predicate: |
  has(auth.identity) && auth.identity.metadata.annotations["secret.kuadrant.io/plan-id"] == "gold"
```

This checks:
- `has(auth.identity)`: Ensures the user is authenticated
- `auth.identity.metadata.annotations["secret.kuadrant.io/plan-id"] == "gold"`: Checks the plan-id annotation

### Rate Limits

Each plan defines different limits:
- **Bronze**: 1 request per day
- **Silver**: 2 requests per day  
- **Gold**: 5 requests per day

You can also define weekly, monthly, yearly, or custom limits:

```yaml
limits:
  daily: 100
  weekly: 500
  monthly: 2000
  custom:
    - limit: 10
      window: "1m"  # 10 requests per minute
```

## Cleanup

Clean up the resources created in this tutorial:

```sh
kubectl delete planpolicy toystore-plans -n ${KUADRANT_DEVELOPER_NS}
kubectl delete authpolicy toystore -n ${KUADRANT_GATEWAY_NS}
kubectl delete secret gold-key silver-key bronze-key -n ${KUADRANT_GATEWAY_NS}
kubectl delete -f https://raw.githubusercontent.com/Kuadrant/Kuadrant-operator/main/examples/toystore/toystore.yaml -n ${KUADRANT_DEVELOPER_NS}
kubectl delete httproute toystore -n ${KUADRANT_DEVELOPER_NS}
kubectl delete gateway ${KUADRANT_GATEWAY_NAME} -n ${KUADRANT_GATEWAY_NS}
kubectl delete ns ${KUADRANT_GATEWAY_NS} ${KUADRANT_DEVELOPER_NS}
```

## Next Steps

- Explore more complex predicate expressions for plan identification
- Integrate with JWT tokens for plan information
- Combine with other Kuadrant policies for comprehensive API management
- Monitor rate limiting metrics and adjust plans based on usage patterns
