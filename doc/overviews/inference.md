# Kuadrant Inference

A Kuadrant inference custom resource, such as "PromptGuardPolicy" or "TokenRateLimitPolicy":

1. Targets Gateway API networking resources such as [HTTPRoutes](https://gateway-api.sigs.k8s.io/reference/spec/#gateway.networking.k8s.io/v1.HTTPRoute) and [Gateways](https://gateway-api.sigs.k8s.io/reference/spec/#gateway.networking.k8s.io/v1.Gateway), using these resources to obtain additional context, i.e., which traffic workload (HTTP attributes, hostnames, user attributes, etc) to apply inference policies to.
2. Supports targeting subsets (sections) of a network resource to apply the policies to.
3. Enables cluster operators to set defaults that govern behavior at the lower levels of the network, until a more specific policy is applied.
4. Enables platform engineers to set overrides over policies and/or individual policy rules specified at the lower levels of the network.

## How it works

### Envoy's External Processing Protocol

Kuadrant integrates an external service that talks to the Gateway using the [External Processing](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/ext_proc_filter) protocol. This service, [Inferno](https://github.com/Kuadrant/inferno), is the one ensuring that kuadrant inference policies are enforced.

The workflow per request goes:

1. On incoming request, the gateway checks the matching rules for enforcing the inference policy rules, as stated in the PromptGuardPolicy/TokenRateLimitPolicy custom resources and targeted Gateway API networking objects
2. If the request matches, the gateway sends a [ProcessingRequest](https://www.envoyproxy.io/docs/envoy/latest/api-v3/service/ext_proc/v3/external_processor.proto#envoy-v3-api-msg-service-ext-proc-v3-processingrequest) to Inferno on the specified grpc port for that service.
3. The request body is parsed by Inferno to pull out the model and prompt (e.g. for prompt guarding)
4. The external inference service responds with a [ProcessingResponse](https://www.envoyproxy.io/docs/envoy/latest/api-v3/service/ext_proc/v3/external_processor.proto#service-ext-proc-v3-processingresponse) back to the gateway for each stage of the request (e.g. request headers, request body, response headers, response body)
5. For the response phase, the response is parsed by Inferno to pull out the response text (e.g. for risk assessment of the response)

An inference policy custom resource and its targeted Gateway API networking resource contain all the statements to configure both the ingress gateway and Inferno. The gateway provides some context from the incoming request.
Inferno will use that context as well as its own configuration to enforce policies.

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
  filters:
    "over-18":
      categories:
        filter: 
          - hate
          - discrimination
      when:
        - predicate: "auth.identity.age >= 18"
    "under-18":
      categories:
        filter: 
            - hate
            - discrimination
            - harm
            - sexual_content
            - violence
      when:
        - predicate: "auth.identity.age < 18"
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
  when:
    - predicate: 'request.auth.claims["kuadrant.io/groups"].split(",").exists(g, g == "free")'
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
    when:
      - predicate: 'request.auth.claims["kuadrant.io/groups"].split(",").exists(g, g == "free")' 
    counters:
      - expression: auth.identity.userid
```

## Using the PromptGuardPolicy & TokenRateLimitPolicy

### Targeting a HTTPRoute networking resource

When targeting a HTTPRoute, an PromptGuardPolicy or TokenRateLimitPolicy can be enforced on:
- all traffic routed by any rule specified in the HTTPRoute; or
- only traffic routed by a specific set of rules as stated in a selected HTTPRouteRule of the HTTPRoute, by specifying the `sectionName` field in the target reference (`spec.targetRef`) of the policy.

Either way, the policy applies across all hostnames (`spec.hostnames`) and Gateways (`spec.parentRefs`) referenced in the HTTPRoute, provided the route is properly attached to the corresponding Gateway listeners.

Additional filters for applying the policy can be set by specifying top-level conditions in the policy (`spec.when`).

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
  limits: { … }
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
  limits: { … }
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
    limits: { … }
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

### Inference service

The Inference service can be directly integrated with Envoy Proxy, or integrated indirectly via Gateway API and Kuadrant Policy APIs with the Kuadrant operator.

#### Direct Integration with Envoy Proxy

With direct integration, the service can be used without the Kuadrant operator.
It depends on Envoy Proxy, without Gateway API or a Gateway API provider.
However, the biggest limitation with this mode is that the configuration is static.
For example, you configure the service with the prompt guarding LLM and risk categories once on startup.
All requests that are checked by the service are subject to the same prompt guarding configuration.

Here is a request flow diagram for this integration:

![ai_service_direct_integration](./Arch%20Overview%20v1%20-%20ai_service_direct_integration.jpg)

In this setup, Envoy Proxy is configured manually with the service as an 'ExternalProcessor' (ext_proc) filter.
The service is configured as a 'cluster' in Enovy Proxy.
The service listens on a different grpc port for each feature (like prompt guarding or semantic caching).
This allows for different features to be executed at different stages of the request lifecycle.

#### Integration via Gateway API and Kuadrant Policies

The service can integrate with Gateway API and the Kuadrant operator, providing additional configuration options and features.
The service is set up automatically by the Kuadrant operator via a WasmPlugin & EnvoyFilter (Istio), or EnvoyPatchPolicy & EnvoyExtensionPolicy (Envoy Gateway).
Default configuration must be provided to the inference service via env vars on startup (like the prompt guarding LLM to use).
Users can configure which gateways, listeners, routes or paths that an inference policy should apply to.
This is done via the policy resources (i.e. PromptGuardPolicy, TokenRateLimitPolicy) and how they target Gateways and HTTPRotues.
Some service configuration can then be set differently per policy.
For example, having a different set of risk categories being checked for a specifc HTTPRoute, rather than the defaults across the entire set of Gateways.
This configuration is passed from the wasm-shim to the inference service at request time as additional context.
Here is a request flow diagram for this integration.

![ai_service_gateway_api_kuadrant](./Arch%20Overview%20v1%20-%20ai_service_gateway_api_kuadrant.jpg)

Any additional context is passed to the inference service as request headers.
For example, `x-kuadrant-guard-categories: harm,violence`.
The inference service will detect these optional headers and change the execution path accordingly.
One security consideration with using headers to pass the context is exploitation by users/cients.
In particular when running where there is no wasm-shim in between the user and the inference service.
To alleviate this risk, the set of known headers should be removed by Envoy proxy using the `request_headers_to_remove` [configuration](https://www.envoyproxy.io/docs/envoy/latest/api-v3/config/route/v3/route.proto).

__NOTE:__ At this time, the token based rate limiting feature is only available when integrating with the Kuadrant operator. This is because of how limiting is applied by limitador at the wasm-shim layer, not the inference service layer. The inference service is responsible for gathering and reporting on token usage stats, not the actual limiting.

TODO: Add details on token rate limting implemenation after these issues are further along:

* https://github.com/Kuadrant/kuadrant-operator/issues/1341
* https://github.com/Kuadrant/kuadrant-operator/issues/1342
* https://github.com/Kuadrant/kuadrant-operator/issues/1343

### Wasm-shim configuration

A Kuadrant wasm-shim configuration for one PromptGuardPolicy custom resources targeting a HTTPRoute looks like the following and is generated automatically by the Kuadrant control plane.
Note the 2 different `services` in the `pluginConfig`, both using `ext_proc`.
These both ultimately route to the same `cluster`, but on different ports.

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
      inference-promptguard-service:
        type: ext_proc
        endpoint: kuadrant-inference-promptguard-service
        failureMode: deny
      inference-tokenratelimit-service:
        type: ext_proc
        endpoint: kuadrant-inference-tokenratelimit-service
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
          - service: inference-promptguard-service
            scope: gateway-system/app-promptguard
            predicates:
              - request.host.endsWith('.models.website')
      - name: some_name_1
        routeRuleConditions:
          hostnames:
            - "*.models.website"
          predicates:
            - request.url_path.startsWith("/openai/v1/completions")
        actions:
          - service: inference-tokenratelimit-service
            scope: gateway-system/app-tokenratelimit
            predicates:
              - request.host.endsWith('.models.website')
```

Here is an example EnvoyFilter that configures the `cluster` for the wasm-shim to route to.
Note the 2 different patches and ports going to the same `cluster` for the Inference service.
The service is listening on different grpc ports for each feature to allow them to be executed at different points in the chain.

```yaml
apiVersion: networking.istio.io/v1alpha3
kind: EnvoyFilter
metadata:
  name: my-inference-gateway
  namespace: my-inference-gateway-ns
spec:
  configPatches:
    - applyTo: CLUSTER
      match:
        cluster:
          service: inference-service.kuadrant-system.svc.cluster.local
      patch:
        operation: ADD
        value:
          connect_timeout: 1s
          http2_protocol_options: {}
          lb_policy: ROUND_ROBIN
          load_assignment:
            cluster_name: kuadrant-inference-promptguard-service
            endpoints:
              - lb_endpoints:
                  - endpoint:
                      address:
                        socket_address:
                          address: inference-service.kuadrant-system.svc.cluster.local
                          port_value: 9090
          name: kuadrant-inference-promptguard-service
          type: STRICT_DNS
    - applyTo: CLUSTER
      match:
        cluster:
          service: inference-service.kuadrant-system.svc.cluster.local
      patch:
        operation: ADD
        value:
          connect_timeout: 1s
          http2_protocol_options: {}
          lb_policy: ROUND_ROBIN
          load_assignment:
            cluster_name: kuadrant-inference-tokenratelimit-service
            endpoints:
              - lb_endpoints:
                  - endpoint:
                      address:
                        socket_address:
                          address: inference-service.kuadrant-system.svc.cluster.local
                          port_value: 9091
          name: kuadrant-inference-tokenratelimit-service
          type: STRICT_DNS
```