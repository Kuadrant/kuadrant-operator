# Secure, Protect, and Connect APIs with Kuadrant

## Overview

In this guide, we will walk you through using Kuadrant to secure, protect and connect an API exposed by a Gateway API Gateway. You can use this walkthrough for a Gateway on a single or a Gateway distributed across multiple clusters that have a shared listener hostname. We will take the approach of assuming certain personas and how they can each utilise Kuadrant to achieve their goals.

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
export AWS_ACCESS_KEY_ID=xxxx
export AWS_SECRET_ACCESS_KEY=xxxx
export AWS_REGION=us-east-1
export clusterIssuerName=lets-encrypt
export EMAIL=foo@example.com
```

### Tooling

While this document uses `kubectl`, working with multiple clusters is complex, so we would recommend looking into something like ArgoCD to manage the deployment of resource to multiple clusters.

### Setup a managed DNS zone

The managed DNS zone declares a zone and credentials to access that zone that can be used by Kuadrant to set up DNS configuration.

**Create the ManagedZone resource**

Apply the `ManagedZone` resource below to each cluster or, if you are adding an additional cluster, add it to the new cluster:

```bash
kubectl create ns ${gatewayNS}
```

Setup AWS credential for Route 53 access:

```bash
kubectl -n ${gatewayNS} create secret generic aws-credentials \
  --type=kuadrant.io/aws \
  --from-literal=AWS_ACCESS_KEY_ID=$AWS_ACCESS_KEY_ID \
  --from-literal=AWS_SECRET_ACCESS_KEY=$AWS_SECRET_ACCESS_KEY
```  

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
    kuadrant.io/gateway: true
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

Check the status of our gateway:

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

### Secure and Protect the Gateway with TLS Rate Limiting and Auth policies.

While our Gateway is now deployed, it has no exposed endpoints and our listener is not programmed. Let's set up a `TLSPolicy` that leverages our CertificateIssuer to set up our listener certificates. We will also define an `AuthPolicy` that will setup a default `403` response for any unprotected endpoints, as well as a `RateLimitPolicy` that will setup a default (artificially) low global limit to further protect any endpoints exposed by this Gateway.


Setup a default, deny-all `AuthPolicy` for our Gateway:

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

Let's check our policy was accepted by the controller:

```bash
kubectl get tlspolicy ${gatewayName}-tls -n ${gatewayNS} -o=jsonpath='{.status.conditions[?(@.type=="Accepted")].message}'
```

Lastly, we'll setup a default `RateLimitPolicy` for our Gateway, with arbitrarily low limits (2 requests in a 10 second window):

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


To check our rate limits have been accepted, run:

```bash
kubectl get ratelimitpolicy ${gatewayName}-rlp -n ${gatewayNS} -o=jsonpath='{.status.conditions[?(@.type=="Accepted")].message}'
```

Let's check the programmed state of our gateway listener once more:

```bash
kubectl get gateway ${gatewayName} -n ${gatewayNS} -o=jsonpath='{.status.listeners[0].conditions[?(@.type=="Programmed")].message}'
```

We should have no errors. **Note:** it can take a minute or two for the LetsEncypt ACME certificate to be issued

### Setup our DNS

Having secured and deployed our gateway, the next step involves applying a `DNSPolicy` to direct traffic toward our gateway via the assigned listener hosts. This policy orchestrates traffic flow to the gateways within our clusters. Specifically, it establishes a load-balanced strategy, using a round-robin method for responding to DNS clients. Additionally, we establish a default geo setting. This setting acts as a universal fallback, categorising records under a broad default, ready for future configurations. This setup ensures that should geo-routing be activated on our gateways (to be discussed later), a default will already be in place for any users outside the specified gateway geos, allowing access from any location. We also set a default weight for all records, ensuring uniform application to maintain round-robin distribution.

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

**Note:** the `DNSPolicy` will leverage the `ManagedZone` we defined earlier based on the listener hosts defined in the gateway.

Let's check our `DNSPolicy` has been accepted:

```bash
kubectl get dnspolicy ${gatewayName}-dnspolicy -n ${gatewayNS} -o=jsonpath='{.status.conditions[?(@.type=="Accepted")].message}'
```

If you have set up the observability components (see the Installation guide), and remote write to a Thanos instance, then you should be able to access the Grafana instance and see your deployed gateway and policies in the `platform` engineer dashboard.

## Platform Engineer review

We have now established an external gateway, secured it with TLS, and protected all endpoints with a default `DENY ALL` AuthPolicy, alongside a restrictive default `RateLimitPolicy`. We have also configured a `ManagedZone` and a `DNSPolicy` to direct traffic to the gateway via the specified listener hosts. Our gateway is now ready to begin receiving traffic.

By creating an `HTTPRoute` for our listeners, we will activate the `DNSPolicy`, `AuthPolicy`, and `RateLimitPolicy`, populating DNS records and configuring auth and rate limiting to safeguard requests to that endpoint.

To verify the setup, we can deploy a simple application and connect it to our gateway:

```bash
kubectl apply -f https://raw.githubusercontent.com/Kuadrant/Kuadrant-operator/main/examples/toystore/toystore.yaml -n ${gatewayNS}
```

Add an `HTTPRoute` to expose this application:

```bash
kubectl apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: toystore
  namespace: ${gatewayNS}
spec:
  parentRefs:
  - name: ${gatewayName}
    namespace: ${gatewayNS}
  hostnames:
  - "toystore.${rootDomain}"
  rules:
  - backendRefs:
    - name: toystore
      port: 80
EOF
```

Check our gateway policies are enforced:

```bash
kubectl get dnspolicy ${gatewayName}-dnspolicy -n ${gatewayNS} -o=jsonpath='{.status.conditions[?(@.type=="Enforced")].message}'
kubectl get authpolicy ${gatewayName}-auth -n ${gatewayNS} -o=jsonpath='{.status.conditions[?(@.type=="Enforced")].message}'
kubectl get ratelimitpolicy ${gatewayName}-rlp -n ${gatewayNS} -o=jsonpath='{.status.conditions[?(@.type=="Enforced")].message}'
```

**Note:** `TLSPolicy` is currently missing an enforced condition. https://github.com/Kuadrant/kuadrant-operator/issues/572. However, looking at the gateway status we can see it is affected:

```bash
kubectl get gateway -n ${gatewayNS} ${gatewayName} -n ${gatewayNS} -o=jsonpath='{.status.conditions[*].message}'
```

### Test connectivity and deny all auth 

We are using `curl` to hit our endpoint. As we are using LetsEncrypt staging in this example, we pass the `-k` flag:

```bash
curl -k -w "%{http_code}" https://$(kubectl get httproute toystore -n ${gatewayNS} -o=jsonpath='{.spec.hostnames[0]}')
```

We should see a `403` response. With our gateway and policies in place, we can now allow other teams to use the gateway:

```bash
kubectl patch gateway ${gatewayName} -n ${gatewayNS} --type='json' -p='[{"op": "replace", "path": "/spec/listeners/0/allowedRoutes/namespaces/from", "value":"All"}]'
```

### Extending this Gateway to multiple clusters and configuring geo-based routing

To distribute this gateway across multiple clusters, repeat the setup process detailed previously for each cluster. By default, this will implement a round-robin DNS strategy to distribute traffic evenly across the different clusters. Setting up our gateways to serve clients based on their geographic location is straightforward with our current configuration.

Assuming you have deployed gateway instances across multiple clusters as per this guide, the next step involves updating the DNS controller with the geographic regions of the visible gateways.

For instance, if you have one cluster in North America and another in the EU, you can direct traffic to these gateways based on their location by applying the appropriate labels:

For our North American cluster:

```bash
kubectl label --overwrite gateway ${gatewayName} kuadrant.io/lb-attribute-geo-code=US -n ${gatewayNS}
```

And our European Cluster:

```bash
kubectl label --overwrite gateway ${gatewayName} kuadrant.io/lb-attribute-geo-code=EU -n ${gatewayNS}
```

After allowing some time for distribution, you can verify the geographic distribution of your traffic using the `HTTPRoute` host with the following command:

```bash
kubectl get httproute toystore -n ${gatewayNS} -o=jsonpath='{.spec.hostnames[0]}'
```

To check this, visit a site such as https://dnsmap.io/.

## Developer

In this section of the walkthrough, we will focus on using an Open API Specification (OAS) to define an API. We will utilise Kuadrant's OAS extensions to specify the routing, authentication, and rate-limiting requirements. Next, we will employ the `kuadrantctl` tool to generate an `AuthPolicy`, an `HTTPRoute`, and a `RateLimitPolicy`, which we will then apply to our cluster to enforce the settings defined in our OAS.

**Note:** While we use the `kuadrantctl` tool here, it is worth noting that it is not essential. `AuthPolicy`, `RateLimitPolicy` and `HTTPRoute`'s can also be created and applied via `oc` or `kubectl`.

### Pre Req

- Install `kuadrantctl`. You can find a compatible binary and download it from the [kuadrantctl releases page](https://github.com/kuadrant/kuadrantctl/releases )
- Ability to distribute resources generated via `kuadrantctl` to multiple clusters, as though you are a platform engineer.

### Setup HTTPRoute and backend

Copy at least one of the following example OAS to a local location:

[sample OAS rate-limiting and API Key spec](../../examples/oas-apikey.yaml)

[sample OAS rate-limiting and OIDC spec](../../examples/oas-oidc.yaml)

Setup some new env vars:

```bash
export oasPath=/path/to/local/oas.yaml
# Ensure you still have these environment variables setup from the start of this guide:
export rootDomain=example.com
export gatewayNS=api-gateway
```

### Use OAS to define our HTTPRoute rules

We can generate Kuadrant and Gateway API resources directly from OAS documents, via an `x-kuadrant` extension.

> **Note:** for a more in-depth look at the OAS extension take a look at our [kuadrantctl documentation](https://docs.kuadrant.io/kuadrantctl/).

Let's use `kuadrantctl` to generate our `HTTPRoute`.

> **Note:** the sample OAS has some placeholders for namespaces and domains - we will inject valid values into these placeholders based on our previous env vars

Generate the resource from our OAS, (`envsubst` will replace the placeholders):

```bash
cat $oasPath | envsubst | kuadrantctl generate gatewayapi httproute --oas -
```
If we're happy with the generated resource, let's apply it to the cluster:

```bash
cat $oasPath | envsubst | kuadrantctl generate gatewayapi httproute --oas - | kubectl apply -f -
```

Check our new route:

```bash
kubectl get httproute -n ${gatewayNS} -o=yaml
```

We should see that this route is affected by the `AuthPolicy` and `RateLimitPolicy` defined as defaults on the gateway.

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
curl -s -k -o /dev/null -w "%{http_code}"  https://$(kubectl get httproute toystore -n toystore -o=jsonpath='{.spec.hostnames[0]}')/v1/toys
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
  namespace: toystore
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
kuadrantctl generate kuadrant authpolicy --oas=$oasPath
```

From this, we can see an `AuthPolicy` generated based on our OAS that will look for API keys in secrets labeled `api_key` and look for that key in the header `api_key`. Let's now apply this to the gateway:

```bash
kuadrantctl generate kuadrant authpolicy --oas=$oasPath  | kubectl apply -f -
```

We should get a `200` from the `GET`, as it has no auth requirement:

```bash
curl -s -k -o /dev/null -w "%{http_code}"  https://$(k get httproute toystore -n toystore -o=jsonpath='{.spec.hostnames[0]}')/v1/toys
200
```

We should get a `403` for a `POST` request, as it does not have any auth requirements:

```bash
curl -XPOST -s -k -o /dev/null -w "%{http_code}"  https://$(kubectl get httproute toystore -n toystore -o=jsonpath='{.spec.hostnames[0]}')/v1/toys
401
```

Finally, if we add our API key header, with a valid key, we should get a `200` response:

```bash
curl -XPOST -H 'api_key:secret' -s -k -o /dev/null -w "%{http_code}"  https://$(kubectl get httproute toystore -n toystore -o=jsonpath='{.spec.hostnames[0]}')/v1/toys
200
```

### OpenID Connect auth flow


In this part of the walkthrough, we will use the `kuadrantctl` tool to create an `AuthPolicy` that integrates with an OpenID provider and a `RateLimitPolicy` that leverages JWT values for per-user rate limiting. It's important to note that OpenID requires an external provider; therefore, the following example should be adapted to suit your specific needs and provider.

During the platform engineer section, we established some default policies for authentication and rate limiting at our gateway. These new developer-defined policies, which we will now create, are intended to target our HTTPRoute and will supersede the existing policies for requests to our API endpoints, similar to our previous API Key example.

Our example Open API Specification (OAS) utilizes Kuadrant-based extensions. These extensions enable you to define routing and service protection requirements. You can learn more about these extensions [here](https://docs.kuadrant.io/kuadrantctl/doc/openapi-kuadrant-extensions/).


### Pre Requisites

- Setup / have an available OpenID Connect provider, such as https://www.keycloak.org/ 
- Have a realm, client and users set up. For this example, we assume a realm in a Keycloak instance called `petstore`
- Copy the OAS from [sample OAS rate-limiting and OIDC spec](../../examples/oas-oidc.yaml) to a local location

### Setup OpenID AuthPolicy

```bash
export openIDHost=some.keycloak.com
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
kubectl get authpolicy -n toystore toystore -o=jsonpath='{.status.conditions}'
```

On our `HTTPRoute`, we should also see it now affected by this `AuthPolicy` in the toystore namespace:

```bash
kubectl get httproute toystore -n toystore -o=jsonpath='{.status.parents[0].conditions[?(@.type=="kuadrant.io/AuthPolicyAffected")].message}'
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
curl -k -XPOST --write-out '%{http_code}\n' --silent --output /dev/null https://$(kubectl get httproute toystore -n toystore -o=jsonpath='{.spec.hostnames[0]}')/v1/toys
```

You should see a `401` response code. Make a request with a valid bearer token:

```bash
curl -k -XPOST --write-out '%{http_code}\n' --silent --output /dev/null -H "Authorization: Bearer $ACCESS_TOKEN" https://$(kubectl get httproute toystore -n toystore -o=jsonpath='{.spec.hostnames[0]}')/v1/toys
```

You should see a `200` response code.

### Setup rate-limiting

Lastly let us generate our `RateLimitPolicy` to add our rate-limits, based on our OAS file. Our rate limiting is simplified for this walkthrough and is based on either the bearer token or the API key value. There are more advanced examples under our how-to guides on the docs site:

https://docs.kuadrant.io/kuadrant-operator/doc/user-guides/authenticated-rl-with-jwt-and-k8s-authnz/

```bash
kuadrantctl generate kuadrant ratelimitpolicy --oas=$oasPath | yq -P
```

You should see we have an artificial limit of 1 request per 5 seconds for the `GET` and 1 request per 10 seconds for the `POST` endpoint.

Apply this to the cluster:

```bash
kuadrantctl generate kuadrant ratelimitpolicy --oas=$oasPath | kubectl apply -f -
```

Again, we should see the rate limit policy accepted and enforced:

```bash
kubectl get ratelimitpolicy -n toystore toystore -o=jsonpath='{.status.conditions}'
```
On our HTTP`R`oute we should now see it is affected by the RateLimitPolicy in the same namespace:

```bash
kubectl get httproute toystore -n toystore -o=jsonpath='{.status.parents[0].conditions[?(@.type=="kuadrant.io/RateLimitPolicyAffected")].message}'
```

Let's now test our rate-limiting.


API Key Auth:

```bash
for i in {1..3}
do
printf "request $i "
curl -XPOST -H 'api_key:secret' -s -k -o /dev/null -w "%{http_code}"  https://$(k get httproute toystore -n toystore -o=jsonpath='{.spec.hostnames[0]}')/v1/toys
printf "\n -- \n"
done 

```

and with OpenID Connect Auth:

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
curl -k -XPOST --write-out '%{http_code}\n' --silent --output /dev/null -H "Authorization: Bearer $ACCESS_TOKEN" https://$(kubectl get httproute toystore -n toystore -o=jsonpath='{.spec.hostnames[0]}')/v1/toys
done
```

## Conclusion

You've now completed our Secure, Protect, and Connect tutorial. To learn more about Kuadrant, visit: https://docs.kuadrant.io
