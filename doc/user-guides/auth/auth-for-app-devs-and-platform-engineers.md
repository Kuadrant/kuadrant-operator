# Enforcing authentication & authorization with Kuadrant AuthPolicy

This guide walks you through the process of setting up a local Kubernetes cluster with Kuadrant where you will protect [Gateway API](https://gateway-api.sigs.k8s.io/) endpoints by declaring Kuadrant AuthPolicy custom resources.

Three AuthPolicies will be declared:

| Use case                       | AuthPolicies                                                                                                                                                                                                                                                                  |
| ------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **App developer**              | 2 AuthPolicies targeting a HTTPRoute that routes traffic to a sample "Toy Store" application → enforce API key authentication to all requests in this route; require API key owners to be mapped to `groups:admins` metadata to access a specific HTTPRouteRule of the route. |
| **Platform engineer use-case** | 1 AuthPolicy targeting the `kuadrant-ingressgateway` Gateway → enforces a trivial "deny-all" policy that locks down any other HTTPRoute attached to the Gateway.                                                                                                              |

Topology:

```
                            ┌─────────────────────────┐
                            │        (Gateway)        │   ┌───────────────┐
                            │ kuadrant-ingressgateway │◄──│ (AuthPolicy)  │
                            │                         │   │    gw-auth    │
                            │            *            │   └───────────────┘
                            └─────────────────────────┘
                              ▲                      ▲
                     ┌────────┴─────────┐   ┌────────┴─────────┐
┌────────────────┐   │   (HTTPRoute)    │   │   (HTTPRoute)    │
│  (AuthPolicy)  │──►│    toystore      │   │      other       │
│ toystore-authn │   │                  │   │                  │
└────────────────┘   │ api.toystore.com │   │ *.other-apps.com │
                     └──────────────────┘   └──────────────────┘
                      ▲                ▲
            ┌─────────┴───────┐ ┌──────┴──────────┐
            | (HTTPRouteRule) | | (HTTPRouteRule) |   ┌─────────────────┐
            |     rule-1      | |     rule-2      |◄──│   (AuthPolicy)  │
            |                 | |                 |   │ toystore-admins │
            | - GET /cars*    | | - /admins*      |   └─────────────────┘
            | - GET /dolls*   | └─────────────────┘
            └─────────────────┘
```

## Setup the environment

Follow this [setup doc](https://github.com/Kuadrant/kuadrant-operator/blob/main/doc/install/install-make-target.md) to set up your environment before continuing with this doc.


### Deploy the Toy Store sample application (Persona: _App developer_)

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
    - method: GET
      path:
        type: PathPrefix
        value: "/dolls"
    backendRefs:
    - name: toystore
      port: 80
  - matches: # rule-2
    - path:
        type: PathPrefix
        value: "/admin"
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

Send requests to the application unprotected:

```sh
curl -H 'Host: api.toystore.com' http://$GATEWAY_URL/cars -i
# HTTP/1.1 200 OK
```

```sh
curl -H 'Host: api.toystore.com' http://$GATEWAY_URL/dolls -i
# HTTP/1.1 200 OK
```

```sh
curl -H 'Host: api.toystore.com' http://$GATEWAY_URL/admin -i
# HTTP/1.1 200 OK
```

### Protect the Toy Store application (Persona: _App developer_)

Create AuthPolicies to enforce the following auth rules:

- **Authentication:**
  - All users must present a valid API key
- **Authorization:**
  - `/admin*` paths (2nd rule of the HTTPRoute) require user mapped to the `admins` group (`kuadrant.io/groups=admins` annotation added to the Kubernetes API key Secret)

```sh
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1
kind: AuthPolicy
metadata:
  name: toystore-authn
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: toystore
  defaults:
    strategy: merge
    rules:
      authentication:
        "api-key-authn":
          apiKey:
            selector:
              matchLabels:
                app: toystore
          credentials:
            authorizationHeader:
              prefix: APIKEY
---
apiVersion: kuadrant.io/v1
kind: AuthPolicy
metadata:
  name: toystore-admins
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: toystore
    sectionName: rule-2
  rules:
    authorization:
      "only-admins":
        opa:
          rego: |
            groups := split(object.get(input.auth.identity.metadata.annotations, "kuadrant.io/groups", ""), ",")
            allow { groups[_] == "admins" }
EOF
```

Create the API keys (must be created in the same namespace as the Kuadrant CR):

```sh
kubectl apply -n kuadrant-system -f -<<EOF
apiVersion: v1
kind: Secret
metadata:
  name: api-key-regular-user
  labels:
    authorino.kuadrant.io/managed-by: authorino
    app: toystore
stringData:
  api_key: iamaregularuser
type: Opaque
---
apiVersion: v1
kind: Secret
metadata:
  name: api-key-admin-user
  labels:
    authorino.kuadrant.io/managed-by: authorino
    app: toystore
  annotations:
    kuadrant.io/groups: admins
stringData:
  api_key: iamanadmin
type: Opaque
EOF
```

Send requests to the application protected by Kuadrant:

```sh
curl -H 'Host: api.toystore.com' http://$GATEWAY_URL/cars -i
# HTTP/1.1 401 Unauthorized
# www-authenticate: APIKEY realm="api-key-authn"
# x-ext-auth-reason: credential not found
```

```sh
curl -H 'Host: api.toystore.com' -H 'Authorization: APIKEY iamaregularuser' http://$GATEWAY_URL/cars -i
# HTTP/1.1 200 OK
```

```sh
curl -H 'Host: api.toystore.com' -H 'Authorization: APIKEY iamaregularuser' http://$GATEWAY_URL/admin -i
# HTTP/1.1 403 Forbidden
# x-ext-auth-reason: Unauthorized
```

```sh
curl -H 'Host: api.toystore.com' -H 'Authorization: APIKEY iamanadmin' http://$GATEWAY_URL/admin -i
# HTTP/1.1 200 OK
```

### Create a default "deny-all" policy at the level of the gateway (Persona: _Platform engineer_)

Create the policy:

```sh
kubectl -n gateway-system apply -f - <<EOF
apiVersion: kuadrant.io/v1
kind: AuthPolicy
metadata:
  name: gw-auth
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: kuadrant-ingressgateway
  defaults:
    strategy: atomic
    rules:
      authorization:
        deny-all:
          opa:
            rego: "allow = false"
      response:
        unauthorized:
          headers:
            "content-type":
              value: application/json
          body:
            value: |
              {
                "error": "Forbidden",
                "message": "Access denied by default by the gateway operator. If you are the administrator of the service, create a specific auth policy for the route."
              }
EOF
```

The policy won't be effective until there is at least one accepted route not yet protected by another more specific policy attached to it.

Create a route that will inherit the default policy attached to the gateway:

```sh
kubectl apply -f -<<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: other
spec:
  parentRefs:
  - name: kuadrant-ingressgateway
    namespace: gateway-system
  hostnames:
  - "*.other-apps.com"
EOF
```

Send requests to the route protected by the default policy set at the level of the gateway:

```sh
curl -H 'Host: foo.other-apps.com' http://$GATEWAY_URL/ -i
# HTTP/1.1 403 Forbidden
# content-type: application/json
# x-ext-auth-reason: Unauthorized
# […]
#
# {
#   "error": "Forbidden",
#   "message": "Access denied by default by the gateway operator. If you are the administrator of the service, create a specific auth policy for the route."
# }
```

## Cleanup

```sh
make local-cleanup
```
