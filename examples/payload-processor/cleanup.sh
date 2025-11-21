#!/bin/bash
set -e

NAMESPACE="${NAMESPACE:-gateway-system}"
GATEWAY_DEPLOYMENT="${GATEWAY_DEPLOYMENT:-kuadrant-ingressgateway}"

echo "🧹 Cleaning up Golang Envoy Filter deployment"
echo "   Namespace: $NAMESPACE"
echo "   Gateway: $GATEWAY_DEPLOYMENT"
echo ""

# Remove EnvoyFilter
echo "🗑️  Removing EnvoyFilter..."
kubectl delete -f envoyfilter-simple.yaml --ignore-not-found=true
echo "✅ EnvoyFilter removed"

# Remove ConfigMap
echo ""
echo "🗑️  Removing ConfigMap..."
kubectl delete configmap golang-filter-lib -n "$NAMESPACE" --ignore-not-found=true
echo "✅ ConfigMap removed"

# Restore gateway deployment (remove volume and volumeMount)
echo ""
echo "🔧 Restoring gateway deployment..."

# Get current deployment spec
DEPLOYMENT_JSON=$(kubectl get deployment "$GATEWAY_DEPLOYMENT" -n "$NAMESPACE" -o json)

# Remove golang-filter-lib volume and volumeMount using jq
echo "$DEPLOYMENT_JSON" | \
  jq 'del(.spec.template.spec.volumes[] | select(.name == "golang-filter-lib")) |
      del(.spec.template.spec.containers[0].volumeMounts[] | select(.name == "golang-filter-lib"))' | \
  kubectl apply -f -

echo "✅ Deployment restored"

# Wait for rollout
echo ""
echo "⏳ Waiting for rollout to complete..."
kubectl rollout status deployment/"$GATEWAY_DEPLOYMENT" -n "$NAMESPACE" --timeout=5m

echo ""
echo "✅ Cleanup complete!"
