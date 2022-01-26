#!/bin/bash

set -euo pipefail

SCRIPTS_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

sed \
"s/%AUTHORINO_OPERATOR_BUNDLE_VERSION%/${AUTHORINO_OPERATOR_BUNDLE_VERSION}/g; \
s/%LIMITADOR_OPERATOR_BUNDLE_VERSION%/${LIMITADOR_OPERATOR_BUNDLE_VERSION}/g" \
${SCRIPTS_DIR}/dependencies-template.yaml
