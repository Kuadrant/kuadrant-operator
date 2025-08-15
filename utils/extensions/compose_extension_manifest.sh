#!/usr/bin/env bash

set -euo pipefail

get_cluster_role_name() {
  local extension_manifest="$1"
  local role_name=$(echo "$extension_manifest" | yq e 'select(.kind == "ClusterRole") | .metadata.name')

  if [ -z "$role_name" ]; then
      echo "🚨 Failed to extract ClusterRole name from YAML"
      return 1
    fi

    echo "$role_name"
}

concat_k8s_objects() {
  local obj1="$1"
  local obj2="$2"

  # Validate Kubernetes objects
  if ! echo "$obj1" | grep -q "apiVersion:" || ! echo "$obj2" | grep -q "apiVersion:"; then
      echo "🚨 Error: Both inputs must be valid Kubernetes objects" >&2
      return 1
  fi

  # Ensure proper indentation
  if [[ $obj1 != *" "* ]] || [[ $obj2 != *" "* ]]; then
      echo "🚨 Error: Kubernetes objects must be properly indented" >&2
      return 1
  fi

  # Concatenate with separator and blank line
  printf "%s\n---\n%s" "$obj1" "$obj2"
}


build_cluster_role_binding() {
  local cluster_role_name="$1"

  cat <<EOF
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: ${cluster_role_name}-binding
roleRef:
  name: ${cluster_role_name}
  kind: ClusterRole
subjects:
- kind: ServiceAccount
  name: ${KUADRANT_SA_NAME}
  namespace: ${KUADRANT_NAMESPACE}
EOF

}

process_extension_manifest() {

  if [ $# -eq 1 ]; then
    local manifest_source="$1"

    if [[ $manifest_source =~ ^https?:// ]]; then
        extension_manifest=$(curl -sf "$manifest_source") || {
          echo "🚨 Failed to download YAML file"
          return 1
        }
    else
      if [ ! -f "$manifest_source" ]; then
        echo "🚨 Error: Local YAML file '$manifest_source' not found"
        return 1
      else
        extension_manifest=$(cat "$manifest_source")
      fi
    fi
  else
    # Handle stdin
    extension_manifest=$(cat || { echo "🚨 Failed to read from stdin" >&2; exit 1; })
  fi

  # Get ClusterRole Name from manifest
  cluster_role_name=$(get_cluster_role_name "$extension_manifest")

  # Build the ClusterRoleBinding
  cluster_role_binding=$(build_cluster_role_binding "$cluster_role_name")


  # Return the processed manifest
  result=$(concat_k8s_objects "$extension_manifest" "$cluster_role_binding")
  if [ $? -eq 0 ]; then
      echo "$result"
  else
      echo "🚨 Failed to concatenate Kubernetes objects" >&2
  fi
}

# Build extension manifests

if [ $# -eq 0 ] && [ -t 0 ]; then
  echo "🚨 Error: No input provided (neither file/URL nor stdin)"
  exit 1
fi

# Set default Kuadrant Operator Service Account name and Namespace if not provided
KUADRANT_SA_NAME=${KUADRANT_SA_NAME:-kuadrant-operator-controller-manager}
KUADRANT_NAMESPACE=${KUADRANT_NAMESPACE:-kuadrant-system}

process_extension_manifest "$@"
