# Limitador Helm Chart

This Helm chart deploys Limitador, a generic rate limiting service.

## Features

- ✅ **CRD Installation**: Installs Limitador CRD
- ✅ **Multiple Storage Backends**: Memory, Redis, Redis-cached, Disk
- ✅ **Service Account**: Manages ServiceAccount for Limitador
- ✅ **ConfigMap Management**: Automatic limits configuration
- ✅ **Dual Protocols**: HTTP REST API and gRPC RLS (Envoy Rate Limit Service)

## Prerequisites

- Kubernetes 1.21+
- Helm 3+
- Redis (if using redis or redis-cached storage)

## Installation

### Install with in-memory storage

```bash
helm install limitador ./charts/limitador \
  --namespace limitador-system \
  --create-namespace
```

### Install with Redis storage

```bash
helm install limitador ./charts/limitador \
  --namespace limitador-system \
  --create-namespace \
  --set storage.type=redis \
  --set env[0].name=REDIS_URL \
  --set env[0].value=redis://redis:6379
```

## Configuration

### CRD Management

By default, CRDs are installed automatically. If CRDs are managed externally (e.g., by kuadrant-operator):

```yaml
crds:
  install: false  # Skip CRD installation
```

**Note**: Helm's CRD management has limitations - CRDs in `crds/` directory are:
- Installed on `helm install`
- **NOT** upgraded on `helm upgrade`
- **NOT** deleted on `helm uninstall`

For production, consider managing CRDs separately via `kubectl apply` or an operator.

### Storage Backends

#### In-Memory (Default)

```yaml
storage:
  type: memory
```

Fast, ephemeral storage. Resets on pod restart.

#### Redis

```yaml
storage:
  type: redis

env:
  - name: REDIS_URL
    value: redis://redis:6379
```

Persistent storage using Redis.

#### Redis-Cached

```yaml
storage:
  type: redis-cached

env:
  - name: REDIS_URL
    value: redis://redis:6379
```

Hybrid: in-memory cache with Redis persistence.

#### Disk

```yaml
storage:
  type: disk
```

RocksDB-based persistent storage on local disk (uses emptyDir volume).

### Limits Configuration

Limits are configured via ConfigMap. By default, an empty ConfigMap is created.

```yaml
limits:
  configMapName: ""  # Auto-generate name
```

Or reference an existing ConfigMap:

```yaml
limits:
  configMapName: my-limits-config
```

### Image Configuration

```yaml
image:
  repository: quay.io/kuadrant/limitador
  tag: latest
  pullPolicy: IfNotPresent
```

### Resources

```yaml
resources:
  limits:
    cpu: 500m
    memory: 512Mi
  requests:
    cpu: 100m
    memory: 128Mi
```

## Values Reference

| Parameter | Description | Default |
|-----------|-------------|---------|
| `crds.install` | Install CRDs | `true` |
| `replicas` | Number of replicas | `1` |
| `image.repository` | Image repository | `quay.io/kuadrant/limitador` |
| `image.tag` | Image tag | `latest` |
| `storage.type` | Storage backend | `memory` |
| `limits.configMapName` | Limits ConfigMap name | `""` (auto-generated) |
| `serviceAccount.create` | Create ServiceAccount | `true` |
| `env` | Environment variables | `[]` |

## Operator Integration

This chart can be used standalone or embedded in kuadrant-operator. When used by the operator:

- CRDs are managed separately (vendored in operator bundle)
- Chart renders Deployment, Service, ServiceAccount, and ConfigMap only

The operator creates a Limitador CR which triggers the Helm reconciler to deploy these resources.
