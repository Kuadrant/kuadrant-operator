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

When enabled, Kuadrant creates ServiceMonitors and PodMonitors for its own components in the same namespace as the Kuadrant operator.
Pod monitors are also created in each gateway namespace (Envoy Gateway or Istio) to scrape metrics from all gateways in the gateway namespace.


You can check all created monitors using this command:

```yaml
kubectl get servicemonitor,podmonitor -A -l kuadrant.io/observability=true
```

You can make changes to the monitors after they are created if you need to.
Monitors will only ever be created or deleted, not updated or reverted.
If you decide the default monitors aren’t suitable, disable the feature by setting `enable: false` and create your own ServiceMonitor/PodMonitor definitions or configure Prometheus directly.
For more details on specific metrics, check out the [Metrics reference page](../../observability/metrics.md).
