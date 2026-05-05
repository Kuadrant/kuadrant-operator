# Feature: Disconnected Cluster Support for Wasm Shim Image Resolution

## Summary

Reference: [Kuadrant/kuadrant-operator#1894](https://github.com/Kuadrant/kuadrant-operator/issues/1894)

On disconnected (air-gapped) OpenShift clusters, the wasm-shim container image is served from an internal mirror registry instead of its original source (e.g., `registry.redhat.io`). Gateway providers like Istio and Envoy Gateway pull wasm images directly and do not honor node-level mirror configuration (`registries.conf` / CRI-O), so the operator must resolve the correct mirror URL and supply registry credentials on their behalf.

## Goals

- Wasm-shim images are pulled from mirror registries on disconnected OpenShift clusters without manual operator configuration
- Registry credentials are automatically discovered from OpenShift cluster pull secrets
- Pull secrets are managed per gateway namespace with proper lifecycle (create, update, cleanup)
- User-created pull secrets are never modified or overwritten
- The feature is transparent on non-OpenShift clusters (no errors, no API calls to missing CRDs)
- A kill-switch (`DISABLE_IMAGE_MIRROR_RESOLUTION=true`) disables the entire feature
- Istio and Envoy Gateway reconcilers follow the same pattern

## Non-Goals

- Mirroring images (this feature only resolves URLs to existing mirrors)
- Supporting mirror configurations outside of OpenShift CRDs (e.g., `registries.conf` files)
- Multi-cluster mirror federation
- Pull secret rotation or expiry handling

## Design

### Overview

The feature adds two steps to the existing data-plane reconciliation loop:

1. **Mirror URL resolution** -- The operator reads OpenShift mirror CRDs (`ImageDigestMirrorSet`, `ImageTagMirrorSet`, `ImageContentPolicy`) and rewrites the wasm-shim image URL to point at the configured mirror registry, following OpenShift's prefix-matching semantics (longest match wins, path-boundary enforcement, wildcard subdomain support, digest-vs-tag filtering).

2. **Pull secret reconciliation** -- For each gateway namespace, the operator discovers registry credentials from the cluster's pull secrets, then creates or updates a managed secret so the gateway provider can authenticate to the mirror registry. When no policies target a gateway, the managed secret is cleaned up.

Both steps are no-ops on non-OpenShift clusters or when the kill-switch is active.

### High-Level Flow

```
Reconcile cycle (per gateway provider)
│
├─ Resolve image URL
│    RELATED_IMAGE_WASMSHIM → mirror CRDs → resolved URL
│    (returns original URL if no mirrors match or not on OpenShift)
│
└─ For each gateway:
     ├─ Build wasm config
     ├─ Reconcile pull secret (single entry point)
     │    ├─ Feature disabled? → no-op
     │    ├─ No active policies? → clean up managed secret
     │    └─ Active? → discover credentials → create/update managed secret
     ├─ Fallback: PROTECTED_REGISTRY check (backward compat)
     └─ Build WasmPlugin / EnvoyExtensionPolicy with resolved URL and pull secret ref
```

### Kubernetes Resources

#### Inputs (read-only, OpenShift only)

| Resource | API Group | Purpose |
|----------|-----------|---------|
| `ImageDigestMirrorSet` | `config.openshift.io/v1` | Digest-based mirror rules (`@sha256:...`) |
| `ImageTagMirrorSet` | `config.openshift.io/v1` | Tag-based mirror rules (`:tag`) |
| `ImageContentPolicy` | `config.openshift.io/v1` | Legacy mirror rules (digest-only, replaces ICSP) |
| `Secret/pull-secret` | `v1` (in `openshift-config`) | Cluster-wide registry credentials |
| `Secret/additional-pull-secret` | `v1` (in `openshift-config`) | Override credentials (takes precedence) |

#### Managed output (per gateway namespace)

| Resource | Name | Purpose |
|----------|------|---------|
| `Secret` (`kubernetes.io/dockerconfigjson`) | `wasm-plugin-pull-secret` | Registry credentials for the mirror, labeled `kuadrant.io/managed=true` |

### Pull Secret Behavior

| Scenario | Action |
|----------|--------|
| User-created secret exists (no `kuadrant.io/managed` label) | Leave untouched, reference it |
| Credentials found, no existing secret | Create managed secret |
| Credentials found, managed secret exists, content changed | Update managed secret |
| Credentials found, managed secret exists, content unchanged | No-op |
| No credentials or no active policies | Delete managed secret if one exists |

### Gating

The feature is gated at two levels:

1. **CRD presence** -- Only activates when at least one mirror CRD (IDMS, ITMS, or ICP) is installed. On non-OpenShift clusters, no mirror-related API calls are made.
2. **Kill-switch** -- `DISABLE_IMAGE_MIRROR_RESOLUTION=true` disables mirror resolution, credential discovery, and pull secret management entirely.

### Backward Compatibility

The `PROTECTED_REGISTRY` env var (default: `registry.redhat.io`) provides a fallback for environments where OpenShift credential discovery is not available. If the resolved URL contains the protected registry host and no pull secret was set through the OpenShift flow, the reconciler still references a pull secret, expecting a user-created one to exist in the gateway namespace.

### Configuration

| Env Var | Default | Purpose |
|---------|---------|---------|
| `RELATED_IMAGE_WASMSHIM` | `quay.io/kuadrant/wasm-shim:latest` | Source wasm-shim image URL |
| `PROTECTED_REGISTRY` | `registry.redhat.io` | Fallback protected registry check |
| `DISABLE_IMAGE_MIRROR_RESOLUTION` | (unset) | Set to `true` to disable the entire feature |
