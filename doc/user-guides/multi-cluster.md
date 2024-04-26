# Secure, Protect and Connect APIs with Kuadrant on Multiple Clusters


## Pre-requisites

This document expects that you have successfully installed Kuadrant [Install Guide](../install/install-openshift.md) onto two different clusters and have configured a shared, accessible redis store. 

- Completed the Kuadrant Install Guide for at least two clusters [Install Guide](../install/install-openshift.md)
- kubectl command line tool
- (optional) have user workload monitoring configured to remote write to a central storage system such as Thanos (also covered in the installation guide).

## Overview

In this doc we will walk you through using Kuadrant to secure, protect and connect an API via a set of Gateways distributed across multiple clusters. 
We will take the approach of assuming certain personas and how they can each work with Kuadrant to achieve their goals.

### What Kuadrant can do for you in a multi-cluster environment

Kuadrant's capabilities can be leveraged in single or multiple clusters. Below is a list of features that are designed to work across multiple clusters as well as in a single cluster environment.

- **Multi-Cluster Ingress:** Kuadrant provides multi-cluster ingress connectivity using DNS to bring traffic to your Gateways using a strategy defined in a `DNSPolicy` (more later). 
- **Global Rate Limiting:** Kuadrant can enable global rate limiting usecases when it is configured to use a shared store (redis) for counters based on limits defined by a `RateLimitPolicy`.
- **Global Auth:*** Kuadrant's `AuthPolicy` can be configured to leverage external auth providers to ensure different cluster exposing the same API are authenticating and authorizing in the same way. 
- **Integration with federated metrics stores:** Kuadrant has example dashboards and metrics that can be used for visualizing your gateways and observing traffic hitting those gateways across multiple clusters. 

**Platform Engineer**

We will walk through deploying a gateway that provides secure communications and is protected and ready to be used by development teams to deploy an API. We will then walk through how you can have this gateway in clusters in different geographic regions and leverage Kuadrant to bring the specific traffic to your geo located gateways to reduce latency and distribute load while still having it protected and secured via global rate limiting and auth.

As an optional extra we will highlight how, with the user workload monitoring observability stack deployed, these gateways can then be observed and monitored. 

**Developer**

We will walk through how you can use the kuadrant OAS extensions and CLI to generate a `HTTPRoute` for your API and add both Auth and Rate Limiting to your API.

## Platform Engineer

The following steps should be done in each cluster individually unless specifically excluded. 

### Environment Variables

For convenience in this guide we use some env vars throughout this document

```bash
export zid=change-this-to-your-zone-id
export rootDomain=example.com
export gatewayNS=api-gateway
export gatewayName=external
export AWS_ACCESS_KEY_ID=xxxx
export AWS_SECRET_ACCESS_KEY=xxxx
export clusterIssuerName=lets-encrypt
export EMAIL=foo@example.com
```

### Tooling

While this document uses kubectl, working with multiple clusters is complex and so we would recommend looking into something like ArgoCD to manage the deployment of resources etc to multiple clusters.

### Setup a managed DNS zone

The managed dns zone declares a zone and credentials to access that zone that can be used by Kuadrant to setup DNS configuration.

**Create the ManagedZone resource**

Ensure your kubectl is targeting the correct cluster. Apply the `ManagedZone` resource below to each cluster or if you are adding an additional cluster add it to the new cluster:

```bash
kubectl create ns ${gatewayNS}
```

Setup AWS credential for route53 access

```bash
kubectl -n ${gatewayNS} create secret generic aws-credentials \
  --type=kuadrant.io/aws \
  --from-literal=AWS_ACCESS_KEY_ID=$AWS_ACCESS_KEY_ID \
  --from-literal=AWS_SECRET_ACCESS_KEY=$AWS_SECRET_ACCESS_KEY
```  


```bash
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

```bash
kubectl wait managedzone/managedzone --for=condition=ready=true -n ${gatewayNS}
```


### Add a TLS Issuer

To secure communication to the gateways we want to define a TLS issuer for TLS certificates. We will use letsencrypt, but you can use any supported by cert-manager.

```bash
kubectl apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: ${clusterIssuerName}
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

In order for Kuadrant to balance traffic using DNS across two or more clusters. We need to define a gateway with a shared host. We will define this with a HTTPS listener with a wildcard hostname based on the root domain. As mentioned, these resources need to be applied to both clusters. Note for now we have set the gateway to only accept HTTPRoutes from the same namespace. This will allow us to restrict who can use the gateway until it is ready for general use.

```bash
kubectl apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: ${gatewayName}
  namespace: ${gatewayNS}
spec:
    gatewayClassName: istio
    listeners:
    - allowedRoutes:
        namespaces:
          from: Same
      hostname: "*.${rootDomain}"
      name: api
      port: 443
      protocol: HTTPS
      tls:
        certificateRefs:
        - group: ""
          kind: Secret
          name: api-${gatewayName}-tls
        mode: Terminate
EOF
```

Let check the status of our gateway

```bash
kubectl get gateway ${gatewayName} -n ${gatewayNS} -o=jsonpath='{.status.conditions[?(@.type=="Accepted")].message}'
kubectl get gateway ${gatewayName} -n ${gatewayNS} -o=jsonpath='{.status.conditions[?(@.type=="Programmed")].message}'
```

So our gateway should be accepted and programmed (IE valid and assigned an external address).

However if we check our listener status we will it is not yet "programmed" or ready to accept traffic due to bad TLS configuration.

```bash
kubectl get gateway ${gatewayName} -n ${gatewayNS} -o=jsonpath='{.status.listeners[0].conditions[?(@.type=="Programmed")].message}'
```

Kuadrant can help with this via TLSPolicy.

### Secure and Protect the Gateway with TLS Rate Limiting and Auth policies.

While our gateway is now deployed it has no exposed endpoints and our listener is not programmed. So lets set up a `TLSPolicy` that leverages our CertificateIssuer to setup our listener certificates. Also lets define an `AuthPolicy` that will setup a default 403 response for any unprotected endpoints and a `RateLimitPolicy` that will setup a default (artificially) low global limit to further protect any endpoints exposed by this gateway.


AuthPolicy

```bash
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1beta2
kind: AuthPolicy
metadata:
  name: ${gatewayName}-auth
  namespace: ${gatewayNS}
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: ${gatewayName}
  defaults:
    rules:
      authorization:
        "deny":
          opa:
            rego: "allow = false"
EOF
```

Lets check our policy was accepted by the controller

```bash
kubectl get authpolicy ${gatewayName}-auth -n ${gatewayNS} -o=jsonpath='{.status.conditions[?(@.type=="Accepted")].message}'
```

TLSPolicy

```bash
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1alpha1
kind: TLSPolicy
metadata:
  name: ${gatewayName}-tls
  namespace: ${gatewayNS}
spec:
  targetRef:
    name: ${gatewayName}
    group: gateway.networking.k8s.io
    kind: Gateway
  issuerRef:
    group: cert-manager.io
    kind: ClusterIssuer
    name: lets-encrypt
EOF
```

Lets check our policy was accepted by the controller

```bash
kubectl get tlspolicy ${gatewayName}-tls -n ${gatewayNS} -o=jsonpath='{.status.conditions[?(@.type=="Accepted")].message}'
```


RateLimitPolicy

```bash
kubectl apply -f  - <<EOF
apiVersion: kuadrant.io/v1beta2
kind: RateLimitPolicy
metadata:
  name: ${gatewayName}-rlp
  namespace: ${gatewayNS}
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: ${gatewayName}
  defaults:
    limits:
      "low-limit":
        rates:
        - limit: 2
          duration: 10
          unit: second
EOF
```


Lets check our rate limits have been accepted. Note we have set it artificially low for demo purposes.

```bash
kubectl get ratelimitpolicy ${gatewayName}-rlp -n ${gatewayNS} -o=jsonpath='{.status.conditions[?(@.type=="Accepted")].message}'
```

Lets check the programmed state of our gateway listener again.

```bash
kubectl get gateway ${gatewayName} -n ${gatewayNS} -o=jsonpath='{.status.listeners[0].conditions[?(@.type=="Programmed")].message}'
```

Should have no errors anymore. Note it can take a minute or two for the letsencrypt cert to be issued.

### Setup our DNS

So with our gateway deployed, secured and protected, next we will apply a `DNSPolicy` to bring traffic to our gateway for the assigned listener hosts. This policy will configure how traffic reaches the gateways deployed to our different clusters. In this case it will setup a loadbalanced strategy, which will mean it will provide a form of RoundRobin response to DNS clients. We also define default GEO, this doesn't have an immediate impact but rather is a "catchall" to put records under and so that when/if we enable geo routing on our gateways (covered later), the default is defined for any users outside of the specified gateway GEOs ensuring all users regardless of their geo will be able to reach our gateway (more later). We also define a default weight. All records will receive this weight meaning they will be returned in a RoundRobin manner.

```bash
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1alpha1
kind: DNSPolicy
metadata:
  name: ${gatewayName}-dnspolicy
  namespace: ${gatewayNS}
spec:
  routingStrategy: loadbalanced
  loadBalancing:
    geo: 
      defaultGeo: US 
    weighted:
      defaultWeight: 120 
  targetRef:
    name: ${gatewayName}
    group: gateway.networking.k8s.io
    kind: Gateway
EOF
```    
Note: the DNSPolicy will leverage the ManagedZone we defined earlier based on the listener hosts defined in the gateway.

Lets check our DNSPolicy has been accepted:

```bash
kubectl get dnspolicy ${gatewayName}-dnspolicy -n ${gatewayNS} -o=jsonpath='{.status.conditions[?(@.type=="Accepted")].message}'
```

If you have setup the observability pieces (See installation) and remote write to a Thanos instance, then you should be able to access the Grafana instance and see your deployed gateway and policies in the `platform` engineer dashboard.

## Platform Engineer review

So far we have setup an external gateway, secured it with TLS, Protected all endpoints with a default `DENY ALL` AuthPolicy added a restrictive default RateLimitPolicy and set up ManagedZone and a DNSPolicy to ensure traffic is brought to the gateway for the listener hosts defined. Our gateway is now ready to start accepting traffic.

Once we create a HTTPRoute for our listeners, it will cause the DNSPolicy, Auth and RateLimitPolicy to be `Enforced`. So DNS records will populate auth and rate limiting will be configured and ready to protect requests to that endpoint. 

We can test this by deploying a simple application and connecting it to our gateway.

```bash
kubectl apply -f https://raw.githubusercontent.com/Kuadrant/Kuadrant-operator/main/examples/toystore/toystore.yaml -n ${gatewayNS}
```

add an `HTTPRoute`:

```bash
kubectl apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: api
  namespace: ${gatewayNS}
spec:
  parentRefs:
  - name: ${gatewayName}
    namespace: ${gatewayNS}
  hostnames:
  - "toys.${rootDomain}"
  rules:
  - backendRefs:
    - name: toystore
      port: 80
EOF
```

Let's check our gateway policies are enforced:

```bash
kubectl get dnspolicy loadbalanced -n ${gatewayNS} -o=jsonpath='{.status.conditions[?(@.type=="Enforced")].message}'
kubectl get authpolicy ${gatewayName}-auth -n ${gatewayNS} -o=jsonpath='{.status.conditions[?(@.type=="Enforced")].message}'
kubectl get ratelimitpolicy ${gatewayName}-rlp -n ${gatewayNS} -o=jsonpath='{.status.conditions[?(@.type=="Enforced")].message}'
```

note TLS policy is currently missing an enforced condition. https://github.com/Kuadrant/kuadrant-operator/issues/572. However looking at the gateway status we can see it is affected by 

```bash
kubectl get gateway -n ${gatewayNS} ${gatewayName} -n ${gatewayNS} -o=jsonpath='{.status.conditions[*].message}'
```

### Test connectivity and deny all auth 

We are using curl to hit our endpoint. As we are using letsencrypt staging in this example we pass the `-k` flag.

```bash
curl -k -w "%{http_code}" https://$(kubectl get httproute api -n ${gatewayNS} -o=jsonpath='{.spec.hostnames[0]}')
```

We should see a `403` response. With our gateway and policies in place we can now allow other teams to use the gateway:

```bash
kubectl patch gateway ${gatewayName} -n ${gatewayNS} --type='json' -p='[{"op": "replace", "path": "/spec/listeners/0/allowedRoutes/namespaces/from", "value":"All"}]'
```

### Extending this Gateway to multiple clusters and configuring GEO based routing

In order to have this gateway distributed across multiple clusters, we would follow the above instructions for each cluster as noted at the start. By default that would set up a `RoundRobin` DNS strategy to bring traffic to the different clusters. Enabling our gateways to serve clients based on their GEO is relatively straight forward based on our current configuration.

Assuming you have deployed a gateway instance to multiple clusters and configured it based on this document. The next step is to inform the DNS controller about what Geographic region the gateways it can see are in.

So for example if we have a cluster in North America and a Cluster in the EU we can bring traffic to those gateways based on location simply by applying the following label:

In our North American cluster:

```bash
kubectl label --overwrite gateway ${gatewayName} kuadrant.io/lb-attribute-geo-code=US -n ${gatewayNS}
```

In our European Cluster

```bash
kubectl label --overwrite gateway ${gatewayName} kuadrant.io/lb-attribute-geo-code=EU -n ${gatewayNS}
```


After some time you can check the geo distribution using the HTTPRoute host `kubectl get httproute api -n ${gatewayNS} -o=jsonpath='{.spec.hostnames[0]}'` via site such as https://dnsmap.io/

## Developer

For this part of the walkthrough, we will go through leveraging an Open API Spec (OAS) to define an API and also using the powerful kuadrant OAS extensions to define the routing, auth and rate limiting requirements. We will then use the `kuadrantctl` tool to generate an AuthPolicy that uses an OpenId provider and a RateLimitPolicy. 
Durig the platform engineer section we defined some default policies for auth and rate limiting at our gateway, these new developer defined policies will target our APIs HTTPRoute and override the policies for requests to our API endpoints.

Our example Open Api Spec (OAS) leverages kuadrant based extensions. It is these extension that allow you to define routing, and service protection requirements. You can learn more about these extension [here](https://docs.kuadrant.io/kuadrantctl/doc/openapi-kuadrant-extensions/) 

### Pre Reqs

- Install kuadrantctl. You can find a compatible binary and download it from the [kuadrantctl releases page](https://github.com/kuadrant/kuadrantctl/releases )
- Setup / have an available openid connect provider such as https://www.keycloak.org/


### Setup HTTPRoute and backend

Copy the following example to a local location:
[sample OAS spec](../../examples/oas.yaml)

setup some new env vars:

```
export openIDHost=some.keycloak.com
export oasPath=/path/to/oas.yaml
export apiNS=petstore
```

Deploy the sample application:

```sh
kubectl create ns petstore
kubectl apply -f https://raw.githubusercontent.com/Kuadrant/api-petstore/main/resources/app.yaml
```

### Use OAS to define our routing

##TODO (is it worth doing openid connect here given the custom config needed)

Ok next we are going to use our OAS to configure our HTTPRoute. Lets use the kuadrantctl to generate our `HTTPRoute`

**Note the sample OAS has some placeholders for namespaces and domains.**

Replace the placeholders:

```
sed -i -e "s/#gatewayNS/$gatewayNS/g" $oasPath
sed -i -e "s/#rootDomain/$rootDomain/g" $oasPath
sed -i -e "s/#openIDHost/$openIDHost/g" $oasPath

kuadrantctl generate gatewayapi httproute --oas=$oasPath | jq -P
```
Happy with the output lets apply to the cluster

```
kuadrantctl generate gatewayapi httproute --oas=$oasPath | kubectl apply -f -
```

Lets check out new route

```
kubectl get httproute -n $apiNS -o=yaml

```

We should see that this route is affected by the `AuthPolicy` and `RateLimitPolicy` on the gateway.

```
- lastTransitionTime: "2024-04-26T13:37:43Z"
        message: Object affected by AuthPolicy demo/external
        observedGeneration: 2
        reason: Accepted
        status: "True"
        type: kuadrant.io/AuthPolicyAffected
- lastTransitionTime: "2024-04-26T14:07:28Z"
        message: Object affected by RateLimitPolicy demo/external
        observedGeneration: 1
        reason: Accepted
        status: "True"
        type: kuadrant.io/RateLimitPolicyAffected        
```        


### Test connectivity and deny all auth 

We are using curl to hit our endpoint. As we are using letsencrypt staging in this example we pass the `-k` flag.

```
curl -s -k -o /dev/null -w "%{http_code}"  https://$(k get httproute petstore -n ${apiNS} -o=jsonpath='{.spec.hostnames[0]}')

```

So we are getting a `403` because of the existing default auth policy.

### Setup HTTPRoute level RateLimits and Auth


Lets use our OAS and kuadrantctl to generate an AuthPolicy to replace the default on the Gateway.

```
kuadrantctl generate kuadrant authpolicy --oas=$oasPath | jq -P

```
Happy with the output lets apply to the cluster

```
kuadrantctl generate kuadrant authpolicy --oas=$oasPath | kubectl apply -f -
```


# TODO
- Add developer flow with OAS
- Define developer focused policies
- Add instructions for using non API Key auth provider
