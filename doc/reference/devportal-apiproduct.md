# The APIProduct Custom Resource Definition (CRD)

## Overview

The APIProduct CRD is part of the Developer Portal extension for Kuadrant. It references an HTTPRoute and adds product catalog information, enabling APIs to be published to a developer portal (Backstage) where they can be discovered and consumed. While HTTPRoute defines the technical routing configuration, APIProduct adds the business and organizational context needed for API consumption: human-readable names, descriptions, documentation links, contact information, versioning, and approval workflows. 

## APIProduct

| **Field** | **Type**                                  | **Required** | **Description**                                   |
|-----------|-------------------------------------------|:------------:|---------------------------------------------------|
| `spec`    | [APIProductSpec](#apiproductspec)         | Yes          | The specification for APIProduct custom resource  |
| `status`  | [APIProductStatus](#apiproductstatus)     | No           | The status for the custom resource                |

## APIProductSpec

| **Field**       | **Type**                                                                                                                                                    | **Required** | **Description**                                                                                                 |
|-----------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------|--------------|-----------------------------------------------------------------------------------------------------------------|
| `targetRef`     | [Gateway API LocalPolicyTargetReference](#localpolicytargetreference)                                                                                      | Yes          | Reference to the HTTPRoute that the API product represents                                                     |
| `displayName`   | String                                                                                                                                                      | Yes          | Human-readable name for the API product shown in the developer portal                                           |
| `approvalMode`  | String                                                                                                                                                      | Yes          | Whether access requests are auto-approved or require manual review. Valid values: `automatic`, `manual`. Default: `manual` |
| `publishStatus` | String                                                                                                                                                      | Yes          | Controls whether the API product appears in the Backstage catalog. Valid values: `Draft`, `Published`. Default: `Draft` |
| `description`   | String                                                                                                                                                      | No           | Detailed description of the API product                                                                         |
| `version`       | String                                                                                                                                                      | No           | API version (e.g., v1, v2)                                                                                      |
| `tags`          | []String                                                                                                                                                    | No           | Tags for categorization and search in the developer portal                                                      |
| `contact`       | [ContactInfo](#contactinfo)                                                                                                                                 | No           | Contact information for API owners                                                                              |
| `documentation` | [Documentation](#documentation)                                                                                                                             | No           | API documentation links                                                                                         |

### LocalPolicyTargetReference

| **Field** | **Type** | **Required** | **Description**                                                        |
|-----------|----------|:------------:|------------------------------------------------------------------------|
| `group`   | String   | Yes          | Group of the target resource. Must be `gateway.networking.k8s.io`     |
| `kind`    | String   | Yes          | Kind of the target resource. Must be `HTTPRoute`                       |
| `name`    | String   | Yes          | Name of the target HTTPRoute resource                                  |

### ContactInfo

| **Field** | **Type** | **Required** | **Description**                              |
|-----------|----------|:------------:|----------------------------------------------|
| `team`    | String   | No           | Team name                                    |
| `email`   | String   | No           | Contact email                                |
| `slack`   | String   | No           | Slack channel (e.g., #api-support)           |
| `url`     | String   | No           | URL to team page or support portal           |

### Documentation

| **Field**        | **Type** | **Required** | **Description**                                                       |
|------------------|----------|:------------:|-----------------------------------------------------------------------|
| `docsURL`        | String   | No           | URL to general documentation                                          |
| `openAPISpecURL` | String   | No           | URL to OpenAPI specification (JSON/YAML)                              |
| `swaggerUI`      | String   | No           | URL to Swagger UI or similar interactive documentation                |
| `gitRepository`  | String   | No           | URL to git repository (shown as "View Source" in Backstage)           |
| `techdocsRef`    | String   | No           | Techdocs reference (e.g., `url:https://github.com/org/repo` or `dir:.` for local docs) |

## APIProductStatus

| **Field**            | **Type**                               | **Description**                                                                                   |
|----------------------|----------------------------------------|---------------------------------------------------------------------------------------------------|
| `observedGeneration` | Integer                                | ObservedGeneration reflects the generation of the most recently observed spec                     |
| `conditions`         | [][ConditionSpec](#conditionspec)      | Represents the observations of the APIProduct's current state                                     |
| `discoveredPlans`    | [][DiscoveredPlan](#discoveredplan)    | List of PlanPolicies discovered from the HTTPRoute                                                |
| `openapi`            | [OpenAPIStatus](#openapistatus)        | OpenAPI specification fetched from the API and its sync status                                    |

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

### DiscoveredPlan

| **Field**        | **Type** | **Required** | **Description**                                                    |
|------------------|----------|:------------:|--------------------------------------------------------------------|
| `tier`           | String   | Yes          | Tier this plan represents                                          |
| `limits`         | [Limits](#limits)   | No           | Rate limits that the plan enforces                                 |

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

### OpenAPIStatus

| **Field**      | **Type**  | **Required** | **Description**                                |
|----------------|-----------|:------------:|------------------------------------------------|
| `raw`          | String    | Yes          | Raw OpenAPI specification content              |
| `lastSyncTime` | Timestamp | Yes          | Last time the raw content was updated          |

## High level example

```yaml
apiVersion: devportal.kuadrant.io/v1alpha1
kind: APIProduct
metadata:
  name: payment-api
  namespace: payment-services
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: payment-route
  displayName: Payment Processing API
  description: |
    Secure API for processing payments, managing transactions,
    and handling refunds. Supports multiple payment methods
    including credit cards, digital wallets, and bank transfers.
  version: v2
  approvalMode: automatic
  publishStatus: Published
  tags:
    - payments
    - fintech
    - transactions
  contact:
    team: Payment Platform Team
    email: payment-api@example.com
    slack: "#payment-support"
    url: https://wiki.example.com/teams/payment-platform
  documentation:
    docsURL: https://docs.example.com/apis/payment
    openAPISpecURL: https://api.example.com/specs/payment-v2.yaml
    swaggerUI: https://api.example.com/docs/payment
    gitRepository: https://github.com/example/payment-api
    techdocsRef: url:https://github.com/example/payment-api/tree/main/docs
```

## Relationship to HTTPRoute, AuthPolicy, and RateLimitPolicy

### HTTPRoute

APIProduct **must** reference an existing HTTPRoute via `targetRef`. The HTTPRoute defines the actual traffic routing and path matching, while APIProduct adds product catalog metadata, documentation, and plan discovery.

### AuthPolicy

AuthPolicy is typically applied to the same HTTPRoute that the APIProduct references. This enforces authentication (such as API key validation) for requests to the API. When a developer requests access to an APIProduct, an APIKey resource is created, which generates a Kubernetes Secret. The AuthPolicy validates incoming requests against these secrets.

### PlanPolicy

PlanPolicy is an extension that can target the same HTTPRoute as the APIProduct. It defines tiered access plans with different rate limits. The APIProduct controller automatically discovers PlanPolicies attached to the same HTTPRoute and surfaces the plan information in the `status.discoveredPlans` field. This allows the developer portal to display available plans to users requesting API access.

### RateLimitPolicy

RateLimitPolicy can be applied to the same HTTPRoute for non-plan-based rate limiting. Unlike PlanPolicy, which provides tiered limits based on API key metadata, RateLimitPolicy applies uniform rate limits to all requests or uses different criteria for rate limiting.

## Complete Integration Example

```yaml
# 1. HTTPRoute - defines the API routing
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: store-api-route
  namespace: store
spec:
  parentRefs:
    - name: api-gateway
      namespace: gateway-system
  hostnames:
    - store-api.example.com
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /api/v1
      backendRefs:
        - name: store-service
          port: 8080
---
# 2. AuthPolicy - enforces API key authentication
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
# 3. PlanPolicy - defines tiered rate limits
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
    - tier: enterprise
      predicate: |
        has(auth.identity) &&
        auth.identity.metadata.annotations["secret.kuadrant.io/plan-id"] == "enterprise"
      limits:
        monthly: 1000000
        custom:
          - limit: 500
            window: 1m
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
# 4. APIProduct - provides the developer portal catalog entry
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
  description: |
    Comprehensive API for managing products, orders, and customers
    in our e-commerce platform. Supports multiple payment methods
    and real-time inventory updates.
  version: v1
  approvalMode: manual
  publishStatus: Published
  tags:
    - e-commerce
    - retail
    - payments
  contact:
    team: E-Commerce Platform Team
    email: store-api@example.com
    slack: "#store-api-support"
    url: https://wiki.example.com/teams/ecommerce
  documentation:
    docsURL: https://docs.example.com/apis/store
    openAPISpecURL: https://store-api.example.com/openapi.yaml
    swaggerUI: https://store-api.example.com/docs
    gitRepository: https://github.com/example/store-api
```
