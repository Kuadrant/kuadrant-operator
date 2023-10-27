# Kuadrant Rate Limiting

A Kuadrant RateLimitPolicy custom resource, often abbreviated "RLP":

1. Targets Gateway API networking resources such as [HTTPRoutes](https://gateway-api.sigs.k8s.io/reference/spec/#gateway.networking.k8s.io/v1.HTTPRoute) and [Gateways](https://gateway-api.sigs.k8s.io/reference/spec/#gateway.networking.k8s.io/v1.Gateway), using these resources to obtain additional context, i.e., which traffic workload (HTTP attributes, hostnames, user attributes, etc) to rate limit.
2. Supports targeting subsets (sections) of a network resource to apply the limits to.
3. Abstracts the details of the underlying Rate Limit protocol and configuration resources, that have a much broader remit and surface area.
4. Enables cluster operators to set defaults that govern behavior at the lower levels of the network, until a more specific policy is applied.

## How it works

### Envoy's Rate Limit Service Protocol

Kuadrant's Rate Limit implementation relies on the Envoy's [Rate Limit Service (RLS)](https://www.envoyproxy.io/docs/envoy/latest/api-v3/service/ratelimit/v3/rls.proto) protocol. The workflow per request goes:
1. On incoming request, the gateway checks the matching rules for enforcing rate limits, as stated in the RateLimitPolicy custom resources and targeted Gateway API networking objects
2. If the request matches, the gateway sends one [RateLimitRequest](https://www.envoyproxy.io/docs/envoy/latest/api-v3/service/ratelimit/v3/rls.proto#service-ratelimit-v3-ratelimitrequest) to the external rate limiting service ("Limitador").
1. The external rate limiting service responds with a [RateLimitResponse](https://www.envoyproxy.io/docs/envoy/latest/api-v3/service/ratelimit/v3/rls.proto#service-ratelimit-v3-ratelimitresponse) back to the gateway with either an `OK` or `OVER_LIMIT` response code.

A RateLimitPolicy and its targeted Gateway API networking resource contain all the statements to configure both the ingress gateway and the external rate limiting service.

### The RateLimitPolicy custom resource

#### Overview

The `RateLimitPolicy` spec includes, basically, two parts:

* A reference to an existing Gateway API resource (`spec.targetRef`)
* Limit definitions (`spec.limits`)

Each limit definition includes:
* A set of rate limits (`spec.limits.<limit-name>.rates[]`)
* (Optional) A set of dynamic counter qualifiers (`spec.limits.<limit-name>.counters[]`)
* (Optional) A set of route selectors, to further qualify the specific routing rules when to activate the limit (`spec.limits.<limit-name>.routeSelectors[]`)
* (Optional) A set of additional dynamic conditions to activate the limit (`spec.limits.<limit-name>.when[]`)

<table>
  <tbody>
    <tr>
      <td>Check out Kuadrant <a href="https://github.com/Kuadrant/architecture/blob/main/rfcs/0002-well-known-attributes.md">RFC 0002</a> to learn more about the Well-known Attributes that can be used to define counter qualifiers (<code>counters</code>) and conditions (<code>when</code>).</td>
    </tr>
  </tbody>
</table>

#### High-level example and field definition

```yaml
apiVersion: kuadrant.io/v1beta2
kind: RateLimitPolicy
metadata:
  name: my-rate-limit-policy
spec:
  # reference to an existing networking resource to attach the policy to
  # it can be a Gateway API HTTPRoute or Gateway resource
  # it can only refer to objects in the same namespace as the RateLimitPolicy
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute / Gateway
    name: myroute / mygateway

  # the limits definitions to apply to the network traffic routed through the targeted resource
  limits:
    "my_limit":
      # the rate limits associated with this limit definition
      # e.g., to specify a 50rps rate limit, add `{ limit: 50, duration: 1, unit: secod }`
      rates: […]

      # (optional) counter qualifiers
      # each dynamic value in the data plane starts a separate counter, combined with each rate limit
      # e.g., to define a separate rate limit for each user name detected by the auth layer, add `metadata.filter_metadata.envoy\.filters\.http\.ext_authz.username`
      # check out Kuadrant RFC 0002 (https://github.com/Kuadrant/architecture/blob/main/rfcs/0002-well-known-attributes.md) to learn more about the Well-known Attributes that can be used in this field
      counters: […]

      # (optional) further qualification of the scpecific HTTPRouteRules within the targeted HTTPRoute that should trigger the limit
      # each element contains a HTTPRouteMatch object that will be used to select HTTPRouteRules that include at least one identical HTTPRouteMatch
      # the HTTPRouteMatch part does not have to be fully identical, but the what's stated in the selector must be identically stated in the HTTPRouteRule
      # do not use it on RateLimitPolicies that target a Gateway
      routeSelectors: […]

      # (optional) additional dynamic conditions to trigger the limit.
      # use it for filtering attributes not supported by HTTPRouteRule or with RateLimitPolicies that target a Gateway
      # check out Kuadrant RFC 0002 (https://github.com/Kuadrant/architecture/blob/main/rfcs/0002-well-known-attributes.md) to learn more about the Well-known Attributes that can be used in this field
      when: […]
```

## Using the RateLimitPolicy

### Targeting a HTTPRoute networking resource

When a RLP targets a HTTPRoute, the policy is enforced to all traffic routed according to the rules and hostnames specified in the HTTPRoute, across all Gateways referenced in the `spec.parentRefs` field of the HTTPRoute.

The targeted HTTPRoute's rules and/or hostnames to which the policy must be enforced can be filtered to specific subsets, by specifying the [`routeSelectors`](reference/route-selectors.md#the-routeselectors-field) field of the limit definition.

Target a HTTPRoute by setting the `spec.targetRef` field of the RLP as follows:

```yaml
apiVersion: kuadrant.io/v1beta2
kind: RateLimitPolicy
metadata:
  name: <RLP name>
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: <HTTPRoute Name>
  limits: {…}
```

![Rate limit policy targeting a HTTPRoute resource](https://i.imgur.com/ObfOp9u.png)

#### Multiple HTTPRoutes with the same hostname

When multiple HTTPRoutes state the same hostname, these HTTPRoutes are usually all admitted and merged together by the gateway implemetation in the same virtual host configuration of the gateway. Similarly, the Kuadrant control plane will also register all rate limit policies referencing the HTTPRoutes, activating the correct limits across policies according to the routing matching rules of the targeted HTTPRoutes.

#### Hostnames and wildcards

If a RLP targets a route defined for `*.com` and another RLP targets another route for `api.com`, the Kuadrant control plane will not merge these two RLPs. Rather, it will mimic the behavior of gateway implementation by which the "most specific hostname wins", thus enforcing only the corresponding applicable policies and limit definitions.

E.g., a request coming for `api.com` will be rate limited according to the rules from the RLP that targets the route for `api.com`; while a request for `other.com` will be rate limited with the rules from the RLP targeting the route for `*.com`.

Example with 3 RLPs and 3 HTTPRoutes:
- RLP A → HTTPRoute A (`a.toystore.com`)
- RLP B → HTTPRoute B (`b.toystore.com`)
- RLP W → HTTPRoute W (`*.toystore.com`)

Expected behavior:
- Request to `a.toystore.com` → RLP A will be enforced
- Request to `b.toystore.com` → RLP B will be enforced
- Request to `other.toystore.com` → RLP W will be enforced

### Targeting a Gateway networking resource

When a RLP targets a Gateway, the policy will be enforced to all HTTP traffic hitting the gateway, unless a more specific RLP targeting a matching HTTPRoute exists.

Any new HTTPRoute referrencing the gateway as parent will be automatically covered by the RLP that targets the Gateway, as well as changes in the existing HTTPRoutes.

This effectively provides cluster operators with the ability to set _defaults_ to protect the infrastructure against unplanned and malicious network traffic attempt, such as by setting preemptive limits for hostnames and hostname wildcards.

Target a Gateway HTTPRoute by setting the `spec.targetRef` field of the RLP as follows:

```yaml
apiVersion: kuadrant.io/v1beta2
kind: RateLimitPolicy
metadata:
  name: <RLP name>
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: <Gateway Name>
  limits: {…}
```

![rate limit policy targeting a Gateway resource](https://i.imgur.com/UkivAqA.png)

#### Overlapping Gateway and HTTPRoute RLPs

Gateway-targeted RLPs will serve as a default to protect all traffic routed through the gateway until a more specific HTTPRoute-targeted RLP exists, in which case the HTTPRoute RLP prevails.

Example with 4 RLPs, 3 HTTPRoutes and 1 Gateway (plus 2 HTTPRoute and 2 Gateways without RLPs attached):
- RLP A → HTTPRoute A (`a.toystore.com`) → Gateway G (`*.com`)
- RLP B → HTTPRoute B (`b.toystore.com`) → Gateway G (`*.com`)
- RLP W → HTTPRoute W (`*.toystore.com`) → Gateway G (`*.com`)
- RLP G → Gateway G (`*.com`)

Expected behavior:
- Request to `a.toystore.com` → RLP A will be enforced
- Request to `b.toystore.com` → RLP B will be enforced
- Request to `other.toystore.com` → RLP W will be enforced
- Request to `other.com` (suppose a route exists) → RLP G will be enforced
- Request to `yet-another.net` (suppose a route and gateway exist) → No RLP will be enforced

### Limit definition

A limit will be activated whenever a request comes in and the request matches:
- any of the route rules selected by the limit (via [`routeSelectors`](reference/route-selectors.md#the-routeselectors-field) or implicit "catch-all" selector), and
- all of the `when` conditions specified in the limit.

A limit can define:
- counters that are qualified based on dynamic values fetched from the request, or
- global counters (implicitly, when no qualified counter is specified)

A limit is composed of one or more rate limits.

E.g.

```yaml
spec:
  limits:
    "toystore-all":
      rates:
      - limit: 5000
        duration: 1
        unit: second

    "toystore-api-per-username":
      rates:
      - limit: 100
        duration: 1
        unit: second
      - limit: 1000
        duration: 1
        unit: minute
      counters:
      - auth.identity.username
      routeSelectors:
        hostnames:
        - api.toystore.com

    "toystore-admin-unverified-users":
      rates:
      - limit: 250
        duration: 1
        unit: second
      routeSelectors:
        hostnames:
        - admin.toystore.com
      when:
      - selector: auth.identity.email_verified
        operator: eq
        value: "false"
```

| Request to           | Rate limits enforced                                         |
|----------------------|--------------------------------------------------------------|
| `api.toystore.com`   | 100rps/username or 1000rpm/username (whatever happens first) |
| `admin.toystore.com` | 250rps                                                       |
| `other.toystore.com` | 5000rps                                                      |

### Route selectors

Route selectors allow targeting sections of a HTTPRoute, by specifying sets of HTTPRouteMatches and/or hostnames that make the policy controller look up within the HTTPRoute spec for compatible declarations, and select the corresponding HTTPRouteRules and hostnames, to then build conditions that activate the policy or policy rule.

Check out [Route selectors](reference/route-selectors.md) for a full description, semantics and API reference.

#### `when` conditions

`when` conditions can be used to scope a limit (i.e. to filter the traffic to which a limit definition applies) without any coupling to the underlying network topology, i.e. without making direct references to HTTPRouteRules via [`routeSelectors`](reference/route-selectors.md#the-routeselectors-field).

Use `when` conditions to conditionally activate limits based on attributes that cannot be expressed in the HTTPRoutes' `spec.hostnames` and `spec.rules.matches` fields, or in general in RLPs that target a Gateway.

The selectors within the `when` conditions of a RateLimitPolicy are a subset of Kuadrant's Well-known Attributes ([RFC 0002](https://github.com/Kuadrant/architecture/blob/main/rfcs/0002-well-known-attributes.md)). Check out the reference for the full list of supported selectors.

### Examples

Check out the following user guides for examples of rate limiting services with Kuadrant:
* [Simple Rate Limiting for Application Developers](user-guides/simple-rl-for-app-developers.md)
* [Authenticated Rate Limiting for Application Developers](user-guides/authenticated-rl-for-app-developers.md)
* [Gateway Rate Limiting for Cluster Operators](user-guides/gateway-rl-for-cluster-operators.md)
* [Authenticated Rate Limiting with JWTs and Kubernetes RBAC](user-guides/authenticated-rl-with-jwt-and-k8s-authnz.md)

### Known limitations

* One HTTPRoute can only be targeted by one RLP.
* One Gateway can only be targeted by one RLP.
* RLPs can only target HTTPRoutes/Gateways defined within the same namespace of the RLP.

## Implementation details

Driven by limitations related to how Istio injects configuration in the filter chains of the ingress gateways, Kuadrant relies on Envoy's [Wasm Network](https://www.envoyproxy.io/docs/envoy/latest/configuration/listeners/network_filters/wasm_filter) filter in the data plane, to manage the integration with rate limiting service ("Limitador"), instead of the [Rate Limit](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/rate_limit_filter) filter.

**Motivation:** _Multiple rate limit domains_<br/>
The first limitation comes from having only one filter chain per listener. This often leads to one single global rate limiting filter configuration per gateway, and therefore to a shared rate limit [domain](https://www.envoyproxy.io/docs/envoy/latest/api-v3/extensions/filters/http/ratelimit/v3/rate_limit.proto#envoy-v3-api-msg-extensions-filters-http-ratelimit-v3-ratelimit) across applications and policies. Even though, in a rate limit filter, the triggering of rate limit calls, via [actions to build so-called "descriptors"](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/rate_limit_filter#composing-actions), can be defined at the level of the virtual host and/or specific route rule, the overall rate limit configuration is only one, i.e., always the same rate limit domain for all calls to Limitador.

On the other hand, the possibility to configure and invoke the rate limit service for multiple domains depending on the context allows to isolate groups of policy rules, as well as to optimize performance in the rate limit service, which can rely on the domain for indexation.

**Motivation:** _Fine-grained matching rules_<br/>
A second limitation of configuring the rate limit filter via Istio, particularly from [Gateway API](https://gateway-api.sigs.k8s.io) resources, is that [rate limit descriptors](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/rate_limit_filter#composing-actions) at the level of a specific HTTP route rule require "named routes" – defined only in an Istio [VirtualService](https://istio.io/latest/docs/reference/config/networking/virtual-service/#HTTPRoute) resource and referred in an [EnvoyFilter](https://istio.io/latest/docs/reference/config/networking/envoy-filter/#EnvoyFilter-RouteConfigurationMatch-RouteMatch) one. Because Gateway API HTTPRoute rules lack a "name" property[^1], as well as the Istio VirtualService resources are only ephemeral data structures handled by Istio in-memory in its implementation of gateway configuration for Gateway API, where the names of individual route rules are auto-generated and not referable by users in a policy[^2][^3], rate limiting by attributes of the HTTP request (e.g., path, method, headers, etc) would be very limited while depending only on Envoy's [Rate Limit](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/rate_limit_filter) filter.

[^1]: https://github.com/kubernetes-sigs/gateway-api/pull/996
[^2]: https://github.com/istio/istio/issues/36790
[^3]: https://github.com/istio/istio/issues/37346

Motivated by the desire to support multiple rate limit domains per ingress gateway, as well as fine-grained HTTP route matching rules for rate limiting, Kuadrant implements a [wasm-shim](https://github.com/Kuadrant/wasm-shim) that handles the rules to invoke the rate limiting service, complying with Envoy's [Rate Limit Service (RLS)](https://www.envoyproxy.io/docs/envoy/latest/api-v3/service/ratelimit/v3/rls.proto) protocol.

The wasm module integrates with the gateway in the data plane via [Wasm Network](https://www.envoyproxy.io/docs/envoy/latest/configuration/listeners/network_filters/wasm_filter) filter, and parses a configuration composed out of user-defined RateLimitPolicy resources by the Kuadrant control plane. Whereas the rate limiting service ("Limitador") remains an implementation of Envoy's RLS protocol, capable of being integrated directly via [Rate Limit](https://www.envoyproxy.io/docs/envoy/latest/api-v3/extensions/filters/network/ratelimit/v3/rate_limit.proto#extension-envoy-filters-network-ratelimit) extension or by Kuadrant, via wasm module for the [Istio Gateway API implementation](https://gateway-api.sigs.k8s.io/implementations/#istio).

As a consequence of this design:
- Users can define fine-grained rate limit rules that match their Gateway and HTTPRoute definitions including for subsections of these.
- Rate limit definitions are insulated, not leaking across unrelated policies or applications.
- Conditions to activate limits are evaluated in the context of the gateway process, reducing the gRPC calls to the external rate limiting service only to the cases where rate limit counters are known in advance to have to be checked/incremented.
- The rate limiting service can rely on the indexation to look up for groups of limit definitions and counters.
- Components remain compliant with industry protocols and flexible for different integration options.

A Kuadrant wasm-shim configuration for a composition of RateLimitPolicy custom resources looks like the following and it is generated automatically by the Kuadrant control plane:

```yaml
apiVersion: extensions.istio.io/v1alpha1
kind: WasmPlugin
metadata:
  name: kuadrant-istio-ingressgateway
  namespace: istio-system
  …
spec:
  phase: STATS
  pluginConfig:
    failureMode: deny
    rateLimitPolicies:
    - domain: istio-system/gw-rlp # allows isolating policy rules and improve performance of the rate limit service
      hostnames:
      - '*.website'
      - '*.io'
      name: istio-system/gw-rlp
      rules: # match rules from the gateway and according to conditions specified in the rlp
      - conditions:
        - allOf:
          - operator: startswith
            selector: request.url_path
            value: /
        data:
        - static: # tells which rate limit definitions and counters to activate
            key: limit.internet_traffic_all__593de456
            value: "1"
      - conditions:
        - allOf:
          - operator: startswith
            selector: request.url_path
            value: /
          - operator: endswith
            selector: request.host
            value: .io
        data:
        - static:
            key: limit.internet_traffic_apis_per_host__a2b149d2
            value: "1"
        - selector:
            selector: request.host
      service: kuadrant-rate-limiting-service
    - domain: default/app-rlp
      hostnames:
      - '*.toystore.website'
      - '*.toystore.io'
      name: default/app-rlp
      rules: # matches rules from a httproute and additional specified in the rlp
      - conditions:
        - allOf:
          - operator: startswith
            selector: request.url_path
            value: /assets/
        data:
        - static:
            key: limit.toystore_assets_all_domains__8cfb7371
            value: "1"
      - conditions:
        - allOf:
          - operator: startswith
            selector: request.url_path
            value: /v1/
          - operator: eq
            selector: request.method
            value: GET
          - operator: endswith
            selector: request.host
            value: .toystore.website
          - operator: eq
            selector: auth.identity.username
            value: ""
        - allOf:
          - operator: startswith
            selector: request.url_path
            value: /v1/
          - operator: eq
            selector: request.method
            value: POST
          - operator: endswith
            selector: request.host
            value: .toystore.website
          - operator: eq
            selector: auth.identity.username
            value: ""
        data:
        - static:
            key: limit.toystore_v1_website_unauthenticated__3f9c40c6
            value: "1"
      service: kuadrant-rate-limiting-service
  selector:
    matchLabels:
      istio.io/gateway-name: istio-ingressgateway
  url: oci://quay.io/kuadrant/wasm-shim:v0.3.0
```
