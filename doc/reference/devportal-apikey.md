# The APIKey Custom Resource Definition (CRD)

## Overview

The APIKey CRD is part of the Developer Portal extension for Kuadrant. It represents a request for API access credentials by a developer for a specific APIProduct and plan tier. When approved, the APIKey creates a Kubernetes Secret containing the actual API key that can be used to authenticate requests. The APIKey resource manages the entire lifecycle of API access requests, from initial submission through approval/rejection to credential generation.

## APIKey

| **Field** | **Type**                          | **Required** | **Description**                           |
|-----------|-----------------------------------|:------------:|-------------------------------------------|
| `spec`    | [APIKeySpec](#apikeyspec)         | Yes          | The specification for APIKey custom resource |
| `status`  | [APIKeyStatus](#apikeystatus)     | No           | The status for the custom resource        |

## APIKeySpec

| **Field**       | **Type**                                  | **Required** | **Description**                                                          |
|-----------------|-------------------------------------------|:------------:|--------------------------------------------------------------------------|
| `apiProductRef` | [APIProductReference](#apiproductreference) | Yes       | Reference to the APIProduct this API key provides access to             |
| `planTier`      | String                                    | Yes          | Tier of the plan (e.g., "premium", "basic", "enterprise")                |
| `requestedBy`   | [RequestedBy](#requestedby)               | Yes          | Information about who requested the API key                              |
| `useCase`       | String                                    | Yes          | Description of how the API key will be used                              |

### APIProductReference

| **Field** | **Type** | **Required** | **Description**                              |
|-----------|----------|:------------:|----------------------------------------------|
| `name`    | String   | Yes          | Name of the APIProduct in the same namespace |

### RequestedBy

| **Field** | **Type** | **Required** | **Description**                                                |
|-----------|----------|:------------:|----------------------------------------------------------------|
| `userId`  | String   | Yes          | Identifier of the user requesting the API key                  |
| `email`   | String   | Yes          | Email address of the user (must be valid email format)         |

## APIKeyStatus

| **Field**       | **Type**                          | **Description**                                                                   |
|-----------------|-----------------------------------|-----------------------------------------------------------------------------------|
| `phase`         | String                            | Current phase of the APIKey. Valid values: `Pending`, `Approved`, `Rejected`     |
| `conditions`    | [][ConditionSpec](#conditionspec) | Represents the observations of the APIKey's current state                         |
| `secretRef`     | [SecretReference](#secretreference) | Reference to the created Secret containing the API key (only when Approved)     |
| `limits`        | [Limits](#limits)                 | Rate limits for the plan                                                          |
| `apiHostname`   | String                            | Hostname from the HTTPRoute that the APIProduct references                        |
| `reviewedBy`    | String                            | Who approved or rejected the request                                              |
| `reviewedAt`    | Timestamp                         | When the request was reviewed                                                     |
| `canReadSecret` | Boolean                           | Permission to read the APIKey's secret. Default: `true`                           |

### ConditionSpec

Standard Kubernetes condition type with the following fields:

| **Field**            | **Type**  | **Description**                                                                   |
|----------------------|-----------|-----------------------------------------------------------------------------------|
| `type`               | String    | Condition type (e.g., `Ready`)                                                    |
| `status`             | String    | Status of the condition: `True`, `False`, or `Unknown`                            |
| `reason`             | String    | Unique, one-word, CamelCase reason for the condition's last transition            |
| `message`            | String    | Human-readable message indicating details about the transition                    |
| `lastTransitionTime` | Timestamp | Last time the condition transitioned from one status to another                   |
| `observedGeneration` | Integer   | The .metadata.generation that the condition was set based upon                    |

### SecretReference

| **Field** | **Type** | **Required** | **Description**                              |
|-----------|----------|:------------:|----------------------------------------------|
| `name`    | String   | Yes          | Name of the secret in the Authorino's namespace |
| `key`     | String   | Yes          | The key of the secret to select from         |

### Limits

| **Field**  | **Type**      | **Required** | **Description**                                                    |
|------------|---------------|:------------:|--------------------------------------------------------------------|
| `daily`    | Integer       | No           | Daily limit of requests for this plan                              |
| `weekly`   | Integer       | No           | Weekly limit of requests for this plan                             |
| `monthly`  | Integer       | No           | Monthly limit of requests for this plan                            |
| `yearly`   | Integer       | No           | Yearly limit of requests for this plan                             |
| `custom`   | [][Rate](#rate) | No         | Additional limits defined in terms of a RateLimitPolicy Rate       |

### Rate

| **Field**  | **Type** | **Required** | **Description**                                                    |
|------------|----------|:------------:|--------------------------------------------------------------------|
| `limit`    | Integer  | Yes          | Maximum value allowed for a given period of time                   |
| `window`   | String   | Yes          | Time period for which the limit applies (pattern: `^([0-9]{1,5}(h\|m\|s\|ms)){1,4}$`) |

## High level example

```yaml
apiVersion: devportal.kuadrant.io/v1alpha1
kind: APIKey
metadata:
  name: developer-john-premium
  namespace: payment-services
spec:
  apiProductRef:
    name: payment-api
  planTier: premium
  requestedBy:
    userId: john-doe-123
    email: john.doe@example.com
  useCase: Building a mobile payment application for retail customers
```

## Relationship to APIProduct and AuthPolicy

### APIProduct

APIKey **must** reference an existing APIProduct via `apiProductRef`. The APIProduct defines the API being accessed and provides metadata about available plans. When an APIKey is created, the controller looks up the corresponding APIProduct and its associated PlanPolicy to determine the rate limits for the specified `planTier`. These limits are then populated in the APIKey's `status.limits` field.

### AuthPolicy

AuthPolicy is typically applied to the HTTPRoute that the APIProduct references. When an APIKey is approved, a Kubernetes Secret is created with an annotation `secret.kuadrant.io/plan-id` set to the `planTier` value. The AuthPolicy validates incoming API requests by checking the API key against secrets that match specific label selectors. The PlanPolicy uses the `plan-id` annotation in its CEL predicates to determine which rate limits to apply for each authenticated request.

### PlanPolicy

PlanPolicy defines the available tiers and their corresponding rate limits. When an APIKey specifies a `planTier`, the controller validates that this tier exists in the PlanPolicy attached to the HTTPRoute. If the tier is valid and the APIKey is approved, the Secret is annotated with `secret.kuadrant.io/plan-id: <planTier>`, allowing PlanPolicy's CEL predicates to match the request to the appropriate rate limits.

## Complete Integration Example

```yaml
# 1. APIProduct - defines the API offering
apiVersion: devportal.kuadrant.io/v1alpha1
kind: APIProduct
metadata:
  name: store-api
  namespace: store
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: store-api-route
  displayName: E-Commerce Store API
  approvalMode: manual
  publishStatus: Published
---
# 2. PlanPolicy - defines available tiers and limits
apiVersion: extensions.kuadrant.io/v1alpha1
kind: PlanPolicy
metadata:
  name: store-api-plans
  namespace: store
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: store-api-route
  plans:
    - tier: professional
      predicate: |
        has(auth.identity) &&
        auth.identity.metadata.annotations["secret.kuadrant.io/plan-id"] == "professional"
      limits:
        monthly: 100000
        custom:
          - limit: 100
            window: 1m
    - tier: free
      predicate: |
        has(auth.identity) &&
        auth.identity.metadata.annotations["secret.kuadrant.io/plan-id"] == "free"
      limits:
        daily: 100
        custom:
          - limit: 10
            window: 1m
---
# 3. AuthPolicy - validates API keys
apiVersion: kuadrant.io/v1
kind: AuthPolicy
metadata:
  name: store-api-auth
  namespace: store
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: store-api-route
  rules:
    authentication:
      "api-key":
        apiKey:
          selector:
            matchLabels:
              devportal.kuadrant.io/api: store-api
        credentials:
          authorizationHeader:
            prefix: Bearer
---
# 4. APIKey - developer requests access to "professional" tier
apiVersion: devportal.kuadrant.io/v1alpha1
kind: APIKey
metadata:
  name: alice-professional
  namespace: store
spec:
  apiProductRef:
    name: store-api
  planTier: professional
  requestedBy:
    userId: alice-123
    email: alice@example.com
  useCase: Building inventory management integration for enterprise retail
```

After approval, the APIKey status will be updated:

```yaml
status:
  phase: Approved
  reviewedBy: admin@example.com
  reviewedAt: "2024-01-15T10:30:00Z"
  apiHostname: store-api.example.com
  secretRef:
    name: alice-professional-api-key
    key: api_key
  limits:
    monthly: 100000
    custom:
      - limit: 100
        window: 1m
  conditions:
    - type: Ready
      status: "True"
      reason: Approved
      message: API key approved and secret created
      lastTransitionTime: "2024-01-15T10:30:00Z"
```

The generated Secret will contain:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: alice-professional-api-key
  namespace: store
  annotations:
    secret.kuadrant.io/plan-id: professional
  labels:
    devportal.kuadrant.io/api: store-api
type: Opaque
data:
  api_key: <base64-encoded-api-key>
```

## APIKey Lifecycle

### 1. Pending Phase

When an APIKey is created, it enters the `Pending` phase. The controller validates that:
- The referenced APIProduct exists
- The specified `planTier` matches a tier defined in the associated PlanPolicy
- The user information is valid

### 2. Approval/Rejection

Based on the APIProduct's `approvalMode`:
- **Automatic**: The APIKey is automatically approved, and the phase transitions to `Approved`
- **Manual**: An administrator must manually update the APIKey status to approve or reject it

### 3. Approved Phase

When approved:
- A Kubernetes Secret is created containing the API key
- The Secret is annotated with `secret.kuadrant.io/plan-id: <planTier>`
- The Secret is labeled with `devportal.kuadrant.io/api: <apiproduct-name>`
- The `status.secretRef` field is populated with the secret reference
- The `status.limits` field is populated with the plan's rate limits
- The `status.apiHostname` is set to the hostname from the HTTPRoute

### 4. Rejected Phase

When rejected:
- No Secret is created
- The `status.reviewedBy` and `status.reviewedAt` fields record who rejected it and when
- Developers can see the rejection in the developer portal

### 5. Secret Management

The Secret created by an approved APIKey:
- Contains a randomly generated API key value
- Is used by AuthPolicy for authentication
- Is annotated to enable PlanPolicy to apply the correct rate limits
- Can be revoked by deleting the APIKey resource, which deletes the associated Secret
