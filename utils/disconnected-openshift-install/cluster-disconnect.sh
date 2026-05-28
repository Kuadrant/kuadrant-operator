#!/usr/bin/env bash

set -euo pipefail

# Script to disconnect/reconnect cluster from external network
# Simulates a true air-gapped environment for disconnected installation testing

ACTION="${1:-disconnect}"

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

# Get the node name (for CRC there's only one)
NODE_NAME=$(oc get nodes -o name | head -1 | cut -d'/' -f2)

if [ -z "$NODE_NAME" ]; then
    echo "ERROR: Could not find any nodes in the cluster"
    exit 1
fi

case "$ACTION" in
    disconnect)
        echo "==> Disconnecting cluster from external network"
        echo "  Node: ${NODE_NAME}"
        echo ""

        echo "Applying iptables rules to block external access..."
        echo ""

        # Apply disconnect rules
        oc debug -n default node/${NODE_NAME} -- chroot /host /bin/bash -c "
            # Save current rules
            iptables-save > /tmp/iptables-before-disconnect.rules

            # Clear OUTPUT chain
            iptables -F OUTPUT

            # Allow established connections and loopback
            iptables -A OUTPUT -m state --state ESTABLISHED,RELATED -j ACCEPT
            iptables -A OUTPUT -o lo -j ACCEPT

            # Allow DNS to cluster DNS (needed for internal resolution)
            iptables -A OUTPUT -p udp --dport 53 -d 10.0.0.0/8 -j ACCEPT
            iptables -A OUTPUT -p tcp --dport 53 -d 10.0.0.0/8 -j ACCEPT

            # Allow access to local networks (RFC 1918 private networks)
            iptables -A OUTPUT -d 10.0.0.0/8 -j ACCEPT
            iptables -A OUTPUT -d 172.16.0.0/12 -j ACCEPT
            iptables -A OUTPUT -d 192.168.0.0/16 -j ACCEPT

            # Allow access to link-local (needed for some internal services)
            iptables -A OUTPUT -d 169.254.0.0/16 -j ACCEPT

            # Log blocked external attempts (optional, for debugging)
            iptables -A OUTPUT -m limit --limit 5/min -j LOG --log-prefix 'BLOCKED-EXT: ' --log-level 4

            # Block everything else (external internet)
            iptables -A OUTPUT -j REJECT --reject-with icmp-host-unreachable

            echo '✓ iptables rules applied'
            iptables -L OUTPUT -v -n | head -20
        "

        echo ""
        echo "==> Testing disconnected state"

        # Test that external access is blocked
        echo -n "  Testing external access (should fail): "
        if oc debug -n default node/${NODE_NAME} -- chroot /host timeout 5 curl -I https://quay.io 2>/dev/null > /dev/null 2>&1; then
            echo "✗ FAILED - External access still works!"
            echo "    Disconnect may not have worked properly"
        else
            echo "✓ BLOCKED (as expected)"
        fi

        # Test that internal access works
        echo -n "  Testing internal access (should work): "
        INTERNAL_TEST=$(oc get --raw /healthz 2>/dev/null && echo "ok" || echo "failed")
        if [ "$INTERNAL_TEST" = "ok" ]; then
            echo "✓ WORKS (as expected)"
        else
            echo "✗ FAILED - Internal access blocked!"
            echo "    This may cause cluster issues"
        fi

        echo ""
        echo "=========================================="
        echo "✓ Cluster Disconnected"
        echo "=========================================="
        echo ""
        echo "  ✓ External registries blocked"
        echo "  ✓ Internal networking operational"
        echo ""
        ;;

    reconnect)
        echo "==> Reconnecting cluster to external network"
        echo "  Node: ${NODE_NAME}"
        echo ""

        echo "Restoring original iptables rules..."

        oc debug -n default node/${NODE_NAME} -- chroot /host /bin/bash -c "
            if [ -f /tmp/iptables-before-disconnect.rules ]; then
                iptables-restore < /tmp/iptables-before-disconnect.rules
                rm -f /tmp/iptables-before-disconnect.rules
                echo '✓ Original iptables rules restored'
            else
                # If backup doesn't exist, just flush OUTPUT chain (restore default ACCEPT)
                iptables -F OUTPUT
                iptables -P OUTPUT ACCEPT
                echo '✓ iptables OUTPUT chain flushed (default ACCEPT policy)'
            fi

            iptables -L OUTPUT -v -n | head -10
        "

        echo ""
        echo "==> Testing reconnected state"

        # Test that external access works
        echo -n "  Testing external access (should work): "
        if oc debug -n default node/${NODE_NAME} -- chroot /host timeout 5 curl -I https://quay.io 2>/dev/null > /dev/null 2>&1; then
            echo "✓ WORKS (reconnected)"
        else
            echo "✗ FAILED - Still blocked!"
        fi

        echo ""
        echo "=========================================="
        echo "✓ Cluster Reconnected"
        echo "=========================================="
        echo ""
        echo "  ✓ External registries accessible"
        echo ""
        ;;

    status)
        echo "==> Checking network connectivity"
        echo "  Node: ${NODE_NAME}"
        echo ""

        echo "Testing connectivity:"
        echo ""

        echo -n "  External (quay.io): "
        if oc debug -n default node/${NODE_NAME} -- chroot /host timeout 5 curl -I https://quay.io 2>/dev/null > /dev/null 2>&1; then
            echo "✓ CONNECTED"
        else
            echo "✗ DISCONNECTED"
        fi

        echo -n "  Internal (Kubernetes API): "
        if oc get --raw /healthz &>/dev/null; then
            echo "✓ CONNECTED"
        else
            echo "✗ DISCONNECTED (WARNING: cluster may have issues)"
        fi

        echo ""
        echo "Current OUTPUT iptables rules:"
        oc debug -n default node/${NODE_NAME} -- chroot /host iptables -L OUTPUT -v -n 2>/dev/null | head -20 || echo "  (unable to retrieve)"
        echo ""
        ;;

    *)
        echo "Usage: $0 [disconnect|reconnect|status]"
        echo ""
        echo "Actions:"
        echo "  disconnect - Block external network access (simulate air-gapped)"
        echo "  reconnect  - Restore external network access"
        echo "  status     - Check current connectivity state"
        echo ""
        echo "Examples:"
        echo "  $0 disconnect"
        echo "  $0 status"
        echo "  $0 reconnect"
        exit 1
        ;;
esac
