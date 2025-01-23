# Kuadrant Auth

A Kuadrant AuthPolicy custom resource:

1. Targets Gateway API networking resources [HTTPRoute](https://gateway-api.sigs.k8s.io/reference/spec/#gateway.networking.k8s.io/v1.HTTPRoute) and [Gateway](https://gateway-api.sigs.k8s.io/reference/spec/#gateway.networking.k8s.io/v1.Gateway), using these to obtain the auth context, i.e., on which traffic workload (HTTP attributes, hostnames, user attributes, etc) to enforce auth.
2. Supports targeting subsets (sections) of a network resource to apply the auth rules to, i.e. specific listeners of a Gateway or HTTP route rules of an HTTPRoute.
3. Abstracts the details of the underlying external authorization protocol and configuration resources, that have a much broader remit and surface area.
4. Enables platform engineers to set defaults that govern behavior at the lower levels of the network, until a more specific policy is applied.
5. Enables platform engineers to set overrides over policies and/or individual policy rules specified at the lower levels of the network.

## How it works

### Integration

Kuadrant's integrates an [External Authorization](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/ext_authz_filter) service ("Authorino") that is triggered on matching HTTP contexts.

The workflow per request goes:

1. On incoming request, the gateway checks the matching rules for enforcing the auth rules, as stated in the AuthPolicy custom resources and targeted Gateway API networking objects
2. If the request matches, the gateway sends a [CheckRequest](https://www.envoyproxy.io/docs/envoy/latest/api-v3/service/auth/v3/external_auth.proto#envoy-v3-api-msg-service-auth-v3-checkrequest) to Authorino.
3. The external auth service responds with a [CheckResponse](https://www.envoyproxy.io/docs/envoy/latest/api-v3/service/auth/v3/external_auth.proto#service-auth-v3-checkresponse) back to the gateway with either an `OK` or `DENIED` response code.

An AuthPolicy and its targeted Gateway API networking resource contain all the statements to configure both the ingress gateway and the external auth service.

### The AuthPolicy custom resource

#### Overview

The `AuthPolicy` spec includes the following parts:

- A reference to an existing Gateway API resource (`spec.targetRef`)
- Authentication/authorization scheme (`spec.rules`)
- Top-level additional conditions (`spec.when`)
- List of named patterns (`spec.patterns`)

The auth scheme specify rules for:

- Authentication (`spec.rules.authentication`)
- External auth metadata fetching (`spec.rules.metadata`)
- Authorization (`spec.rules.authorization`)
- Custom response items (`spec.rules.response`)
- Callbacks (`spec.rules.callbacks`)

Each auth rule can declare specific `when` conditions for the rule to apply.

The auth scheme (`rules`), as well as conditions and named patterns can be declared at the top-level level of the spec (with the semantics of _defaults_) or alternatively within explicit `defaults` or `overrides` blocks.

Check out the [API reference](../reference/authpolicy.md) for a full specification of the AuthPolicy CRD.

## Using the AuthPolicy

### Targeting a HTTPRoute networking resource

When targeting a HTTPRoute, an AuthPolicy can be enforced on:
- all traffic routed by the any rules specified in the HTTPRoute; or
- only traffic routed by a specific set of rules as stated in a selected HTTPRouteRule of the HTTPRoute, by specifying the `sectionName` field in the target reference (`spec.targetRef`) of the policy.

Either way, the policy applies across all hostnames (`spec.hostnames`) and Gateways (`spec.parentRefs`) referenced in the HTTPRoute, provided the route is properly attached to the corresponding Gateway listeners.

Additional filters for applying the policy can be set by specifying top-level conditions in the policy (`spec.rules.when`).

**Example 1** - Targeting an entire HTTPRoute

```yaml
apiVersion: kuadrant.io/v1
kind: AuthPolicy
metadata:
  name: my-auth
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: my-route
  rules: { … }
```

```
┌─────────────────────┐            ┌─────────────────────┐
│ (Gateway namespace) │            │   (App namespace)   │
│                     │            │                     │
│    ┌─────────┐      │ parentRefs │  ┌────────────┐     │
│    │ Gateway │◄─────┼────────────┼──┤ HTTPRoute  │     │
│    └─────────┘      │            │  | (my-route) │     |
│                     │            │  └────────────┘     │
│                     │            │        ▲            │
│                     │            │        │            │
│                     │            │        │ targetRef  │
│                     │            │        │            │
│                     │            │  ┌─────┴──────┐     │
│                     │            │  │ AuthPolicy │     │
│                     │            │  │ (my-auth)  │     │
│                     │            │  └────────────┘     │
└─────────────────────┘            └─────────────────────┘
```

**Example 2** - Targeting a specific set of rules of a HTTPRoute

```yaml
apiVersion: kuadrant.io/v1
kind: AuthPolicy
metadata:
  name: my-route-auth
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: my-route
    sectionName: rule-2
  rules: { … }
```

```
┌─────────────────────┐            ┌──────────────────────┐
│ (Gateway namespace) │            │    (App namespace)   │
│                     │            │                      │
│    ┌─────────┐      │ parentRefs │  ┌────────────┐      │
│    │ Gateway │◄─────┼────────────┼──┤ HTTPRoute  │      │
│    └─────────┘      │            │  | (my-route) │      |
│                     │            │  |------------│      |
│                     │            │  | - rule-1   │      |
│                     │            │  | - rule-2   │      |
│                     │            │  └────────────┘      │
│                     │            │        ▲             │
│                     │            │        │             │
│                     │            │        │ targetRef   │
│                     │            │        │             │
│                     │            │  ┌─────┴───────────┐ │
│                     │            │  │   AuthPolicy    │ │
│                     │            │  │ (my-route-auth) │ │
│                     │            │  └─────────────────┘ │
└─────────────────────┘            └──────────────────────┘
```

### Targeting a Gateway networking resource

An AuthPolicy that targets a Gateway, without overrides, will be enforced to all HTTP traffic hitting the gateway, unless a more specific AuthPolicy targeting a matching HTTPRoute exists. Any new HTTPRoute referrencing the gateway as parent will be automatically covered by the gateway-targeting AuthPolicy, as well as changes in the existing HTTPRoutes.

Target a Gateway HTTPRoute by setting the `spec.targetRef` field of the AuthPolicy as follows:

```yaml
apiVersion: kuadrant.io/v1
kind: AuthPolicy
metadata:
  name: my-gw-auth
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: my-gw
  defaults: # alternatively: `overrides`
    rules: { … }
```

```
┌───────────────────┐             ┌──────────────────────┐
│ (Infra namespace) │             │    (App namespace)   │
│                   │             │                      │
│  ┌─────────┐      │  parentRefs │  ┌───────────┐       │
│  │ Gateway │◄─────┼─────────────┼──┤ HTTPRoute │       │
│  | (my-gw) |      │             │  └───────────┘       │
│  └─────────┘      │             │        ▲             │
│       ▲           │             │        |             │
│       │           │             │        │             │
│       │ targetRef │             │        │ targetRef   │
│       │           │             │        │             │
│ ┌─────┴────────┐  │             │  ┌─────┴───────────┐ │
│ │  AuthPolicy  │  │             │  │   AuthPolicy    │ │
│ | (my-gw-auth) |  │             │  │ (my-route-auth) │ │
│ └──────────────┘  │             │  └─────────────────┘ │
└───────────────────┘             └──────────────────────┘
```

### Defaults and Overrides

Kuadrant AuthPolicies support Defaults & Overrides essentially as specified in Gateway API [GEP-2649](https://gateway-api.sigs.k8s.io/geps/gep-2649/).

An AuthPolicy can declare a block of _defaults_ (`spec.defaults`) or a block of _overrides_ (`spec.overrides`). By default, policies that do not specify neither `defaults` nor `overrides`, act implicitly as if specifying `defaults`. A default set of policy rules are enforced until a more specific set supersedes them. In contrast, a set of overrides wins over any more specific set of rules.

Setting _default_ AuthPolicies provide, e.g., platform engineers with the ability to protect the infrastructure against unplanned and malicious network traffic attempt, such as by setting preemptive "deny-all" policies at the level of the gateways that block access on all routes attached o the gateway. Later on, application developers can define more specific auth rules at the level of the HTTPRoutes, opening access to individual routes.

Inversely, a gateway policy that specify _overrides_ declares a set of rules that is enforced on all routes attached to the gateway, thus atomically replacing any more specific policy occasionally attached to any of those routes.

Although typical examples involve specifying `defaults` and `overrides` at the level of the Gateway object which interact with sets of policy rules defined at the more specific context (HTTPRoute), Defaults & Overrides are actually transversal to object kinds. One can define AuthPolicies with `defaults` or `overrides` at any level of the following hierarchy and including multiple policies at the same level:
1. Gateway
2. Gateway listener (by targeting a Gateway with `sectionName`)
3. HTTPRoute
4. HTTPRouteRule (by targeting a HTTPRoute with `sectionName`)

The final set of policy rules to enforce for a given request, known as "effective policy", is computed based on the basic principles stated in the [Hierarchy](https://gateway-api.sigs.k8s.io/geps/gep-2649/#hierarchy) section of GEP-2649 and [Conflict Resolution](https://gateway-api.sigs.k8s.io/geps/gep-2649/#conflict-resolution) of its predecessor [GEP-713](https://gateway-api.sigs.k8s.io/geps/gep-713/#conflict-resolution), for the hierarchical levels above.

Kuadrant AuthPolicies extend Gateway API's Defaults & Overrides with additional merge strategies for allowing users to specify sets of policy rules under `defaults` and/or `overrides` blocks that can be either _atomically_ applied or _merged_ into a composition of policy rules from the multiple AuthPolicies affecting a hierarchy of newtworking objects. The name of the policy rule is used for detecting conflicts.

For details of the behavior of Defaults & Overrides for the AuthPolicies covering all supported merge strategies, see [RFC-0009](https://github.com/Kuadrant/architecture/blob/main/rfcs/0009-defaults-and-overrides.md).

### Hostnames and wildcards

If an AuthPolicy targets a route defined for a hostname wildcard `*.com` and a second AuthPolicy targets another route for a hostname `api.com`, without any overrides nor merges in place, the policies will be enforced according to the principle of "the more specific wins". E.g., a request coming for `api.com` will be protected according to the rules from the AuthPolicy that targets the route for `api.com`, while a request for `other.com` will be protected with the rules from the AuthPolicy targeting the route for `*.com`. One should not expect both set of policy rules to be enforced on requests to `api.com` simply because both hostname and wildcard match.

Example with 3 AuthPolicies and 3 HTTPRoutes, without merges nor overrides in place:

- AuthPolicy A → HTTPRoute A (`a.toystore.com`)
- AuthPolicy B → HTTPRoute B (`b.toystore.com`)
- AuthPolicy W → HTTPRoute W (`*.toystore.com`)

Expected behavior:

- Request to `a.toystore.com` → AuthPolicy A will be enforced
- Request to `b.toystore.com` → AuthPolicy B will be enforced
- Request to `other.toystore.com` → AuthPolicy W will be enforced

### `when` conditions

`when` conditions can be used to scope an AuthPolicy or auth rule within an AuthPolicy (i.e. to filter the traffic to which a policy or policy rule applies) without any coupling to the underlying network topology.

Use `when` conditions to conditionally activate policies and policy rules based on attributes that cannot be expressed in the HTTPRoutes' `spec.hostnames` and `spec.rules.matches` fields, or in general in AuthPolicies that target a Gateway.

`when` conditions in an AuthPolicy are compatible with Authorino [conditions](https://docs.kuadrant.io/latest/authorino/docs/features/#common-feature-conditions-when), thus supporting complex boolean expressions with AND and OR operators, as well as grouping.

The selectors within the `when` conditions of an AuthPolicy are a subset of Kuadrant's Well-known Attributes ([RFC 0002](https://github.com/Kuadrant/architecture/blob/main/rfcs/0002-well-known-attributes.md)). Check out the reference for the full list of supported selectors.

Authorino [JSON path string modifiers](https://docs.kuadrant.io/latest/authorino/docs/features/#string-modifiers) can also be applied to the selectors within the `when` conditions of an AuthPolicy.

## Examples

Check out the following user guides for examples of protecting services with Kuadrant:

- [Enforcing authentication & authorization with Kuadrant AuthPolicy, for app developers and platform engineers](../user-guides/auth/auth-for-app-devs-and-platform-engineers.md)
- [Authenticated Rate Limiting for Application Developers](../user-guides/ratelimiting/authenticated-rl-for-app-developers.md)
- [Authenticated Rate Limiting with JWTs and Kubernetes RBAC](../user-guides/ratelimiting/authenticated-rl-with-jwt-and-k8s-authnz.md)

## Known limitations

- AuthPolicies can only target HTTPRoutes/Gateways defined within the same namespace of the AuthPolicy.
- AuthPolicies that reference other Kubernetes objects (typically `Secret`s) require those objects to the created in the same namespace as the `Kuadrant` custom resource managing the deployment. This is the case of AuthPolicies that define API key authentication with `allNamespaces` option set to `false` (default), where the API key Secrets must be created in the Kuadrant CR namespace and not in the AuthPolicy namespace.
