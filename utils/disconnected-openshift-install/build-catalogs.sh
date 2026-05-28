#!/usr/bin/env bash

set -euo pipefail

# Script to build and push all Kuadrant operator images to a container registry
#
# This script builds all four Kuadrant operators (authorino, limitador, dns, kuadrant)
# along with their OLM bundles and a combined catalog, then pushes all images to the
# specified registry. All images use digest-based references for disconnected/air-gapped
# OpenShift cluster installations.
#
# IMPORTANT: This script PUSHES images to the registry - ensure you have push access
#            and are logged in before running.
#
# Usage:
#   ./utils/disconnected-openshift-install/build-catalogs.sh [IMAGE_TAG]
#
# Arguments:
#   IMAGE_TAG  - Optional image tag (default: dev)
#
# Environment Variables:
#   REGISTRY        - Container registry (default: quay.io)
#   ORG             - Organization/namespace (default: kuadrant)
#   IMAGE_TAG       - Image tag for all operators
#
# Examples:
#   # Build and push with default tag
#   ./utils/disconnected-openshift-install/build-catalogs.sh
#
#   # Build and push with custom tag
#   IMAGE_TAG=v0.11.0 ./utils/disconnected-openshift-install/build-catalogs.sh
#
#   # Build and push to custom registry
#   REGISTRY=registry.example.com ORG=myorg IMAGE_TAG=v1.0.0 ./utils/disconnected-openshift-install/build-catalogs.sh
#
# Requirements:
#   - Must be run from kuadrant-operator directory
#   - Requires authorino-operator, limitador-operator, dns-operator as sibling directories
#   - Requires registry login (e.g., docker login quay.io)
#   - Requires make, docker/podman, operator-sdk, opm, yq
#
# Output:
#   Builds and pushes 10 images total:
#     - 1 CoreDNS image
#     - 4 operator images (with digest references)
#     - 4 operator bundles (with digest references in CSV)
#     - 1 combined catalog (File-Based Catalog with digest references)

# Configuration
IMAGE_TAG="${IMAGE_TAG:-${1:-dev}}"
REGISTRY="${REGISTRY:-quay.io}"
ORG="${ORG:-kuadrant}"

echo "=========================================="
echo "Building Kuadrant Disconnected Catalog"
echo "=========================================="
echo ""
echo "Configuration:"
echo "  Registry: ${REGISTRY}"
echo "  Organization: ${ORG}"
echo "  Image Tag: ${IMAGE_TAG}"
echo "  Workspace: $(pwd)"
echo ""

# Check we're in the kuadrant-operator directory
if [ ! -f "Makefile" ] || [ ! -d "../authorino-operator" ]; then
    echo "ERROR: This script must be run from the kuadrant-operator directory"
    echo "       with authorino-operator, limitador-operator, and dns-operator"
    echo "       as sibling directories."
    exit 1
fi

# Function to build an operator
build_operator() {
    local OPERATOR_NAME=$1
    local OPERATOR_DIR=$2
    local BUILD_CMD=$3

    echo ""
    echo "=========================================="
    echo "Building ${OPERATOR_NAME}"
    echo "=========================================="
    echo ""

    if [ ! -d "$OPERATOR_DIR" ]; then
        echo "ERROR: ${OPERATOR_DIR} not found"
        exit 1
    fi

    cd "$OPERATOR_DIR"

    echo "Running: $BUILD_CMD"
    echo ""

    eval "$BUILD_CMD"

    if [ $? -eq 0 ]; then
        echo ""
        echo "✓ ${OPERATOR_NAME} build complete"
    else
        echo ""
        echo "✗ ${OPERATOR_NAME} build failed"
        exit 1
    fi

    cd - > /dev/null
}

START_TIME=$(date +%s)

# Build CoreDNS image first (required by dns-operator)
echo ""
echo "=========================================="
echo "Building CoreDNS"
echo "=========================================="
echo ""

COREDNS_IMAGE="${REGISTRY}/${ORG}/coredns-kuadrant:${IMAGE_TAG}"

# Build CoreDNS using dns-operator's make targets (which delegate to coredns plugin)
cd ../dns-operator

echo "Building and pushing CoreDNS image: ${COREDNS_IMAGE}"
make coredns-docker-build coredns-docker-push COREDNS_IMG=${COREDNS_IMAGE}

if [ $? -eq 0 ]; then
    echo "✓ CoreDNS build and push complete"
else
    echo "✗ CoreDNS build/push failed"
    exit 1
fi

cd - > /dev/null

# Build dependency operators first (they can be built in any order since they don't depend on each other)

build_operator \
    "authorino-operator" \
    "../authorino-operator" \
    "make docker-build docker-push bundle bundle-build bundle-push \
        IMAGE_TAG_BASE=${REGISTRY}/${ORG}/authorino-operator \
        IMAGE_TAG=${IMAGE_TAG} \
        USE_IMAGE_DIGESTS=true"

build_operator \
    "limitador-operator" \
    "../limitador-operator" \
    "make docker-build docker-push bundle bundle-build bundle-push \
        IMAGE_TAG_BASE=${REGISTRY}/${ORG}/limitador-operator \
        IMAGE_TAG=${IMAGE_TAG} \
        USE_IMAGE_DIGESTS=true"

build_operator \
    "dns-operator" \
    "../dns-operator" \
    "make docker-build docker-push bundle bundle-build bundle-push \
        IMAGE_TAG_BASE=${REGISTRY}/${ORG}/dns-operator \
        IMAGE_TAG=${IMAGE_TAG} \
        COREDNS_IMG=${REGISTRY}/${ORG}/coredns-kuadrant:${IMAGE_TAG} \
        USE_IMAGE_DIGESTS=true"

# Build kuadrant-operator last (depends on the other three bundles)
build_operator \
    "kuadrant-operator" \
    "." \
    "make docker-build docker-push bundle bundle-build bundle-push catalog catalog-build catalog-push \
        IMAGE_TAG=${IMAGE_TAG} \
        LIMITADOR_OPERATOR_BUNDLE_IMG=${REGISTRY}/${ORG}/limitador-operator-bundle:${IMAGE_TAG} \
        AUTHORINO_OPERATOR_BUNDLE_IMG=${REGISTRY}/${ORG}/authorino-operator-bundle:${IMAGE_TAG} \
        DNS_OPERATOR_BUNDLE_IMG=${REGISTRY}/${ORG}/dns-operator-bundle:${IMAGE_TAG} \
        USE_IMAGE_DIGESTS=true"

END_TIME=$(date +%s)
DURATION=$((END_TIME - START_TIME))

echo ""
echo "=========================================="
echo "✓ Build and Push Complete"
echo "=========================================="
echo ""
echo "Successfully built and pushed 10 images to ${REGISTRY}/${ORG}:"
echo ""
echo "CoreDNS:"
echo "  ${REGISTRY}/${ORG}/coredns-kuadrant:${IMAGE_TAG}"
echo ""
echo "Authorino Operator:"
echo "  ${REGISTRY}/${ORG}/authorino-operator:${IMAGE_TAG}"
echo "  ${REGISTRY}/${ORG}/authorino-operator-bundle:${IMAGE_TAG}"
echo ""
echo "Limitador Operator:"
echo "  ${REGISTRY}/${ORG}/limitador-operator:${IMAGE_TAG}"
echo "  ${REGISTRY}/${ORG}/limitador-operator-bundle:${IMAGE_TAG}"
echo ""
echo "DNS Operator:"
echo "  ${REGISTRY}/${ORG}/dns-operator:${IMAGE_TAG}"
echo "  ${REGISTRY}/${ORG}/dns-operator-bundle:${IMAGE_TAG}"
echo ""
echo "Kuadrant Operator:"
echo "  ${REGISTRY}/${ORG}/kuadrant-operator:${IMAGE_TAG}"
echo "  ${REGISTRY}/${ORG}/kuadrant-operator-bundle:${IMAGE_TAG}"
echo "  ${REGISTRY}/${ORG}/kuadrant-operator-catalog:${IMAGE_TAG}"
echo ""
echo "Build time: ${DURATION} seconds"
echo ""
