# Configure Counter Storage for Resilient Deployment

## Overview

This guide includes steps to enable the Kuadrant counter storage feature.
The feature provides a high level integration with rate limit counter storage, enabling a more resilient deployment.
It works by configuring external storage to persist rate limit counters in HA deployments and service restarts.

## Prerequisites

- You have installed Kuadrant in a Kubernetes cluster.
- [Optional] Redis instance for backend storage.

## Enabling Counter Storage

To enable Counter Storage for rate limit counters, set `counterStorge: {}` to the required configuration in the `resilience` section in your Kuadrant CR:

```yaml
apiVersion: kuadrant.io/v1beta1
kind: Kuadrant
metadata:
  name: kuadrant-sample
spec:
  resilience:
    counterStorage: {}
```

When configured, Kuadrant copies the `counterStorage` configuration to the Limitador CR `spec.storage`, and maintains the configuration.
Errors raised by the configuration will be reflected in the Kuadrant status block.

## Counter Storage Configuration
<!-- Reference: https://github.com/Kuadrant/limitador-operator/blob/main/doc/storage.md -->

Rate limit counters are stored in a backend storage. 
This is in contrast to the storage of the limits themselves, which are always stored in ephemeral memory. 
Supported storage configurations:

* In-Memory: ephemeral and cannot be shared
* Redis: Persistent (depending on the redis storage configuration) and can be shared
* Redis Cached: Persistent (depending on the redis storage configuration) and can be shared
* Disk: Persistent (depending on the underlying disk persistence capabilities) and cannot be shared

### In-Memory

Counters are held in Limitador (ephemeral)

In-Memory is the default option.
In-Memory storage can be explicitly defined.

```yaml
apiVersion: kuadrant.io/v1beta1
kind: Kuadrant
metadata:
  name: kuadrant-sample
spec:
  resilience:
    counterStorage: {}
```

Or implicitly defined by not adding any configuration
```yaml
apiVersion: kuadrant.io/v1beta1
kind: Kuadrant
metadata:
  name: kuadrant-sample
spec: {}
```

### Redis

Uses Redis to store counters.

Selected when `spec.resilience.countStorage.redis` is not `null`.

```yaml
apiVersion: kuadrant.io/v1beta1
kind: Kuadrant
metadata:
  name: kuadrant-sample
spec:
  resilience:
    counterStorage:
        redis:
          configSecretRef: # The secret reference storing the URL for Redis
            name: redisconfig
```

The URL of the Redis service is provided inside a K8s opaque
[Secret](https://kubernetes.io/docs/concepts/configuration/secret/).
The secret is required to be in the same namespace as the `kuadrant` CR.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: redisconfig
stringData:
  URL: redis://127.0.0.1/a # Redis URL of its running instance
type: Opaque
```

**Note**: Limitador's Operator will only read the `URL` field of the secret, and the Kuadrant Operator does not read the secret.

### Redis Cached

Uses Redis to store counters, with an in-memory cache.

Selected when `spec.storage.redis-cached` is not `null`.

```yaml
apiVersion: kuadrant.io/v1beta1
kind: Kuadrant
metadata:
  name: kuadrant-sample
spec:
  resilience:
    counterStorage:
        redis-cached:
          configSecretRef: # The secret reference storing the URL for Redis
            name: redisconfig
```

The URL of the Redis service is provided inside a K8s opaque
[Secret](https://kubernetes.io/docs/concepts/configuration/secret/).
The secret is required to be in the same namespace as the `kuadrant` CR.
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: redisconfig
stringData:
  URL: redis://127.0.0.1/a # Redis URL of its running instance
type: Opaque
```

**Note**: Limitador's Operator will only read the `URL` field of the secret, and the Kuadrant Operator does not read the secret.

Additionally, caching options can be specified in the `spec.resilience.counterStorage.redis-cached.options` field.

#### Options

| Option               | Description                                                           |
|----------------------|-----------------------------------------------------------------------|
| `batch-size`         | Size of entries to flush in as single flush [default: 100]            |
| `flush-period`       | Flushing period for counters in milliseconds [default: 1000]          |
| `max-cached`         | Maximum amount of counters cached [default: 10000]                    |
| `response-timeout`   | Timeout for Redis commands in milliseconds [default: 350]             |

For example:

```yaml
apiVersion: kuadrant.io/v1beta1
kind: Kuadrant
metadata:
  name: kuadrant-sample
spec:
  resilience:
    counterStorage:
        redis-cached:
          configSecretRef: # The secret reference storing the URL for Redis
            name: redisconfig
          options: # Every option is optional
            batch-size: 50
            max-cached: 5000
```

### Disk

Counters are held on disk (persistent).
Kubernetes [Persistent Volumes](https://kubernetes.io/docs/concepts/storage/persistent-volumes/)
will be used to store counters.

Selected when `spec.resilience.counterStorage.disk` is not `null`.

```yaml
apiVersion: kuadrant.io/v1beta1
kind: Kuadrant
metadata:
  name: kuadrant-sample
spec:
  resilience:
    counterStorage:
        disk: {}
```

Additionally, disk options can be specified in the `spec.resilience.counterStorage.disk.persistentVolumeClaim`
and `spec.resilience.counteStorage.disk.optimize` fields.

#### Persistent Volume Claim Options

`spec.resilience.counterStorage.disk.persistentVolumeClaim` field is an object with the following fields.

| Field                | Description                                                                                                                                                                                                                                                                                           |
|----------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `storageClassName`   | [StorageClass](https://kubernetes.io/docs/concepts/storage/storage-classes/) of the storage offered by cluster administrators [default: default storage class of the cluster]                                                                                                                         |
| `resources`          | The minimum [resources](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.25/#quantity-resource-core) the volume should have. Resources will not take any effect when VolumeName is provided. This parameter is not updateable when the underlying PV is not resizable. [default: 1Gi] |
| `volumeName`         | The binding reference to the existing PersistentVolume backing this claim [default: *null*]                                                                                                                                                                                                           |

Example:

```yaml
apiVersion: kuadrant.io/v1beta1
kind: Kuadrant
metadata:
  name: kuadrant-sample
spec:
  resilience:
    counterStorage:
        disk:
          persistentVolumeClaim:
            storageClassName: "customClass"
            resources:
              requests: 2Gi
```

#### Optimize

Defines the valid optimization option of the disk persistence type.

`spec.resilience.counterStorage.disk.optimize` field is a `string` type with the following valid values:

| Option         | Description                              |
|----------------|------------------------------------------|
| `throughput`   | Optimizes for higher throughput. **Default** |
| `disk`         | Optimizes for disk usage                 |

Example:

```yaml
apiVersion: kuadrant.io/v1beta1
kind: Kuadrant
metadata:
  name: kuadrant-sample
spec:
  resilience:
    counterStorage:
        disk:
          optimize: disk
```

## Disabling Counter Storage

When the counter storage configuration is removed, the Kuadrant Operator will revert the `Limirtador` CR back to the default of using In-Memory counters.
The Kuadrant Operator will not remove user created resources, such as the redis configuration secret.

Wtih the counter storage not being configured, the Kuadrant Operator allows the user to modify `spec.storage` directly in the `limitador` CR.
The Kuadrant Operator will not revert user defined configuration.
