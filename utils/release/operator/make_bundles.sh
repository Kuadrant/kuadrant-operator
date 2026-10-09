#!/usr/bin/env bash
set -euo pipefail

: "${env:?env must be set to the release directory}"
source "$env/utils/release/shared.sh"
release_make_args

bundle_metadata_opts="--channels $OLM_CHANNELS"
if [[ "$OLM_DEFAULT_CHANNEL" != "null" ]]; then
  bundle_metadata_opts+=" --default-channel $OLM_DEFAULT_CHANNEL"
fi

cd "$env" || exit 1
make bundle "${RELEASE_MAKE_ARGS[@]}" "BUNDLE_METADATA_OPTS=$bundle_metadata_opts"
