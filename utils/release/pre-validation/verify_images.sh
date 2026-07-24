#!/usr/bin/env bash

echo "Verifying dependency images exist on Quay"
file=$env/release.yaml
FAILED=0

check_image() {
  local repo=$1 tag=$2
  if curl -sf "https://quay.io/api/v1/repository/kuadrant/$repo/tag/?specificTag=$tag" | grep -q '"name"'; then
    echo "  OK: quay.io/kuadrant/$repo:$tag"
  else
    echo "  MISSING: quay.io/kuadrant/$repo:$tag"
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
  tag=$(mod_version "$version")
  echo "Checking $op $tag images..."
  check_image "$op" "$tag"
  check_image "$op-bundle" "$tag"
  check_image "$op-catalog" "$tag"
done

# Supporting components: check operator image only
components=("console-plugin" "wasm-shim" "developer-portal-controller")
for comp in "${components[@]}"; do
  version=$(yq "(.dependencies.\"$comp\")" "$file")
  tag=$(mod_version "$version")
  echo "Checking $comp $tag image..."
  check_image "$comp" "$tag"
done

if [[ $FAILED -ne 0 ]]; then
  echo "Some dependency images are missing on Quay. Release cannot proceed."
  exit 1
fi

echo "All dependency images verified on Quay"
