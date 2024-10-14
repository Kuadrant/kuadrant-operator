# Secure, protect, and connect services with Kuadrant on Kubernetes

## Prerequisites

- You have completed the [Single-cluster Quick Start](https://docs.kuadrant.io/latest/getting-started-single-cluster/) or [Multi-cluster Quick Start](https://docs.kuadrant.io/latest/getting-started-multi-cluster/).


### Local Cluster (metallb)

>**Note:** If you are running on a local kind cluster, it is also recommended you use [metallb](https://metallb.universe.tf/) to setup an IP address pool to use with loadbalancer services for your gateways. An example script for configuring metallb based on the docker network once installed can be found [here](https://github.com/Kuadrant/kuadrant-operator/blob/main/utils/docker-network-ipaddresspool.sh).

```
./utils/docker-network-ipaddresspool.sh kind yq 1 | kubectl apply -n metallb-system -f -
```

## Overview

In this guide, we will cover the different policies from Kuadrant and how you can use them to secure, protect and connect an Istio-controlled gateway in a single cluster, and how you can set more refined protection on the HTTPRoutes exposed by that gateway.

Here are the steps we will go through:

1) [Deploy a sample application](#deploy-the-example-app-we-will-serve-via-our-gateway)


2) [Define a new Gateway](#define-a-new-istio-managed-gateway)


3) [Ensure TLS-based secure connectivity to the gateway with a TLSPolicy](#define-the-tlspolicy)


4) [Define a default RateLimitPolicy to set some infrastructure limits on your gateway](#define-infrastructure-rate-limiting)


5) [Define a default AuthPolicy to deny all access to the gateway](#define-the-gateway-authpolicy)


6) [Define a DNSPolicy to bring traffic to the gateway](#define-the-dnspolicy)


7) [Override the Gateway's deny-all AuthPolicy with an endpoint-specific policy](#override-the-gateways-deny-all-authpolicy)


8) [Override the Gateway rate limits with an endpoint-specific policy](#override-the-gateways-ratelimitpolicy)


You will need to set the `KUBECTL_CONTEXT` environment variable for the kubectl context of the cluster you are targeting.
If you have followed the single cluster setup, it should be something like below.
Adjust the name of the cluster accordingly to match the kubernetes cluster you are targeting.
You can get the current context with `kubectl config current-context`

```sh
# Typical single cluster context
export KUBECTL_CONTEXT=kind-kuadrant-local

# Example context for additional 'multi cluster' clusters
# export KUBECTL_CONTEXT=kind-kuadrant-local-1
```

To help with this walk through, you should also set a `KUADRANT_ZONE_ROOT_DOMAIN` environment variable to a domain you want to use. If you want to try DNSPolicy, this should also be a domain you have access to the DNS for in AWS Route53 or GCP. E.g.:

```sh
export KUADRANT_ZONE_ROOT_DOMAIN=my.domain.iown
```

### ❶ Deploy the example app we will serve via our gateway

```sh
kubectl --context $KUBECTL_CONTEXT apply -f https://raw.githubusercontent.com/Kuadrant/kuadrant-operator/main/examples/toystore/toystore.yaml
```

### ❷ Define a new Istio-managed gateway

```sh
kubectl --context $KUBECTL_CONTEXT apply -f - <<EOF
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

If you take a look at the gateway status, you will see a TLS status error similar to the following:

```
message: invalid certificate reference /Secret/apps-hcpapps-tls. secret kuadrant-system/apps-hcpapps-tls not found
```

This is because currently there is not a TLS secret in place. Let's fix that by creating a TLSPolicy.

### ❸ Define the TLSPolicy

```sh
kubectl --context $KUBECTL_CONTEXT apply -f - <<EOF
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
    name: kuadrant-operator-glbc-ca
EOF
```

> **Note:** You may have to create a cluster issuer in the Kubernetes cluster, depending on if one was created during your initial cluster setup or not. Here is an example of how to create a self-signed CA as a cluster issuer. This is a self signed issuer for simplicity, but you can use other issuers such as [letsencrypt](https://letsencrypt.org/). Refer to the [cert-manager docs](https://cert-manager.io/docs/configuration/acme/dns01/route53/#iam-user-with-long-term-access-key)

```sh
kubectl --context $KUBECTL_CONTEXT apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: kuadrant-operator-glbc-ca
spec:
  selfSigned: {}
EOF
```

The TLSPolicy should eventually have an `Accepted` condition.

```sh
kubectl --context $KUBECTL_CONTEXT wait tlspolicy api-gateway-tls -n kuadrant-system --for=condition=accepted
```

Now, if you look at the status of the gateway, you will see the error is gone, and the status of the policy will report the listener as now secured with a TLS certificate and the gateway as affected by the TLS policy.

Our communication with our gateway is now secured via TLS. Note that any new listeners will also be handled by the TLSPolicy.

Let's define a HTTPRoute and test our policy. We will re-use this later on with some of the other policies as well.

```sh
kubectl --context $KUBECTL_CONTEXT apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1beta1
kind: HTTPRoute
metadata:
  name: toystore
  labels:
    deployment: toystore
    service: toystore
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

With this HTTPRoute in place, the service we deployed is exposed via the gateway. We should be able to access our endpoint via HTTPS:

```sh
export INGRESS_HOST=$(kubectl --context $KUBECTL_CONTEXT get gtw api-gateway -o jsonpath='{.status.addresses[0].value}' -n kuadrant-system)

curl -k --resolve api.${KUADRANT_ZONE_ROOT_DOMAIN}:443:${INGRESS_HOST} "https://api.$KUADRANT_ZONE_ROOT_DOMAIN/cars"
```

### ❹ Define Infrastructure Rate Limiting

We have a secure communication in place. However, there is nothing limiting users from overloading our infrastructure and service components that will sit behind this gateway. Let's add a rate limiting layer to protect our services and infrastructure.

```sh
kubectl --context $KUBECTL_CONTEXT apply -f - <<EOF
apiVersion: kuadrant.io/v1beta3
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

kubectl --context $KUBECTL_CONTEXT wait ratelimitpolicy infra-ratelimit -n kuadrant-system --for=condition=accepted
```

> **Note:** It may take a couple of minutes for the RateLimitPolicy to be applied depending on your cluster.

The limit here is artificially low in order for us to show it working easily. Let's test it with our endpoint:

```sh
for i in {1..10}; do curl -k --resolve api.${KUADRANT_ZONE_ROOT_DOMAIN}:443:${INGRESS_HOST} "https://api.$KUADRANT_ZONE_ROOT_DOMAIN/cars" && sleep 1; done
```

We should see `409 Too Many Requests`s start returning after the 5th request.

### ❺ Define the Gateway AuthPolicy

Communication is secured and we have some protection for our infrastructure, but we do not trust any client to access our endpoints. By default, we want to allow only authenticated access. To protect our gateway, we will add a _deny-all_ AuthPolicy. Later, we will override this with a more specific AuthPolicy for the API.

```sh
kubectl --context $KUBECTL_CONTEXT apply -f - <<EOF
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

Let's test it again. This time we expect a `403 Forbidden`.

```sh
curl -k --resolve api.${KUADRANT_ZONE_ROOT_DOMAIN}:443:${INGRESS_HOST}  "https://api.$KUADRANT_ZONE_ROOT_DOMAIN/cars"
```

### ❻ Define the DNSPolicy

(Skip this step if you did not configure a DNS provider during the setup.)

Now, we have our gateway protected and communications secured. We are ready to configure DNS, so it is easy for clients to connect and access the APIs we intend to expose via this gateway.

> **Note:** You may need to create a DNS Provider Secret resource depending on if one was created during your initial cluster setup or not. You should have an `aws-credentials` Secret already created in the `kuadrant-system` namespace. However, if it doesn't exist, you can follow these commands to create one:

```sh
export AWS_ACCESS_KEY_ID=xxxxxxx # Key ID from AWS with Route 53 access
export AWS_SECRET_ACCESS_KEY=xxxxxxx # Access key from AWS with Route 53 access

kubectl -n kuadrant-system create secret generic aws-credentials \
  --type=kuadrant.io/aws \
  --from-literal=AWS_ACCESS_KEY_ID=$AWS_ACCESS_KEY_ID \
  --from-literal=AWS_SECRET_ACCESS_KEY=$AWS_SECRET_ACCESS_KEY
```

Next, create the DNSPolicy. There are two options here. 

1. **Single gateway with no shared hostnames:**

```sh
kubectl --context $KUBECTL_CONTEXT apply -f - <<EOF
apiVersion: kuadrant.io/v1alpha1
kind: DNSPolicy
metadata:
  name: simple-dnspolicy
  namespace: kuadrant-system
spec:
  targetRef:
    name: api-gateway
    group: gateway.networking.k8s.io
    kind: Gateway
  providerRefs:
  - name: aws-credentials
EOF
```
2. **multiple gateways with shared hostnames:**

If you want to use a gateway with a shared listener host (IE the same hostname on more than one gateway instance). Then you should use the following configuration:

> **Note:** This configuration will work fine for a single gateway also, but does create some additional records.

```sh
kubectl --context $KUBECTL_CONTEXT apply -f - <<EOF
apiVersion: kuadrant.io/v1alpha1
kind: DNSPolicy
metadata:
  name: simple-dnspolicy
  namespace: kuadrant-system
spec:
  targetRef:
    name: api-gateway
    group: gateway.networking.k8s.io
    kind: Gateway
  providerRefs:
    - name: aws-credentials
  loadBalancing:
    weight: 120
    geo: EU
    defaultGeo: true
EOF
```    

The loadbalancing section here has the following attributes:

- **Weight:** This will be the weighting used for records created for hosts defined by this gateway. It will decide how often the records for this gateway are returned. If you have 2 gateways and each DNSPolicy specifies the same weight, then you will get an even distribution. If you define one weight as larger than the other, the gateway with the larger weight will receive more traffic (record weight / sum of all records).
- **geo:** This will be the geo used to decide whether to return records defined for this gateway based on the requesting client's location. This should be set even if you have one gateway in a single geo. 
- **defaultGeo:** For Azure and AWS, this will decide, if there should be a default geo. A default geo acts as a "catch-all" (GCP always sets a catch-all) for clients outside of the defined geo locations. There can only be one default value and so it is important you set `defaultGeo` as true for **one** and **only one** geo code for each of the gateways in that geo. 

Wait for the DNSPolicy to marked as enforced:

```
kubectl --context $KUBECTL_CONTEXT wait dnspolicy simple-dnspolicy -n kuadrant-system --for=condition=enforced
```

If you want to see the actual DNSRecord created by the this policy, execute the following command:

> **Note:** This resource is managed by kuadrant and so shouldn't be changed directly.

```sh
kubectl --context $KUBECTL_CONTEXT get dnsrecord.kuadrant.io api-gateway-api -n kuadrant-system -o=yaml
```


With DNS in place, let's test it again. This time we expect a `403` still as the _deny-all_ policy is still in effect. Notice we no longer need to set the Host header directly.

> **Note:** If you have followed through this guide on more than 1 cluster, the DNS record for the HTTPRoute hostname will have multiple IP addresses. This means that requests will be made in a round robin pattern across clusters as your DNS provider sends different responses to lookups. You may need to send multiple requests before one hits the cluster you are currently configuring.

```sh
curl -k "https://api.$KUADRANT_ZONE_ROOT_DOMAIN/cars" -i
```

### ❼ Override the Gateway's deny-all AuthPolicy

Next, we are going to allow authenticated access to our Toystore API. To do this, we will define an AuthPolicy that targets the HTTPRoute. Note that any new HTTPRoutes will still be affected by the gateway-level policy, but as we want users to now access this API, we need to override that policy. For simplicity, we will use API keys to authenticate the requests, though many other options are available.

Let's define an API Key for users **bob** and **alice**.

```sh
kubectl --context $KUBECTL_CONTEXT apply -f - <<EOF
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

Now, we will override the AuthPolicy to start accepting the API keys:

```sh
kubectl --context $KUBECTL_CONTEXT apply -f - <<EOF
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
    response:
      success:
        dynamicMetadata:
          "identity":
            json:
              properties:
                "userid":
                  selector: auth.identity.metadata.annotations.secret\.kuadrant\.io/user-id
EOF
```

### ❽ Override the Gateway's RateLimitPolicy

The gateway limits are a good set of limits for the general case, but as the developers of this API we know that we only want to allow a certain number of requests to specific users, and a general limit for all other users.

```sh
kubectl --context $KUBECTL_CONTEXT apply -f - <<EOF
apiVersion: kuadrant.io/v1beta3
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
      counters:
      - metadata.filter_metadata.envoy\.filters\.http\.ext_authz.identity.userid
      when:
      - selector: metadata.filter_metadata.envoy\.filters\.http\.ext_authz.identity.userid
        operator: neq
        value: bob
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

> **Note:** It may take a couple of minutes for the RateLimitPolicy to be applied depending on your cluster.

As just another example, we have given **bob** twice as many requests to use compared to everyone else.

Let's test this new setup.

By sending requests as **alice**:

```sh
while :; do curl -k --resolve api.${KUADRANT_ZONE_ROOT_DOMAIN}:443:${INGRESS_HOST} --write-out '%{http_code}\n' --silent --output /dev/null -H 'Authorization: APIKEY IAMALICE' "https://api.$KUADRANT_ZONE_ROOT_DOMAIN/cars" | grep -E --color "\b(429)\b|$"; sleep 1; done
```

By sending requests as **bob**:

```sh
while :; do curl -k --resolve api.${KUADRANT_ZONE_ROOT_DOMAIN}:443:${INGRESS_HOST} --write-out '%{http_code}\n' --silent --output /dev/null -H 'Authorization: APIKEY IAMBOB' "https://api.$KUADRANT_ZONE_ROOT_DOMAIN/cars" | grep -E --color "\b(429)\b|$"; sleep 1; done
```

> **Note:** If you configured a DNS provider during the setup and defined the DNSPolicy as described in one of the previous chapters you can omit the `--resolve api.${KUADRANT_ZONE_ROOT_DOMAIN}:443:${INGRESS_HOST}` flag.

> **Note:** If you have followed through this guide on more than 1 cluster, the DNS record for the HTTPRoute hostname will have multiple IP addresses. This means that requests will be made in a round robin pattern across clusters as your DNS provider sends different responses to lookups.

```sh
while :; do curl -k --write-out '%{http_code}\n' --silent --output /dev/null -H 'Authorization: APIKEY IAMALICE' "https://api.$KUADRANT_ZONE_ROOT_DOMAIN/cars" | grep -E --color "\b(429)\b|$"; sleep 1; done
```

```sh
while :; do curl -k --write-out '%{http_code}\n' --silent --output /dev/null -H 'Authorization: APIKEY IAMBOB' "https://api.$KUADRANT_ZONE_ROOT_DOMAIN/cars" | grep -E --color "\b(429)\b|$"; sleep 1; done
```
