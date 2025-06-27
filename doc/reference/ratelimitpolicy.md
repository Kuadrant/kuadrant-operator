# The RateLimitPolicy Custom Resource Definition (CRD)

## RateLimitPolicy

| **Field** | **Type**                                        | **Required** | **Description**                                       |
|-----------|-------------------------------------------------|:------------:|-------------------------------------------------------|
| `spec`    | [RateLimitPolicySpec](#ratelimitpolicyspec)     |     Yes      | The specification for RateLimitPolicy custom resource |
| `status`  | [RateLimitPolicyStatus](#ratelimitpolicystatus) |      No      | The status for the custom resource                    |

## RateLimitPolicySpec

| **Field**   | **Type**                                                                                                                                    | **Required** | **Description**                                                                                                                                                                             |
|-------------|---------------------------------------------------------------------------------------------------------------------------------------------|--------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `targetRef` | [LocalPolicyTargetReferenceWithSectionName](#localpolicytargetreferencewithsectionname) | Yes          | Reference to a Kubernetes resource that the policy attaches to. For more [info](https://gateway-api.sigs.k8s.io/reference/spec/#localpolicytargetreferencewithsectionname)                                                                                                                              |
| `defaults`  | [RateLimitPolicyCommonSpec](#rateLimitPolicyCommonSpec)                                                                                     | No           | Default limit definitions. This field is mutually exclusive with the `limits` field                                                                                                         |
| `overrides` | [RateLimitPolicyCommonSpec](#rateLimitPolicyCommonSpec)                                                                                     | No           | Overrides limit definitions. This field is mutually exclusive with the `limits` field and `defaults` field. This field is only allowed for policies targeting `Gateway` in `targetRef.kind` |
| `limits`    | Map<String: [Limit](#limit)>                                                                                                                | No           | Limit definitions. This field is mutually exclusive with the [`defaults`](#rateLimitPolicyCommonSpec) field                                                                                 |




### LocalPolicyTargetReferenceWithSectionName
| **Field**       | **Type**                                | **Required** | **Description**                                            |
|------------------|-----------------------------------------|--------------|------------------------------------------------------------|
| `LocalPolicyTargetReference`         | [LocalPolicyTargetReference](#localpolicytargetreference)          | Yes          | Reference to a local policy target.               |
| `sectionName`    | [SectionName](#sectionname)                         | No           | Section name for further specificity (if needed). |

### LocalPolicyTargetReference
| **Field** | **Type**     | **Required** | **Description**                |
|-----------|--------------|--------------|--------------------------------|
| `group`   | `Group`      | Yes          | Group of the target resource. |
| `kind`    | `Kind`       | Yes          | Kind of the target resource.  |
| `name`    | `ObjectName` | Yes          | Name of the target resource.  |

### SectionName
| Field       | Type                     | Required | Description                                                                                                                                                                                                                         |
|-------------|--------------------------|----------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| SectionName | v1.SectionName (String)  | Yes      | SectionName is the name of a section in a Kubernetes resource. <br>In the following resources, SectionName is interpreted as the following: <br>* Gateway: Listener name<br>* HTTPRoute: HTTPRouteRule name<br>* Service: Port name |
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

#### High-level example

```yaml
apiVersion: kuadrant.io/v1
kind: RateLimitPolicy
metadata:
  name: my-rate-limit-policy
spec:
  # Reference to an existing networking resource to attach the policy to. REQUIRED.
  # It can be a Gateway API HTTPRoute or Gateway resource.
  # It can only refer to objects in the same namespace as the RateLimitPolicy.
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute / Gateway
    name: myroute / mygateway

  # The limits definitions to apply to the network traffic routed through the targeted resource.
  # Equivalent to if otherwise declared within `defaults`.
  limits:
    "my_limit":
      # The rate limits associated with this limit definition. REQUIRED.
      # E.g., to specify a 50rps rate limit, add `{ limit: 50, duration: 1, unit: secod }`
      rates: […]

      # Counter qualifiers.
      # Each dynamic value in the data plane starts a separate counter, combined with each rate limit.
      # E.g., to define a separate rate limit for each user name detected by the auth layer, add `metadata.filter_metadata.envoy\.filters\.http\.ext_authz.username`.
      # Check out Kuadrant RFC 0002 (https://github.com/Kuadrant/architecture/blob/main/rfcs/0002-well-known-attributes.md) to learn more about the Well-known Attributes that can be used in this field.
      counters: […]

      # Additional dynamic conditions to trigger the limit.
      # Use it for filtering attributes not supported by HTTPRouteRule or with RateLimitPolicies that target a Gateway.
      # Check out Kuadrant RFC 0002 (https://github.com/Kuadrant/architecture/blob/main/rfcs/0002-well-known-attributes.md) to learn more about the Well-known Attributes that can be used in this field.
      when: […]

    # Explicit defaults. Used in policies that target a Gateway object to express default rules to be enforced on
    # routes that lack a more specific policy attached to.
    # Mutually exclusive with `overrides` and with declaring `limits` at the top-level of the spec.
    defaults:
      limits: { … }

    # Overrides. Used in policies that target a Gateway object to be enforced on all routes linked to the gateway,
    # thus also overriding any more specific policy occasionally attached to any of those routes.
    # Mutually exclusive with `defaults` and with declaring `limits` at the top-level of the spec.
    overrides:
      limits: { … }
```
