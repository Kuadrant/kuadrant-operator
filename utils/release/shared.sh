#!/usr/bin/env bash

dry_run="0"
_log="0"

while [[ $# -gt 0 ]]; do
	echo "ARG: \"$1\""
	if [[ "$1" == "--dry_run" ]]; then
		dry_run="1"
	elif [[ "$1" == "--echo" ]]; then
		_log="1"
	else
		grep="$1"
	fi
	shift
done

log() {
	if [[ $dry_run == "1" ]]; then
		echo "[DRY_RUN]: $1"
	else
		echo "$1"
	fi
}

if [[ -z "${env}" ]]; then
	echo "[WARNING] env var env not set, using $(pwd)"
	env=$(pwd)
fi

if [ ! -f $env/release.yaml ]; then
  >&2 echo "🚨 File $env/release.yaml does not exist"
  exit 1
fi

# Preserve caller-supplied Make/environment variables before release.yaml
# parsing replaces component version names. The nested make receives these first.
declare -A RELEASE_CALLER_OVERRIDES=()
for release_var in VERSION IMG RELATED_IMAGE_AUTHORINO RELATED_IMAGE_AUTHORINO_OPERATOR RELATED_IMAGE_LIMITADOR RELATED_IMAGE_LIMITADOR_OPERATOR RELATED_IMAGE_DNS_OPERATOR RELATED_IMAGE_MCP_GATEWAY RELATED_IMAGE_MCP_GATEWAY_BROKER RELATED_IMAGE_WASMSHIM RELATED_IMAGE_DEVELOPERPORTAL RELATED_IMAGE_CONSOLE_PLUGIN_LATEST AUTHORINO_VERSION AUTHORINO_OPERATOR_VERSION LIMITADOR_VERSION LIMITADOR_OPERATOR_VERSION DNS_OPERATOR_VERSION MCP_GATEWAY_VERSION WASM_SHIM_VERSION DEVELOPERPORTAL_VERSION CONSOLEPLUGIN_VERSION BUNDLE_METADATA_OPTS CHANNELS DEFAULT_CHANNEL; do
  if [[ ${!release_var+x} ]]; then
    RELEASE_CALLER_OVERRIDES["$release_var"]="${!release_var}"
  fi
done
unset release_var

AUTHORINO_VERSION=$(yq '.dependencies.authorino' $env/release.yaml)
AUTHORINO_OPERATOR_VERSION=$(yq '.dependencies.authorino-operator' $env/release.yaml)
CONSOLEPLUGIN_VERSION=$(yq '.dependencies.console-plugin' $env/release.yaml)
DEVELOPERPORTAL_VERSION=$(yq '.dependencies.developer-portal-controller' $env/release.yaml)
DNS_OPERATOR_VERSION=$(yq '.dependencies.dns-operator' $env/release.yaml)
MCP_GATEWAY_VERSION=$(yq '.dependencies.mcp-gateway' $env/release.yaml)
LIMITADOR_VERSION=$(yq '.dependencies.limitador' $env/release.yaml)
LIMITADOR_OPERATOR_VERSION=$(yq '.dependencies.limitador-operator' $env/release.yaml)
WASM_SHIM_VERSION=$(yq '.dependencies.wasm-shim' $env/release.yaml)
KUADRANT_OPERATOR_VERSION="$(yq '.kuadrant-operator.version' $env/release.yaml)"
KUADRANT_OPERATOR_TAG="v$KUADRANT_OPERATOR_VERSION"
OLM_CHANNELS="$(yq '.olm.channels | join(",")' $env/release.yaml)"
OLM_DEFAULT_CHANNEL="$(yq '.olm.default-channel' $env/release.yaml)"

# Centralize release.yaml -> make command-line overrides for bundle and Helm.
# The Makefile remains mechanical; both release scripts use the same versions.
release_image_tag() {
  if [[ "$1" == "0.0.0" || "$1" == "latest" ]]; then
    printf '%s' latest
  else
    printf 'v%s' "$1"
  fi
}

release_make_version() {
  if [[ "$1" == "0.0.0" ]]; then
    printf '%s' latest
  else
    printf '%s' "$1"
  fi
}

# Preserve explicit caller options. When only a version is specified,
# allow the mechanical Makefile to derive the matching image from that version.
release_make_arg() {
  local name="$1" fallback="$2" derives_from="${3:-}"
  if [[ ${RELEASE_CALLER_OVERRIDES[$name]+present} ]]; then
    RELEASE_MAKE_ARGS+=("$name=${RELEASE_CALLER_OVERRIDES[$name]}")
  elif [[ -z "$derives_from" || ! ${RELEASE_CALLER_OVERRIDES[$derives_from]+present} ]]; then
    RELEASE_MAKE_ARGS+=("$name=$fallback")
  fi
}

release_make_args() {
  RELEASE_MAKE_ARGS=()
  release_make_arg VERSION "$KUADRANT_OPERATOR_VERSION"
  release_make_arg IMG "quay.io/kuadrant/kuadrant-operator:$(release_image_tag "$KUADRANT_OPERATOR_VERSION")" VERSION
  release_make_arg RELATED_IMAGE_AUTHORINO "quay.io/kuadrant/authorino:$(release_image_tag "$AUTHORINO_VERSION")" AUTHORINO_VERSION
  release_make_arg RELATED_IMAGE_AUTHORINO_OPERATOR "quay.io/kuadrant/authorino-operator:$(release_image_tag "$AUTHORINO_OPERATOR_VERSION")" AUTHORINO_OPERATOR_VERSION
  release_make_arg RELATED_IMAGE_LIMITADOR "quay.io/kuadrant/limitador:$(release_image_tag "$LIMITADOR_VERSION")" LIMITADOR_VERSION
  release_make_arg RELATED_IMAGE_LIMITADOR_OPERATOR "quay.io/kuadrant/limitador-operator:$(release_image_tag "$LIMITADOR_OPERATOR_VERSION")" LIMITADOR_OPERATOR_VERSION
  release_make_arg RELATED_IMAGE_DNS_OPERATOR "quay.io/kuadrant/dns-operator:$(release_image_tag "$DNS_OPERATOR_VERSION")" DNS_OPERATOR_VERSION
  release_make_arg RELATED_IMAGE_MCP_GATEWAY "ghcr.io/kuadrant/mcp-controller:$(release_image_tag "$MCP_GATEWAY_VERSION")" MCP_GATEWAY_VERSION
  release_make_arg RELATED_IMAGE_MCP_GATEWAY_BROKER "ghcr.io/kuadrant/mcp-gateway:$(release_image_tag "$MCP_GATEWAY_VERSION")" MCP_GATEWAY_VERSION
  release_make_arg RELATED_IMAGE_WASMSHIM "quay.io/kuadrant/wasm-shim:$(release_image_tag "$WASM_SHIM_VERSION")" WASM_SHIM_VERSION
  release_make_arg RELATED_IMAGE_DEVELOPERPORTAL "quay.io/kuadrant/developer-portal-controller:$(release_image_tag "$DEVELOPERPORTAL_VERSION")" DEVELOPERPORTAL_VERSION
  release_make_arg RELATED_IMAGE_CONSOLE_PLUGIN_LATEST "quay.io/kuadrant/console-plugin:$(release_image_tag "$CONSOLEPLUGIN_VERSION")" CONSOLEPLUGIN_VERSION
  release_make_arg AUTHORINO_VERSION "$(release_make_version "$AUTHORINO_VERSION")"
  release_make_arg AUTHORINO_OPERATOR_VERSION "$(release_make_version "$AUTHORINO_OPERATOR_VERSION")"
  release_make_arg LIMITADOR_VERSION "$(release_make_version "$LIMITADOR_VERSION")"
  release_make_arg LIMITADOR_OPERATOR_VERSION "$(release_make_version "$LIMITADOR_OPERATOR_VERSION")"
  release_make_arg DNS_OPERATOR_VERSION "$(release_make_version "$DNS_OPERATOR_VERSION")"
  release_make_arg MCP_GATEWAY_VERSION "$(release_make_version "$MCP_GATEWAY_VERSION")"
  release_make_arg WASM_SHIM_VERSION "$(release_make_version "$WASM_SHIM_VERSION")"
  release_make_arg DEVELOPERPORTAL_VERSION "$(release_make_version "$DEVELOPERPORTAL_VERSION")"
  release_make_arg CONSOLEPLUGIN_VERSION "$(release_make_version "$CONSOLEPLUGIN_VERSION")"
}
