# The Kuadrant Custom Resource Definition (CRD)

## kuadrant

| **Field** | **Type**                          | **Required** | **Description**                      |
|-----------|-----------------------------------|:------------:|--------------------------------------|
| `spec`    | [KuadrantSpec](#kuadrantspec)     |      No      | Blank specification                  |
| `status`  | [KuadrantStatus](#kuadrantstatus) |      No      | The status for the custom resources. |

### KuadrantSpec

| **Field** | **Type**                          | **Required** | **Description**                      |
|-----------|-----------------------------------|:------------:|--------------------------------------|
| `observability`    | [Observability](#observability)     | No | Kuadrant observability configuration. |
| `mtls`  | [mTLS](#mtls) |      No      | Two way authentication between kuadrant components. |

#### mTLS

| **Field** | **Type**                          | **Required** | **Description**                      |
|-----------|-----------------------------------|:------------:|--------------------------------------|
| `enable`    | Boolean     |  No | Enable mutual authentication communication between the gateway and the kuadrant data plane components. Default: `false`|
| `limitador` | Boolean     |  No | Enable mutual authentication communication between the gateway and Limitador. Default: `not set`|
| `authorino` | Boolean     |  No | Enable mutual authentication communication between the gateway and Authorino. Default: `not set`|

The truth table for limitador component is as follows:

| Spec |  Limtador mTLS enabled |
| --- | ---  |
| `{Enable: false, limitador: null}` | false |
| `{Enable: true, limitador: null}` | true |
| `{Enable: false, limitador: false}` | false |
| `{Enable: false, limitador: true}` | false |
| `{Enable: true, limitador: false}` | false |
| `{Enable: true, limitador: true}` | true |

The truth table for authorino component is as follows:

| Spec |  Authorino mTLS enabled |
| --- | ---  |
| `{Enable: false, authorino: null}` | false |
| `{Enable: true, authorino: null}` | true |
| `{Enable: false, authorino: false}` | false |
| `{Enable: false, authorino: true}` | false |
| `{Enable: true, authorino: false}` | false |
| `{Enable: true, authorino: true}` | true |

#### Observability

| **Field** | **Type**                          | **Required** | **Description**                      |
|-----------|-----------------------------------|:------------:|--------------------------------------|
| `enable`    | Boolean     |  No | Enable observability on kuadrant. Default: `false` |

### KuadrantStatus

| **Field**            | **Type**                                                                                     | **Description**                                                                                                                     |
|----------------------|----------------------------------------------------------------------------------------------|-------------------------------------------------------------------------------------------------------------------------------------|
| `observedGeneration` | String                                                                                       | Number of the last observed generation of the resource. Use it to check if the status info is up to date with latest resource spec. |
| `conditions`         | [][ConditionSpec](https://pkg.go.dev/k8s.io/apimachinery@v0.28.4/pkg/apis/meta/v1#Condition) | List of conditions that define that status of the resource.                                                                         |
| `mtlsLimitador` | Boolean | Limitador mTLS enabled. |
| `mtlsAuthorino` | Boolean | Authorino mTLS enabled. |
