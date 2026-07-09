# Wrapper CRD Removal - POC Implementation

## Overview

This document describes the changes made to remove dependency on Authorino and Limitador wrapper CRDs in the Helm-based deployment model.

## What Changed

### Before (Wrapper CRD Model)

```
Kuadrant CR
  ↓ (AuthorinoReconciler creates)
Authorino CR (wrapper)
  ↓ (HelmAuthorinoReconciler watches Authorino CR)
Deployment/Service (owned by Authorino CR)
```

### After (Direct Model - Like DNS Operator)

```
Kuadrant CR
  ↓ (HelmAuthorinoReconciler watches Kuadrant CR directly)
Deployment/Service (owned by Kuadrant CR)
```

## Changes Made

### 1. Authorino Reconciler (`helm_authorino_reconciler.go`)

**Subscription Changes:**
- **Added** watch for `KuadrantGroupKind` (Create, Update)
- **Kept** watch for `AuthorinoGroupKind` (Create, Update, Delete) for migration detection
- Primary trigger is now Kuadrant CR

**Reconcile Logic:**
```go
// Get Kuadrant CR (primary)
kuadrantObj := GetKuadrantFromTopology(topology, state)

// Check for wrapper CR (migration path)
authorinoWrapperObj := GetAuthorinoFromTopology(topology, state)

// Build values from wrapper CR if exists, otherwise defaults
if authorinoWrapperObj != nil {
    values = buildHelmValues(authorinoWrapperObj)  // Use wrapper CR config
} else {
    values = buildDefaultHelmValues()  // Fresh install defaults
}
```

**Owner Reference:**
- Changed from `Authorino CR` → `Kuadrant CR`
- Deployments now owned by Kuadrant (garbage collection works)

**New Function:**
- `buildDefaultHelmValues()` - Returns minimal values (RBAC settings only); chart's `values.yaml` provides all other defaults

### 2. Limitador Reconciler (`helm_limitador_reconciler.go`)

Same pattern as Authorino:
- Watch Kuadrant CR primarily
- Keep Limitador CR watch for migration
- Build from wrapper CR if exists, otherwise defaults
- Owner reference changed to Kuadrant CR

### 3. DNS Operator (Already Correct)

DNS operator already follows this pattern - no changes needed.

## Migration Strategy

### Current State (POC)

**Wrapper CR detection:**
- Reconcilers watch for both Kuadrant CR and wrapper CRs
- If wrapper CR exists: use its configuration
- If no wrapper CR: use defaults
- Topology keeps wrapper CR types for detection

**Migration left for future task:**
1. Detect existing wrapper CR
2. Adopt existing Deployment (change owner to Kuadrant CR)
3. Delete wrapper CR
4. Future reconciliations use Kuadrant CR directly

**Why keep topology code:**
```go
// Keep these functions for migration
GetAuthorinoFromTopology(topology, state)
GetLimitadorFromTopology(topology, state)

// Still watch wrapper CR events
{Kind: ptr.To(v1beta1.AuthorinoGroupKind), ...}
```

### Benefits of Keeping Topology

✅ **Graceful migration** - Detect existing installations
✅ **No breaking changes** - Works with wrapper CRs if they exist  
✅ **Future-proof** - Migration logic can be added later
✅ **Flexibility** - Can coexist with old and new installations

## Testing

### Fresh Install (No Wrapper CRs)

```bash
# Create Kuadrant CR
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1beta1
kind: Kuadrant
metadata:
  name: kuadrant
  namespace: kuadrant-system
spec: {}
EOF

# Check deployments created with default config
kubectl get deployment -n kuadrant-system
# Should see: authorino, limitador, dns-operator (owned by Kuadrant CR)
```

### Existing Install (With Wrapper CRs)

```bash
# Wrapper CRs already exist from AuthorinoReconciler/LimitadorReconciler
kubectl get authorino,limitador -A

# HelmAuthorinoReconciler detects wrapper CR
# Uses wrapper CR configuration
# Still owned by wrapper CR (no migration yet)
```

### Verify Owner References

```bash
# Fresh install - owned by Kuadrant
kubectl get deployment authorino -n kuadrant-system -o jsonpath='{.metadata.ownerReferences[0].kind}'
# Should show: Kuadrant

# Existing install - owned by wrapper CR
kubectl get deployment authorino -n kuadrant-system -o jsonpath='{.metadata.ownerReferences[0].kind}'
# Should show: Authorino
```

## Default Values

### How Defaults Work

When no wrapper CR exists, `buildDefaultHelmValues()` returns minimal values:

**Authorino:**
```go
return map[string]interface{}{
    "rbac": map[string]interface{}{
        "install":               false,                // OLM installs ClusterRoles
        "create":                true,                 // Chart creates ClusterRoleBindings
        "clusterRoleNamePrefix": "kuadrant-operator-", // Match Kustomize namePrefix
    },
    // Everything else comes from charts/authorino/values.yaml
}
```

**Limitador:**
```go
return map[string]interface{}{
    // Empty - everything comes from charts/limitador/values.yaml
}
```

**Chart defaults are the source of truth:**
- `charts/authorino/values.yaml` - defines image, args, TLS, etc.
- `charts/limitador/values.yaml` - defines image, storage, etc.
- No duplication of defaults in Go code ✅

## Future Work

### Migration Task (Separate Spike)

⚠️ **CRITICAL MIGRATION ISSUE - Customized Wrapper CRs**

**Problem:** Users may have customized wrapper CRs in production with fields we don't currently extract:

```yaml
# Production Authorino CR - user customizations
spec:
  volumes:                      # ❌ Not extracted - custom certs lost!
    items:
      - name: custom-ca
        mountPath: /etc/ssl/custom
        secrets: ["corporate-ca"]
  affinity:                     # ❌ Not extracted - HA breaks!
    podAntiAffinity:
      requiredDuringScheduling...: 
  resourceRequirements:         # ❌ Not extracted - no limits!
    limits:
      memory: 512Mi
  logLevel: debug               # ❌ Not extracted - reverts to "info"!
  tracing:                      # ❌ Not extracted!
    endpoint: "tempo:4317"
```

**Current buildHelmValues() only extracts:**
- ✅ image, replicas, clusterWide, TLS settings (Authorino)
- ✅ image, replicas, storage type (Limitador)

**All other fields are LOST during migration!**

**See:** `docs/wrapper-crd-fields-comparison.md` for complete list of missing fields.

**Migration must handle:**

1. **Detection:** Scan wrapper CRs for customized fields before migration
2. **Warning:** Alert users about fields that won't be preserved
3. **Options:**
   - **Option A:** Enhance buildHelmValues() to extract ALL wrapper CR fields (~50 fields)
   - **Option B:** Provide migration script to copy wrapper CR → Deployment patches
   - **Option C:** Wait for spec.authorino/spec.limitador in Kuadrant CR API
   - **Option D:** Keep wrapper CR reconcilers during deprecation period (2-3 releases)

**Recommended approach:**
1. Add detection warnings to buildHelmValues() - flag customized fields
2. Document manual migration steps for common customizations
3. Implement spec.authorino/spec.limitador API fields (longer term)
4. Don't force migration until API is ready

**Example migration logic (incomplete - needs field preservation):**

```go
if authorinoWrapperObj != nil {
    // 1. Detect customized fields
    customizations := detectCustomizations(authorinoWrapperObj)
    if len(customizations) > 0 {
        logger.Warn("Wrapper CR has customizations that won't be migrated",
            "fields", customizations,
            "action", "Manual migration required - see docs/migration-guide.md")
    }
    
    // 2. Get existing deployment
    deployment := getExistingDeployment()
    
    // 3. Change owner to Kuadrant CR
    deployment.SetOwnerReferences([]metav1.OwnerReference{{
        APIVersion: kuadrantObj.APIVersion,
        Kind:       kuadrantObj.Kind,
        Name:       kuadrantObj.Name,
        UID:        kuadrantObj.UID,
    }})
    
    // 4. Apply with Force: true (one-time takeover)
    // NOTE: This preserves Deployment fields due to existing field ownership
    // BUT new installs won't have these customizations!
    apply(deployment, Force: true)
    
    // 5. Delete wrapper CR (ONLY after confirming Deployment is stable)
    deleteAuthorinoCR(authorinoWrapperObj)
    
    logger.Info("migrated from wrapper CR to Kuadrant CR ownership")
}
```

**Considerations:**
- One-time `Force: true` to take ownership
- Handle race conditions (wrapper CR recreation)
- Status updates (migration progress)
- Rollback plan if migration fails
- **Field preservation strategy** - most critical unsolved problem

### API Enhancement (Future)

Add `spec.authorino` and `spec.limitador` to Kuadrant CR:

```yaml
apiVersion: kuadrant.io/v1beta1
kind: Kuadrant
spec:
  authorino:
    image: quay.io/kuadrant/authorino:v0.15.0
    replicas: 2
    clusterWide: true
    resources:
      limits:
        memory: 512Mi
  limitador:
    image: quay.io/kuadrant/limitador:v1.5.0
    replicas: 3
    storage:
      type: redis
```

This would allow full configuration without wrapper CRs.

## Comparison with DNS Operator

DNS operator already follows this pattern:

| Aspect | DNS Operator | Authorino/Limitador (After) |
|--------|--------------|----------------------------|
| **Watches** | Kuadrant CR | Kuadrant CR |
| **Owner** | Kuadrant CR | Kuadrant CR |
| **Wrapper CR** | Never existed | Kept for migration only |
| **Config source** | Defaults only | Wrapper CR if exists, else defaults |

The key difference: Authorino/Limitador keep wrapper CR detection for migration, while DNS never had wrapper CRs.

## Benefits

✅ **Simpler architecture** - One less abstraction layer
✅ **Consistent** - All three operators (Authorino, Limitador, DNS) work the same way
✅ **Better SSA** - Direct Deployment ownership
✅ **User-friendly** - Standard Kubernetes patterns (`kubectl patch deployment`)
✅ **Aligns with RFC** - "Helm as deployment" not "operator manages operators"
✅ **Garbage collection** - Delete Kuadrant CR → all resources deleted
✅ **Migration-aware** - Doesn't break existing installations

## Limitations

⚠️ **No spec.authorino/spec.limitador** - Kuadrant CR doesn't expose these fields yet (future work)
⚠️ **Migration not implemented** - Wrapper CRs no longer created, but manual migration needed for existing installations
⚠️ **Defaults only** - Fresh installs get chart defaults (no customization via CR yet)

These are acceptable for the POC and will be addressed in production implementation.

## Reconciler Removal

**`AuthorinoReconciler` and `LimitadorReconciler` removed from workflow registration.**

These reconcilers created wrapper CRs from Kuadrant CR. With direct Helm-based deployment:
- No new wrapper CRs are created
- Existing wrapper CRs become "frozen" (not updated by any controller)
- HelmAuthorinoReconciler still detects existing wrapper CRs for configuration
- Fresh Kuadrant installations skip wrapper CRs entirely

**Files changed:**
- `internal/controller/state_of_the_world.go` - Commented out reconciler registration

**Impact:**
- ✅ Proves wrapper CR creation is unnecessary
- ✅ Existing clusters continue working (wrapper CRs frozen but functional)
- ✅ Fresh installs work without wrapper CRDs
- ⚠️ Existing wrapper CRs won't receive updates (must migrate manually)
