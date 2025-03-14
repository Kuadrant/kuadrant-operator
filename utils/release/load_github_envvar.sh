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

authorinoOperatorVersion=$(yq '.dependencies.authorino-operator' $ROOT/release.yaml)
consolePluginURL=$(yq '.dependencies.console-plugin' $ROOT/release.yaml)
dnsOperatorVersion=$(yq '.dependencies.dns-operator' $ROOT/release.yaml)
limitadorOperatorVersion=$(yq '.dependencies.limitador-operator' $ROOT/release.yaml)
wasmShimVersion=$(yq '.dependencies.wasm-shim' $ROOT/release.yaml)

releaseBody="**This release enables installations of Authorino Operator v$authorinoOperatorVersion, Limitador Operator v$limitadorOperatorVersion, DNS Operator v$dnsOperatorVersion, WASM Shim v$wasmShimVersion and ConsolePlugin $consolePluginURL**"

kuadratantOperatorTag="v$(yq '.kuadrant-operator.version' $ROOT/release.yaml)"
releaseBranch="release-$(echo "$kuadratantOperatorTag" | sed -E 's/^(v[0-9]+\.[0-9]+).*/\1/')"

prerelease=false
if [[ "$kuadratantOperatorTag" =~ [-+] ]]; then
	prerelease=true
fi

if [[ $_log == "1" ]]; then
	log "kuadratantOperatorTag=$kuadratantOperatorTag"
	log "releaseBody=$releaseBody"
	log "prerelease=$prerelease"
	log "releaseBranch=$releaseBranch"
fi

if [[ $dry_run == "0" ]]; then
	echo "kuadratantOperatorTag=$kuadratantOperatorTag" >> "$GITHUB_ENV"
	echo "releaseBody=$releaseBody" >> "$GITHUB_ENV"
	echo "prerelease=$prerelease" >> "$GITHUB_ENV"
	echo "releaseBranch=$releaseBranch" >> "$GITHUB_ENV"
fi
