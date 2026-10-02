#!/usr/bin/env bash
# Manual, one-time, per-gateway step used to prototype the
# Gateway.spec.infrastructure.parametersRef approach described in
# poc/ecds-server/README.md ("Researching and confirming the fix").
# Not automated by the operator yet - see the README's "Open follow-ups".
set -euo pipefail

kubectl apply -f "$(dirname "$0")/05-ecds-bootstrap-configmaps.yaml"

kubectl patch gateway kuadrant-ingressgateway -n gateway-system --type=merge -p \
  '{"spec":{"infrastructure":{"parametersRef":{"group":"","kind":"ConfigMap","name":"kuadrant-ingressgateway-params"}}}}'
