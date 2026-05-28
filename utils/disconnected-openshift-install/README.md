# Kuadrant Disconnected Installation Test Scripts

> **⚠️ FOR TESTING PURPOSES ONLY**
>
> These scripts are designed for **testing and validating** Kuadrant's disconnected installation capabilities.
> They use a **local in-cluster mirror** to simulate a disconnected environment.
>
> **This is NOT the recommended approach for production disconnected installations.**
>
> For production disconnected clusters, use a proper external mirror registry.

Scripts for testing Kuadrant installation in disconnected (air-gapped) OpenShift and OKD clusters using
an in-cluster image mirror for validation purposes.

## Test Environment

These scripts create an **in-cluster mirror** to simulate a disconnected environment for testing purposes.
This approach:
- ✅ Validates that Kuadrant operators work in disconnected environments
- ✅ Tests image mirroring and digest-based references
- ✅ Simulates air-gap constraints for development
- ❌ Is NOT suitable for production (uses cluster resources for the mirror)
- ❌ Does NOT represent best practices for real disconnected deployments

## Prerequisites

- OpenShift or OKD cluster (4.12+) with `oc` CLI authenticated
- `opm`, `docker` or `podman`
- Registry access (default: quay.io)
- Istio (installed via `install-istio.sh`) for full validation

**Supported Platforms:**
- OpenShift Container Platform (OCP) 4.12+
- OKD (OpenShift Kubernetes Distribution) 4.12+
- CodeReady Containers (CRC) with either OpenShift or OKD preset

Both platforms use the same Operator Lifecycle Manager (OLM) and catalog mechanisms, so these scripts work identically on both.

## Building Custom Catalog (Optional)

If you want to build and push your own catalog images (e.g., for development or custom versions):

```bash
# Build and push with default tag (dev)
./utils/disconnected-openshift-install/build-catalogs.sh

# Or build and push with custom tag
IMAGE_TAG=v0.11.0 ./utils/disconnected-openshift-install/build-catalogs.sh

# Or build and push to custom registry (recommended for development)
REGISTRY=registry.example.com ORG=myorg IMAGE_TAG=dev ./utils/disconnected-openshift-install/build-catalogs.sh
```

**Important Notes:**
- This script builds **and pushes** images to the registry - ensure you have push access
- **Builds and pushes images for all four operators:**
  - Operator images: kuadrant-operator, dns-operator, authorino-operator, limitador-operator
  - Bundle images for each operator
  - One combined catalog image: kuadrant-operator-catalog
- **All images will use the same IMAGE_TAG** - this is for testing purposes only
- The default tag is `dev` to avoid accidentally overwriting official `latest` images in quay.io
- If pushing to quay.io/kuadrant, use a unique tag (e.g., your username or feature branch name)
- For testing, consider using a private registry or different organization
- This step is only needed if building custom catalogs. For testing with published releases, skip to E2E Test.

## E2E Test

### Automated

Run all steps automatically:

```bash
# Basic run (assumes Istio already installed)
./utils/disconnected-openshift-install/run-e2e-test.sh

# Install Istio as part of the run
./utils/disconnected-openshift-install/run-e2e-test.sh --install-istio

# Full air-gap simulation with Istio installation
./utils/disconnected-openshift-install/run-e2e-test.sh --install-istio --disconnect

# Custom options
./utils/disconnected-openshift-install/run-e2e-test.sh --install-istio --image-tag=dev --disable-sources
```

### Manual Steps

Test disconnected installation using published catalog images:

```bash
# 1. Install Istio (Gateway API provider)
./utils/disconnected-openshift-install/install-istio.sh

# 2. Configure cluster mirrors and create CatalogSource
#    Uses quay.io/kuadrant/kuadrant-operator-catalog:latest by default
DISABLE_DEFAULT_SOURCES=true IMAGE_TAG=latest ./utils/disconnected-openshift-install/setup.sh

# 3. (Optional) Simulate air-gap by disconnecting cluster
./utils/disconnected-openshift-install/cluster-disconnect.sh disconnect

# 4. Install Kuadrant
./utils/disconnected-openshift-install/tmp/install/install.sh

# 5. Validate installation
./utils/disconnected-openshift-install/smoke-test.sh --cleanup

# 6. (Optional) Reconnect cluster
./utils/disconnected-openshift-install/cluster-disconnect.sh reconnect

# 7. Cleanup
./utils/disconnected-openshift-install/cleanup.sh
```

## Environment Variables

```bash
# Setup (setup.sh)
REGISTRY=quay.io                 # Registry (default: quay.io)
ORG=kuadrant                     # Organization (default: kuadrant)
IMAGE_TAG=latest                 # Catalog tag (default: latest)
DISABLE_DEFAULT_SOURCES=true     # Disable default OperatorHub sources

# Build and Push (build-catalogs.sh) - optional, only for custom builds
REGISTRY=quay.io/myorg           # Registry to push to (default: quay.io)
ORG=myteam                       # Organization (default: kuadrant)
IMAGE_TAG=v0.11.0                # Tag (default: dev)
```

## Scripts

| Script | Purpose |
|--------|---------|
| `run-e2e-test.sh` | Execute complete E2E Test workflow automatically |
| `build-catalogs.sh` | Build and push operators and catalog with digest references |
| `install-istio.sh` | Install Istio via Sail operator (Gateway API provider) |
| `setup.sh` | Configure mirrors, create CatalogSource, generate install manifests |
| `cluster-disconnect.sh` | Simulate air-gap by blocking external network |
| `smoke-test.sh` | Validate operators, policies, and digest references |
| `cleanup.sh` | Restore cluster to original state (use `--yes` to skip prompts) |

## What Gets Installed

**Istio (Gateway API Provider):**
- **Sail operator** - Istio lifecycle manager (in `istio-system`)
- **IstioCNI** - CNI plugin for pod networking
- **Istio control plane** - istiod deployment
- **GatewayClass `istio`** - Gateway API implementation

**Kuadrant Operators:**
- **kuadrant-operator** - Policy attachment controller
- **authorino-operator** - Authentication/authorization  
- **limitador-operator** - Rate limiting
- **dns-operator** - DNS management

All Kuadrant operators installed in `kuadrant-system` namespace (cluster-scoped).

## Generated Files

`setup.sh` generates installation manifests in `utils/disconnected-openshift-install/tmp/install/`:

- `01-namespace.yaml` - kuadrant-system namespace
- `02-operatorgroup.yaml` - OperatorGroup (AllNamespaces)
- `03-subscription.yaml` - Subscription (auto-detects channel)
- `04-kuadrant.yaml` - Kuadrant CR
- `install.sh` / `uninstall.sh` - Automated scripts

## Troubleshooting

**CatalogSource not ready:**
```bash
oc get catalogsource -n openshift-marketplace
oc logs -n openshift-marketplace -l olm.catalogSource=kuadrant-disconnected-operator-catalog
```

**Subscription stuck:**
```bash
oc get installplan -n kuadrant-system
oc patch installplan <name> -n kuadrant-system --type merge -p '{"spec":{"approved":true}}'
```

**Images not pulling:**
```bash
oc get imagedigestmirrorset,imagetagmirrorset
oc debug node/<node> -- chroot /host cat /etc/containers/registries.conf.d/99-*
```

## Related Documentation

- [OLM Disconnected Environments](https://olm.operatorframework.io/docs/tasks/disconnected/)
- [OpenShift Mirroring Images](https://docs.openshift.com/container-platform/latest/installing/disconnected_install/installing-mirroring-installation-images.html)
- [OKD Mirroring Images](https://docs.okd.io/latest/installing/disconnected_install/installing-mirroring-installation-images.html)
- [Kuadrant Documentation](https://docs.kuadrant.io/)
