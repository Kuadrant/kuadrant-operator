# Enforcing anonymous access with Kuadrant AuthPolicy

Learn how to allow anonymous access to certain endpoints using Kuadrant's `AuthPolicy`

## Requisites

- [Docker](https://docker.io)

## Run the guide ① → ④

### ①  Setup 

Clone the repo:

```sh
git clone git@github.com:Kuadrant/kuadrant-operator.git && cd kuadrant-operator
```

Run the following command to create a local Kubernetes cluster with [Kind](https://kind.sigs.k8s.io/), install & deploy Kuadrant:

```sh
make local-setup
```

Request an instance of Kuadrant in the `kuadrant-system` namespace:

```sh
kubectl -n kuadrant-system apply -f - <<EOF
apiVersion: kuadrant.io/v1beta1
kind: Kuadrant
metadata:
  name: kuadrant
spec: {}
EOF
```

### ② Deploy the Talker API

```sh
kubectl apply -f examples/toystore/toystore.yaml

kubectl apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: toystore
spec:
  parentRefs:
  - name: kuadrant-ingressgateway
    namespace: gateway-system
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

Export the gateway hostname and port:

```sh
export INGRESS_HOST=$(kubectl get gtw kuadrant-ingressgateway -n gateway-system -o jsonpath='{.status.addresses[0].value}')
export INGRESS_PORT=$(kubectl get gtw kuadrant-ingressgateway -n gateway-system -o jsonpath='{.spec.listeners[?(@.name=="http")].port}')
export GATEWAY_URL=$INGRESS_HOST:$INGRESS_PORT
```

Test requests to the unprotected application:

```sh
curl -H 'Host: api.toystore.com' http://$GATEWAY_URL/cars -i
# HTTP/1.1 200 OK
```

```sh
curl -H 'Host: api.toystore.com' http://$GATEWAY_URL/public -i
# HTTP/1.1 200 OK
```

### ③ Protect the Toy Store application

Create an `AuthPolicy` to protect the `HTTPRoute`:

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

Test requests to the protected application:

```sh
curl -H 'Host: api.toystore.com' http://$GATEWAY_URL/cars -i
# HTTP/1.1 403 Forbidden
```

```sh
curl -H 'Host: api.toystore.com' http://$GATEWAY_URL/public -i
# HTTP/1.1 403 Forbidden
```

### ④ Allow Anonymous Access to the Public Route
Create an `AuthPolicy` to enable anonymous access for the `/public` rule:

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

### ④ Consume the API

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
kubectl delete -f examples/toystore/toystore.yaml
kubectl delete httproute toystore
kubectl delete authpolicy route-auth
kubectl delete authpolicy rule-2-auth
kubectl delete kuadrant kuadrant -n kuadrant-system
```
