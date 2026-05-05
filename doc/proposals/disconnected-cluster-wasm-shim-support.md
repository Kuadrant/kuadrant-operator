# Feature: Disconnected Cluster Support for Wasm Shim Image Resolution

## Summary

Reference: [Kuadrant/kuadrant-operator#1894](https://github.com/Kuadrant/kuadrant-operator/issues/1894)

On disconnected (air-gapped) OpenShift clusters, the wasm-shim container image is served from an internal mirror registry instead of its original source (e.g., `registry.redhat.io`). The Kuadrant operator must resolve the mirror URL and supply registry credentials so that Istio and Envoy Gateway can pull the image. This is necessary because these gateway providers pull wasm images directly and do not honor node-level mirror configuration (`registries.conf` / CRI-O).

This feature adds three capabilities to the data-plane reconciliation loop:

1. **Mirror URL resolution** — rewrites the wasm-shim image URL using OpenShift mirror CRDs
2. **Registry credential discovery** — reads cluster pull secrets to find credentials for the resolved registry
3. **Pull secret lifecycle management** — creates, updates, and cleans up a per-namespace `wasm-plugin-pull-secret`

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

### High-Level Flow

The feature integrates into the existing data-plane reconciliation loop. Each reconcile cycle follows this sequence:

```
┌─────────────────────────────────────────────────────────────────────┐
│                    Reconciler (Istio / Envoy Gateway)               │
│                                                                     │
│  1. Resolve image URL                                               │
│     RELATED_IMAGE_WASMSHIM ──► ResolveImageURL() ──► resolvedURL   │
│                                     │                               │
│                          ┌──────────┴──────────┐                    │
│                          │  OpenShift mirror    │                    │
│                          │  CRDs installed?     │                    │
│                          └──────────┬──────────┘                    │
│                             yes     │     no                        │
│                          ┌──────────┴──────────┐                    │
│                          │  Collect rules from  │  return original  │
│                          │  IDMS / ITMS / ICP   │  URL unchanged    │
│                          │  Find best prefix    │                   │
│                          │  match, rewrite URL  │                   │
│                          └─────────────────────┘                    │
│                                                                     │
│  2. For each gateway:                                               │
│     ┌─────────────────────────────────────────────────────────┐     │
│     │  Build wasmConfig for this gateway                      │     │
│     │                                                         │     │
│     │  ReconcileWasmPluginPullSecret(ctx, cfg)                │     │
│     │    cfg = PullSecretReconcileConfig{                     │     │
│     │      Client, ImageURL, Namespace, SecretName,           │     │
│     │      Active: len(ActionSets) > 0,                       │     │
│     │      IsIDMSInstalled, IsITMSInstalled, IsICPInstalled,  │     │
│     │      Logger,                                            │     │
│     │    }                                                    │     │
│     │    ├─ Kill-switch or no CRDs? → return false            │     │
│     │    ├─ !Active? → cleanup managed secret, return false   │     │
│     │    ├─ extractRegistryHost(imageURL)                     │     │
│     │    ├─ resolveRegistryCredentials(registryHost)           │     │
│     │    │    ├─ Read pull-secret from openshift-config        │     │
│     │    │    ├─ Read additional-pull-secret (overrides)       │     │
│     │    │    └─ Filter to matching registry host              │     │
│     │    └─ ensureWasmPluginPullSecret(ns, creds)              │     │
│     │         ├─ User secret exists? → leave it, return ✓     │     │
│     │         ├─ Creds nil? → delete managed secret if any    │     │
│     │         ├─ No existing? → create managed secret         │     │
│     │         └─ Existing managed? → update if changed        │     │
│     │                                                         │     │
│     │  Fallback: PROTECTED_REGISTRY check                     │     │
│     │    (backward compat for non-OpenShift)                   │     │
│     │                                                         │     │
│     │  Build WasmPlugin / EnvoyExtensionPolicy                │     │
│     │    with resolvedURL and imagePullSecret ref              │     │
│     └─────────────────────────────────────────────────────────┘     │
└─────────────────────────────────────────────────────────────────────┘
```

### Kubernetes Resources

#### Read (cluster-scoped, OpenShift only)

| Resource | API Group | Namespace | Purpose |
|----------|-----------|-----------|---------|
| `ImageDigestMirrorSet` | `config.openshift.io/v1` | cluster-scoped | Digest-based mirror rules (`@sha256:...`) |
| `ImageTagMirrorSet` | `config.openshift.io/v1` | cluster-scoped | Tag-based mirror rules (`:tag`) |
| `ImageContentPolicy` | `config.openshift.io/v1` | cluster-scoped | Legacy mirror rules (digest-only, replaces ICSP) |
| `Secret/pull-secret` | `v1` | `openshift-config` | Cluster-wide registry credentials |
| `Secret/additional-pull-secret` | `v1` | `openshift-config` | Override credentials (takes precedence over `pull-secret`) |

#### Managed (per gateway namespace)

| Resource | Name | Purpose |
|----------|------|---------|
| `Secret` (type `kubernetes.io/dockerconfigjson`) | `wasm-plugin-pull-secret` | Registry credentials for the resolved mirror, labeled `kuadrant.io/managed=true` |

#### Produced (per gateway)

| Gateway Provider | Resource | Field Set |
|-----------------|----------|-----------|
| Istio | `WasmPlugin` | `spec.url` = resolved URL, `spec.imagePullSecret` = secret name |
| Envoy Gateway | `EnvoyExtensionPolicy` | `spec.wasm[0].code.image.url` = resolved URL, `spec.wasm[0].code.image.pullSecretRef` = secret ref |

### RBAC

```yaml
# Read mirror CRDs (cluster-scoped, only used when CRDs are installed)
- groups: ["config.openshift.io"]
  resources: [imagedigestmirrorsets, imagetagmirrorsets, imagecontentpolicies]
  verbs: [get, list, watch]

# Read cluster pull secrets (restricted to well-known names)
- groups: [""]
  resources: [secrets]
  resourceNames: [pull-secret, additional-pull-secret]
  verbs: [get]

# Manage wasm-plugin-pull-secret (create cannot use resourceNames — K8s limitation)
- groups: [""]
  resources: [secrets]
  verbs: [create]
- groups: [""]
  resources: [secrets]
  resourceNames: [wasm-plugin-pull-secret]
  verbs: [get, update, patch, delete]
```

### Mirror Resolution Logic

`ResolveImageURL` collects mirror rules from all three CRD types, then applies prefix-matching semantics consistent with the OpenShift container runtime:

1. **Collect rules** from IDMS (digest-only), ITMS (tag-only), and ICP (digest-only, legacy)
2. **Filter by pull type**: IDMS/ICP rules only apply to `@sha256:` references, ITMS rules only to `:tag` references
3. **Find best match**: longest source prefix that matches the image URL, with path-boundary enforcement
4. **Wildcard support**: sources like `*.redhat.io` match any subdomain
5. **Rewrite**: replace the matched source prefix with the first mirror, preserving the image path and digest/tag

### Credential Discovery

`ResolveRegistryCredentials` merges credentials from two sources:

1. `openshift-config/pull-secret` — base cluster credentials
2. `openshift-config/additional-pull-secret` — overrides (entries here replace entries from `pull-secret`)

The merged credentials are filtered to only the registry host extracted from the resolved image URL. This produces a minimal `dockerconfigjson` blob containing only the credentials needed.

### Pull Secret Lifecycle

`EnsureWasmPluginPullSecret` manages a `wasm-plugin-pull-secret` in each gateway namespace:

| Condition | Action |
|-----------|--------|
| User-created secret exists (no `kuadrant.io/managed` label) | Leave untouched, return `useImagePullSecret=true` |
| Credentials provided, no existing secret | Create managed secret |
| Credentials provided, managed secret exists, content changed | Update managed secret |
| Credentials provided, managed secret exists, content unchanged | No-op (semantic JSON comparison) |
| No credentials, managed secret exists | Delete managed secret |
| No credentials, no existing secret | No-op, return `useImagePullSecret=false` |

Change detection uses semantic JSON equality (`json.Unmarshal` + `reflect.DeepEqual`) to avoid spurious updates from formatting differences. Falls back to `bytes.Equal` for non-JSON data.

### Gating and Kill-Switch

The feature is gated at two levels:

1. **CRD presence**: `pullSecretEnabled` is only true when at least one mirror CRD (IDMS, ITMS, or ICP) is installed. On non-OpenShift clusters, no mirror-related API calls are made.
2. **Kill-switch**: Setting `DISABLE_IMAGE_MIRROR_RESOLUTION=true` disables the entire feature — mirror resolution returns the original URL, and `pullSecretEnabled` evaluates to false.

### Backward Compatibility

The `PROTECTED_REGISTRY` env var (default: `registry.redhat.io`) provides a fallback for environments where OpenShift credential discovery is not available. If the resolved URL contains the protected registry host and no pull secret was set through the OpenShift flow, the reconciler still sets `useImagePullSecret=true`, expecting a user-created `wasm-plugin-pull-secret` to exist in the gateway namespace.

### Configuration

| Env Var | Default | Purpose |
|---------|---------|---------|
| `RELATED_IMAGE_WASMSHIM` | `quay.io/kuadrant/wasm-shim:latest` | Source wasm-shim image URL |
| `PROTECTED_REGISTRY` | `registry.redhat.io` | Fallback protected registry check |
| `DISABLE_IMAGE_MIRROR_RESOLUTION` | (unset) | Set to `true` to disable the entire feature |

### Package Structure

```
internal/openshift/
├── mirror.go              # IsImageMirrorResolutionDisabled, ResolveImageURL, mirror rule collection
├── mirror_test.go         # Unit tests for mirror resolution and kill-switch
├── mirror_resolve_test.go # Integration-style tests for ResolveImageURL with fake clients
├── pullsecret.go          # ReconcileWasmPluginPullSecret, credential discovery, secret lifecycle
└── pullsecret_test.go     # Unit tests for credential discovery, secret management, JSON equality

internal/controller/
├── data_plane_policies_workflow.go       # Constants, RBAC markers, workflow wiring
├── istio_extension_reconciler.go         # Istio reconciler (uses ReconcileWasmPluginPullSecret)
├── envoy_gateway_extension_reconciler.go # Envoy Gateway reconciler (same pattern)
└── disconnected_cluster_test.go           # Flow tests with fake clients
```
