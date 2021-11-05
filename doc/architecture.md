# Architecture

## Table of contents

* [High level](#high-level)
* [CustomResourceDefinitions](#customresourcedefinitions)
   * [API Product CRD](#api-product-crd)
      * [Setting prefix path for a kuadrant API](#setting-prefix-path-for-a-kuadrant-api)
   * [API CRD](#api-crd)
* [<a href="service-discovery.md">Service discovery</a>](#service-discovery)
* [Authentication](#authentication)
   * [API key](#api-key)
   * [OpenID Connect](#openid-connect)
* [Rate Limiting](#rate-limiting)
   * [Global Rate limiting](#global-rate-limiting)
   * [Rate Limiting Per Remote IP](#rate-limiting-per-remote-ip)
   * [Authenticated Rate Limiting](#authenticated-rate-limiting)

## High level

Kuadrant relies on [Istio](https://istio.io/) to operate the
[istio ingress gateway](https://istio.io/latest/docs/reference/config/networking/gateway/)
to provide API management with authentication and rate limit capabilities. Kuadrant configures, optionally,
the integration of the [istio ingress gateway](https://istio.io/latest/docs/reference/config/networking/gateway/)
with few kuadrant components to leverage the portfolio of features.

* The AuthN/AuthZ enforcer [Authorino](https://github.com/Kuadrant/authorino), as the [external istio authorizer](https://istio.io/latest/docs/tasks/security/authorization/authz-custom/) ([envoy external authorization](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/ext_authz_filter) serving [gRPC](https://grpc.io/) service).
* The rate limit service [Limitador](https://github.com/Kuadrant/limitador) which exposes a [gRPC](https://grpc.io/) service implementing the [Envoy Rate Limit protocol (v3)](https://www.envoyproxy.io/docs/envoy/latest/api-v3/service/ratelimit/v3/rls.proto).

![Kuadrant overview](kuadrant-overview.svg)

The kuadrant controller is the component reading the customer desired configuration
(declaratively as kubernetes custom resources) and ensures all components
are configured to obey customer's desired behavior.

## CustomResourceDefinitions

| CRD | Description |
| --- | --- |
| [APIProduct](/apis/networking/v1beta1/apiproduct_types.go) | Customer-facing APIs. APIProduct facilitates the creation of strong and simplified offerings for API consumers |
| [API](/apis/networking/v1beta1/api_types.go) | Internal APIs bundled in a product. Kuadrant API objects grant API providers the freedom to map their internal API organization structure to kuadrant |

An API Product can contain multiple APIs, and an API can be used in multiple API Products. In other words, to integrate and manage your API in kuadrant you need to create both:

* A kuadrant API CR containing at least the reference to the kuberntes service of your API.
* A kuadrant API Product CR for which you define the used APIs in addition to protection features like authN and rate limiting.

The following diagram illustrates the relationship between the CRDs with a simple example involving two API Products and two APIs.

![Kuadrant CRD](kuadrant-crd.svg)

### API Product CRD

Customer-facing APIs. APIProduct facilitates the creation of strong and simplified offerings for API consumers.

The APIProduct CRD contains basically:
* `hosts`: Domains names to apply the configuration
* `APIs`: List of kuadrant API bundled in the product
* `securityScheme`: Authentication method to apply
* `rateLimit`: Rate limiting configuration to apply

An API Product custom resource looks like this:

```yaml
---
apiVersion: networking.kuadrant.io/v1beta1
kind: APIProduct
metadata:
  name: toystore
spec:
  hosts:
    - api.toystore.io
  APIs:
    - name: dogs
      namespace: default
    - name: cats
      namespace: default
  securityScheme:
    - name: MyAPIKey
      apiKeyAuth:
        location: authorization_header
        name: APIKEY
        credential_source:
          labelSelectors:
            secret.kuadrant.io/managed-by: authorino
            api: toystore
  rateLimit:
    global:
      maxValue: 100
      period: 30
    perRemoteIP:
      maxValue: 10
      period: 30
    authenticated:
      maxValue: 5
      period: 30
```

#### Setting prefix path for a kuadrant API

To avoid conflicts on endpoints exposed by the APIs,
a **prefix** path can be added to a given API reference. For example:

```yaml
---
apiVersion: networking.kuadrant.io/v1beta1
kind: APIProduct
metadata:
  name: toystore
spec:
  hosts:
    - api.toystore.io
  APIs:
    - name: dogs
      namespace: default
      prefix: /dogs
```

Kuadrant will expose the API referenced by `dogs` with a prefix of `/dogs`.
The upstream request will not have the added prefix to match upstream API.

### API CRD

Internal APIs bundled in a product. Kuadrant API objects grant API providers the freedom to map
their internal API organization structure to kuadrant.

An API custom resource looks like this:

```yaml
---
apiVersion: networking.kuadrant.io/v1beta1
kind: API
metadata:
  name: toystore
  namespace: default
spec:
  destination:
    schema: http
    serviceReference:
      name: toystore
      namespace: default
      port: 80
  mappings:
    HTTPPathMatch:
      type: Prefix
      value: /
```

## [Service discovery](service-discovery.md)

## Authentication

### API key

Kuadrant relies on Kubernetes `Secret` resources to represent API keys.
To define an API key, create a `Secret` in the cluster containing an `api_key` entry
that holds the value of the API key.
The resource must also include the same labels listed in the `APIProduct` custom resource
for the protected API that accepts the API key as a valid credential. For example:

For the following security scheme:

```yaml
---
apiVersion: networking.kuadrant.io/v1beta1
kind: APIProduct
metadata:
  name: toystore
  namespace: default
spec:
  securityScheme:
    - name: MyAPIKey
      apiKeyAuth:
        location: authorization_header
        name: APIKEY
        credential_source:
          labelSelectors:
            secret.kuadrant.io/managed-by: authorino
            api: toystore
```

The following secret would represent a valid API key:

```yaml
---
apiVersion: v1
kind: Secret
metadata:
  name: user-1-api-key-1
  labels:
    secret.kuadrant.io/managed-by: authorino
    api: toystore
stringData:
  api_key: <some-randomly-generated-api-key-value>
type: Opaque
```

Follow the [AuthN based on API key](authn-api-key.md) user guide to see that working.

**User Identification**

Optionally, the API key can be associated to a named user id or user name.
It is used for security based on authenticated requests,
like [authenticated rate limit](#authenticated-rate-limiting).

The association is done adding a custom kuadrant annotation

```
secret.kuadrant.io/user-id: <USERNAME>
```

To follow up with the previous example:

```yaml
---
apiVersion: v1
kind: Secret
metadata:
  name: user-1-api-key-1
  annotations:
    secret.kuadrant.io/user-id: user-1
  labels:
    secret.kuadrant.io/managed-by: authorino
    api: toystore
stringData:
  api_key: <some-randomly-generated-api-key-value>
type: Opaque
```

### OpenID Connect

Kuadrant automatically discovers OpenID Connect configurations for the configured issuers
and verifies JSON Web Tokens (JWTs) supplied on each request.

For the following security scheme:

```yaml
---
apiVersion: networking.kuadrant.io/v1beta1
kind: APIProduct
metadata:
  name: toystore
  namespace: default
spec:
  securityScheme:
    - name: MyOIDCAuth
      openIDConnectAuth:
        url: https://myoidcprovider.example.com/auth/realms/basic
```

Kuadrant would accept any valid ID token (JWT) by verifying the signature and timing validations (exp, nbf).

Follow the [AuthN based on OpenID Connect](authn-oidc.md) user guide to see that working.

**User Identification**

The user identification from the received token is done reading the well known field `sub`
(subject) of the ID token in JWT format.
It is used for security based on authenticated requests,
like [authenticated rate limit](#authenticated-rate-limiting).

## Rate Limiting

Kuadrant offers some basic rate limiting modes:
* Global Rate Limiting
* Rate Limiting Per Remote IP
* Authenticated Rate Limiting

The controller supports activation of any type of rate limit individually or any combination of them as well.
Even all of them at the same time.

### Global Rate limiting

Single global rate limit for all requests.
Global rate limit sets an upper limit that cannot be exceeded under any circumstances.
Main use case for protecting infrastructure resources.

The following example will set global rate limit for 100 request for a period of time of 30 seconds.

```
---
apiVersion: networking.kuadrant.io/v1beta1
kind: APIProduct
metadata:
  name: toystore
spec:
  rateLimit:
    global:
      maxValue: 100
      period: 30
```

### Rate Limiting Per Remote IP

Rate limit configuration per each remote IP address.
Main use case for protecting infrastructure resources.

The following example will set rate limit for 10 request for a period of time of 30 seconds.

```
---
apiVersion: networking.kuadrant.io/v1beta1
kind: APIProduct
metadata:
  name: toystore
spec:
  rateLimit:
    perRemoteIP:
      maxValue: 10
      period: 30
```

### Authenticated Rate Limiting

Rate limit configuration per each authenticated client.
This type of rate limit cannot be applied to specific clients.
All authenticated clients get the same rate limit configuration.

The following example will set rate limit for 5 request for a period of time of 30 seconds
for each authenticated client.

```
---
apiVersion: networking.kuadrant.io/v1beta1
kind: APIProduct
metadata:
  name: toystore
spec:
  rateLimit:
    authenticated:
      maxValue: 5
      period: 30
```

