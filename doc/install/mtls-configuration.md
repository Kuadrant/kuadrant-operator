# Configure mTLS between the Gateway and Kuadrant components

## Overview

This guide includes manual steps to enable mTLS between an Istio provided gateway and the Kuadrant components.
If you use an AuthPolicy or RateLimitPolicy, there will be communication between the gateway and the respective Kuadrant components at request time. This communication happens between the Wasm plugin in Envoy proxy, and Authorino or Limitador.
At the time of writing there is [an RFC](https://github.com/Kuadrant/architecture/pull/110) discussing how to add mTLS capabilities as a feature of the Kuadrant operator. If you are interested in having that feature or influencing how it is delivered, please engage on that pull request.

!!! note

    This method currently only works if the Gateway is provided by Istio, with service mesh capabilities enabled across the cluster. For example, the [Istio CNI](https://github.com/istio-ecosystem/sail-operator/blob/main/docs/README.md#istiocni-resource) agent is running on each node.

## Prerequisites

- You have installed Kuadrant in a Kubernetes cluster.
- Additionally, you have at least 1 AuthPolicy or RateLimitPolicy attached to your Gateway or HTTPRoute.

## Enabling mTLS

In order to ensure that communications between the gateway and the kuadrant components are secured,
set kuadrant's custom resource `spec.mtls.enable` field to `true`.

Example:

```yaml
apiVersion: kuadrant.io/v1beta1
kind: Kuadrant
metadata:
  name: kuadrant-sample
spec:
  mtls:
    enable: true
```

!!! note

    Behind the scenes, kuadrant will create a [PeerAuthentication](https://istio.io/latest/docs/reference/config/security/peer_authentication/) resource where the `mtls` mode is set to `STRICT`.

## Disabling mTLS

To disable mTLS, either set kuadrant's custom resource `spec.mtls.enable` field to `false` or just remove optional `spec.mtls` field.

Example:

```yaml
apiVersion: kuadrant.io/v1beta1
kind: Kuadrant
metadata:
  name: kuadrant-sample
spec:
  mtls: null
```
