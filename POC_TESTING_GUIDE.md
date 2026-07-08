# POC Testing Guide - Helm-based Authorino/Limitador Deployment

## Quick Start

```bash
# 1. Build and deploy
make docker-build IMG=quay.io/kuadrant/kuadrant-operator:helm-poc
make local-setup IMG=quay.io/kuadrant/kuadrant-operator:helm-poc

# 2. Create Kuadrant CR (automatically creates Authorino + Limitador CRs)
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1beta1
kind: Kuadrant
metadata:
  name: kuadrant
  namespace: kuadrant-system
spec: {}
EOF

# 3. Wait for CRs to be created
kubectl wait --for=jsonpath='{.status.conditions[?(@.type=="Ready")].status}'=True kuadrant/kuadrant -n kuadrant-system --timeout=120s

# 4. Verify Authorino workload deployed via Helm
kubectl get deployment,svc,clusterrolebinding -n kuadrant-system | grep authorino

# 5. Verify Limitador workload deployed via Helm
kubectl get deployment,svc -n kuadrant-system | grep limitador
```

## Detailed Testing Steps

### Prerequisites

```bash
# Required tools
- Docker or Podman
- Kind (Kubernetes in Docker)
- kubectl
- make
- Go 1.22+ (if rebuilding)

# Verify
docker --version
kind --version
kubectl version --client
```

### Step 1: Build Operator Image

```bash
# From /workspace/kuadrant-operator

# Build operator image with Helm charts embedded
make docker-build IMG=quay.io/kuadrant/kuadrant-operator:helm-poc

# Verify image built
docker images | grep helm-poc
```

**What this does:**
- Compiles operator binary with Helm reconcilers
- Embeds `charts/authorino/` and `charts/limitador/` in image
- Tags as `helm-poc` for testing

### Step 2: Create Kind Cluster and Deploy

**Option A: Use make local-setup (recommended)**

```bash
# Creates cluster + deploys everything
make local-setup IMG=quay.io/kuadrant/kuadrant-operator:helm-poc
```

This will:
1. Create Kind cluster
2. Install Gateway API CRDs
3. Install Istio (default gateway provider)
4. Deploy dependencies (dns-operator, developer-portal)
5. Load operator image to Kind
6. Deploy operator

**Option B: Manual steps**

```bash
# Create Kind cluster
make kind-create-cluster

# Install Gateway API + Istio
make gateway-api-install
make istio-install

# Load operator image to Kind
kind load docker-image quay.io/kuadrant/kuadrant-operator:helm-poc

# Deploy dependencies (dns-operator + developer-portal only, NO operators!)
make deploy-dependencies

# Deploy operator
make deploy IMG=quay.io/kuadrant/kuadrant-operator:helm-poc
```

**Verify operator is running:**

```bash
kubectl get deployment -n kuadrant-system kuadrant-operator-controller-manager

# Check logs
kubectl logs -n kuadrant-system deployment/kuadrant-operator-controller-manager -f
```

### Step 3: Test End-to-End Flow

**Create Kuadrant CR (which creates Authorino + Limitador CRs):**

```bash
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1beta1
kind: Kuadrant
metadata:
  name: kuadrant
  namespace: kuadrant-system
spec: {}
EOF
```

**The flow:**
1. Kuadrant CR created
2. `AuthorinoReconciler` creates Authorino CR (owned by Kuadrant)
3. `LimitadorReconciler` creates Limitador CR (owned by Kuadrant)
4. `HelmAuthorinoReconciler` watches Authorino CR → renders Helm chart → deploys workload
5. `HelmLimitadorReconciler` watches Limitador CR → renders Helm chart → deploys workload

**Watch reconciliation:**

```bash
# Watch operator logs for the full flow
kubectl logs -n kuadrant-system deployment/kuadrant-operator-controller-manager -f | grep -i "kuadrant\|authorino\|limitador\|helm"

# In another terminal, watch CRs and resources being created
watch 'kubectl get kuadrant,authorino,limitador -n kuadrant-system; echo "---"; kubectl get deployment,svc -n kuadrant-system'
```

**Verify resources created:**

```bash
# 1. Deployment
kubectl get deployment authorino -n kuadrant-system
# Should show: 2/2 READY

# 2. Services (auth + oidc)
kubectl get svc -n kuadrant-system | grep authorino
# Should show: authorino-auth and authorino-oidc

# 3. ServiceAccount
kubectl get sa authorino -n kuadrant-system

# 4. ClusterRoleBindings (2 because clusterWide=true)
kubectl get clusterrolebinding | grep authorino
# Should show:
#   authorino (→ kuadrant-operator-authorino-manager-role)
#   authorino-k8s-auth (→ kuadrant-operator-authorino-manager-k8s-auth-role)

# 5. Check pod is running
kubectl get pods -n kuadrant-system | grep authorino
# Should show: authorino-xxxxx  1/1  Running

# 6. Check pod logs
kubectl logs -n kuadrant-system -l app.kubernetes.io/name=authorino
# Should NOT see permission errors
```

**Verify RBAC works:**

```bash
# Check ServiceAccount can read secrets
kubectl auth can-i get secrets \
  --as=system:serviceaccount:kuadrant-system:authorino \
  -n kuadrant-system
# Output: yes

# Check ServiceAccount can list authconfigs
kubectl auth can-i list authconfigs.authorino.kuadrant.io \
  --as=system:serviceaccount:kuadrant-system:authorino
# Output: yes

# Check ServiceAccount can create tokenreviews (if clusterWide)
kubectl auth can-i create tokenreviews.authentication.k8s.io \
  --as=system:serviceaccount:kuadrant-system:authorino
# Output: yes
```

**Check ownerReferences (for cleanup):**

```bash
kubectl get deployment authorino -n kuadrant-system -o yaml | grep -A 5 ownerReferences
# Should reference: kind: Authorino, name: authorino
```

### Step 4: Test Authorino Configuration

**Test with clusterWide=false:**

```bash
# Delete and recreate with clusterWide=false
kubectl delete authorino authorino -n kuadrant-system

# Wait for cleanup
kubectl get deployment,svc,clusterrolebinding -n kuadrant-system | grep authorino
# Should return nothing

# Create with clusterWide=false
kubectl apply -f - <<EOF
apiVersion: operator.authorino.kuadrant.io/v1beta1
kind: Authorino
metadata:
  name: authorino
  namespace: kuadrant-system
spec:
  replicas: 1
  clusterWide: false
EOF

# Verify only 1 ClusterRoleBinding (not 2)
kubectl get clusterrolebinding | grep authorino | wc -l
# Should output: 1

# Verify no k8s-auth binding
kubectl get clusterrolebinding authorino-k8s-auth 2>&1
# Should output: Error... not found
```

**Test replica scaling:**

```bash
# Update replicas
kubectl patch authorino authorino -n kuadrant-system --type=merge -p '{"spec":{"replicas":3}}'

# Verify deployment scaled
kubectl get deployment authorino -n kuadrant-system -o jsonpath='{.spec.replicas}'
# Output: 3

kubectl get pods -n kuadrant-system | grep authorino | wc -l
# Output: 3
```

### Step 5: Test Limitador Deployment

**Create Limitador CR:**

```bash
kubectl apply -f - <<EOF
apiVersion: limitador.kuadrant.io/v1alpha1
kind: Limitador
metadata:
  name: limitador
  namespace: kuadrant-system
spec:
  replicas: 2
  version: latest
EOF
```

**Verify resources created:**

```bash
# 1. Deployment
kubectl get deployment limitador -n kuadrant-system
# Should show: 2/2 READY

# 2. Service
kubectl get svc limitador -n kuadrant-system

# 3. ServiceAccount
kubectl get sa limitador -n kuadrant-system

# 4. ConfigMap (for limits)
kubectl get configmap -n kuadrant-system | grep limitador

# 5. Pod logs
kubectl logs -n kuadrant-system -l app=limitador
# Should show limitador-server starting
```

**Test storage types:**

```bash
# Delete
kubectl delete limitador limitador -n kuadrant-system

# Create with Redis storage (if you have Redis)
kubectl apply -f - <<EOF
apiVersion: limitador.kuadrant.io/v1alpha1
kind: Limitador
metadata:
  name: limitador
  namespace: kuadrant-system
spec:
  replicas: 1
  storage:
    redis:
      configSecretRef:
        name: redis-config
EOF

# Check deployment args include redis
kubectl get deployment limitador -n kuadrant-system -o yaml | grep args -A 10
# Should show redis storage args
```

### Step 6: Test Cleanup (ownerReferences)

**Delete Authorino CR:**

```bash
# Delete CR
kubectl delete authorino authorino -n kuadrant-system

# Verify all resources cleaned up
kubectl get deployment,svc,sa -n kuadrant-system | grep authorino
# Should return nothing

kubectl get clusterrolebinding | grep authorino
# Should return nothing
```

**Delete Limitador CR:**

```bash
kubectl delete limitador limitador -n kuadrant-system

kubectl get deployment,svc,sa -n kuadrant-system | grep limitador
# Should return nothing
```

### Step 7: Test Manual CR Creation (Advanced)

You can also create Authorino/Limitador CRs manually (bypassing Kuadrant CR):

```bash
# Delete Kuadrant CR if it exists
kubectl delete kuadrant kuadrant -n kuadrant-system 2>/dev/null || true

# Create Authorino CR directly
kubectl apply -f - <<EOF
apiVersion: operator.authorino.kuadrant.io/v1beta1
kind: Authorino
metadata:
  name: authorino
  namespace: kuadrant-system
spec:
  replicas: 2
  clusterWide: true
  listener:
    tls:
      enabled: false
  oidcServer:
    tls:
      enabled: false
EOF

# HelmAuthorinoReconciler should still deploy workload
kubectl wait --for=condition=available --timeout=120s deployment/authorino -n kuadrant-system

# Verify
kubectl get deployment,svc,clusterrolebinding -n kuadrant-system | grep authorino
```

**Note:** This bypasses the normal Kuadrant flow and is mainly useful for testing the Helm reconcilers in isolation.

### Step 8: End-to-End Integration Test

**Create a complete setup:**

```bash
# 1. Kuadrant CR (creates Authorino + Limitador CRs)
kubectl apply -f config/samples/kuadrant_v1beta1_kuadrant.yaml

# 2. Create Gateway
kubectl apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: test-gateway
  namespace: gateway-system
spec:
  gatewayClassName: istio
  listeners:
  - name: http
    port: 80
    protocol: HTTP
EOF

# 3. Create HTTPRoute
kubectl apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: test-route
  namespace: gateway-system
spec:
  parentRefs:
  - name: test-gateway
  hostnames:
  - "test.example.com"
  rules:
  - backendRefs:
    - name: test-app
      port: 8080
EOF

# 4. Apply AuthPolicy
kubectl apply -f config/samples/kuadrant_v1_authpolicy.yaml

# 5. Apply RateLimitPolicy
kubectl apply -f config/samples/kuadrant_v1_ratelimitpolicy.yaml

# 6. Verify everything running
kubectl get kuadrant,gateway,httproute,authpolicy,ratelimitpolicy --all-namespaces
kubectl get deployment,svc -n kuadrant-system
```

## Verification Checklist

- [ ] Operator deployment running
- [ ] Authorino CR creates Deployment (2 replicas)
- [ ] Authorino creates 2 Services (auth + oidc)
- [ ] Authorino creates ServiceAccount
- [ ] Authorino creates 2 ClusterRoleBindings when clusterWide=true
- [ ] Authorino creates 1 ClusterRoleBinding when clusterWide=false
- [ ] Authorino pod starts without permission errors
- [ ] Authorino ServiceAccount has correct RBAC permissions
- [ ] Limitador CR creates Deployment (2 replicas)
- [ ] Limitador creates Service
- [ ] Limitador creates ServiceAccount
- [ ] Limitador pod starts successfully
- [ ] Delete Authorino CR cleans up all resources
- [ ] Delete Limitador CR cleans up all resources
- [ ] Kuadrant CR still creates Authorino/Limitador CRs
- [ ] Helm reconcilers deploy workloads from those CRs

## Troubleshooting

### Operator pod not starting

```bash
# Check events
kubectl describe deployment -n kuadrant-system kuadrant-operator-controller-manager

# Check image loaded
kind get clusters
kind export logs --name <cluster-name>
```

### Authorino deployment not created

```bash
# Check operator logs
kubectl logs -n kuadrant-system deployment/kuadrant-operator-controller-manager | grep -i error

# Check Authorino CR status
kubectl get authorino authorino -n kuadrant-system -o yaml

# Check if reconciler is registered
kubectl logs -n kuadrant-system deployment/kuadrant-operator-controller-manager | grep "HelmAuthorinoReconciler"
```

### Permission errors in Authorino pod

```bash
# Check ClusterRoles exist
kubectl get clusterrole | grep authorino-manager

# Check ClusterRoleBindings exist
kubectl get clusterrolebinding | grep authorino

# Check what permissions ServiceAccount has
kubectl auth can-i --list --as=system:serviceaccount:kuadrant-system:authorino
```

### Resources not cleaned up after CR deletion

```bash
# Check ownerReferences on resources
kubectl get deployment authorino -n kuadrant-system -o yaml | grep -A 10 ownerReferences

# Check if CR still exists
kubectl get authorino -n kuadrant-system
```

## Cleanup

```bash
# Delete everything
kubectl delete kuadrant kuadrant -n kuadrant-system
kubectl delete authorino --all -n kuadrant-system
kubectl delete limitador --all -n kuadrant-system

# Or delete entire cluster
make kind-delete-cluster
```

## What Success Looks Like

✅ Authorino/Limitador workloads deployed via Helm (not operators)
✅ No authorino-operator or limitador-operator running
✅ CRDs owned by kuadrant-operator
✅ RBAC works correctly
✅ ownerReferences cleanup works
✅ User experience unchanged (same CRs work)

## Next Steps After POC Success

1. Performance testing (reconciliation speed)
2. Upgrade testing (existing CRs → new operator)
3. Full feature parity (all Authorino/Limitador CR fields)
4. OLM catalog testing
5. Documentation updates
6. Migration guide for existing users
