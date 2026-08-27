#!/usr/bin/env bash
# Setup a mock AI API behind an Istio egress gateway for TRLP validation.
#
# Deploys an OpenAI-compatible LLM simulator as an "external" service
# accessible through the egress gateway, plus test workloads with distinct
# ServiceAccounts for per-workload and per-tier token rate limiting.
#
# Prerequisites:
#   - Kubernetes cluster with Kuadrant and Istio installed
#   - Egress gateway infrastructure already deployed (run setup-egress.sh first)
#
# Usage:
#   # From a cloned repo:
#   ./hack/setup-egress-ai-mock.sh
#
#   # Without cloning:
#   curl -sL https://raw.githubusercontent.com/Kuadrant/kuadrant-operator/refs/heads/main/hack/setup-egress-ai-mock.sh | bash
#
#   # Cleanup:
#   ./hack/setup-egress-ai-mock.sh cleanup
#   # or:
#   curl -sL https://raw.githubusercontent.com/Kuadrant/kuadrant-operator/refs/heads/main/hack/setup-egress-ai-mock.sh | bash -s cleanup

set -euo pipefail

EGRESS_NS="gateway-system"
AI_MOCK_NS="ai-mock"
EGRESS_TEST_NS="egress-test"
KUADRANT_SYSTEM_NS="kuadrant-system"
AI_MOCK_HOST="api.ai-mock.local"
BASE_URL="https://raw.githubusercontent.com/Kuadrant/kuadrant-operator/refs/heads/main"

# Use local files if running from within the repo, otherwise fetch from GitHub
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" 2>/dev/null && pwd)"
REPO_DIR="$SCRIPT_DIR/.."
if [ -f "$REPO_DIR/examples/egress-gateway/ai-mock.yaml" ]; then
    src() { echo "$REPO_DIR/$1"; }
else
    src() { echo "$BASE_URL/$1"; }
fi

info()  { echo "[INFO] $*"; }
error() { echo "[ERROR] $*" >&2; }

# ── Cleanup ──────────────────────────────────────────────────────────
if [ "${1:-}" = "cleanup" ]; then
    info "Cleaning up AI mock egress resources..."
    kubectl delete tokenratelimitpolicy -n "$EGRESS_NS" ai-token-limit ai-per-workload ai-per-tier --ignore-not-found
    kubectl delete authpolicy -n "$EGRESS_NS" workload-identity --ignore-not-found
    kubectl delete httproute ai-mock-external -n "$EGRESS_NS" --ignore-not-found
    kubectl delete serviceentry ai-mock-external -n "$EGRESS_NS" --ignore-not-found
    kubectl delete pod team-gold -n "$EGRESS_TEST_NS" --ignore-not-found
    kubectl delete sa team-gold -n "$EGRESS_TEST_NS" --ignore-not-found
    kubectl delete namespace "$AI_MOCK_NS" --ignore-not-found
    info "Cleanup complete. Base egress resources (gateway, test-client) are preserved."
    exit 0
fi

# ── Pre-flight checks ───────────────────────────────────────────────
info "Checking prerequisites..."
kubectl cluster-info --request-timeout=5s > /dev/null 2>&1 || { error "Cannot reach cluster"; exit 1; }

kubectl get gateway kuadrant-egressgateway -n "$EGRESS_NS" > /dev/null 2>&1 || {
    error "Egress gateway not found. Run setup-egress.sh first."
    exit 1
}

kubectl get pod test-client -n "$EGRESS_TEST_NS" > /dev/null 2>&1 || {
    error "test-client pod not found in $EGRESS_TEST_NS. Run setup-egress.sh first."
    exit 1
}

# ── Ensure Kuadrant CR exists ────────────────────────────────────────
KUADRANT_NS=$(kubectl get kuadrant -A -o jsonpath='{.items[0].metadata.namespace}' 2>/dev/null)
if [ -z "$KUADRANT_NS" ]; then
    info "Kuadrant CR not found. Creating in $KUADRANT_SYSTEM_NS..."
    kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1beta1
kind: Kuadrant
metadata:
  name: kuadrant
  namespace: $KUADRANT_SYSTEM_NS
EOF
    info "Waiting for Kuadrant to be ready..."
    kubectl wait --timeout=5m -n "$KUADRANT_SYSTEM_NS" kuadrant/kuadrant --for=condition=Ready
else
    info "Kuadrant CR found in $KUADRANT_NS."
fi

# ── Deploy mock AI API ───────────────────────────────────────────────
info "Deploying mock AI API (llm-d-inference-sim)..."
kubectl apply -f "$(src examples/egress-gateway/ai-mock.yaml)"

info "Waiting for mock AI API to be ready..."
kubectl wait --timeout=2m -n "$AI_MOCK_NS" deployment/llm-sim --for=condition=Available

# ── Get mock service ClusterIP ───────────────────────────────────────
AI_MOCK_IP=$(kubectl get svc llm-sim -n "$AI_MOCK_NS" -o jsonpath='{.spec.clusterIP}')
info "Mock AI API ClusterIP: $AI_MOCK_IP"

# ── Create ServiceEntry ─────────────────────────────────────────────
info "Creating ServiceEntry for $AI_MOCK_HOST..."
kubectl apply -f - <<EOF
apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: ai-mock-external
  namespace: $EGRESS_NS
spec:
  hosts:
    - $AI_MOCK_HOST
  ports:
    - number: 80
      name: http
      protocol: HTTP
  location: MESH_EXTERNAL
  resolution: STATIC
  endpoints:
    - address: $AI_MOCK_IP
EOF

# ── Create HTTPRoute ─────────────────────────────────────────────────
info "Creating HTTPRoute for AI mock through egress gateway..."
kubectl apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: ai-mock-external
  namespace: $EGRESS_NS
spec:
  parentRefs:
    - name: kuadrant-egressgateway
      namespace: $EGRESS_NS
  hostnames:
    - $AI_MOCK_HOST
  rules:
    - filters:
        - type: URLRewrite
          urlRewrite:
            hostname: $AI_MOCK_HOST
      backendRefs:
        - group: networking.istio.io
          kind: Hostname
          name: $AI_MOCK_HOST
          port: 80
EOF

# ── Deploy additional test workload ──────────────────────────────────
info "Creating additional test workload for per-tier testing..."

kubectl apply -f - <<EOF
apiVersion: v1
kind: ServiceAccount
metadata:
  name: team-gold
  namespace: $EGRESS_TEST_NS
---
apiVersion: v1
kind: Pod
metadata:
  name: team-gold
  namespace: $EGRESS_TEST_NS
spec:
  serviceAccountName: team-gold
  containers:
    - name: curl
      image: curlimages/curl:latest
      command: ["sleep", "infinity"]
  restartPolicy: Never
EOF

info "Waiting for team-gold pod to be ready..."
kubectl wait --timeout=2m -n "$EGRESS_TEST_NS" pod/team-gold --for=condition=Ready

# ── Verify connectivity ──────────────────────────────────────────────
info "Verifying AI mock connectivity through egress gateway..."

info "Waiting for gateway address..."
for attempt in $(seq 1 30); do
    EGRESS_IP=$(kubectl get gtw kuadrant-egressgateway -n "$EGRESS_NS" -o jsonpath='{.status.addresses[0].value}' 2>/dev/null)
    [ -n "$EGRESS_IP" ] && break
    sleep 2
done
if [ -z "$EGRESS_IP" ]; then
    error "Gateway address not available after 60s. Check: kubectl get gtw kuadrant-egressgateway -n $EGRESS_NS -o yaml"
    exit 1
fi

RESULT=$(kubectl exec test-client -n "$EGRESS_TEST_NS" -- curl -s --max-time 10 -o /dev/null -w "%{http_code}" \
    -H "Host: $AI_MOCK_HOST" "http://$EGRESS_IP/v1/models" 2>/dev/null || echo "000")

if [ "$RESULT" = "200" ]; then
    info "AI mock is reachable through egress gateway. /v1/models returned HTTP 200."
else
    error "Connectivity check returned HTTP $RESULT (expected 200)."
    error "The gateway may need a few more seconds. Try manually:"
    error "  kubectl exec test-client -n $EGRESS_TEST_NS -- curl -s -H 'Host: $AI_MOCK_HOST' http://$EGRESS_IP/v1/models"
    exit 1
fi

echo ""
info "AI mock egress environment is ready."
info ""
info "  Mock AI API:  llm-sim ($AI_MOCK_NS namespace, ClusterIP $AI_MOCK_IP)"
info "  Hostname:     $AI_MOCK_HOST"
info "  Gateway:      kuadrant-egressgateway ($EGRESS_IP)"
info "  Test clients: test-client (default SA), team-gold (team-gold SA)"
info ""
info "  Test:"
info "    kubectl exec test-client -n $EGRESS_TEST_NS -- curl -s -H 'Host: $AI_MOCK_HOST' http://$EGRESS_IP/v1/models"
info ""
info "  Cleanup:"
info "    curl -sL https://raw.githubusercontent.com/Kuadrant/kuadrant-operator/refs/heads/main/hack/setup-egress-ai-mock.sh | bash -s cleanup"
