# The DNSPolicy Custom Resource Definition (CRD)

- [DNSPolicy](#DNSPolicy)
- [DNSPolicySpec](#dnspolicyspec)
    - [excludeAddresses]()
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


## ExcludeAddresses
| **Field**          | **Type**   | **Required** | **Description**                                                                                                                 |
|------------|----------|:------------:|----------------------------------------------------------------------------------------|
| `excludeAddresses` | []String   |      No      | set of hostname, CIDR or IP Addresses to exclude from the DNS Provider

## HealthCheckSpec

| **Field**  | **Type** | **Required** | **Description**                                                                        |
|------------|----------|:------------:|----------------------------------------------------------------------------------------|
| `name`     | String   |     Yes      | Name of the secret in the same namespace that contains the provider credentials
|--------------------|------------|:------------:|-----------------------------------------------------------------------------------------------------------|
| `endpoint`         | String     |     Yes      | Endpoint is the path to append to the host to reach the expected health check                             | 
| `port`             | Number     |     Yes      | Port to connect to the host on                                                                            | 
| `protocol`         | String     |     Yes      | Protocol to use when connecting to the host, valid values are "HTTP" or "HTTPS"                           | 
| `failureThreshold` | Number     |     Yes      | FailureThreshold is a limit of consecutive failures that must occur for a host to be considered unhealthy | 

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
