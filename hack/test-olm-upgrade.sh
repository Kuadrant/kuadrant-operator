#!/usr/bin/env bash

# Test OLM upgrade from a released version to the current branch.
#
# Installs kuadrant via OLM from a specified catalog (defaults to latest release),
# then builds the current branch as an upgrade and verifies the transition.
#
# Can be run locally or as a CI job on main/PR updates for continuous upgrade testing.
#
# Prerequisites:
#   - A cluster with OLM installed (e.g. OpenShift, or Kind with operator-sdk olm install)
#   - Push access to a container registry (for the upgrade images)
#   - opm and yq in ./bin/ (run: make opm yq)
#
# Usage:
#   ./hack/test-olm-upgrade.sh <push-registry> [from-catalog] [from-channel]
#
#   push-registry:  Where to push test catalog image.
#                   Never use quay.io/kuadrant — that's the production registry.
#   from-catalog:   Published release catalog to upgrade FROM (default: quay.io/kuadrant/kuadrant-operator-catalog:v1.5.3)
#   from-channel:   OLM channel for the from version (default: preview)
#
# Env vars:
#   UPGRADE_BUNDLE_IMG=<image>  Skip building operator and bundle; use this
#                               pre-built bundle image to build the upgrade catalog.
#                               push-registry is still required to push the catalog.
#   WAIT_BEFORE_UPGRADE=true    Pause for Enter right before the upgrade is
#                               triggered, once the "from" version is installed,
#                               baselined, and the upgrade images/catalog are
#                               already built and pushed — giving you a chance
#                               to poke around the pre-upgrade cluster state.
#                               Default: off (unattended/CI runs straight through).
#
# Examples:
#   # Upgrade from latest release to current branch
#   ./hack/test-olm-upgrade.sh quay.io/mnairn
#
#   # Upgrade from a specific release
#   ./hack/test-olm-upgrade.sh quay.io/mnairn quay.io/kuadrant/kuadrant-operator-catalog:v1.5.3
#
#   # Upgrade using a pre-built bundle (skips operator+bundle build)
#   UPGRADE_BUNDLE_IMG=quay.io/kuadrant/kuadrant-operator-bundle:latest \
#     ./hack/test-olm-upgrade.sh quay.io/mnairn

set -euo pipefail

PUSH_REGISTRY="${1:?Error: provide registry to push test catalog image e.g. quay.io/mnairn}"
FROM_CATALOG="${2:-quay.io/kuadrant/kuadrant-operator-catalog:v1.5.3}"
FROM_CHANNEL="${3:-stable}"
NAMESPACE="${NAMESPACE:-kuadrant-system}"
IMAGE_TAG="${IMAGE_TAG:-upgrade-test-$(whoami)}"
QUAY_IMAGE_EXPIRY="${QUAY_IMAGE_EXPIRY:-3d}"
OPERATOR_IMG="${PUSH_REGISTRY}/kuadrant-operator:${IMAGE_TAG}"
BUNDLE_IMG="${PUSH_REGISTRY}/kuadrant-operator-bundle:${IMAGE_TAG}"
CATALOG_IMG="${PUSH_REGISTRY}/kuadrant-operator-catalog:${IMAGE_TAG}"
UPGRADE_BUNDLE_IMG="${UPGRADE_BUNDLE_IMG:-}"
OPM="./bin/opm"
YQ="./bin/yq"

echo "============================================"
echo "OLM Upgrade Test"
echo "============================================"
echo "From catalog:  ${FROM_CATALOG}"
echo "From channel:  ${FROM_CHANNEL}"
echo "Push registry: ${PUSH_REGISTRY}"
echo "Image tag:     ${IMAGE_TAG}"
echo "Image expiry:  ${QUAY_IMAGE_EXPIRY}"
echo "Namespace:     ${NAMESPACE}"
echo "============================================"
echo ""

# ── Phase 1: Install the "from" version ──────────────────────────────

echo "=== Phase 1: Install released version via OLM ==="

kubectl create namespace "${NAMESPACE}" 2>/dev/null || true

cat <<EOF | kubectl apply -f -
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: kuadrant-operator-catalog
  namespace: ${NAMESPACE}
spec:
  sourceType: grpc
  image: ${FROM_CATALOG}
  displayName: Kuadrant Operator (upgrade test)
  updateStrategy:
    registryPoll:
      interval: 45s
EOF

echo "Waiting for catalog pod (this may take a few minutes on CRC)..."
for attempt in $(seq 1 5); do
    sleep 15
    if kubectl -n "${NAMESPACE}" wait --timeout=60s --for=condition=Ready \
        pod -l olm.catalogSource=kuadrant-operator-catalog 2>/dev/null; then
        break
    fi
    echo "  Catalog pod not ready yet, retrying... (attempt ${attempt}/5)"
done

cat <<EOF | kubectl apply -f -
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: kuadrant-operator
  namespace: ${NAMESPACE}
spec: {}
---
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: kuadrant
  namespace: ${NAMESPACE}
spec:
  channel: ${FROM_CHANNEL}
  name: kuadrant-operator
  source: kuadrant-operator-catalog
  sourceNamespace: ${NAMESPACE}
  installPlanApproval: Automatic
EOF

echo "Waiting for initial install..."
CSV_PHASE=""
for i in $(seq 1 60); do
    CSV_NAME=$(kubectl get subscription kuadrant -n "${NAMESPACE}" -o jsonpath='{.status.currentCSV}' 2>/dev/null || echo "")
    if [ -n "${CSV_NAME}" ]; then
        CSV_PHASE=$(kubectl get csv "${CSV_NAME}" -n "${NAMESPACE}" -o jsonpath='{.status.phase}' 2>/dev/null || echo "")
        if [ "${CSV_PHASE}" = "Succeeded" ]; then
            break
        fi
    fi
    echo "  Waiting for install... (csv=${CSV_NAME:-pending}, phase=${CSV_PHASE:-pending})"
    sleep 10
done

if [ "${CSV_PHASE}" != "Succeeded" ]; then
    echo "FAIL: initial install did not complete"
    kubectl get subscription kuadrant -n "${NAMESPACE}" -o yaml 2>/dev/null || true
    exit 1
fi

echo "Initial install complete: ${CSV_NAME}"

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
BASELINE_DIR="$(pwd)/tmp/olm-upgrade-baseline"
rm -rf "${BASELINE_DIR}"
mkdir -p "${BASELINE_DIR}"

kubectl get csv -n "${NAMESPACE}" -o yaml > "${BASELINE_DIR}/csvs.yaml" 2>/dev/null || true
kubectl get subscription -n "${NAMESPACE}" -o yaml > "${BASELINE_DIR}/subscriptions.yaml" 2>/dev/null || true
kubectl get deployment -n "${NAMESPACE}" -o yaml > "${BASELINE_DIR}/deployments.yaml" 2>/dev/null || true
kubectl get clusterrole -o yaml -l app.kubernetes.io/managed-by=helm > "${BASELINE_DIR}/clusterroles.yaml" 2>/dev/null || true
kubectl get clusterrolebinding -o yaml -l app.kubernetes.io/managed-by=helm > "${BASELINE_DIR}/clusterrolebindings.yaml" 2>/dev/null || true
kubectl get serviceaccount -n "${NAMESPACE}" -o yaml > "${BASELINE_DIR}/serviceaccounts.yaml" 2>/dev/null || true
kubectl get crd -o yaml | grep -A 5 'kuadrant\|limitador\|authorino\|mcp' > "${BASELINE_DIR}/crds.yaml" 2>/dev/null || true
kubectl get configmap -n "${NAMESPACE}" -o yaml > "${BASELINE_DIR}/configmaps.yaml" 2>/dev/null || true
kubectl get service -n "${NAMESPACE}" -o yaml > "${BASELINE_DIR}/services.yaml" 2>/dev/null || true

echo "CSVs:"
kubectl get csv -n "${NAMESPACE}" --no-headers
echo "Subscriptions:"
kubectl get subscription -n "${NAMESPACE}" --no-headers
echo "Deployments:"
kubectl get deployment -n "${NAMESPACE}" --no-headers
echo "Component CRDs:"
kubectl get crd | grep -E 'dnsrecord|dnshealthcheck|authconfig|limitador|mcpserver' || echo "  (none)"

# ── Phase 2: Build and push upgrade images ───────────────────────────

echo ""
if [ -n "${UPGRADE_BUNDLE_IMG}" ]; then
    echo "=== Phase 2: Using pre-built bundle ${UPGRADE_BUNDLE_IMG} ==="
    BUNDLE_IMG="${UPGRADE_BUNDLE_IMG}"
else
    echo "=== Phase 2: Build upgrade images from current branch ==="
    make docker-build IMG="${OPERATOR_IMG}" QUAY_IMAGE_EXPIRY="${QUAY_IMAGE_EXPIRY}"
    make docker-push IMG="${OPERATOR_IMG}"
    make bundle IMG="${OPERATOR_IMG}" VERSION=99.0.0 CHANNELS="${FROM_CHANNEL}"
    make bundle-build BUNDLE_IMG="${BUNDLE_IMG}" QUAY_IMAGE_EXPIRY="${QUAY_IMAGE_EXPIRY}"
    make docker-push IMG="${BUNDLE_IMG}"
fi

echo ""
echo "=== Determining upgrade CSV name from bundle ==="
UPGRADE_CSV_NAME=$(${OPM} render "${BUNDLE_IMG}" --output=yaml | ${YQ} 'select(.schema == "olm.bundle") | .name')
echo "Upgrade CSV: ${UPGRADE_CSV_NAME}"

echo ""
echo "=== Phase 3: Build upgrade catalog ==="
TMP_DIR="$(pwd)/tmp/olm-upgrade-test"
rm -rf "${TMP_DIR}"
mkdir -p "${TMP_DIR}/catalog-dir"

${OPM} render "${FROM_CATALOG}" --output=yaml > "${TMP_DIR}/catalog-dir/operator.yaml"
${OPM} render "${BUNDLE_IMG}" --output=yaml >> "${TMP_DIR}/catalog-dir/operator.yaml"
${YQ} -i "select(.schema == \"olm.channel\" and .package == \"kuadrant-operator\").entries += [{\"name\": \"${UPGRADE_CSV_NAME}\", \"replaces\": \"${CSV_NAME}\"}]" "${TMP_DIR}/catalog-dir/operator.yaml"
${OPM} validate "${TMP_DIR}/catalog-dir"
${OPM} generate dockerfile "${TMP_DIR}/catalog-dir"
docker build -t "${CATALOG_IMG}" -f "${TMP_DIR}/catalog-dir.Dockerfile" \
    --label "quay.expires-after=${QUAY_IMAGE_EXPIRY}" "${TMP_DIR}"
docker push "${CATALOG_IMG}"
rm -rf "${TMP_DIR}"

# ── Phase 3: Trigger upgrade ─────────────────────────────────────────

if [ "${WAIT_BEFORE_UPGRADE:-false}" = "true" ]; then
    echo ""
    echo "=== Paused before triggering upgrade (WAIT_BEFORE_UPGRADE=true) ==="
    echo "The \"from\" version is installed and baselined, and the upgrade"
    echo "images/catalog (${CATALOG_IMG}) are already built and pushed."
    echo "Inspect the cluster now if you want; nothing further happens until you continue."
    read -rp "Press Enter to trigger the upgrade (Ctrl+C to abort)... " _
fi

echo ""
echo "=== Phase 4: Trigger upgrade ==="
kubectl patch catalogsource kuadrant-operator-catalog -n "${NAMESPACE}" \
    --type=merge -p "{\"spec\":{\"image\":\"${CATALOG_IMG}\"}}"

for attempt in $(seq 1 5); do
    sleep 15
    if kubectl -n "${NAMESPACE}" wait --timeout=60s --for=condition=Ready \
        pod -l olm.catalogSource=kuadrant-operator-catalog 2>/dev/null; then
        break
    fi
    echo "  Catalog pod not ready yet, retrying... (attempt ${attempt}/5)"
done

echo ""
echo "=== Phase 5: Wait for upgrade ==="
for i in $(seq 1 90); do
    CSV_STATUS=$(kubectl get csv "${UPGRADE_CSV_NAME}" -n "${NAMESPACE}" -o jsonpath='{.status.phase}' 2>/dev/null || echo "NotFound")
    if [ "${CSV_STATUS}" = "Succeeded" ]; then
        break
    fi
    echo "  Waiting... (${CSV_STATUS})"
    sleep 5
done

if [ "${CSV_STATUS}" != "Succeeded" ]; then
    echo "FAIL: upgrade did not complete within timeout"
    echo ""
    echo "Subscription status:"
    kubectl get subscription kuadrant -n "${NAMESPACE}" -o jsonpath='{range .status.conditions[*]}{.type}{": "}{.message}{"\n"}{end}' 2>/dev/null || true
    echo ""
    echo "Operator logs:"
    kubectl logs -n "${NAMESPACE}" -l control-plane=controller-manager --tail=50 2>/dev/null || true
    exit 1
fi

echo "Upgrade complete: ${UPGRADE_CSV_NAME}"

echo ""
echo "=== Waiting for KuadrantControlPlane to be ready ==="
for i in $(seq 1 60); do
    KCP_READY=$(kubectl get kuadrantcontrolplane default -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || echo "")
    if [ "${KCP_READY}" = "True" ]; then
        break
    fi
    echo "  Waiting... (Ready=${KCP_READY:-pending})"
    sleep 5
done

# ── Phase 6: Verify ──────────────────────────────────────────────────

echo ""
echo "=== Phase 6: Verification ==="

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

echo ""
echo "=== Final state ==="
echo "CSVs:"
kubectl get csv -n "${NAMESPACE}" --no-headers 2>/dev/null || echo "  (none)"
echo "Subscriptions:"
kubectl get subscription -n "${NAMESPACE}" --no-headers 2>/dev/null || echo "  (none)"
echo "Deployments:"
kubectl get deployment -n "${NAMESPACE}" --no-headers 2>/dev/null || echo "  (none)"
echo "KuadrantControlPlane:"
kubectl get kuadrantcontrolplane --no-headers 2>/dev/null || echo "  (none)"

# ── Resource diff (informational, does not affect pass/fail) ──────────

echo ""
echo "=== Resource changes (before → after upgrade) ==="
AFTER_DIR="$(pwd)/tmp/olm-upgrade-after"
rm -rf "${AFTER_DIR}"
mkdir -p "${AFTER_DIR}"

kubectl get csv -n "${NAMESPACE}" -o yaml > "${AFTER_DIR}/csvs.yaml" 2>/dev/null || true
kubectl get subscription -n "${NAMESPACE}" -o yaml > "${AFTER_DIR}/subscriptions.yaml" 2>/dev/null || true
kubectl get deployment -n "${NAMESPACE}" -o yaml > "${AFTER_DIR}/deployments.yaml" 2>/dev/null || true
kubectl get clusterrole -o yaml -l app.kubernetes.io/managed-by=helm > "${AFTER_DIR}/clusterroles.yaml" 2>/dev/null || true
kubectl get clusterrolebinding -o yaml -l app.kubernetes.io/managed-by=helm > "${AFTER_DIR}/clusterrolebindings.yaml" 2>/dev/null || true
kubectl get serviceaccount -n "${NAMESPACE}" -o yaml > "${AFTER_DIR}/serviceaccounts.yaml" 2>/dev/null || true
kubectl get crd -o yaml | grep -A 5 'kuadrant\|limitador\|authorino\|mcp' > "${AFTER_DIR}/crds.yaml" 2>/dev/null || true
kubectl get configmap -n "${NAMESPACE}" -o yaml > "${AFTER_DIR}/configmaps.yaml" 2>/dev/null || true
kubectl get service -n "${NAMESPACE}" -o yaml > "${AFTER_DIR}/services.yaml" 2>/dev/null || true

CHANGED=""
for resource in csvs subscriptions deployments clusterroles clusterrolebindings serviceaccounts crds configmaps services; do
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
    echo "No resource changes detected (unexpected — CSVs should have changed at minimum)"
fi

echo ""
if [ "${PASS}" = true ]; then
    echo "=== ALL CHECKS PASSED ==="
else
    echo "=== SOME CHECKS FAILED ==="
    exit 1
fi
