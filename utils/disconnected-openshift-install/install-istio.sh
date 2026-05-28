#!/usr/bin/env bash

set -euo pipefail

# Script to install Istio via Sail operator on OpenShift
# Uses Community Operators catalog to install sailoperator
# Then deploys Istio control plane with IstioCNI

echo "=========================================="
echo "Installing Istio via Sail Operator"
echo "=========================================="
echo ""

# Check if oc is available
if ! command -v oc &>/dev/null; then
    echo "ERROR: oc command not found. Please install OpenShift CLI."
    exit 1
fi

# Check if cluster is accessible
if ! oc whoami &>/dev/null; then
    echo "ERROR: Not connected to OpenShift cluster. Please login first."
    exit 1
fi

# Check if Istio is already installed
echo "==> Checking for existing Istio installation"
if oc get gatewayclass istio &>/dev/null 2>&1; then
    echo "  ✓ Istio GatewayClass already exists"
    echo ""
    echo "Istio is already installed. Skipping installation."
    echo ""
    oc get gatewayclass istio
    echo ""
    exit 0
fi
echo "  No Istio GatewayClass found"
echo ""

# Check if sailoperator package is available
echo "==> Checking sailoperator availability"
if ! oc get packagemanifest sailoperator -n openshift-marketplace &>/dev/null; then
    echo "ERROR: sailoperator package not found in openshift-marketplace"
    echo "       Make sure Community Operators catalog source is enabled"
    exit 1
fi

SAIL_CHANNEL=$(oc get packagemanifest sailoperator -n openshift-marketplace -o jsonpath='{.status.defaultChannel}' 2>/dev/null)
echo "  ✓ sailoperator found (channel: ${SAIL_CHANNEL})"
echo ""

# Create istio-system namespace
echo "==> Creating istio-system namespace"
cat <<EOF | oc apply -f -
apiVersion: v1
kind: Namespace
metadata:
  name: istio-system
EOF
echo ""

# Create OperatorGroup for Sail operator
echo "==> Creating OperatorGroup for Sail operator"
cat <<EOF | oc apply -f -
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: sail-operator-group
  namespace: istio-system
spec: {}
EOF
echo ""

# Create Subscription for Sail operator
echo "==> Creating Subscription for sailoperator"
cat <<EOF | oc apply -f -
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: sailoperator
  namespace: istio-system
spec:
  channel: ${SAIL_CHANNEL}
  name: sailoperator
  source: community-operators
  sourceNamespace: openshift-marketplace
  installPlanApproval: Automatic
EOF
echo ""

# Wait for Sail operator CSV to be ready
echo "==> Waiting for Sail operator installation (60s timeout)"
TIMEOUT=60
ELAPSED=0
CSV_PHASE=""
while [ $ELAPSED -lt $TIMEOUT ]; do
    # Get CSV name that starts with sailoperator (|| true to avoid pipefail exit)
    CSV_NAME=$(oc get csv -n istio-system --no-headers 2>/dev/null | grep "^sailoperator" | awk '{print $1}' | head -1 || true)
    if [ -n "$CSV_NAME" ]; then
        CSV_PHASE=$(oc get csv "$CSV_NAME" -n istio-system -o jsonpath='{.status.phase}' 2>/dev/null || echo "")
        if [ "$CSV_PHASE" = "Succeeded" ]; then
            echo "  ✓ Sail operator CSV ready ($CSV_NAME)"
            break
        fi
    fi
    sleep 5
    ELAPSED=$((ELAPSED + 5))
    echo "  Waiting... (${ELAPSED}s)"
done

if [ "$CSV_PHASE" != "Succeeded" ]; then
    echo "  ✗ FAILED - Sail operator CSV not ready after ${TIMEOUT}s"
    echo ""
    echo "CSV status:"
    oc get csv -n istio-system
    exit 1
fi
echo ""

# Wait for Sail operator pod to be ready
echo "==> Waiting for Sail operator pod to be ready (30s timeout)"
TIMEOUT=30
ELAPSED=0
while [ $ELAPSED -lt $TIMEOUT ]; do
    READY=$(oc get pods -n istio-system -l control-plane=sail-operator -o jsonpath='{.items[0].status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || echo "")
    if [ "$READY" = "True" ]; then
        echo "  ✓ Sail operator pod ready"
        break
    fi
    sleep 5
    ELAPSED=$((ELAPSED + 5))
    echo "  Waiting... (${ELAPSED}s)"
done

if [ "$READY" != "True" ]; then
    echo "  ✗ FAILED - Sail operator pod not ready after ${TIMEOUT}s"
    echo ""
    echo "Pod status:"
    oc get pods -n istio-system
    exit 1
fi
echo ""

# Wait for Sail operator CRDs to be created
echo "==> Waiting for Sail operator CRDs (30s timeout)"
TIMEOUT=30
ELAPSED=0
while [ $ELAPSED -lt $TIMEOUT ]; do
    if oc get crd istiocnis.sailoperator.io &>/dev/null && oc get crd istios.sailoperator.io &>/dev/null; then
        echo "  ✓ Sail operator CRDs ready"
        break
    fi
    sleep 3
    ELAPSED=$((ELAPSED + 3))
    echo "  Waiting... (${ELAPSED}s)"
done

if ! oc get crd istiocnis.sailoperator.io &>/dev/null || ! oc get crd istios.sailoperator.io &>/dev/null; then
    echo "  ✗ FAILED - CRDs not created after ${TIMEOUT}s"
    echo ""
    echo "Expected CRDs:"
    echo "  - istiocnis.sailoperator.io"
    echo "  - istios.sailoperator.io"
    echo ""
    echo "Available CRDs:"
    oc get crd | grep sailoperator
    exit 1
fi
echo ""

# Create istio-cni namespace
echo "==> Creating istio-cni namespace"
cat <<EOF | oc apply -f -
apiVersion: v1
kind: Namespace
metadata:
  name: istio-cni
EOF
echo ""

# Create IstioCNI resource (required for OpenShift)
echo "==> Creating IstioCNI resource"
cat <<EOF | oc apply -f -
apiVersion: sailoperator.io/v1
kind: IstioCNI
metadata:
  name: default
spec:
  version: v1.29.2
  namespace: istio-cni
EOF
echo ""

# Wait for IstioCNI to be ready
echo "==> Waiting for IstioCNI to be ready (60s timeout)"
TIMEOUT=60
ELAPSED=0
while [ $ELAPSED -lt $TIMEOUT ]; do
    CNI_READY=$(oc get istiocni default -o jsonpath='{.status.state}' 2>/dev/null || echo "")
    if [ "$CNI_READY" = "Healthy" ]; then
        echo "  ✓ IstioCNI ready"
        break
    fi
    sleep 5
    ELAPSED=$((ELAPSED + 5))
    echo "  Waiting... (${ELAPSED}s) [status: ${CNI_READY}]"
done

if [ "$CNI_READY" != "Healthy" ]; then
    echo "  ⚠ IstioCNI not healthy after ${TIMEOUT}s (status: ${CNI_READY})"
    echo "  Continuing anyway - Istio may still work"
fi
echo ""

# Create Istio resource (deploys control plane)
echo "==> Creating Istio resource"
cat <<EOF | oc apply -f -
apiVersion: sailoperator.io/v1
kind: Istio
metadata:
  name: default
spec:
  version: v1.29.2
  namespace: istio-system
EOF
echo ""

# Wait for Istio to be ready
echo "==> Waiting for Istio control plane to be ready (120s timeout)"
TIMEOUT=120
ELAPSED=0
while [ $ELAPSED -lt $TIMEOUT ]; do
    ISTIO_READY=$(oc get istio default -o jsonpath='{.status.state}' 2>/dev/null || echo "")
    if [ "$ISTIO_READY" = "Healthy" ]; then
        echo "  ✓ Istio control plane ready"
        break
    fi
    sleep 5
    ELAPSED=$((ELAPSED + 5))
    echo "  Waiting... (${ELAPSED}s) [status: ${ISTIO_READY}]"
done

if [ "$ISTIO_READY" != "Healthy" ]; then
    echo "  ✗ FAILED - Istio not healthy after ${TIMEOUT}s (status: ${ISTIO_READY})"
    echo ""
    echo "Istio status:"
    oc get istio default -o yaml
    exit 1
fi
echo ""

# Wait for istiod pod to be ready
echo "==> Waiting for istiod pod to be ready (30s timeout)"
TIMEOUT=30
ELAPSED=0
while [ $ELAPSED -lt $TIMEOUT ]; do
    ISTIOD_READY=$(oc get pods -n istio-system -l app=istiod -o jsonpath='{.items[0].status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || echo "")
    if [ "$ISTIOD_READY" = "True" ]; then
        echo "  ✓ istiod pod ready"
        break
    fi
    sleep 5
    ELAPSED=$((ELAPSED + 5))
    echo "  Waiting... (${ELAPSED}s)"
done

if [ "$ISTIOD_READY" != "True" ]; then
    echo "  ✗ FAILED - istiod pod not ready after ${TIMEOUT}s"
    exit 1
fi
echo ""

# Verify GatewayClass was created
echo "==> Verifying GatewayClass creation"
if oc get gatewayclass istio &>/dev/null; then
    echo "  ✓ GatewayClass 'istio' created"
    echo ""
    oc get gatewayclass istio
else
    echo "  ✗ FAILED - GatewayClass 'istio' not found"
    echo "  Istio is installed but GatewayClass not created"
    exit 1
fi
echo ""

echo "=========================================="
echo "✓ Istio Installation Complete"
echo "=========================================="
echo ""
echo "  GatewayClass 'istio' is available"
echo ""
