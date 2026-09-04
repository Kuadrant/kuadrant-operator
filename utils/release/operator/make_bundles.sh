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

# Pass version to Make as-is, but map 0.0.0 to "latest" to match the
# Makefile's ifeq(latest,...) check. Without this, 0.0.0 would hit the
# semantic version branch and produce GITREF=v0.0.0 (a non-existent tag).
mod_version_for_make() {
  version=$1
  if [ "$version" == "0.0.0" ]; then
    echo "latest"
  else
    echo "$version"
  fi
}

echo "make bundle"
root=$(pwd)
cd $env

# Set desired dns-operator image
dns_operator_version=$(mod_version $DNS_OPERATOR_VERSION)
dns_operator_image="quay.io/kuadrant/dns-operator:$dns_operator_version"

# Set desired mcp-gateway images
mcp_gateway_version=$(mod_version $MCP_GATEWAY_VERSION)
mcp_gateway_image="ghcr.io/kuadrant/mcp-controller:$mcp_gateway_version"
mcp_gateway_broker_image="ghcr.io/kuadrant/mcp-gateway:$mcp_gateway_version"

# Set desired Wasm-shim image
wasm_shim=$(mod_version $WASM_SHIM_VERSION)
wasm_shim_image="quay.io/kuadrant/wasm-shim:$wasm_shim"

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

# Set up bundle dependency images (dns-operator no longer has an OLM bundle)
limitador_version=$(mod_version $LIMITADOR_OPERATOR_VERSION)
limitador_image=quay.io/kuadrant/limitador-operator-bundle:$limitador_version

authorino_version=$(mod_version $AUTHORINO_OPERATOR_VERSION)
authorino_image=quay.io/kuadrant/authorino-operator-bundle:$authorino_version

make bundle \
  BUNDLE_VERSION=$KUADRANT_OPERATOR_VERSION \
  BUNDLE_METADATA_OPTS="--channels $OLM_CHANNELS $default_channel_opt" \
  IMG=$operator_image \
  RELATED_IMAGE_DNS_OPERATOR=$dns_operator_image \
  RELATED_IMAGE_MCP_GATEWAY=$mcp_gateway_image \
  RELATED_IMAGE_MCP_GATEWAY_BROKER=$mcp_gateway_broker_image \
  RELATED_IMAGE_WASMSHIM=$wasm_shim_image \
  RELATED_IMAGE_DEVELOPERPORTAL=$developerportal_image \
  RELATED_IMAGE_CONSOLE_PLUGIN_LATEST=$consoleplugin_image \
  LIMITADOR_OPERATOR_BUNDLE_IMG=$limitador_image \
  AUTHORINO_OPERATOR_BUNDLE_IMG=$authorino_image \
  AUTHORINO_OPERATOR_VERSION=$(mod_version_for_make $AUTHORINO_OPERATOR_VERSION) \
  LIMITADOR_OPERATOR_VERSION=$(mod_version_for_make $LIMITADOR_OPERATOR_VERSION) \
  DNS_OPERATOR_VERSION=$(mod_version_for_make $DNS_OPERATOR_VERSION) \
  MCP_GATEWAY_VERSION=$(mod_version_for_make $MCP_GATEWAY_VERSION) \
  DEVELOPERPORTAL_VERSION=$(mod_version_for_make $DEVELOPERPORTAL_VERSION)

operator-sdk bundle validate $env/bundle
git diff --quiet -I'^    createdAt: ' ./bundle && git checkout ./bundle || true

# exit script and return to initail directory
cd $root
