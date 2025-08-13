#!/usr/bin/env bash

set -euo pipefail

# Get absolute path for this script
SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )

source "$SCRIPT_DIR/shared.sh"

# Install extension manifests

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
