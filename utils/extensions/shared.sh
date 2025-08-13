#!/usr/bin/env bash

# Install extension manifests

set -euo pipefail

process_manifest() {
  if [ $# -eq 0 ] && [ -t 0 ]; then
    echo "🚨 Error: No input provided (neither file/URL nor stdin)"
    cleanup
    exit 1
  fi

  local temp_file="extension-manifest.yaml"
  # Process input
  if [ $# -eq 1 ]; then
    local manifest_source="$1"

    if [[ $manifest_source =~ ^https?:// ]]; then
        curl -sf "$manifest_source" > "$temp_file" || {
          echo "🚨 Failed to download YAML file"
          rm -f "$temp_file"
          return 1
        }
    else
        if [ ! -f "$manifest_source" ]; then
          echo "🚨 Error: Local YAML file '$manifest_source' not found"
          return 1
        fi
        cp "$manifest_source" "$temp_file" || {
          echo "🚨 Failed to copy local YAML file"
          rm -f "$temp_file"
          return 1
        }
    fi
  else
    # Handle stdin
    cat > extension-manifest.yaml || {
      echo "🚨 Failed to read from stdin"
      rm -f extension-manifest.yaml
      exit 1
    }
  fi

  # Extract role name
  local role_name=$(yq e 'select(.kind == "ClusterRole") | .metadata.name' "$temp_file")
  if [ -z "$role_name" ]; then
    echo "🚨 Failed to extract Role name from YAML"
    rm -f "$temp_file"
    return 1
  fi

  echo "$role_name"
}

cleanup() {
  rm -f extension-manifest.yaml
}
