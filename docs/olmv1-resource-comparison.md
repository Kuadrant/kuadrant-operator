# Resource Comparison: main vs Phase 1

Differences between current main branch deployment and the Phase 1 umbrella operator POC.
These must be addressed for migration compatibility.

## New Resources (Phase 1 only)

| Resource | Name | Notes |
|----------|------|-------|
| Deployment | `mcp-gateway-controller` | New component, not on main |
| ServiceAccount | `mcp-gateway-controller` | New component |
| ClusterRole | `mcp-gateway-controller` | New component |
| ClusterRoleBinding | `mcp-gateway-controller` | New component |

## Removed Resources (main only)

| Resource | Name | Notes |
|----------|------|-------|
| ServiceAccount | `developer-portal-controller-manager` | developer-portal disabled in POC |
| Role | `developer-portal-leader-election-role` | developer-portal disabled in POC |
| RoleBinding | `developer-portal-leader-election-rolebinding` | developer-portal disabled in POC |

## Changed: OwnerReferences

The most significant structural change. On main, child operator Deployments have no ownerReference (installed by kustomize/OLM). On Phase 1, they are owned by the Kuadrant CR.

| Deployment | main ownerRef | Phase 1 ownerRef |
|------------|---------------|------------------|
| `kuadrant-operator-controller-manager` | (none) | (none) |
| `authorino-operator` | (none) | **Kuadrant/kuadrant** |
| `limitador-operator-controller-manager` | (none) | **Kuadrant/kuadrant** |
| `dns-operator-controller-manager` | (none) | **Kuadrant/kuadrant** |
| `authorino` | Authorino/authorino | Authorino/authorino (unchanged) |
| `limitador-limitador` | Limitador/limitador | Limitador/limitador (unchanged) |

**Impact**: Deleting the Kuadrant CR on Phase 1 will cascade-delete all child operator Deployments. On main, deleting the Kuadrant CR only deletes the wrapper CRs (and their workloads). In practice this is generally correct: once the wrapper CRs are deleted, the workloads are gone and there is nothing left for the child operators to reconcile.

**Exception: finalizers**. Some CRDs use finalizers that require their controller to be running:
- Authorino CR has finalizer `authorino.kuadrant.io/finalizer`
- DNSRecord CRs have finalizers for cleaning up external DNS provider records

If the Kuadrant CR is deleted and the child operator Deployments are cascade-deleted at the same time as the CRs they manage, those CRs will be stuck in `Terminating` because the controller that needs to run the finalizer is already gone. DNSRecords are particularly critical since their finalizers clean up external state (DNS provider records).

This needs addressing in the real implementation. The Kuadrant CR currently has no finalizer for managing child operator deletion order (only `kuadrant.io/developerportal`). A new finalizer is needed that:

1. Deletes wrapper CRs (Authorino CR, Limitador CR) and waits for their finalizers to complete
2. Waits for any DNSRecord finalizers to complete (external DNS provider cleanup)
3. Then removes the finalizer, allowing the Kuadrant CR deletion to proceed and cascade-delete the child operator Deployments

Without this, deleting the Kuadrant CR is a race between the cascade deleting the operators and the finalizers needing those operators to be running.

## Changed: Deployment Selector Labels

| Deployment | main selector | Phase 1 selector | Breaking? |
|------------|---------------|-------------------|-----------|
| `limitador-operator-controller-manager` | `control-plane: controller-manager` | `control-plane: limitador-operator-controller-manager` | **Yes** (selector is immutable, cannot adopt existing Deployment) |
| All others | unchanged | unchanged | No |

**Impact**: The limitador-operator selector was patched in Phase 1 (POC hack) to fix a collision with kuadrant-operator. This means the existing Deployment cannot be adopted via SSA during migration. It must be deleted and recreated.

## Changed: Labels

| Resource | Field | main | Phase 1 | Notes |
|----------|-------|------|---------|-------|
| `limitador-operator-controller-manager` Deployment | `metadata.labels` | `control-plane: controller-manager` | `control-plane: controller-manager`, `app.kubernetes.io/managed-by: helm` | Added helm label |
| `limitador-operator-controller-manager` SA | `metadata.labels` | `app: limitador-operator` | `app: limitador-operator`, `app.kubernetes.io/managed-by: helm` | Added helm label |
| `dns-operator-controller-manager` Deployment | `metadata.labels` | `app.kubernetes.io/managed-by: kustomize` | `app.kubernetes.io/managed-by: helm` | Changed from kustomize to helm |
| `dns-operator-controller-manager` SA | `metadata.labels` | `app.kubernetes.io/managed-by: kustomize` | `app.kubernetes.io/managed-by: helm` | Changed from kustomize to helm |
| `dns-operator-*` ClusterRoles | `metadata.labels` | (none) | `app.kubernetes.io/managed-by: helm` | Added helm label |
| `limitador-operator-manager-role` ClusterRole | `metadata.labels` | (none) | `app.kubernetes.io/managed-by: helm` | Added helm label |
| Various dns-operator resources | `metadata.labels` | `app.kubernetes.io/managed-by: kustomize` | `app.kubernetes.io/managed-by: helm` | Changed from kustomize to helm |
| `limitador-operator-manager-config` ConfigMap | `metadata.labels` | (none) | `app.kubernetes.io/managed-by: helm` | Added helm label |
| `dns-operator-controller-env` ConfigMap | `metadata.labels` | (none) | `app.kubernetes.io/managed-by: helm` | Added helm label |

## Changed: Service Selectors

| Service | main selector | Phase 1 selector | Breaking? |
|---------|---------------|-------------------|-----------|
| `limitador-operator-metrics` | `app: limitador-operator`, `control-plane: controller-manager` | `app: limitador-operator`, `control-plane: controller-manager` | **No** (unchanged, but selector collision with kuadrant-operator exists on both) |

## Unchanged (compatible)

The following are identical between main and Phase 1:

- All resource names (Deployments, SAs, ClusterRoles, ClusterRoleBindings, Roles, RoleBindings, Services, ConfigMaps)
- Wrapper CR names and ownerReferences (`authorino` owned by `Kuadrant`, `limitador` owned by `Kuadrant`)
- Workload Deployment names, selectors, and ownerReferences (`authorino`, `limitador-limitador`)
- All Service names, selectors, and ports
- All ClusterRoleBinding role-to-SA mappings
- All Role and RoleBinding names and mappings
- Container names and images

## Summary of Migration Issues

| Issue | Severity | Action Required |
|-------|----------|-----------------|
| limitador-operator selector change | **High** | Deployment must be deleted and recreated (POC hack, fix upstream) |
| Child operator Deployments gain ownerRef to Kuadrant CR | **High** | Cascade deletion will break finalizers on Authorino CR and DNSRecords. Umbrella operator must handle deletion order explicitly |
| `app.kubernetes.io/managed-by` label changes (kustomize to helm) | **Low** | Cosmetic, SSA can update labels |
| Added `app.kubernetes.io/managed-by: helm` labels | **Low** | Cosmetic, SSA can add labels |
| developer-portal resources missing | **Low** | POC configuration, will be re-enabled |
