# The APIKeyRequest Custom Resource Definition (CRD)

## APIKeyRequest

| **Field** | **Type**                                        | **Required** | **Description**                                    |
|-----------|-------------------------------------------------|:------------:|----------------------------------------------------|
| `spec`    | [APIKeyRequestSpec](#apikeyrequestspec)         | Yes          | The specification for APIKeyRequest custom resource |
| `status`  | [APIKeyRequestStatus](#apikeyrqueststatus)     | No           | The status for the custom resource                 |

## APIKeyRequestSpec

| **Field**      | **Type**                          | **Required** | **Description**                                                      |
|----------------|-----------------------------------|:------------:|----------------------------------------------------------------------|
| `apiName`      | String                            | Yes          | Name of the APIProduct being requested                               |
| `apiNamespace` | String                            | Yes          | Namespace where the APIProduct exists                                |
| `planTier`     | String                            | Yes          | Requested plan tier (arbitrary string defined in PlanPolicy)         |
| `useCase`      | String                            | No           | User's use case for requesting access                                |
| `requestedBy`  | [RequestedBy](#requestedby)       | Yes          | Information about the requester                                      |
| `requestedAt`  | String (date-time)                | No           | When the request was created                                         |

### RequestedBy

| **Field** | **Type** | **Required** | **Description**         |
|-----------|----------|:------------:|-------------------------|
| `userId`  | String   | Yes          | User identifier         |
| `email`   | String   | No           | User email (format: email) |

## APIKeyRequestStatus

| **Field**        | **Type**                                                                                     | **Required** | **Description**                                                         |
|------------------|----------------------------------------------------------------------------------------------|:------------:|-------------------------------------------------------------------------|
| `phase`          | String                                                                                       | No           | Current status of the request. Options: `Pending`, `Approved`, `Rejected`. Default: `Pending` |
| `reviewedBy`     | String                                                                                       | No           | User ID of the approver/rejecter                                        |
| `reviewedAt`     | String (date-time)                                                                           | No           | When the request was reviewed                                           |
| `reason`         | String                                                                                       | No           | Reason for approval or rejection                                        |
| `apiKey`         | String                                                                                       | No           | Generated API key (populated after approval)                            |
| `apiHostname`    | String                                                                                       | No           | API hostname (populated after approval)                                 |
| `apiBasePath`    | String                                                                                       | No           | API base path (populated after approval)                                |
| `apiDescription` | String                                                                                       | No           | API description (populated after approval)                              |
| `apiOasUrl`      | String                                                                                       | No           | OpenAPI specification URL (if available)                                |
| `apiOasUiUrl`    | String                                                                                       | No           | OpenAPI UI URL (if available)                                           |
| `planLimits`     | [PlanLimits](#planlimits)                                                                    | No           | Rate limit details for the approved plan                                |
| `conditions`     | [][ConditionSpec](https://pkg.go.dev/k8s.io/apimachinery@v0.28.4/pkg/apis/meta/v1#Condition) | No           | List of conditions that define the status of the resource               |

### PlanLimits

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
