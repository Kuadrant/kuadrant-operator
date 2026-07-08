# Developer Portal RBAC Note

## Architecture

Developer-portal has a unique deployment model:

1. **CRDs**: Owned by kuadrant-operator (in bundle as `devportal.kuadrant.io/*`)
2. **Deployment**: Created dynamically by `DeveloperPortalReconciler` in kuadrant-operator
3. **RBAC**: Managed via CSV permissions (OLM creates ServiceAccount)

## How It Works

### Local Development

`make deploy-dependencies` includes `config/dependencies/developer-portal`:
- Fetches from upstream: https://github.com/Kuadrant/developer-portal-controller
- Creates: ServiceAccount, ClusterRole, RoleBindings
- kuadrant-operator then creates: Deployment

### OLM Installation

Bundle CSV declares permissions for `serviceAccountName: developer-portal-controller-manager`:
```yaml
spec:
  install:
    spec:
      clusterPermissions:
      - rules: [...]
        serviceAccountName: developer-portal-controller-manager
```

**OLM behavior:**
- Reads CSV permissions
- Auto-creates ServiceAccount: `developer-portal-controller-manager`
- Grants ClusterRole permissions to that ServiceAccount
- kuadrant-operator Deployment references this ServiceAccount

## Why This Differs from Authorino/Limitador

**Authorino/Limitador (Helm POC):**
- ClusterRoles in bundle (static)
- ClusterRoleBindings in Helm charts (dynamic namespace reference)
- ServiceAccount in Helm charts

**Developer-Portal:**
- Permissions in CSV (OLM-managed)
- ServiceAccount auto-created by OLM
- Deployment created by kuadrant-operator reconciler

## Verification Needed

When testing OLM bundle, verify:

```bash
# After OLM install
kubectl get serviceaccount developer-portal-controller-manager -n kuadrant-system
# Should exist (created by OLM)

kubectl get clusterrolebinding | grep developer-portal
# Should show bindings (created by OLM from CSV)

# After Kuadrant CR created with developerPortal enabled
kubectl get deployment developer-portal-controller -n kuadrant-system
# Should exist (created by DeveloperPortalReconciler)

kubectl get pods -n kuadrant-system | grep developer-portal
# Should be running
```

## Comparison

| Component | RBAC Location | ServiceAccount Creation | Deployment Creation |
|-----------|---------------|------------------------|---------------------|
| Authorino | Bundle (ClusterRoles) + Helm (Bindings) | Helm chart | Helm chart |
| Limitador | None needed | Helm chart | Helm chart |
| Developer-Portal | CSV permissions | OLM auto-creates | kuadrant-operator reconciler |

## For POC

✅ **No changes needed** - developer-portal already works with OLM model
✅ Keep in `config/dependencies` for local dev
✅ CSV already has correct permissions
✅ Test during integration to verify OLM creates ServiceAccount correctly
