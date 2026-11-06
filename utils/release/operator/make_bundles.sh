#!/usr/bin/env bash
set -euo pipefail

: "${env:?env must be set to the release directory}"
source "$env/utils/release/shared.sh"
release_make_args

# Caller metadata has priority; individual channel options override
# the release defaults only where explicitly supplied.
if [[ ${RELEASE_CALLER_OVERRIDES[BUNDLE_METADATA_OPTS]+present} ]]; then
  bundle_metadata_opts="${RELEASE_CALLER_OVERRIDES[BUNDLE_METADATA_OPTS]}"
else
  bundle_channels="${RELEASE_CALLER_OVERRIDES[CHANNELS]-$OLM_CHANNELS}"
  bundle_default_channel="${RELEASE_CALLER_OVERRIDES[DEFAULT_CHANNEL]-$OLM_DEFAULT_CHANNEL}"
  bundle_metadata_opts="--channels=$bundle_channels"
  if [[ "$bundle_default_channel" != "null" ]]; then
    bundle_metadata_opts+=" --default-channel=$bundle_default_channel"
  fi
fi

cd "$env" || exit 1
make bundle "${RELEASE_MAKE_ARGS[@]}" "BUNDLE_METADATA_OPTS=$bundle_metadata_opts"
operator-sdk bundle validate bundle
