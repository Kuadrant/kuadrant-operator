#!/usr/bin/env bash
# Setup an Istio egress gateway environment for Kuadrant.
#
# Deploys an egress Gateway, ServiceEntry, DestinationRule, HTTPRoute for
# httpbin.org, a test-client pod, and a dev Vault instance with Kubernetes
# auth method configured for per-workload credential injection.
#
# Prerequisites:
#   - Kubernetes cluster with Kuadrant and Istio installed
#   - kubectl and helm configured to access the cluster
#
# Usage:
#   # From a cloned repo:
#   ./hack/setup-egress.sh
#
#   # Without cloning:
#   curl -sL https://raw.githubusercontent.com/Kuadrant/kuadrant-operator/refs/heads/main/hack/setup-egress.sh | bash
#
#   # Cleanup:
#   ./hack/setup-egress.sh cleanup
#   # or:
#   curl -sL https://raw.githubusercontent.com/Kuadrant/kuadrant-operator/refs/heads/main/hack/setup-egress.sh | bash -s cleanup

set -euo pipefail

EGRESS_NS="gateway-system"
KUADRANT_NS="kuadrant-system"
VAULT_NS="vault"
BASE_URL="https://raw.githubusercontent.com/Kuadrant/kuadrant-operator/refs/heads/main"

# Use local files if running from within the repo, otherwise fetch from GitHub
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" 2>/dev/null && pwd)"
REPO_DIR="$SCRIPT_DIR/.."
if [ -f "$REPO_DIR/config/dependencies/istio/egress-gateway/gateway.yaml" ]; then
    src() { echo "$REPO_DIR/$1"; }
else
    src() { echo "$BASE_URL/$1"; }
fi

info()  { echo "[INFO] $*"; }
error() { echo "[ERROR] $*" >&2; }

# ── Cleanup ──────────────────────────────────────────────────────────
if [ "${1:-}" = "cleanup" ]; then
    info "Cleaning up egress gateway resources..."
    kubectl delete httproute httpbin-external -n "$EGRESS_NS" --ignore-not-found
    kubectl delete destinationrule httpbin-external -n "$EGRESS_NS" --ignore-not-found
    kubectl delete serviceentry httpbin-external -n "$EGRESS_NS" --ignore-not-found
    kubectl delete gateway kuadrant-egressgateway -n "$EGRESS_NS" --ignore-not-found
    # No Vault secrets to clean up — Vault K8s auth uses workload SA tokens directly
    kubectl delete pod test-client -n egress-test --ignore-not-found
    kubectl delete namespace egress-test --ignore-not-found
    helm uninstall vault -n "$VAULT_NS" 2>/dev/null || true
    kubectl delete namespace "$VAULT_NS" --ignore-not-found
    info "Cleanup complete."
    exit 0
fi

# ── Pre-flight checks ───────────────────────────────────────────────
info "Checking cluster connectivity..."
kubectl cluster-info --request-timeout=5s > /dev/null 2>&1 || { error "Cannot reach cluster"; exit 1; }

kubectl get crd kuadrants.kuadrant.io > /dev/null 2>&1 || { error "Kuadrant CRDs not found — is Kuadrant installed?"; exit 1; }
kubectl get crd gateways.gateway.networking.k8s.io > /dev/null 2>&1 || { error "Gateway API CRDs not found"; exit 1; }
command -v helm > /dev/null 2>&1 || { error "helm is required but not found"; exit 1; }

# ── Deploy egress gateway infrastructure ─────────────────────────────
info "Deploying egress gateway infrastructure..."

kubectl apply -n "$EGRESS_NS" \
    -f "$(src config/dependencies/istio/egress-gateway/gateway.yaml)" \
    -f "$(src config/dependencies/istio/egress-gateway/service-entry.yaml)" \
    -f "$(src config/dependencies/istio/egress-gateway/destination-rule.yaml)" \
    -f "$(src config/dependencies/istio/egress-gateway/httproute.yaml)"

kubectl apply -f "$(src examples/egress-gateway/test-client.yaml)"

info "Waiting for egress gateway to be ready..."
kubectl wait --timeout=5m -n "$EGRESS_NS" gateway/kuadrant-egressgateway --for=condition=Programmed

info "Waiting for test client to be ready..."
kubectl wait --timeout=2m -n egress-test pod/test-client --for=condition=Ready

# ── Deploy dev Vault instance ────────────────────────────────────────
info "Deploying Vault (dev mode)..."

helm repo add hashicorp https://helm.releases.hashicorp.com 2>/dev/null || true
helm repo update hashicorp 2>/dev/null

if helm status vault -n "$VAULT_NS" > /dev/null 2>&1; then
    info "Vault already deployed — skipping."
else
    helm install vault hashicorp/vault \
        --set "server.dev.enabled=true" \
        --set "server.dev.devRootToken=root" \
        -n "$VAULT_NS" --create-namespace --wait --timeout=5m
fi

info "Waiting for Vault to be ready..."
kubectl wait --timeout=2m -n "$VAULT_NS" pod/vault-0 --for=condition=Ready

# Store test credentials at per-identity Vault paths (secret/egress/<namespace>/<sa-name>)
info "Storing test credentials in Vault..."
kubectl exec vault-0 -n "$VAULT_NS" -- vault kv put secret/egress/egress-test/default \
    api_key=sk-test-openai-key-for-egress

# Configure Vault Kubernetes auth method
info "Configuring Vault Kubernetes auth..."
kubectl exec vault-0 -n "$VAULT_NS" -- vault auth enable kubernetes 2>/dev/null || true
kubectl exec vault-0 -n "$VAULT_NS" -- vault write auth/kubernetes/config \
    kubernetes_host="https://kubernetes.default.svc:443"

# Create Vault policy scoped to egress secrets
kubectl exec -i vault-0 -n "$VAULT_NS" -- vault policy write egress-read /dev/stdin <<'POLICY'
path "secret/data/egress/*" {
  capabilities = ["read"]
}
POLICY

# Create Vault role binding egress-test namespace to the policy
kubectl exec vault-0 -n "$VAULT_NS" -- vault write auth/kubernetes/role/egress-workload \
    bound_service_account_names=default \
    bound_service_account_namespaces=egress-test \
    policies=egress-read \
    ttl=1h

info "Vault configured with test credentials and Kubernetes auth."

# ── Verify connectivity ──────────────────────────────────────────────
info "Verifying egress connectivity..."
EGRESS_IP=$(kubectl get gtw kuadrant-egressgateway -n "$EGRESS_NS" -o jsonpath='{.status.addresses[0].value}')

# Wait for Envoy to be ready
sleep 5

RESULT=$(kubectl exec test-client -n egress-test -- curl -s --max-time 10 -o /dev/null -w "%{http_code}" \
    -H "Host: httpbin.org" "http://$EGRESS_IP/get" 2>/dev/null || echo "000")

if [ "$RESULT" = "200" ]; then
    info "Egress gateway is working. httpbin.org returned HTTP 200."
else
    error "Egress connectivity check returned HTTP $RESULT (expected 200)."
    error "The gateway may need a few more seconds. Try manually:"
    error "  kubectl exec test-client -n egress-test -- curl -s -H 'Host: httpbin.org' http://$EGRESS_IP/get"
    exit 1
fi

echo ""
info "Egress gateway environment is ready."
info ""
info "  Gateway:     kuadrant-egressgateway ($EGRESS_IP)"
info "  Test client: test-client (egress-test namespace)"
info "  Vault:       vault-0 (vault namespace, dev mode, K8s auth enabled)"
info "  Vault role:  egress-workload (bound to egress-test/default SA)"
info ""
info "  Test: kubectl exec test-client -n egress-test -- curl -s -H 'Host: httpbin.org' http://$EGRESS_IP/get"
info ""
info "  Cleanup: curl -sL https://raw.githubusercontent.com/Kuadrant/kuadrant-operator/refs/heads/main/hack/setup-egress.sh | bash -s cleanup"
