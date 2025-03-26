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

AUTHORINO_OPERATOR_VERSION=$(yq '.dependencies.authorino-operator' $env/release.yaml)
CONSOLEPLUGIN_VERSION=$(yq '.dependencies.console-plugin' $env/release.yaml)
DNS_OPERATOR_VERSION=$(yq '.dependencies.dns-operator' $env/release.yaml)
LIMITADOR_OPERATOR_VERSION=$(yq '.dependencies.limitador-operator' $env/release.yaml)
WASM_SHIM_VERSION=$(yq '.dependencies.wasm-shim' $env/release.yaml)
KUADRANT_OPERATOR_VERSION="$(yq '.kuadrant-operator.version' $env/release.yaml)"
KUADRANT_OPERATOR_TAG="v$KUADRANT_OPERATOR_VERSION"
OLM_CHANNELS="$(yq '.olm.channels | join(",")' $env/release.yaml)"
OLM_DEFAULT_CHANNEL="$(yq '.olm.default-channel' $env/release.yaml)"
