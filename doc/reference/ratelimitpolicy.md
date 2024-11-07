# The RateLimitPolicy Custom Resource Definition (CRD)

## RateLimitPolicy

| **Field** | **Type**                                        | **Required** | **Description**                                       |
|-----------|-------------------------------------------------|:------------:|-------------------------------------------------------|
| `spec`    | [RateLimitPolicySpec](#ratelimitpolicyspec)     |     Yes      | The specification for RateLimitPolicy custom resource |
| `status`  | [RateLimitPolicyStatus](#ratelimitpolicystatus) |      No      | The status for the custom resource                    |

## RateLimitPolicySpec

| **Field**   | **Type**                                                                                                                                    | **Required** | **Description**                                                                                                                                                                             |
|-------------|---------------------------------------------------------------------------------------------------------------------------------------------|--------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `targetRef` | [LocalPolicyTargetReference](https://gateway-api.sigs.k8s.io/reference/spec/#gateway.networking.k8s.io/v1alpha2.LocalPolicyTargetReference) | Yes          | Reference to a Kubernetes resource that the policy attaches to                                                                                                                              |
| `defaults`  | [RateLimitPolicyCommonSpec](#rateLimitPolicyCommonSpec)                                                                                     | No           | Default limit definitions. This field is mutually exclusive with the `limits` field                                                                                                         |
| `overrides` | [RateLimitPolicyCommonSpec](#rateLimitPolicyCommonSpec)                                                                                     | No           | Overrides limit definitions. This field is mutually exclusive with the `limits` field and `defaults` field. This field is only allowed for policies targeting `Gateway` in `targetRef.kind` |
| `limits`    | Map<String: [Limit](#limit)>                                                                                                                | No           | Limit definitions. This field is mutually exclusive with the [`defaults`](#rateLimitPolicyCommonSpec) field                                                                                 |

### RateLimitPolicyCommonSpec

| **Field** | **Type**                     | **Required** | **Description**                                                                                                              |
|-----------|------------------------------|--------------|------------------------------------------------------------------------------------------------------------------------------|
| `when`    | [][Predicate](#predicate)    | No           | List of dynamic predicates to activate the policy. All expression must evaluate to true for the policy to be applied         |
| `limits`  | Map<String: [Limit](#limit)> | No           | Explicit Limit definitions. This field is mutually exclusive with [RateLimitPolicySpec](#ratelimitpolicyspec) `limits` field |

### Predicate

| **Field** | **Type**                     | **Required** | **Description**                                                                                                              |
|----------------|-------------------------|--------------|------------------------------------------------------------------------------------------------------------------------------|
| `predicate`    | String                  | Yes          | Defines one CEL expression that must be evaluated to bool                                                                    |

### Counter

| **Field** | **Type**                     | **Required** | **Description**                                                                                                               |
|-----------------|-------------------------|--------------|------------------------------------------------------------------------------------------------------------------------------|
| `expression`    | String                  | Yes          | Defines one CEL expression that will be used as rate limiting counter                                                        |

### Limit

| **Field**        | **Type**                                            | **Required** | **Description**                                                                                                                                                                                                                                                                                                  |
|------------------|-----------------------------------------------------|:------------:|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `rates`          | [][RateLimit](#ratelimit)                           |      No      | List of rate limits associated with the limit definition                                                                                                                                                                                                                                                         |
| `counters`       | [][Counter](#counter)                               |      No      | List of rate limit counter qualifiers. Items must be a valid [Well-known attribute](https://github.com/Kuadrant/architecture/blob/main/rfcs/0002-well-known-attributes.md). Each distinct value resolved in the data plane starts a separate counter for each rate limit.                                        |
| `when`           | [][Predicate](#predicate)                           |      No      | List of dynamic predicates to activate the limit. All expression must evaluate to true for the limit to be applied                                                                        |

#### RateLimit

| **Field**  | **Type** | **Required** | **Description**                                                                        |
|------------|----------|:------------:|----------------------------------------------------------------------------------------|
| `limit`    | Number   |     Yes      | Maximum value allowed within the given period of time (duration)                       |
| `window`   | String   |     Yes      | The period of time that the limit applies. Follows [Gateway API Duration format](https://gateway-api.sigs.k8s.io/geps/gep-2257/?h=duration#gateway-api-duration-format) |

## RateLimitPolicyStatus

| **Field**            | **Type**                          | **Description**                                                                                                                     |
|----------------------|-----------------------------------|-------------------------------------------------------------------------------------------------------------------------------------|
| `observedGeneration` | String                            | Number of the last observed generation of the resource. Use it to check if the status info is up to date with latest resource spec. |
| `conditions`         | [][ConditionSpec](#conditionspec) | List of conditions that define that status of the resource.                                                                         |

### ConditionSpec

* The *lastTransitionTime* field provides a timestamp for when the entity last transitioned from one status to another.
* The *message* field is a human-readable message indicating details about the transition.
* The *reason* field is a unique, one-word, CamelCase reason for the condition’s last transition.
* The *status* field is a string, with possible values **True**, **False**, and **Unknown**.
* The *type* field is a string with the following possible values:
    * Available: the resource has successfully configured;

| **Field**            | **Type**  | **Description**              |
|----------------------|-----------|------------------------------|
| `type`               | String    | Condition Type               |
| `status`             | String    | Status: True, False, Unknown |
| `reason`             | String    | Condition state reason       |
| `message`            | String    | Condition state description  |
| `lastTransitionTime` | Timestamp | Last transition timestamp    |
