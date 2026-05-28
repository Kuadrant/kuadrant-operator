#!/usr/bin/env bash

set -euo pipefail

# Script to run the complete End-to-End Test workflow for disconnected installation testing
#
# This script executes all steps from the README End-to-End Test section:
#   1. Install Istio via Sail operator
#   2. Configure cluster mirrors and create CatalogSource
#   3. (Optional) Disconnect cluster to simulate air-gap
#   4. Install Kuadrant operator
#   5. Run smoke tests to validate installation
#   6. (Optional) Reconnect cluster
#   7. (Optional) Cleanup all resources
#
# Usage:
#   ./utils/disconnected-openshift-install/run-e2e-test.sh [options]
#
# Options:
#   --install-istio       Install Istio (skip if already installed)
#   --disconnect          Simulate air-gap by disconnecting cluster
#   --skip-cleanup        Skip cleanup step at the end
#   --image-tag=TAG       Catalog image tag (default: latest)
#   --disable-sources     Disable default OperatorHub sources
#
# Environment Variables:
#   IMAGE_TAG                 - Catalog image tag (default: latest)
#   DISABLE_DEFAULT_SOURCES   - Set to "true" to disable default sources (default: false)
#
# Examples:
#   # Basic run (assumes Istio already installed)
#   ./utils/disconnected-openshift-install/run-e2e-test.sh
#
#   # Install Istio as part of the run
#   ./utils/disconnected-openshift-install/run-e2e-test.sh --install-istio
#
#   # Full run with Istio installation and air-gap simulation
#   ./utils/disconnected-openshift-install/run-e2e-test.sh --install-istio --disconnect
#
#   # Use custom catalog tag and disable default sources
#   ./utils/disconnected-openshift-install/run-e2e-test.sh --image-tag=dev --disable-sources

# Parse command-line arguments
INSTALL_ISTIO=false
DISCONNECT=false
SKIP_CLEANUP=false
IMAGE_TAG="${IMAGE_TAG:-latest}"
DISABLE_DEFAULT_SOURCES="${DISABLE_DEFAULT_SOURCES:-false}"

for arg in "$@"; do
    case $arg in
        --install-istio)
            INSTALL_ISTIO=true
            shift
            ;;
        --disconnect)
            DISCONNECT=true
            shift
            ;;
        --skip-cleanup)
            SKIP_CLEANUP=true
            shift
            ;;
        --image-tag=*)
            IMAGE_TAG="${arg#*=}"
            shift
            ;;
        --disable-sources)
            DISABLE_DEFAULT_SOURCES=true
            shift
            ;;
        --help|-h)
            grep '^#' "$0" | grep -v '#!/usr/bin/env' | sed 's/^# \?//'
            exit 0
            ;;
        *)
            echo "Unknown option: $arg"
            echo "Use --help for usage information"
            exit 1
            ;;
    esac
done

# Check we're in the right directory
if [ ! -f "Makefile" ] || [ ! -d "utils/disconnected-openshift-install" ]; then
    echo "ERROR: This script must be run from the kuadrant-operator directory"
    exit 1
fi

# Check cluster access
if ! oc whoami &>/dev/null; then
    echo "ERROR: Not logged into OpenShift cluster"
    echo "  Please login first: oc login ..."
    exit 1
fi

START_TIME=$(date +%s)

echo "=========================================="
echo "Kuadrant Disconnected End-to-End Test"
echo "=========================================="
echo ""
echo "Configuration:"
echo "  Install Istio: ${INSTALL_ISTIO}"
echo "  Catalog Tag: ${IMAGE_TAG}"
echo "  Disable Default Sources: ${DISABLE_DEFAULT_SOURCES}"
echo "  Disconnect Cluster: ${DISCONNECT}"
echo "  Skip Cleanup: ${SKIP_CLEANUP}"
echo ""
echo "Cluster:"
CLUSTER_API=$(oc whoami --show-server)
CLUSTER_USER=$(oc whoami)
echo "  API: ${CLUSTER_API}"
echo "  User: ${CLUSTER_USER}"

# Get OpenShift/OKD version
OCP_VERSION=$(oc get clusterversion version -o jsonpath='{.status.desired.version}' 2>/dev/null || echo "Unknown")
echo "  Version: ${OCP_VERSION}"

# Get Kubernetes version
K8S_VERSION=$(oc version -o json 2>/dev/null | jq -r '.serverVersion.gitVersion' 2>/dev/null || echo "Unknown")
echo "  Kubernetes: ${K8S_VERSION}"

# Detect platform (OpenShift vs OKD)
PLATFORM=$(oc get clusterversion version -o jsonpath='{.spec.channel}' 2>/dev/null | grep -q "okd" && echo "OKD" || echo "OpenShift")
echo "  Platform: ${PLATFORM}"

# Get node count
NODE_COUNT=$(oc get nodes --no-headers 2>/dev/null | wc -l || echo "0")
echo "  Nodes: ${NODE_COUNT}"
echo ""
read -p "Continue with end-to-end test? [y/N] " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo "Aborted"
    exit 0
fi
echo ""

# Step 1: Install Istio (optional)
if [ "$INSTALL_ISTIO" = true ]; then
    echo "=========================================="
    echo "Step 1/5: Installing Istio"
    echo "=========================================="
    echo ""
    ./utils/disconnected-openshift-install/install-istio.sh

    if [ $? -ne 0 ]; then
        echo ""
        echo "✗ Istio installation failed"
        exit 1
    fi
    echo ""
else
    echo "=========================================="
    echo "Step 1/5: Skipping Istio Installation"
    echo "=========================================="
    echo ""
    echo "  Checking for existing Istio installation..."
    if oc get gatewayclass istio &>/dev/null 2>&1; then
        echo "  ✓ Istio GatewayClass found"
    else
        echo "  ⚠ Istio GatewayClass 'istio' not found"
        echo "    Kuadrant requires Istio to be installed"
        echo "    Run with --install-istio flag or install manually:"
        echo "      ./utils/disconnected-openshift-install/install-istio.sh"
        echo ""
        read -p "  Continue anyway? [y/N] " -n 1 -r
        echo
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            echo "Aborted"
            exit 1
        fi
    fi
    echo ""
fi

# Step 2: Configure cluster mirrors and CatalogSource
echo "=========================================="
echo "Step 2/5: Configuring Cluster Mirrors"
echo "=========================================="
echo ""
DISABLE_DEFAULT_SOURCES=${DISABLE_DEFAULT_SOURCES} IMAGE_TAG=${IMAGE_TAG} ./utils/disconnected-openshift-install/setup.sh

if [ $? -ne 0 ]; then
    echo ""
    echo "✗ Setup failed"
    exit 1
fi
echo ""

# Step 3: Optional - Disconnect cluster
if [ "$DISCONNECT" = true ]; then
    echo "=========================================="
    echo "Step 3/5: Disconnecting Cluster (Air-gap)"
    echo "=========================================="
    echo ""
    ./utils/disconnected-openshift-install/cluster-disconnect.sh disconnect

    if [ $? -ne 0 ]; then
        echo ""
        echo "✗ Cluster disconnect failed"
        exit 1
    fi
    echo ""
else
    echo "=========================================="
    echo "Step 3/5: Skipping Cluster Disconnect"
    echo "=========================================="
    echo ""
    echo "  Running in connected mode (not simulating air-gap)"
    echo "  Use --disconnect flag to simulate air-gapped environment"
    echo ""
fi

# Step 4: Install Kuadrant
echo "=========================================="
echo "Step 4/5: Installing Kuadrant"
echo "=========================================="
echo ""

INSTALL_SCRIPT="./utils/disconnected-openshift-install/tmp/install/install.sh"
if [ ! -f "$INSTALL_SCRIPT" ]; then
    echo "✗ Install script not found: ${INSTALL_SCRIPT}"
    echo "  The setup script should have generated this file"
    exit 1
fi

${INSTALL_SCRIPT}

if [ $? -ne 0 ]; then
    echo ""
    echo "✗ Kuadrant installation failed"
    exit 1
fi
echo ""

# Step 5: Run smoke tests
echo "=========================================="
echo "Step 5/5: Running Smoke Tests"
echo "=========================================="
echo ""
./utils/disconnected-openshift-install/smoke-test.sh --cleanup

SMOKE_TEST_RESULT=$?
echo ""

END_TIME=$(date +%s)
DURATION=$((END_TIME - START_TIME))
MINUTES=$((DURATION / 60))
SECONDS=$((DURATION % 60))

# Final status
echo "=========================================="
if [ $SMOKE_TEST_RESULT -eq 0 ]; then
    echo "✓ End-to-End Test Complete"
else
    echo "✗ End-to-End Test Failed"
fi
echo "=========================================="
echo ""
echo "Total time: ${MINUTES}m ${SECONDS}s"
echo ""

if [ $SMOKE_TEST_RESULT -eq 0 ]; then
    echo "  ✓ Istio installed"
    echo "  ✓ Kuadrant operators installed"
    echo "  ✓ All tests passed"
    echo ""

    # Cleanup if not skipped (cleanup will handle reconnect)
    if [ "$SKIP_CLEANUP" = false ]; then
        echo "=========================================="
        echo "Cleanup"
        echo "=========================================="
        echo ""
        ./utils/disconnected-openshift-install/cleanup.sh --yes
        echo ""
    else
        # If skipping cleanup but cluster was disconnected, reconnect it
        if [ "$DISCONNECT" = true ]; then
            echo "=========================================="
            echo "Reconnecting Cluster"
            echo "=========================================="
            echo ""
            ./utils/disconnected-openshift-install/cluster-disconnect.sh reconnect
            if [ $? -ne 0 ]; then
                echo "⚠ Cluster reconnect failed (non-fatal)"
            fi
            echo ""
        fi
    fi
fi

exit $SMOKE_TEST_RESULT
