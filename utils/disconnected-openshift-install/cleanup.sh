#!/usr/bin/env bash

set -euo pipefail

# Script to cleanup resources created by setup.sh
# Removes both cluster resources (if accessible) and local files
# Restores cluster to original state (reconnects network, removes mirrors, etc.)
#
# Usage:
#   ./cleanup.sh [--yes]
#
# Options:
#   --yes, -y    Skip all prompts and clean everything automatically

# Parse arguments
AUTO_YES=false
if [ "${1:-}" = "--yes" ] || [ "${1:-}" = "-y" ]; then
    AUTO_YES=true
fi

echo "==> Cleanup Disconnected Installation Test Resources"
echo ""

# Configuration
WORK_DIR="${WORK_DIR:-./utils/disconnected-openshift-install/tmp}"
MIRROR_NAMESPACE="mirror-registry"

# Check if cluster is accessible
CLUSTER_ACCESSIBLE=false
if oc whoami &>/dev/null; then
    echo "✓ OpenShift cluster accessible"
    CLUSTER_ACCESSIBLE=true
else
    echo "✗ OpenShift cluster not accessible (skipping cluster cleanup)"
fi

# Cluster Cleanup
if [ "$CLUSTER_ACCESSIBLE" = true ]; then
    echo ""
    echo "==> Cleaning up cluster resources"

    # Find all Kuadrant disconnected test CatalogSources (using the label)
    echo "  Finding Kuadrant disconnected test CatalogSources..."
    CATALOG_NAMES=$(oc get catalogsource -n openshift-marketplace -l kuadrant.io/disconnected-test=true -o name 2>/dev/null | \
        cut -d'/' -f2 || echo "")

    if [ -n "$CATALOG_NAMES" ]; then
        echo "  Removing CatalogSources:"
        for catalog_name in ${CATALOG_NAMES}; do
            echo "    - ${catalog_name}"
            oc delete catalogsource ${catalog_name} -n openshift-marketplace --ignore-not-found=true
        done
    else
        echo "  No Kuadrant CatalogSources found"
    fi

    # Remove kuadrant-system namespace if it exists
    echo ""
    echo "  Checking for kuadrant-system namespace..."
    if oc get namespace kuadrant-system &>/dev/null; then
        echo "  Found namespace kuadrant-system"

        # Check if uninstall script exists
        if [ -d "${WORK_DIR}/install" ] && [ -f "${WORK_DIR}/install/uninstall.sh" ]; then
            echo "  Automated uninstall script available"
            if [ "$AUTO_YES" = true ]; then
                REPLY="y"
            else
                read -p "  Run automated uninstall script? [y/N] " -n 1 -r
                echo
            fi
            if [[ $REPLY =~ ^[Yy]$ ]]; then
                ${WORK_DIR}/install/uninstall.sh
                echo "  ✓ Automated uninstall complete"
            else
                echo "  Skipped automated uninstall"
                if [ "$AUTO_YES" = true ]; then
                    REPLY="y"
                else
                    read -p "  Delete kuadrant-system namespace manually? [y/N] " -n 1 -r
                    echo
                fi
                if [[ $REPLY =~ ^[Yy]$ ]]; then
                    oc delete namespace kuadrant-system
                    echo "  ✓ Removed kuadrant-system namespace"
                else
                    echo "  Skipped namespace removal"
                fi
            fi
        else
            if [ "$AUTO_YES" = true ]; then
                REPLY="y"
            else
                read -p "  Delete kuadrant-system namespace? [y/N] " -n 1 -r
                echo
            fi
            if [[ $REPLY =~ ^[Yy]$ ]]; then
                # Delete subscription first
                oc delete subscription kuadrant-operator -n kuadrant-system --ignore-not-found=true 2>/dev/null || true
                # Delete CSVs
                oc delete csv --all -n kuadrant-system --ignore-not-found=true 2>/dev/null || true
                # Wait for pods to terminate
                oc wait --for=delete pod --all -n kuadrant-system --timeout=60s 2>/dev/null || true
                # Delete namespace
                oc delete namespace kuadrant-system
                echo "  ✓ Removed kuadrant-system namespace"
            else
                echo "  Skipped namespace removal"
            fi
        fi
    else
        echo "  Namespace kuadrant-system not found"
    fi

    # Remove ImageDigestMirrorSets
    echo ""
    echo "  Checking for ImageDigestMirrorSets..."
    IDMS_COUNT=$(oc get imagedigestmirrorset -o name 2>/dev/null | wc -l || echo "0")
    IDMS_REMOVED=false
    if [ "$IDMS_COUNT" -gt 0 ]; then
        echo "  Found ${IDMS_COUNT} ImageDigestMirrorSet(s)"
        if [ "$AUTO_YES" = false ]; then
            echo "  WARNING: This will remove ALL ImageDigestMirrorSets on the cluster"
            echo "           Removal triggers control plane restart (cluster may be briefly unstable)"
            read -p "  Remove all ImageDigestMirrorSets? [y/N] " -n 1 -r
            echo
        else
            REPLY="y"
        fi
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            oc delete imagedigestmirrorset --all
            echo "  ✓ Removed ImageDigestMirrorSets"
            IDMS_REMOVED=true
        else
            echo "  Skipped ImageDigestMirrorSet removal"
        fi
    else
        echo "  No ImageDigestMirrorSets found"
    fi

    # Remove ImageTagMirrorSets
    echo ""
    echo "  Checking for ImageTagMirrorSets..."
    ITMS_COUNT=$(oc get imagetagmirrorset -o name 2>/dev/null | wc -l || echo "0")
    ITMS_REMOVED=false
    if [ "$ITMS_COUNT" -gt 0 ]; then
        echo "  Found ${ITMS_COUNT} ImageTagMirrorSet(s)"
        if [ "$AUTO_YES" = false ]; then
            echo "  WARNING: This will remove ALL ImageTagMirrorSets on the cluster"
            echo "           Removal triggers control plane restart (cluster may be briefly unstable)"
            read -p "  Remove all ImageTagMirrorSets? [y/N] " -n 1 -r
            echo
        else
            REPLY="y"
        fi
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            oc delete imagetagmirrorset --all
            echo "  ✓ Removed ImageTagMirrorSets"
            ITMS_REMOVED=true
        else
            echo "  Skipped ImageTagMirrorSet removal"
        fi
    else
        echo "  No ImageTagMirrorSets found"
    fi

    # If we removed IDMS or ITMS, wait for cluster to stabilize
    if [ "$IDMS_REMOVED" = true ] || [ "$ITMS_REMOVED" = true ]; then
        echo ""
        echo "  Waiting for cluster to stabilize after removing image mirror configuration..."

        # Check if single-node cluster
        NODE_COUNT=$(oc get nodes --no-headers 2>/dev/null | wc -l || echo "0")
        if [ "$NODE_COUNT" -eq 1 ]; then
            echo "  (Single-node cluster detected - API server may restart)"
        fi

        # Give cluster a moment to start processing
        sleep 5

        # Wait for openshift-apiserver to stabilize (with shorter timeout for cleanup)
        WAIT_TIMEOUT=180  # 3 minutes
        APISERVER_STABLE=false
        START_TIME=$(date +%s)

        while [ $(($(date +%s) - START_TIME)) -lt $WAIT_TIMEOUT ]; do
            APISERVER_AVAILABLE=$(oc get co openshift-apiserver -o jsonpath='{.status.conditions[?(@.type=="Available")].status}' 2>/dev/null)
            APISERVER_PROGRESSING=$(oc get co openshift-apiserver -o jsonpath='{.status.conditions[?(@.type=="Progressing")].status}' 2>/dev/null)

            if [ "$APISERVER_AVAILABLE" = "True" ] && [ "$APISERVER_PROGRESSING" = "False" ]; then
                APISERVER_STABLE=true
                echo "  ✓ Cluster stabilized"
                break
            fi

            # On single-node, auto-fix stuck pods
            if [ "$NODE_COUNT" -eq 1 ]; then
                PENDING_PODS=$(oc get pods -n openshift-apiserver --no-headers 2>/dev/null | grep -c "Pending" || echo "0")
                TERMINATING_PODS=$(oc get pods -n openshift-apiserver --no-headers 2>/dev/null | grep -c "Terminating" || echo "0")

                if [ "$PENDING_PODS" -gt 0 ] && [ "$TERMINATING_PODS" -gt 0 ]; then
                    echo "  Auto-fixing stuck apiserver pods..."
                    oc get pods -n openshift-apiserver --no-headers 2>/dev/null | grep "Terminating" | awk '{print $1}' | while read pod; do
                        oc delete pod -n openshift-apiserver "$pod" --force --grace-period=0 2>/dev/null || true
                    done
                    sleep 10
                fi
            fi

            sleep 5
        done

        if [ "$APISERVER_STABLE" = false ]; then
            echo "  ⚠ Cluster did not stabilize within ${WAIT_TIMEOUT}s (this is normal)"
            echo "    Background MachineConfig update may still be in progress"
        fi
    fi

    # Remove mirror-registry namespace
    echo ""
    echo "  Checking for mirror-registry namespace..."
    if oc get namespace ${MIRROR_NAMESPACE} &>/dev/null; then
        echo "  Found namespace ${MIRROR_NAMESPACE}"
        if [ "$AUTO_YES" = true ]; then
            REPLY="y"
        else
            echo "  This will delete the test Docker registry and all its data"
            read -p "  Remove mirror-registry namespace? [y/N] " -n 1 -r
            echo
        fi
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            oc delete namespace ${MIRROR_NAMESPACE}
            echo "  ✓ Removed ${MIRROR_NAMESPACE} namespace"
        else
            echo "  Skipped namespace removal"
        fi
    else
        echo "  Namespace ${MIRROR_NAMESPACE} not found"
    fi

    # Re-enable default OperatorHub sources if they were disabled
    echo ""
    echo "  Checking OperatorHub default sources..."
    SOURCES_DISABLED=$(oc get operatorhub cluster -o jsonpath='{.spec.disableAllDefaultSources}' 2>/dev/null || echo "false")

    if [ "$SOURCES_DISABLED" = "true" ]; then
        echo "  Default OperatorHub sources are currently disabled"
        if [ "$AUTO_YES" = true ]; then
            REPLY="y"
        else
            read -p "  Re-enable default sources (Red Hat Operators, Community Operators, etc.)? [y/N] " -n 1 -r
            echo
        fi
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            oc patch operatorhub cluster --type json \
                -p '[{"op": "replace", "path": "/spec/disableAllDefaultSources", "value": false}]'
            echo "  ✓ Default OperatorHub sources re-enabled"
        else
            echo "  Skipped re-enabling default sources"
        fi
    else
        echo "  Default OperatorHub sources are enabled (no change needed)"
    fi

    echo ""
    echo "✓ Cluster cleanup complete"
fi

# Local Files Cleanup
echo ""
echo "==> Cleaning up local files"

if [ -d "$WORK_DIR" ]; then
    # Calculate size
    SIZE=$(du -sh "$WORK_DIR" 2>/dev/null | cut -f1 || echo "unknown")
    echo "  Found working directory: ${WORK_DIR} (${SIZE})"

    if [ "$AUTO_YES" = false ]; then
        # List contents
        echo "  Contents:"
        ls -lh "${WORK_DIR}" 2>/dev/null | tail -n +2 | awk '{printf "    %s  %s\n", $5, $9}' || echo "    (empty or inaccessible)"

        echo ""
        read -p "  Remove ${WORK_DIR}? [y/N] " -n 1 -r
        echo
    else
        REPLY="y"
    fi
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        rm -rf "$WORK_DIR"
        echo "  ✓ Removed ${WORK_DIR}"
    else
        echo "  Skipped local files removal"
    fi
else
    echo "  Working directory ${WORK_DIR} not found"
fi

# Network Connectivity Cleanup
if [ "$CLUSTER_ACCESSIBLE" = true ]; then
    echo ""
    echo "==> Restoring network connectivity"

    # Get node name
    NODE_NAME=$(oc get nodes -o name | head -1 | cut -d'/' -f2)

    if [ -z "$NODE_NAME" ]; then
        echo "  ⚠ Could not determine node name (skipping network restore)"
    else
        # Test if cluster is disconnected (check external connectivity)
        echo "  Checking current connectivity status..."
        if oc debug -n default node/${NODE_NAME} -- chroot /host timeout 5 curl -I https://quay.io 2>/dev/null > /dev/null 2>&1; then
            echo "  ✓ Cluster already connected to external network"
        else
            echo "  ⚠ Cluster disconnected - restoring connectivity..."

            # Check if iptables backup exists (indicates disconnect.sh was used)
            HAS_BACKUP=$(oc debug -n default node/${NODE_NAME} -- chroot /host test -f /tmp/iptables-before-disconnect.rules 2>/dev/null && echo "yes" || echo "no")

            if [ "$HAS_BACKUP" = "yes" ]; then
                echo "  Restoring original iptables rules..."
                oc debug -n default node/${NODE_NAME} -- chroot /host /bin/bash -c "
                    iptables-restore < /tmp/iptables-before-disconnect.rules 2>/dev/null
                    rm -f /tmp/iptables-before-disconnect.rules
                " 2>/dev/null || echo "  ⚠ Failed to restore iptables (may already be restored)"
            else
                echo "  Flushing iptables OUTPUT chain..."
                oc debug -n default node/${NODE_NAME} -- chroot /host /bin/bash -c "
                    iptables -F OUTPUT 2>/dev/null
                    iptables -P OUTPUT ACCEPT 2>/dev/null
                " 2>/dev/null || echo "  ⚠ Failed to flush iptables"
            fi

            # Verify reconnection worked
            sleep 2
            if oc debug -n default node/${NODE_NAME} -- chroot /host timeout 5 curl -I https://quay.io 2>/dev/null > /dev/null 2>&1; then
                echo "  ✓ Cluster reconnected to external network"
            else
                echo "  ⚠ Cluster may still be disconnected (check manually)"
            fi
        fi
    fi
fi

echo ""
echo "=========================================="
echo "✓ Cleanup Complete"
echo "=========================================="
echo ""

if [ "$CLUSTER_ACCESSIBLE" = true ]; then
    REMAINING_IDMS=$(oc get imagedigestmirrorset --no-headers 2>/dev/null | wc -l || echo "0")
    REMAINING_ITMS=$(oc get imagetagmirrorset --no-headers 2>/dev/null | wc -l || echo "0")

    if [ "$REMAINING_IDMS" -eq 0 ] && [ "$REMAINING_ITMS" -eq 0 ]; then
        echo "Cluster restored to normal operation:"
        echo "  ✓ All image pulls use original registries"
        echo "  ✓ Disconnected mode disabled"
    else
        echo "Image pull behavior:"
        echo "  ⚠ Some mirror sets still present"
        echo "    IDMS: ${REMAINING_IDMS} | ITMS: ${REMAINING_ITMS}"
        if [ "$REMAINING_IDMS" -gt 0 ] || [ "$REMAINING_ITMS" -gt 0 ]; then
            echo ""
            echo "  MachineConfigPool updating (may take 10-20 minutes)"
            echo "  Monitor: oc get mcp"
        fi
    fi
else
    echo "Cluster not accessible - local files cleaned up only"
fi
echo ""
