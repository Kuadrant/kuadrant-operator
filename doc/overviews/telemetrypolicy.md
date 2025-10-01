# Kuadrant TelemetryPolicy

The Kuadrant [TelemetryPolicy CRD](../reference/telemetrypolicy.md) allows you to add custom labels to [Kuadrant data plane component metrics](//TODO).

## How it works

Custom labels are defined as key-value pairs, where the key is the label name and the value is either a literal or a CEL expression.
Using dynamically evaluated CEL expressions, you can label existing metrics with any data referenced by the [Well-known Attributes](https://github.com/Kuadrant/architecture/blob/main/rfcs/0002-well-known-attributes.md).

Only labels with CEL expressions that resolve successfully will be included.

## How to activate

The TelemetryPolicy API is not enabled by default. You must explicitly opt-in to use it.
To activate it, you need to run the following `make` targets from the [kuadrant-operator](https://github.com/Kuadrant/kuadrant-operator) repository.

First, clone the repository locally:

```bash
git clone --depth=1 https://github.com/Kuadrant/kuadrant-operator.git
cd kuadrant-operator
```

Then, activate the Kuadrant extensions:

```bash
make local-apply-extensions apply-extensions-manifests
```

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