#!/bin/bash
set -e

NAMESPACE="${NAMESPACE:-gateway-system}"
GATEWAY_NAME="${GATEWAY_NAME:-kuadrant-ingressgateway}"
ENVOYFILTER_FILE="${ENVOYFILTER_FILE:-envoyfilter-dynamic_metadata.yaml}"

echo "🔧 Deploying Golang Envoy Filter"
echo "   Namespace: $NAMESPACE"
echo "   Gateway: $GATEWAY_NAME"
echo "   EnvoyFilter: $ENVOYFILTER_FILE"
echo ""

# Step 1: Verify .so file exists and is ELF format
if [ ! -f extract_model.so ]; then
    echo "❌ extract_model.so not found. Run 'make build' first."
    exit 1
fi

if ! file extract_model.so | grep -q "ELF"; then
    echo "❌ extract_model.so is not in ELF format (Linux binary)"
    echo "   Run 'make clean && make build' to build for Linux"
    exit 1
fi

echo "✅ Found extract_model.so (ELF format)"
echo ""

# Step 2: Delete EnvoyFilter if it exists (prevents Envoy from loading missing .so file)
echo "🗑️  Removing EnvoyFilter if it exists..."
kubectl delete -f "$ENVOYFILTER_FILE" --ignore-not-found=true
echo "✅ EnvoyFilter removed (if it existed)"
echo ""

# Step 3: Discover the deployment
echo "🔍 Discovering gateway deployment..."
GATEWAY_DEPLOYMENT=$(kubectl get deployment -n "$NAMESPACE" \
  -l "gateway.networking.k8s.io/gateway-name=$GATEWAY_NAME" \
  -o jsonpath='{.items[0].metadata.name}')

if [ -z "$GATEWAY_DEPLOYMENT" ]; then
    echo "❌ Could not find deployment for gateway '$GATEWAY_NAME'"
    exit 1
fi

echo "✅ Found deployment: $GATEWAY_DEPLOYMENT"
echo ""

# Step 4: Check if volume already exists
VOLUME_EXISTS=$(kubectl get deployment "$GATEWAY_DEPLOYMENT" -n "$NAMESPACE" \
  -o jsonpath='{.spec.template.spec.volumes[?(@.name=="golang-filter-lib")].name}' 2>/dev/null || echo "")

if [ -z "$VOLUME_EXISTS" ]; then
    echo "🔧 Patching deployment to add emptyDir volume..."

    kubectl patch deployment "$GATEWAY_DEPLOYMENT" -n "$NAMESPACE" --type=json -p='[
      {
        "op": "add",
        "path": "/spec/template/spec/volumes/-",
        "value": {
          "name": "golang-filter-lib",
          "emptyDir": {}
        }
      },
      {
        "op": "add",
        "path": "/spec/template/spec/containers/0/volumeMounts/-",
        "value": {
          "name": "golang-filter-lib",
          "mountPath": "/var/lib/golang-filters"
        }
      }
    ]'

    echo "✅ Deployment patched"
    echo ""
    echo "⏳ Waiting for rollout to complete..."
    kubectl rollout status deployment/"$GATEWAY_DEPLOYMENT" -n "$NAMESPACE" --timeout=5m
else
    echo "✅ Volume already exists"
    echo ""
    echo "🔄 Restarting deployment to get fresh pods..."
    kubectl rollout restart deployment/"$GATEWAY_DEPLOYMENT" -n "$NAMESPACE"
    kubectl rollout status deployment/"$GATEWAY_DEPLOYMENT" -n "$NAMESPACE" --timeout=5m
    sleep 10 # Give pods a moment to stabilize before copying files
fi

# Step 5: Copy .so file to all running pods
echo ""
echo "🔍 Finding gateway pods..."
PODS=$(kubectl get pods -n "$NAMESPACE" \
  -l "gateway.networking.k8s.io/gateway-name=$GATEWAY_NAME" \
  --field-selector=status.phase=Running \
  -o jsonpath='{.items[*].metadata.name}')

if [ -z "$PODS" ]; then
    echo "❌ No running pods found"
    exit 1
fi

echo "✅ Found pods: $PODS"
echo ""

for POD in $PODS; do
    echo "📤 Copying extract_model.so to pod $POD..."
    kubectl cp extract_model.so "$NAMESPACE/$POD:/var/lib/golang-filters/extract_model.so" -c istio-proxy

    # Verify
    echo "🔍 Verifying file in pod..."
    kubectl exec -n "$NAMESPACE" "$POD" -c istio-proxy -- ls -lh /var/lib/golang-filters/extract_model.so
    echo "✅ Copied to $POD"
done

# Step 6: Apply EnvoyFilter (now that .so file is in place)
echo ""
echo "🎯 Applying EnvoyFilter..."
kubectl apply -f "$ENVOYFILTER_FILE"

echo ""
echo "✅ Deployment complete!"
echo ""
echo "🧪 Test with:"
echo "  curl -X POST http://your-gateway/api/endpoint \\"
echo "    -H 'Content-Type: application/json' \\"
echo "    -d '{\"model\":\"gpt-4\",\"messages\":[]}'"
echo ""
echo "📋 Check logs:"
echo "  kubectl logs -n $NAMESPACE -l gateway.networking.k8s.io/gateway-name=$GATEWAY_NAME -c istio-proxy --tail=50 | grep -E 'Pattern found|Exported to'"
echo ""
echo "🔍 Verify filter is loaded:"
echo "  kubectl exec -n $NAMESPACE -l gateway.networking.k8s.io/gateway-name=$GATEWAY_NAME -c istio-proxy -- ls -lh /var/lib/golang-filters/"
