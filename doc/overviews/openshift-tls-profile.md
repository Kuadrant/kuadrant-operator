# OpenShift TLS Security Profile Propagation

On OpenShift clusters, the Kuadrant operator automatically detects and propagates the cluster's TLS security profile to managed components. This ensures that components like Authorino comply with the cluster-wide TLS policy without manual configuration.

## How it works

OpenShift exposes TLS configuration through a cluster-scoped [`APIServer`](https://docs.openshift.com/container-platform/latest/security/tls-security-profiles.html) custom resource (`config.openshift.io/v1`). This resource defines a `tlsSecurityProfile` that specifies the minimum TLS version and allowed cipher suites for the cluster.

At startup, the Kuadrant operator checks whether the `APIServer` CRD is installed. If it is, the operator reads the `APIServer` CR named `cluster` (the standard OpenShift singleton) and:

1. Watches the `APIServer` CR named `cluster` for changes to the TLS security profile.
2. Resolves the profile into a minimum TLS version and a list of IANA cipher suite names.
3. Applies these settings to managed components when TLS is enabled on those components.

When the `APIServer` CR named `cluster` is updated, the operator automatically reconciles the affected components to reflect the new profile.

### Supported components

| Component | Status |
|-----------|--------|
| Authorino (Listener and OIDC Server) | Supported |

### Non-OpenShift clusters

On clusters where the `APIServer` CRD is not present, the operator falls back to the [Intermediate](https://wiki.mozilla.org/Security/Server_Side_TLS#Intermediate_compatibility_.28recommended.29) TLS profile (TLS 1.2, modern cipher suites). This is the same default used by OpenShift when no explicit profile is configured.

## Supported TLS profiles

The operator supports all four OpenShift TLS profile types:

| Profile | Min TLS Version | Description |
|---------|----------------|-------------|
| **Old** | 1.0 | Maximum backward compatibility, includes legacy ciphers |
| **Intermediate** (default) | 1.2 | Recommended for most deployments, balances security and compatibility |
| **Modern** | 1.3 | TLS 1.3 only, highest security, limited client compatibility |
| **Custom** | User-defined | Allows specifying individual ciphers and minimum TLS version |

### Cipher suite translation

OpenShift TLS profiles use OpenSSL-style cipher names, while Authorino uses Go/IANA-style names. The operator translates between the two formats automatically. DHE cipher suites are excluded as they are not supported by Go's `crypto/tls` library.

## Configuration

### APIServer CR name

By default, the operator reads the `APIServer` CR named `cluster`, which is the standard singleton on OpenShift. This can be overridden by setting the `APISERVER_CR_NAME` environment variable on the operator deployment:

```yaml
env:
  - name: APISERVER_CR_NAME
    value: "my-custom-apiserver"
```

### TLS must be enabled on the component

The TLS profile is only applied to a component when TLS is enabled on that component. If TLS is not enabled, the profile fields are omitted entirely to avoid unnecessary configuration.

For example, the Authorino CR must have `spec.listener.tls.enabled: true` and/or `spec.oidcServer.tls.enabled: true` for the profile to be applied to those respective sections.

## Example

On an OpenShift cluster with the following `APIServer` configuration:

```yaml
apiVersion: config.openshift.io/v1
kind: APIServer
metadata:
  name: cluster
spec:
  tlsSecurityProfile:
    type: Modern
```

The operator will configure Authorino with:

- **Minimum TLS version**: 1.3
- **Cipher suites**: `TLS_AES_128_GCM_SHA256`, `TLS_AES_256_GCM_SHA384`, `TLS_CHACHA20_POLY1305_SHA256`

These settings are applied via Server-Side Apply so that only the TLS profile fields are managed by the operator, while other TLS fields (such as `enabled` and `certSecretRef`) remain under user control.
