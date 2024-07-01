#!/usr/bin/env bash

# Generate OLM catalog file

set -euo pipefail

OPM="${1?:Error \$OPM not set. Bye}"
YQ="${2?:Error \$YQ not set. Bye}"
BUNDLE_IMG="${3?:Error \$BUNDLE_IMG not set. Bye}"
REPLACES_VERSION="${4?:Error \$REPLACES_VERSION not set. Bye}"
LIMITADOR_OPERATOR_BUNDLE_IMG="${5?:Error \$LIMITADOR_OPERATOR_BUNDLE_IMG not set. Bye}"
AUTHORINO_OPERATOR_BUNDLE_IMG="${6?:Error \$AUTHORINO_OPERATOR_BUNDLE_IMG not set. Bye}"
DNS_OPERATOR_BUNDLE_IMG="${7?:Error \$DNS_OPERATOR_BUNDLE_IMG not set. Bye}"
CHANNELS="${8?:Error \$CHANNELS not set. Bye}"
CATALOG_FILE="${9?:Error \$CATALOG_FILE not set. Bye}"

CATALOG_FILE_BASEDIR="$( cd "$( dirname "$(realpath ${CATALOG_FILE})" )" && pwd )"
CATALOG_BASEDIR="$( cd "$( dirname "$(realpath ${CATALOG_FILE_BASEDIR})" )" && pwd )"

TMP_DIR=$(mktemp -d)

${OPM} render ${BUNDLE_IMG} --output=yaml >> ${TMP_DIR}/kuadrant-operator-bundle.yaml
${OPM} render ${LIMITADOR_OPERATOR_BUNDLE_IMG} --output=yaml >> ${TMP_DIR}/limitador-operator-bundle.yaml
${OPM} render ${AUTHORINO_OPERATOR_BUNDLE_IMG} --output=yaml >> ${TMP_DIR}/authorino-operator-bundle.yaml
${OPM} render ${DNS_OPERATOR_BUNDLE_IMG} --output=yaml >> ${TMP_DIR}/dns-operator-bundle.yaml

# Verify kuadrant operator bundle's limitador/authorino/dns references are the same
# as provided by LIMITADOR_OPERATOR_BUNDLE_IMG; AUTHORINO_OPERATOR_BUNDLE_IMG and DNS_OPERATOR_BUNDLE_IMG
LIMITADOR_VERSION=`${YQ} eval '.properties[] | select(.type == "olm.package") | .value.version' ${TMP_DIR}/limitador-operator-bundle.yaml`
AUTHORINO_VERSION=`${YQ} eval '.properties[] | select(.type == "olm.package") | .value.version' ${TMP_DIR}/authorino-operator-bundle.yaml`
DNS_VERSION=`${YQ} eval '.properties[] | select(.type == "olm.package") | .value.version' ${TMP_DIR}/dns-operator-bundle.yaml`
LIMITADOR_REFERENCED_VERSION=`${YQ} eval '.properties[] | select(.type == "olm.package.required") | select(.value.packageName == "limitador-operator").value.versionRange' ${TMP_DIR}/kuadrant-operator-bundle.yaml`
AUTHORINO_REFERENCED_VERSION=`${YQ} eval '.properties[] | select(.type == "olm.package.required") | select(.value.packageName == "authorino-operator").value.versionRange' ${TMP_DIR}/kuadrant-operator-bundle.yaml`
DNS_REFERENCED_VERSION=`${YQ} eval '.properties[] | select(.type == "olm.package.required") | select(.value.packageName == "dns-operator").value.versionRange' ${TMP_DIR}/kuadrant-operator-bundle.yaml`

if [[ "${LIMITADOR_VERSION}" != "${LIMITADOR_REFERENCED_VERSION}" ]]
then
    echo -e "\033[31m[ERROR] Referenced Limitador version is ${LIMITADOR_REFERENCED_VERSION}, but found ${LIMITADOR_VERSION} in the bundle \033[0m" >/dev/stderr
    exit 1
fi

if [[ "${AUTHORINO_VERSION}" != "${AUTHORINO_REFERENCED_VERSION}" ]]
then
    echo -e "\033[31mReferenced Limitador version is ${AUTHORINO_REFERENCED_VERSION}, but found ${AUTHORINO_VERSION} in the bundle \033[0m" >/dev/stderr
    exit 1
fi

if [[ "${DNS_VERSION}" != "${DNS_REFERENCED_VERSION}" ]]
then
    echo -e "\033[31mReferenced DNS version is ${DNS_REFERENCED_VERSION}, but found ${DNS_VERSION} in the bundle \033[0m" >/dev/stderr
    exit 1
fi

mkdir -p ${CATALOG_FILE_BASEDIR}
touch ${CATALOG_FILE}

###
# Limitador Operator
###
# Add the package
${OPM} init limitador-operator --default-channel=${CHANNELS} --output yaml >> ${CATALOG_FILE}
# Add a bundles to the Catalog
cat ${TMP_DIR}/limitador-operator-bundle.yaml >> ${CATALOG_FILE}
# Add a channel entry for the bundle
V=`${YQ} eval '.name' ${TMP_DIR}/limitador-operator-bundle.yaml` \
CHANNELS=${CHANNELS} \
    ${YQ} eval '(.entries[0].name = strenv(V)) | (.name = strenv(CHANNELS))' ${CATALOG_BASEDIR}/limitador-operator-channel-entry.yaml >> ${CATALOG_FILE}

###
# Authorino Operator
###
# Add the package
${OPM} init authorino-operator --default-channel=${CHANNELS} --output yaml >> ${CATALOG_FILE}
# Add a bundles to the Catalog
cat ${TMP_DIR}/authorino-operator-bundle.yaml >> ${CATALOG_FILE}
# Add a channel entry for the bundle
V=`${YQ} eval '.name' ${TMP_DIR}/authorino-operator-bundle.yaml` \
CHANNELS=${CHANNELS} \
    ${YQ} eval '(.entries[0].name = strenv(V)) | (.name = strenv(CHANNELS))' ${CATALOG_BASEDIR}/authorino-operator-channel-entry.yaml >> ${CATALOG_FILE}

###
# DNS Operator
###
# Add the package
${OPM} init dns-operator --default-channel=${CHANNELS} --output yaml >> ${CATALOG_FILE}
# Add a bundles to the Catalog
cat ${TMP_DIR}/dns-operator-bundle.yaml >> ${CATALOG_FILE}
# Add a channel entry for the bundle
V=`${YQ} eval '.name' ${TMP_DIR}/dns-operator-bundle.yaml` \
CHANNELS=${CHANNELS} \
    ${YQ} eval '(.entries[0].name = strenv(V)) | (.name = strenv(CHANNELS))' ${CATALOG_BASEDIR}/dns-operator-channel-entry.yaml >> ${CATALOG_FILE}

###
# Kuadrant Operator
###
# Add the package
${OPM} init kuadrant-operator --default-channel=${CHANNELS} --output yaml >> ${CATALOG_FILE}
# Add a bundles to the Catalog
cat ${TMP_DIR}/kuadrant-operator-bundle.yaml >> ${CATALOG_FILE}
# Add a channel entry for the bundle
NAME=`${YQ} eval '.name' ${TMP_DIR}/kuadrant-operator-bundle.yaml` \
REPLACES=kuadrant-operator.v${REPLACES_VERSION} \
CHANNELS=${CHANNELS} \
    ${YQ} eval '(.entries[0].name = strenv(NAME)) | (.entries[0].replaces = strenv(REPLACES)) | (.name = strenv(CHANNELS))' ${CATALOG_BASEDIR}/kuadrant-operator-channel-entry.yaml >> ${CATALOG_FILE}

rm -rf $TMP_DIR
