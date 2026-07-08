# Kuadrant Helm Charts

This directory contains Helm charts for deploying Kuadrant components.

## Charts

### authorino/
Authorino authorization service deployment.

**Features:**
- CRD installation (Authorino, AuthConfig)
- RBAC management (ClusterRoles, ClusterRoleBindings)
- ServiceAccount creation
- Dual services (gRPC auth, HTTP OIDC)
- TLS support
- Cluster-wide or namespace-scoped mode

See [authorino/README.md](authorino/README.md) for details.

### limitador/
Limitador rate limiting service deployment.

**Features:**
- CRD installation (Limitador)
- Multiple storage backends (memory, Redis, disk)
- ServiceAccount creation
- Limits configuration via ConfigMap
- Dual protocols (HTTP, gRPC)

See [limitador/README.md](limitador/README.md) for details.

## Usage

### Standalone Installation

These charts can be installed directly with Helm:

```bash
# Authorino
helm install authorino ./charts/authorino \
  --namespace authorino-system \
  --create-namespace

# Limitador
helm install limitador ./charts/limitador \
  --namespace limitador-system \
  --create-namespace
```

### Operator Integration (POC)

These charts are embedded in the kuadrant-operator binary and rendered at runtime via the Helm library (helm.sh/helm/v3). The operator:

1. Watches Authorino/Limitador CRs (created by Kuadrant CR reconciler)
2. Renders charts based on CR specs
3. Applies resources using server-side apply
4. Manages lifecycle without Tiller or Helm release state

When used by the operator, CRDs and RBAC ClusterRoles are managed separately (vendored in the operator bundle and installed by OLM).

**POC Architecture:**
```
Kuadrant CR → kuadrant-operator → creates Authorino/Limitador CRs
                                 ↓
                          Helm reconcilers → renders charts
                                 ↓
                          Server-Side Apply → Deployments/Services
```

This replaces the separate authorino-operator and limitador-operator dependencies.

## Structure

Each chart follows standard Helm conventions:

```
chart/
├── Chart.yaml              # Chart metadata
├── values.yaml            # Default values
├── README.md              # Documentation
├── crds/                  # CRD manifests (static, copied from config/crd/bases/)
│   └── *.yaml             # Installed automatically by Helm
└── templates/             # Kubernetes resource templates
    ├── _helpers.tpl       # Template helpers
    ├── deployment.yaml
    ├── service.yaml
    ├── serviceaccount.yaml
    └── clusterrolebinding.yaml  # (Authorino only - references static ClusterRoles)
```

**Static vs Templated:**

| Resource | Location | Reason |
|----------|----------|--------|
| **CRDs** | `config/crd/bases/` (copied to `crds/`) | OLM bundle requirement (static) |
| **ClusterRoles** | `config/rbac/` (NOT in chart) | OLM bundle requirement (static) |
| **ClusterRoleBindings** | Chart templates | Need namespace for ServiceAccount subjects |
| **Deployment/Service/SA** | Chart templates | Namespace-scoped resources |

## CRD Management

By default, CRDs in `crds/` are installed automatically when using `helm install`. To skip CRD installation:

```yaml
crds:
  install: false
```

**Important**: Helm CRD limitations:
- CRDs are installed on `helm install`
- CRDs are **NOT** upgraded on `helm upgrade`
- CRDs are **NOT** deleted on `helm uninstall`

For production or operator usage, manage CRDs separately via `kubectl apply` or OLM bundles.

## RBAC Management (Authorino Only)

**ClusterRoles** are **static manifests** in `config/rbac/`:
- `authorino-manager-role` - Main permissions (secrets, authconfigs, leases)
- `authorino-manager-k8s-auth-role` - TokenReviews/SubjectAccessReviews (clusterWide mode)

**ClusterRoleBindings** are **templated** in the chart (need namespace for ServiceAccount subjects):

```yaml
rbac:
  create: true  # Create ClusterRoleBindings
```

This split allows:
- ClusterRoles → OLM bundle (static, fixed names)
- ClusterRoleBindings → Helm chart (templated, namespace-aware)

When using standalone Helm, install ClusterRoles manually first:
```bash
kubectl apply -f config/rbac/authorino_manager_role.yaml
```

## Development

When modifying charts:

1. Update templates and values
2. Test rendering: `helm template test-name ./charts/authorino/`
3. Rebuild operator image (charts are embedded at build time)
4. Test with `make local-setup`

Chart changes require operator rebuild because charts are copied into the binary at build time (see Dockerfile).

## POC Status

This is a **proof-of-concept** for Spike #183 (OLMv1 consolidation). 

**What's proven:**
✅ Helm charts can be used as templating engine
✅ Server-side apply works with rendered manifests
✅ Charts can be embedded in operator binary
✅ CRDs and RBAC can be included in charts

**Known limitations:**
- Simplified compared to full operator implementations
- Missing some advanced features (see individual chart READMEs)
- No upgrade/cleanup strategy implemented yet

See [olmv1-resource-cleanup-concern.md](/workspace/architecture/docs/design/olmv1-resource-cleanup-concern.md) for cleanup strategy discussion.
