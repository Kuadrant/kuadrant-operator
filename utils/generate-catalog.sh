#!/usr/bin/env bash

# Generate OLM catalog file

set -euo pipefail

### CONSTANTS
# Used as well in the subscription object
DEFAULT_CHANNEL=stable
###

OPM="${1?:Error \$OPM not set. Bye}"
YQ="${2?:Error \$YQ not set. Bye}"
BUNDLE_IMG="${3?:Error \$BUNDLE_IMG not set. Bye}"
CATALOG_FILE="${4?:Error \$CATALOG_FILE not set. Bye}"
CHANNELS="${5:-$DEFAULT_CHANNEL}"

CATALOG_FILE_BASEDIR="$(realpath "$(dirname "${CATALOG_FILE}")")"
CATALOG_BASEDIR="$(realpath "$(dirname "${CATALOG_FILE_BASEDIR}")")"

TMP_DIR=$(mktemp -d)

"${OPM}" render "${BUNDLE_IMG}" --output=yaml >> "${TMP_DIR}/kuadrant-operator-bundle.yaml"

mkdir -p "${CATALOG_FILE_BASEDIR}"
touch "${CATALOG_FILE}"

###
# Kuadrant Operator
###
# Add the package
"${OPM}" init kuadrant-operator --default-channel="${CHANNELS}" --output yaml >> "${CATALOG_FILE}"
# Add a bundles to the Catalog
cat "${TMP_DIR}/kuadrant-operator-bundle.yaml" >> "${CATALOG_FILE}"
# Add a channel entry for the bundle
NAME=$("${YQ}" eval '.name' "${TMP_DIR}/kuadrant-operator-bundle.yaml") \
CHANNELS="${CHANNELS}" \
    "${YQ}" eval '(.entries[0].name = strenv(NAME)) | (.name = strenv(CHANNELS))' "${CATALOG_BASEDIR}/kuadrant-operator-channel-entry.yaml" >> "${CATALOG_FILE}"

rm -rf "${TMP_DIR}"
