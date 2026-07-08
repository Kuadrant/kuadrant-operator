# Helm POC Status - Spike #183

**Goal:** Replace authorino-operator and limitador-operator dependencies with Helm chart-based deployment.

## Current Status

✅ **Achieved:**
- Helm charts for Authorino and Limitador created in `charts/`
- Charts work standalone with conditional CRD/RBAC installation
- Helm reconcilers integrated into kuadrant-operator
- CRDs vendored in `config/crd/bases/` and `charts/*/crds/`
- ClusterRoles vendored in `config/rbac/` and `charts/*/static/rbac/`
- OLM bundle includes all CRDs and ClusterRoles
- Operator uses Helm as templating engine (no Tiller, server-side apply)
- Successfully eliminates **runtime** operator dependencies

❌ **Known Limitations (Technical Debt):**
- Go module compile-time dependencies remain (see below)
- ClusterRole names have `kuadrant-operator-` prefix (Kustomize namePrefix)

## Architecture

### Chart Structure

```
charts/authorino/
├── crds/                           # Helm auto-installs (--skip-crds to skip)
│   ├── operator.authorino.kuadrant.io_authorinos.yaml
│   └── authorino.kuadrant.io_authconfigs.yaml
├── static/                         # Pure YAML for OLM bundle
│   └── rbac/
│       ├── clusterrole.yaml        # Fetched from authorino/install/rbac
│       └── clusterrole-k8s-auth.yaml
└── templates/
    ├── rbac.yaml                   # Wrapper: {{ .Files.Get "static/rbac/..." }}
    ├── clusterrolebinding.yaml     # Templated (needs namespace)
    ├── deployment.yaml
    ├── service.yaml
    └── serviceaccount.yaml
```

**Key insight:** `static/rbac/` contains pure YAML consumed two ways:
1. Helm renders via `.Files.Get` in `templates/rbac.yaml`
2. OLM fetches directly for bundle (no template parsing needed)

### Installation Modes

| Mode | CRDs | ClusterRoles | ClusterRoleBindings |
|------|------|--------------|---------------------|
| **Standalone Helm** | Auto-installed from `crds/` | Installed from `static/rbac/` (rbac.install=true) | Templated |
| **OLM/Operator** | OLM installs from bundle | OLM installs from bundle | Templated by operator |

**Operator values:**
```go
values := map[string]interface{}{
    "rbac": map[string]interface{}{
        "install":               false,  // OLM installs ClusterRoles
        "create":                true,   // Chart creates bindings
        "clusterRoleNamePrefix": "kuadrant-operator-",  // Match Kustomize
    },
}
```

### Helm Renderer

`pkg/helm/renderer.go` uses `helm.sh/helm/v3` library:
- `ClientOnly: true` - No Tiller, no cluster connection
- `DryRun: true` - Only renders templates
- `SkipCRDs: true` - CRDs handled separately
- Returns `[]*unstructured.Unstructured` for server-side apply

## Go Dependencies (Technical Debt)

### What Remains

```go
// go.mod
require (
    github.com/kuadrant/authorino-operator v0.25.1
    github.com/kuadrant/limitador-operator v0.18.2
)
```

**Used for:**
- API type definitions (`Authorino`, `Limitador` structs)
- Helper functions (`limitador-operator/pkg/helpers`)

### Why Not Vendored

Attempted to vendor types to `api/external/` but hit blockers:
1. **Type alias conflicts** - Changing import path breaks type compatibility
2. **Helper dependencies** - Deep transitive dependencies
3. **Circular imports** - Risk of import cycles
4. **Maintenance burden** - Syncing types manually

### Recommendation for Production

**Option: Extract shared API package**
- Create `github.com/kuadrant/kuadrant-apis` repository
- Move all CRD types to shared package
- All operators import from `kuadrant-apis`
- Follows Kubernetes ecosystem pattern (`k8s.io/api`, `istio.io/api`)

**For POC:** Compile-time dependencies are acceptable (no runtime impact).

## Migration Considerations

### ClusterRole Name Prefixes

**Current:** ClusterRoles have `kuadrant-operator-` prefix (Kustomize namePrefix)

**Problem:**
- Deviates from upstream (Authorino uses unprefixed names)
- Ownership confusion (ClusterRoles are Authorino's RBAC, not operator's)
- Breaks multi-tenancy (can't share across kuadrant instances)

**Workaround:** Chart uses `rbac.clusterRoleNamePrefix` value to match

**Recommendation for Production:**
- Remove prefix (breaking change, cleaner)
- OR keep for backwards compatibility (document as packaging artifact)

### Source of Truth

| Resource | Source | Sync Strategy |
|----------|--------|---------------|
| **CRDs** | `github.com/Kuadrant/authorino/install/crd/` | Makefile fetch (TODO) |
| **ClusterRoles** | `github.com/Kuadrant/authorino/install/rbac/` | Makefile fetch (TODO) |
| **Deployment/Service** | Managed in `charts/authorino/templates/` | N/A |

**Future:** Implement `make vendor-authorino` to fetch from upstream

## Open Questions

1. **Chart location:** Keep in `kuadrant-operator/charts/` or separate `helm-charts` repo?
2. **Operator CRD removal:** When can we remove Authorino/Limitador operator interface CRDs?
3. **Kustomize namePrefix:** Should it apply to dependency RBAC?
4. **Migration path:** Breaking change vs gradual deprecation for ClusterRole names?

## Testing

**Verification commands:**
```bash
# Build and deploy
make docker-build IMG=quay.io/kuadrant/kuadrant-operator:dev
kind load docker-image quay.io/kuadrant/kuadrant-operator:dev --name kuadrant-local
kubectl rollout restart -n kuadrant-system deployment/kuadrant-operator-controller-manager

# Check resources
kubectl get authorino -A
kubectl get deployments -n kuadrant-system
kubectl get clusterrole | grep authorino
kubectl logs -n kuadrant-system deployment/kuadrant-operator-controller-manager

# Verify RBAC
kubectl auth can-i get secrets \
  --as=system:serviceaccount:kuadrant-system:authorino
```

## References

- Spike issue: https://github.com/Kuadrant/architecture/issues/183
- Chart architecture: `charts/ARCHITECTURE.md`
- Helm renderer: `pkg/helm/renderer.go`
- Authorino reconciler: `internal/controller/helm_authorino_reconciler.go`
