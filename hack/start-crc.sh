#!/usr/bin/env bash
# Start a CRC (OpenShift Local) VM for Kuadrant development.
#
# Assumes `crc setup` has already been run. Configures CPU and memory,
# starts the VM, and prints instructions for connecting.
#
# Prerequisites:
#   - crc installed (https://console.redhat.com/openshift/create/local)
#   - crc setup already completed
#   - A pull secret configured (prompted on first `crc start`)
#
# Usage:
#   ./hack/start-crc.sh              # Start with defaults (8 CPU, 16 GB RAM)
#   CRC_CPUS=6 ./hack/start-crc.sh   # Override CPU count
#   ./hack/start-crc.sh stop         # Stop the VM
#   ./hack/start-crc.sh status       # Check VM status

set -euo pipefail

CRC_CPUS="${CRC_CPUS:-8}"
CRC_MEMORY="${CRC_MEMORY:-20480}"
CRC_DISK="${CRC_DISK:-100}"
CRC_PULL_SECRET="${CRC_PULL_SECRET:-${HOME}/.crc/pull-secret}"

info()  { echo "[INFO]  $*"; }
error() { echo "[ERROR] $*" >&2; }

if ! command -v crc > /dev/null 2>&1; then
    error "crc not found. Install it from: https://console.redhat.com/openshift/create/local"
    exit 1
fi

ACTION="${1:-start}"

case "$ACTION" in
    stop)
        info "Stopping CRC..."
        crc stop
        info "CRC stopped."
        exit 0
        ;;
    status)
        crc status
        exit 0
        ;;
    start)
        ;;
    *)
        error "Unknown action: $ACTION. Use: start, stop, status"
        exit 1
        ;;
esac

if crc status 2>/dev/null | grep -q "Running"; then
    info "CRC is already running."
    crc status
    echo ""
    info "To connect: eval \$(crc oc-env)"
    exit 0
fi

info "Configuring CRC: ${CRC_CPUS} CPUs, ${CRC_MEMORY} MB RAM, ${CRC_DISK} GB disk, monitoring enabled"
crc config set cpus "$CRC_CPUS"
crc config set memory "$CRC_MEMORY"
crc config set disk-size "$CRC_DISK"
crc config set enable-cluster-monitoring true

if [[ ! -f "$CRC_PULL_SECRET" ]]; then
    error "Pull secret not found at: $CRC_PULL_SECRET"
    error "Download from: https://console.redhat.com/openshift/create/local"
    error "Save to ~/.crc/pull-secret or set CRC_PULL_SECRET=/path/to/file"
    exit 1
fi

info "Starting CRC..."
crc start --pull-secret-file "$CRC_PULL_SECRET"

echo ""
info "CRC is running."
info ""
info "  Connect:   eval \$(crc oc-env)"
info "  Console:   crc console"
info "  Credentials: crc console --credentials"
info ""
info "  Then run:  make crc-setup"
