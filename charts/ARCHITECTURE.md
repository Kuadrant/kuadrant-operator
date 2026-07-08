# Helm Charts Architecture

## Design Principle: Static vs Templated

The charts follow a strict separation based on **OLM bundle requirements**:

### Cluster-Scoped Resources (Static)

**Location**: `config/crd/bases/` and `config/rbac/`  
**Reason**: OLM bundle requires static manifests with fixed names  
**Installed by**: OLM (operator installation) or `kubectl apply` (standalone Helm)

- ✅ CRDs (CustomResourceDefinitions)
- ✅ ClusterRoles

These resources:
- Have **fixed, hardcoded names** (e.g., `authorino-manager-role`)
- Cannot use Helm template variables
- Are copied to `crds/` directory in charts for Helm compatibility
- Are referenced by name in chart templates

### Namespace-Scoped Resources (Templated)

**Location**: `charts/*/templates/`  
**Reason**: Need dynamic values (namespace, release name, custom values)  
**Installed by**: Helm (chart rendering) or operator (Helm library)

- ✅ Deployments
- ✅ Services
- ✅ ServiceAccounts
- ✅ ConfigMaps
- ✅ ClusterRoleBindings (need namespace for ServiceAccount subjects)

These resources:
- Use Helm template variables (e.g., `{{ .Release.Namespace }}`)
- Can be customized via `values.yaml`
- Are rendered dynamically

## Why ClusterRoleBindings Are Templated

ClusterRoleBindings are **cluster-scoped** but need to reference **namespace-scoped** ServiceAccounts:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: {{ include "authorino.fullname" . }}  # Dynamic
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: authorino-manager-role  # Static! References ClusterRole from config/rbac/
subjects:
- kind: ServiceAccount
  name: {{ include "authorino.serviceAccountName" . }}  # Dynamic
  namespace: {{ .Release.Namespace }}  # Must be dynamic!
```

The `subjects[].namespace` field **must** be dynamic because:
1. Helm release can be installed in any namespace
2. Operator deploys to the namespace where the Authorino CR exists
3. ServiceAccount is namespace-scoped

## Directory Structure

```
kuadrant-operator/
├── config/
│   ├── crd/bases/                    # Static CRDs (source of truth)
│   │   ├── operator.authorino.kuadrant.io_authorinos.yaml
│   │   ├── authorino.kuadrant.io_authconfigs.yaml
│   │   └── limitador.kuadrant.io_limitadors.yaml
│   └── rbac/                         # Static RBAC (source of truth)
│       ├── authorino_manager_role.yaml
│       ├── authorino_manager_k8s_auth_role.yaml
│       └── role.yaml                 # Operator's own ClusterRole
└── charts/
    ├── authorino/
    │   ├── crds/                     # Copies of static CRDs (for Helm)
    │   │   ├── operator.authorino.kuadrant.io_authorinos.yaml
    │   │   └── authorino.kuadrant.io_authconfigs.yaml
    │   └── templates/                # Templated resources
    │       ├── clusterrolebinding.yaml  # References static ClusterRoles
    │       ├── deployment.yaml
    │       ├── service.yaml
    │       └── serviceaccount.yaml
    └── limitador/
        ├── crds/                     # Copy of static CRD
        │   └── limitador.kuadrant.io_limitadors.yaml
        └── templates/
            ├── deployment.yaml
            ├── service.yaml
            └── serviceaccount.yaml
```

## Installation Flows

### Flow 1: OLM Installation (Production)

```
OLM Bundle Install
    ↓
Installs static resources:
    - CRDs (from bundle/manifests/*.yaml)
    - ClusterRoles (from bundle/manifests/*_clusterrole.yaml)
    - Operator Deployment
    ↓
User creates Kuadrant CR
    ↓
Operator reconciles:
    - Creates Authorino/Limitador CRs
    - Helm reconcilers render charts
    - Server-side apply templated resources
    ↓
Chart creates ClusterRoleBindings that reference OLM-installed ClusterRoles
```

### Flow 2: Standalone Helm Install (Development/Testing)

```
Manual installation:
    kubectl apply -f config/rbac/authorino_manager_role.yaml
    kubectl apply -f config/rbac/authorino_manager_k8s_auth_role.yaml
    ↓
Helm install:
    helm install authorino charts/authorino/
    ↓
Helm installs:
    - CRDs (from crds/ directory - automatic)
    - Templated resources (from templates/)
    ↓
Chart creates ClusterRoleBindings that reference manually-installed ClusterRoles
```

### Flow 3: Local Development (make local-setup)

```
make local-setup
    ↓
Kustomize installs static resources:
    - CRDs (from config/crd/bases/)
    - ClusterRoles (from config/rbac/)
    - Operator Deployment
    ↓
Same as Flow 1 (operator reconciliation)
```

## Key Design Decisions

### ✅ Decision: CRDs Copied to charts/*/crds/

**Why**: Helm convention - CRDs in `crds/` are installed automatically on `helm install`

**Trade-off**: Duplication, but maintains single source of truth in `config/crd/bases/`

**Alternative considered**: Reference CRDs from `config/` - rejected because non-standard for Helm

### ✅ Decision: ClusterRoles Stay in config/rbac/ Only

**Why**: OLM bundle requires static manifests with fixed names

**Trade-off**: Standalone Helm install requires manual `kubectl apply` first

**Alternative considered**: Include static ClusterRoles in charts - rejected because can't parse Helm templates in OLM bundle

### ✅ Decision: ClusterRoleBindings Templated in Charts

**Why**: Must reference dynamic namespace for ServiceAccount subjects

**Trade-off**: ClusterRoleBindings managed differently than ClusterRoles

**Alternative considered**: Static ClusterRoleBindings with hardcoded namespace - rejected because breaks multi-namespace deployments

## Validation

Charts can be validated with:

```bash
# Syntax check
helm lint charts/authorino/
helm lint charts/limitador/

# Template rendering
helm template test charts/authorino/ --namespace test-ns

# Verify ClusterRoleBinding references
helm template test charts/authorino/ | grep "name: authorino-manager-role"
```

Expected output:
- ClusterRole names are static: `authorino-manager-role`, `authorino-manager-k8s-auth-role`
- ServiceAccount namespace is dynamic: `{{ .Release.Namespace }}`
- No ClusterRole definitions in templates (only in `config/rbac/`)
