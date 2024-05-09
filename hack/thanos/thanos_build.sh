#!/usr/bin/env bash

# This script uses arg $1 (name of *.jsonnet file to use) to generate the manifests/*.yaml files.

set -e
set -x
# only exit with zero if all commands of the pipeline exit successfully
set -o pipefail

SCRIPT_DIR="$(dirname "${BASH_SOURCE[0]}")"
THANOS_MANIFESTS_KUSTOMIZATION_DIR=${SCRIPT_DIR}/../../config/thanos/manifests
THANOS_JSONNET_FILE=${SCRIPT_DIR}/thanos.jsonnet

JSONNET=${JSONNET:-jsonnet}
GOJSONTOYAML=${GOJSONTOYAML:-gojsontoyaml}

# Make sure to start with a clean 'manifests' dir
rm -rf ${THANOS_MANIFESTS_KUSTOMIZATION_DIR}
mkdir ${THANOS_MANIFESTS_KUSTOMIZATION_DIR}

# optional, but we would like to generate yaml, not json
${JSONNET} -J vendor -m ${THANOS_MANIFESTS_KUSTOMIZATION_DIR} "${THANOS_JSONNET_FILE}" | xargs -I{} sh -c "cat {} | ${GOJSONTOYAML} > {}.yaml; rm -f {}" -- {}
find ${THANOS_MANIFESTS_KUSTOMIZATION_DIR} -type f ! -name '*.yaml' -delete
