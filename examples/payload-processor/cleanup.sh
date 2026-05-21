#!/bin/bash
set -e

NAMESPACE="${NAMESPACE:-gateway-system}"
GATEWAY_NAME="${GATEWAY_NAME:-kuadrant-ingressgateway}"
ENVOYFILTER_FILE="${ENVOYFILTER_FILE:-envoyfilter-dynamic_metadata.yaml}"

echo "🧹 Cleaning up Golang Envoy Filter deployment"
echo "   Namespace: $NAMESPACE"
echo "   Gateway: $GATEWAY_NAME"
echo "   EnvoyFilter: $ENVOYFILTER_FILE"
echo ""

# Remove EnvoyFilter
echo "🗑️  Removing EnvoyFilter..."
kubectl delete -f "$ENVOYFILTER_FILE" --ignore-not-found=true
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
echo "🔍 Discovering gateway deployment..."
GATEWAY_DEPLOYMENT=$(kubectl get deployment -n "$NAMESPACE" \
  -l "gateway.networking.k8s.io/gateway-name=$GATEWAY_NAME" \
  -o jsonpath='{.items[0].metadata.name}')

if [ -z "$GATEWAY_DEPLOYMENT" ]; then
    echo "❌ Could not find deployment for gateway '$GATEWAY_NAME'. No restoration needed."
else
    echo "✅ Found deployment: $GATEWAY_DEPLOYMENT"
    # Remove golang-filter-lib volume and volumeMount using jq
    DEPLOYMENT_JSON=$(kubectl get deployment "$GATEWAY_DEPLOYMENT" -n "$NAMESPACE" -o json)
    echo "$DEPLOYMENT_JSON" | \
      jq 'del(.spec.template.spec.volumes[] | select(.name == "golang-filter-lib")) |
          del(.spec.template.spec.containers[0].volumeMounts[] | select(.name == "golang-filter-lib"))' | \
      kubectl apply -f -
    echo "✅ Deployment restored"

    # Wait for rollout
    echo ""
    echo "⏳ Waiting for rollout to complete..."
    kubectl rollout status deployment/"$GATEWAY_DEPLOYMENT" -n "$NAMESPACE" --timeout=5m
fi

echo ""
echo "✅ Cleanup complete!"
