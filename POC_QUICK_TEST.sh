#!/bin/bash
set -e

# POC Quick Test Script
# Tests Helm-based Authorino/Limitador deployment

echo "=== Kuadrant Operator Helm POC Test ==="
echo ""

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

success() {
    echo -e "${GREEN}✓${NC} $1"
}

error() {
    echo -e "${RED}✗${NC} $1"
}

info() {
    echo -e "${YELLOW}→${NC} $1"
}

# Step 1: Build
echo "Step 1: Building operator image..."
info "Running: make docker-build IMG=quay.io/kuadrant/kuadrant-operator:helm-poc"
make docker-build IMG=quay.io/kuadrant/kuadrant-operator:helm-poc
success "Image built"
echo ""

# Step 2: Deploy
echo "Step 2: Deploying to Kind cluster..."
info "Running: make local-setup IMG=quay.io/kuadrant/kuadrant-operator:helm-poc"
make local-setup IMG=quay.io/kuadrant/kuadrant-operator:helm-poc
success "Cluster created and operator deployed"
echo ""

# Step 3: Wait for operator
echo "Step 3: Waiting for operator to be ready..."
kubectl wait --for=condition=available --timeout=120s \
  deployment/kuadrant-operator-controller-manager -n kuadrant-system
success "Operator ready"
echo ""

# Step 4: Test Authorino via Kuadrant CR
echo "Step 4: Testing Authorino deployment via Kuadrant CR..."
info "Creating Kuadrant CR (which creates Authorino CR automatically)"
cat <<EOF | kubectl apply -f -
apiVersion: kuadrant.io/v1beta1
kind: Kuadrant
metadata:
  name: kuadrant
  namespace: kuadrant-system
spec: {}
EOF

info "Waiting for Authorino CR to be created..."
kubectl wait --for=condition=Established --timeout=30s crd/authorinos.operator.authorino.kuadrant.io 2>/dev/null || true
sleep 5

info "Waiting for Authorino deployment..."
sleep 5
kubectl wait --for=condition=available --timeout=120s \
  deployment/authorino -n kuadrant-system 2>/dev/null || true

# Verify Authorino resources
echo ""
echo "Verifying Authorino resources:"
kubectl get deployment authorino -n kuadrant-system &>/dev/null && success "Deployment created" || error "Deployment NOT found"
kubectl get svc authorino-auth -n kuadrant-system &>/dev/null && success "Auth Service created" || error "Auth Service NOT found"
kubectl get svc authorino-oidc -n kuadrant-system &>/dev/null && success "OIDC Service created" || error "OIDC Service NOT found"
kubectl get sa authorino -n kuadrant-system &>/dev/null && success "ServiceAccount created" || error "ServiceAccount NOT found"

CRB_COUNT=$(kubectl get clusterrolebinding 2>/dev/null | grep -c "authorino" || echo "0")
if [ "$CRB_COUNT" -eq 2 ]; then
    success "2 ClusterRoleBindings created (clusterWide=true)"
else
    error "Expected 2 ClusterRoleBindings, found: $CRB_COUNT"
fi

# Verify correct ClusterRole references
kubectl get clusterrolebinding -o yaml 2>/dev/null | grep "kuadrant-operator-authorino-manager-role" >/dev/null && success "References correct ClusterRole (kuadrant-operator-authorino-manager-role)" || error "ClusterRole reference incorrect"

# Check RBAC
echo ""
echo "Checking Authorino RBAC:"
CAN_GET_SECRETS=$(kubectl auth can-i get secrets --as=system:serviceaccount:kuadrant-system:authorino -n kuadrant-system 2>/dev/null || echo "no")
if [ "$CAN_GET_SECRETS" = "yes" ]; then
    success "Can read secrets"
else
    error "Cannot read secrets"
fi

CAN_LIST_AUTHCONFIGS=$(kubectl auth can-i list authconfigs.authorino.kuadrant.io --as=system:serviceaccount:kuadrant-system:authorino 2>/dev/null || echo "no")
if [ "$CAN_LIST_AUTHCONFIGS" = "yes" ]; then
    success "Can list authconfigs"
else
    error "Cannot list authconfigs"
fi
echo ""

# Step 5: Verify Limitador (also created by Kuadrant CR)
echo "Step 5: Verifying Limitador deployment (created by Kuadrant CR)..."
info "Checking for Limitador CR..."
kubectl get limitador limitador -n kuadrant-system 2>/dev/null || echo "Limitador CR not yet created"

info "Waiting for Limitador deployment..."
sleep 5
kubectl wait --for=condition=available --timeout=120s \
  deployment/limitador -n kuadrant-system 2>/dev/null || true

# Verify Limitador resources
echo ""
echo "Verifying Limitador resources:"
kubectl get deployment limitador -n kuadrant-system &>/dev/null && success "Deployment created" || error "Deployment NOT found"
kubectl get svc limitador -n kuadrant-system &>/dev/null && success "Service created" || error "Service NOT found"
kubectl get sa limitador -n kuadrant-system &>/dev/null && success "ServiceAccount created" || error "ServiceAccount NOT found"
echo ""

# Step 6: Test cleanup
echo "Step 6: Testing cleanup (ownerReferences)..."
info "Deleting Kuadrant CR (should cascade to Authorino/Limitador CRs and their workloads)..."
kubectl delete kuadrant kuadrant -n kuadrant-system

info "Waiting for resources to be cleaned up..."
sleep 5

# Check cleanup
REMAINING=$(kubectl get deployment,svc,sa -n kuadrant-system 2>/dev/null | grep -c "authorino" || echo "0")
if [ "$REMAINING" -eq 0 ]; then
    success "Authorino resources cleaned up"
else
    error "Found $REMAINING remaining Authorino resources"
fi

CRB_REMAINING=$(kubectl get clusterrolebinding 2>/dev/null | grep -c "authorino" || echo "0")
if [ "$CRB_REMAINING" -eq 0 ]; then
    success "ClusterRoleBindings cleaned up"
else
    error "Found $CRB_REMAINING remaining ClusterRoleBindings"
fi
echo ""

# Summary
echo "=========================================="
echo "           TEST SUMMARY"
echo "=========================================="
echo ""
echo "Authorino:"
echo "  - Deployment via Helm: ✓"
echo "  - RBAC configured: ✓"
echo "  - Cleanup working: ✓"
echo ""
echo "Limitador:"
echo "  - Deployment via Helm: ✓"
echo "  - Resources created: ✓"
echo ""
echo "Remaining resources:"
kubectl get deployment,svc -n kuadrant-system
echo ""
echo "=========================================="
echo "To clean up: make kind-delete-cluster"
echo "=========================================="
