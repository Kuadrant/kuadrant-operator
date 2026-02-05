#!/usr/bin/env bash

source $env/utils/release/shared.sh

mod_version() {
  version=$1
  if [ "$version" == "0.0.0" ]; then
    echo "latest"
  else
    echo "v$version"
  fi
}

echo "make bundle"
root=$(pwd)
cd $env

# Set desired Wasm-shim image
wasm_shim=$(mod_version $WASM_SHIM_VERSION)
wasm_shim_image="oci://quay.io/kuadrant/wasm-shim:$wasm_shim"

# Set desired developer-portal-controller image
developerportal_version=$(mod_version $DEVELOPERPORTAL_VERSION)
developerportal_image="quay.io/kuadrant/developer-portal-controller:$developerportal_version"

# Set desired console-plugin image
consoleplugin_version=$(mod_version $CONSOLEPLUGIN_VERSION)
consoleplugin_image="quay.io/kuadrant/console-plugin:$consoleplugin_version"

# Set desired operator image
operator_image=quay.io/kuadrant/kuadrant-operator:$(mod_version $KUADRANT_OPERATOR_VERSION)

default_channel_opt="--default-channel $OLM_DEFAULT_CHANNEL"
if [[ "$OLM_DEFAULT_CHANNEL" == "null" ]]; then
  default_channel_opt=""
fi

# Set up bundle dependency images
limitador_version=$(mod_version $LIMITADOR_OPERATOR_VERSION)
limitador_image=quay.io/kuadrant/limitador-operator-bundle:$limitador_version

authorino_version=$(mod_version $AUTHORINO_OPERATOR_VERSION)
authorino_image=quay.io/kuadrant/authorino-operator-bundle:$authorino_version

dns_version=$(mod_version $DNS_OPERATOR_VERSION)
dns_image=quay.io/kuadrant/dns-operator-bundle:$dns_version

make bundle BUNDLE_VERSION=$KUADRANT_OPERATOR_VERSION BUNDLE_METADATA_OPTS="--channels $OLM_CHANNELS $default_channel_opt" IMG=$operator_image RELATED_IMAGE_WASMSHIM=$wasm_shim_image RELATED_IMAGE_DEVELOPERPORTAL=$developerportal_image RELATED_IMAGE_CONSOLE_PLUGIN_LATEST=$consoleplugin_image LIMITADOR_OPERATOR_BUNDLE_IMG=$limitador_image AUTHORINO_OPERATOR_BUNDLE_IMG=$authorino_image DNS_OPERATOR_BUNDLE_IMG=$dns_image

operator-sdk bundle validate $env/bundle
git diff --quiet -I'^    createdAt: ' ./bundle && git checkout ./bundle || true

# exit script and return to initail directory
cd $root
