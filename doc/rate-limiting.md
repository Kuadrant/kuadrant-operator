# Kuadrant Rate Limiting

A Kuadrant RateLimitPolicy custom resource, often abbreviated "RLP":

1. Allows it to target Gateway API networking resources such as [HTTPRoutes](https://gateway-api.sigs.k8s.io/v1alpha2/references/spec/#gateway.networking.k8s.io/v1beta1.HTTPRoute) and [Gateways](https://gateway-api.sigs.k8s.io/v1alpha2/references/spec/#gateway.networking.k8s.io/v1beta1.Gateway), using these resources to obtain additional context, i.e., which traffic workload (HTTP attributes, hostnames, user attributes, etc) to rate limit.
2. Allows to specify which specific subsets of the targeted network resource to apply the limits to.
3. Abstracts the details of the underlying Rate Limit protocol and configuration resources, that have a much broader remit and surface area.
4. Supports cluster operators to set overrides (soon) and defaults that govern what can be done at the lower levels.

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
      # use it for filterring attributes not supported by HTTPRouteRule or with RateLimitPolicies that target a Gateway
      # check out Kuadrant RFC 0002 (https://github.com/Kuadrant/architecture/blob/main/rfcs/0002-well-known-attributes.md) to learn more about the Well-known Attributes that can be used in this field
      when: […]
```

## Using the RateLimitPolicy

### Targeting a HTTPRoute networking resource

When a RLP targets a HTTPRoute, the policy is enforced to all traffic routed according to the rules and hostnames specified in the HTTPRoute, across all Gateways referenced in the `spec.parentRefs` field of the HTTPRoute.

The targeted HTTPRoute's rules and/or hostnames to which the policy must be enforced can be filtered to specific subsets, by specifying the `routeSelectors` field of the limit definition.

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
- any of the route rules selected by the limit (via `routeSelectors` or implicit "catch-all" selector), and
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

#### Route selectors

The `routeSelectors` field of the limit definition allows to specify **selectors of routes** (or parts of a route), that _transitively induce a set of conditions for a limit to be enforced_. It is defined as a set of route matching rules, where these rules must exist, partially or identically stated within the HTTPRouteRules of the HTTPRoute that is targeted by the RLP.

The field is typed as a list of objects based on a special type defined from Gateway API's [HTTPRouteMatch](https://gateway-api.sigs.k8s.io/references/spec/#gateway.networking.k8s.io/v1beta1.HTTPPathMatch) type (`matches` subfield of the route selector object), and an additional field `hostnames`.

Route selectors matches and the HTTPRoute's HTTPRouteMatches are pairwise compared to select or not select HTTPRouteRules that should activate a limit. To decide whether the route selector selects a HTTPRouteRule or not, for each pair of route selector HTTPRouteMatch and HTTPRoute HTTPRouteMatch:
1. The route selector selects the HTTPRoute's HTTPRouteRule if the HTTPRouteRule contains at least one HTTPRouteMatch that specifies fields that are literally identical to all the fields specified by at least one HTTPRouteMatch of the route selector.
2. A HTTPRouteMatch within a HTTPRouteRule may include other fields that are not specified in a route selector match, and yet the route selector match selects the HTTPRouteRule if all fields of the route selector match are identically included in the HTTPRouteRule's HTTPRouteMatch; the opposite is NOT true.
3. Each field `path` of a HTTPRouteMatch, as well as each field `method` of a HTTPRouteMatch, as well as each element of the fields `headers` and `queryParams` of a HTTPRouteMatch, is atomic – this is true for the HTTPRouteMatches within a HTTPRouteRule, as well as for HTTPRouteMatches of a route selector.

Additionally, at least one hostname specified in a route selector must identically match one of the hostnames specified (or inherited, when omitted) by the targeted HTTPRoute.

The semantics of the route selectors allows to assertively relate limit definitions to routing rules, with benefits for identifying the subsets of the network that are covered by a limit, while preventing unreachable definitions, as well as the overhead associated with the maintenance of such rules across multiple resources throughout time, according to network topology beneath. Moreover, the requirement of not having to be a full copy of the targeted HTTPRouteRule matches, but only partially identical, helps prevent repetition to some degree, as well as it enables to more easily define limits that scope across multiple HTTPRouteRules (by specifying less rules in the selector).

A few rules and corner cases to keep in mind while using the RLP's `routeSelectors`:
1. **The golden rule –** The route selectors in a RLP are **not** to be read strictly as the route matching rules that activate a limit, but as selectors of the route rules that activate the limit.
2. Due to (1) above, this can lead to cases, e.g., where a route selector that states `matches: [{ method: POST }]` selects a HTTPRouteRule that defines `matches: [{ method: POST },  { method: GET }]`, effectively causing the limit to be activated on requests to the HTTP method `POST`, but **also** to the HTTP method `GET`.
3. The requirement for the route selector match to state patterns that are identical to the patterns stated by the HTTPRouteRule (partially or entirely) makes, e.g., a route selector such as `matches: { path: { type: PathPrefix, value: /foo } }` to select a HTTPRouteRule that defines `matches: { path: { type: PathPrefix, value: /foo }, method: GET }`, but **not** to select a HTTPRouteRule that only defines `matches: { method: GET }`, even though the latter includes technically all HTTP paths; **nor** it selects a HTTPRouteRule that only defines `matches: { path: { type: Exact, value: /foo } }`, even though all requests to the exact path `/foo` are also technically requests to `/foo*`.
4. The atomicity property of fields of the route selectors makes, e.g., a route selector such as `matches: { path: { value: /foo } }` to select a HTTPRouteRule that defines `matches: { path: { value: /foo } }`, but **not** to select a HTTPRouteRule that only defines `matches: { path: { type: PathPrefix, value: /foo } }`. (This case may actually never happen because `PathPrefix` is the default value for `path.type` and will be set automatically by the Kubernetes API server.)

Due to the nature of route selectors of defining pointers to HTTPRouteRules, the `routeSelectors` field is not supported in a RLP that targets a Gateway resource.

#### `when` conditions

`when` conditions can be used to scope a limit (i.e. to filter the traffic to which a limit definition applies) without any coupling to the underlying network topology, i.e. without making direct references to HTTPRouteRules via `routeSelectors`.

The syntax of the `when` conditions selectors comply with Kuadrant's [Well-known Attributes (RFC 0002)](https://github.com/Kuadrant/architecture/blob/main/rfcs/0002-well-known-attributes.md).

Use the `when` conditions to conditionally activate limits based on attributes that cannot be expressed in the HTTPRoutes' `spec.hostnames` and `spec.rules.matches` fields, or in general in RLPs that target a Gateway.

### Known limitations

* One HTTPRoute can only be targeted by one RLP.
* One Gateway can only be targeted by one RLP.
* RLPs can only target HTTPRoutes/Gateways defined within the same namespace of the RLP.

## Examples

Check out the following user guides for examples of rate limiting services with Kuadrant:
* [Simple Rate Limiting for Application Developers](user-guides/simple-rl-for-app-developers.md)
* [Authenticated Rate Limiting for Application Developers](user-guides/authenticated-rl-for-app-developers.md)
* [Gateway Rate Limiting for Cluster Operators](user-guides/gateway-rl-for-cluster-operators.md)
* [Authenticated Rate Limiting with JWTs and Kubernetes RBAC](user-guides/authenticated-rl-with-jwt-and-k8s-authnz.md)
