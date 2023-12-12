# Kuadrant Quick Start

## Pre-requisites

- Completed the [single cluster quick start](https://docs.kuadrant.io/getting-started-single-cluster/)

## Overview 

In this guide, we will cover the different policies from Kuadrant and how you can use them to secure, protect and connect an istio controlled gateway in a single cluster and how you can set more refined protection on the HTTPRoutes exposed by that gateway.

Here are the steps we will go through:

1) [Deploy a sample application](#deploy-the-example-app-we-will-serve-via-our-gateway)
2) [Define a new Gateway](#define-a-new-istio-managed-gateway)
3) [Ensure TLS based secure connectivity to the gateway with `TLSPolicy`](#define-tlspolicy)
4) [Define a default `RateLimitPolicy` to set some infrastructure limits on your gateway](#define-infrastructure-rate-limiting)
5) [Define a default `AuthPolicy` to `Deny ALL` access to the gateway](#define-a-gateway-authpolicy)
6) [Define `DNSPolicy` to bring traffic to the gateway](#define-dnspolicy)
7) [Override the Gateway's Deny ALL `AuthPolicy` with an endpoint specific policy](#override-the-gateways-deny-all-authpolicy)
8) [Override the Gateway `RateLimits` with an endpoint specific policy](#override-the-gateways-ratelimits) 


To help with this walk through, you should set a `KUADRANT_ZONE_ROOT_DOMAIN` environmental variable to a domain you want to use. If it you want to try `DNSPolicy` this should also be a domain you have access to the DNS for in `route53 or GCP`. Example:
```export KUADRANT_ZONE_ROOT_DOMAIN=my.domain.iown```

### Deploy the example app we will serve via our gateway

`kubectl apply -f https://raw.githubusercontent.com/Kuadrant/kuadrant-operator/main/examples/toystore/toystore.yaml`

### Define a new Istio managed gateway

```
kubectl --context kind-kuadrant-local apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: api-gateway
  namespace: kuadrant-system
spec:
  gatewayClassName: istio
  listeners:
  - allowedRoutes:
      namespaces:
        from: All
    name: api
    hostname: "*.$KUADRANT_ZONE_ROOT_DOMAIN"
    port: 443
    protocol: HTTPS
    tls:
      mode: Terminate
      certificateRefs:
        - name: apps-hcpapps-tls
          kind: Secret
EOF
```


If you take a look at the gateway status you will see a tls status error something like: 
` message: invalid certificate reference /Secret/apps-hcpapps-tls. secret kuadrant-system/apps-hcpapps-tls not found`. This is because there is currently not TLS secret in place. Lets fix that with a `TLSPolicy`

### Define TLSPolicy

Note: For convenience in the setup, we have setup a self signed CA as a cluster issuer on the kind cluster.

```
kubectl --context kind-kuadrant-local apply -f - <<EOF
apiVersion: kuadrant.io/v1alpha1
kind: TLSPolicy
metadata:
  name: api-gateway-tls
  namespace: kuadrant-system
spec:
  targetRef:
    name: api-gateway
    group: gateway.networking.k8s.io
    kind: Gateway
  issuerRef:
    group: cert-manager.io
    kind: ClusterIssuer
    name: glbc-ca
EOF

kubectl wait tlspolicy api-gateway-tls -n kuadrant-system --for=condition=ready

```

Now if you look at the status of the gateway you will see the error is gone and the status of the policy you will see the listener is now secured with a tls certificate and the gateway is marked as affected by the tls policy. Our communication with our gateway is now secured via TLS. Note any new listeners will also be handled by the `TLSPolicy`


Lets define a HTTPRoute and test our policy. We will re-use this with some of the other policies.



```

export INGRESS_PORT=$(kubectl get gtw api-gateway -o jsonpath='{.spec.listeners[?(@.name=="api")].port}' -n kuadrant-system)

export INGRESS_HOST=$(kubectl get gtw api-gateway -o jsonpath='{.status.addresses[0].value}' -n kuadrant-system)


kubectl --context kind-kuadrant-local apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1beta1
kind: HTTPRoute
metadata:
  name: toystore
spec:
  parentRefs:
  - name: api-gateway
    namespace: kuadrant-system
  hostnames:
  - "api.$KUADRANT_ZONE_ROOT_DOMAIN"
  rules:
  - matches:
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
  - matches:
    - path:
        type: PathPrefix
        value: "/admin"
    backendRefs:
    - name: toystore
      port: 80
EOF



```

With this HTTPRoute in place the service we deployed later is exposed via the gateway. We should be able to access our endpoint via HTTPS:

```
curl -k --resolve api.${KUADRANT_ZONE_ROOT_DOMAIN}:443:${INGRESS_HOST} "https://api.$KUADRANT_ZONE_ROOT_DOMAIN/cars"

```

### Define Infrastructure Rate Limiting

So we have a secure communications in place. But there is nothing limiting users from overloading our infrastructure and service components that will sit behind this gateway. Lets and a rate limiting layer to protect our services and infrastructure.

```
kubectl --context kind-kuadrant-local apply -f - <<EOF
apiVersion: kuadrant.io/v1beta2
kind: RateLimitPolicy
metadata:
  name: infra-ratelimit
  namespace: kuadrant-system
spec:
  targetRef:
    name: api-gateway
    group: gateway.networking.k8s.io
    kind: Gateway
  limits:
    "global":
      rates:
      - limit: 5
        duration: 10
        unit: second
EOF


kubectl wait ratelimitpolicy infra-ratelimit -n kuadrant-system --for=condition=available

```

The limit here is artificially low in order for us to show it working easily. Lets test it with our endpoint:

```
for i in {1..10}; do curl -k --resolve api.${KUADRANT_ZONE_ROOT_DOMAIN}:443:${INGRESS_HOST} "https://api.$KUADRANT_ZONE_ROOT_DOMAIN/cars" && sleep 1; done
```

Here we should see `409s` start returning after the 5th request.

### Define a Gateway `AuthPolicy`

So communication is secured and we have some protection for our infrastructure, but we do not trust any client to access our endpoints. By default we want to allow only authenticated access. To protect our gateway we will add a `DENY ALL` `AuthPolicy`. Later we will override this with a specific `AuthPolicy` for the API.

```
kubectl --context kind-kuadrant-local apply -f - <<EOF
apiVersion: kuadrant.io/v1beta2
kind: AuthPolicy
metadata:
  name: deny-all
  namespace: kuadrant-system
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: api-gateway
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

Lets test again. This time we expect a `403`

```
curl -k --resolve api.${KUADRANT_ZONE_ROOT_DOMAIN}:443:${INGRESS_HOST}  "https://api.$KUADRANT_ZONE_ROOT_DOMAIN/cars"

```

### Define DNSPolicy 
(skip of you did not setup a DNS provider during the setup)

Now we have our gateway protected and communications secured, we are ready to configure DNS so it is easy for clients to connect and access the APIs we intend to expose via this gateway. Note during the setup of this walk through, we setup a `DNS Provider` secret and a `ManagedZone`.

```
kubectl --context kind-kuadrant-local apply -f - <<EOF
apiVersion: kuadrant.io/v1alpha1
kind: DNSPolicy
metadata:
  name: simple-dnspolicy
  namespace: kuadrant-system
spec:
  routingStrategy: simple
  targetRef:
    name: api-gateway
    group: gateway.networking.k8s.io
    kind: Gateway
EOF

kubectl wait dnspolicy simple-dnspolicy -n kuadrant-system --for=condition=ready
```

If you want to see the DNSRecord created by the this policy, execute 

```
kubectl get dnsrecord api-gateway-api -n kuadrant-system -o=yaml
```

So now we have a wildcard DNS record to bring traffic to our gateway. 

Lets test again. This time we expect a `403` still as the DENY_ALL is still in effect. Notice we no longer need to set the Host header directly.


```
curl -k  "https://api.$KUADRANT_ZONE_ROOT_DOMAIN/cars" -i

```

### Override the Gateway's Deny ALL `AuthPolicy`

Next we are going to allow authenticated access to our toystore API. To do this we will define an `AuthPolicy` that targets the `HTTPRoute`. Note that any new `HTTPRoutes` will still be effected by the gateway level policy but as we want users to now access this API we need to override the gateway policy. For simplicity we will use an API Key to allow access, but many other options are available.

Lets define an API Key for user `bob` and `alice`

```
kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: bob-key
  labels:
    authorino.kuadrant.io/managed-by: authorino
    app: toystore
  annotations:
    secret.kuadrant.io/user-id: bob
stringData:
  api_key: IAMBOB
type: Opaque

---
apiVersion: v1
kind: Secret
metadata:
  name: alice-key
  labels:
    authorino.kuadrant.io/managed-by: authorino
    app: toystore
  annotations:
    secret.kuadrant.io/user-id: alice
stringData:
  api_key: IAMALICE
type: Opaque
EOF
``` 

Now we will override the `AuthPolicy` to use API Keys:

```
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1beta2
kind: AuthPolicy
metadata:
  name: toystore
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: toystore
  rules:
    authentication:
      "api-key-users":
        apiKey:
          selector:
            matchLabels:
              app: toystore
        credentials:
          authorizationHeader:
            prefix: APIKEY
EOF
```

### Override the Gateway's RateLimits

So the gateway limits are good general set of limits. But as the developers of this API we know that we only want to allow a certain number of requests to specific users and a general limit for all other users


```sh
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1beta2
kind: RateLimitPolicy
metadata:
  name: toystore
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: toystore
  limits:
    "general-user":
      rates:
      - limit: 1
        duration: 3
        unit: second
    "bob-limit":
      rates:
      - limit: 2
        duration: 3
        unit: second
      when:
      - selector: metadata.filter_metadata.envoy\.filters\.http\.ext_authz.identity.userid
        operator: eq
        value: bob
EOF
```

So here again just an example, we have given `bob` twice as many requests to use as everyone else.

Lets test this new setup:

```sh
while :; do curl -k --resolve api.${KUADRANT_ZONE_ROOT_DOMAIN}:443:${INGRESS_HOST} --write-out '%{http_code}\n' --silent --output /dev/null -H 'Authorization: APIKEY IAMALICE' "https://api.$KUADRANT_ZONE_ROOT_DOMAIN/cars" | grep -E --color "\b(429)\b|$"; sleep 1; done
```


```sh
while :; do curl -k --resolve api.${KUADRANT_ZONE_ROOT_DOMAIN}:443:${INGRESS_HOST} --write-out '%{http_code}\n' --silent --output /dev/null -H 'Authorization: APIKEY IAMBOB' "https://api.$KUADRANT_ZONE_ROOT_DOMAIN/cars" | grep -E --color "\b(429)\b|$"; sleep 1; done
```
