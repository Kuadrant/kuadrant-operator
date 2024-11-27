# Configure mTLS between the Gateway and Kuadrant components

## Overview

This guide includes manual steps to enable mTLS between the gateway and Kuadrant components.
If you use an AuthPolicy or RateLimitPolicy, there will be communication between the gateway and the respective Kuadrant components at request time. This communication happens between the Wasm plugin in Envoy proxy, and Authorino or Limitador.
At the time of writing there is [an RFC](https://github.com/Kuadrant/architecture/pull/110) discussing how to add this capability as a feature of the Kuadrant operator. If you are interested in having that feature or influencing how it is delivered, please engage on that pull request.

!!! note

    This method currently only works if the Gateway is provided by Istio, with service mesh capabilities enabled across the cluster. For example, the [Istio CNI](https://github.com/istio-ecosystem/sail-operator/blob/main/docs/README.md#istiocni-resource) agent is running on each node.

## Prerequisites

You have installed Kuadrant in a [Kubernetes](https://docs.kuadrant.io/latest/kuadrant-operator/doc/install/install-kubernetes/) or [OpenShift](https://docs.kuadrant.io/latest/kuadrant-operator/doc/install/install-openshift/) cluster.
Additionally, you have at least 1 AuthPolicy or RateLimitPolicy attached to your Gateway or HTTPRoute.

## Enabling mTLS

### Kuadrant components

As the Kuadrant components (Authorino & Limitador) are already part of the service mesh in Istio, mTLS can be enabled after an Envoy proxy sidecar is deployed alongside them.
To do this, apply the Istio sidecar label to both Deployment templates.

```bash
kubectl -n kuadrant-system patch deployment authorino \
  -p '{"spec":{"template":{"metadata":{"labels":{"sidecar.istio.io/inject":"true"}}}}}'

kubectl -n kuadrant-system patch deployment limitador-limitador \
  -p '{"spec":{"template":{"metadata":{"labels":{"sidecar.istio.io/inject":"true"}}}}}'
```

You should see the number of containers in either pod increase from 1 to 2, as the `istio-proxy` is added to the pods. This change will force all traffic to those pods to go through the proxy. However, mTLS is not enabled yet.

### Envoy Filter

The next step enables mTLS for traffic originating in the gateway (where the Wasm plugin executes), going to the Kuadrant components.
This requires modifying the EnvoyFilters directly.

!!! note

    Any changes to the EnvoyFilters may be reverted by the Kuadrant operator when related resources like Gateways, HTTPRoutes or policies are modified. It is recommended to automate the next step, for example via a job or GitOps controller, to ensure the changes persist.

The EnvoyFilter resources will typically have a name prefix of `kuadrant-` in the same namespace as your Gateway.
Add the snippet below to the `spec.configPatches[].patch.value` value in each EnvoyFilter.

```yaml
        transport_socket:
          name: envoy.transport_sockets.tls
          typed_config:
            '@type': type.googleapis.com/envoy.extensions.transport_sockets.tls.v3.UpstreamTlsContext
            common_tls_context:
              tls_certificate_sds_secret_configs:
              - name: default
                sds_config:
                  api_config_source:
                    api_type: GRPC
                    grpc_services:
                    - envoy_grpc:
                        cluster_name: sds-grpc
              validation_context_sds_secret_config:
                name: ROOTCA
                sds_config:
                  api_config_source:
                    api_type: GRPC
                    grpc_services:
                    - envoy_grpc:
                        cluster_name: sds-grpc
```

The `envoy.transport_sockets.tls` [transport socket](https://www.envoyproxy.io/docs/envoy/latest/api-v3/extensions/transport_sockets/tls/v3/tls.proto#tls-transport-socket-proto) name tells Envoy to use the built-in TLS transport socket, enabling TLS encryption.
The `@type` specifies that the configuration follows the `UpstreamTlsContext` message from Envoy's TLS transport socket extension. This is used for [client-side TLS settings](https://www.envoyproxy.io/docs/envoy/latest/api-v3/extensions/transport_sockets/tls/v3/tls.proto#envoy-v3-api-msg-extensions-transport-sockets-tls-v3-upstreamtlscontext).
The `tls_certificate_sds_secret_configs` configures Envoy to obtain client certificates and private keys via the Secret Discovery Service (SDS) over GRPC.
The `validation_context_sds_secret_config` configures Envoy to obtain the root CA certificates via SDS (over GRPC) to validate the server's certificate.

### Istio configuration

The last step is to ensure Authorino and Limitador are configured to require and accept mTLS connections.
In Istio, this is done by creating a [PeerAuthentication](https://istio.io/latest/docs/reference/config/security/peer_authentication/) resource where the `mtls` mode is set to `STRICT`.
The below command will enable STRICT mode on all pods with Istio sidecar injection in the `kuadrant-system` namespace.

```bash
kubectl apply -f - <<EOF
apiVersion: security.istio.io/v1
kind: PeerAuthentication
metadata:
  name: default
  namespace: kuadrant-system
spec:
  mtls:
    mode: STRICT
EOF
```

If you prefer to only enable mTLS for a specific component, you can modify just the EnvoyFilter and Deployment for that component.
Then, when creating the `PeerAuthentication` resource, you can be more specific about what pods the mTLS mode apply to. For example, the following resource would enable STRICT mode just for the Limitador component.

```yaml
apiVersion: security.istio.io/v1
kind: PeerAuthentication
metadata:
  name: limitador-mtls
  namespace: kuadrant-system
spec:
  selector:
    matchLabels:
      app: limitador
  mtls:
    mode: STRICT
```

## Disabling mTLS

To disable mTLS, remove the `transport_socket` changes from any EnvoyFilters.
Then you can either set the mTLS mode to PERMISSIVE in the `PeerAuthentication` resource:

```bash
kubectl patch peerauthentication default -n kuadrant-system --type='merge' -p '{"spec":{"mtls":{"mode":"PERMISSIVE"}}}'
```

Or delete the resource:

```bash
kubectl delete peerauthentication -n kuadrant-system default
```

You don't have to remove the sidecar from the Kuadrant components, but it is safe to do so by removing the `sidecar.istio.io/inject` label:

```bash
kubectl -n kuadrant-system patch deployment authorino \
  --type='json' \
  -p='[{"op": "remove", "path": "/spec/template/metadata/labels/sidecar.istio.io~1inject"}]'

kubectl -n kuadrant-system patch deployment limitador-limitador \
  --type='json' \
  -p='[{"op": "remove", "path": "/spec/template/metadata/labels/sidecar.istio.io~1inject"}]'
```

Or set the value to `false`:

```bash
kubectl -n kuadrant-system patch deployment authorino \
  -p '{"spec":{"template":{"metadata":{"labels":{"sidecar.istio.io/inject":"false"}}}}}'


kubectl -n kuadrant-system patch deployment limitador-limitador \
  -p '{"spec":{"template":{"metadata":{"labels":{"sidecar.istio.io/inject":"false"}}}}}'
```
