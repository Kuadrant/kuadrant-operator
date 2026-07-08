# Helm POC Deployment Guide

This guide explains how to deploy the Helm-based POC for OLMv1 consolidation (Spike #183).

## What Changed

### 1. Operator Dependencies Removed

**Before (OLM v0):**
```yaml
# bundle/metadata/dependencies.yaml
dependencies:
  - authorino-operator  # ❌ Removed
  - limitador-operator  # ❌ Removed
  - dns-operator        # ✅ Kept
```

**After (OLMv1 POC):**
```yaml
# bundle/metadata/dependencies.yaml
dependencies:
  - dns-operator  # Only external dependency
```

### 2. CRDs Vendored

kuadrant-operator now owns:
- `Authorino` CRD (operator.authorino.kuadrant.io/v1beta1)
- `Limitador` CRD (limitador.kuadrant.io/v1alpha1)

**Source location:**
- `config/crd/bases/operator.authorino.kuadrant.io_authorinos.yaml`
- `config/crd/bases/limitador.kuadrant.io_limitadors.yaml`

**Bundle generation:**
```bash
# CRDs are copied to bundle via:
make manifests  # Ensures CRDs are up-to-date
make bundle     # Copies CRDs to bundle/manifests/
```

**Bundle location:**
- `bundle/manifests/operator.authorino.kuadrant.io_authorinos.yaml`
- `bundle/manifests/limitador.kuadrant.io_limitadors.yaml`

**CSV ownership:**
Declared in `bundle/manifests/kuadrant-operator.clusterserviceversion.yaml` with proper descriptions.

### 3. Helm Charts Added

Charts in `charts/`:
- `authorino/` - Deploys Authorino workload
- `limitador/` - Deploys Limitador workload

Reconcilers:
- `HelmAuthorinoReconciler` - Watches Authorino CRs, renders charts
- `HelmLimitadorReconciler` - Watches Limitador CRs, renders charts

## Local Development Setup

### Option 1: Kind Cluster with Dependencies

```bash
# Create Kind cluster
make kind-create-cluster

# Deploy dependencies (dns-operator + developer-portal only)
# NOTE: authorino-operator and limitador-operator removed from kustomization
make deploy-dependencies

# Deploy operator
make deploy IMG=quay.io/kuadrant/kuadrant-operator:dev
```

**What gets deployed:**
- ✅ dns-operator (still external dependency)
- ✅ developer-portal
- ❌ NO authorino-operator (workloads managed by Helm)
- ❌ NO limitador-operator (workloads managed by Helm)

### Option 2: Build and Test Locally

```bash
# Build operator with embedded charts
make docker-build IMG=quay.io/kuadrant/kuadrant-operator:helm-poc

# Load to Kind
kind load docker-image quay.io/kuadrant/kuadrant-operator:helm-poc

# Deploy
make deploy IMG=quay.io/kuadrant/kuadrant-operator:helm-poc
```

## Testing the POC

### 1. Create Authorino CR

```bash
kubectl apply -f - <<EOF
apiVersion: operator.authorino.kuadrant.io/v1beta1
kind: Authorino
metadata:
  name: authorino
  namespace: kuadrant-system
spec:
  replicas: 2
  image: quay.io/kuadrant/authorino:latest
EOF
```

**Expected behavior:**
- HelmAuthorinoReconciler triggers
- Renders `charts/authorino/` with values from spec
- Creates: Deployment, 2 Services, ServiceAccount
- ownerReferences set to Authorino CR

### 2. Verify Resources Created

```bash
# Check Deployment
kubectl get deployment authorino -n kuadrant-system

# Check replicas match CR
kubectl get deployment authorino -n kuadrant-system -o jsonpath='{.spec.replicas}'
# Should output: 2

# Check Services
kubectl get svc -n kuadrant-system -l app=authorino
# Should see: authorino-auth, authorino-oidc

# Check owner references
kubectl get deployment authorino -n kuadrant-system -o yaml | grep -A 5 ownerReferences
# Should reference Authorino CR
```

### 3. Test User Edits (Server-Side Apply)

```bash
# Edit Authorino CR
kubectl patch authorino authorino -n kuadrant-system --type=merge -p '{"spec":{"replicas":3}}'

# Verify Deployment updated
kubectl get deployment authorino -n kuadrant-system -o jsonpath='{.spec.replicas}'
# Should output: 3
```

### 4. Test Cleanup

```bash
# Delete Authorino CR
kubectl delete authorino authorino -n kuadrant-system

# Verify resources cleaned up (ownerReferences working)
kubectl get deployment,svc -n kuadrant-system -l app=authorino
# Should return: No resources found
```

### 5. Test Limitador (Same Pattern)

```bash
kubectl apply -f - <<EOF
apiVersion: limitador.kuadrant.io/v1alpha1
kind: Limitador
metadata:
  name: limitador
  namespace: kuadrant-system
spec:
  replicas: 2
EOF

# Verify
kubectl get deployment limitador -n kuadrant-system
kubectl get svc limitador -n kuadrant-system
```

## Architecture

```
User creates Authorino CR
    ↓
Controller watches Authorino objects in topology
    ↓
HelmAuthorinoReconciler triggered (event: Create/Update)
    ↓
GetAuthorinoFromTopology() → reads CR spec
    ↓
buildHelmValues() → {replicas: 2, image: {...}}
    ↓
Render charts/authorino/ with values
    ↓
Server-Side Apply:
  - Deployment (ownerRef → Authorino CR)
  - Service: authorino-auth
  - Service: authorino-oidc
  - ServiceAccount
```

## Configuration

Users configure via Authorino/Limitador CRs (same as today!):

```yaml
apiVersion: operator.authorino.kuadrant.io/v1beta1
kind: Authorino
spec:
  replicas: 3              # ← User sets
  image: custom:v1         # ← User sets
  logLevel: debug          # ← User sets
  # ... all current fields supported
```

Helm charts read from CR spec and render resources accordingly.

## Limitations (POC)

1. **Helm charts are minimal** - Not full feature parity with operators yet
2. **No resource cleanup strategy** - When charts remove resources (see `architecture/docs/design/olmv1-resource-cleanup-concern.md`)
3. **Chart paths hardcoded** - `charts/authorino`, `charts/limitador`
4. **No ClusterRole/RBAC** - Authorino/Limitador workload RBAC not included yet

## Next Steps

See `/workspace/spike-183-working-notes.md` for:
- Full implementation plan
- Tasks A-E breakdown
- Integration test scenarios
- Follow-up work

## Reverting Changes

To revert to operator-based deployment:

1. Restore `bundle/metadata/dependencies.yaml`:
   ```bash
   git checkout bundle/metadata/dependencies.yaml
   ```

2. Deploy with operators:
   ```bash
   kubectl apply -k config/dependencies  # Original kustomization
   ```

3. Use existing reconcilers (not Helm-based)

## References

- **Spike Issue:** https://github.com/Kuadrant/architecture/issues/183
- **Working Notes:** `/workspace/spike-183-working-notes.md`
- **Architecture Doc:** `architecture/docs/design/olmv1-resource-cleanup-concern.md`
