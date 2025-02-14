#!/usr/bin/env bash

script_dir="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"

if [[ -z "${ROOT}" ]]; then
	echo "[WARNING] env var ROOT not set, using $(pwd)"
	ROOT=$(pwd)
fi

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

log "Loading Environment Variables"

authorinoOperatorVersion=$(yq '.dependencies.Authorino' $ROOT/release.yaml)
consolePluginURL=$(yq '.dependencies.Console_plugin' $ROOT/release.yaml)
dnsOperatorVersion=$(yq '.dependencies.DNS' $ROOT/release.yaml)
limitadorOperatorVersion=$(yq '.dependencies.Limitador' $ROOT/release.yaml)
wasmShimVersion=$(yq '.dependencies.Wasm_shim' $ROOT/release.yaml)

releaseBody="**This release enables installations of Authorino Operator v$authorinoOperatorVersion, Limitador Operator v$limitadorOperatorVersion, DNS Operator v$dnsOperatorVersion, WASM Shim v$wasmShimVersion and ConsolePlugin $consolePluginURL**"

kuadrantOperatorVersion=$(yq '.kuadrant.release' $ROOT/release.yaml)

prerelease=false
if [[ "$kuadrantOperatorVersion" == *"-"* ]]; then
	prerelease=true
fi

if [[ $_log == "1" ]]; then
	log $kuadrantOperatorVersion
	log "$releaseBody"
	log $prerelease
fi

if [[ $dry_run == "0" ]]; then
	echo "kuadrantOperatorVersion=$kuadrantOperatorVersion" >> "$GITHUB_ENV"
	echo "releaseBody=$releaseBody" >> "$GITHUB_ENV"
	echo "prerelease=$prerelease" >> "$GITHUB_ENV"
fi
