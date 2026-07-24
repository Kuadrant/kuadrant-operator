#!/usr/bin/env bash

set -euo pipefail

COMPONENT="${1?:Error \$COMPONENT not set. Bye}"
VERSION="${2?:Error \$VERSION not set. Bye}"

# Skip for non-semver versions (e.g. "latest" in dev builds)
if [[ ! "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-.+)?$ ]]; then
  echo "Skipping $COMPONENT dependency update: version '$VERSION' is not semver"
  exit 0
fi

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
DEP_FILE="${SCRIPT_DIR}/../bundle/metadata/dependencies.yaml"

COMPONENT=$COMPONENT V=$VERSION \
  yq eval '(.dependencies[] | select(.value.packageName == strenv(COMPONENT)).value.version) = strenv(V)' -i $DEP_FILE
