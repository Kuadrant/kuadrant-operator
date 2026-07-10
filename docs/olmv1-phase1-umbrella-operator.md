# OLMv1 Phase 1: Umbrella Operator Pattern

## Overview

Phase 1 implements an umbrella operator pattern that solves OLMv1 compliance while minimizing risk and complexity.

## The Problem

OLMv1 removes `dependencies.yaml` support, meaning Kuadrant operator can no longer depend on OLM to install and manage Authorino Operator and Limitador Operator.

## Phase 1 Solution

**Deploy the operators themselves via Helm, keep wrapper CRs.**

### Architecture

```
Kuadrant Operator (umbrella)
  ├─ HelmAuthorinoOperatorReconciler → Authorino Operator Deployment
  │     ↓
  │   Authorino Operator (watches Authorino CRs)
  │     ↓
  │   AuthorinoReconciler → Authorino CR (wrapper)
  │     ↓
  │   Authorino Operator → Authorino workload Deployment
  │
  ├─ HelmLimitadorOperatorReconciler → Limitador Operator Deployment
  │     ↓
  │   Limitador Operator (watches Limitador CRs)
  │     ↓
  │   LimitadorReconciler → Limitador CR (wrapper)
  │     ↓
  │   Limitador Operator → Limitador workload Deployment + ConfigMap
  │
  └─ HelmDNSOperatorReconciler → DNS Operator Deployment
```

### What Changed

| Component | Before (OLMv0) | After (Phase 1) |
|-----------|----------------|-----------------|
| **Authorino Operator** | Installed by OLM | Deployed by Kuadrant via Helm |
| **Limitador Operator** | Installed by OLM | Deployed by Kuadrant via Helm |
| **Authorino CR** | Created by AuthorinoReconciler | ✅ **No change** |
| **Limitador CR** | Created by LimitadorReconciler | ✅ **No change** |
| **Workload deployment** | Managed by operators | ✅ **No change** |
| **User experience** | Install 3 operators | ✅ **No change** (install 1) |

### Benefits

1. **✅ OLMv1 compliant** - No dependencies.yaml
2. **✅ Zero migration risk** - Wrapper CRs unchanged
3. **✅ No breaking changes** - Same user experience
4. **✅ Small code change** - Similar to DNS operator pattern
5. **✅ No field loss** - Wrapper CRs preserve all customization
6. **✅ Low complexity** - Just deploy operators, everything else same

## Files Changed

### New Files

- `internal/controller/helm_authorino_operator_reconciler.go` - Deploys Authorino Operator
- `internal/controller/helm_limitador_operator_reconciler.go` - Deploys Limitador Operator
- `internal/controller/helm_helpers.go` - Shared Helm utilities
- `charts/authorino-operator/` - Helm chart for Authorino Operator (placeholder)
- `charts/limitador-operator/` - Helm chart for Limitador Operator (placeholder)

### Restored Files

- `internal/controller/authorino_reconciler.go` - Creates Authorino CR wrapper
- `internal/controller/limitador_reconciler.go` - Creates Limitador CR wrapper

### Deleted Files

- `internal/controller/helm_authorino_reconciler.go` - Was deploying workload directly
- `internal/controller/helm_limitador_reconciler.go` - Was deploying workload directly
- `internal/controller/helm_reconciler_test.go` - Tests for deleted reconcilers

### Modified Files

- `internal/controller/state_of_the_world.go` - Re-enabled wrapper CR reconcilers, swapped workload for operator Helm reconcilers

## Next Steps

### To Complete Phase 1

1. **Port Authorino Operator manifests** to `charts/authorino-operator/`
   - Source: https://github.com/Kuadrant/authorino-operator/tree/main/config/deploy
   - Deployment, RBAC, ServiceAccount

2. **Port Limitador Operator manifests** to `charts/limitador-operator/`
   - Source: https://github.com/Kuadrant/limitador-operator/tree/main/config/deploy
   - Deployment, RBAC, ServiceAccount

3. **Test on cluster**
   - Deploy Kuadrant CR
   - Verify operators are deployed
   - Verify wrapper CRs are created
   - Verify workloads are created
   - Verify policies work end-to-end

## Comparison: Phase 1 vs Full Removal

| Aspect | Phase 1 (Umbrella) | Full Removal |
|--------|-------------------|--------------|
| **OLMv1 compliant** | ✅ Yes | ✅ Yes |
| **Code changes** | Small | Large |
| **Migration risk** | Zero | High |
| **Breaking changes** | None | Wrapper CRs removed |
| **Field loss** | None | Yes (customized wrapper CRs) |
| **ConfigMap logic** | None needed | Custom reconciliation required |
| **Complexity** | Low | High |
| **Time to implement** | Days | Weeks |
| **User impact** | None | Medium (lost fields) |

## Why Phase 1 is Better

1. **Solves the actual problem**: OLMv1 compliance
2. **Lower risk**: No migration, no breaking changes
3. **Faster delivery**: Simple pattern, less code
4. **Preserves options**: Can still do full removal later if beneficial
5. **Proven pattern**: Already working for DNS operator

## Future: Phase 2 (Optional)

If wrapper CRs become a maintenance burden, Phase 2 could remove them:
- Deploy workloads directly via Helm
- Write limits directly to ConfigMap
- More complex, but already explored in `olmv1-umbrella-poc` branch

Phase 1 doesn't block Phase 2 - it just defers the decision until there's a clear reason to remove wrapper CRs.
