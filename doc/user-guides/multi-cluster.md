# Secure, Protect and Connect APIs with Kuadrant on Multiple Clusters


## Pre-requisites

- Completed the Kuadrant Install Guide for at least two clusters [Install Guide](../install/install-openshift.md)
- kubectl command line tool

## Overview

This doc expects that you have successfully installed kuadrant onto two different clusters and have configured a shared, accessible redis store. 
In this doc we will walk you through using Kuadrant to secure, protect and connect an API that is distributed across multiple clusters. We will go through setting up DNS based load balancing, Global Rate Limiting, Auth and TLS for a HTTP based API.

**Note:** It is important to note that unless explicitly stated, it is expected that these commands are executed against both clusters. For more complex setups a management tool such as ArgoCD can be very useful for distributing the configuration.

### Setup a managed DNS zone

This is a zone where kuadrant will manage records for listener hosts added to your gateway(s) and connect traffic to your endpoints. It is this zone plus the hostnames defined in the gateway listeners that allow Kuadrant to define a multi-cluster DNS configuration.

Create the ManagedZone resource

```
export zid=change-this-to-your-zone-id
export rootDomain=example.com
```
apply the zone resource to each cluster or if you are adding an additional cluster add it to the new cluster:

```
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1alpha1
kind: ManagedZone
metadata:
  name: managedzone
  namespace: ingress-gateway
spec:
  id: ${zid}
  domainName: ${rootDomain}
  description: "kuadrant managed zone"
  dnsProviderSecretRef:
    name: aws-credentials
EOF
```

Wait for the zone to be ready

```
k wait managedzone/managedzone --for=condition=ready=true -n ingress-gateway
```


### Add a TLS Issuer

To secure our gateways we want to define a TLS issuer for TLS certificates.


```
export EMAIL=myemail@email.com
```
```
kubectl apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: lets-encrypt
spec:
  acme:
    email: ${EMAIL} 
    privateKeySecretRef:
      name: le-secret
    server: https://acme-staging-v02.api.letsencrypt.org/directory
    solvers:
      - dns01:
          route53:
            hostedZoneID: ${zid}
            region: us-east-1
            accessKeyIDSecretRef:
              key: AWS_ACCESS_KEY_ID
              name: aws-credentials
            secretAccessKeySecretRef:
              key: AWS_SECRET_ACCESS_KEY
              name: aws-credentials
EOF
```

### Setup a Gateway

In order for Kuadrant to balance traffic using DNS across two clusers. We need to define a gateway with a shared host. We will define this is a HTTPS listener with a wildcard DNS entry based on your root domain.

```
kubectl apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: external
  namespace: ingress-gateway
spec:
    gatewayClassName: istio
    listeners:
    - allowedRoutes:
        namespaces:
          from: All
      hostname: "*.${rootDomain}"
      name: api
      port: 443
      protocol: HTTPS
      tls:
        certificateRefs:
        - group: ""
          kind: Secret
          name: api-external-tls
        mode: Terminate
EOF
```        

### Secure and Protect the Gateway with ratelimiting, Auth and TLS

While our gateway is deployed, it is not yet exposed via an address. This is because we have not yet setup the TLS cert. Before we do that lets set up an `AuthPolicy` that will default all endpoint to a `DENY ALL` 403 response and also setup a `ratelimitpolicy`. Finally we will then also apply a `TLSPolicy` to setup our certificates.

```
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1beta2
kind: AuthPolicy
metadata:
  name: external
  namespace: ingress-gateway
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: external
  defaults:
    rules:
      authorization:
        "deny":
          opa:
            rego: "allow = false"
EOF
```

RateLimitPolicy

```
kubectl apply -f  - <<EOF
apiVersion: kuadrant.io/v1beta2
kind: RateLimitPolicy
metadata:
  name: low-limit
  namespace: ingress-gateway
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: external
  overrides:
    limits:
      "low-limit":
        rates:
        - limit: 2
          duration: 10
          unit: second
EOF
```

TLSPolicy

```
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1alpha1
kind: TLSPolicy
metadata:
  name: external
  namespace: ingress-gateway
spec:
  targetRef:
    name: external
    group: gateway.networking.k8s.io
    kind: Gateway
  issuerRef:
    group: cert-manager.io
    kind: ClusterIssuer
    name: lets-encrypt
EOF
```


Lets check our policies have been accepted. 

```
kubectl get tlspolicy external -n ingress-gateway -o=jsonpath='{.status.conditions[?(@.type=="Accepted")].message}'

kubectl get authpolicy external -n ingress-gateway -o=jsonpath='{.status.conditions[?(@.type=="Accepted")].message}'

kubectl get ratelimitpolicy low-limit -n ingress-gateway -o=jsonpath='{.status.conditions[?(@.type=="Accepted")].message}'

```
Again we should see it has been accepted but not yet enforced. It is not enforced as we have not added any HTTPRoutes to the gateway yet.

### Setup our DNS

Next we will apply a `DNSPolicy`. This policy will configure how traffic reaches the gateway. Again it will be accepted but not yet enforced as we have no HTTPRoutes defined at this point.

```
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1alpha1
kind: DNSPolicy
metadata:
  name: loadbalanced
  namespace: ingress-gateway
spec:
  routingStrategy: loadbalanced
  targetRef:
    name: external
    group: gateway.networking.k8s.io
    kind: Gateway
EOF
```    
Note: the DNSPolicy will leverage the ManagedZone we defined earlier based on the listener hosts defined in the gateway.

check our status conditions
```
k get dnspolicy loadbalanced -n ingress-gateway -o=jsonpath='{.status.conditions[?(@.type=="Accepted")].message}'
```

You should see it has been accepted but not yet enforced.


#TODO add section about viewing Gateway dashboards

## RECAP

So far we have setup an external gateway, secured it with TLS, Protected all endpoints with a default `DENY ALL` AuthPolicy added a restrictive RateLimitPolicy and set up ManagedZone and a DNSPolicy to ensure traffic is brought to the gateway for the listener hosts defined once we apply a HTTPRoute. Now that our policies are in place lets add a backend and HTTPRoute

### Setup HTTPRoute and backend

This will setup a toy application in the default ns

```sh
kubectl create ns toystore
kubectl apply -f https://raw.githubusercontent.com/Kuadrant/kuadrant-operator/main/examples/toystore/toystore.yaml -n toystore
```

### Setup HTTPRoute level RateLimits and Auth

```
kubectl apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: api
  namespace: toystore
spec:
  parentRefs:
  - name: external
    namespace: ingress-gateway
  hostnames:
  - "toys.${rootDomain}"
  rules:
  - backendRefs:
    - name: toystore
      port: 80
EOF
```      

```
kubectl apply -f  - <<EOF
apiVersion: kuadrant.io/v1beta2
kind: RateLimitPolicy
metadata:
  name: high-limit-api
  namespace: toystore
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: api
  limits:
    "high-limit":
      rates:
      - limit: 5
        duration: 10
        unit: second
EOF
```

# TODO 
- Add some verification steps
- Add some dashboard directions
- Add instructions for GEO
- Add instructions for using non API Key auth provider