#!/usr/bin/env bash
# Applies all manifests in order. Assumes `make local-setup` has already been
# run from a kuadrant-operator checkout (kind cluster + Istio + Kuadrant
# operator + Gateway API CRDs + the kuadrant-ingressgateway Gateway).
set -euo pipefail

manifest_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/manifests"

for f in "$manifest_dir"/*.yaml; do
  echo "--- applying $f ---"
  kubectl apply -f "$f"
done
