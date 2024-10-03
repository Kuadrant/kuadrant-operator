# The DNSPolicy Custom Resource Definition (CRD)

- [DNSPolicy](#DNSPolicy)
- [DNSPolicySpec](#dnspolicyspec)
    - [ProviderRefs](#providerRefs)
    - [HealthCheckSpec](#healthcheckspec)
    - [LoadBalancingSpec](#loadbalancingspec)
      - [LoadBalancingWeighted](#loadbalancingweighted)
        - [CustomWeight](#customweight)
      - [LoadBalancingGeo](#loadbalancinggeo)
- [DNSPolicyStatus](#dnspolicystatus)
    - [HealthCheckStatus](#healthcheckstatus)

## DNSPolicy

| **Field** | **Type**                                       | **Required** | **Description**                                |
|-----------|------------------------------------------------|:------------:|------------------------------------------------|
| `spec`    | [DNSPolicySpec](#dnspolicyspec)     |    Yes       | The specification for DNSPolicy custom resource |
| `status`  | [DNSPolicyStatus](#dnspolicystatus) |      No      | The status for the custom resource             | 

## DNSPolicySpec

| **Field**        | **Type**                                                                                                                          |     **Required**      | **Description**                                            |
|------------------|-----------------------------------------------------------------------------------------------------------------------------------|:---------------------:|------------------------------------------------------------|
| `targetRef`      | [Gateway API LocalPolicyTargetReference](https://gateway-api.sigs.k8s.io/reference/spec/#gateway.networking.k8s.io/v1alpha2.LocalPolicyTargetReference)   |          Yes          | Reference to a Kubernetes resource that the policy attaches to |
| `healthCheck`    | [HealthCheckSpec](#healthcheckspec)                                                                                               |          No           | HealthCheck spec                                           |
| `loadBalancing`  | [LoadBalancingSpec](#loadbalancingspec)                                                                                           | No | LoadBalancing Spec       |
| `providerRefs`   | [ProviderRefs](#providerrefs)                                                                                                         |          Yes          | array of references to providers. (currently limited to max 1) |

## ProviderRefs

| **Field**          | **Type**                          | **Required** | **Description**                                                                                           |
|--------------------|-----------------------------------|:------------:|-----------------------------------------------------------------------------------------------------------|
| `providerRefs`     | [][ProviderRef](#providerref)     |     Yes      | max 1 reference. This is an array of providerRef that points to a local secret(s) that contains the required provider auth values

## ProviderRef

| **Field**  | **Type** | **Required** | **Description**                                                                        |
|------------|----------|:------------:|----------------------------------------------------------------------------------------|
| `name`     | String   |     Yes      | Name of the secret in the same namespace that contains the provider credentials


## HealthCheckSpec

| **Field**          | **Type**   | **Required** | **Description**                                                                                           |
|--------------------|------------|:------------:|-----------------------------------------------------------------------------------------------------------|
| `path`         | String     |     Yes      | Path is the path to append to the host to reach the expected health check. Must start with "?" or "/", contain only valid URL characters and end with alphanumeric char or "/". For example "/" or "/healthz" are common              | 
| `port`             | Number     |     Yes      | Port to connect to the host on. Must be either 80, 443 or 1024-49151                          | 
| `protocol`         | String     |     Yes      | Protocol to use when connecting to the host, valid values are "HTTP" or "HTTPS"                           | 
| `failureThreshold` | Number     |     Yes      | FailureThreshold is a limit of consecutive failures that must occur for a host to be considered unhealthy | 
| `interval`         | Duration     |     Yes      | Interval defines how frequently this probe should execute     
| `additionalHeadersRef`         | String     |     No      | AdditionalHeadersRef refers to a secret that contains extra headers to send in the probe request, this is primarily useful if an authentication token is required by the endpoint.
| `allowInsecureCertificate`         | Boolean     |     No      | AllowInsecureCertificate will instruct the health check probe to not fail on a self-signed or otherwise invalid SSL certificate this is primarily used in development or testing environments

## LoadBalancingSpec

| **Field**    | **Type** | **Required** | **Description**                                          |
|--------------|----------|:------------:|----------------------------------------------------------|
| `defaultGeo` | Boolean  |     Yes      | Specifies if this is the default geo                     |
| `geo`        | String   |     Yes      | Geo value to apply to geo endpoints                      |
| `weight`     | Number   |      No      | Weight value to apply to weighted endpoints default: 120 |

## DNSPolicyStatus

| **Field**            | **Type**                                                                                                    | **Description**                                                                                                                     |
|----------------------|-------------------------------------------------------------------------------------------------------------|-------------------------------------------------------------------------------------------------------------------------------------|
| `observedGeneration` | String                                                                                                      | Number of the last observed generation of the resource. Use it to check if the status info is up to date with latest resource spec. |
| `conditions`         | [][Kubernetes meta/v1.Condition](https://pkg.go.dev/k8s.io/apimachinery/pkg/apis/meta/v1#Condition)         | List of conditions that define that status of the resource.                                                                         |
| `healthCheck`        | [HealthCheckStatus](#healthcheckstatus)                                                                     | HealthCheck status.                                                                                                                 |
| `recordConditions`   | [String][][Kubernetes meta/v1.Condition](https://pkg.go.dev/k8s.io/apimachinery/pkg/apis/meta/v1#Condition) | Status of individual DNSRecords owned by this policy.                                                                               |

## HealthCheckStatus

| **Field**     | **Type**                          | **Description**                                                                                                                     |
|---------------|-----------------------------------|-------------------------------------------------------------------------------------------------------------------------------------|
| `conditions`  | [][Kubernetes meta/v1.Condition](https://pkg.go.dev/k8s.io/apimachinery/pkg/apis/meta/v1#Condition)  | List of conditions that define that status of the resource.                                                                         |
