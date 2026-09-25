#!/usr/bin/env bash
# OLMv0 Upgrade Test
#
# Validates the OLM upgrade path from a previous Kuadrant release to the
# current build. Designed to be called from CI or run locally.
#
# Run against a disposable cluster with OLM, cert-manager and the Envoy Gateway
# installed (make install-metallb install-olm install-cert-manager envoy-gateway-install deploy-eg-gateway).
# Requires kubectl, jq, curl, docker, kind and make opm yq.
# KUADRANT_START_VERSION: previous release, without v (required).
# KUADRANT_UPGRADE_VERSION: target release, without v (required).
# KUADRANT_UPGRADE_CATALOG / KUADRANT_UPGRADE_OPERATOR: override target images.
# KUADRANT_NAMESPACE: operator namespace (default kuadrant-system).
# KIND_CLUSTER_NAME: cluster to load the temporary upgrade catalog into (default kuadrant-test).
# TIMEOUT: seconds per wait (default 600). Uses the existing toystore examples.

set -euo pipefail

# Bound API requests as well as resource waits, including failure diagnostics.
kubectl() { command kubectl --request-timeout=30s "$@"; }

cd "$(dirname "${BASH_SOURCE[0]}")/.."
: "${KUADRANT_START_VERSION:?Set the previous release version}"
: "${KUADRANT_UPGRADE_VERSION:?Set the target release version}"
KUADRANT_START_VERSION=${KUADRANT_START_VERSION#v}
KUADRANT_UPGRADE_VERSION=${KUADRANT_UPGRADE_VERSION#v}
[[ "$KUADRANT_START_VERSION" != "$KUADRANT_UPGRADE_VERSION" ]]
KUADRANT_NAMESPACE=${KUADRANT_NAMESPACE:-kuadrant-system}
TIMEOUT=${TIMEOUT:-600}
KIND_CLUSTER_NAME=${KIND_CLUSTER_NAME:-kuadrant-test}
START_CATALOG=$(make -s print-catalog-image VERSION="$KUADRANT_START_VERSION")
START_OPERATOR=$(make -s print-operator-image VERSION="$KUADRANT_START_VERSION")
KUADRANT_UPGRADE_CATALOG=${KUADRANT_UPGRADE_CATALOG:-$(make -s print-catalog-image VERSION="$KUADRANT_UPGRADE_VERSION")}
KUADRANT_UPGRADE_OPERATOR=${KUADRANT_UPGRADE_OPERATOR:-$(make -s print-operator-image VERSION="$KUADRANT_UPGRADE_VERSION")}
TARGET_CSV=kuadrant-operator.v${KUADRANT_UPGRADE_VERSION}
WORK_DIR=$(mktemp -d)
PROBE_PID=

# Unlike kubectl wait on its own, also tolerate resources not yet created by OLM.
wait_for() {
  local deadline=$((SECONDS + TIMEOUT))
  until "$@"; do
    if ((SECONDS >= deadline)); then
      echo "Timed out: $*" >&2
      return 1
    fi
    sleep 5
  done
}
k() { kubectl --request-timeout=30s "$@"; }

dump_debug_state() {
  echo "Upgrade failure diagnostics"
  echo "--- Pods ---"
  kubectl get pods -A --no-headers 2>/dev/null || true
  echo "--- Pods in ${KUADRANT_NAMESPACE} ---"
  kubectl get pods -n "${KUADRANT_NAMESPACE}" -o wide 2>/dev/null || true
  echo "--- CSVs ---"
  kubectl get csv -n "${KUADRANT_NAMESPACE}" -o wide 2>/dev/null || true
  echo "--- Subscriptions ---"
  kubectl get subscription -n "${KUADRANT_NAMESPACE}" -o yaml 2>/dev/null || true
  echo "--- InstallPlans ---"
  kubectl get installplan -n "${KUADRANT_NAMESPACE}" -o yaml 2>/dev/null || true
  echo "--- CatalogSources ---"
  kubectl get catalogsource -n "${KUADRANT_NAMESPACE}" -o yaml 2>/dev/null || true
  echo "--- Kuadrant CRDs ---"
  kubectl get crd | grep -E 'kuadrant|authorino|limitador|dns' 2>/dev/null || true
  echo "--- Kuadrant CR status ---"
  kubectl get kuadrant -n "${KUADRANT_NAMESPACE}" -o yaml 2>/dev/null || true
  echo "--- Events (last 50) ---"
  kubectl get events -n "${KUADRANT_NAMESPACE}" --sort-by='.lastTimestamp' 2>/dev/null | tail -50 || true
  echo "--- Controller logs (last 100 lines) ---"
  kubectl logs deployment/kuadrant-operator-controller-manager -n "${KUADRANT_NAMESPACE}" --tail=100 2>/dev/null || true
}

# Ensure debug dump on any failure
cleanup() {
  local ec=$?
  if [[ -n "$PROBE_PID" ]]; then
    kill "$PROBE_PID" 2>/dev/null || true
    wait "$PROBE_PID" 2>/dev/null || true
  fi
  if ((ec != 0)); then
    dump_debug_state
    cat "$WORK_DIR/traffic.log" 2>/dev/null || true
  fi
  rm -rf "$WORK_DIR"
  exit "$ec"
}
trap cleanup EXIT

# Release catalogs contain one bundle and no upgrade graph. Preserve the published
# target content and add the previous bundle plus an explicit replaces edge for
# this test only. Never change the release catalog or use bundle-upgrade.
make opm
bin/opm render "$KUADRANT_UPGRADE_CATALOG" -o json >"$WORK_DIR/current.json"
bin/opm render "$START_CATALOG" -o json >"$WORK_DIR/previous.json"
jq -s --arg old "kuadrant-operator.v${KUADRANT_START_VERSION}" --arg new "$TARGET_CSV" '
  [.[] | select(.schema == "olm.bundle" and .name == $old)] |
  if length == 1 then .[] else error("previous bundle missing or ambiguous") end
' "$WORK_DIR/previous.json" >"$WORK_DIR/old.json"
mkdir "$WORK_DIR/configs"
jq -s --arg old "kuadrant-operator.v${KUADRANT_START_VERSION}" --arg new "$TARGET_CSV" '
  if ([.[] | select(.schema == "olm.bundle" and .name == $new)] | length) != 1
  then error("target bundle missing or ambiguous") else . end |
  .[] | if .schema == "olm.channel" and .package == "kuadrant-operator" and .name == "stable"
  then .entries = [{name: $old}, {name: $new, replaces: $old}] else . end
' "$WORK_DIR/current.json" >"$WORK_DIR/configs/catalog.json"
cat "$WORK_DIR/old.json" >>"$WORK_DIR/configs/catalog.json"
bin/opm validate "$WORK_DIR/configs"
(cd "$WORK_DIR" && "$OLDPWD/bin/opm" generate dockerfile configs)
TEST_CATALOG="kuadrant-upgrade-catalog:test"
docker build -t "$TEST_CATALOG" -f "$WORK_DIR/configs.Dockerfile" "$WORK_DIR"
kind load docker-image "$TEST_CATALOG" --name "$KIND_CLUSTER_NAME"

echo "Installing $KUADRANT_START_VERSION, upgrading to $KUADRANT_UPGRADE_VERSION"
kubectl create namespace "${KUADRANT_NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f -

kubectl apply -f - <<EOF
---
apiVersion: operators.coreos.com/v1alpha2
kind: OperatorGroup
metadata:
  name: kuadrant-operator-group
  namespace: ${KUADRANT_NAMESPACE}
---
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: kuadrant-operator-catalog
  namespace: ${KUADRANT_NAMESPACE}
spec:
  sourceType: grpc
  image: ${START_CATALOG}
  displayName: Kuadrant Operators
  publisher: grpc

---
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: kuadrant-operator
  namespace: ${KUADRANT_NAMESPACE}
spec:
  installPlanApproval: Automatic
  startingCSV: kuadrant-operator.v${KUADRANT_START_VERSION}
  channel: stable
  name: kuadrant-operator
  source: kuadrant-operator-catalog
  sourceNamespace: ${KUADRANT_NAMESPACE}
EOF

wait_for k wait --for=jsonpath='{.status.state}'=AtLatestKnown \
  subscription/kuadrant-operator -n "${KUADRANT_NAMESPACE}" --timeout=10s

wait_for k wait --for=jsonpath='{.status.phase}'=Succeeded \
  "csv/kuadrant-operator.v${KUADRANT_START_VERSION}" \
  -n "${KUADRANT_NAMESPACE}" --timeout=10s

wait_for k wait --timeout=10s \
  --for=jsonpath="{.spec.template.spec.containers[0].image}=${START_OPERATOR}" \
  deployment/kuadrant-operator-controller-manager -n "${KUADRANT_NAMESPACE}"

kubectl -n "${KUADRANT_NAMESPACE}" apply -f - <<EOF
apiVersion: kuadrant.io/v1beta1
kind: Kuadrant
metadata:
  name: kuadrant
spec: {}
EOF

wait_for k wait --timeout=10s --for=condition=Ready \
  kuadrant/kuadrant -n "${KUADRANT_NAMESPACE}"

# Exercise existing workloads, authentication and rate limiting before and during
# the upgrade. Separate paths keep the availability probe out of the rate limit.
kubectl -n "$KUADRANT_NAMESPACE" apply -f examples/toystore/toystore.yaml -f examples/toystore/httproute.yaml -f examples/toystore/alice-api-key-secret.yaml
kubectl -n "$KUADRANT_NAMESPACE" apply -f - <<EOF
apiVersion: kuadrant.io/v1
kind: AuthPolicy
metadata:
  name: upgrade-auth
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: toystore
  rules:
    authentication:
      apikey:
        apiKey:
          selector:
            matchLabels:
              app: toystore
        credentials:
          authorizationHeader:
            prefix: APIKEY
---
apiVersion: kuadrant.io/v1
kind: RateLimitPolicy
metadata:
  name: upgrade-ratelimit
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: toystore
  limits:
    probe:
      when:
        - predicate: "'x-upgrade-probe' in request.headers && request.headers['x-upgrade-probe'] == 'limited'"
      rates:
        - limit: 1
          window: 1s
EOF
wait_for k rollout status deployment/toystore -n "$KUADRANT_NAMESPACE" --timeout=10s
wait_for k wait --for=condition=Enforced authpolicy/upgrade-auth ratelimitpolicy/upgrade-ratelimit -n "$KUADRANT_NAMESPACE" --timeout=10s
GATEWAY_ADDRESS=$(kubectl get gateway kuadrant-ingressgateway -n gateway-system -o jsonpath='{.status.addresses[0].value}')
: "${GATEWAY_ADDRESS:?Gateway has no address}"
probe() {
  local code limited=false
  code=$(curl -sS --max-time 5 -o /dev/null -w '%{http_code}' -H 'Host: api.toystore.com' -H 'Authorization: APIKEY ALICEKEYFORDEMO' "http://$GATEWAY_ADDRESS/toy") || return 1
  [[ "$code" == 200 ]] || {
    echo "Authenticated request: $code"
    return 1
  }
  code=$(curl -sS --max-time 5 -o /dev/null -w '%{http_code}' -H 'Host: api.toystore.com' "http://$GATEWAY_ADDRESS/toy") || return 1
  [[ "$code" == 401 ]] || {
    echo "Unauthenticated request: $code"
    return 1
  }
  for _ in {1..10}; do
    code=$(curl -sS --max-time 5 -o /dev/null -w '%{http_code}' -H 'Host: api.toystore.com' -H 'Authorization: APIKEY ALICEKEYFORDEMO' -H 'x-upgrade-probe: limited' "http://$GATEWAY_ADDRESS/toy") || return 1
    case "$code" in
    429)
      limited=true
      break
      ;;
    200) ;;
    *)
      echo "Rate limited request: $code"
      return 1
      ;;
    esac
  done
  [[ "$limited" == true ]] || {
    echo 'Rate limit was not enforced'
    return 1
  }
}
wait_for probe

# UID comparison detects deletion/recreation, not just names reappearing.
snapshot() {
  kubectl get crd -o json | jq -r '.items[] | select(.spec.group | test("(^|\\.)(kuadrant|authorino|limitador)\\.io$")) | [.metadata.name,.metadata.uid] | @tsv' | sort
  kubectl get kuadrant,authorino,authconfig,limitador,dnsrecord,authpolicy,ratelimitpolicy,httproute -n "$KUADRANT_NAMESPACE" -o json | jq -r '.items[] | [.kind,.metadata.name,.metadata.uid] | @tsv' | sort
  kubectl get deployment/toystore service/toystore secret/toystore-alice-apikey -n "$KUADRANT_NAMESPACE" -o json | jq -r '.items[] | [.kind,.metadata.name,.metadata.uid] | @tsv' | sort
}
snapshot >"$WORK_DIR/before"
(
  while [[ ! -f "$WORK_DIR/stop" ]]; do
    if ! probe; then
      touch "$WORK_DIR/traffic-failed"
      exit 1
    fi
    echo "$(date -u +%FT%TZ) traffic checks passed"
    sleep 1
  done
) >"$WORK_DIR/traffic.log" 2>&1 &
PROBE_PID=$!

kubectl apply -f - <<EOF
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: kuadrant-operator-catalog
  namespace: ${KUADRANT_NAMESPACE}
spec:
  sourceType: grpc
  image: ${TEST_CATALOG}
  grpcPodConfig:
    securityContextConfig: restricted
  displayName: Kuadrant Operators
  publisher: grpc

EOF

# Wait for the exact target, never the stale AtLatestKnown status.
wait_for k wait --for=jsonpath="{.status.installedCSV}=$TARGET_CSV" subscription/kuadrant-operator -n "$KUADRANT_NAMESPACE" --timeout=10s
wait_for k wait --for=jsonpath='{.status.phase}'=Succeeded "csv/$TARGET_CSV" -n "$KUADRANT_NAMESPACE" --timeout=10s
wait_for k wait --for=jsonpath="{.spec.template.spec.containers[0].image}=$KUADRANT_UPGRADE_OPERATOR" deployment/kuadrant-operator-controller-manager -n "$KUADRANT_NAMESPACE" --timeout=10s
# Rollout checks account for observedGeneration and updated replicas.
for deployment in $(kubectl get deployments -n "$KUADRANT_NAMESPACE" -o name); do
  wait_for k rollout status "$deployment" -n "$KUADRANT_NAMESPACE" --timeout=10s
done
wait_for k wait --for=condition=Ready kuadrant/kuadrant -n "$KUADRANT_NAMESPACE" --timeout=10s
wait_for k wait --for=condition=Enforced authpolicy/upgrade-auth ratelimitpolicy/upgrade-ratelimit -n "$KUADRANT_NAMESPACE" --timeout=10s
snapshot >"$WORK_DIR/after"
# New CRDs are allowed, but every original UID must still be present.
comm -23 <(sort "$WORK_DIR/before") <(sort "$WORK_DIR/after") >"$WORK_DIR/missing"
if [[ -s "$WORK_DIR/missing" ]]; then
  cat "$WORK_DIR/missing"
  exit 1
fi
probe
touch "$WORK_DIR/stop"
wait "$PROBE_PID"
PROBE_PID=
[[ ! -f "$WORK_DIR/traffic-failed" ]]
cat "$WORK_DIR/traffic.log"
echo "OLM upgrade and uninterrupted traffic checks passed"
