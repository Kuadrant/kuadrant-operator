#!/usr/bin/env bash

# Update component-charts/sync.yaml tracked-branches from release.yaml versions,
# then sync component Helm charts from upstream repos.

set -euo pipefail

RELEASE_FILE="$env/release.yaml"
SYNC_FILE="$env/component-charts/sync.yaml"

errors=()

if [[ ! -f "$RELEASE_FILE" ]]; then
  echo "ERROR: $RELEASE_FILE not found"
  exit 1
fi

if [[ ! -f "$SYNC_FILE" ]]; then
  echo "ERROR: $SYNC_FILE not found"
  exit 1
fi

if ! command -v yq &>/dev/null; then
  echo "ERROR: yq is required but not found in PATH"
  exit 1
fi

# Check: every sync component must exist in release.yaml dependencies
while IFS= read -r component; do
  dep_version=$(yq ".dependencies.\"$component\" // \"\"" "$RELEASE_FILE")
  if [[ -z "$dep_version" ]]; then
    errors+=("Component '$component' is in $SYNC_FILE but missing from $RELEASE_FILE dependencies")
  fi
done < <(yq '.components | keys | .[]' "$SYNC_FILE")

if [[ ${#errors[@]} -gt 0 ]]; then
  echo ""
  echo "=============================="
  echo "ERRORS"
  echo "=============================="
  for err in "${errors[@]}"; do
    echo "  ERROR: $err"
  done
  exit 1
fi

# Update tracked-branches in sync.yaml based on release.yaml versions
while IFS= read -r component; do
  version=$(yq ".dependencies.\"$component\" // \"\"" "$RELEASE_FILE")

  if [[ -z "$version" ]]; then
    echo "INFO: '$component' is in $SYNC_FILE but has no version in $RELEASE_FILE — skipping"
    continue
  fi

  if [[ "$version" == "0.0.0" ]]; then
    echo "WARN: '$component' version is 0.0.0 — tracked-branch not updated"
    continue
  fi

  expected_branch="release-${version%.*}"
  current_branch=$(yq ".components.\"$component\".tracked-branch" "$SYNC_FILE")

  if [[ "$current_branch" != "$expected_branch" ]]; then
    echo "Updating '$component' tracked-branch: '$current_branch' -> '$expected_branch'"
    yq -i ".components.\"$component\".tracked-branch = \"$expected_branch\"" "$SYNC_FILE"
  else
    echo "OK: '$component' tracked-branch already set to '$expected_branch'"
  fi
done < <(yq '.components | keys | .[]' "$SYNC_FILE")

# Warn about release.yaml deps not tracked in sync.yaml
while IFS='=' read -r dep version; do
  in_sync=$(yq ".components.\"$dep\" // \"\"" "$SYNC_FILE")
  if [[ -z "$in_sync" ]]; then
    echo "INFO: '$dep' ($version) is in $RELEASE_FILE but not tracked in $SYNC_FILE"
  fi
done < <(yq '.dependencies | to_entries | .[] | .key + "=" + .value' "$RELEASE_FILE")

echo ""
echo "Syncing component charts..."
make -C "$env" sync-component-charts
