#!/usr/bin/env bash

# Test OLM upgrade from current multi-operator installation to consolidated operator.
#
# Builds the consolidated bundle/catalog, then performs the documented migration:
# update catalog, remove child operator subscriptions/CSVs, verify upgrade completes.
#
# Prerequisites:
#   - A cluster with kuadrant installed via OLM (e.g. make local-setup-olm-latest)
#   - Push access to a container registry
#   - opm and yq in ./bin/ (run: make opm yq)
#
# Usage:
#   ./hack/test-olm-upgrade.sh <registry-org>
#   e.g. ./hack/test-olm-upgrade.sh quay.io/mnairn

set -euo pipefail

REGISTRY_ORG="${1:?Error: provide registry org e.g. quay.io/mnairn}"
NAMESPACE="${NAMESPACE:-kuadrant-system}"
CATALOG_IMG_CURRENT="${CATALOG_IMG_CURRENT:-quay.io/kuadrant/kuadrant-operator-catalog:latest}"
OPERATOR_IMG="${REGISTRY_ORG}/kuadrant-operator:olmv1-upgrade-test"
BUNDLE_IMG="${REGISTRY_ORG}/kuadrant-operator-bundle:olmv1-upgrade-test"
CATALOG_IMG="${REGISTRY_ORG}/kuadrant-operator-catalog:olmv1-upgrade-test"
OPM="./bin/opm"
YQ="./bin/yq"
CHANNELS="${CHANNELS:-preview}"

echo "=== Step 1: Build and push consolidated operator and bundle ==="
make docker-build IMG="${OPERATOR_IMG}"
make docker-push IMG="${OPERATOR_IMG}"
make bundle IMG="${OPERATOR_IMG}" VERSION=0.0.1 CHANNELS="${CHANNELS}"
make bundle-build BUNDLE_IMG="${BUNDLE_IMG}"
make docker-push IMG="${BUNDLE_IMG}"

echo "=== Step 2: Build and push upgrade catalog ==="
TMP_DIR="$(pwd)/tmp/olm-upgrade-test"
rm -rf "${TMP_DIR}"
mkdir -p "${TMP_DIR}/catalog-dir"

${OPM} render "${CATALOG_IMG_CURRENT}" --output=yaml > "${TMP_DIR}/catalog-dir/operator.yaml"
${OPM} render "${BUNDLE_IMG}" --output=yaml >> "${TMP_DIR}/catalog-dir/operator.yaml"
${YQ} -i 'select(.schema == "olm.channel" and .package == "kuadrant-operator").entries += [{"name": "kuadrant-operator.v0.0.1", "replaces": "kuadrant-operator.v0.0.0"}]' "${TMP_DIR}/catalog-dir/operator.yaml"
${OPM} validate "${TMP_DIR}/catalog-dir"
${OPM} generate dockerfile "${TMP_DIR}/catalog-dir"
docker build -t "${CATALOG_IMG}" -f "${TMP_DIR}/catalog-dir.Dockerfile" "${TMP_DIR}"
docker push "${CATALOG_IMG}"
rm -rf "${TMP_DIR}"

echo ""
echo "=== Baseline state ==="
kubectl get csv -n "${NAMESPACE}" --no-headers
kubectl get subscription -n "${NAMESPACE}" --no-headers

echo ""
echo "=== Step 3: Update catalog ==="
kubectl patch catalogsource kuadrant-operator-catalog -n "${NAMESPACE}" \
    --type=merge -p "{\"spec\":{\"image\":\"${CATALOG_IMG}\"}}"
sleep 10
kubectl -n "${NAMESPACE}" wait --timeout=120s --for=condition=Ready \
    pod -l olm.catalogSource=kuadrant-operator-catalog

echo ""
echo "=== Step 4: Migration - remove child operator subscriptions and CSVs ==="
kubectl get subscription -n "${NAMESPACE}" --no-headers | \
    grep -v "^kuadrant " | awk '{print $1}' | \
    xargs -r kubectl delete subscription -n "${NAMESPACE}"

kubectl get csv -n "${NAMESPACE}" --no-headers | \
    grep -v "^kuadrant-operator\." | awk '{print $1}' | \
    xargs -r kubectl delete csv -n "${NAMESPACE}"

echo ""
echo "=== Step 5: Wait for upgrade ==="
for i in $(seq 1 60); do
    CSV_STATUS=$(kubectl get csv "kuadrant-operator.v0.0.1" -n "${NAMESPACE}" -o jsonpath='{.status.phase}' 2>/dev/null || echo "NotFound")
    if [ "${CSV_STATUS}" = "Succeeded" ]; then
        break
    fi
    echo "  Waiting... (${CSV_STATUS})"
    sleep 5
done

if [ "${CSV_STATUS}" != "Succeeded" ]; then
    echo "FAIL: upgrade did not complete"
    kubectl get subscription kuadrant -n "${NAMESPACE}" -o jsonpath='{range .status.conditions[*]}{.type}{": "}{.message}{"\n"}{end}'
    exit 1
fi

echo ""
echo "=== Verification ==="
echo "CSVs:"
kubectl get csv -n "${NAMESPACE}" --no-headers
echo "CRDs:"
kubectl get crd | grep -E 'authconfig|authorino\.kuadrant|dnsrecord|dnshealthcheck|limitador\.|mcpgateway|mcpserver|mcpvirtual' || true
echo "Deployments:"
kubectl get deployment -n "${NAMESPACE}" --no-headers
echo ""
echo "=== PASS ==="
