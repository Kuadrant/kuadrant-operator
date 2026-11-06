#!/usr/bin/env bash
set -euo pipefail

: "${env:?env must be set to the release directory}"
source "$env/utils/release/shared.sh"
release_make_args

cd "$env" || exit 1
make helm-build "${RELEASE_MAKE_ARGS[@]}"
