# Feature: OCI-based Extension Installation for OpenShift

## Summary

Enable third-party extension authors to install custom extensions into the Kuadrant operator on OLM-managed clusters by having the operator pull extension binaries from OCI images at runtime. This eliminates the need to modify the operator Deployment spec (which OLM reverts) and provides a clean installation path that survives OLM upgrade cycles.

Prior investigation: [Kuadrant/kuadrant-operator#1840](https://github.com/Kuadrant/kuadrant-operator/issues/1840)

## Context

### Why was this document created?

Issue [#1840](https://github.com/Kuadrant/kuadrant-operator/issues/1840) investigated how third-party extensions can be installed into the Kuadrant operator on OpenShift. Several approaches were tested, two were found viable, and one (OCI image pull) was proven with a working POC. This document captures the findings and proposes a production-ready design so the work can move from investigation to implementation.

### Why should you read this?

This design affects how extension authors will package, distribute, and install their extensions on OpenShift. If you are building an extension, maintaining the operator, or working on the extension SDK, this document describes the mechanism your extension will use to reach the operator in OLM-managed environments.

### What do we expect from readers?

Review the proposed design and raise concerns about:
- The installation UX for extension authors (is it too manual? too many steps?)
- The security model (pull secrets, RBAC, binary extraction)
- Edge cases in the OLM upgrade lifecycle
- Whether environment variables are the right configuration surface
- How upgrades will impact extensions and how to recover from this

Have a read of the open questions, and share your thoughts.

Review the todo list, and think about if the tasks feel like the correct size to you.

### Why does OpenShift require a different approach to plain Kubernetes?

On plain Kubernetes, an extension author can patch the operator Deployment directly — adding an init container that copies the extension binary into a shared volume. This works and persists across pod restarts.

On OpenShift with OLM, this breaks. OLM continuously reconciles the operator Deployment to match the spec defined in the ClusterServiceVersion (CSV). Any manual patch to the Deployment — init containers, volumes, volume mounts — is detected as drift and reverted within seconds. OLM treats the CSV as the source of truth and enforces it. This means the standard Kubernetes approach of patching the Deployment is not viable on OLM-managed clusters, and the operator must handle extension delivery itself.

### Why environment variables for configuration?

We considered three options for specifying which extension images to pull: environment variables on the operator container, fields on the Kuadrant CRD, or a dedicated ExtensionConfig CRD.

Environment variables were chosen because:
- **They work with the existing OLM model.** CSV env var patches are a well-understood pattern for configuring OLM-managed operators. Extension authors already need to interact with the CSV for other reasons (e.g., verifying operator version compatibility).
- **They require no API changes.** Adding fields to the Kuadrant CRD or introducing a new CRD would require API review, versioning, code generation, and documentation — overhead that isn't justified for what is essentially a list of image references.
- **They align with the startup-time pull model.** Extensions are pulled and discovered once at operator startup. Environment variables are the natural configuration mechanism for process startup behaviour. A CRD-based approach would imply runtime dynamism (watch, reconcile, restart extensions) that this design explicitly does not support.
- **They match the POC.** The working proof of concept uses `EXTENSION_IMAGES`, reducing the gap between what has been tested and what ships.

## Goals

- Extension authors can install extensions without modifying the operator Deployment
- The installation mechanism works on OLM-managed OpenShift clusters
- Private OCI registries are supported via image pull secrets
- Extension RBAC and CRDs are managed independently of OLM and persist across operator upgrades
- The operator handles image pulling, caching, and extraction transparently

## Non-Goals

- Automatic discovery of available extensions (extension authors must explicitly configure image references)
- Hot-reloading extensions without operator restart (extensions are loaded at startup)
- Managing extension CRD lifecycle from the operator (extension authors apply their own CRDs)
- Replacing the built-in extension bundling mechanism (bundled extensions in `/extensions` continue to work)
- Multi-architecture image selection (the extension image must match the operator's architecture)

## Design

### Backwards Compatibility

No breaking changes. The existing extension discovery mechanism (`EXTENSIONS_DIR` / bundled `/extensions` directory) continues to work unchanged. OCI-pulled extensions are discovered alongside bundled extensions. If `EXTENSION_IMAGES` is not set, the operator behaves identically to today.

### Architecture Changes

A new OCI pull phase is added to the operator startup sequence, before extension discovery:

```
Operator Start
    |
    v
[OCI Pull Phase] -- reads EXTENSION_IMAGES env var
    |                 pulls each image (with auth if configured)
    |                 extracts binaries to /tmp/extensions/
    |                 caches by digest to skip unchanged images
    v
[Extension Discovery] -- scans /extensions (bundled)
    |                      scans /tmp/extensions (OCI-pulled)
    |                      merges both sets
    v
[Extension Manager] -- launches all discovered extensions
    |                   (unchanged from current behavior)
    v
[Normal Operation]
```

The OCI pull phase uses [go-containerregistry](https://github.com/google/go-containerregistry) (crane) to fetch images. This library is already used in the POC on the `feat/oci-extension-pull` branch.

### API Changes

No CRD changes. Configuration is via environment variables on the operator container, set through CSV env var patches:

```yaml
# Patch the CSV to configure extension images
oc patch csv kuadrant-operator.v1.0.0 -n kuadrant-system --type='json' -p='[
  {
    "op": "add",
    "path": "/spec/install/spec/deployments/0/spec/template/spec/containers/0/env/-",
    "value": {
      "name": "EXTENSION_IMAGES",
      "value": "quay.io/acme/my-extension:v1.0.0"
    }
  }
]'
```

**Environment variables:**

| Variable | Default | Description |
|----------|---------|-------------|
| `EXTENSION_IMAGES` | (empty) | Comma-separated list of OCI image references to pull |
| `EXTENSIONS_OCI_DIR` | `/tmp/extensions` | Writable directory where OCI-pulled binaries are extracted |
| `EXTENSION_IMAGE_PULL_SECRET` | (empty) | Name of a Secret in the operator namespace containing `.dockerconfigjson` for private registry authentication |

**Pull secret configuration:**

For private registries, create a docker-registry Secret in the operator namespace and reference it:

```bash
# Create pull secret
oc create secret docker-registry extension-pull-secret \
  -n kuadrant-system \
  --docker-server=registry.example.com \
  --docker-username=user \
  --docker-password=token

# Add to CSV
oc patch csv kuadrant-operator.v1.0.0 -n kuadrant-system --type='json' -p='[
  {
    "op": "add",
    "path": "/spec/install/spec/deployments/0/spec/template/spec/containers/0/env/-",
    "value": {
      "name": "EXTENSION_IMAGE_PULL_SECRET",
      "value": "extension-pull-secret"
    }
  },
  {
    "op": "add",
    "path": "/spec/install/spec/deployments/0/spec/template/spec/containers/0/env/-",
    "value": {
      "name": "EXTENSION_IMAGES",
      "value": "registry.example.com/acme/my-extension:v1.0.0"
    }
  }
]'
```

The Secret supports multi-registry credentials via the standard `.dockerconfigjson` format, so a single Secret can authenticate against multiple registries.

### Component Changes

#### `internal/extension/oci.go` (new file, based on POC)

Responsible for pulling OCI images and extracting extension binaries:

- **`PullExtensionImages(imageRefs []string, destDir string, opts ...crane.Option) error`**: Pulls each image reference using crane, extracts extension binaries from the `/extensions/` path in image layers to `destDir`. Applies authentication options if provided.
- **`extractExtensions(img v1.Image, destDir string, logger logr.Logger) error`**: Iterates image layers, finds files under `/extensions/<name>/<name>`, extracts them preserving directory structure and execute permissions.
- **`extractLayer(layer v1.Layer, destDir string, logger logr.Logger) error`**: Processes a single tar layer, with path traversal protection.
- **`loadAuthFromSecret(ctx context.Context, client kubernetes.Interface, namespace, secretName string) (crane.Option, error)`**: Reads a Kubernetes Secret of type `kubernetes.io/dockerconfigjson`, parses the registry credentials, and returns a `crane.Option` that provides authentication to crane for matching registries.
- **`digestCacheFile(destDir, imageRef string) string`**: Returns the path to a digest cache file for a given image reference, used to skip re-pulling unchanged images.
- **`shouldPull(destDir, imageRef string, remoteDigest string) bool`**: Compares the cached digest against the remote digest. Returns false if they match (skip pull).

#### `internal/controller/state_of_the_world.go`

Modified in `getExtensionsOptions()` to:

1. Read `EXTENSION_IMAGES` env var
2. If set, read `EXTENSION_IMAGE_PULL_SECRET` and load auth credentials from the referenced Secret
3. Call `extension.PullExtensionImages()` with the image refs, destination dir, and auth options
4. On success, pass `EXTENSIONS_OCI_DIR` as the extensions directory to the manager (merged with `EXTENSIONS_DIR` for bundled extensions)
5. On failure, log the error and continue startup (graceful degradation — bundled extensions still work)

#### Extension Manager (`internal/extension/manager.go`)

Modified to accept multiple extension directories. `discoverExtensions()` scans both the bundled directory (`/extensions`) and the OCI directory (`/tmp/extensions`), merging discovered extensions. If the same extension name exists in both locations, the bundled version takes precedence (operator-shipped extensions are authoritative).

### Security Considerations

- **Path traversal protection**: The OCI extraction logic validates that extracted file paths do not escape the destination directory (already implemented in POC).
- **Execute permission validation**: Only files with execute permission bits set are treated as extension binaries (existing behavior in `discoverExtensions()`).
- **Pull secret isolation**: The pull secret is a standard Kubernetes Secret in the operator namespace. RBAC controls who can create/modify it. The operator only reads the Secret at startup.
- **Image digest pinning**: Extension authors are encouraged to use digest-pinned references (`image@sha256:...`) rather than mutable tags for reproducibility and supply chain security.
- **No privilege escalation**: The OCI pull runs within the operator's existing security context. No additional capabilities or volume mounts are required. `/tmp` is already writable as a tmpfs.
- **Extension RBAC**: Extension ClusterRoles follow the principle of least privilege. The `compose_extension_manifest.sh` script generates minimal RBAC scoped to the extension's CRD types.

### RBAC Lifecycle Across OLM Upgrades

**Finding**: OLM only manages RBAC resources defined within the CSV's `clusterPermissions` and `permissions` sections. Resources created externally (without owner references to the CSV) are not touched during upgrades.

This means:

1. **Extension ClusterRoles persist across OLM upgrades.** An extension author creates a ClusterRole (e.g., `threat-policy-manager-role`) using `compose_extension_manifest.sh`. OLM does not own this resource and will not delete or modify it during operator upgrades.

2. **Extension ClusterRoleBindings persist across OLM upgrades.** The binding references the operator's ServiceAccount (`kuadrant-operator-controller-manager`). OLM recreates the ServiceAccount during upgrades but does not remove bindings that reference it.

3. **Extension CRDs persist across OLM upgrades.** CRDs applied separately are not owned by the CSV.

4. **CSV env var patches do NOT persist across upgrades.** When OLM installs a new CSV version, the old CSV (with `EXTENSION_IMAGES` and `EXTENSION_IMAGE_PULL_SECRET` patches) is replaced. **Extension authors must reapply the CSV env var patch after each operator upgrade.**

**Recommended extension author workflow after operator upgrade:**

```bash
# 1. Re-patch the CSV with extension configuration
oc patch csv kuadrant-operator.v1.1.0 -n kuadrant-system --type='json' -p='[...]'

# 2. RBAC and CRDs: no action needed (they persist)

# 3. Verify extension is running
oc logs -n kuadrant-system deployment/kuadrant-operator-controller-manager | grep "Discovered extension"
```

## Implementation Plan

1. **Harden OCI pull mechanism**: Refine the POC (`feat/oci-extension-pull` branch) with production error handling, retry logic, and logging.
2. **Add pull secret authentication**: Implement `loadAuthFromSecret()` to read Kubernetes Secrets and pass credentials to crane.
3. **Add digest-based caching**: Skip re-pulling images whose digest hasn't changed since last pull, reducing startup time on restarts.
4. **Support multiple extension directories**: Modify the extension manager to discover extensions from both bundled and OCI directories.
5. **Documentation**: Extension author guide covering image building, RBAC setup, CSV patching, and upgrade procedures.

## Testing Strategy

- **Unit tests**: `internal/extension/oci_test.go` — test image extraction logic with mock tar layers, path traversal rejection, digest caching, and auth option construction from Secret data.
- **Integration tests**: Test the full flow from `EXTENSION_IMAGES` env var through to extension discovery. Requires a test OCI image published to a registry (can use a local registry in CI).
- **E2E tests**: Deploy on an OLM-managed cluster, configure an extension via CSV patch, verify the extension starts, reconciles a CR, and survives an operator restart. Test the OLM upgrade scenario to verify RBAC persistence.

## Open Questions

- Should the operator emit Kubernetes Events or status conditions when an extension image fails to pull, to give visibility beyond logs?
- Should there be a maximum number of OCI extension images to prevent excessive startup time?
- Should the operator support OCI image references with platform/architecture selectors for multi-arch clusters?

## Execution

### Todo

- [ ] Harden OCI pull mechanism with retry logic and structured error handling
  - [ ] Unit tests for extraction, path traversal protection, error cases
  - [ ] Integration tests with a local OCI registry
- [ ] Implement pull secret authentication (`loadAuthFromSecret`)
  - [ ] Unit tests for Secret parsing and crane option construction
  - [ ] Integration tests with authenticated registry pull
- [ ] Add digest-based image caching to skip unchanged images on restart
  - [ ] Unit tests for cache hit/miss logic
- [ ] Modify extension manager to discover from multiple directories
  - [ ] Unit tests for merged discovery with precedence rules
- [ ] Extension author documentation (image building, RBAC, CSV patching, upgrades)

### Completed

## Change Log

### 2026-04-10 — Initial design

- Chose OCI image pull as the primary installation mechanism based on investigation in #1840
- Included pull secret support for private registries
- Confirmed RBAC resources created outside the CSV persist across OLM upgrades
- Identified CSV env var patch as the only step requiring reapplication after upgrades

## References

- [Kuadrant/kuadrant-operator#1840](https://github.com/Kuadrant/kuadrant-operator/issues/1840) — Investigation issue with POC results
- [go-containerregistry (crane)](https://github.com/google/go-containerregistry) — OCI image library used for pulling
- [OLM Architecture: ClusterServiceVersion](https://olm.operatorframework.io/docs/concepts/crds/clusterserviceversion/) — How OLM manages operator deployments and RBAC
- [POC branch: feat/oci-extension-pull](https://github.com/Kuadrant/kuadrant-operator/compare/main...philbrookes:kuadrant-operator:feat/oci-extension-pull) — Working proof of concept
