#!/usr/bin/env bash

# Pull a child operator Helm chart from its upstream repo.
# Works with any chart structure (single manifests.yaml or multiple template files).
#
# CRDs: extracted from templates/manifests.yaml if no native crds/ directory exists.
# ClusterRoles: for simple charts (single manifests.yaml), ClusterRoles are extracted
# into static/clusterroles.yaml and removed from templates.
#
# NOTE: Several post-processing hacks exist below for the mcp-gateway chart
# specifically, due to differences between its Helm chart conventions and the
# simple kustomize-generated charts used by the other child operators. These are
# marked as HACK and are for POC purposes only. In the real implementation, we
# should find a better approach to chart syncing — possibly reusing the same Helm
# rendering engine used internally by the operator to extract resources, rather
# than sed/yq post-processing of template files.
#
# Usage: pull-child-chart.sh ORG/REPO GITREF CHART_NAME OUTPUT_DIR

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

# Extract ClusterRoles into static/ for bundle/dependencies
mkdir -p "${OUTPUT_DIR}/static"
if [ -f "${OUTPUT_DIR}/templates/manifests.yaml" ]; then
    # Simple charts: ClusterRoles inline in manifests.yaml (already namePrefix'd)
    ${YQ} 'select(.kind == "ClusterRole")' "${OUTPUT_DIR}/templates/manifests.yaml" \
        > "${OUTPUT_DIR}/static/clusterroles.yaml"
    FILTERED=$(${YQ} 'select(.kind != "ClusterRole")' "${OUTPUT_DIR}/templates/manifests.yaml")
    echo "${FILTERED}" > "${OUTPUT_DIR}/templates/manifests.yaml"
fi

# HACK (POC only, mcp-gateway): The mcp-gateway chart uses Helm-templated RBAC
# in templates/rbac.yaml with ClusterRole names that differ from the kustomize
# source (config/rbac/role.yaml). We strip the ClusterRole (managed by the
# installer) and preserve only the ClusterRoleBinding. The static ClusterRole in
# config/dependencies/child-operators/ is manually maintained with the correct name.
# TODO: upstream should align ClusterRole naming between kustomize and Helm, or
# the sync process should use the Helm renderer to extract resources cleanly.
if [ -f "${OUTPUT_DIR}/templates/rbac.yaml" ]; then
    sed -n '/kind: ClusterRoleBinding/,$ p' "${OUTPUT_DIR}/templates/rbac.yaml" | \
        sed '1 i\apiVersion: rbac.authorization.k8s.io/v1' > "${TMP}/crb.yaml"
    if [ -s "${TMP}/crb.yaml" ]; then
        echo '{{- if .Values.controller.enabled }}' > "${OUTPUT_DIR}/templates/rbac.yaml"
        echo '---' >> "${OUTPUT_DIR}/templates/rbac.yaml"
        cat "${TMP}/crb.yaml" >> "${OUTPUT_DIR}/templates/rbac.yaml"
        echo "  HACK: Stripped ClusterRole from templates/rbac.yaml (mcp-gateway) — managed by installer"
    else
        rm "${OUTPUT_DIR}/templates/rbac.yaml"
        echo "  HACK: Removed templates/rbac.yaml (mcp-gateway) — managed externally"
    fi
fi

# HACK (POC only, limitador-operator): Uses the generic 'control-plane: controller-manager'
# selector which collides with kuadrant-operator in the same namespace. Patch it to use
# a unique selector. This MUST be fixed upstream in the limitador-operator chart and this
# hack removed — it will break upgrades if the Deployment already exists with the old selector.
if [ "${CHART_NAME}" = "limitador-operator" ] && [ -f "${OUTPUT_DIR}/templates/manifests.yaml" ]; then
    ${YQ} -i '
        (select(.kind == "Deployment").spec.selector.matchLabels."control-plane") = "limitador-operator-controller-manager" |
        (select(.kind == "Deployment").spec.template.metadata.labels."control-plane") = "limitador-operator-controller-manager"
    ' "${OUTPUT_DIR}/templates/manifests.yaml"
    echo "  HACK: Patched limitador-operator Deployment selector to avoid collision"
fi

echo "  Chart pulled to ${OUTPUT_DIR}"
