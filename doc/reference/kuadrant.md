# The Kuadrant Custom Resource Definition (CRD)

## kuadrant

| **Field** | **Type**                          | **Required** | **Description**                      |
|-----------|-----------------------------------|:------------:|--------------------------------------|
| `spec`    | [KuadrantSpec](#kuadrantspec)     |      No      | Blank specification                  |
| `status`  | [KuadrantStatus](#kuadrantstatus) |      No      | The status for the custom resources. |

## KuadrantSpec
Currently blank specification.

## KuadrantStatus

| **Field**            | **Type**                                                                                     | **Description**                                                                                                                     |
|----------------------|----------------------------------------------------------------------------------------------|-------------------------------------------------------------------------------------------------------------------------------------|
| `observedGeneration` | String                                                                                       | Number of the last observed generation of the resource. Use it to check if the status info is up to date with latest resource spec. |
| `conditions`         | [][ConditionSpec](https://pkg.go.dev/k8s.io/apimachinery@v0.28.4/pkg/apis/meta/v1#Condition) | List of conditions that define that status of the resource.                                                                         |
