# PlanPolicy Extension

The PlanPolicy extension provides plan-based rate limiting capabilities for Kuadrant. It enables you to define different service tiers (plans) with associated rate limits and automatically categorize authenticated users into these plans based on predicate expressions.

## Overview

PlanPolicy enhances the existing Kuadrant AuthPolicy by adding plan identification and automatic rate limiting based on authenticated user metadata. This allows you to implement tiered service offerings where different users receive different rate limits based on their subscription plan or other attributes.

## How it works

### Integration

PlanPolicy works in conjunction with AuthPolicy and RateLimitPolicy:

1. **AuthPolicy** handles authentication and stores identity metadata in secrets
2. **PlanPolicy** evaluates predicate expressions against the authenticated identity to determine the user's plan
3. The policy automatically creates rate limits for each plan tier
4. **Rate limiting** is enforced based on the identified plan

### The PlanPolicy custom resource

#### Overview

The `PlanPolicy` spec includes the following parts:

- A reference to an existing Gateway API resource (`spec.targetRef`)
- List of plans with predicates and limits (`spec.plans`)

Each plan defines:
- **Tier**: A unique identifier for the plan (e.g., "gold", "silver", "bronze")
- **Predicate**: A CEL expression that determines if a user belongs to this plan
- **Limits**: Rate limiting configuration for the plan

#### High-level example and field definition

```yaml
apiVersion: extensions.kuadrant.io/v1alpha1
kind: PlanPolicy
metadata:
  name: my-plan-policy
spec:
  # Reference to an existing networking resource to attach the policy to
  # Can target Gateway or HTTPRoute resources
  # Must be in the same namespace as the PlanPolicy
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute  # or Gateway
    name: my-route
  
  # List of plans ordered by priority (first match wins)
  plans:
    - tier: gold
      predicate: |
        has(auth.identity) && auth.identity.metadata.annotations["secret.kuadrant.io/plan-id"] == "gold"
      limits:
        daily: 1000
        weekly: 5000
    - tier: silver
      predicate: |
        has(auth.identity) && auth.identity.metadata.annotations["secret.kuadrant.io/plan-id"] == "silver"
      limits:
        daily: 500
        weekly: 2000
    - tier: bronze
      predicate: |
        has(auth.identity) && auth.identity.metadata.annotations["secret.kuadrant.io/plan-id"] == "bronze"
      limits:
        daily: 100
        weekly: 500
```

## Using the PlanPolicy

### Targeting a HTTPRoute networking resource

When a PlanPolicy targets an HTTPRoute, the policy will be enforced on all traffic flowing through that specific route.

Target an HTTPRoute by setting the `spec.targetRef` field of the PlanPolicy as follows:

```yaml
apiVersion: extensions.kuadrant.io/v1alpha1
kind: PlanPolicy
metadata:
  name: my-route-plan
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: my-route
  plans:
    # ... plan definitions
```

### Targeting a Gateway networking resource

When a PlanPolicy targets a Gateway, the policy will be enforced on all routes attached to that gateway.

Target a Gateway by setting the `spec.targetRef` field of the PlanPolicy as follows:

```yaml
apiVersion: extensions.kuadrant.io/v1alpha1
kind: PlanPolicy
metadata:
  name: my-gateway-plan
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: my-gateway
  plans:
    # ... plan definitions
```

### Plan predicates

Plan predicates are CEL expressions that determine which plan a user belongs to. The predicates are evaluated in order, and the first matching plan is selected.

Common predicate patterns:

```yaml
# Plan based on annotation in auth secret
predicate: |
  has(auth.identity) && auth.identity.metadata.annotations["secret.kuadrant.io/plan-id"] == "gold"

# Plan based on JWT claim
predicate: |
  has(auth.identity) && auth.identity.sub == "premium-user"

# Plan based on multiple conditions
predicate: |
  has(auth.identity) && 
  auth.identity.metadata.annotations["secret.kuadrant.io/plan-id"] == "gold" &&
  auth.identity.metadata.labels["app"] == "my-app"
```

### Plan limits

Plan limits define the rate limiting configuration for each tier. You can specify:

- **daily**: Daily request limit
- **weekly**: Weekly request limit  
- **monthly**: Monthly request limit
- **yearly**: Yearly request limit
- **custom**: Custom rate limits using RateLimitPolicy Rate format

```yaml
limits:
  daily: 1000
  weekly: 5000
  monthly: 20000
  yearly: 200000
  custom:
    - limit: 100
      window: "1h"
    - limit: 10
      window: "1m"
```

## Prerequisites

Before using PlanPolicy, ensure you have:

1. **Kuadrant Operator** installed and running
2. **Gateway API** resources (Gateway and HTTPRoute) configured
3. **AuthPolicy** configured for authentication

## Examples

Check out the following user guide for a complete example of using PlanPolicy:

- [Plan-based Rate Limiting Tutorial](../user-guides/planpolicy/plan-based-rate-limiting.md)

## Known limitations

- PlanPolicies can only target HTTPRoutes/Gateways defined within the same namespace as the PlanPolicy
- Plan predicates are evaluated in order - ensure more specific plans come before general ones
- Requires authentication to be configured via AuthPolicy for plan identification
