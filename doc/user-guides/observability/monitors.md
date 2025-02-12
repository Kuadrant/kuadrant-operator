# Configure Observability of Gateway and Kuadrant components

## Overview

This guide includes steps to enable the Kuadrant observability feature.
This feature provides an integration between the Kuadrant components (including any gateways) and the [Prometheus Operator](https://github.com/prometheus-operator/prometheus-operator) if you have it installed in your cluster.
The feature works by creating a set of ServiceMonitors and PodMonitors, which instruct prometheus to scrape metrics from the Kuadrant and Gateway components.
The scraped metrics are used in the [Example Dashboards and Alerts](../../observability/examples.md).

## Prerequisites

- You have installed Kuadrant in a Kubernetes cluster.
- You have installed the [Prometheus Operator](https://github.com/prometheus-operator/prometheus-operator).

## Enabling Observability

To enable observability for Kuadrant and any gateways, set `enable: true` under the `observability` section in your Kuadrant CR:

```yaml
apiVersion: kuadrant.io/v1beta1
kind: Kuadrant
metadata:
  name: kuadrant-sample
spec:
  observability:
    enable: true
```

When enabled, Kuadrant creates ServiceMonitors and PodMonitors for its own components and in each gateway namespace (Envoy Gateway or Istio).
Monitors are also created in the corresponding gateway "system" namespace:

- Istio: `istio-system` namespace for the istiod pod
- Envoy Gateway:  `envoy-gateway-system` namespace for the envoy gateway pod

You can check all created monitors using this command:

```yaml
kubectl get servicemonitor,podmonitor -A -l kuadrant-observability=true
```

You can make changes to the monitors after they are created if you have need to.
Monitors will only ever be created or deleted, not updated or reverted.
If you decide the default monitors aren’t suitable, disable the feature by setting `enable: false` and create your own ServiceMonitor/PodMonitor definitions or configure Prometheus directly.
For more details on specific metrics, check out the [Metrics reference page](../../observability/metrics.md).
