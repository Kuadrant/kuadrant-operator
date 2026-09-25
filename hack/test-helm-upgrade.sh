#!/usr/bin/env bash

# Test Helm upgrade from a released version to the current branch.
#
# Installs kuadrant via Helm from the published chart repo, then builds the
# current branch as an upgrade and verifies the transition.
#
# Can be run locally or as a CI job on main/PR updates for continuous upgrade testing.
#
# Prerequisites:
#   - A cluster with cert-manager, metallb, and Istio installed
#   - Push access to a container registry (for the upgrade operator image)
#   - helm and yq in ./bin/ (run: make helm yq)
#
# Usage:
#   ./hack/test-helm-upgrade.sh <push-registry> [from-version]
#
#   push-registry:  Where to push the upgrade operator image.
#                   Not required when UPGRADE_OPERATOR_IMG is set.
#                   Never use quay.io/kuadrant — that's the production registry.
#   from-version:   Published chart version to upgrade FROM (default: 1.5.3)
#
# Env vars:
#   UPGRADE_OPERATOR_IMG=<image>  Skip building the operator; use this pre-built
#                                 image in the upgrade chart instead.
#   WAIT_BEFORE_UPGRADE=true      Pause for Enter right before the upgrade is
#                                 triggered — giving you a chance to poke around
#                                 the pre-upgrade cluster state.
#                                 Default: off (unattended/CI runs straight through).
#   NAMESPACE=<ns>                Namespace (default: kuadrant-system)
#
# Examples:
#   # Upgrade from default release to current branch
#   ./hack/test-helm-upgrade.sh quay.io/mnairn
#
#   # Upgrade from a specific release
#   ./hack/test-helm-upgrade.sh quay.io/mnairn 1.4.7
#
#   # Upgrade using a pre-built operator image (skips docker build)
#   UPGRADE_OPERATOR_IMG=quay.io/kuadrant/kuadrant-operator:latest \
#     ./hack/test-helm-upgrade.sh quay.io/mnairn

set -euo pipefail

PUSH_REGISTRY="${1:-}"
FROM_VERSION="${2:-1.5.3}"
NAMESPACE="${NAMESPACE:-kuadrant-system}"
IMAGE_TAG="${IMAGE_TAG:-upgrade-test-$(whoami)}"
QUAY_IMAGE_EXPIRY="${QUAY_IMAGE_EXPIRY:-3d}"
UPGRADE_OPERATOR_IMG="${UPGRADE_OPERATOR_IMG:-}"
OPERATOR_IMG="${UPGRADE_OPERATOR_IMG:-${PUSH_REGISTRY}/kuadrant-operator:${IMAGE_TAG}}"
HELM="./bin/helm"
YQ="./bin/yq"

if [ -z "${UPGRADE_OPERATOR_IMG}" ] && [ -z "${PUSH_REGISTRY}" ]; then
    echo "Error: provide push-registry as first argument, or set UPGRADE_OPERATOR_IMG to skip building"
    exit 1
fi

echo "============================================"
echo "Helm Upgrade Test"
echo "============================================"
echo "From version:  ${FROM_VERSION}"
echo "Upgrade image: ${OPERATOR_IMG}"
echo "Image expiry:  ${QUAY_IMAGE_EXPIRY}"
echo "Namespace:     ${NAMESPACE}"
echo "============================================"
echo ""

# ── Phase 1: Install the "from" version ──────────────────────────────

echo "=== Phase 1: Install released version ${FROM_VERSION} via Helm ==="

make helm-add-kuadrant-repo

kubectl create namespace "${NAMESPACE}" 2>/dev/null || true

${HELM} install kuadrant kuadrant/kuadrant-operator \
    --wait \
    --timeout 3m0s \
    --version "${FROM_VERSION}" \
    --namespace "${NAMESPACE}"

echo ""
echo "=== Deploying Kuadrant CR ==="
cat <<EOF | kubectl apply -f -
apiVersion: kuadrant.io/v1beta1
kind: Kuadrant
metadata:
  name: kuadrant
  namespace: ${NAMESPACE}
spec:
  components:
    developerPortal:
      enabled: true
EOF

echo "Waiting for Kuadrant to be ready..."
kubectl wait --timeout=300s --for=condition=Ready kuadrant kuadrant -n "${NAMESPACE}"
echo "Initial install complete ✅"

echo ""
echo "=== Waiting for deployments to be ready ==="
kubectl -n "${NAMESPACE}" wait --timeout=300s --for=condition=Available deployments --all

echo ""
echo "=== Capturing baseline state (before upgrade) ==="
BASELINE_DIR="$(pwd)/tmp/helm-upgrade-baseline"
rm -rf "${BASELINE_DIR}"
mkdir -p "${BASELINE_DIR}"

kubectl get deployment -n "${NAMESPACE}" -o yaml > "${BASELINE_DIR}/deployments.yaml" 2>/dev/null || true
kubectl get serviceaccount -n "${NAMESPACE}" -o yaml > "${BASELINE_DIR}/serviceaccounts.yaml" 2>/dev/null || true
kubectl get crd -o yaml | grep -A 5 'kuadrant\|limitador\|authorino\|mcp' > "${BASELINE_DIR}/crds.yaml" 2>/dev/null || true
kubectl get configmap -n "${NAMESPACE}" -o yaml > "${BASELINE_DIR}/configmaps.yaml" 2>/dev/null || true
kubectl get service -n "${NAMESPACE}" -o yaml > "${BASELINE_DIR}/services.yaml" 2>/dev/null || true

echo "Deployments:"
kubectl get deployment -n "${NAMESPACE}" --no-headers
echo "Component CRDs:"
kubectl get crd | grep -E 'dnsrecord|dnshealthcheck|authconfig|limitador|mcpserver' || echo "  (none)"

# ── Phase 2: Build upgrade images ────────────────────────────────────

echo ""
if [ -n "${UPGRADE_OPERATOR_IMG}" ]; then
    echo "=== Phase 2: Using pre-built operator ${UPGRADE_OPERATOR_IMG} ==="
    OPERATOR_IMG="${UPGRADE_OPERATOR_IMG}"
else
    echo "=== Phase 2: Build upgrade operator from current branch ==="
    make docker-build IMG="${OPERATOR_IMG}" QUAY_IMAGE_EXPIRY="${QUAY_IMAGE_EXPIRY}"
    make docker-push IMG="${OPERATOR_IMG}"
fi

echo ""
echo "=== Building Helm chart ==="
make helm-dependency-build
make helm-build IMG="${OPERATOR_IMG}"

# ── Phase 3: Trigger upgrade ─────────────────────────────────────────

if [ "${WAIT_BEFORE_UPGRADE:-false}" = "true" ]; then
    echo ""
    echo "=== Paused before triggering upgrade (WAIT_BEFORE_UPGRADE=true) ==="
    echo "The \"from\" version is installed. Inspect the cluster now if you want."
    read -rp "Press Enter to trigger the upgrade (Ctrl+C to abort)... " _
fi

echo ""
echo "=== Phase 3: Helm upgrade ==="
${HELM} upgrade kuadrant charts/kuadrant-operator \
    --wait \
    --timeout 5m0s \
    --namespace "${NAMESPACE}"

echo ""
echo "=== Phase 4: Wait for KuadrantControlPlane to be ready ==="
for i in $(seq 1 60); do
    KCP_READY=$(kubectl get kuadrantcontrolplane default -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || echo "")
    if [ "${KCP_READY}" = "True" ]; then
        break
    fi
    echo "  Waiting... (Ready=${KCP_READY:-pending})"
    sleep 5
done

# ── Phase 5: Verify ──────────────────────────────────────────────────

echo ""
echo "=== Phase 5: Verification ==="

PASS=true

# KuadrantControlPlane should exist and be ready
echo ""
echo "--- KuadrantControlPlane ---"
if kubectl get kuadrantcontrolplane default &>/dev/null; then
    KCP_READY=$(kubectl get kuadrantcontrolplane default -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || echo "Unknown")
    KCP_REASON=$(kubectl get kuadrantcontrolplane default -o jsonpath='{.status.conditions[?(@.type=="Ready")].reason}' 2>/dev/null || echo "Unknown")
    echo "  Ready=${KCP_READY}, Reason=${KCP_REASON}"
    if [ "${KCP_READY}" = "True" ]; then
        echo "PASS: KuadrantControlPlane is ready"
    else
        echo "FAIL: KuadrantControlPlane not ready"
        PASS=false
    fi
else
    echo "FAIL: KuadrantControlPlane missing"
    PASS=false
fi

# All deployments in the namespace should be available
echo ""
echo "--- Deployments ---"
if kubectl -n "${NAMESPACE}" wait --timeout=120s --for=condition=Available deployments --all 2>/dev/null; then
    echo "PASS: All deployments available"
else
    echo "FAIL: Some deployments not available"
    kubectl get deployment -n "${NAMESPACE}" --no-headers
    PASS=false
fi

# Core Kuadrant CRDs should exist
echo ""
echo "--- Core CRDs ---"
CRDS=(
    "authpolicies.kuadrant.io"
    "ratelimitpolicies.kuadrant.io"
    "dnspolicies.kuadrant.io"
    "tlspolicies.kuadrant.io"
)
for crd in "${CRDS[@]}"; do
    if kubectl get crd "${crd}" &>/dev/null; then
        echo "PASS: CRD ${crd} exists"
    else
        echo "FAIL: CRD ${crd} missing"
        PASS=false
    fi
done

# Helm release should report upgraded chart version
echo ""
echo "--- Helm release ---"
INSTALLED_VERSION=$(make helm-print-installed-chart-version 2>/dev/null || echo "unknown")
LOCAL_VERSION=$(${HELM} show chart charts/kuadrant-operator 2>/dev/null | grep -E "^version:" | awk '{print $2}' || echo "unknown")
echo "Installed chart version: ${INSTALLED_VERSION}"
echo "Local chart version:     ${LOCAL_VERSION}"
if [ "${INSTALLED_VERSION}" = "${LOCAL_VERSION}" ]; then
    echo "PASS: Helm release is at the upgraded version"
else
    echo "FAIL: Helm release version mismatch (installed=${INSTALLED_VERSION}, expected=${LOCAL_VERSION})"
    PASS=false
fi

echo ""
echo "=== Final state ==="
echo "Deployments:"
kubectl get deployment -n "${NAMESPACE}" --no-headers 2>/dev/null || echo "  (none)"
echo "KuadrantControlPlane:"
kubectl get kuadrantcontrolplane --no-headers 2>/dev/null || echo "  (none)"

echo ""
echo "=== KuadrantControlPlane Status ==="
kubectl get kcp/default -o json | jq -r '(.status.conditions[] | select(.type=="Ready") | if .status=="True" then "Status: Healthy" else "Status: Not healthy" end),"",(.status.components[] | "\(.name) (v\(.chartVersion)):\n  \([.images[].image] | join("\n  "))")' 2>/dev/null || echo "  (not available)"

# ── Resource diff (informational) ────────────────────────────────────

echo ""
echo "=== Resource changes (before → after upgrade) ==="
AFTER_DIR="$(pwd)/tmp/helm-upgrade-after"
rm -rf "${AFTER_DIR}"
mkdir -p "${AFTER_DIR}"

kubectl get deployment -n "${NAMESPACE}" -o yaml > "${AFTER_DIR}/deployments.yaml" 2>/dev/null || true
kubectl get serviceaccount -n "${NAMESPACE}" -o yaml > "${AFTER_DIR}/serviceaccounts.yaml" 2>/dev/null || true
kubectl get crd -o yaml | grep -A 5 'kuadrant\|limitador\|authorino\|mcp' > "${AFTER_DIR}/crds.yaml" 2>/dev/null || true
kubectl get configmap -n "${NAMESPACE}" -o yaml > "${AFTER_DIR}/configmaps.yaml" 2>/dev/null || true
kubectl get service -n "${NAMESPACE}" -o yaml > "${AFTER_DIR}/services.yaml" 2>/dev/null || true

CHANGED=""
for resource in deployments serviceaccounts crds configmaps services; do
    if ! diff -q "${BASELINE_DIR}/${resource}.yaml" "${AFTER_DIR}/${resource}.yaml" &>/dev/null; then
        CHANGED="${CHANGED} ${resource}"
    fi
done

if [ -n "${CHANGED}" ]; then
    echo "Resources changed:${CHANGED}"
    echo ""
    echo "To inspect changes:"
    for resource in ${CHANGED}; do
        echo "  diff ${BASELINE_DIR}/${resource}.yaml ${AFTER_DIR}/${resource}.yaml"
    done
else
    echo "No resource changes detected"
fi

echo ""
if [ "${PASS}" = true ]; then
    echo "=== ALL CHECKS PASSED ==="
else
    echo "=== SOME CHECKS FAILED ==="
    exit 1
fi
