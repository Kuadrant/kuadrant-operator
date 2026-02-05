#!/usr/bin/env bash

# Access token will be required. Check to ensure that is it provied
if [[ -z "$GITHUB_TOKEN" ]]; then
  echo "GITHUB_TOKEN must be set"
fi
auth_header="-H Authorization: Bearer $GITHUB_TOKEN"

script_dir="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
source $script_dir/shared.sh

# Get latest release version
log "Getting 'latest' release version tag"
previous_tag_name=$(curl -L "https://api.github.com/repos/kuadrant/kuadrant-operator/releases/latest" -H "Accept: apllication/vnd.github+json" | yq '.tag_name')
log "'latest' released version is $previous_tag_name"

# Get current release tag
log "Getting this releases tag"
release_tag=$KUADRANT_OPERATOR_TAG
log "Release in progress for $KUADRANT_OPERATOR_TAG"

# Generate the release change log
log "Generate release change log"
payload=$(cat <<EOF
{"tag_name": "$release_tag","previous_tag_name": "$previous_tag_name"}
EOF
)

data=$(curl -L "https://api.github.com/repos/kuadrant/kuadrant-operator/releases/generate-notes" -X POST -H "Accept: apllication/vnd.github+json" -H "Authorization: Bearer $GITHUB_TOKEN" -H "X-GitHub-Api-Version: 2022-11-28" -d "$payload")

release_body_data=$(echo $data | yq '.body')
release_body="**This release enables installations of Authorino Operator v$AUTHORINO_OPERATOR_VERSION, Limitador Operator v$LIMITADOR_OPERATOR_VERSION, DNS Operator v$DNS_OPERATOR_VERSION, WASM Shim v$WASM_SHIM_VERSION, ConsolePlugin v$CONSOLEPLUGIN_VERSION and Developer Portal Controller v$DEVELOPERPORTAL_VERSION**$release_body_data"

if [[ $_log == "1" ]]; then
  log "releaseBody=$release_body"
fi

if [[ $dry_run == "0" ]]; then
  # For multiline strings, you may use a delimiter
  # https://docs.github.com/en/actions/writing-workflows/choosing-what-your-workflow-does/workflow-commands-for-github-actions#multiline-strings
  echo "releaseBody<<EOF" >> $GITHUB_ENV
  echo "$release_body" >> $GITHUB_ENV
  echo "EOF" >> $GITHUB_ENV
fi
