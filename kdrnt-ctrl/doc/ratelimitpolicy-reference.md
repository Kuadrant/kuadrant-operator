# The RateLimitPolicy Custom Resource Definition (CRD)

<!--ts-->
* [The RateLimitPolicy Custom Resource Definition (CRD)](#the-ratelimitpolicy-custom-resource-definition-crd)
   * [RateLimitPolicy](#ratelimitpolicy)
   * [RateLimitPolicySpec](#ratelimitpolicyspec)
      * [RateLimit](#ratelimit)
         * [Configuration](#configuration)
         * [ActionSpecifier](#actionspecifier)
         * [Rule](#rule)
         * [Limit](#limit)
   * [RateLimitPolicyStatus](#ratelimitpolicystatus)
      * [ConditionSpec](#conditionspec)

<!-- Created by https://github.com/ekalinin/github-markdown-toc -->
<!-- Added by: eguzki, at: jue 28 jul 2022 21:06:35 CEST -->

<!--te-->

Generated using [github-markdown-toc](https://github.com/ekalinin/github-markdown-toc)

## RateLimitPolicy

| **json/yaml field**| **Type** | **Required** | **Description** |
| --- | --- | --- | --- |
| `spec` | [RateLimitPolicySpec](#RateLimitPolicySpec) | Yes | The specfication for RateLimitPolicy custom resource |
| `status` | [RateLimitPolicyStatus](#RateLimitPolicyStatus) | No | The status for the custom resource  |

## RateLimitPolicySpec

| **json/yaml field**| **Type** | **Required** | **Default value** | **Description** |
| --- | --- | --- | --- | --- |
| `targetRef` | [gatewayapiv1alpha2.PolicyTargetReference](https://github.com/kubernetes-sigs/gateway-api/blob/main/apis/v1alpha2/policy_types.go) | Yes | N/A | identifies an API object to apply policy to |
| `rateLimits` | [][RateLimit](#RateLimit) | No | empy list | list of rate limit configurations |

### RateLimit

| **json/yaml field**| **Type** | **Required** | **Default value** | **Description** |
| --- | --- | --- | --- | --- |
| `configurations` | [][Configuration](#Configuration) | Yes | N/A | list of action configurations |
| `rules` | [][Rule](#Rule) | No | Empty. All configurations apply | list of action configurations rules. Rate limit configuration will apply when at least one rule matches the request |
| `limits` | [][Limit](#Limit) | No | Empty | list of Limitador limit objects |

#### Configuration

| **json/yaml field**| **Type** | **Required** | **Default value** | **Description** |
| --- | --- | --- | --- | --- |
| `actions` | [][ActionSpecifier](#ActionSpecifier) | No | empty | list of action specifiers. Each action specifier can only define one action type. |

#### ActionSpecifier

| **json/yaml field**| **Type** | **Required** | **Default value** | **Description** |
| --- | --- | --- | --- | --- |
| `generic_key` | [config.route.v3.RateLimit.Action.GenericKey](https://www.envoyproxy.io/docs/envoy/latest/api-v3/config/route/v3/route_components.proto#config-route-v3-ratelimit-action-generickey) | No | null | generic key action |
| `metadata` | [config.route.v3.RateLimit.Action.MetaData](https://www.envoyproxy.io/docs/envoy/latest/api-v3/config/route/v3/route_components.proto#config-route-v3-ratelimit-action-metadata) | No | null | descriptor entry is appended when the metadata contains a key value |
| `remote_address` | [config.route.v3.RateLimit.Action.RemoteAddress](https://www.envoyproxy.io/docs/envoy/latest/api-v3/config/route/v3/route_components.proto#config-route-v3-ratelimit-action-remoteaddress) | No | null | descriptor entry is appended to the descriptor and is populated using the trusted address from x-forwarded-for |
| `request_headers` | [config.route.v3.RateLimit.Action.RequestHeaders](https://www.envoyproxy.io/docs/envoy/latest/api-v3/config/route/v3/route_components.proto#config-route-v3-ratelimit-action-requestheaders) | No | null | descriptor entry is appended when a header contains a key that matches the header_name |

#### Rule

| **json/yaml field**| **Type** | **Required** | **Default value** | **Description** |
| --- | --- | --- | --- | --- |
| `paths` | []string | No | null | list of paths. Request matches when one from the list matches |
| `methods` | []string | No | null | list of methods to match. Request matches when one from the list matches |
| `hosts` | []string | No | null | list of hostnames to match. Wildcard hostnames are valid. Request matches when one from the list matches. Each defined hostname must be subset of one of the hostnames defined by the targeted network resource |

#### Limit

| **json/yaml field**| **Type** | **Required** | **Default value** | **Description** |
| --- | --- | --- | --- | --- |
| `maxValue` | int | Yes | N/A | max number of request for the specified time period |
| `seconds` | int | Yes | N/A | time period in seconds |
| `conditions` | []string | Yes | N/A | Limit conditions. Check [Limitador](https://github.com/Kuadrant/limitador) for more information |
| `variables` | []string | Yes | N/A | Limit variables. Check [Limitador](https://github.com/Kuadrant/limitador) for more information |

## RateLimitPolicyStatus

| **json field **| **Type** | **Info** |
| --- | --- | --- |
| `observedGeneration` | string | helper field to see if status info is up to date with latest resource spec |
| `conditions` | array of [condition](#ConditionSpec)s | resource conditions |

### ConditionSpec

The status object has an array of Conditions through which the resource has or has not passed.
Each element of the Condition array has the following fields:

* The *lastTransitionTime* field provides a timestamp for when the entity last transitioned from one status to another.
* The *message* field is a human-readable message indicating details about the transition.
* The *reason* field is a unique, one-word, CamelCase reason for the condition’s last transition.
* The *status* field is a string, with possible values **True**, **False**, and **Unknown**.
* The *type* field is a string with the following possible values:
  * Available: the resource has successfully configured;

| **Field** | **json field**| **Type** | **Info** |
| --- | --- | --- | --- |
| Type | `type` | string | Condition Type |
| Status | `status` | string | Status: True, False, Unknown |
| Reason | `reason` | string | Condition state reason |
| Message | `message` | string | Condition state description |
| LastTransitionTime | `lastTransitionTime` | timestamp | Last transition timestamp |
