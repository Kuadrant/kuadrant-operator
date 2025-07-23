# The DNSPolicy Custom Resource Definition (CRD)

- [DNSPolicy](#DNSPolicy)
- [DNSPolicySpec](#dnspolicyspec)
  - [ExcludeAddresses](#excludeaddresses)
  - [ProviderRefs](#providerrefs)
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
| `targetRef`      | [Gateway API LocalPolicyTargetReferenceWithSectionName](https://gateway-api.sigs.k8s.io/reference/spec/#localpolicytargetreferencewithsectionname)   |          Yes          | Reference to a Kubernetes resource that the policy attaches to |
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

#### High-level example

```yaml
apiVersion: kuadrant.io/v1
kind: DNSPolicy
metadata:
  name: my-dns-policy
spec:
  # reference to an existing networking resource to attach the policy to
  # it can only be a Gateway API Gateway resource
  # it can only refer to objects in the same namespace as the DNSPolicy
  # it can target a specific listener using sectionName
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: mygateway
    sectionName: api # (optional) if not set policy applies to all listeners that do not have a policy attached directly

  # reference to an existing secret resource containing provider credentials and configuration
  # it can only refer to Secrets in the same namespace as the DNSPolicy that have the type kuadrant.io/(provider) e.g kuadrant.io/aws
  providerRefs:
    - name: my-aws-credentials

  # (optional) loadbalancing specification
  # use it for providing the specification of how dns will be configured in order to provide balancing of requests across multiple clusters. If not configured, a simple A or CNAME record will be created. If you have a policy with no loadbalancing defined and want to move to a loadbalanced configuration, you will need to delete and re-create the policy.
  loadBalancing:
    # is this the default geo to be applied to records. It is important that you set the default geo flag to true **Only** for the GEO value you wish to act as the catchall GEO, you should not set multiple GEO values as default for a given targeted listener. Example: policy 1 targets listener 1 with a geo of US and sets default to true. Policy 2 targets a listener on another cluster and set the geo to EU and default to false. It is fine for policies in the same default GEO to set the value to true. The main thing is to have only one unique GEO set as the default for any shared listener hostname.
    defaultGeo: true
    # weighted specification. This will apply the given weight to the records created based on the targeted gateway listeners. If you have multiple gateways that share a listener host, you can set different weight values to influence how much traffic will be brought to a given gateway.
    weight: 100
    # This is the actual GEO location to set for records created by this policy. This can and should be different if you have multiple gateways across multiple geographic areas.

    # AWS: To see all regions supported by AWS Route 53, please see the official (documentation)[https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/resource-record-sets-values-geo.html]. With Route 53 when setting a continent code use a "GEO-" prefix otherwise it will be considered a country code.

    # GCP: To see all regions supported by GCP Cloud DNS, please see the official (documentation)[https://cloud.google.com/compute/docs/regions-zones]

    #To see the different values you can use for the geo based DNS with Azure take a look at the following (documentation)[https://learn.microsoft.com/en-us/azure/traffic-manager/traffic-manager-geographic-regions]
    geo: IE

  # (optional) health check specification
  # health check probes with the following specification will be created for each DNS target, these probes constantly check that the endpoint can be reached. They will flag an unhealthy endpoint in the status. If no DNSRecord has yet been published and the endpoint is unhealthy, the record will not be published until the health check passes.
  healthCheck:
    # the path on the listener host(s) that you want to check.
    path: /health
    # how many times does the health check need to fail before unhealthy.
    failureThreshold: 3
    # how often should it be checked.
    interval: 5min
    # additionalHeadersRef is reference to a local secret with a set of key value pairs to be used as headers when sending the health check request.
    additionalHeadersRef:
      name: headers
```
