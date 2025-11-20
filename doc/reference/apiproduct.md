# The APIProduct Custom Resource Definition (CRD)

## APIProduct

| **Field** | **Type**                                    | **Required** | **Description**                                  |
|-----------|---------------------------------------------|:------------:|--------------------------------------------------|
| `spec`    | [APIProductSpec](#apiproductspec)           | Yes          | The specification for APIProduct custom resource |
| `status`  | [APIProductStatus](#apiproductstatus)       | No           | The status for the custom resource               |

## APIProductSpec

| **Field**      | **Type**                                  | **Required** | **Description**                                                                                                                           |
|----------------|-------------------------------------------|:------------:|-------------------------------------------------------------------------------------------------------------------------------------------|
| `displayName`  | String                                    | Yes          | Human-readable name for the API product                                                                                                   |
| `description`  | String                                    | No           | Detailed description of the API product                                                                                                   |
| `version`      | String                                    | No           | API version (e.g., v1, v2)                                                                                                                |
| `approvalMode` | String                                    | No           | Whether access requests are auto-approved (`automatic`) or require manual review (`manual`). Options: `automatic`, `manual`. Default: `manual` |
| `tags`         | []String                                  | No           | Tags for categorization and search                                                                                                        |
| `targetRef`    | [PolicyTargetReference](#policytargetreference) | Yes          | Reference to the HTTPRoute that this API product represents                                                                               |
| `plans`        | [][PlanInfo](#planinfo)                   | No           | Discovered plan information from PlanPolicies attached to HTTPRoute (populated by controller)                                             |
| `documentation`| [Documentation](#documentation)           | No           | API documentation links                                                                                                                   |
| `contact`      | [Contact](#contact)                       | No           | Contact information for API owners                                                                                                        |
| `publishStatus`| String                                    | No           | Controls whether the API product appears in the Backstage catalog. Options: `Draft` (hidden), `Published` (visible). Default: `Draft`    |

### PolicyTargetReference

| **Field**   | **Type** | **Required** | **Description**                                                                        |
|-------------|----------|:------------:|----------------------------------------------------------------------------------------|
| `group`     | String   | Yes          | Group of the target resource. Default: `gateway.networking.k8s.io`                    |
| `kind`      | String   | Yes          | Kind of the target resource. Default: `HTTPRoute`                                     |
| `name`      | String   | Yes          | Name of the target resource                                                           |
| `namespace` | String   | No           | Namespace of the target resource (defaults to APIProduct namespace if not specified)  |

### PlanInfo

| **Field**     | **Type**                  | **Required** | **Description**                           |
|---------------|---------------------------|:------------:|-------------------------------------------|
| `tier`        | String                    | Yes          | Plan tier name (can be any custom name)   |
| `description` | String                    | No           | Human-readable description of this plan   |
| `limits`      | [RateLimits](#ratelimits) | No           | Rate limit summary for this plan          |

### RateLimits

| **Field**  | **Type**                        | **Required** | **Description**        |
|------------|---------------------------------|:------------:|------------------------|
| `daily`    | Integer                         | No           | Daily request limit    |
| `weekly`   | Integer                         | No           | Weekly request limit   |
| `monthly`  | Integer                         | No           | Monthly request limit  |
| `custom`   | [][CustomLimit](#customlimit)   | No           | Custom rate limits     |

### CustomLimit

| **Field** | **Type** | **Required** | **Description**                                               |
|-----------|----------|:------------:|---------------------------------------------------------------|
| `limit`   | Integer  | Yes          | Request limit value                                           |
| `window`  | String   | Yes          | Time window (pattern: `^([0-9]{1,5}(h\|m\|s\|ms)){1,4}$`)    |

### Documentation

| **Field**      | **Type** | **Required** | **Description**                                                                          |
|----------------|----------|:------------:|------------------------------------------------------------------------------------------|
| `openAPISpec`  | String   | No           | URL to OpenAPI specification (JSON/YAML)                                                 |
| `swaggerUI`    | String   | No           | URL to Swagger UI or similar interactive documentation                                  |
| `docsURL`      | String   | No           | URL to general documentation                                                             |
| `gitRepository`| String   | No           | URL to Git repository (shown as "View Source" in Backstage)                              |
| `techdocsRef`  | String   | No           | Techdocs reference (e.g., `url:https://github.com/org/repo` or `dir:.` for local docs)  |

### Contact

| **Field** | **Type** | **Required** | **Description**                             |
|-----------|----------|:------------:|---------------------------------------------|
| `team`    | String   | No           | Team name                                   |
| `email`   | String   | No           | Contact email (format: email)               |
| `slack`   | String   | No           | Slack channel (e.g., #api-support)          |
| `url`     | String   | No           | URL to team page or support portal          |

## APIProductStatus

| **Field**          | **Type**                                                                                     | **Required** | **Description**                                                                                                                     |
|--------------------|----------------------------------------------------------------------------------------------|:------------:|-------------------------------------------------------------------------------------------------------------------------------------|
| `conditions`       | [][ConditionSpec](https://pkg.go.dev/k8s.io/apimachinery@v0.28.4/pkg/apis/meta/v1#Condition) | No           | List of conditions that define the status of the resource                                                                           |
| `httpRouteStatus`  | String                                                                                       | No           | Status of referenced HTTPRoute                                                                                                      |
| `discoveredPlans`  | [][DiscoveredPlan](#discoveredplan)                                                          | No           | List of PlanPolicies discovered from HTTPRoute                                                                                      |
| `lastSyncTime`     | String (date-time)                                                                           | No           | Last time plan data was synced from HTTPRoute                                                                                       |

### DiscoveredPlan

| **Field**   | **Type** | **Required** | **Description**               |
|-------------|----------|:------------:|-------------------------------|
| `name`      | String   | No           | Name of the discovered plan   |
| `namespace` | String   | No           | Namespace of the discovered plan |
