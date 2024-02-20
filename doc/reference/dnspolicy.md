# The DNSPolicy Custom Resource Definition (CRD)

- [DNSPolicy](#DNSPolicy)
- [DNSPolicySpec](#dnspolicyspec)
    - [HealthCheckSpec](#healthcheckspec)
      - [AdditionalHeadersRef](#additionalheadersref)
    - [LoadBalancingSpec](#loadbalancingspec)
      - [LoadBalancingWeighted](#loadbalancingweighted)
        - [CustomWeight](#customweight)
      - [LoadBalancingGeo](#loadbalancinggeo)
- [DNSPolicyStatus](#dnspolicystatus)

## DNSPolicy

| **Field** | **Type**                                       | **Required** | **Description**                                |
|-----------|------------------------------------------------|:------------:|------------------------------------------------|
| `spec`    | [DNSPolicySpec](#dnspolicyspec)     |    Yes       | The specification for DNSPolicy custom resource |
| `status`  | [DNSPolicyStatus](#dnspolicystatus) |      No      | The status for the custom resource             | 

## DNSPolicySpec

| **Field**         | **Type**                                                                                                                                    |  **Required**  | **Description**                                                |
|-------------------|---------------------------------------------------------------------------------------------------------------------------------------------|:--------------:|----------------------------------------------------------------|
| `targetRef`       | [Gateway API PolicyTargetReference](https://gateway-api.sigs.k8s.io/geps/gep-713/?h=policytargetreference#policy-targetref-api)    |      Yes       | Reference to a Kuberentes resource that the policy attaches to |
| `healthCheck`     | [HealthCheckSpec](#healthcheckspec)                                                                                                         |       No       | HealthCheck spec                                               |
| `loadBalancing`   | [LoadBalancingSpec](#loadbalancingspec)                                                                                                     |       No       | LoadBancking Spec                                              |
| `routingStrategy` | String                                                                                                                                      |      Yes       | Routing Strategy to use, one of "simple" or "loadbalacned"     |

## HealthCheckSpec

| **Field**                   | **Type**                                      | **Description**                                                                                                        |
|-----------------------------|-----------------------------------------------|------------------------------------------------------------------------------------------------------------------------|
| `endpoint`                  | String                                        | The endpoint to connect to (e.g. IP address or hostname of a clusters loadbalancer)                                    |
| `port`                      | Number                                        | The port to use                                                                                                        |
| `protocol`                  | String                                        | The protocol to use for this request (e.g. Https;Https)                                                                |
| `failureThreshold`          | Number                                        | Failure Threshold                                                                                                      |
| `additionalHeadersRef`      | [AdditionalHeadersRef](#additionalheadersref) | Secret ref which contains k/v: headers and their values that can be specified to ensure the health check is successful |
| `expectedResponses`         | []Number                                      | HTTP response codes that should be considered healthy (defaults are 200 and 201)                                       |
| `allowInsecureCertificates` | Boolean                                       | Allow using invalid (e.g. self-signed) certificates, default is false                                                  |
| `interval`                  | [Kubernetes meta/v1.Duration](https://pkg.go.dev/k8s.io/apimachinery/pkg/apis/meta/v1#Duration)                                          | How frequently this check would ideally be executed                                                                    |

## AdditionalHeadersRef

| **Field** | **Type**   | **Description**                                             |
|-----------|------------|-------------------------------------------------------------|
| `name`    | String     | Name of the secret containing additional header information |

## LoadBalancingSpec

| **Field**  | **Type**                                        | **Description**       |
|------------|-------------------------------------------------|-----------------------|
| `weighted` | [LoadBalancingWeighted](#loadbalancingweighted) | Weighted routing spec |
| `geo`      | [LoadBalancingGeo](#loadbalancinggeo)           | Geo routing spec      |

## LoadBalancingWeighted

| **Field**       | **Type**                         | **Description**                                                       |
|-----------------|----------------------------------|-----------------------------------------------------------------------|
| `defaultWeight` | Number                           | Default weight to apply to created records                            |
| `custom`        | [][CustomWeight](#customweight)  | Custom weights to manipulate records weights based on label selectors |

## CustomWeight

| **Field**  | **Type**             | **Description**                                                          |
|------------|----------------------|--------------------------------------------------------------------------|
| `selector` | metav1.LabelSelector | Label Selector to specify resources that should have this weight applied |
| `weight`   | Number               | Weight value to apply for matching resources                             |

## LoadBalancingGeo

| **Field**    | **Type** | **Description**                 |
|--------------|----------|---------------------------------|
| `defaultGeo` | String   | Default geo to apply to records |

## DNSPolicyStatus

| **Field**            | **Type**                                                                                                  | **Description**                                                                                                                     |
|----------------------|-----------------------------------------------------------------------------------------------------------|-------------------------------------------------------------------------------------------------------------------------------------|
| `observedGeneration` | String                                                                                                    | Number of the last observed generation of the resource. Use it to check if the status info is up to date with latest resource spec. |
| `conditions`         | [][Kubernetes meta/v1.Condition](https://pkg.go.dev/k8s.io/apimachinery/pkg/apis/meta/v1#Condition)       | List of conditions that define that status of the resource.                                                                         |
| `healthCheck`        | [HealthCheckStatus](#healthcheckstatus)                                                                   | HealthCheck status.                                                                                                                 |

## HealthCheckStatus

| **Field**     | **Type**                          | **Description**                                                                                                                     |
|---------------|-----------------------------------|-------------------------------------------------------------------------------------------------------------------------------------|
| `conditions`  | [][Kubernetes meta/v1.Condition](https://pkg.go.dev/k8s.io/apimachinery/pkg/apis/meta/v1#Condition)  | List of conditions that define that status of the resource.<br/>                                                                         |
