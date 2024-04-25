# Secure, Protect and Connect APIs with Kuadrant on Multiple Clusters


## Pre-requisites

This document expects that you have successfully installed Kuadrant [Install Guide](../install/install-openshift.md) onto two different clusters and have configured a shared, accessible redis store. 

- Completed the Kuadrant Install Guide for at least two clusters [Install Guide](../install/install-openshift.md)
- kubectl command line tool
- (optional) have user workload monitoring configured to remote write to a central storage system such as Thanos (also covered in the installation guide).

## Overview

In this doc we will walk you through using Kuadrant to secure, protect and connect an API via a set of Gateways distributed across multiple clusters. 
We will take the approach of assuming certain personas and how they can each work with Kuadrant to achieve their goals.

### what Kuadrant can do for you in a multi-cluster environment

Kuadrant's capabilities can be leveraged in single or multiple clusters. Below is a list of features that are designed to work across multiple clusters as well as in a single cluster environment.

**Multi-Cluster Ingress:** Kuadrant provides multi-cluster ingress connectivity using DNS to bring traffic to your Gateways using a strategy defined in a `DNSPolicy` (more later). 
**Global RateLimiting:** Kuadrant can enable global rate limiting usecases when it is configured to use a shared store (redis) for counters based on limits defined by a `RateLimitPolicy`.
**Global Auth:*** Kuadrant's `AuthPolicy` can be configured to leverage external auth providers to ensure different cluster exposing the same API are authenticating and authorizing in the same way. 
**Integration with federated metrics stores:** Kuadrant has example dashboards and metrics that can be used for visualizing your gateways and observing traffic hitting those gateways across multiple clusters. 

**Platform Engineer**

We will walk through deploying a gateway that provides secure communications and is protected and ready to be used by development teams to deploy an API. We will then walk through how you can have this gateway in clusters in different geographic regions and leverage Kuadrant to bring the specific traffic to your geo located gateways to reduce latency and distribute load while still having it protected and secured via global rate limiting and auth.

As an optional extra we will highlight how, with the user workload monitoring observability stack deployed, these gateways can then be observed and monitored. 

**Developer**

We will walk through how you can use the kuadrant OAS extensions and CLI to generate a `HTTPRoute` for your API and add both Auth and Rate Limiting to your API.

## Platform Engineer

The following steps should be done in each cluster individually unless specifically excluded. 

### Env Vars

For convenience in this guide we use some env vars throughout this document

```
export zid=change-this-to-your-zone-id
export rootDomain=example.com
export gatewayNS=ingress-gateway
export AWS_ACCESS_KEY_ID=xxxx
export AWS_SECRET_ACCESS_KEY=xxxx

```

### Tooling

While this document uses kubectl, working with multiple clusters is complex and so we would recommend looking into something like ArgoCD to manage the deployment of resources etc to multiple clusters.

### Setup a managed DNS zone

The managed dns zone declares a zone and credentials to access that zone that can be used by Kuadrant to setup DNS configuration.

**Create the ManagedZone resource**

Ensure your kubectl is targeting the correct cluster. Apply the `ManagedZone` resource below to each cluster or if you are adding an additional cluster add it to the new cluster:

```
kubectl create ns ${gatewayNS}
```

Setup AWS credential for route53 access

```
kubectl -n ${gatewayNS} create secret generic aws-credentials \
  --type=kuadrant.io/aws \
  --from-literal=AWS_ACCESS_KEY_ID=$AWS_ACCESS_KEY_ID \
  --from-literal=AWS_SECRET_ACCESS_KEY=$AWS_SECRET_ACCESS_KEY
```  


```
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1alpha1
kind: ManagedZone
metadata:
  name: managedzone
  namespace: ${gatewayNS}
spec:
  id: ${zid}
  domainName: ${rootDomain}
  description: "Kuadrant managed zone"
  dnsProviderSecretRef:
    name: aws-credentials
EOF
```

Wait for the zone to be ready

```
kubectl wait managedzone/managedzone --for=condition=ready=true -n ingress-gateway
```


### Add a TLS Issuer

To secure communication to the gateways we want to define a TLS issuer for TLS certificates. We will use letsencrypt, but you can use any supported by cert-manager.


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


kubectl wait clusterissuer/lets-encrypt --for=condition=ready=true
```

### Setup a Gateway

In order for Kuadrant to balance traffic using DNS across two or more clusters. We need to define a gateway with a shared host. We will define this with a HTTPS listener with a wildcard hostname based on the root domain. As mentioned, these resources need to be applied to both clusters.

```
kubectl apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: external
  namespace: ${gatewayNS}
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

### Secure and Protect the Gateway with Rate Limiting, Auth and TLS policies.

While our gateway is now deployed it has no exposed endpoints. So Before we do that lets set up a `TLSPolicy` that leverages our CertificateIssuer to setup our listener certificates. Also lets define an `AuthPolicy` that will setup a default 403 response for any unprotected endpoints and a `RateLimitPolicy` that will setup a default (artificially) low global limit to further protect any endpoints exposed by this gateway.


TLSPolicy

```
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1alpha1
kind: TLSPolicy
metadata:
  name: external
  namespace: ${gatewayNS}
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

AuthPolicy

```
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1beta2
kind: AuthPolicy
metadata:
  name: external
  namespace: ${gatewayNS}
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
  name: external
  namespace: ${gatewayNS}
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: external
  defaults:
    limits:
      "low-limit":
        rates:
        - limit: 2
          duration: 10
          unit: second
EOF
```


Lets check our policies have been accepted. 

```
kubectl get tlspolicy external -n ${gatewayNS} -o=jsonpath='{.status.conditions[?(@.type=="Accepted")].message}'

kubectl get authpolicy external -n ${gatewayNS} -o=jsonpath='{.status.conditions[?(@.type=="Accepted")].message}'

kubectl get ratelimitpolicy low-limit -n ${gatewayNS} -o=jsonpath='{.status.conditions[?(@.type=="Accepted")].message}'

```

### Setup our DNS

Next we will apply a `DNSPolicy`. This policy will configure how traffic reaches the gateways deployed to our different clusters. In this case it will setup a loadbalanced strategy, which will mean it will provide a form of RoundRobin response to DNS clients. We also define default GEO, this doesn't have an immediate impact but rather is there so that when/if we enable geo routing on our gateways, the default is defined for any users outside of the specified gateway GEOs ensuring all users regardless of their geo will be able to reach our gateway (more later).

```
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1alpha1
kind: DNSPolicy
metadata:
  name: loadbalanced
  namespace: ${gatewayNS}
spec:
  routingStrategy: loadbalanced
    geo:
      defaultGeo: US
  targetRef:
    name: external
    group: gateway.networking.k8s.io
    kind: Gateway
EOF
```    
Note: the DNSPolicy will leverage the ManagedZone we defined earlier based on the listener hosts defined in the gateway.

Lets check our DNSPolicy has been accepted.
```
kubectl get dnspolicy loadbalanced -n ${gatewayNS} -o=jsonpath='{.status.conditions[?(@.type=="Accepted")].message}'
```


#TODO add section about viewing Gateway dashboards

## Platform Engineer review

So far we have setup an external gateway, secured it with TLS, Protected all endpoints with a default `DENY ALL` AuthPolicy added a restrictive default RateLimitPolicy and set up ManagedZone and a DNSPolicy to ensure traffic is brought to the gateway for the listener hosts defined. Our gateway is now ready to start accepting traffic.

To cause DNS to populate and the DNSPolicy to be enforced, we first need to add an actual HTTPRoute based endpoint.

## Developer


TODO define OAS


### Setup HTTPRoute and backend

This will setup a toy application in the same ns as the gateway but you can deploy it to any namespace.

```sh
kubectl apply -f https://raw.githubusercontent.com/Kuadrant/Kuadrant-operator/main/examples/toystore/toystore.yaml -n ${gatewayNS}
```

Open Up the application to traffic via a `HTTPRoute`

```
kubectl apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: api
  namespace: ${gatewayNS}
spec:
  parentRefs:
  - name: external
    namespace: ${gatewayNS}
  hostnames:
  - "toys.${rootDomain}"
  rules:
  - backendRefs:
    - name: toystore
      port: 80
EOF
```


Ok lets check our gateway policies are enforced

//TODO describe where to view dashboards

```
kubectl get dnspolicy loadbalanced -n ${gatewayNS} -o=jsonpath='{.status.conditions[?(@.type=="Enforced")].message}'
kubectl get authpolicy external -n ${gatewayNS} -o=jsonpath='{.status.conditions[?(@.type=="Enforced")].message}'
kubectl get ratelimitpolicy external -n ${gatewayNS} -o=jsonpath='{.status.conditions[?(@.type=="Enforced")].message}'

```

note TLS policy is currently missing an enforced condition. https://github.com/Kuadrant/kuadrant-operator/issues/572. However looking at the gateway status we can see it is affected by 

```
kubectl get gateway -n ${gatewayNS} external -n demo -o=jsonpath='{.status.conditions[*].message}'
```

### Test connectivity and deny all auth 

We are using curl to hit our endpoint. As we are using letsencrypt staging in this example we pass the `-k` flag.

```
curl -s -k -o /dev/null -w "%{http_code}"  https://$(k get httproute api -n demo -o=jsonpath='{.spec.hostnames[0]}')

```

### Setup HTTPRoute level RateLimits and Auth

      

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