# Kuadrant Rate Limiting

## Goals

Kuadrant sees the following requirements for an **ingress gateway** based rate limit policy:

* Allow it to target **routing/network** resources such as
[HTTPRoute](https://gateway-api.sigs.k8s.io/v1alpha2/references/spec/#gateway.networking.k8s.io/v1alpha2.HTTPRoute)
and [Gateway](https://gateway-api.sigs.k8s.io/v1alpha2/references/spec/#gateway.networking.k8s.io/v1alpha2.Gateway) and use these resources to provide needed context (which traffic workload (hostname), which gateway).
* Use it to define when to invoke rate limiting (what paths, what methods etc) and the needed
metadata IE actions and descriptors that are needed to enforce the rate limiting requirements.
* Avoid exposing the end user to the complexity of the underlying configuration resources that has
a much broader remit and surface area.
* Allow administrators (cluster operators) to set overrides and defaults that govern what can be
done at the lower levels.

## How it works

### Envoy's Rate Limit Service Potocol

Kuadrant's rate limit implementation relies on the
[Rate Limit Service (RLS)](https://www.envoyproxy.io/docs/envoy/latest/api-v3/service/ratelimit/v3/rls.proto)
protocol. The workflow per request would be:

1. On incoming request, the gateway sends (optionally, depending on the context)
one [RateLimitRequest](https://www.envoyproxy.io/docs/envoy/latest/api-v3/service/ratelimit/v3/rls.proto#service-ratelimit-v3-ratelimitrequest)
to the external rate limiting service.
2. The external rate limiting service answers with a [RateLimitResponse](https://www.envoyproxy.io/docs/envoy/latest/api-v3/service/ratelimit/v3/rls.proto#service-ratelimit-v3-ratelimitresponse)
back to the gateway with either `OK` or `OVER_LIMIT` response code.

The RateLimitPolicy contains the bits to configure both the gateway and the external rate limiting service.

### The RateLimitPolicy object overview

The `RateLimitPolicy` resource includes, basically, three parts:

* A reference to existing routing/networing Gateway API resource.
  * location: `spec.targetRef`
* Gateway configuration to produce rate limit descriptors.
  * location: `spec.rateLimits[].configurations` and `spec.rateLimits[].rules`
* External rate limiting service, [Limitador's](https://github.com/Kuadrant/limitador) configuration.
  * location: `spec.rateLimits[].limits`

```yaml
---
apiVersion: apim.kuadrant.io/v1alpha1
kind: RateLimitPolicy
metadata:
  name: my-rate-limit-policy
spec:
  #  targetRef defines a reference to existing routing/networking resource object to apply policy to.
  targetRef: { ... }
    group: gateway.networking.k8s.io
    kind: HTTPRoute / Gateway
    name: myroute / mygateway
  rateLimits:
      # Rules defines the list of conditions for which rate limit configuration will apply.
      # Used to configure ingress gateway.
    - rules: [ ... ]
      # Each configuration object represents one action configuration.
      # Each configuration produces, at most, one rate limit descriptor.
      # Used to configure ingress gateway.
      configurations: [ ... ]
      # Limits are used to configure rate limiting boundaries on time periods.
      # Used to configure kuadrant's external rate limiting service.
      limits: [ ... ]
```

## Using the RateLimitPolicy

### Targeting a HTTPRoute networking resource

When a rate limit policy targets an HTTPRoute, the policy is scoped by the domains defined
at the referenced HTTPRoute's hostnames.

The rate limit policy targeting an HTTPRoute will be applied to every single ingress gateway
referenced by the HTTPRoute in the `spec.parentRefs` field.

Targeting is defined with the `spec.targetRef` field, as follows:

```yaml
apiVersion: apim.kuadrant.io/v1alpha1
kind: RateLimitPolicy
metadata:
  name: <RLP name>
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: <HTTPRoute Name>
  rateLimits: [ ... ]
```

![](https://i.imgur.com/ObfOp9u.png)

**Multiple HTTPRoutes with the same hostname**

When there are multiple HTTPRoutes with the same hostname,
HTTPRoutes are all admitted and the ingress gateway will merge the routing configurations
in the same virtualhost. In these cases, kuadrant control plane will also merge rate limit
policies referencing HTTPRoutes with the same hostname.

**Overlapping HTTPRoutes**

If one RLP targets a route for `*.com` and other RLP targets another route for `api.com`,
the kuadrant's control plane does not do any *merging*. A request coming for `api.com` will be
rate limited according to the rules from the RLP targeting the route `api.com`.
On the other hand, a request coming for `other.com` will be rate limited with the rules
from the RLP targeting the route `*.com`.

For example, let's say we have three rate limit policies in place:

```
RLP A -> HTTPRoute A (api.toystore.com)

RLP B -> HTTPRoute B (other.toystore.com)

RLP H -> HTTPRoute H (*.toystore.com)
```

Request 1 (api.toystore.com) -> RLP A will be applied

Request 2 (other.toystore.com) -> RLP B will be applied

Request 3 (unknown.toystore.com) -> RLP H will be applied

### Targeting a Gateway networking resource

A key use case is being able to provide governance over what service providers can and cannot do
when exposing a service via a shared ingress gateway. As well as providing certainty that
no service is exposed without my ability as a cluster administrator to protect my infrastructure
from unplanned load from badly behaving clients etc.

When a rate limit policy targets Gateway, the policy will be applied to all HTTP traffic hitting
the gateway.

Targeting is defined with the `spec.targetRef` field, as follows:

```yaml
apiVersion: apim.kuadrant.io/v1alpha1
kind: RateLimitPolicy
metadata:
  name: <RLP name>
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: <Gateway Name>
  rateLimits: [ ... ]
```

![](https://i.imgur.com/UkivAqA.png)

The kuadrant control plane will aggregate all the rate limit policies that apply to a gateway,
including multiple RLP targeting HTTPRoutes and Gateways. For example,
let's say we have four rate limit policies in place:

```
RLP A -> HTTPRoute A (`api.toystore.com`) -> Gateway G (`*.com`)

RLP B -> HTTPRoute B (`other.toystore.com`) -> Gateway G (`*.com`)

RLP H -> HTTPRoute H (`*.toystore.com`) -> Gateway G (`*.com`)

RLP G -> Gateway G (`*.com`)
```

Request 1 (`api.toystore.com`) -> apply RLP A and RLP G

Request 2 (`other.toystore.com`) -> apply RLP B and RLP G

Request 3 (`unknown.toystore.com`) -> apply RLP H and RLP G

Request 4 (`other.com`) -> apply RLP G

**Note**: When a request falls under the scope of multiple policies, all the policies will be applied.
Following the rate limiting design guidelines, the most restrictive policy will be enforced.

### Action configurations

Action configurations are defined via rate limit configuration objects.
The rate limit configuration object is the equivalent of the
[config.route.v3.RateLimit](https://www.envoyproxy.io/docs/envoy/latest/api-v3/config/route/v3/route_components.proto#envoy-v3-api-msg-config-route-v3-ratelimit) envoy object.
One configuration is, in turn, a list of
[rate limit actions](https://www.envoyproxy.io/docs/envoy/latest/api-v3/config/route/v3/route_components.proto#envoy-v3-api-msg-config-route-v3-ratelimit-action).
Each action populates a descriptor entry. A list of descriptor entries compose a descriptor.
A list of descriptors compose a [RateLimitRequest](https://www.envoyproxy.io/docs/envoy/latest/api-v3/service/ratelimit/v3/rls.proto#service-ratelimit-v3-ratelimitrequest).
Each configuration produces, at most, one descriptor.
Depending on the incoming request, one configuration may or may not produce a rate limit descriptor.
These rate limiting configuration rules provide flexibility to produce multiple descriptors.

An example to illustrate

```yaml
configurations:
  - actions:
    - request_headers:
        header_name: "X-MY-CUSTOM-HEADER"
        descriptor_key: "custom-header"
        skip_if_absent: true
  - actions:
    - generic_key:
        descriptor_key: admin
        descriptor_value: "1"
```

A request without "X-MY-CUSTOM-HEADER" would generate one descriptor with one entry:

```
("admin": "1")
```

A request with a header "X-MY-CUSTOM-HEADER=MY-VALUE" would generate two descriptors,
one entry each descriptor:

```
("admin": "1")
("custom-header": "MY-VALUE")
```

**Note**: If one action is not able to populate a descriptor entry, the entire descriptor is discarded.

**Note**: The external rate limiting service will be called only when there is at least one not empty
descriptor.

### Rate limiting configuration rules

Configuration rules allow rate limit configurations to be activated conditionally depending on
the current context (the incoming HTTP request properties).
Each rate limit configuration list can define, optionally, a list of rules to match the request.
A match occurs when *at least* one rule matches the request.

An example to illustrate. Given these rate limit configurations,

```yaml
spec:
    rateLimits:
    - configurations:
      - actions:
        - generic_key:
            descriptor_key: toystore-app
            descriptor_value: "1"
    - rules:
      - hosts: ["api.toystore.com"]
      configurations:
      - actions:
        - generic_key:
            descriptor_key: api
            descriptor_value: "1"
    - rules:
      - hosts: ["admin.toystore.com"]
      configurations:
      - actions:
        - generic_key:
            descriptor_key: admin
            descriptor_value: "1"
```

* When a request for `api.toystore.com` hits the gateway, the descriptors generated would be:

```
("toystore-app", "1")
("api", "1")
```

* When a request for `admin.toystore.com` hits the gateway, the descriptors generated would be:

```
("toystore-app", "1")
("admin", "1")
```

* When a request for `other.toystore.com` hits the gateway, the descriptors generated would be:

```
("toystore-app", "1")
```

**Note**: If rules are not set, it is equivalent to matching all the requests.

### Known limitations

* One HTTPRoute can only be targeted by one rate limit policy.
* One Gateway can only be targeted by one rate limit policy.
* Only supporting HTTPRoute/Gateway references from within the same namespace.
* `hosts` in rules, `spec.rateLimits[].rules`, do not support wildcard prefixes.

## How: Implementation details

### The WASM Filter

On designing kuadrant rate limiting and considering Istio/Envoy's rate limiting offering,
we hit two limitations.

* *Shared Rate Limiting Domain*: The rate limiting
[domain](https://www.envoyproxy.io/docs/envoy/latest/api-v3/extensions/filters/http/ratelimit/v3/rate_limit.proto#envoy-v3-api-msg-extensions-filters-http-ratelimit-v3-ratelimit)
used in the global rate limiting filter in Envoy are shared across the Ingress Gateway.
This is because Istio creates only one filter chain by default at the listener level.
This means the rate limiting filter configuration is shared at the gateway level
(which rate limiting service to call, which domain to use). The triggering of actual rate limiting
calls happens at the
[virtual host / route level by adding actions and descriptors](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/rate_limit_filter#rate-limit).
This need to have shared domains causes several issues:
  * All rate limit configurations applied to limitador need to use a shared domain or set of
  shared domains (when using stages). This means that for each rate limiting request,
  limitador will need to iterate through each of the rate limit resources within the shared domain
  and evaluate each of their conditions to find which one applies. As the number of APIs increases
  so would the number of resources that limitador would need to evaluate.
  * With a shared domain comes the risk of a clash. To avoid a potential clash, either the user or
  Kuadrant controller would need to inject a globally unique condition into each rate limit
  resource.
* *Limited ability to invoke rate limiting based on the method or path*: Although Envoy supports
applying rate limits at both the virtual host and also the route level, via Istio this currently
only works if you are using a VirtualService. This is because the EnvoyFilter needed to configure
rate limiting needs a
[named route](https://istio.io/latest/docs/reference/config/networking/envoy-filter/#EnvoyFilter-RouteConfigurationMatch-RouteMatch)
in order to match and apply a change to a specific route. This means for non VirtualService routing
(IE HTTPRoute) path, header and method conditional rules must all be applied in Limitador directly
which naturally creates additional load on Limitador, latency for endpoints that don’t need/want
rate limiting and the descriptors needed to apply rate limiting rules must all be defined at the
host level rather than based on the path / method. Issues capturing this limitation are linked
below:
  * https://github.com/istio/istio/issues/36790
  * https://github.com/istio/istio/issues/37346
  * https://github.com/kubernetes-sigs/gateway-api/pull/996

Therefore, not giving up entirely in existing
[Envoy's RateLimit Filter](https://www.envoyproxy.io/docs/envoy/latest/api-v3/extensions/filters/network/ratelimit/v3/rate_limit.proto#extension-envoy-filters-network-ratelimit),
we decided to move on and leverage the Envoy's
[Wasm Network Filter](https://www.envoyproxy.io/docs/envoy/latest/configuration/listeners/network_filters/wasm_filter)
and implement rate limiting [wasm-shim](https://github.com/Kuadrant/wasm-shim)
module compliant with the Envoy's
[Rate Limit Service (RLS)](https://www.envoyproxy.io/docs/envoy/latest/api-v3/service/ratelimit/v3/rls.proto).
This wasm-shim module accepts a [PluginConfig](https://github.com/Kuadrant/kuadrant-controller/blob/fa2b52967409b7c4ea2c2e3412ecf80a8ad2b802/pkg/istio/wasm.go#L24)
struct object as input configuration object.

WASM filter configuration object ([PluginConfig](https://github.com/Kuadrant/wasm-shim/blob/0b8a12a66fd0d511cb487338f0eb5d9d021fb082/src/configuration.rs) struct):

```yaml
#  The filter’s behaviour in case the rate limiting service does not respond back. When it is set to true, Envoy will not allow traffic in case of communication failure between rate limiting service and the proxy.
failure_mode_deny: true
rate_limit_policies:
  - name: toystore
    rate_limit_domain: toystore-app
    upstream_cluster: rate-limit-cluster
    hostnames: ["*.toystore.com"]
    gateway_actions:
      - rules:
          - paths: ["/admin/toy"]
            methods: ["GET"]
            hosts: ["pets.toystore.com"]
        configurations:
          - actions:
            - generic_key:
                descriptor_key: admin
                descriptor_value: "1"
```

The WASM filter configuration resources are part of the  internal configuration
and therefore not exposed to the end user.

At the WASM filter level, there are no HTTPRoute level or Gateway level rate limit policies.
The rate limit policies in the wasm plugin configuration may not map 1:1 to
user managed RateLimitPolicy custom resources. WASM rate limit policies have an internal logical
name and a set of hostnames to activate it based on the incoming request’s host header.

Kuadrant deploys one WASM filter for rate limiting per gateway. Only when rate limiting needs to be
applied in a gateway.

The WASM filter builds a tree based data structure holding the rate limit policies.
The longest (sub)domain match is used to select the policy to be applied.
Only one policy is being applied per invocation.
