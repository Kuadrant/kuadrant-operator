#!/bin/bash

helm repo add grafana https://grafana.github.io/helm-charts
helm repo update
helm install tempo grafana/tempo --namespace tempo --create-namespace

if [ "$ISTIO_INSTALL_SAIL" = true ]; then
  kubectl patch -n istio-system istio/default --type=merge -p '{"spec": {"values": {"meshConfig":{"defaultConfig":{"tracing":{}},"enableTracing":true},"global":{"proxy":{"logLevel": "info"}}}}}'
  kubectl patch -n istio-system istio/default --type=json -p '[{"op": "add", "path": "/spec/values/meshConfig/extensionProviders/-", "value": {"name": "jaeger-tempo","opentelemetry":{"service":"tempo.tempo.svc.cluster.local","port":4317}}}]'
else
  kubectl patch -n istio-system istiooperator/istiocontrolplane --type=merge -p '{"spec":{"meshConfig":{"defaultConfig":{"tracing":{}},"enableTracing":true},"values":{"global":{"proxy":{"logLevel": "info"}}}}}'
  kubectl patch -n istio-system istiooperator/istiocontrolplane --type=json -p '[{"op": "add", "path": "/spec/meshConfig/extensionProviders/-", "value": {"name": "jaeger-tempo","opentelemetry":{"service":"tempo.tempo.svc.cluster.local","port":4317}}}]'
fi

kubectl apply -f - <<EOF
apiVersion: telemetry.istio.io/v1alpha1
kind: Telemetry
metadata:
  name: mesh-default
  namespace: istio-system
spec:
  tracing:
  - providers:
    - name: jaeger-tempo
    randomSamplingPercentage: 100
EOF

# Scale down authorino operator & patch the authorino deployment to enable tracing
kubectl -n kuadrant-system scale deployment authorino-operator --replicas=0
kubectl -n kuadrant-system patch deployment authorino --type='json' -p='[{"op": "add", "path": "/spec/template/spec/containers/0/args/-", "value":"--tracing-service-endpoint=rpc://tempo.tempo.svc.cluster.local:4317"},{"op": "add", "path": "/spec/template/spec/containers/0/args/-", "value": "--tracing-service-insecure"}]'

# Scale down limitador operator & patch the limitador deployment to enable tracing
kubectl scale --replicas=0 deployments/limitador-operator-controller-manager -n kuadrant-system
kubectl patch -n kuadrant-system deployment/limitador-limitador --type=json -p '[{"op": "replace", "path": "/spec/template/spec/containers/0/command", "value": ["limitador-server","--rate-limit-headers","DRAFT_VERSION_03","--limit-name-in-labels","--http-port","8080","--rls-port","8081","-vvv","--tracing-endpoint","rpc://tempo.tempo.svc.cluster.local:4317","/home/limitador/etc/limitador-config.yaml","memory"]}]'
kubectl patch -n kuadrant-system deployment/limitador-limitador --type=json -p '[{"op": "replace", "path": "/spec/template/spec/containers/0/image", "value": "quay.io/kuadrant/limitador:tracing-otel"}]'

# Patch the wasm-shim image for tracing support
kubectl patch wasmplugin kuadrant-api-gateway -n kuadrant-system --type=merge -p '{"spec": {"url": "oci://quay.io/kuadrant/wasm-shim:latest"}}'
