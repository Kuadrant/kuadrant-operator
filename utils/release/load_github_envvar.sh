#!/usr/bin/env bash

script_dir="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"

source $script_dir/shared.sh

log "Loading Environment Variables"

releaseBranch="release-$(echo "$KUADRANT_OPERATOR_TAG" | sed -E 's/^v?([0-9]+\.[0-9]+).*/\1/')"

prerelease=false
if [[ "$KUADRANT_OPERATOR_TAG" =~ [-+] ]]; then
  prerelease=true
fi

if [[ $_log == "1" ]]; then
  log "kuadrantOperatorTag=$KUADRANT_OPERATOR_TAG"
  log "prerelease=$prerelease"
  log "releaseBranch=$releaseBranch"
  log "limitadorVersion=$LIMITADOR_VERSION"
  log "limitadorOperatorVersion=$LIMITADOR_OPERATOR_VERSION"
  log "authorinoVersion=$AUTHORINO_VERSION"
  log "authorinoOperatorVersion=$AUTHORINO_OPERATOR_VERSION"
  log "dnsOperatorVersion=$DNS_OPERATOR_VERSION"
  log "mcpGatewayVersion=$MCP_GATEWAY_VERSION"
  log "developerPortalVersion=$DEVELOPERPORTAL_VERSION"
fi

if [[ $dry_run == "0" ]]; then
  echo "kuadrantOperatorTag=$KUADRANT_OPERATOR_TAG" >> "$GITHUB_ENV"
  echo "prerelease=$prerelease" >> "$GITHUB_ENV"
  echo "releaseBranch=$releaseBranch" >> "$GITHUB_ENV"
  echo "limitadorVersion=$LIMITADOR_VERSION" >> "$GITHUB_ENV"
  echo "limitadorOperatorVersion=$LIMITADOR_OPERATOR_VERSION" >> "$GITHUB_ENV"
  echo "authorinoVersion=$AUTHORINO_VERSION" >> "$GITHUB_ENV"
  echo "authorinoOperatorVersion=$AUTHORINO_OPERATOR_VERSION" >> "$GITHUB_ENV"
  echo "dnsOperatorVersion=$DNS_OPERATOR_VERSION" >> "$GITHUB_ENV"
  echo "mcpGatewayVersion=$MCP_GATEWAY_VERSION" >> "$GITHUB_ENV"
  echo "developerPortalVersion=$DEVELOPERPORTAL_VERSION" >> "$GITHUB_ENV"
fi
