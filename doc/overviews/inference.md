# Kuadrant Inference

A Kuadrant inference custom resource, such as "PromptGuardPolicy" or "TokenRateLimitPolicy":

1. Targets Gateway API networking resources such as [HTTPRoutes](https://gateway-api.sigs.k8s.io/reference/spec/#gateway.networking.k8s.io/v1.HTTPRoute) and [Gateways](https://gateway-api.sigs.k8s.io/reference/spec/#gateway.networking.k8s.io/v1.Gateway), using these resources to obtain additional context, i.e., which traffic workload (HTTP attributes, hostnames, user attributes, etc) to apply inference policies to.
2. Supports targeting subsets (sections) of a network resource to apply the policies to.
3. Also supports targeting a specific inference 'model', specified in the request body.
4. Enables cluster operators to set defaults that govern behavior at the lower levels of the network, until a more specific policy is applied.
5. Enables platform engineers to set overrides over policies and/or individual policy rules specified at the lower levels of the network.

## How it works

### Envoy's External Processing Protocol

Kuadrant integrates an [External Processing](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/ext_proc_filter) service ("__REPLACE_WITH_NAME_OF_INFERENCE_SERVICE__") that is triggered on matching HTTP contexts.

The workflow per request goes:

1. On incoming request, the gateway checks the matching rules for enforcing the inference policy rules, as stated in the PromptGuardPolicy/TokenRateLimitPolicy custom resources and targeted Gateway API networking objects
2. If the request matches, the gateway sends a [ProcessingRequest](https://www.envoyproxy.io/docs/envoy/latest/api-v3/service/ext_proc/v3/external_processor.proto#envoy-v3-api-msg-service-ext-proc-v3-processingrequest) to __REPLACE_WITH_NAME_OF_INFERENCE_SERVICE__.
3. The external inference service responds with a [ProcessingResponse](https://www.envoyproxy.io/docs/envoy/latest/api-v3/service/ext_proc/v3/external_processor.proto#service-ext-proc-v3-processingresponse) back to the gateway for each stage of the request (e.g. request headers, request body, response headers, response body)

An inference policy custom resource and its targeted Gateway API networking resource contain all the statements to configure both the ingress gateway and the external inference service.

### The PromptGuardPolicy & TokenRateLimitPolicy custom resources

```yaml
apiVersion: kuadrant.io/v1alpha1
kind: PromptGuardPolicy
metadata:
  name: prompt-guard
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: my-llm-gateway
  model:
    url: http://huggingface-granite-guardian-default.example.com/v1
    apiKey:
      secretRef:
        name: granite-api-key
        key: token
  categories:
    - harm
    - violence
    - sexual_content
  response:
    unauthorized:
      headers:
        "content-type":
          value: application/json
      body:
        value: |
          {
            "error": "Unauthorized",
            "message": "Request prompt blocked by content policy."
          }
  rules: { … }
```

```yaml
apiVersion: kuadrant.io/v1alpha1
kind: TokenRateLimitPolicy
metadata:
  name: token-limit-free
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: my-llm-gateway
  limit:
    rate:
      limit: 20000
      window: 1d
    predicate: 'request.auth.claims["kuadrant.io/groups"].split(",").exists(g, g == "free")' 
    counter: auth.identity.userid
  rules: { … }
```

## Using the PromptGuardPolicy & TokenRateLimitPolicy

### Targeting a HTTPRoute networking resource

When targeting a HTTPRoute, an PromptGuardPolicy or TokenRateLimitPolicy can be enforced on:
- all traffic routed by any rule specified in the HTTPRoute; or
- only traffic routed by a specific set of rules as stated in a selected HTTPRouteRule of the HTTPRoute, by specifying the `sectionName` field in the target reference (`spec.targetRef`) of the policy.

Either way, the policy applies across all hostnames (`spec.hostnames`) and Gateways (`spec.parentRefs`) referenced in the HTTPRoute, provided the route is properly attached to the corresponding Gateway listeners.

Additional filters for applying the policy can be set by specifying top-level conditions in the policy (`spec.rules.when`).

**Example 1** - Targeting an entire HTTPRoute

```yaml
apiVersion: kuadrant.io/v1alpha1
kind: TokenRateLimitPolicy
metadata:
  name: my-limit
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: my-route
  rules: { … }
```

```
┌─────────────────────┐            ┌───────────────────────────────┐
│ (Gateway namespace) │            │   (App namespace)             │
│                     │            │                               │
│    ┌─────────┐      │ parentRefs │  ┌────────────┐               │
│    │ Gateway │◄─────┼────────────┼──┤ HTTPRoute  │               │
│    └─────────┘      │            │  | (my-route) │               |
│                     │            │  └────────────┘               │
│                     │            │        ▲                      │
│                     │            │        │                      │
│                     │            │        │ targetRef            │
│                     │            │        │                      │
│                     │            │  ┌─────┴────────────────┐     │
│                     │            │  │ TokenRateLimitPolicy │     │
│                     │            │  │ (my-limit)           │     │
│                     │            │  └──────────────────────┘     │
└─────────────────────┘            └───────────────────────────────┘
```

**Example 2** - Targeting a specific set of rules of a HTTPRoute

```yaml
apiVersion: kuadrant.io/v1alpha1
kind: TokenRateLimitPolicy
metadata:
  name: my-route-limit
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: my-route
    sectionName: rule-2
  rules: { … }
```

```
┌─────────────────────┐            ┌───────────────────────────────┐
│ (Gateway namespace) │            │    (App namespace)            │
│                     │            │                               │
│    ┌─────────┐      │ parentRefs │  ┌────────────┐               │
│    │ Gateway │◄─────┼────────────┼──┤ HTTPRoute  │               │
│    └─────────┘      │            │  | (my-route) │               |
│                     │            │  |------------│               |
│                     │            │  | - rule-1   │               |
│                     │            │  | - rule-2   │               |
│                     │            │  └────────────┘               │
│                     │            │        ▲                      │
│                     │            │        │                      │
│                     │            │        │ targetRef            │
│                     │            │        │                      │
│                     │            │  ┌─────┴────────────────┐     │
│                     │            │  │ TokenRateLimitPolicy │     │
│                     │            │  │ (my-route-limit)     │     │
│                     │            │  └──────────────────────┘     │
└─────────────────────┘            └───────────────────────────────┘
```

### Targeting a Gateway networking resource

An PromptGuardPolicy or TokenRateLimitPolicy that targets a Gateway, without overrides, will be enforced to all HTTP traffic hitting the gateway, unless a more specific policy targeting a matching HTTPRoute exists. Any new HTTPRoute referrencing the gateway as parent will be automatically covered by the gateway-targeting PromptGuardPolicy or TokenRateLimitPolicy, as well as changes in the existing HTTPRoutes.

Target a Gateway HTTPRoute by setting the `spec.targetRef` field of the PromptGuardPolicy or TokenRateLimitPolicy as follows:

```yaml
apiVersion: kuadrant.io/v1alpha1
kind: TokenRateLimitPolicy
metadata:
  name: my-gw-limit
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: my-gw
  defaults: # alternatively: `overrides`
    rules: { … }
```

```
┌──────────────────────────┐             ┌─────────────────────────┐
│ (Infra namespace)        │             │    (App namespace)      │
│                          │             │                         │
│  ┌─────────┐             │  parentRefs │  ┌───────────┐          │
│  │ Gateway │◄────────────┼─────────────┼──┤ HTTPRoute │          │
│  | (my-gw) |             │             │  └───────────┘          │
│  └─────────┘             │             │        ▲                │
│       ▲                  │             │        |                │
│       │                  │             │        │                │
│       │ targetRef        │             │        │ targetRef      │
│       │                  │             │        │                │
│ ┌─────┴───────────────┐  │             │ ┌──────┴──────────────┐ │
│ │ TokenRateLimitPolicy│  │             │ │ TokenRateLimitPolicy│ │
│ | (my-gw-limit)       |  │             │ │ (my-route-limit)    │ │
│ └─────────────────────┘  │             │ └─────────────────────┘ │
└──────────────────────────┘             └─────────────────────────┘
```

### Defaults and Overrides

Kuadrant PromptGuardPolicies & TokenRateLimitPolicies support Defaults & Overrides essentially as specified in Gateway API [GEP-2649](https://gateway-api.sigs.k8s.io/geps/gep-2649/).

An PromptGuardPolicy or TokenRateLimitPolicy can declare a block of _defaults_ (`spec.defaults`) or a block of _overrides_ (`spec.overrides`). By default, policies that do not specify neither `defaults` nor `overrides`, act implicitly as if specifying `defaults`. A default set of policy rules are enforced until a more specific set supersedes them. In contrast, a set of overrides wins over any more specific set of rules.

Setting _default_ PromptGuardPolicies & TokenRateLimitPolicies provide, e.g., platform engineers with the ability to protect there inference models against overloading/excessive use and undesired content, such as by setting preemptive prompt guarding of all risk categories at the level of the gateways. Later on, application developers can define more specific guard rules at the level of the HTTPRoutes.

Inversely, a gateway policy that specify _overrides_ declares a set of rules that is enforced on all routes attached to the gateway, thus atomically replacing any more specific policy occasionally attached to any of those routes.

Although typical examples involve specifying `defaults` and `overrides` at the level of the Gateway object which interact with sets of policy rules defined at the more specific context (HTTPRoute), Defaults & Overrides are actually transversal to object kinds. One can define inference policies with `defaults` or `overrides` at any level of the following hierarchy and including multiple policies at the same level:
1. Gateway
2. Gateway listener (by targeting a Gateway with `sectionName`)
3. HTTPRoute
4. HTTPRouteRule (by targeting a HTTPRoute with `sectionName`)

The final set of policy rules to enforce for a given request, known as "effective policy", is computed based on the basic principles stated in the [Hierarchy](https://gateway-api.sigs.k8s.io/geps/gep-2649/#hierarchy) section of GEP-2649 and [Conflict Resolution](https://gateway-api.sigs.k8s.io/geps/gep-2649/#conflict-resolution) of its predecessor [GEP-713](https://gateway-api.sigs.k8s.io/geps/gep-713/#conflict-resolution), for the hierarchical levels above.

Kuadrant PromptGuardPolicies & TokenRateLimitPolicies extend Gateway API's Defaults & Overrides with additional merge strategies for allowing users to specify sets of policy rules under `defaults` and/or `overrides` blocks that can be either _atomically_ applied or _merged_ into a composition of policy rules from the multiple PromptGuardPolicies & TokenRateLimitPolicies affecting a hierarchy of newtworking objects. The name of the policy rule is used for detecting conflicts.

For details of the behavior of Defaults & Overrides, including supported merge strategies, see [RFC-0009](https://github.com/Kuadrant/architecture/blob/main/rfcs/0009-defaults-and-overrides.md).

### Hostnames and wildcards

If a PromptGuardPolicy or TokenRateLimitPolicy targets a route defined for a hostname wildcard `*.com` and a second  policy targets another route for a hostname `api.com`, without any overrides nor merges in place, the policies will be enforced according to the principle of "the more specific wins". E.g., a request coming for `api.com` will be protected according to the rules from a PromptGuardPolicy that targets the route for `api.com`, while a request for `other.com` will be protected with the rules from the PromptGuardPolicy targeting the route for `*.com`. One should not expect both set of policy rules to be enforced on requests to `api.com` simply because both hostname and wildcard match.

Example with 3 PromptGuardPolicy and 3 HTTPRoutes, without merges nor overrides in place:

- PromptGuardPolicy A → HTTPRoute A (`a.toystore.com`)
- PromptGuardPolicy B → HTTPRoute B (`b.toystore.com`)
- PromptGuardPolicy W → HTTPRoute W (`*.toystore.com`)

Expected behavior:

- Request to `a.toystore.com` → PromptGuardPolicy A will be enforced
- Request to `b.toystore.com` → PromptGuardPolicy B will be enforced
- Request to `other.toystore.com` → PromptGuardPolicy W will be enforced

### `when` conditions

`when` conditions can be used to scope a PromptGuardPolicy or TokenRateLimitPolicy (i.e. to filter the traffic to which a policy or policy rule applies) without any coupling to the underlying network topology.

Use `when` conditions to conditionally activate policies and policy rules based on attributes that cannot be expressed in the HTTPRoutes' `spec.hostnames` and `spec.rules.matches` fields, or in general in PromptGuardPolicies or TokenRateLimitPolicies that target a Gateway.

The selectors within the `when` conditions of an PromptGuardPolicy or TokenRateLimitPolicy are a subset of Kuadrant's Well-known Attributes ([RFC 0002](https://github.com/Kuadrant/architecture/blob/main/rfcs/0002-well-known-attributes.md)). Check out the reference for the full list of supported selectors.

### Known limitations

- PromptGuardPolicy & TokenRateLimitPolicy can only target HTTPRoutes/Gateways defined within the same namespace of the them.

## Implementation details

TODO

A Kuadrant wasm-shim configuration for one PromptGuardPolicy custom resources targeting a HTTPRoute looks like the following and it is generated automatically by the Kuadrant control plane:

```yaml
apiVersion: extensions.istio.io/v1alpha1
kind: WasmPlugin
metadata:
  name: kuadrant-kuadrant-ingressgateway
  namespace: gateway-system
spec:
  phase: STATS
  pluginConfig:
    services:
      ai-policy-service:
        type: ext_proc
        endpoint: ai-policy-cluster
        failureMode: allow
    actionSets:
      - name: some_name_0
        routeRuleConditions:
          hostnames:
            - "*.models.website"
            - "*.models.io"
          predicates:
            - request.url_path.startsWith("/openai/v1/completions")
        actions:
          - service: ai-policy-service
            scope: gateway-system/app-promptguard
            predicates:
              - request.host.endsWith('.models.website')
```
