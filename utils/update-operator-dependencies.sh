#!/usr/bin/env bash

set -euo pipefail

COMPONENT="${1?:Error \$COMPONENT not set. Bye}"
VERSION="${2?:Error \$VERSION not set. Bye}"

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
DEP_FILE="${SCRIPT_DIR}/../bundle/metadata/dependencies.yaml"

COMPONENT=$COMPONENT V=$VERSION \
  yq eval '(.dependencies[] | select(.value.packageName == strenv(COMPONENT)).value.version) = strenv(V)' -i $DEP_FILE
