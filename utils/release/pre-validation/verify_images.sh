#!/usr/bin/env bash

set -euo pipefail

if [[ -z "${env:-}" ]]; then
  echo "ERROR: \$env is not set"
  exit 1
fi

if ! command -v crane &>/dev/null; then
  echo "WARNING: crane not installed, skipping image verification"
  exit 0
fi

echo "Verifying dependency images exist on Quay"
file=$env/release.yaml
FAILED=0

check_image() {
  local image=$1
  local output
  if output=$(crane digest "$image" 2>&1); then
    echo "  OK: $image ($output)"
  else
    echo "  MISSING: $image — $output"
    FAILED=1
  fi
}

mod_version() {
  local v=$1
  if [[ "$v" == "0.0.0" ]]; then
    echo "latest"
  else
    echo "v$v"
  fi
}

# Skip for development (any dependency at 0.0.0 means dev mode)
kuadrant_version=$(yq '.kuadrant-operator.version' "$file")
if [[ "$kuadrant_version" == "0.0.0" ]]; then
  echo "Skipping image verification for development version"
  exit 0
fi

# Operator dependencies: check operator + bundle + catalog images
operators=("authorino-operator" "limitador-operator" "dns-operator")
for op in "${operators[@]}"; do
  version=$(yq "(.dependencies.\"$op\")" "$file")
  if [[ "$version" == "0.0.0" ]]; then
    echo "Skipping $op (version 0.0.0)"
    continue
  fi
  tag=$(mod_version "$version")
  echo "Checking $op $tag images..."
  check_image "quay.io/kuadrant/$op:$tag"
  check_image "quay.io/kuadrant/$op-bundle:$tag"
  check_image "quay.io/kuadrant/$op-catalog:$tag"
done

# Supporting components: check image only
components=("console-plugin" "wasm-shim" "developer-portal-controller")
for comp in "${components[@]}"; do
  version=$(yq "(.dependencies.\"$comp\")" "$file")
  if [[ "$version" == "0.0.0" ]]; then
    echo "Skipping $comp (version 0.0.0)"
    continue
  fi
  tag=$(mod_version "$version")
  echo "Checking $comp $tag image..."
  check_image "quay.io/kuadrant/$comp:$tag"
done

if [[ $FAILED -ne 0 ]]; then
  echo "Some dependency images are missing on Quay. Release cannot proceed."
  exit 1
fi

echo "All dependency images verified on Quay"
