#!/usr/bin/env bash

set -euo pipefail

# Get absolute path for this script
SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )

source "$SCRIPT_DIR/shared.sh"

# Uninstall extension manifests

# Set default Kuadrant Operator Service Account name and Namespace if not provided
KUADRANT_SA_NAME=${KUADRANT_SA_NAME:-kuadrant-operator-controller-manager}
KUADRANT_NAMESPACE=${KUADRANT_NAMESPACE:-kuadrant-system}

# Process extension manifest
if [ -t 0 ]; then
  # Input is from argument
  CLUSTER_ROLE_NAME=$(process_manifest "$1")
else
  # Input is from stdin
  CLUSTER_ROLE_NAME=$(process_manifest)
fi

echo "Found CLUSTER_ROLE_NAME: $CLUSTER_ROLE_NAME"

# Delete the ClusterRoleBinding
echo "Deleting ClusterRoleBinding..."
kubectl delete clusterrolebinding ${CLUSTER_ROLE_NAME}-binding || {
  echo "Warning: Failed to delete ClusterRoleBinding, continuing..."
}

# Delete the extension manifest resources
echo "Deleting extension manifest resources..."
kubectl delete -f extension-manifest.yaml || {
  echo "Warning: Failed to delete some resources, continuing..."
}

# Cleanup temporary files
cleanup

echo "🎉 Extension manifest deleted successfully"
