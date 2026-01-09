# Configure mTLS between the Gateway and Kuadrant components

## Overview

This guide includes manual steps to enable mTLS between an Istio provided gateway and the Kuadrant data plane components.
If you use an AuthPolicy or RateLimitPolicy, there will be communication between the gateway and the respective Kuadrant components at request time. This communication happens between the Wasm plugin in Envoy proxy, and Authorino or Limitador.
At the time of writing there is [an RFC](https://github.com/Kuadrant/architecture/pull/110) discussing how to add mTLS capabilities as a feature of the Kuadrant operator. If you are interested in having that feature or influencing how it is delivered, please engage on that pull request.


!!! note

    This method currently only works if the Gateway is provided by Istio, with service mesh capabilities enabled across the cluster. For example, the [Istio CNI](https://github.com/istio-ecosystem/sail-operator/blob/main/docs/README.md#istiocni-resource) agent is running on each node.

!!! warning "OpenShift 4.19+ Users"

    If you require mTLS on OpenShift Container Platform (OCP) 4.19 or later, the Cluster Ingress Operator (CIO) managed Istio is **not** a viable option because it lacks the necessary service mesh capabilities that Kuadrant's mTLS feature relies on.

    Instead, you must create a custom Istio CR (Custom Resource) with the CNI (Container Network Interface) requirement enabled. The Gateway API can also be enabled on this custom Istio CR.

    **Important:** When defining your Gateways, ensure they avoid using the `openshift.io/gateway-controller/v1` controller name. This prevents the Cluster Ingress Operator from attempting to manage resources for your custom Istio control plane.

## Prerequisites

- Have [Istio](https://istio.io/) as the Gateway API provider installed.
- You have installed Kuadrant in a Kubernetes cluster.

## Enabling mTLS

In order to ensure that communications between the gateway and the kuadrant data plane components are secured,
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

    In the absence of AuthPolicy or RateLimitPolicy, the gateway does not communicate with kuadrant data plane components. Hence, enabling mTLS is useless.

!!! note

    Behind the scenes, kuadrant will create a [PeerAuthentication](https://istio.io/latest/docs/reference/config/security/peer_authentication/) resource where the `mtls` mode is set to `STRICT`.

### mTLS enabled at a component level

*mTLS* can also be enabled at a component level. Kuadrant allows to opt-out from using mTLS for some specific component.

For example, enable mTLS for kuadrant data plane components, but disable limitador:

```yaml
apiVersion: kuadrant.io/v1beta1
kind: Kuadrant
metadata:
  name: kuadrant-sample
spec:
  mtls:
    enable: true
    limitador: false
```

For example, enable mTLS for kuadrant data plane components, but disable authorino:

```yaml
apiVersion: kuadrant.io/v1beta1
kind: Kuadrant
metadata:
  name: kuadrant-sample
spec:
  mtls:
    enable: true
    authorino: false
```

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
