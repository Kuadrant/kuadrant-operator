#!/usr/bin/env bash

script_dir="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"

if [[ -z "${ROOT}" ]]; then
	echo "[WARNING] env var ROOT not set, using $(pwd)"
	ROOT=$(pwd)
fi

source $script_dir/shared.sh

log "Loading Environment Variables"

releaseBody="**This release enables installations of Authorino Operator v$AUTHORINO_OPERATOR_VERSION, Limitador Operator v$LIMITADOR_OPERATOR_VERSION, DNS Operator v$DNS_OPERATOR_VERSION, WASM Shim v$WASM_SHIM_VERSION and ConsolePlugin $CONSOLEPLUGIN_URL**"
releaseBranch="release-$(echo "$KUADRANT_OPERATOR_TAG" | sed -E 's/^(v[0-9]+\.[0-9]+).*/\1/')"

prerelease=false
if [[ "$KUADRANT_OPERATOR_TAG" =~ [-+] ]]; then
	prerelease=true
fi

if [[ $_log == "1" ]]; then
	log "kuadrantOperatorTag=$KUADRANT_OPERATOR_TAG"
	log "releaseBody=$releaseBody"
	log "prerelease=$prerelease"
	log "releaseBranch=$releaseBranch"
fi

if [[ $dry_run == "0" ]]; then
	echo "kuadrantOperatorTag=$KUADRANT_OPERATOR_TAG" >> "$GITHUB_ENV"
	echo "releaseBody=$releaseBody" >> "$GITHUB_ENV"
	echo "prerelease=$prerelease" >> "$GITHUB_ENV"
	echo "releaseBranch=$releaseBranch" >> "$GITHUB_ENV"
fi
