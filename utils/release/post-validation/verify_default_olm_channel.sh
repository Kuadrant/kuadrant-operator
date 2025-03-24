#!/usr/bin/env bash

source $env/utils/release/shared.sh

echo "Verifying OLM default channel"

if [[ "$OLM_DEFAULT_CHANNEL" == "null" ]]; then
  echo "OLM default channel is not set in release.yaml"
  bundle_default_channel=$(yq '.annotations | has("operators.operatorframework.io.bundle.channel.default.v1")' $env/bundle/metadata/annotations.yaml)
  if [[ "$bundle_default_channel" == "true" ]]; then
    >&2 echo "🚨 OLM default channel is set in bundle and should not be"
    exit 1
  fi
else
  echo "OLM default channel is set in release.yaml"
  bundle_default_channel=$(yq '.annotations."operators.operatorframework.io.bundle.channel.default.v1"' $env/bundle/metadata/annotations.yaml)
  if [[ "$bundle_default_channel" != "$OLM_DEFAULT_CHANNEL" ]]; then
    >&2 echo "🚨 OLM default channel in release.yaml ($OLM_DEFAULT_CHANNEL) does not match bundle annotations ($bundle_default_channel)"
    exit 1
  fi
fi

echo "✅ OLM default channel verified"
