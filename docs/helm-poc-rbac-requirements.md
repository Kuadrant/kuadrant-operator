# RBAC Requirements for Helm POC

## Overview

When moving from operator-based deployment to Helm-based deployment, we need to ensure workloads have proper RBAC permissions.

## Authorino Workload RBAC

### Required ClusterRoles

Authorino requires two ClusterRoles that must be in the bundle:

#### 1. authorino-manager-role

**Purpose:** Core permissions for Authorino to function

**Permissions:**
```yaml
rules:
- apiGroups: [""]
  resources: [secrets]
  verbs: [get, list, watch]
- apiGroups: [authorino.kuadrant.io]
  resources: [authconfigs]
  verbs: [create, delete, get, list, patch, update, watch]
- apiGroups: [authorino.kuadrant.io]
  resources: [authconfigs/status]
  verbs: [get, patch, update]
- apiGroups: [coordination.k8s.io]
  resources: [leases]
  verbs: [create, get, list, update]
```

**Source:** `/workspace/authorino-operator/bundle/manifests/authorino-manager-role_rbac.authorization.k8s.io_v1_clusterrole.yaml`

#### 2. authorino-manager-k8s-auth-role

**Purpose:** Additional permissions for Kubernetes TokenReview authentication

**Permissions:**
```yaml
rules:
- apiGroups: [authentication.k8s.io]
  resources: [tokenreviews]
  verbs: [create]
- apiGroups: [authorization.k8s.io]
  resources: [subjectaccessreviews]
  verbs: [create]
```

**Source:** `/workspace/authorino-operator/bundle/manifests/authorino-manager-k8s-auth-role_rbac.authorization.k8s.io_v1_clusterrole.yaml`

### Required ClusterRoleBindings

The operator currently creates these dynamically based on the Authorino CR:

```go
// From authorino-operator/pkg/reconcilers/authorino_reconciler.go:119-147

// Always created
ClusterRoleBinding:
  name: authorino-<cr-name>
  roleRef: authorino-manager-role
  subjects:
  - kind: ServiceAccount
    name: authorino-<cr-name>
    namespace: <cr-namespace>

// Created only if spec.clusterWide: true
ClusterRoleBinding:
  name: authorino-k8s-auth-<cr-name>
  roleRef: authorino-manager-k8s-auth-role
  subjects:
  - kind: ServiceAccount
    name: authorino-<cr-name>
    namespace: <cr-namespace>
```

### Additional RBAC (Namespace-scoped)

If `spec.clusterWide: false`, the operator creates namespace-scoped RoleBindings instead.

Also creates a RoleBinding for leader election:
```yaml
RoleBinding:
  name: authorino-leader-election-<cr-name>
  namespace: <cr-namespace>
  roleRef: authorino-leader-election-role
  subjects:
  - kind: ServiceAccount
    name: authorino-<cr-name>
    namespace: <cr-namespace>
```

## Limitador Workload RBAC

### Required Permissions

✅ **None** - Limitador is a stateless rate limiting service that doesn't access the Kubernetes API.

The limitador-server binary only needs:
- Network access to serve RLS protocol
- Volume mounts for configuration (ConfigMap)
- Storage access (Redis, disk, memory)

No ServiceAccount permissions required.

## Implementation Strategy for POC

### Phase 1: Bundle the ClusterRoles

**Action:** Copy ClusterRole manifests from authorino-operator to kuadrant-operator bundle.

**Files to copy:**
1. `authorino-operator/bundle/manifests/authorino-manager-role_rbac.authorization.k8s.io_v1_clusterrole.yaml`
   → `kuadrant-operator/bundle/manifests/authorino-manager-role_rbac.authorization.k8s.io_v1_clusterrole.yaml`

2. `authorino-operator/bundle/manifests/authorino-manager-k8s-auth-role_rbac.authorization.k8s.io_v1_clusterrole.yaml`
   → `kuadrant-operator/bundle/manifests/authorino-manager-k8s-auth-role_rbac.authorization.k8s.io_v1_clusterrole.yaml`

**Why bundle?**
- ClusterRoles are cluster-scoped
- OLM installs bundle resources with proper ownership
- Ensures ClusterRoles exist before workloads start

### Phase 2: Create ClusterRoleBindings Dynamically

**Action:** Update `HelmAuthorinoReconciler` to create ClusterRoleBindings.

**Vendor these functions from authorino-operator:**
```go
// From pkg/resources/k8s_rbac.go
func GetAuthorinoClusterRoleBinding(crName, clusterRoleBindingNameSuffix, clusterRoleName string, serviceAccount *k8score.ServiceAccount, labels map[string]string) *k8srbac.ClusterRoleBinding

// From pkg/resources/k8s_rbac.go
func GetAuthorinoServiceAccount(namespace, crName string, labels map[string]string) *k8score.ServiceAccount
```

**Reconciler changes:**
```go
func (r *HelmAuthorinoReconciler) Reconcile(...) error {
    // ... existing helm rendering ...
    
    // Create ServiceAccount
    sa := GetAuthorinoServiceAccount(authorinoObj.Namespace, authorinoObj.Name, labels)
    r.Client.Apply(ctx, sa, metav1.ApplyOptions{FieldManager: FieldManagerName})
    
    // Create ClusterRoleBinding for manager role (always)
    crb := GetAuthorinoClusterRoleBinding(authorinoObj.Name, "", "authorino-manager-role", sa, labels)
    r.Client.Apply(ctx, crb, metav1.ApplyOptions{FieldManager: FieldManagerName})
    
    // Create ClusterRoleBinding for k8s auth (if clusterWide)
    if authorinoObj.Spec.ClusterWide {
        crbAuth := GetAuthorinoClusterRoleBinding(authorinoObj.Name, "k8s-auth", "authorino-manager-k8s-auth-role", sa, labels)
        r.Client.Apply(ctx, crbAuth, metav1.ApplyOptions{FieldManager: FieldManagerName})
    }
    
    // ... apply helm-rendered resources ...
}
```

**Why dynamic bindings?**
- ClusterRoleBindings reference the specific ServiceAccount namespace
- Allows multiple Authorino instances (different namespaces)
- Matches current operator behavior
- Cleanup via ownerReferences when Authorino CR deleted

### Alternative: Helm Charts with RBAC

**Not recommended for POC** because:
- ClusterRoles in Helm charts are cluster-scoped (awkward)
- Bindings need namespace interpolation
- Less flexible than dynamic creation
- Deviates from operator pattern

**Could work for production** with:
```yaml
# charts/authorino/templates/clusterrolebinding.yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: authorino-{{ .Values.name }}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: authorino-manager-role
subjects:
- kind: ServiceAccount
  name: {{ .Values.serviceAccountName }}
  namespace: {{ .Release.Namespace }}
```

## Verification

### Test Authorino RBAC

```bash
# Deploy Authorino
kubectl apply -f - <<EOF
apiVersion: operator.authorino.kuadrant.io/v1beta1
kind: Authorino
metadata:
  name: authorino
  namespace: kuadrant-system
spec:
  clusterWide: true
EOF

# Verify ClusterRoleBindings created
kubectl get clusterrolebinding | grep authorino
# Should see:
#   authorino-authorino
#   authorino-k8s-auth-authorino

# Verify ServiceAccount has permissions
kubectl auth can-i get secrets \
  --as=system:serviceaccount:kuadrant-system:authorino-authorino
# Should output: yes

kubectl auth can-i list authconfigs \
  --as=system:serviceaccount:kuadrant-system:authorino-authorino
# Should output: yes

# Check pod logs for permission errors
kubectl logs -n kuadrant-system deployment/authorino
# Should NOT see "forbidden" or "unauthorized" errors
```

## References

- Authorino reconciler RBAC logic: `/workspace/authorino-operator/pkg/reconcilers/authorino_reconciler.go:117-147`
- Authorino RBAC factories: `/workspace/authorino-operator/pkg/resources/k8s_rbac.go`
- Authorino ClusterRole manifests: `/workspace/authorino-operator/bundle/manifests/*role*.yaml`
