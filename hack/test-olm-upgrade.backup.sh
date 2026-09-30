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
#   push-registry:  Where to push test images (operator, bundle, catalog).
#                   Never use quay.io/kuadrant — that's the production registry.
#   from-catalog:   Published release catalog to upgrade FROM (default: quay.io/kuadrant/kuadrant-operator-catalog:latest)
#   from-channel:   OLM channel for the from version (default: preview)
#
# Env vars:
#   WAIT_BEFORE_UPGRADE=true   Pause for Enter right before the upgrade is
#                              triggered, once the "from" version is installed,
#                              baselined, and the upgrade images/catalog are
#                              already built and pushed — giving you a chance
#                              to poke around the pre-upgrade cluster state.
#                              Default: off (unattended/CI runs straight through).
#   TO_CHANNEL                 Channel to upgrade INTO, if different from
#                              from-channel (e.g. installed on "stable",
#                              upgrading via the "preview" dev/test channel).
#                              The new build is only added to this channel's
#                              graph, and the Subscription is switched to it
#                              when the upgrade is triggered. Default: same
#                              as from-channel (upgrade within one channel).
#                              Requires the CSV currently installed under
#                              from-channel to already be a real member of
#                              to-channel's history, or the upgrade graph
#                              opm validate checks won't be satisfiable.
#   INSTALL_MCP_GATEWAY=true  Also install mcp-gateway via ITS OWN OLM
#                              catalog into namespace mcp-system before the
#                              baseline is captured, using the install method
#                              documented at
#                              https://github.com/Kuadrant/mcp-gateway/blob/main/docs/guides/olm-install.md
#                              This gives migrateMCPGateway an actual
#                              pre-consolidation install to migrate away
#                              from, so Phase 6 can verify that cleanup for
#                              real instead of it being a no-op. Default: off.
#   MCP_GATEWAY_VERSION        mcp-gateway release tag to install when
#                              INSTALL_MCP_GATEWAY=true. Default: 0.8.0.
#
# Examples:
#   # Upgrade from latest release to current branch
#   ./hack/test-olm-upgrade.sh quay.io/mnairn
#
#   # Upgrade from a specific release
#   ./hack/test-olm-upgrade.sh quay.io/mnairn quay.io/kuadrant/kuadrant-operator-catalog:v1.5.3
#
#   # Upgrade from a specific channel
#   ./hack/test-olm-upgrade.sh quay.io/mnairn quay.io/kuadrant/kuadrant-operator-catalog:latest stable
#
#   # Installed on stable, upgrade via the preview (dev/test) channel
#   TO_CHANNEL=preview ./hack/test-olm-upgrade.sh quay.io/mnairn quay.io/kuadrant/kuadrant-operator-catalog:latest stable
#
#   # Also exercise the mcp-gateway OLM migration path
#   INSTALL_MCP_GATEWAY=true ./hack/test-olm-upgrade.sh quay.io/mnairn

set -euo pipefail

PUSH_REGISTRY="${1:?Error: provide registry to push test images e.g. quay.io/mnairn}"
FROM_CATALOG="${2:-quay.io/kuadrant/kuadrant-operator-catalog:latest}"
FROM_CHANNEL="${3:-preview}"
NAMESPACE="${NAMESPACE:-kuadrant-system}"
OPERATOR_IMG="${PUSH_REGISTRY}/kuadrant-operator:upgrade-test"
BUNDLE_IMG="${PUSH_REGISTRY}/kuadrant-operator-bundle:upgrade-test"
CATALOG_IMG="${PUSH_REGISTRY}/kuadrant-operator-catalog:upgrade-test"
OPM="./bin/opm"
YQ="./bin/yq"
TO_CHANNEL="${TO_CHANNEL:-${FROM_CHANNEL}}"
INSTALL_MCP_GATEWAY="${INSTALL_MCP_GATEWAY:-false}"
MCP_GATEWAY_VERSION="${MCP_GATEWAY_VERSION:-0.8.0}"
MCP_GATEWAY_NAMESPACE="mcp-system"
# Where OLM discovers CatalogSources cluster-wide, for any Subscription
# regardless of namespace. openshift-marketplace on OpenShift; the vanilla
# community OLM installer typically uses "olm" instead -- override if needed.
GLOBAL_CATALOG_NAMESPACE="${GLOBAL_CATALOG_NAMESPACE:-openshift-marketplace}"

echo "============================================"
echo "OLM Upgrade Test"
echo "============================================"
echo "From catalog:  ${FROM_CATALOG}"
echo "From channel:  ${FROM_CHANNEL}"
echo "To channel:    ${TO_CHANNEL}"
echo "Push registry: ${PUSH_REGISTRY}"
echo "Namespace:     ${NAMESPACE}"
if [ "${INSTALL_MCP_GATEWAY}" = "true" ]; then
    echo "mcp-gateway:   also installing v${MCP_GATEWAY_VERSION} via OLM in ${MCP_GATEWAY_NAMESPACE}"
fi
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
echo "=== Waiting for deployments to be ready ==="
kubectl -n "${NAMESPACE}" wait --timeout=300s --for=condition=Available deployments --all

if [ "${INSTALL_MCP_GATEWAY}" = "true" ]; then
    echo ""
    echo "=== Phase 1b: Install mcp-gateway via its own OLM catalog (optional) ==="
    # mcp-gateway.v${MCP_GATEWAY_VERSION}'s bundle declares an OLM dependency
    # on kuadrant-operator (package + version range). OLM's dependency
    # resolver only searches CatalogSources that are either in the same
    # namespace as the subscribing Subscription, or in the cluster's global
    # catalog namespace -- it does NOT just check "is a satisfying CSV
    # already installed somewhere". The kuadrant-operator-catalog created in
    # Phase 1 lives in ${NAMESPACE}, which is neither, so it's invisible to
    # mcp-gateway's Subscription (in ${MCP_GATEWAY_NAMESPACE}) no matter what
    # version of kuadrant-operator is actually running. Publish a copy into
    # the global catalog namespace so the dependency can actually resolve.
    cat <<EOF | kubectl apply -f -
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: kuadrant-operator-catalog-global
  namespace: ${GLOBAL_CATALOG_NAMESPACE}
spec:
  sourceType: grpc
  image: ${FROM_CATALOG}
  displayName: Kuadrant Operator (upgrade test, global for mcp-gateway dependency resolution)
  updateStrategy:
    registryPoll:
      interval: 45s
EOF

    echo "Waiting for global catalog pod..."
    for attempt in $(seq 1 5); do
        sleep 15
        if kubectl -n "${GLOBAL_CATALOG_NAMESPACE}" wait --timeout=60s --for=condition=Ready \
            pod -l olm.catalogSource=kuadrant-operator-catalog-global 2>/dev/null; then
            break
        fi
        echo "  Global catalog pod not ready yet, retrying... (attempt ${attempt}/5)"
    done

    # Mirrors https://github.com/Kuadrant/mcp-gateway/blob/main/docs/guides/olm-install.md:
    # a self-contained kustomize dir at a pinned release tag creates the
    # mcp-system namespace, OperatorGroup, CatalogSource, and Subscription.
    kubectl apply -k "https://github.com/Kuadrant/mcp-gateway/config/deploy/olm?ref=v${MCP_GATEWAY_VERSION}"

    echo "Waiting for mcp-gateway CSV..."
    for i in $(seq 1 30); do
        if kubectl wait csv -n "${MCP_GATEWAY_NAMESPACE}" -l operators.coreos.com/mcp-gateway.mcp-system="" \
            --for=jsonpath='{.status.phase}'=Succeeded --timeout=10s 2>/dev/null; then
            break
        fi
        echo "  Waiting for mcp-gateway CSV... (attempt ${i}/30)"
        sleep 10
    done

    kubectl -n "${MCP_GATEWAY_NAMESPACE}" wait --timeout=120s --for=condition=Available deployment/mcp-gateway-controller

    echo "mcp-gateway installed:"
    kubectl get subscription,csv -n "${MCP_GATEWAY_NAMESPACE}" --no-headers
fi

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
echo "dns-operator CRDs:"
kubectl get crd | grep -E 'dnsrecord|dnshealthcheck' || echo "  (none)"

# ── Phase 2: Build and push upgrade images ───────────────────────────

echo ""
echo "=== Phase 2: Build upgrade images from current branch ==="
make docker-build IMG="${OPERATOR_IMG}"
make docker-push IMG="${OPERATOR_IMG}"
make bundle IMG="${OPERATOR_IMG}" VERSION=99.0.0 CHANNELS="${TO_CHANNEL}"
make bundle-build BUNDLE_IMG="${BUNDLE_IMG}"
make docker-push IMG="${BUNDLE_IMG}"

echo ""
echo "=== Phase 3: Build upgrade catalog ==="
TMP_DIR="$(pwd)/tmp/olm-upgrade-test"
rm -rf "${TMP_DIR}"
mkdir -p "${TMP_DIR}/catalog-dir"

${OPM} render "${FROM_CATALOG}" --output=yaml > "${TMP_DIR}/catalog-dir/operator.yaml"
${OPM} render "${BUNDLE_IMG}" --output=yaml >> "${TMP_DIR}/catalog-dir/operator.yaml"
# Only graft the new entry into TO_CHANNEL, not every channel the package has.
# The Subscription is switched to TO_CHANNEL below as part of triggering the
# upgrade, so that's the only channel that needs to resolve it -- grafting
# into other channels too (e.g. FROM_CHANNEL, if different) would add a
# "replaces" edge to a CSV that was never actually a member of that channel,
# which opm validate (below) rejects as a broken upgrade graph.
#
# TO_CHANNEL must already exist as a channel for this package in FROM_CATALOG
# for this to have any effect -- yq's += is a no-op if the select matches
# nothing. It's also on you to make sure "${CSV_NAME}" (resolved from
# FROM_CHANNEL) is actually a real member of TO_CHANNEL's history: if
# TO_CHANNEL has moved past retaining that old entry, this "replaces" edge
# won't be satisfiable and opm validate will reject it too.
${YQ} -i "select(.schema == \"olm.channel\" and .package == \"kuadrant-operator\" and .name == \"${TO_CHANNEL}\").entries += [{\"name\": \"kuadrant-operator.v99.0.0\", \"replaces\": \"${CSV_NAME}\"}]" "${TMP_DIR}/catalog-dir/operator.yaml"
${OPM} validate "${TMP_DIR}/catalog-dir"
${OPM} generate dockerfile "${TMP_DIR}/catalog-dir"
# --no-cache: the generated Dockerfile bakes a pre-rendered serving cache
# (opm serve --cache-only) into the image in a RUN step following COPY
# configs/. That's what the catalog pod's gRPC API actually serves from --
# not /configs/operator.yaml directly. TMP_DIR is a fixed path recreated
# fresh each run, and its content is often byte-identical across runs (same
# FROM_CATALOG, same CSV_NAME), so Docker/BuildKit's layer cache can reuse a
# stale cache-generation layer from an earlier invocation even though the
# actual YAML in this run is correct -- this was observed live: on-disk
# /configs/operator.yaml had the right "replaces" edge but GetBundle over
# gRPC still returned none, causing OLM's resolver to see no upgrade edge
# and silently do nothing. Forcing a clean build every time is cheap (this
# image is tiny) and removes the whole class of bug.
docker build --no-cache -t "${CATALOG_IMG}" -f "${TMP_DIR}/catalog-dir.Dockerfile" "${TMP_DIR}"
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
if [ "${TO_CHANNEL}" != "${FROM_CHANNEL}" ]; then
    echo "Switching Subscription from channel ${FROM_CHANNEL} to ${TO_CHANNEL}..."
    kubectl patch subscription kuadrant -n "${NAMESPACE}" \
        --type=merge -p "{\"spec\":{\"channel\":\"${TO_CHANNEL}\"}}"
fi

# CATALOG_IMG reuses the same tag across runs (":upgrade-test"), so this patch
# is often a no-op value-wise if a prior run already set it -- no spec change
# means no generation bump, so OLM has no explicit signal to recreate the
# catalog pod. It would normally notice the digest changed via its own
# background registryPoll, but that verify-then-promote cycle has been
# observed getting raced by the next poll before it can complete, leaving
# the old pod (old digest) serving indefinitely even though a replacement
# briefly becomes Ready. Deleting the pod directly sidesteps that: OLM's
# catalog-operator recreates it immediately from the current spec, and since
# it uses imagePullPolicy=Always for tag-based images, the replacement is
# guaranteed to pull whatever the tag currently resolves to -- no need for a
# unique tag per run, and no dependency on the racy poll/promote path at all.
kubectl delete pod -n "${NAMESPACE}" -l olm.catalogSource=kuadrant-operator-catalog --ignore-not-found
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
    CSV_STATUS=$(kubectl get csv "kuadrant-operator.v99.0.0" -n "${NAMESPACE}" -o jsonpath='{.status.phase}' 2>/dev/null || echo "NotFound")
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

echo "Upgrade complete: kuadrant-operator.v99.0.0"

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

# OLM migration event should be emitted on the KuadrantControlPlane
echo ""
echo "--- OLM migration event ---"
OLM_EVENT_TYPE=$(kubectl get events --field-selector involvedObject.name=default,involvedObject.kind=KuadrantControlPlane \
    -o jsonpath='{range .items[?(@.reason=="OLMMigrationComplete")]}{.type}{end}{range .items[?(@.reason=="OLMMigrationIncomplete")]}{.type}{end}' 2>/dev/null || echo "")
OLM_EVENT_REASON=$(kubectl get events --field-selector involvedObject.name=default,involvedObject.kind=KuadrantControlPlane \
    -o jsonpath='{range .items[?(@.reason=="OLMMigrationComplete")]}{.reason}{end}{range .items[?(@.reason=="OLMMigrationIncomplete")]}{.reason}{end}' 2>/dev/null || echo "")
OLM_EVENT_MSG=$(kubectl get events --field-selector involvedObject.name=default,involvedObject.kind=KuadrantControlPlane \
    -o jsonpath='{range .items[?(@.reason=="OLMMigrationComplete")]}{.message}{end}{range .items[?(@.reason=="OLMMigrationIncomplete")]}{.message}{end}' 2>/dev/null || echo "")

if [ -n "${OLM_EVENT_REASON}" ]; then
    echo "  Type:    ${OLM_EVENT_TYPE}"
    echo "  Reason:  ${OLM_EVENT_REASON}"
    echo "  Message: ${OLM_EVENT_MSG}"
    if [ "${OLM_EVENT_REASON}" = "OLMMigrationComplete" ] && [ "${OLM_EVENT_TYPE}" = "Normal" ]; then
        echo "PASS: OLM migration completed successfully"
    else
        echo "FAIL: OLM migration did not complete successfully"
        PASS=false
    fi
else
    echo "WARN: No OLM migration event found (may have expired)"
fi

# dns-operator Subscription/CSV should be cleaned up by the operator
echo ""
echo "--- OLM cleanup ---"
if kubectl get subscription -n "${NAMESPACE}" --no-headers 2>/dev/null | grep -q "dns-operator"; then
    echo "FAIL: dns-operator Subscription still exists"
    PASS=false
else
    echo "PASS: dns-operator Subscription cleaned up"
fi

if kubectl get csv -n "${NAMESPACE}" --no-headers 2>/dev/null | grep -q "^dns-operator\."; then
    echo "FAIL: dns-operator CSV still exists"
    PASS=false
else
    echo "PASS: dns-operator CSV cleaned up"
fi

# dns-operator CRDs should exist (applied by the operator)
echo ""
echo "--- CRD survival ---"
for crd in dnsrecords.kuadrant.io dnshealthcheckprobes.kuadrant.io; do
    if kubectl get crd "${crd}" &>/dev/null; then
        echo "PASS: CRD ${crd} exists"
    else
        echo "FAIL: CRD ${crd} missing"
        PASS=false
    fi
done

# dns-operator Deployment should be running
echo ""
echo "--- Component deployments ---"
DEPLOY_READY=$(kubectl get deployment -n "${NAMESPACE}" dns-operator-controller-manager -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo "0")
if [ "${DEPLOY_READY}" -gt 0 ] 2>/dev/null; then
    echo "PASS: dns-operator running (${DEPLOY_READY} ready)"
else
    echo "FAIL: dns-operator not ready"
    PASS=false
fi

if [ "${INSTALL_MCP_GATEWAY}" = "true" ]; then
    # mcp-gateway's pre-consolidation Subscription/CSV (in mcp-system, not
    # NAMESPACE -- it was installed via its own OLM catalog into its own
    # namespace) should be cleaned up by migrateMCPGateway.
    echo ""
    echo "--- mcp-gateway OLM cleanup (mcp-system) ---"
    if kubectl get subscription -n "${MCP_GATEWAY_NAMESPACE}" --no-headers 2>/dev/null | grep -q "mcp-gateway"; then
        echo "FAIL: mcp-gateway Subscription still exists in ${MCP_GATEWAY_NAMESPACE}"
        PASS=false
    else
        echo "PASS: mcp-gateway Subscription cleaned up"
    fi

    if kubectl get csv -n "${MCP_GATEWAY_NAMESPACE}" --no-headers 2>/dev/null | grep -q "^mcp-gateway\."; then
        echo "FAIL: mcp-gateway CSV still exists in ${MCP_GATEWAY_NAMESPACE}"
        PASS=false
    else
        echo "PASS: mcp-gateway CSV cleaned up"
    fi

    # mcp-gateway CRDs should survive (stripped of OLM ownership, not
    # cascade-deleted with the CSV) -- same reasoning as dns-operator's.
    echo ""
    echo "--- mcp-gateway CRD survival ---"
    for crd in mcpgatewayextensions.mcp.kuadrant.io mcpserverregistrations.mcp.kuadrant.io mcpvirtualservers.mcp.kuadrant.io; do
        if kubectl get crd "${crd}" &>/dev/null; then
            echo "PASS: CRD ${crd} exists"
        else
            echo "FAIL: CRD ${crd} missing"
            PASS=false
        fi
    done

    # Unlike dns-operator, migrateMCPGateway deliberately does NOT protect the
    # old controller Deployment -- nothing owns it, and kuadrant-operator
    # deploys its own replacement into NAMESPACE, not mcp-system. So the old
    # one should be gone (cascade-deleted with its CSV), and the new one
    # should be running in NAMESPACE instead.
    echo ""
    echo "--- mcp-gateway controller relocation ---"
    if kubectl get deployment -n "${MCP_GATEWAY_NAMESPACE}" mcp-gateway-controller &>/dev/null; then
        echo "FAIL: old mcp-gateway-controller Deployment still exists in ${MCP_GATEWAY_NAMESPACE} (should have cascade-deleted with its CSV)"
        PASS=false
    else
        echo "PASS: old mcp-gateway-controller Deployment gone from ${MCP_GATEWAY_NAMESPACE}"
    fi

    MCP_DEPLOY_READY=$(kubectl get deployment -n "${NAMESPACE}" mcp-gateway-controller -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo "0")
    if [ "${MCP_DEPLOY_READY}" -gt 0 ] 2>/dev/null; then
        echo "PASS: mcp-gateway-controller running in ${NAMESPACE} (${MCP_DEPLOY_READY} ready)"
    else
        echo "FAIL: mcp-gateway-controller not ready in ${NAMESPACE}"
        PASS=false
    fi
fi

# KuadrantControlPlane should exist
echo ""
echo "--- KuadrantControlPlane ---"
if kubectl get kuadrantcontrolplane default &>/dev/null; then
    KCP_READY=$(kubectl get kuadrantcontrolplane default -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || echo "Unknown")
    KCP_REASON=$(kubectl get kuadrantcontrolplane default -o jsonpath='{.status.conditions[?(@.type=="Ready")].reason}' 2>/dev/null || echo "Unknown")
    echo "PASS: KuadrantControlPlane exists (Ready=${KCP_READY}, Reason=${KCP_REASON})"
else
    echo "FAIL: KuadrantControlPlane missing"
    PASS=false
fi

# Other child operators should still be running (still OLM-managed for now)
echo ""
echo "--- Other operators ---"
for deploy in authorino-operator limitador-operator-controller-manager; do
    if kubectl get deployment -n "${NAMESPACE}" "${deploy}" &>/dev/null; then
        echo "PASS: ${deploy} running"
    else
        echo "WARN: ${deploy} not found"
    fi
done

# Only the kuadrant-operator CSV should remain
echo ""
echo "--- Single CSV check ---"
CSV_COUNT=$(kubectl get csv -n "${NAMESPACE}" --no-headers 2>/dev/null | wc -l)
if [ "${CSV_COUNT}" -le 3 ]; then
    echo "PASS: ${CSV_COUNT} CSV(s) remaining (expected: kuadrant + authorino + limitador)"
else
    echo "WARN: ${CSV_COUNT} CSVs remaining (expected 3 or fewer after dns-operator cleanup)"
    kubectl get csv -n "${NAMESPACE}" --no-headers
fi

echo ""
echo "=== Final state ==="
echo "CSVs:"
kubectl get csv -n "${NAMESPACE}" --no-headers 2>/dev/null || echo "  (none)"
echo "Subscriptions:"
kubectl get subscription -n "${NAMESPACE}" --no-headers 2>/dev/null || echo "  (none)"
echo "Deployments:"
kubectl get deployment -n "${NAMESPACE}" --no-headers
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
