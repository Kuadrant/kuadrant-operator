# Kuadrant TelemetryPolicy

The Kuadrant [TelemetryPolicy CRD](../reference/telemetrypolicy.md) allows you to add custom labels to [Kuadrant data plane component metrics](//TODO).

## How it works

Custom labels are defined as key-value pairs, where the key is the label name and the value is either a literal or a CEL expression.
Using dynamically evaluated CEL expressions, you can label existing metrics with any data referenced by the [Well-known Attributes](https://github.com/Kuadrant/architecture/blob/main/rfcs/0002-well-known-attributes.md).

Expressions are evaluated on every request and are not skipped when they fail. On the
authorization action an `auth.*` expression that cannot be resolved is left out; any other
expression that cannot be evaluated for a request, for example one that reads a header the
request does not carry, fails the action, and the request then follows the failure mode of the
corresponding protection service (by default HTTP 500 on the authorization action, and no rate
limiting on a rate-limit action). Guard optional attributes in the expression; the
[TelemetryPolicy reference](../reference/telemetrypolicy.md#evaluation-of-expressions)
describes the evaluation model and the guard spellings.

Expressions are evaluated in the data plane and their values are emitted verbatim, so a
policy whose expression reads a sensitive request header such as `authorization` is
rejected. See the [TelemetryPolicy reference](../reference/telemetrypolicy.md#telemetrypolicyspec)
for the full rule.

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
