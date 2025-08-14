#!/usr/bin/env bash

set -euo pipefail

process_manifest() {
  if [ $# -eq 0 ]; then
    echo "🚨 Error: No manifest provided"
    cleanup
    exit 1
  fi
  # Process input
  local temp_file="extension-manifest.yaml"
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
      if [ "$manifest_source" != "$temp_file" ]; then
        cp "$manifest_source" "$temp_file" || {
          echo "🚨 Failed to copy local YAML file"
          rm -f "$temp_file"
          return 1
        }
      fi
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

# Install extension manifests

# Set default Kuadrant Operator Service Account name and Namespace if not provided
KUADRANT_SA_NAME=${KUADRANT_SA_NAME:-kuadrant-operator-controller-manager}
KUADRANT_NAMESPACE=${KUADRANT_NAMESPACE:-kuadrant-system}

# Process extension manifest
if [ -t 0 ]; then
  # Prompt for the path of the manifests.yaml file
  read -p "Please enter the path to the manifest yaml file (local or remote URL): " manifest_source
  # Check if the input is empty
  if [ -z "$manifest_source" ]; then
    echo "🚨 Error: No path provided."
    exit 1
  fi
  CLUSTER_ROLE_NAME=$(process_manifest "$manifest_source")
else
  echo "Reading from stdin"
  # Input is from stdin
  cat > extension-manifest.yaml || {
    echo "🚨 Failed to read from stdin"
    rm -f extension-manifest.yaml
    exit 1
  }
  CLUSTER_ROLE_NAME=$(process_manifest "extension-manifest.yaml")
fi

echo "Found CLUSTER_ROLE_NAME: $CLUSTER_ROLE_NAME"

# Apply extension manifest YAML file
echo "Applying extension manifest file..."
kubectl apply -f extension-manifest.yaml || {
  echo "🚨 Failed to apply extension YAML"
  rm -f extension-manifest.yaml
  exit 1
}

# Apply ClusterRoleBinding
echo "Applying ClusterRole $CLUSTER_ROLE_NAME to ServiceAccount $KUADRANT_SA_NAME ..."
kubectl apply -f -<<EOF
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: ${CLUSTER_ROLE_NAME}-binding
roleRef:
  name: ${CLUSTER_ROLE_NAME}
  kind: ClusterRole
subjects:
- kind: ServiceAccount
  name: ${KUADRANT_SA_NAME}
  namespace: ${KUADRANT_NAMESPACE}
EOF

# Cleanup temporary files
cleanup

echo "🎉 Extension manifest applied successfully"
