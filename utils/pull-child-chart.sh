#!/usr/bin/env bash

# Pull a child operator Helm chart from its upstream repo and split
# the single manifests.yaml into crds/, static/, and templates/
# directories for use by the umbrella operator.
#
# Usage: pull-child-chart.sh ORG/REPO GITREF CHART_NAME OUTPUT_DIR
#
# Example: pull-child-chart.sh Kuadrant/dns-operator main dns-operator ./charts/dns-operator

set -euo pipefail

REPO="${1?:Error: ORG/REPO not set}"
GITREF="${2?:Error: GITREF not set}"
CHART_NAME="${3?:Error: CHART_NAME not set}"
OUTPUT_DIR="${4?:Error: OUTPUT_DIR not set}"
YQ="${YQ:-yq}"

BASE_URL="https://raw.githubusercontent.com/${REPO}/${GITREF}/charts/${CHART_NAME}"

echo "Pulling ${CHART_NAME} chart from ${REPO}@${GITREF}..."

rm -rf "${OUTPUT_DIR}"
mkdir -p "${OUTPUT_DIR}/crds" "${OUTPUT_DIR}/static" "${OUTPUT_DIR}/templates"

curl -sSfL "${BASE_URL}/Chart.yaml" -o "${OUTPUT_DIR}/Chart.yaml"
curl -sSfL "${BASE_URL}/values.yaml" -o "${OUTPUT_DIR}/values.yaml"

TMP=$(mktemp)
trap 'rm -f "${TMP}"' EXIT
curl -sSfL "${BASE_URL}/templates/manifests.yaml" -o "${TMP}"

${YQ} 'select(.kind == "CustomResourceDefinition")' "${TMP}" > "${OUTPUT_DIR}/crds/manifests.yaml"
${YQ} 'select(.kind == "ClusterRole")' "${TMP}" > "${OUTPUT_DIR}/static/clusterroles.yaml"
${YQ} 'select(.kind != "CustomResourceDefinition" and .kind != "ClusterRole")' "${TMP}" > "${OUTPUT_DIR}/templates/manifests.yaml"

echo "  Chart.yaml, values.yaml downloaded"
echo "  CRDs      → crds/manifests.yaml"
echo "  ClusterRoles → static/clusterroles.yaml"
echo "  Workloads → templates/manifests.yaml"
