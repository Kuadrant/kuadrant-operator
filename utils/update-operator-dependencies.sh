#!/usr/bin/env bash

set -euo pipefail

COMPONENT="${1?:Error \$COMPONENT not set. Bye}"
IMG="${2?:Error \$IMG not set. Bye}"

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
DEP_FILE="${SCRIPT_DIR}/../bundle/metadata/dependencies.yaml"
V="$( ${SCRIPT_DIR}/parse-bundle-version.sh opm yq ${IMG} )"

COMPONENT=$COMPONENT V=$V \
  yq eval '(.dependencies[] | select(.value.packageName == strenv(COMPONENT)).value.version) = strenv(V)' -i $DEP_FILE
