# Kuadrant TelemetryPolicy

Kuadrant provides the [TelemetryPolicy CRD](../reference/telemetrypolicy.md), which enables custom metric labeling for [Kuadrant data plane component metrics](//TODO).

## How it works

Custom labels are defined as key-value pairs, where keys are the label names and values are literals or CEL expressions.
Using dynamically evaluated CEL expressions, you can label existing metrics with any data referenced by the [Well-known Attributes](https://github.com/Kuadrant/architecture/blob/main/rfcs/0002-well-known-attributes.md).

Only labels with CEL expressions that resolve successfully will be included.

## Examples

The following example configuration adds `user` and `group` labels for authenticated traffic.

```yaml
apiVersion: extensions.kuadrant.io/v1alpha1
kind: TelemetryPolicy
metadata:
  name: user-group
  namespace: gateway-system
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: kuadrant-ingressgateway
  metrics:
    default:
      labels:
        user: auth.identity.userid
        group: auth.identity.groups
```