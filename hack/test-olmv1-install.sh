#!/usr/bin/env bash
# OLMv1 ClusterExtension Install Test
#
# Validates that the current Kuadrant operator build can be installed using
# OLMv1's ClusterExtension API. This does NOT test an upgrade from OLMv0 to
# OLMv1; it validates fresh OLMv1 install readiness per RFC 0019.
#
# Required environment variables:
#   KUADRANT_CATALOG_IMG - Catalog image URL for the operator to install.
#   KUADRANT_VERSION     - Expected bundle version, without v.
#
# Optional environment variables:
#   KUADRANT_NAMESPACE   - Namespace for verification (default: kuadrant-system).
#   OLMV1_VERSION        - OLMv1 operator-controller version (default: v1.2.0).
# Run on a disposable cluster after make install-cert-manager envoy-gateway-install.
# OLMv1 v1.2.0 ships both controllers in operator-controller.yaml.

set -euo pipefail

# Bound API requests as well as resource waits, including failure diagnostics.
kubectl() { command kubectl --request-timeout=30s "$@"; }

KUADRANT_NAMESPACE="${KUADRANT_NAMESPACE:-kuadrant-system}"
OLMV1_VERSION="${OLMV1_VERSION:-v1.2.0}"
: "${KUADRANT_VERSION:?Set the expected bundle version}"
KUADRANT_VERSION=${KUADRANT_VERSION#v}

dump_debug_state() {
  echo "OLMv1 failure diagnostics"
  echo "--- Pods ---"
  kubectl get pods -A --no-headers 2>/dev/null || true
  echo "--- ClusterExtensions ---"
  kubectl get clusterextension -A -o yaml 2>/dev/null || true
  echo "--- ClusterCatalogs ---"
  kubectl get clustercatalog -A -o yaml 2>/dev/null || true
  kubectl logs deployment/kuadrant-operator-controller-manager -n "$KUADRANT_NAMESPACE" --all-containers --tail=100 2>/dev/null || true
  kubectl get kuadrant -n "$KUADRANT_NAMESPACE" -o yaml 2>/dev/null || true
  echo "--- CRDs ---"
  kubectl get crd | grep -E 'kuadrant|authorino|limitador|dns|olm|catalog' 2>/dev/null || true
  echo "--- Events (last 50, olmv1-system) ---"
  kubectl get events -n olmv1-system --sort-by='.lastTimestamp' 2>/dev/null | tail -50 || true
  echo "--- Operator namespace events ---"
  kubectl get events -n "$KUADRANT_NAMESPACE" --sort-by='.lastTimestamp' 2>/dev/null | tail -50 || true
  echo "--- operator-controller logs (last 100 lines) ---"
  kubectl logs deployment/operator-controller-controller-manager -n olmv1-system --tail=100 2>/dev/null || true
  echo "--- catalogd logs (last 100 lines) ---"
  kubectl logs deployment/catalogd-controller-manager -n olmv1-system --tail=100 2>/dev/null || true
}

cleanup() {
  local ec=$?
  if [ "$ec" -ne 0 ]; then dump_debug_state; fi
  exit "$ec"
}
trap cleanup EXIT

if [[ -z "${KUADRANT_CATALOG_IMG:-}" ]]; then
  echo "KUADRANT_CATALOG_IMG must be set to the catalog image URL"
  exit 1
fi

kubectl apply -f "https://github.com/operator-framework/operator-controller/releases/download/${OLMV1_VERSION}/operator-controller.yaml"
kubectl wait --for=condition=Available deployment/catalogd-controller-manager deployment/operator-controller-controller-manager \
  -n olmv1-system --timeout=300s

kubectl apply -f - <<EOF
apiVersion: olm.operatorframework.io/v1
kind: ClusterCatalog
metadata:
  name: kuadrant-catalog
  labels:
    upgrade-test: kuadrant
spec:
  source:
    type: Image
    image:
      ref: ${KUADRANT_CATALOG_IMG}
      pollIntervalMinutes: 60
EOF

kubectl wait --for=condition=Serving clustercatalog/kuadrant-catalog --timeout=300s

kubectl create namespace "${KUADRANT_NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f -

kubectl apply -f - <<EOF
apiVersion: v1
kind: ServiceAccount
metadata:
  name: kuadrant-operator-installer
  namespace: ${KUADRANT_NAMESPACE}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: kuadrant-operator-installer-binding
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: cluster-admin
subjects:
  - kind: ServiceAccount
    name: kuadrant-operator-installer
    namespace: ${KUADRANT_NAMESPACE}
EOF

kubectl apply -f - <<EOF
apiVersion: olm.operatorframework.io/v1
kind: ClusterExtension
metadata:
  name: kuadrant-operator
spec:
  namespace: ${KUADRANT_NAMESPACE}
  serviceAccount:
    name: kuadrant-operator-installer
  source:
    sourceType: Catalog
    catalog:
      packageName: kuadrant-operator
      channels:
        - stable
      version: "${KUADRANT_VERSION}"
      selector:
        matchLabels:
          upgrade-test: kuadrant
EOF

kubectl wait --for=condition=Installed clusterextension/kuadrant-operator --timeout=600s

kubectl get clusterextension kuadrant-operator -o yaml

kubectl wait --for=condition=Established crd/kuadrants.kuadrant.io crd/authpolicies.kuadrant.io crd/ratelimitpolicies.kuadrant.io --timeout=300s
kubectl rollout status deployment/kuadrant-operator-controller-manager -n "$KUADRANT_NAMESPACE" --timeout=300s
kubectl apply -n "$KUADRANT_NAMESPACE" -f - <<EOF
apiVersion: kuadrant.io/v1beta1
kind: Kuadrant
metadata:
  name: kuadrant
spec: {}
EOF
kubectl wait --for=condition=Established crd/authorinos.operator.authorino.kuadrant.io crd/limitadors.limitador.kuadrant.io crd/dnsrecords.kuadrant.io --timeout=300s
kubectl wait --for=condition=Ready kuadrant/kuadrant -n "$KUADRANT_NAMESPACE" --timeout=300s
for deployment in $(kubectl get deployments -n "$KUADRANT_NAMESPACE" -o name); do
  kubectl rollout status "$deployment" -n "$KUADRANT_NAMESPACE" --timeout=300s
done
echo "OLMv1 installation passed"
