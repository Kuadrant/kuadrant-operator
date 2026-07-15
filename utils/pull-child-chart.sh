#!/usr/bin/env bash

# Pull a child operator Helm chart from its upstream repo.
# Works with any chart structure (single manifests.yaml or multiple template files).
#
# If the chart has a native crds/ directory, it is used as-is.
# If not (CRDs inline in templates/manifests.yaml), they are extracted via yq.
#
# ClusterRoles are extracted from templates/manifests.yaml (simple charts) or
# downloaded from the repo's config/rbac/ with namePrefix applied (complex charts).
#
# Usage: pull-child-chart.sh ORG/REPO GITREF CHART_NAME OUTPUT_DIR
#
# Example: pull-child-chart.sh Kuadrant/dns-operator main dns-operator ./charts/dns-operator
# Example: pull-child-chart.sh Kuadrant/mcp-gateway main mcp-gateway ./charts/mcp-gateway

set -euo pipefail

REPO="${1?:Error: ORG/REPO not set}"
GITREF="${2?:Error: GITREF not set}"
CHART_NAME="${3?:Error: CHART_NAME not set}"
OUTPUT_DIR="${4?:Error: OUTPUT_DIR not set}"
YQ="${YQ:-yq}"

echo "Pulling ${CHART_NAME} chart from ${REPO}@${GITREF}..."

TMP=$(mktemp -d)
trap 'rm -rf "${TMP}"' EXIT

curl -sSL "https://api.github.com/repos/${REPO}/tarball/${GITREF}" | \
    tar -xz --wildcards -C "${TMP}" "*/charts/${CHART_NAME}/"

EXTRACTED=$(find "${TMP}" -type d -name "${CHART_NAME}" -path "*/charts/${CHART_NAME}" | head -1)
if [ -z "${EXTRACTED}" ]; then
    echo "Error: chart ${CHART_NAME} not found in ${REPO}@${GITREF}" >&2
    exit 1
fi

rm -rf "${OUTPUT_DIR}"
cp -r "${EXTRACTED}" "${OUTPUT_DIR}"

# If chart has no native crds/ directory, extract CRDs from templates/manifests.yaml
if [ ! -d "${OUTPUT_DIR}/crds" ] && [ -f "${OUTPUT_DIR}/templates/manifests.yaml" ]; then
    mkdir -p "${OUTPUT_DIR}/crds"
    ${YQ} 'select(.kind == "CustomResourceDefinition")' "${OUTPUT_DIR}/templates/manifests.yaml" \
        > "${OUTPUT_DIR}/crds/manifests.yaml"
    FILTERED=$(${YQ} 'select(.kind != "CustomResourceDefinition")' "${OUTPUT_DIR}/templates/manifests.yaml")
    echo "${FILTERED}" > "${OUTPUT_DIR}/templates/manifests.yaml"
fi

# Extract ClusterRoles for bundle/dependencies
mkdir -p "${OUTPUT_DIR}/static"
if [ -f "${OUTPUT_DIR}/templates/manifests.yaml" ]; then
    # Simple charts: ClusterRoles inline in manifests.yaml (already namePrefix'd by kustomize)
    ${YQ} 'select(.kind == "ClusterRole")' "${OUTPUT_DIR}/templates/manifests.yaml" \
        > "${OUTPUT_DIR}/static/clusterroles.yaml"
    FILTERED=$(${YQ} 'select(.kind != "ClusterRole")' "${OUTPUT_DIR}/templates/manifests.yaml")
    echo "${FILTERED}" > "${OUTPUT_DIR}/templates/manifests.yaml"
else
    # Complex charts: download raw ClusterRole from config/rbac/
    curl -sSfL "https://raw.githubusercontent.com/${REPO}/${GITREF}/config/rbac/role.yaml" \
        -o "${OUTPUT_DIR}/static/clusterroles.yaml"
fi

echo "  Chart pulled to ${OUTPUT_DIR}"
echo "  ClusterRoles → static/clusterroles.yaml"
