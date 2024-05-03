# Secure, Protect, and Connect APIs with Kuadrant

## Overview

This guide walks you through using Kuadrant to secure, protect, and connect an API exposed by a Gateway using Kubernetes Gateway API. You can use this walkthrough for a Gateway on a single cluster or a Gateway distributed across multiple clusters with a shared listener hostname. This guide shows how specific user personas can each use Kuadrant to achieve their goals.

## Pre-requisites

This document expects that you have successfully installed Kuadrant [Install Guide](../install/install-openshift.md) onto at least one cluster. If you are looking at multicluster scenarios, follow the install guide on at least two different clusters and have a shared, accessible Redis store.

- Completed the Kuadrant Install Guide for one or more clusters [Install Guide](../install/install-openshift.md)
- `kubectl` command line tool
- (optional) have user workload monitoring configured to remote write to a central storage system such as Thanos (also covered in the installation guide).

### What Kuadrant can do for you in a multi-cluster environment

Kuadrant's capabilities can be leveraged in single or multiple clusters. Below is a list of features that are designed to work across multiple clusters as well as in a single-cluster environment.

- **Multi-Cluster Ingress:** Kuadrant provides multi-cluster ingress connectivity using DNS to bring traffic to your Gateways using a strategy defined in a `DNSPolicy` (more later). 
- **Global Rate Limiting:** Kuadrant can enable global rate limiting use cases when it is configured to use a shared store (redis) for counters based on limits defined by a `RateLimitPolicy`.
- **Global Auth:*** Kuadrant's `AuthPolicy` can be configured to leverage external auth providers to ensure different clusters exposing the same API are authenticating and authorizing in the same way. 
- **Integration with federated metrics stores:** Kuadrant has example dashboards and metrics that can be used for visualizing your gateways and observing traffic hitting those gateways across multiple clusters. 

**Platform Engineer**

We will walk through deploying a gateway that provides secure communications and is protected and ready to be used by development teams to deploy an API. We will then walk through how you can have this gateway in clusters in different geographic regions and leverage Kuadrant to bring the specific traffic to your geo-located gateways to reduce latency and distribute load while still having it protected and secured via global rate limiting and auth.

As an optional extra we will highlight how, with the user workload monitoring observability stack deployed, these gateways can then be observed and monitored. 

**Developer**

We will walk through how you can use the kuadrant OAS extensions and CLI to generate an `HTTPRoute` for your API and add specific Auth and Rate Limiting requirements to your API.

## Platform Engineer

The following steps should be done in each cluster individually unless specifically excluded. 

### Environment Variables

For convenience in this guide, we use some env vars throughout this document:

```bash
export zid=change-this-to-your-zone-id
export rootDomain=example.com
export gatewayNS=api-gateway
export gatewayName=external
export devNS=toystore
export AWS_ACCESS_KEY_ID=xxxx
export AWS_SECRET_ACCESS_KEY=xxxx
export AWS_REGION=us-east-1
export clusterIssuerName=lets-encrypt
export EMAIL=foo@example.com
```

### Deployment management tooling

While this document uses `kubectl`, working with multiple clusters is complex, and it is best to use a tool such as Argo CD to manage the deployment of resources to multiple clusters.
### Set up a managed DNS zone

The managed DNS zone declares a zone and credentials to access that zone that Kuadrant can use to set up DNS configuration.

**Create the ManagedZone resource**

Apply the following `ManagedZone` resource to each cluster or, if you are adding an additional cluster, add it to the new cluster:

```bash
kubectl create ns ${gatewayNS}
Then create a `ManagedZone`:

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

Wait for the `ManagedZone` to be ready in your cluster(s):

```bash
kubectl wait managedzone/managedzone --for=condition=ready=true -n ${gatewayNS}
```

### Add a TLS Issuer

To secure communication to the Gateways, we will to define a TLS issuer for TLS certificates. We will use LetsEncrypt here, but you can use any supported by `cert-manager`.

Below is an example that uses LetsEncrypt staging: This should also be applied to all clusters.


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
            region: ${AWS_REGION}
            accessKeyIDSecretRef:
              key: AWS_ACCESS_KEY_ID
              name: aws-credentials
            secretAccessKeySecretRef:
              key: AWS_SECRET_ACCESS_KEY
              name: aws-credentials
EOF
```

Then wait for the `ClusterIssuer` to become ready:

```bash
kubectl wait clusterissuer/${clusterIssuerName} --for=condition=ready=true
```

### Setup a Gateway

For Kuadrant to balance traffic using DNS across two or more clusters, we need to define a Gateway with a shared host. We will define this with a HTTPS listener with a wildcard hostname based on the root domain. As mentioned earlier, these resources need to be applied to all clusters. 

**Note:** for now we have set the Gateway to only accept `HTTPRoute`'s from the same namespace. This will allow us to restrict who can use the Gateway until it is ready for general use.

```bash
kubectl apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: ${gatewayName}
  namespace: ${gatewayNS}
  labels:
    kuadrant.io/gateway: "true"
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

Check the status of your Gateway:

```bash
kubectl get gateway ${gatewayName} -n ${gatewayNS} -o=jsonpath='{.status.conditions[?(@.type=="Accepted")].message}'
kubectl get gateway ${gatewayName} -n ${gatewayNS} -o=jsonpath='{.status.conditions[?(@.type=="Programmed")].message}'
```

Our gateway should be accepted and programmed (i.e. valid and assigned an external address).

However, if we check our listener status we will it is not yet "programmed" or ready to accept traffic due to bad TLS configuration.

```bash
kubectl get gateway ${gatewayName} -n ${gatewayNS} -o=jsonpath='{.status.listeners[0].conditions[?(@.type=="Programmed")].message}'
```

Kuadrant can help with this via TLSPolicy.

### Secure and protect the Gateway with TLS rate limiting and auth policies.

While your Gateway is now deployed, it has no exposed endpoints and your listener is not programmed. Next, you can set up a `TLSPolicy` that leverages your CertificateIssuer to set up your listener certificates. 

You will also define an `AuthPolicy` that will set up a default `403` response for any unprotected endpoints, as well as a `RateLimitPolicy` that will set up a default artificially low global limit to further protect any endpoints exposed by this Gateway.

Set up a default, deny-all `AuthPolicy` for your Gateway as follows:

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

Let's check our policy was accepted by the controller:

```bash
kubectl get authpolicy ${gatewayName}-auth -n ${gatewayNS} -o=jsonpath='{.status.conditions[?(@.type=="Accepted")].message}'
```

Setup a `TLSPolicy` for our Gateway:

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
    name: ${clusterIssuerName}
EOF
```

Check that your policy was accepted by the controller:

```bash
kubectl get tlspolicy ${gatewayName}-tls -n ${gatewayNS} -o=jsonpath='{.status.conditions[?(@.type=="Accepted")].message}'
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


To check your rate limits have been accepted, enter the following command:

```bash
kubectl get ratelimitpolicy ${gatewayName}-rlp -n ${gatewayNS} -o=jsonpath='{.status.conditions[?(@.type=="Accepted")].message}'
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

NOTE:  The `DNSPolicy` will leverage the `ManagedZone` that you defined earlier based on the listener hosts defined in the Gateway.

Check that your `DNSPolicy` has been accepted:

```bash
kubectl get dnspolicy ${gatewayName}-dnspolicy -n ${gatewayNS} -o=jsonpath='{.status.conditions[?(@.type=="Accepted")].message}'

```bash
kubectl apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: test
  namespace: ${gatewayNS}
spec:
  parentRefs:
  - name: ${gatewayName}
    namespace: ${gatewayNS}
  hostnames:
  - "test.${rootDomain}"
  rules:
  - backendRefs:
    - name: toystore
      port: 80
EOF
```

Check your Gateway policies are enforced:

```bash
kubectl get dnspolicy ${gatewayName}-dnspolicy -n ${gatewayNS} -o=jsonpath='{.status.conditions[?(@.type=="Enforced")].message}'
kubectl get authpolicy ${gatewayName}-auth -n ${gatewayNS} -o=jsonpath='{.status.conditions[?(@.type=="Enforced")].message}'
kubectl get ratelimitpolicy ${gatewayName}-rlp -n ${gatewayNS} -o=jsonpath='{.status.conditions[?(@.type=="Enforced")].message}'
### Test connectivity and deny all auth 

You can use `curl` to hit your endpoint. Because this example uses Let's Encrypt staging, you can pass the `-k` flag:

```bash
curl -k -w "%{http_code}" https://$(kubectl get httproute test -n ${gatewayNS} -o=jsonpath='{.spec.hostnames[0]}')
### Extending this Gateway to multiple clusters and configuring geo-based routing

To distribute this Gateway across multiple clusters, repeat this setup process for each cluster. By default, this will implement a round-robin DNS strategy to distribute traffic evenly across the different clusters. Setting up your Gateways to serve clients based on their geographic location is straightforward with your current configuration.

Assuming you have deployed Gateway instances across multiple clusters as per this guide, the next step involves updating the DNS controller with the geographic regions of the visible Gateways.

For instance, if you have one cluster in North America and another in the EU, you can direct traffic to these Gateways based on their location by applying the appropriate labels:

For your North American cluster:

```bash
kubectl label --overwrite gateway ${gatewayName} kuadrant.io/lb-attribute-geo-code=US -n ${gatewayNS}
## Application developer workflow

This section of the walkthrough focuses on using an OpenAPI Specification (OAS) to define an API. You will use Kuadrant OAS extensions to specify the routing, authentication, and rate-limiting requirements. Next, you will use the `kuadrantctl` tool to generate an `AuthPolicy`, an `HTTPRoute`, and a `RateLimitPolicy`, which you will then apply to your cluster to enforce the settings defined in your OAS.

NOTE: While this section uses the `kuadrantctl` tool, this is not essential. You can also create and apply an `AuthPolicy`, `RateLimitPolicy`, and `HTTPRoute` by using the `oc` or `kubectl` commands.

To begin, you will deploy a new version of the `toystore` app to a developer namespace:

```bash
kubectl apply -f https://raw.githubusercontent.com/Kuadrant/Kuadrant-operator/main/examples/toystore/toystore.yaml -n ${devNS}

### Prerequisites

- Install `kuadrantctl`. You can find a compatible binary and download it from the [kuadrantctl releases page](https://github.com/Kuadrant/kuadrantctl/releases/tag/v0.2.2).
- Ability to distribute resources generated by `kuadrantctl` to multiple clusters, as though you are a platform engineer.

### Set up HTTPRoute and backend

Copy at least one of the following example OAS to a local location:

- [Sample OAS for rate-limiting and API Key](../../examples/oas-apikey.yaml)

- [Sample OAS for rate-limiting and OIDC](../../examples/oas-oidc.yaml)

Set up some new environment variables:

```bash
export oasPath=examples/oas-apikey.yaml
# Ensure you still have these environment variables setup from the start of this guide:
export rootDomain=example.com
export gatewayNS=api-gateway
```

### Use OAS to define our HTTPRoute rules

You can generate Kuadrant and Gateway API resources directly from OAS documents by using an `x-kuadrant` extension.

NOTE: For a more in-depth look at the OAS extension, see the [kuadrantctl documentation](https://docs.kuadrant.io/kuadrantctl/).

Use `kuadrantctl` to generate your `HTTPRoute`.

NOTE: The sample OAS has some placeholders for namespaces and domains. You will inject valid values into these placeholders based on your previous environment variables.

Generate the resource from your OAS, (`envsubst` will replace the placeholders):

```bash
cat $oasPath | envsubst | kuadrantctl generate gatewayapi httproute --oas -

```bash
kubectl get httproute toystore -n ${devNS} -o=yaml
```

We should see that this route is affected by the `AuthPolicy` and `RateLimitPolicy` defined as defaults on the gateway in the gateway namespace.

```yaml
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

### Test connectivity and deny-all auth 

We'll use `curl` to hit an endpoint in the toystore app. As we are using LetsEncrypt staging in this example, we pass the `-k` flag:

```bash
curl -s -k -o /dev/null -w "%{http_code}" "https://$(kubectl get httproute toystore -n ${devNS} -o=jsonpath='{.spec.hostnames[0]}')/v1/toys"
```

We are getting a `403` because of the existing default, deny-all `AuthPolicy` applied at the Gateway. Let's override this for our `HTTPRoute`.

Choose one of the following options:

### API key auth flow

Set up an example API key in our cluster(s):

```bash
kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: toystore-api-key
  namespace: ${devNS}
  labels:
    authorino.kuadrant.io/managed-by: authorino
    kuadrant.io/apikeys-by: api_key
stringData:
  api_key: secret
type: Opaque
EOF
```

Next, generate an `AuthPolicy` that uses secrets in our cluster as APIKeys:

```bash
cat $oasPath | envsubst | kuadrantctl generate kuadrant authpolicy --oas -
```

From this, we can see an `AuthPolicy` generated based on our OAS that will look for API keys in secrets labeled `api_key` and look for that key in the header `api_key`. Let's now apply this to the gateway:

```bash
cat $oasPath | envsubst | kuadrantctl generate kuadrant authpolicy --oas -  | kubectl apply -f -
```

We should get a `200` from the `GET`, as it has no auth requirement:

```bash
curl -s -k -o /dev/null -w "%{http_code}" "https://$(kubectl get httproute toystore -n ${devNS} -o=jsonpath='{.spec.hostnames[0]}')/v1/toys"
```

We should get a `401` for a `POST` request, as it does not have any auth requirements:

```bash
curl -XPOST -s -k -o /dev/null -w "%{http_code}" "https://$(kubectl get httproute toystore -n ${devNS} -o=jsonpath='{.spec.hostnames[0]}')/v1/toys"
```

Finally, if we add our API key header, with a valid key, we should get a `200` response:

```bash
curl -XPOST -H 'api_key: secret' -s -k -o /dev/null -w "%{http_code}" "https://$(kubectl get httproute toystore -n ${devNS} -o=jsonpath='{.spec.hostnames[0]}')/v1/toys"
```

### OpenID Connect auth flow (skip if interested in API key only)

This section of the walkthrough uses the `kuadrantctl` tool to create an `AuthPolicy` that integrates with an OpenID provider and a `RateLimitPolicy` that leverages JWT values for per-user rate limiting. It is important to note that OpenID requires an external provider. Therefore, you should adapt the following example to suit your specific needs and provider.

The platform engineer workflow established some default policies for authentication and rate limiting at your Gateway. The new developer-defined policies, which you will create, are intended to target your HTTPRoute and will supersede the existing policies for requests to your API endpoints, similar to your previous API key example.

The example OAS uses Kuadrant-based extensions. These extensions enable you to define routing and service protection requirements. For more details, see [OpenAPI Kuadrant extensions](https://docs.kuadrant.io/kuadrantctl/doc/openapi-kuadrant-extensions/).


### Pre Requisites

- Setup / have an available OpenID Connect provider, such as https://www.keycloak.org/ 
- Have a realm, client and users set up. For this example, we assume a realm in a Keycloak instance called `toystore`
- Copy the OAS from [sample OAS rate-limiting and OIDC spec](../../examples/oas-oidc.yaml) to a local location

### Set up an OpenID AuthPolicy

```bash
export openIDHost=some.keycloak.com
export oasPath=examples/oas-oidc.yaml
```

> **Note:** the sample OAS has some placeholders for namespaces and domains - we will inject valid values into these placeholders based on our previous env vars

Let's use our OAS and `kuadrantctl` to generate an `AuthPolicy` to replace the default on the Gateway.

```bash
cat $oasPath | envsubst | kuadrantctl generate kuadrant authpolicy --oas -
```

If we're happy with the generated resource, let's apply it to the cluster:

```bash
cat $oasPath | envsubst | kuadrantctl generate kuadrant authpolicy --oas - | kubectl apply -f -
```

We should see in the status of the `AuthPolicy` that it has been accepted and enforced:

```bash
kubectl get authpolicy -n ${devNS} toystore -o=jsonpath='{.status.conditions}'
```

On our `HTTPRoute`, we should also see it now affected by this `AuthPolicy` in the toystore namespace:

```bash
kubectl get httproute toystore -n ${devNS} -o=jsonpath='{.status.parents[0].conditions[?(@.type=="kuadrant.io/AuthPolicyAffected")].message}'
```

Let's now test our `AuthPolicy`:

```bash
export ACCESS_TOKEN=$(curl -k -H "Content-Type: application/x-www-form-urlencoded" \
        -d 'grant_type=password' \
        -d 'client_id=toystore' \
        -d 'scope=openid' \
        -d 'username=bob' \
        -d 'password=p' "https://${openIDHost}/auth/realms/toystore/protocol/openid-connect/token" | jq -r '.access_token')
```        

```bash
curl -k -XPOST --write-out '%{http_code}\n' --silent --output /dev/null "https://$(kubectl get httproute toystore -n ${devNS} -o=jsonpath='{.spec.hostnames[0]}')/v1/toys"
```

You should see a `401` response code. Make a request with a valid bearer token:

```bash
curl -k -XPOST --write-out '%{http_code}\n' --silent --output /dev/null -H "Authorization: Bearer $ACCESS_TOKEN" "https://$(kubectl get httproute toystore -n ${devNS} -o=jsonpath='{.spec.hostnames[0]}')/v1/toys"
```

You should see a `200` response code.

### Set up rate limiting

Lastly, you can generate your `RateLimitPolicy` to add your rate limits, based on your OAS file. Rate limiting is simplified for this walkthrough and is based on either the bearer token or the API key value. There are more advanced examples in the How-to guides on the Kuadrant documentation site, for example:
[Authenticated rate limiting with JWTs and Kubernetes RBAC](https://docs.kuadrant.io/kuadrant-operator/doc/user-guides/authenticated-rl-with-jwt-and-k8s-authnz/)

 You can continue to use this sample OAS document, which includes both authentication and a rate limit:

```bash
export oasPath=examples/oas-oidc.yaml
Again, you should see the rate limit policy accepted and enforced:

```bash
kubectl get ratelimitpolicy -n ${devNS} toystore -o=jsonpath='{.status.conditions}'
```
On your `HTTPoute` we should now see it is affected by the RateLimitPolicy in the same namespace:

```bash
kubectl get httproute toystore -n ${devNS} -o=jsonpath='{.status.parents[0].conditions[?(@.type=="kuadrant.io/RateLimitPolicyAffected")].message}'
```

Let's now test your rate-limiting.

Note you may need to wait a minute for the new rate limits to be applied. With the below requests you should see some number of 429 responses.


API Key Auth:

```bash
for i in {1..3}
do
printf "request $i "
curl -XPOST -H 'api_key:secret' -s -k -o /dev/null -w "%{http_code}"  "https://$(kubectl get httproute toystore -n ${devNS} -o=jsonpath='{.spec.hostnames[0]}')/v1/toys"
printf "\n -- \n"
done 
```

And with OpenID Connect Auth:

```bash
export ACCESS_TOKEN=$(curl -k -H "Content-Type: application/x-www-form-urlencoded" \
        -d 'grant_type=password' \
        -d 'client_id=toystore' \
        -d 'scope=openid' \
        -d 'username=bob' \
        -d 'password=p' "https://${openIDHost}/auth/realms/toystore/protocol/openid-connect/token" | jq -r '.access_token')
```      

```bash
for i in {1..3}
do
curl -k -XPOST --write-out '%{http_code}\n' --silent --output /dev/null -H "Authorization: Bearer $ACCESS_TOKEN" https://$(kubectl get httproute toystore -n ${devNS}-o=jsonpath='{.spec.hostnames[0]}')/v1/toys
done
```

## Conclusion

You've completed the secure, protect, and connect walkthrough. To learn more about Kuadrant, visit https://docs.kuadrant.io
