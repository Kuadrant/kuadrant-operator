#!/usr/bin/env bash

set -euo pipefail

# Script to test disconnected installation using oc-mirror on CRC
# Tests the full mirroring workflow: quay.io → in-cluster registry → OLM install
#
# Usage:
#   ./setup.sh
#
# Environment Variables:
#   DISABLE_DEFAULT_SOURCES - Set to "true" to disable default OperatorHub sources
#                            (Red Hat Operators, Community Operators, etc.)
#                            This simulates a true air-gapped environment.
#                            Default: false (keep default catalogs for testing)
#   REGISTRY                - Override registry (default: quay.io)
#   ORG                     - Override organization (default: kuadrant)
#   IMAGE_TAG               - Image tag for catalog (default: latest)

DISABLE_DEFAULT_SOURCES="${DISABLE_DEFAULT_SOURCES:-false}"

echo "==> Testing Disconnected Installation with oc-mirror"
echo ""

# Check CRC is running
if ! oc whoami &>/dev/null; then
    echo "ERROR: Not logged into OpenShift. Please start CRC and login first:"
    echo "  crc start"
    echo "  eval \$(crc oc-env)"
    echo "  oc login -u kubeadmin https://api.crc.testing:6443"
    exit 1
fi

# Check oc-mirror is installed
if ! command -v oc-mirror &>/dev/null; then
    echo "ERROR: oc-mirror not found. Please install it first:"
    echo "  wget https://mirror.openshift.com/pub/cgw/mirror-registry/latest/mirror-registry-amd64.tar.gz"
    echo "  tar xvf oc-mirror.tar.gz"
    echo "  sudo install oc-mirror /usr/local/bin/"
    exit 1
fi

# Configuration
# Working directory for all generated files
WORK_DIR="${WORK_DIR:-./utils/disconnected-openshift-install/tmp}"

# Registry and organization
REGISTRY="${REGISTRY:-quay.io}"
ORG="${ORG:-kuadrant}"

# Image tag for the catalogs
IMAGE_TAG="${IMAGE_TAG:-latest}"

# Catalog image - combined catalog containing all four operators
CATALOG_IMG="${REGISTRY}/${ORG}/kuadrant-operator-catalog:${IMAGE_TAG}"

MIRROR_NAMESPACE="mirror-registry"

# Create working directory
mkdir -p "${WORK_DIR}"

echo "==> Configuration:"
echo "  Working Directory: ${WORK_DIR}"
echo "  Catalog Image: ${CATALOG_IMG}"
echo "  Mirror Namespace: ${MIRROR_NAMESPACE}"
echo ""

# Step 1: Deploy simple Docker registry (same as OpenShift internal registry)
echo "==> Setting up simple Docker registry for mirroring"

echo "  Ensuring namespace exists..."
oc new-project ${MIRROR_NAMESPACE} 2>/dev/null || echo "  Namespace ${MIRROR_NAMESPACE} already exists"

# Check if Docker registry already deployed
if oc get deployment docker-registry -n ${MIRROR_NAMESPACE} &>/dev/null; then
    echo "  Docker registry already deployed"
else
    echo "  Deploying simple Docker registry..."
    cat <<EOF | oc apply -f -
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: registry-storage
  namespace: ${MIRROR_NAMESPACE}
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 30Gi
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: docker-registry
  namespace: ${MIRROR_NAMESPACE}
spec:
  replicas: 1
  selector:
    matchLabels:
      app: docker-registry
  template:
    metadata:
      labels:
        app: docker-registry
    spec:
      containers:
      - name: registry
        # Use standard Docker registry (OpenShift image has permission issues in regular pods)
        image: docker.io/library/registry:2
        ports:
        - containerPort: 5000
        volumeMounts:
        - name: registry-storage
          mountPath: /var/lib/registry
        env:
        - name: REGISTRY_HTTP_ADDR
          value: ":5000"
        - name: REGISTRY_STORAGE_FILESYSTEM_ROOTDIRECTORY
          value: /var/lib/registry
      volumes:
      - name: registry-storage
        persistentVolumeClaim:
          claimName: registry-storage
---
apiVersion: v1
kind: Service
metadata:
  name: docker-registry
  namespace: ${MIRROR_NAMESPACE}
spec:
  selector:
    app: docker-registry
  ports:
  - port: 5000
    targetPort: 5000
EOF

    # Create Route
    if ! oc get route docker-registry -n ${MIRROR_NAMESPACE} &>/dev/null; then
        oc create route edge docker-registry \
          --service=docker-registry \
          --port=5000 \
          -n ${MIRROR_NAMESPACE}
    fi

    # Wait for registry to be ready
    echo "  Waiting for registry to be ready..."
    oc wait --for=condition=available deployment/docker-registry \
      -n ${MIRROR_NAMESPACE} \
      --timeout=5m
fi

# Get registry hostname
REGISTRY_HOSTNAME=$(oc get route docker-registry -n ${MIRROR_NAMESPACE} -o jsonpath='{.spec.host}')

if [ -z "$REGISTRY_HOSTNAME" ]; then
    echo "  ERROR: Could not determine registry hostname"
    echo "  Available routes:"
    oc get routes -n ${MIRROR_NAMESPACE}
    exit 1
fi

echo "  Docker registry route: ${REGISTRY_HOSTNAME}"
echo ""

# Wait for registry HTTP endpoint to be ready
echo "  Waiting for registry HTTP endpoint to respond (90s timeout)..."
TIMEOUT=90
ELAPSED=0
REGISTRY_READY=false
while [ $ELAPSED -lt $TIMEOUT ]; do
    # Try to access the registry's /v2/ endpoint (Docker registry API health check)
    HTTP_STATUS=$(curl -k -s -o /dev/null -w "%{http_code}" https://${REGISTRY_HOSTNAME}/v2/ 2>/dev/null || echo "000")

    # 200 = OK, 401 = Unauthorized (but registry is responding)
    if [ "$HTTP_STATUS" = "200" ] || [ "$HTTP_STATUS" = "401" ]; then
        echo "  ✓ Registry HTTP endpoint ready (HTTP ${HTTP_STATUS})"
        REGISTRY_READY=true
        break
    fi

    sleep 3
    ELAPSED=$((ELAPSED + 3))
    if [ $((ELAPSED % 15)) -eq 0 ]; then
        echo "  Still waiting... (${ELAPSED}s) [HTTP ${HTTP_STATUS}]"
    fi
done

if [ "$REGISTRY_READY" = false ]; then
    echo "  ✗ Registry HTTP endpoint not ready after ${TIMEOUT}s"
    echo ""
    echo "  Deployment status:"
    oc get deployment docker-registry -n ${MIRROR_NAMESPACE}
    echo ""
    echo "  Pod status:"
    oc get pods -n ${MIRROR_NAMESPACE} -l app=docker-registry
    echo ""
    echo "  Recent pod logs:"
    oc logs -n ${MIRROR_NAMESPACE} -l app=docker-registry --tail=20
    exit 1
fi

echo "  (No authentication required)"
echo ""

# Step 1.5: Configure CA trust for mirror registry
echo "==> Configuring CA trust for mirror registry"

# Get the ingress CA certificate (same CA used by all routes in CRC)
INGRESS_CA=$(oc get configmap -n openshift-config-managed default-ingress-cert -o jsonpath='{.data.ca-bundle\.crt}')

if [ -z "$INGRESS_CA" ]; then
    echo "  WARNING: Could not get ingress CA certificate"
    echo "  Pods may fail to pull images from mirror registry due to TLS verification"
else
    # Check if registry-certs configmap exists
    if oc get configmap registry-certs -n openshift-config &>/dev/null; then
        echo "  Updating existing registry-certs configmap..."
        # Create temp file with CA cert for patching
        TEMP_CA_FILE="${WORK_DIR}/ca-bundle.crt"
        echo "${INGRESS_CA}" > "${TEMP_CA_FILE}"
        # Patch to add the new hostname entry
        oc patch configmap registry-certs -n openshift-config --type merge \
            --patch "{\"data\":{\"${REGISTRY_HOSTNAME}\":$(cat ${TEMP_CA_FILE} | jq -Rs .)}}"
        rm -f "${TEMP_CA_FILE}"
    else
        echo "  Creating registry-certs configmap..."
        TEMP_CA_FILE="${WORK_DIR}/ca-bundle.crt"
        echo "${INGRESS_CA}" > "${TEMP_CA_FILE}"
        oc create configmap registry-certs -n openshift-config \
            --from-file="${REGISTRY_HOSTNAME}=${TEMP_CA_FILE}"
        rm -f "${TEMP_CA_FILE}"
    fi

    # Update image.config.openshift.io/cluster to reference the configmap
    if ! oc get image.config.openshift.io/cluster -o yaml | grep -q "registry-certs"; then
        echo "  Configuring cluster to use registry-certs..."
        oc patch image.config.openshift.io/cluster --type merge \
            -p '{"spec":{"additionalTrustedCA":{"name":"registry-certs"}}}'
    fi

    echo "  ✓ CA trust configured for ${REGISTRY_HOSTNAME}"
fi
echo ""

# Step 2: Create ImageSetConfiguration
echo "==> Creating ImageSetConfiguration"
IMAGESET_CONFIG="${WORK_DIR}/imageset-config.yaml"

cat <<EOF > ${IMAGESET_CONFIG}
kind: ImageSetConfiguration
apiVersion: mirror.openshift.io/v2alpha1
mirror:
  operators:
    - catalog: ${CATALOG_IMG}
      full: true
  additionalImages:
    - name: quay.io/kuadrant/httpbin:latest
    - name: registry.istio.io/release/pilot@sha256:3681fe197870abad5c953107e241e104a813ba2d6e63933ff53e0545348ff8b1
    - name: registry.istio.io/release/proxyv2@sha256:69ca4585993f4057861954f18be88df8268b713d114e7036bf9b185458c7a2c1
    - name: registry.istio.io/release/install-cni@sha256:c83945796021a2992ceec8b8e672b1c19771e3906555a3a1dd467eddf2b82b4d
EOF

cat ${IMAGESET_CONFIG}
echo ""

# Step 3: Run oc-mirror
echo "==> Running oc-mirror v2 (this may take several minutes)..."
echo "  Source Catalog: ${CATALOG_IMG}"
echo "  Destination: ${REGISTRY_HOSTNAME}"
echo ""

# Authenticate to source registry
echo "==> Authenticating to ${REGISTRY}..."
echo "  You may need to login to ${REGISTRY} if this is a private catalog"
podman login ${REGISTRY} || echo "  Continuing without ${REGISTRY} auth (assuming public catalog)"

# Run oc-mirror v2
# Note: v2 requires --workspace for mirror-to-mirror workflow
# --dest-tls-verify=false because our test registry uses self-signed certs
oc-mirror --v2 --config ${IMAGESET_CONFIG} \
  --workspace file://${WORK_DIR}/oc-mirror-workspace \
  --dest-tls-verify=false \
  docker://${REGISTRY_HOSTNAME}

echo ""
echo "==> oc-mirror completed"
echo ""

# Step 4: Apply image mirror configuration
echo "==> Applying image mirror configuration"

# oc-mirror v2 puts files in working-dir/cluster-resources
RESULTS_DIR="${WORK_DIR}/oc-mirror-workspace/working-dir/cluster-resources"

if [ ! -d "$RESULTS_DIR" ]; then
    echo "ERROR: Results directory not found at ${RESULTS_DIR}"
    echo "  oc-mirror may have failed."
    exit 1
fi

echo "  Results directory: ${RESULTS_DIR}"

# oc-mirror v2 can generate IDMS, ITMS, or ICSP depending on how images were mirrored
IDMS_FILE="${RESULTS_DIR}/idms-oc-mirror.yaml"
ITMS_FILE="${RESULTS_DIR}/itms-oc-mirror.yaml"
ICSP_FILE="${RESULTS_DIR}/icsp-oc-mirror.yaml"

APPLIED_ANY=false

if [ -f "$IDMS_FILE" ]; then
    echo "  Applying ImageDigestMirrorSet..."
    oc apply -f ${IDMS_FILE}
    oc get imagedigestmirrorset
    APPLIED_ANY=true
fi

if [ -f "$ITMS_FILE" ]; then
    echo "  Applying ImageTagMirrorSet..."
    echo "  (Note: ITMS handles tag-based pulls - needed for catalog images)"
    oc apply -f ${ITMS_FILE}
    oc get imagetagmirrorset
    APPLIED_ANY=true
fi

if [ -f "$ICSP_FILE" ]; then
    echo "  Applying ImageContentSourcePolicy..."
    oc apply -f ${ICSP_FILE}
    oc get imagecontentsourcepolicy
    APPLIED_ANY=true
fi

if [ "$APPLIED_ANY" = false ]; then
    echo "ERROR: No image mirror configuration found"
    echo "  Available files:"
    ls -la ${RESULTS_DIR}/
    exit 1
fi
echo ""

# Add ImageTagMirrorSet for Istio registry (needed for tag-based pulls like proxyv2:1.29.2)
echo "  Creating additional ImageTagMirrorSet for Istio images..."
cat <<EOF | oc apply -f -
apiVersion: config.openshift.io/v1
kind: ImageTagMirrorSet
metadata:
  name: itms-istio
  labels:
    kuadrant.io/disconnected-test: "true"
spec:
  imageTagMirrors:
  - mirrors:
    - ${REGISTRY_HOSTNAME}/release
    source: registry.istio.io/release
EOF
echo "  ✓ Istio ImageTagMirrorSet created"
echo ""

# Step 5: Wait for cluster to stabilize after IDMS/ITMS changes
echo "==> Waiting for cluster to stabilize after image mirror configuration"
echo "  Note: Applying IDMS/ITMS triggers control plane restart"
echo ""

# Give the cluster a moment to start processing the changes
sleep 5

# Check if this is a single-node cluster (like CRC)
NODE_COUNT=$(oc get nodes --no-headers 2>/dev/null | wc -l)
IS_SINGLE_NODE=false
if [ "$NODE_COUNT" -eq 1 ]; then
    IS_SINGLE_NODE=true
    echo "  Detected single-node cluster (e.g., CRC)"
    echo "  API server pods may need special handling due to anti-affinity rules"
fi
echo ""

# Wait for openshift-apiserver to stabilize (critical for API access)
echo "  Waiting for openshift-apiserver to stabilize..."
APISERVER_TIMEOUT=300  # 5 minutes
APISERVER_STABLE=false
START_TIME=$(date +%s)

while [ $(($(date +%s) - START_TIME)) -lt $APISERVER_TIMEOUT ]; do
    # Check if openshift-apiserver is available
    APISERVER_AVAILABLE=$(oc get co openshift-apiserver -o jsonpath='{.status.conditions[?(@.type=="Available")].status}' 2>/dev/null)
    APISERVER_PROGRESSING=$(oc get co openshift-apiserver -o jsonpath='{.status.conditions[?(@.type=="Progressing")].status}' 2>/dev/null)
    APISERVER_DEGRADED=$(oc get co openshift-apiserver -o jsonpath='{.status.conditions[?(@.type=="Degraded")].status}' 2>/dev/null)

    if [ "$APISERVER_AVAILABLE" = "True" ] && [ "$APISERVER_PROGRESSING" = "False" ] && [ "$APISERVER_DEGRADED" = "False" ]; then
        APISERVER_STABLE=true
        echo "  ✓ openshift-apiserver is stable"
        break
    fi

    # On single-node clusters, check for stuck apiserver pods due to anti-affinity
    if [ "$IS_SINGLE_NODE" = true ]; then
        PENDING_PODS=$(oc get pods -n openshift-apiserver --no-headers 2>/dev/null | grep -c "Pending" || echo "0")
        TERMINATING_PODS=$(oc get pods -n openshift-apiserver --no-headers 2>/dev/null | grep -c "Terminating" || echo "0")

        if [ "$PENDING_PODS" -gt 0 ] && [ "$TERMINATING_PODS" -gt 0 ]; then
            echo "  ⚠ Detected stuck pod transition (anti-affinity on single-node)"
            echo "  Force-deleting terminating pods to unblock new pods..."
            oc get pods -n openshift-apiserver --no-headers 2>/dev/null | grep "Terminating" | awk '{print $1}' | while read pod; do
                oc delete pod -n openshift-apiserver "$pod" --force --grace-period=0 2>/dev/null || true
            done
            sleep 10
        fi
    fi

    echo "  Waiting... (Available: $APISERVER_AVAILABLE, Progressing: $APISERVER_PROGRESSING, Degraded: $APISERVER_DEGRADED)"
    sleep 10
done

if [ "$APISERVER_STABLE" = false ]; then
    echo "  WARNING: openshift-apiserver did not stabilize within ${APISERVER_TIMEOUT}s"
    echo "  Current status:"
    oc get co openshift-apiserver 2>/dev/null || echo "  (unable to query cluster operator)"
    echo ""
    echo "  You may need to:"
    echo "    1. Wait longer and retry: oc get co openshift-apiserver -w"
    echo "    2. Check apiserver pods: oc get pods -n openshift-apiserver"
    echo "    3. Check for terminating pods blocking new ones on single-node clusters"
    echo ""
    read -p "  Continue anyway? [y/N] " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        exit 1
    fi
fi
echo ""

# Step 6: Wait for MachineConfig rollout (if needed)
echo "==> Checking MachineConfig status"
echo "  Note: On CRC, MachineConfig updates may take 10-20 minutes"
echo "  Press Ctrl+C if you want to skip waiting and continue manually"
echo ""

# Check if any MCP is updating
if oc get mcp -o json | jq -e '.items[] | select(.status.conditions[] | select(.type=="Updating" and .status=="True"))' &>/dev/null; then
    echo "  MachineConfigPool is updating..."
    echo "  Waiting for rollout (timeout: 30m)..."
    oc wait mcp --all --for=condition=Updated --timeout=30m || {
        echo "  WARNING: MachineConfig rollout timed out or failed"
        echo "  You may need to wait longer or check manually with: oc get mcp"
    }
else
    echo "  No MachineConfigPool updates needed"
fi
echo ""

# Step 6: Apply CatalogSource
echo "==> Creating CatalogSource"

# oc-mirror v2 generates CatalogSource file for the catalog
CATALOG_FILE=$(find ${RESULTS_DIR} -name "cs-*.yaml" -o -name "catalogSource-*.yaml" 2>/dev/null | head -1)

if [ -z "$CATALOG_FILE" ]; then
    echo "  ERROR: No CatalogSource file found in ${RESULTS_DIR}"
    ls -la ${RESULTS_DIR}/
    exit 1
fi

# Rename CatalogSource for clarity
ORIGINAL_NAME=$(yq eval '.metadata.name' ${CATALOG_FILE})
NEW_NAME="kuadrant-disconnected-operator-catalog"

echo "  Renaming CatalogSource: ${ORIGINAL_NAME} → ${NEW_NAME}"

# Update the name and add a label for easy identification
yq eval ".metadata.name = \"${NEW_NAME}\" | .metadata.labels.\"kuadrant.io/disconnected-test\" = \"true\"" \
    -i ${CATALOG_FILE}

oc apply -f ${CATALOG_FILE}
echo ""

# Step 7: Wait for CatalogSource to be ready
echo "==> Waiting for CatalogSource to be ready"

# Give OLM a moment to process the CatalogSource
sleep 5

# Find the catalog name (using the label we added)
CATALOG_NAME=$(oc get catalogsource -n openshift-marketplace -l kuadrant.io/disconnected-test=true -o name | cut -d'/' -f2)

if [ -z "$CATALOG_NAME" ]; then
    echo "  ERROR: Kuadrant CatalogSource not found"
    echo "  Available CatalogSources:"
    oc get catalogsource -n openshift-marketplace
    exit 1
fi

echo "  Waiting for catalog: ${CATALOG_NAME}"

# CatalogSource doesn't use standard conditions - check connectionState instead
CATALOG_TIMEOUT=300  # 5 minutes
CATALOG_READY=false
START_TIME=$(date +%s)

while [ $(($(date +%s) - START_TIME)) -lt $CATALOG_TIMEOUT ]; do
    LAST_STATE=$(oc get catalogsource ${CATALOG_NAME} -n openshift-marketplace \
        -o jsonpath='{.status.connectionState.lastObservedState}' 2>/dev/null || echo "")

    if [ "$LAST_STATE" = "READY" ]; then
        CATALOG_READY=true
        echo "    ✓ READY"
        break
    fi

    sleep 5
done

if [ "$CATALOG_READY" = false ]; then
    echo "    ✗ ERROR: Timed out waiting for catalog to be READY"
    echo "    Current state: ${LAST_STATE}"
    echo "    Check pod logs:"
    echo "      oc logs -n openshift-marketplace -l olm.catalogSource=${CATALOG_NAME}"
    exit 1
fi
echo ""

# Step 7.5: Optionally disable default OperatorHub sources
if [ "$DISABLE_DEFAULT_SOURCES" = "true" ]; then
    echo "==> Disabling default OperatorHub sources"
    echo "  This simulates a true air-gapped environment where external registries are unreachable"
    echo ""

    # Check current state
    CURRENT_STATE=$(oc get operatorhub cluster -o jsonpath='{.spec.disableAllDefaultSources}' 2>/dev/null || echo "false")

    if [ "$CURRENT_STATE" = "true" ]; then
        echo "  Default sources already disabled"
    else
        echo "  Disabling: redhat-operators, community-operators, certified-operators, redhat-marketplace"
        oc patch operatorhub cluster --type json \
            -p '[{"op": "add", "path": "/spec/disableAllDefaultSources", "value": true}]'

        echo "  ✓ Default OperatorHub sources disabled"
        echo "  Note: In a real disconnected cluster, this would be done during installation"
    fi
    echo ""
else
    echo "==> Keeping default OperatorHub sources enabled"
    echo "  (Set DISABLE_DEFAULT_SOURCES=true to simulate a true air-gapped environment)"
    echo ""
fi

# Step 8: Verify packages are available
echo "==> Verifying Kuadrant operator packages are available"
echo "  Note: OLM needs time to index catalogs and create PackageManifests"
echo ""

EXPECTED_PACKAGES="kuadrant-operator authorino-operator limitador-operator dns-operator"
PACKAGE_TIMEOUT=120  # 2 minutes for OLM to index
ALL_FOUND=false
START_TIME=$(date +%s)

echo "  Waiting for PackageManifests to appear from disconnected catalogs..."

while [ $(($(date +%s) - START_TIME)) -lt $PACKAGE_TIMEOUT ]; do
    ALL_FOUND=true

    for package in ${EXPECTED_PACKAGES}; do
        # Check if package exists from any of our disconnected catalogs (kuadrant-disconnected-*)
        # Note: Multiple PackageManifests can exist for the same package name from different catalogs
        FOUND_COUNT=$(oc get packagemanifest -n openshift-marketplace -o json 2>/dev/null | \
            jq -r ".items[] | select(.metadata.name==\"${package}\") | select(.status.catalogSource | startswith(\"kuadrant-disconnected-\")) | .metadata.name" 2>/dev/null | wc -l || echo "0")

        if [ "$FOUND_COUNT" -eq 0 ]; then
            ALL_FOUND=false
            break
        fi
    done

    if [ "$ALL_FOUND" = true ]; then
        break
    fi

    sleep 5
done

# Final verification and reporting
echo ""
for package in ${EXPECTED_PACKAGES}; do
    # Find the PackageManifest from our disconnected catalog
    # (there may be multiple PackageManifests with the same name from different catalogs)
    CATALOG_SOURCE=$(oc get packagemanifest -n openshift-marketplace -o json 2>/dev/null | \
        jq -r ".items[] | select(.metadata.name==\"${package}\") | select(.status.catalogSource | startswith(\"kuadrant-disconnected-\")) | .status.catalogSource" 2>/dev/null | head -1)

    if [ -n "$CATALOG_SOURCE" ]; then
        echo "  ✓ ${package} (from ${CATALOG_SOURCE})"
    else
        echo "  ✗ ${package} (not found in disconnected catalogs)"
        # Show what catalog it was found in (if any)
        FALLBACK_CATALOG=$(oc get packagemanifest ${package} -n openshift-marketplace -o jsonpath='{.status.catalogSource}' 2>/dev/null || echo "NOT_FOUND")
        echo "    Found in: ${FALLBACK_CATALOG}"
        ALL_FOUND=false
    fi
done

if [ "$ALL_FOUND" = false ]; then
    echo ""
    echo "  ERROR: Not all packages available from disconnected catalogs"
    echo ""
    echo "  Packages from disconnected catalogs (kuadrant-disconnected-*):"
    oc get packagemanifest -n openshift-marketplace -o json | \
        jq -r '.items[] | select(.status.catalogSource | startswith("kuadrant-disconnected-")) | "    \(.metadata.name) (\(.status.catalogSource))"' | \
        sort || echo "    None"
    echo ""
    echo "  Troubleshooting:"
    echo "    1. Check catalog pod logs: oc logs -n openshift-marketplace -l olm.catalogSource"
    echo "    2. Check OLM catalog-operator logs: oc logs -n openshift-operator-lifecycle-manager -l app=catalog-operator"
    echo "    3. Wait longer and retry - OLM indexing can take 2-3 minutes"
    exit 1
fi

echo ""

# Generate installation manifests
INSTALL_DIR="${WORK_DIR}/install"
mkdir -p "${INSTALL_DIR}"

# Use the catalog name (should be kuadrant-disconnected-operator-catalog)
KUADRANT_CATALOG="${CATALOG_NAME}"

# Generate Namespace manifest
cat > "${INSTALL_DIR}/01-namespace.yaml" <<EOF
apiVersion: v1
kind: Namespace
metadata:
  name: kuadrant-system
  labels:
    kuadrant.io/disconnected-test: "true"
EOF

# Generate OperatorGroup manifest
# NOTE: Kuadrant operators only support AllNamespaces install mode (cluster-scoped)
# Do NOT set targetNamespaces - this makes it cluster-scoped
cat > "${INSTALL_DIR}/02-operatorgroup.yaml" <<EOF
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: kuadrant-operator-group
  namespace: kuadrant-system
spec: {}
EOF

# Detect default channel for kuadrant-operator
echo "Detecting default channel for kuadrant-operator..."
# Wait a bit for packagemanifest to be available after CatalogSource is ready
sleep 5
DEFAULT_CHANNEL=$(oc get packagemanifest kuadrant-operator -n openshift-marketplace -o jsonpath='{.status.defaultChannel}' 2>/dev/null || echo "")

if [ -z "$DEFAULT_CHANNEL" ]; then
    # Fallback: try to get channels from packagemanifest
    AVAILABLE_CHANNELS=$(oc get packagemanifest kuadrant-operator -n openshift-marketplace -o jsonpath='{.status.channels[*].name}' 2>/dev/null || echo "")
    if [ -n "$AVAILABLE_CHANNELS" ]; then
        # Use first available channel
        DEFAULT_CHANNEL=$(echo "$AVAILABLE_CHANNELS" | awk '{print $1}')
        echo "  No default channel set, using first available: ${DEFAULT_CHANNEL}"
    else
        # Final fallback
        DEFAULT_CHANNEL="alpha"
        echo "  WARNING: Could not detect channel, defaulting to: ${DEFAULT_CHANNEL}"
        echo "  If subscription fails, check available channels with:"
        echo "    oc get packagemanifest kuadrant-operator -n openshift-marketplace -o jsonpath='{.status.channels[*].name}'"
    fi
else
    echo "  Using default channel: ${DEFAULT_CHANNEL}"
fi
echo ""

# Generate Subscription manifest
cat > "${INSTALL_DIR}/03-subscription.yaml" <<EOF
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: kuadrant-operator
  namespace: kuadrant-system
spec:
  channel: ${DEFAULT_CHANNEL}
  name: kuadrant-operator
  source: ${KUADRANT_CATALOG}
  sourceNamespace: openshift-marketplace
  installPlanApproval: Automatic
EOF

# Generate Kuadrant CR manifest
cat > "${INSTALL_DIR}/04-kuadrant.yaml" <<EOF
apiVersion: kuadrant.io/v1beta1
kind: Kuadrant
metadata:
  name: kuadrant
  namespace: kuadrant-system
spec: {}
EOF

# Generate install script
cat > "${INSTALL_DIR}/install.sh" <<'SCRIPT'
#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "==> Installing Kuadrant from disconnected catalogs"
echo ""

echo "Step 1: Creating namespace..."
oc apply -f "${SCRIPT_DIR}/01-namespace.yaml"
echo ""

echo "Step 2: Creating OperatorGroup..."
oc apply -f "${SCRIPT_DIR}/02-operatorgroup.yaml"
echo ""

echo "Step 3: Creating Subscription..."
oc apply -f "${SCRIPT_DIR}/03-subscription.yaml"
echo ""

echo "Step 4: Waiting for operator installation..."
echo "  (This may take 2-3 minutes)"
echo ""

# Wait for kuadrant-operator CSV to appear
echo "  Waiting for kuadrant-operator CSV..."
for i in {1..60}; do
    if oc get csv -n kuadrant-system 2>/dev/null | grep -q kuadrant-operator; then
        break
    fi
    sleep 5
done

# Wait for all CSVs to be in Succeeded phase
echo "  Waiting for all operators to be ready..."
oc wait --for=jsonpath='{.status.phase}'=Succeeded csv --all -n kuadrant-system --timeout=300s

echo ""
echo "Step 5: Creating Kuadrant instance..."
oc apply -f "${SCRIPT_DIR}/04-kuadrant.yaml"
echo ""

echo "  Waiting for Kuadrant CR to be created..."
sleep 5

echo ""
echo "=========================================="
echo "✓ Installation Complete"
echo "=========================================="
echo ""

echo "Installed operators:"
oc get csv -n kuadrant-system
echo ""

echo "Kuadrant instance:"
oc get kuadrant -n kuadrant-system
echo ""

echo "Running pods:"
oc get pods -n kuadrant-system
echo ""

echo "Kuadrant status:"
oc get kuadrant kuadrant -n kuadrant-system -o jsonpath='{.status.conditions[?(@.type=="Ready")]}' | jq '.'
echo ""

READY_STATUS=$(oc get kuadrant kuadrant -n kuadrant-system -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || echo "Unknown")
if [ "$READY_STATUS" != "True" ]; then
    echo "⚠ Note: Kuadrant is not fully ready yet"
    READY_MESSAGE=$(oc get kuadrant kuadrant -n kuadrant-system -o jsonpath='{.status.conditions[?(@.type=="Ready")].message}' 2>/dev/null || echo "")
    if [ -n "$READY_MESSAGE" ]; then
        echo "  Reason: ${READY_MESSAGE}"
    fi
    echo ""
    echo "  Deployments may still be starting up. This usually takes 1-2 minutes."
    echo "  Monitor with: oc get pods -n kuadrant-system -w"
    echo ""
fi

echo "Verify images are from mirror registry:"
oc get pods -n kuadrant-system -o jsonpath='{range .items[*]}{"\n"}{.metadata.name}{"\n"}{range .spec.containers[*]}{" "}{.image}{"\n"}{end}{end}'
echo ""
SCRIPT

chmod +x "${INSTALL_DIR}/install.sh"

# Generate cleanup script
cat > "${INSTALL_DIR}/uninstall.sh" <<'SCRIPT'
#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "==> Uninstalling Kuadrant"
echo ""

echo "Deleting Kuadrant CR..."
oc delete -f "${SCRIPT_DIR}/04-kuadrant.yaml" --ignore-not-found

echo "Deleting Subscription..."
oc delete -f "${SCRIPT_DIR}/03-subscription.yaml" --ignore-not-found

echo "Deleting CSVs..."
oc delete csv --all -n kuadrant-system --ignore-not-found

echo "Deleting OperatorGroup..."
oc delete -f "${SCRIPT_DIR}/02-operatorgroup.yaml" --ignore-not-found

echo "Waiting for pods to terminate..."
oc wait --for=delete pod --all -n kuadrant-system --timeout=60s 2>/dev/null || true

echo "Deleting namespace..."
oc delete -f "${SCRIPT_DIR}/01-namespace.yaml" --ignore-not-found

echo ""
echo "✓ Uninstall complete"
SCRIPT

chmod +x "${INSTALL_DIR}/uninstall.sh"

echo ""
echo "=========================================="
echo "✓ Setup Complete"
echo "=========================================="
echo ""
echo "  ✓ ImageDigestMirrorSet created"
echo "  ✓ ImageTagMirrorSet created"
echo "  ✓ CatalogSource ready (${CATALOG_NAME})"
echo "  ✓ Installation manifests: ${INSTALL_DIR}/"
if [ "$DISABLE_DEFAULT_SOURCES" = "true" ]; then
    echo "  ✓ Default OperatorHub sources disabled"
fi
echo ""
