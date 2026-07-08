# Helm POC Status - Spike #183

**Goal:** Replace authorino-operator and limitador-operator dependencies with Helm chart-based deployment.

## Current Status

✅ **Achieved:**
- Helm charts for Authorino, Limitador, and DNS Operator created in `charts/`
- Charts work standalone with conditional CRD/RBAC installation
- Helm reconcilers integrated into kuadrant-operator
- CRDs vendored in `config/crd/bases/` and `charts/*/crds/`
- ClusterRoles vendored in `config/rbac/` and `charts/*/static/rbac/`
- OLM bundle includes all CRDs and ClusterRoles
- Operator uses Helm as templating engine (no Tiller, server-side apply)
- Successfully eliminates **ALL OLM operator dependencies** (authorino-operator, limitador-operator, dns-operator)
- Helm reconcilers use graceful error handling (continue on failure, don't block other components)

❌ **Known Limitations (Technical Debt):**
- Go module compile-time dependencies remain (see below)
- ClusterRole names have `kuadrant-operator-` prefix (Kustomize namePrefix)
- **Migration issue:** All operators require Kuadrant CR to exist (see Migration Considerations)

## Architecture

### Chart Structure

All three operators (Authorino, Limitador, DNS Operator) follow the same pattern:

```
charts/{authorino,limitador,dns-operator}/
├── crds/                           # Helm auto-installs (--skip-crds to skip)
│   ├── *.yaml                      # CRD definitions
├── static/                         # Pure YAML for OLM bundle
│   └── rbac/
│       └── clusterrole.yaml        # Pure YAML, unprefixed names
└── templates/
    ├── rbac.yaml                   # Wrapper: {{ .Files.Get "static/rbac/..." }}
    ├── clusterrolebinding.yaml     # Templated (uses clusterRoleNamePrefix)
    ├── deployment.yaml
    ├── service.yaml (Authorino only)
    ├── serviceaccount.yaml
    └── configmap.yaml (DNS Operator only)
```

**Key insight:** `static/rbac/` contains pure YAML consumed two ways:
1. Helm renders via `.Files.Get` in `templates/rbac.yaml`
2. OLM fetches directly for bundle (copied to `config/rbac/`, no template parsing needed)

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

| Component | CRDs | ClusterRoles | Templates |
|-----------|------|--------------|-----------|
| **Authorino** | Manual copy from authorino repo | Manual copy from authorino/install/rbac | Managed in `charts/authorino/templates/` |
| **Limitador** | Manual copy from limitador-operator repo | Manual copy from limitador-operator/config/rbac | Managed in `charts/limitador/templates/` |
| **DNS Operator** | Manual copy from dns-operator repo | Manual copy from dns-operator/config/rbac | Managed in `charts/dns-operator/templates/` |

**Future:** Implement `make vendor-{authorino,limitador,dns-operator}` to fetch from upstream

### DNS Operator Kuadrant CR Requirement (Breaking Change)

**Current Behavior (before this POC):**
- **Authorino/Limitador:** Always required Kuadrant CR (created Authorino/Limitador wrapper CRs)
- **DNS Operator:** Installed as OLM dependency, runs independently - **NO Kuadrant CR required**
- Users can use DNSPolicy without Kuadrant CR (just need dns-operator installed)

**New Behavior (this POC):**
- DNS Operator deployment now controlled by Kuadrant CR (via HelmDNSOperatorReconciler)
- **This introduces a NEW requirement** - DNS Operator won't deploy without Kuadrant CR

**Migration Problem - DNS Operator Specific:**

Existing installations using DNSPolicy **without** Kuadrant CR:
1. User has dns-operator installed via OLM dependency
2. User uses DNSPolicy (no Kuadrant CR exists - currently valid)
3. User upgrades kuadrant-operator to Helm-based version
4. OLM removes dns-operator package dependency
5. Helm reconciler won't deploy dns-operator (no Kuadrant CR)
6. **Result:** DNSPolicy stops working - breaking change for users!

**Note:** This is NOT an issue for Authorino/Limitador (they always required Kuadrant CR)

**Options for Production:**

1. **Option A: DNS Operator independent of Kuadrant CR**
   - Deploy dns-operator whenever kuadrant-operator is installed (not tied to Kuadrant CR)
   - Maintains backwards compatibility - DNSPolicy works without Kuadrant CR
   - Matches current behavior
   - **Recommended:** Preserves existing use case

2. **Option B: Detect existing dns-operator and adopt**
   - Check if dns-operator deployment exists (from old OLM install)
   - If exists but no Kuadrant CR → take ownership via server-side apply
   - Smooth migration, no user action needed
   - Still requires Kuadrant CR for new installs (breaking change)

3. **Option C: Require Kuadrant CR + migration doc (current POC)**
   - Document migration requirement: create Kuadrant CR before upgrading
   - **Breaking change** for users using only DNSPolicy
   - Simpler implementation
   - May break existing installations

4. **Option D: Auto-create Kuadrant CR if DNSPolicy exists**
   - Migration controller detects DNSPolicy resources
   - Automatically creates Kuadrant CR if missing
   - **Side effect:** Also deploys Authorino and Limitador (not optional components)
   - Users only wanting DNS Operator get unwanted control plane components
   - Most automated, most complex, most invasive

**POC Decision:** Option C (simplest implementation, documents the issue)

**Production Recommendation:** Option A (preserve backwards compatibility - DNSPolicy shouldn't require Kuadrant CR)

## Open Questions

1. **Chart location:** Keep in `kuadrant-operator/charts/` or separate `helm-charts` repo?
2. **Operator CRD removal:** When can we remove Authorino/Limitador operator interface CRDs?
3. **Kustomize namePrefix:** Should it apply to dependency RBAC?
4. **Migration path:** Breaking change vs gradual deprecation for ClusterRole names?
5. **DNS Operator CR requirement:** Should DNS operator deploy without Kuadrant CR for smoother migration?

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
- Helm renderer: `pkg/helm/renderer.go`
- Helm reconcilers:
  - Authorino: `internal/controller/helm_authorino_reconciler.go`
  - Limitador: `internal/controller/helm_limitador_reconciler.go`
  - DNS Operator: `internal/controller/helm_dnsoperator_reconciler.go`
- State of the world integration: `internal/controller/state_of_the_world.go`
