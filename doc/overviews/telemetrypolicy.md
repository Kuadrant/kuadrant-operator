# Kuadrant TelemetryPolicy

The Kuadrant [TelemetryPolicy CRD](../reference/telemetrypolicy.md) allows you to add custom labels to [Kuadrant data plane component metrics](//TODO).

## How it works

Custom labels are defined as key-value pairs, where the key is the label name and the value is either a literal or a CEL expression.
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

## Optional request headers

Logging fields enrich Authorino's authorization decision records the same way as metric
labels, see the [reference](../reference/telemetrypolicy.md#loggingconfig). A logging field or
metric label that is a bare read of one request header is guarded automatically and yields
`""` when the header is absent; a compound expression that reads a header, and any label on
the rate-limit path, needs its own `has()` or `in` guard.
