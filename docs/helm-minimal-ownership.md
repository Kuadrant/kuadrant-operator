# Helm Minimal Field Ownership

## Overview

This document explains the field ownership strategy for Helm-based operator deployments, addressing [CONNLINK-1022](https://redhat.atlassian.net/browse/CONNLINK-1022) - allowing users to customize deployments without having changes reverted.

## Server-Side Apply Behavior

With Server-Side Apply (SSA), field ownership is **binary**:
- **Field in Apply payload** → We own it → We always update it to our value
- **Field NOT in Apply payload** → We don't own it → Users can modify freely

There is no "suggest but allow override" mode.

## Changes Made

### 1. Set `Force: false` in All Reconcilers

**Before:**
```go
metav1.ApplyOptions{
    FieldManager: "kuadrant-operator",
    Force:        true,  // Takes ownership of ALL fields
}
```

**After:**
```go
metav1.ApplyOptions{
    FieldManager: "kuadrant-operator",
    Force:        false, // Only own fields we explicitly set
}
```

**Files changed:**
- `internal/controller/helm_authorino_reconciler.go`
- `internal/controller/helm_limitador_reconciler.go`
- `internal/controller/helm_dnsoperator_reconciler.go`

### 2. Conditional Replicas Ownership

**Before:**
```go
// Always set replicas, always own it
if authorinoObj.Spec.Replicas != nil {
    values["replicas"] = *authorinoObj.Spec.Replicas
} else {
    values["replicas"] = 1  // ❌ Always own replicas
}
```

**After:**
```go
// Only set replicas if explicitly specified in CR
if authorinoObj.Spec.Replicas != nil {
    values["replicas"] = *authorinoObj.Spec.Replicas
}
// If not set, don't include in values → not owned → user can scale freely
```

### 3. Removed Default Replicas from values.yaml

**Before:**
```yaml
replicas: 1  # ❌ Default makes it always present
```

**After:**
```yaml
# replicas: 1  # Commented out - only set via CR to allow user scaling/HPA
```

**Files changed:**
- `charts/authorino/values.yaml`
- `charts/limitador/values.yaml`
- `charts/dns-operator/values.yaml`

### 4. Conditional Replicas in Templates

**Before:**
```yaml
spec:
  replicas: {{ .Values.replicas }}  # ❌ Fails if not set
```

**After:**
```yaml
spec:
  {{- if .Values.replicas }}
  replicas: {{ .Values.replicas }}
  {{- end }}
```

**Files changed:**
- `charts/authorino/templates/deployment.yaml`
- `charts/limitador/templates/deployment.yaml`
- `charts/dns-operator/templates/deployment.yaml`

### 5. Fixed DNS Operator Resources Template

Made resources conditional (already conditional in Authorino/Limitador):

```yaml
{{- with .Values.resources }}
resources:
  {{- toYaml . | nindent 10 }}
{{- end }}
```

## Field Ownership Tiers

### Understanding CONNLINK-1022

**What CONNLINK-1022 Actually Requires:**

The issue ([CONNLINK-1022](https://redhat.atlassian.net/browse/CONNLINK-1022)) is about **HPA (Horizontal Pod Autoscaler) support**, specifically:

**Primary requirement:**
- ✅ **Support HPA integration** - Don't force ownership of `spec.replicas`
- ✅ **Allow replicas configuration via Kuadrant CR** - Users should be able to set replicas OR use HPA

**Secondary requirement (optional):**
- ⚠️ **Optionally allow resource configuration** - For proper HPA metrics (CPU/memory %)

**What we've fixed:**
- ✅ Conditional replicas (only set if specified in CR)
- ✅ `Force: false` (no forced ownership conflicts)
- ✅ HPA can now control replicas when not set in CR

**Resources are NOT required by CONNLINK-1022** - the issue is about replicas and HPA, not resource limits.

### Open Question: Resource Defaults

**Current state:** Resources are Tier 3 (never set, never owned)

**Trade-offs:**

**Pros (current approach):**
- ✅ Maximum user flexibility
- ✅ No field ownership conflicts
- ✅ Users can customize freely via `kubectl set resources`

**Cons (current approach):**
- ❌ Fresh deployments have NO resource limits (dangerous in production)
- ❌ No sensible defaults for users who don't know what to set
- ❌ HPA CPU-based scaling won't work without CPU requests

**Alternative:** Set defaults, allow override via CR
```yaml
resources:  # We own this field
  limits:
    memory: 512Mi
  requests:
    memory: 256Mi
    cpu: 100m  # Needed for CPU-based HPA
```

Users override via Kuadrant CR:
```yaml
spec.authorino.resources:
  limits: {...}
```

**Decision needed:** Should we set resource defaults or leave fully user-controlled?

### Field Decision Framework

When adding fields to Helm charts, consider:

**Questions to ask:**

1. **Is this critical for operation?** → Tier 1 (always own)
   - Image, args, serviceAccountName
   - We MUST control these for version management

2. **Do users need to customize this?** → Tier 3 (never own)
   - Sidecars, volumes, annotations
   - User-specific operational concerns

3. **Is there a sensible default, but users might want to override?** → Tier 2 (conditional)
   - Replicas (default 1, but HPA might control)
   - Resources (default limits, but users might tune)
   - Node selectors, tolerations (cluster-specific)

4. **Would setting a default prevent a valid use case?** → Tier 3 (don't set it)
   - Example: Setting replicas=1 prevents HPA (CONNLINK-1022)
   - Example: Setting resources prevents user tuning

**Fields requiring decision:**

| Field | Current Tier | Question | Decision Needed? |
|-------|--------------|----------|------------------|
| `resources` | Tier 3 (never set) | Should we set defaults for production safety? | ✅ Yes |
| `tolerations` | Tier 3 (never set) | Should users control this or CR-driven? | Maybe |
| `nodeSelector` | Tier 3 (never set) | Cluster-specific, probably Tier 3 | No |
| `affinity` | Tier 3 (never set) | Complex, probably Tier 3 | No |
| `securityContext` | Tier 3 (never set) | Security-sensitive, probably Tier 3 | No |

**Recommendation:**
- Review each field before adding to templates
- Default to Tier 3 (user freedom) unless there's a strong reason
- Document the decision (why Tier 1/2/3) in code comments

### Tier 1: Always Own (Critical for Operation)

**We MUST control these fields:**
- `spec.template.spec.containers[*].image` - Version control
- `spec.template.spec.containers[*].args` - Feature flags, critical config
- `spec.template.spec.serviceAccountName` - RBAC integration
- Essential labels for selector matching

**Users cannot override these** (by design - we control versions/config).

### Tier 2: Conditionally Own (User Choice)

**We own ONLY if set in Kuadrant/Authorino/Limitador CR:**
- `spec.replicas` - If unset in CR, users can use `kubectl scale` or HPA ✅ **Required for CONNLINK-1022**
- `spec.template.spec.containers[*].imagePullPolicy` - Only if set in CR
- Other CR-driven fields

**Example:**
- User leaves `spec.authorino.replicas` unset in Kuadrant CR → User can scale freely / HPA works ✅
- User sets `spec.authorino.replicas: 3` in Kuadrant CR → We control it, user scaling reverted ✅

### Tier 3: Never Own (User Freedom)

**We NEVER set these in values - users always control:**
- `spec.template.spec.containers[*].resources` (limits/requests) - **Decision pending** (see above)
- Additional containers (sidecars, init containers)
- Additional volumes, volumeMounts
- Annotations (except our own)
- Tolerations, nodeSelector, affinity
- SecurityContext details

## User Impact

### CONNLINK-1022 Resolution ✅

**Before:**
```bash
# User sets resource limits
kubectl set resources deployment authorino --limits=memory=512Mi

# Kuadrant reconciles
# ❌ Reverts to whatever we set (or empty if Force: true)
```

**After:**
```bash
# User sets resource limits
kubectl set resources deployment authorino --limits=memory=512Mi

# Kuadrant reconciles
# ✅ User's 512Mi persists - we don't own the resources field
```

### User Scaling

**Before:**
```bash
# User scales deployment
kubectl scale deployment authorino --replicas=3

# Kuadrant reconciles
# ❌ Reverts to 1 (we always owned replicas)
```

**After (if replicas NOT set in CR):**
```bash
# User scales deployment
kubectl scale deployment authorino --replicas=3

# Kuadrant reconciles
# ✅ Stays at 3 - we don't own replicas field

# User creates HPA
kubectl autoscale deployment authorino --min=2 --max=10
# ✅ HPA works - we don't own replicas field
```

**After (if replicas IS set in CR):**
```bash
# User sets spec.authorino.replicas: 5 in Kuadrant CR

# User tries to scale
kubectl scale deployment authorino --replicas=3

# Kuadrant reconciles
# Reverts to 5 - we own replicas because it's set in CR
# This is expected - CR takes precedence
```

## Documentation for Users

Users should be informed:

### What Kuadrant Controls

**Always controlled (will revert changes):**
- Container image versions
- Container arguments (feature flags, auth settings)
- ServiceAccount references
- Essential selector labels

**Controlled IF set in CR:**
- Replicas (leave unset in CR to use kubectl scale/HPA)

### What Users Control

**Always user-controlled:**
- Resource limits/requests
- Sidecar containers
- Volumes and volume mounts
- Pod affinity, anti-affinity
- Tolerations, node selectors
- Annotations
- Additional environment variables

## Testing

### Test Scenarios

1. **Resource limits persist:**
   ```bash
   kubectl set resources deployment authorino --limits=memory=512Mi
   # Wait for reconciliation
   kubectl get deployment authorino -o yaml | grep -A2 resources
   # Should show 512Mi ✅
   ```

2. **User scaling works (when replicas not in CR):**
   ```bash
   # Ensure Kuadrant CR doesn't set spec.authorino.replicas
   kubectl scale deployment authorino --replicas=3
   # Wait for reconciliation
   kubectl get deployment authorino -o jsonpath='{.spec.replicas}'
   # Should show 3 ✅
   ```

3. **HPA works (when replicas not in CR):**
   ```bash
   kubectl autoscale deployment authorino --min=2 --max=10 --cpu-percent=70
   # Wait for HPA to scale
   kubectl get hpa authorino
   # Should show active HPA managing replicas ✅
   ```

4. **CR overrides user scaling (when replicas in CR):**
   ```bash
   # Set spec.authorino.replicas: 5 in Kuadrant CR
   kubectl scale deployment authorino --replicas=3
   # Wait for reconciliation
   kubectl get deployment authorino -o jsonpath='{.spec.replicas}'
   # Should show 5 (CR wins) ✅
   ```

## Chart Evolution and Limitations

### What Happens When Chart Adds New Fields?

With `Force: false`, there are limitations on chart evolution:

**Scenario:**
1. Chart v1.0 doesn't template `tolerations`
2. User customizes: `kubectl patch deployment authorino --patch '{"spec":{"template":{"spec":{"tolerations":[...]}}}}'`
3. User now owns `spec.template.spec.tolerations` field
4. Chart v2.0 adds tolerations to template
5. **On reconciliation:** Conflict! User owns the field, chart wants to own it

**Current Behavior:**
- Apply fails with conflict error
- **User's customization is preserved** ✅
- Error logged: "field ownership conflict detected - preserving user customization"
- Chart's new tolerations template is **ignored** for this deployment

**Why This Happens:**

Server-Side Apply ownership is binary per field:
- **Field in Apply payload** → We own it → We update it
- **Field NOT in Apply payload** → We don't touch it → Others can own it
- **Field we want but someone else owns** → Conflict error

`Force: false` means we **respect existing ownership** and fail on conflicts rather than overwriting.

### Mitigation Strategies

#### 1. Clear Field Ownership Contract (Current Approach)

Define three tiers of fields and commit to never crossing boundaries:

**Tier 1: Forever Operator Territory**
- We always template these
- Users should never customize (will be reverted)
- Example: `image`, `args`, `serviceAccountName`

**Tier 2: Opt-In Operator Territory**
- We template ONLY if set in CR
- Users can customize if not set in CR
- Example: `replicas`, `nodeSelector`, `tolerations` (if we add them)

**Tier 3: Forever User Territory**
- We will NEVER template these
- Users always control
- Example: `resources`, sidecars, `securityContext` details

**This contract limits chart evolution** - we can't start managing Tier 3 fields without breaking users.

#### 2. Handling Conflicts in Logs

When conflicts occur, logs provide guidance:

```
INFO field ownership conflict detected - preserving user customization
  kind=Deployment name=authorino
  message=This resource has fields owned by another manager (likely user customization).
          User's values will be preserved. Kuadrant only manages: image, args, serviceAccountName.
          See docs/helm-minimal-ownership.md for details.
```

Users see:
- ✅ Their customization is preserved (not overwritten)
- ✅ What fields Kuadrant manages
- ✅ Where to find details

#### 3. Future Options for Breaking Changes

If we need to start managing a previously-user field (major version upgrade):

**Option A: Force Update with Warning**
```go
// In major version upgrade path
if obj.GetAnnotations()["kuadrant.io/major-upgrade"] == "v2" {
    logger.Warn("forcing ownership for major upgrade - user customizations may be lost")
    // One-time Force: true apply
}
```

**Option B: Opt-Out Annotation**
```go
// Allow users to mark deployments as "hands off"
if obj.GetAnnotations()["kuadrant.io/managed"] == "false" {
    logger.Info("deployment marked as user-managed, skipping")
    return nil
}
```

**Option C: Migration Guide**
- Document the breaking change in release notes
- Provide migration script to remove user customizations
- Clear communication before upgrade

### Current Limitations

**Fields We Can Never Manage Without Breaking Changes:**
- Any field users have customized
- Once a field is "user territory", it stays that way (with `Force: false`)

**Acceptable Trade-Off:**
- ✅ User customizations are safe (CONNLINK-1022 fixed)
- ✅ Chart can still evolve Tier 1 fields
- ❌ Can't add new Tier 1 fields to manage
- ❌ Chart evolution is limited to additive changes only

**Why This Is Good:**
- Respects user autonomy
- Prevents silent data loss
- Clear contract between operator and users
- Conflicts are visible, not silent

### Recommended Practice

**For chart maintainers:**
1. Be conservative adding new templated fields
2. Make new fields Tier 2 (conditional via values)
3. Document which fields are user-controlled
4. Breaking changes require major version bump + migration guide

**For users:**
1. Understand Tier 1 fields (we always manage)
2. Customize Tier 3 fields freely
3. For Tier 2 fields, leave unset in CR to customize
4. Watch logs for conflict messages

## Migration from POC

POC deployments using `Force: true` may have taken ownership of user-modified fields. After upgrade:

1. Users should re-apply their customizations after upgrade
2. Future reconciliations will respect user changes
3. No data loss - just need to reapply customizations once

## References

- JIRA: [CONNLINK-1022](https://redhat.atlassian.net/browse/CONNLINK-1022)
- Kubernetes Docs: [Server-Side Apply](https://kubernetes.io/docs/reference/using-api/server-side-apply/)
- GEP: [SSA in Controllers](https://gateway-api.sigs.k8s.io/geps/gep-2080/)
