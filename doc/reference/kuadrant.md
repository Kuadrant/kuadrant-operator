# The Kuadrant Custom Resource Definition (CRD)

## kuadrant

<details>
    <summary>Note on Limitador</summary>
The Kuadrant operator creates a Limitador CR named `limitador` in the same namespace as the Kuadrant CR. If there is a pre-existing Limitador CR of the same name the kuadrant operator will take ownership of that Limitador CR. 
</details>

| **Field** | **Type**                          | **Required** | **Description**                                 |
|-----------|-----------------------------------|:------------:|-------------------------------------------------|
| `spec`    | [KuadrantSpec](#kuadrantspec)     |      No      | The specification for Kuadrant custom resource. |
| `status`  | [KuadrantStatus](#kuadrantstatus) |      No      | The status for the custom resources.            |

## KuadrantSpec

| **Field**   | **Type**                | **Required** | **Description**                  |
|-------------|-------------------------|:------------:|----------------------------------|
| `authorino` | [Authorino](#authorino) |      No      | Configure Authorino deployments. | 

### Authorino

| **Field**          | **Type**                    | **Required** | **Description**                                          |
|--------------------|-----------------------------|:------------:|----------------------------------------------------------|
| evaluatorCacheSize | Integer                     |      No      | Cache size (in megabytes) of each Authorino evaluator.   |
| listener           | [Listener](#listener)       |      No      | Specification of authorization service (gRPC interface). |
| metrics            | [Metrics](#metrics)         |      No      | Configuration of the metrics server.                     |
| oidcServer         | [OIDCServer](#oidcserver)   |      No      | Specification of the OIDC service.                       |
| replicas           | Integer                     |      No      | Number of replicas desired for the Authorino instance.   |
| tracing            | [Tracing](#tracing)         |      No      | Configuration f the OpenTelemetry tracing exporter.      |
| volumes            | [VolumesSpec](#volumesSpec) |      No      | Additional volumes to be mounted in the Authorino pods.  |

#### Listener

| **Field**              | **Type**        | **Required** | **Description**                                                                                                 |
|------------------------|-----------------|:------------:|-----------------------------------------------------------------------------------------------------------------|
| ports                  | [Ports](#ports) |      No      | Port numbers of the authorization server (gRPC and raw HTTP interfaces).                                        |
| tls                    | [Tls](#tls)     |      No      | TLS configuration of the authorization server (gRPC and HTTP interfaces).                                       |
| timeout                | Integer         |      No      | Timeout of external authorization request (in milliseconds), controlled internally by the authorization server. |
| maxHttpRequestBodySize | Integer         |      No      | Maximum payload (request body) size for the auth service (HTTP interface0, in bytes.                            |

##### Ports

| **Field** | **Type** | **Required** | **Description**                                                                                        |
|-----------|----------|:------------:|--------------------------------------------------------------------------------------------------------|
| grpc      | Integer  |      No      | Port number of the gRPC interface of the authorization server. Set to 0 to disable this interface.     |
| http      | Integer  |      No      | Port number of the raw HTTP interface of the authorization server. Set to 0 to disable this interface. |

#### Metrics

| **Field** | **Type** | **Required** | **Description**                                                                              |
|-----------|----------|:------------:|----------------------------------------------------------------------------------------------|
| deep      | Boolean  |      No      | Enable/disable metrics at the level of each evaluator config exported by the metrics server. |
| port      | Integer  |      No      | Port number of the metrics server.                                                           |

#### OIDCServer

| **Field**  | **Type**    | **Required** | **Description**                                                               |
|------------|-------------|:------------:|-------------------------------------------------------------------------------|
| port       | Integer     |      No      | Port number of OIDC Discovery server for Festival Wristband tokens.           |
| tls        | [TLS](#tls) |     Yes      | TLS configuration of the ODIC Discovery server for Festival Wristband tokens. |

#### Tracing

| **Field** | **Type** | **Required** | **Description**                                                                                     |
|-----------|----------|:------------:|-----------------------------------------------------------------------------------------------------|
| endpoint  | String   |     Yes      | Full endpoint of the OpenTelemetry tracing collector service (e.g. http://jaegar:14268/api/traces). |
| tags      | Map      |      No      | Key-value map of fixed tags to add to all OpenTelemetry traces emitted by Authorino.                |
| insecure  | Bool     |      No      | Enable/disable insecure connection to the tracing endpoint. Disabled by default.                    |

#### VolumesSpec

| **Field**   | **Type**                    | **Required** | **Description**                                                                                                                    |
|-------------|-----------------------------|:------------:|------------------------------------------------------------------------------------------------------------------------------------|
| defaultMode | [[]VolumeSpec](#volumespec) |      No      | List of additional volumes items to project.                                                                                       |
| items       | Integer                     |      No      | Mode bits used to set permissions on the files. Must be an octal value between 0000 and 0777 or a decimal value between 0 and 511. |

##### VolumeSpec

| **Field**  | **Type**                                                                                              |           **Required**            | **Description**                                                                         |
|------------|-------------------------------------------------------------------------------------------------------|:---------------------------------:|-----------------------------------------------------------------------------------------|
| configMaps | []String                                                                                              |  Yes, if `secrets` is not used.   | List of Kubernetes ConfigMap names to mount.                                            |
| items      | [[]keyToPath](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.23/#keytopath-v1-core) |                No                 | Mount details for selecting specific ConfigMap or Secret entries.                       |
| mountPath  | String                                                                                                |                Yes                | Absolute path where to all the items.                                                   |
| name       | String                                                                                                |                No                 | Name of the volume and volume mount within the Deployment. It must be unique in the CR. |
| secrets    | []String                                                                                              | Yes, if `configMaps` is not used. | List of Kubernetes Secret names to mount.                                               |

#### Tls

| **Field**     | **Type**                                                                                                                  |          **Required**          | **Description**                                                                          |
|---------------|---------------------------------------------------------------------------------------------------------------------------|:------------------------------:|------------------------------------------------------------------------------------------|
| enabled       | Boolean                                                                                                                   |               No               | Whether TLS is enabled or disabled for the server.                                       |
| certSecretRef | [LocalObjectReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.23/#localobjectreference-v1-core) | Required when `enabled: true`  | The reference to the secret that contains the TLS certificates `tls.cert` and `tls.key`. |
| `limitador` | [Limitador](#limitador) |      No      | Configure limitador deployments. | 

### Limitador

| **Field**              | **Type**                                                                           | **Required** | **Description**                                    |
|------------------------|------------------------------------------------------------------------------------|:------------:|----------------------------------------------------|
| `affinity`             | [Affinity](https://pkg.go.dev/k8s.io/api/core/v1#Affinity)                         |      No      | Describes the scheduling rules for limitador pods. |
| `replicas`             | Number                                                                             |      No      | Sets the number of limitador replicas to deploy.   |
| `resourceRequirements` | [ResourceRequirements](https://pkg.go.dev/k8s.io/api/core/v1#ResourceRequirements) |      No      | Set the resource requirements for limitador pods.  |
| `pdb`                  | [PodDisruptionBudgetType](#poddisruptionbudgettype)                                |      No      | Configure allowed pod disruption budget fields.    |
| `storage`              | [Storage](#storage)                                                                |      No      | Define backend storage option for limitador.       |

### PodDisruptionBudgetType

| **Field**        | **Type** | **Required** | **Description**                                                                                                                                                                                                                                                                |
|------------------|----------|:------------:|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `maxUnavailable` | Number   |      No      | An eviction is allowed if at most "maxUnavailable" limitador pods are unavailable after the eviction, i.e. even in absence of the evicted pod. For example, one can prevent all voluntary evictions by specifying 0. This is a mutually exclusive setting with "minAvailable". |
| `minAvailable`   | Number   |      No      | An eviction is allowed if at least "minAvailable" limitador pods will still be available after the eviction, i.e. even in the absence of the evicted pod.  So for example you can prevent all voluntary evictions by specifying "100%".                                        |

### Storage

| **Field**      | **Type**                    | **Required** | **Description**                                                                                                                                                          |
|----------------|-----------------------------|:------------:|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `redis`        | [Redis](#redis)             |      No      | Uses Redis to store limitador counters.                                                                                                                                  |
| `redis-cached` | [RedisCached](#redisCached) |      No      | Uses Redis to store limitador counters, with an in-memory cache                                                                                                          |
| `disk`         | [Disk](#disk)               |      No      | Counters are held on disk (persistent). Kubernetes [Persistent Volumes](https://kubernetes.io/docs/concepts/storage/persistent-volumes/) will be used to store counters. |

#### Redis

| **Field**         | **Type**                                                                           | **Required** | **Description**                                                 |
|-------------------|------------------------------------------------------------------------------------|:------------:|-----------------------------------------------------------------|
| `configSecretRef` | [LocalObjectReference](https://pkg.go.dev/k8s.io/api/core/v1#LocalObjectReference) |      No      | ConfigSecretRef refers to the secret holding the URL for Redis. |

#### RedisCached

| **Field**         | **Type**                                                                           | **Required** | **Description**                                                 |
|-------------------|------------------------------------------------------------------------------------|:------------:|-----------------------------------------------------------------|
| `configSecretRef` | [LocalObjectReference](https://pkg.go.dev/k8s.io/api/core/v1#LocalObjectReference) |      No      | ConfigSecretRef refers to the secret holding the URL for Redis. |
| `options`         | [Options](#options)                                                                |      No      | Configures a number of caching options for limitador.           |

##### Options

| **Field**      | **Type** | **Required** | **Description**                                                            |
|----------------|----------|:------------:|----------------------------------------------------------------------------|
| `ttl`          | Number   |      No      | TTL for cached counters in milliseconds [default: 5000]                    |
| `ratio`        | Number   |      No      | Ratio to apply to the TTL from Redis on cached counters [default: 10]      |
| `flush-period` | Number   |      No      | FlushPeriod for counters in milliseconds [default: 1000]                   |
| `max-cached`   | Number   |      No      | MaxCached refers to the maximum amount of counters cached [default: 10000] |

#### Disk

| **Field**               | **Type**                          | **Required** | **Description**                                                                               |
|-------------------------|-----------------------------------|:------------:|-----------------------------------------------------------------------------------------------|
| `persistentVolumeClaim` | [PVCGenericSpec](#pvcgenericspec) |      No      | Configure resources for PVC.                                                                  |
| `Optimize`              | String                            |      No      | Defines optimization option of the disk persistence type. Valid options: "throughput", "disk" |

##### PVCGenericSpec

| **Field**          | **Type**                                                          | **Required** | **Description**                                                               |
|--------------------|-------------------------------------------------------------------|:------------:|-------------------------------------------------------------------------------|
| `storageClassName` | String                                                            |      No      | Storage class name                                                            |
| `resources`        | [PersistentVolumeClaimResources](#persistentvolumeclaimresources) |      No      | Resources represent the minimum resources the volume should have              |
| `volumeName`       | String                                                            |      No      | VolumeName is the binding reference to the PersistentVolume backing the claim |

###### PersistentVolumeClaimResources

| **Field**  | **Type**                                                                             | **Required** | **Description**                                                     |
|------------|--------------------------------------------------------------------------------------|:------------:|---------------------------------------------------------------------|
| `requests` | [Quantity](https://pkg.go.dev/k8s.io/apimachinery@v0.28.4/pkg/api/resource#Quantity) |     Yes      | Storage resources requests to be used on the persisitentVolumeClaim |

## KuadrantStatus

| **Field**            | **Type**                                                                                     | **Description**                                                                                                                     |
|----------------------|----------------------------------------------------------------------------------------------|-------------------------------------------------------------------------------------------------------------------------------------|
| `observedGeneration` | String                                                                                       | Number of the last observed generation of the resource. Use it to check if the status info is up to date with latest resource spec. |
| `conditions`         | [][ConditionSpec](https://pkg.go.dev/k8s.io/apimachinery@v0.28.4/pkg/apis/meta/v1#Condition) | List of conditions that define that status of the resource.                                                                         |
