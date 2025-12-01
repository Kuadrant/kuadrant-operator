# Enforcing anonymous access with Kuadrant AuthPolicy

Learn how to allow anonymous access to certain endpoints using Kuadrant's `AuthPolicy`

## Prerequisites

- Kubernetes cluster with Kuadrant operator installed. See our [Getting Started](/latest/getting-started) guide for more information.
- [kubectl](https://kubernetes.io/docs/tasks/tools/#kubectl) command line tool.

### Create Gateway
Create a `Gateway` resource for this guide:

```sh
kubectl apply -f -<<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: kuadrant-ingressgateway
spec:
  gatewayClassName: istio
  listeners:
  - name: http
    protocol: HTTP
    port: 80
    allowedRoutes:
      namespaces:
        from: Same
EOF
```
The `Gateway` resource created above uses Istio as the gateway provider. For Envoy Gateway, use the Envoy Gateway `GatewayClass` as the `gatewayClassName`.

### Deploy Toy Store application

Deploy a simple HTTP application service that echoes back the request data:

```sh
kubectl apply -f https://raw.githubusercontent.com/Kuadrant/kuadrant-operator/refs/heads/main/examples/toystore/toystore.yaml
```

### Expose the Application

Create an `HTTPRoute` to expose an `/cars` and `/public` path to the application:

```sh
kubectl apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: toystore
spec:
  parentRefs:
  - name: kuadrant-ingressgateway
    namespace: default
  hostnames:
  - api.toystore.com
  rules:
  - matches: # rule-1
    - method: GET
      path:
        type: PathPrefix
        value: "/cars"
    backendRefs:
    - name: toystore
      port: 80
  - matches: # rule-2
    - method: GET
      path:
        type: PathPrefix
        value: "/public"
    backendRefs:
    - name: toystore
      port: 80
EOF
```

Export the gateway hostname and port for testing:

```sh
export INGRESS_HOST=$(kubectl get gtw kuadrant-ingressgateway -n default -o jsonpath='{.status.addresses[0].value}')
export INGRESS_PORT=$(kubectl get gtw kuadrant-ingressgateway -n default -o jsonpath='{.spec.listeners[?(@.name=="http")].port}')
export GATEWAY_URL=$INGRESS_HOST:$INGRESS_PORT
```

### Test the Unprotected Application
Test requests to the unprotected application:

```sh
curl -H 'Host: api.toystore.com' http://$GATEWAY_URL/cars -i
# HTTP/1.1 200 OK
```

```sh
curl -H 'Host: api.toystore.com' http://$GATEWAY_URL/public -i
# HTTP/1.1 200 OK
```

### Deny All Traffic with AuthPolicy

Apply an `AuthPolicy` to deny all traffic to the `HTTPRoute`:

```sh
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1
kind: AuthPolicy
metadata:
  name: route-auth
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: toystore
  defaults:
    strategy: atomic
    rules:
      authorization:
        deny-all:
          opa:
            rego: "allow = false"
EOF
```

### Test the Protected Application

```sh
curl -H 'Host: api.toystore.com' http://$GATEWAY_URL/cars -i
# HTTP/1.1 403 Forbidden
```

```sh
curl -H 'Host: api.toystore.com' http://$GATEWAY_URL/public -i
# HTTP/1.1 403 Forbidden
```

### Allow Anonymous Access to /public
Create an `AuthPolicy` to allow anonymous access to the `/public` endpoint:

```sh
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1
kind: AuthPolicy
metadata:
  name: rule-2-auth
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: toystore
    sectionName: rule-2
  defaults:
    rules:
      authentication:
        "public":
          anonymous: {}
EOF
```

The example above enables anonymous access (i.e. removes authentication) to the `/public` rule of the `HTTPRoute`.

### Test the Application with Anonymous Access

Test requests to the application protected by Kuadrant:

```sh
curl -H 'Host: api.toystore.com' http://$GATEWAY_URL/cars -i
# HTTP/1.1 403 Forbidden
```

```sh
curl -H 'Host: api.toystore.com' http://$GATEWAY_URL/public -i
# HTTP/1.1 200 OK
```

## Cleanup

```sh
kubectl delete -f https://raw.githubusercontent.com/Kuadrant/kuadrant-operator/refs/heads/main/examples/toystore/toystore.yaml
kubectl delete httproute toystore
kubectl delete authpolicy route-auth
kubectl delete authpolicy rule-2-auth
kubectl delete gateway kuadrant-ingressgateway
```
