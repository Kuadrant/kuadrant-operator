# Authorino Helm Chart

This Helm chart deploys Authorino, a Kubernetes-native authorization service.

## Features

- ✅ **CRD Management**: Helm auto-installs CRDs from `crds/` directory (use `--skip-crds` to skip)
- ✅ **RBAC Management**: Conditionally installs ClusterRoles from `static/rbac/` (use `rbac.install=false` to skip)
- ✅ **Service Account**: Manages ServiceAccount for Authorino
- ✅ **Dual Services**: Auth service (gRPC) and OIDC service
- ✅ **TLS Support**: Optional TLS configuration for listener and OIDC server
- ✅ **Cluster-Wide Mode**: Watch all namespaces or namespace-scoped

## Prerequisites

- Kubernetes 1.21+
- Helm 3+

## Installation

### Standard Installation (Helm manages everything)

```bash
helm install authorino ./charts/authorino \
  --namespace authorino-system \
  --create-namespace
```

This installs:
- ✅ CRDs (from `crds/` directory, auto-installed by Helm)
- ✅ ClusterRoles (from `static/rbac/`, via `rbac.install=true`)
- ✅ ClusterRoleBindings, Deployment, Services, ServiceAccount

### OLM/Operator-Managed Installation

When managed by kuadrant-operator or OLM:

```bash
helm install authorino ./charts/authorino \
  --namespace authorino-system \
  --create-namespace \
  --skip-crds \
  --set rbac.install=false
```

This installs:
- ❌ CRDs (installed by OLM from bundle)
- ❌ ClusterRoles (installed by OLM from bundle)
- ✅ ClusterRoleBindings, Deployment, Services, ServiceAccount

### Install with custom values

```bash
helm install authorino ./charts/authorino \
  --namespace authorino-system \
  --create-namespace \
  --set replicas=2 \
  --set clusterWide=false
```

## Configuration

### CRD Management

CRDs are located in the `crds/` directory and are **auto-installed by Helm** on `helm install`.

**Helm CRD behavior:**
- ✅ Installed automatically before other resources
- ❌ NOT upgraded on `helm upgrade` (manual upgrade required)
- ❌ NOT deleted on `helm uninstall` (manual deletion required)
- ❌ NOT templated (no values.yaml control)

**To skip CRD installation:**
```bash
helm install authorino ./charts/authorino --skip-crds
```

**Source of truth:** CRDs are fetched from `github.com/Kuadrant/authorino/install/crd/`

### RBAC Configuration

```yaml
rbac:
  install: true  # Install ClusterRoles from static/rbac/
  create: true   # Create ClusterRoleBindings

clusterWide: true  # Watch all namespaces (requires additional k8s auth RBAC)
```

**ClusterRoles** are pure YAML in `static/rbac/`:
- `clusterrole.yaml` - Main ClusterRole (`authorino-manager-role`, always installed when `rbac.install=true`)
- `clusterrole-k8s-auth.yaml` - K8s auth ClusterRole (`authorino-manager-k8s-auth-role`, only when `rbac.install=true` AND `clusterWide=true`)

**ClusterRoleBindings** are templated in `templates/clusterrolebinding.yaml`:
- Reference the static ClusterRole names
- Include namespace for ServiceAccount subjects (from `.Release.Namespace`)

**Why this separation?**
- ✅ Static ClusterRoles can be included in OLM bundle (no templating)
- ✅ Templated ClusterRoleBindings get correct namespace
- ✅ Helm chart can conditionally install (`rbac.install=false` for OLM)

**Source of truth:** ClusterRoles are fetched from `github.com/Kuadrant/authorino/install/rbac/`

### TLS Configuration

```yaml
tls:
  enabled: false  # Enable/disable TLS
  certSecretName: authorino-oidc-server-cert
```

When enabled, expects a Secret with `tls.crt`, `tls.key`, `oidc.crt`, and `oidc.key`.

### Image Configuration

```yaml
image:
  repository: quay.io/kuadrant/authorino
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
| `rbac.install` | Install ClusterRoles from static/rbac/ | `true` |
| `rbac.create` | Create ClusterRoleBindings | `true` |
| `clusterWide` | Watch all namespaces | `true` |
| `replicas` | Number of replicas | `1` |
| `image.repository` | Image repository | `quay.io/kuadrant/authorino` |
| `image.tag` | Image tag | `latest` |
| `tls.enabled` | Enable TLS | `true` |
| `serviceAccount.create` | Create ServiceAccount | `true` |

**Note:** CRDs have no values.yaml control - use `--skip-crds` flag instead.

## Operator Integration

This chart works standalone **or** embedded in kuadrant-operator.

### Resource Sources

| Resource | Standalone Helm | OLM/Operator |
|----------|----------------|--------------|
| **CRDs** | Helm auto-installs from `crds/` | OLM installs from bundle (chart uses `--skip-crds`) |
| **ClusterRoles** | Helm installs from `static/rbac/` | OLM installs from bundle (chart uses `rbac.install=false`) |
| **ClusterRoleBindings** | Helm templates → kubectl apply | Operator templates → server-side apply |
| **Deployment, Services** | Helm templates → kubectl apply | Operator templates → server-side apply |

### Chart Structure

```
charts/authorino/
├── crds/                           # Auto-installed by Helm (no templating)
│   ├── operator.authorino.kuadrant.io_authorinos.yaml
│   └── authorino.kuadrant.io_authconfigs.yaml
├── static/                         # Pure YAML for OLM bundle consumption
│   └── rbac/
│       ├── clusterrole.yaml        # authorino-manager-role
│       └── clusterrole-k8s-auth.yaml  # authorino-manager-k8s-auth-role
└── templates/
    ├── rbac.yaml                   # Wrapper that reads from static/rbac/
    ├── clusterrolebinding.yaml     # References static ClusterRole names
    ├── deployment.yaml
    ├── service.yaml
    └── serviceaccount.yaml
```

**Key insight:** `static/rbac/` contains **pure YAML** that can be:
1. ✅ Rendered by Helm via `.Files.Get` in `templates/rbac.yaml`
2. ✅ Fetched directly by OLM for bundle inclusion (no template parsing needed)

This eliminates duplication - single source of truth in `static/`, consumed two ways.
