# Kuadrant in Disconnected OpenShift/OKD Clusters

This document describes the requirements for adding Kuadrant to a disconnected (air-gapped) OpenShift or OKD cluster.

## Overview

Kuadrant operators can be installed in disconnected OpenShift or OKD environments where external registries are not accessible. This requires mirroring Kuadrant's operator catalog and images to your internal registry.

**Assumes you already have:**
- A disconnected OpenShift (4.12+) or OKD cluster with mirror registry configured
- oc-mirror or similar tooling for mirroring operator catalogs
- ImageDigestMirrorSet/ImageTagMirrorSet already configured for platform images

**For general disconnected cluster setup, see:**
- **OpenShift:** [Disconnected Installation](https://docs.redhat.com/en/documentation/openshift_container_platform/4.20/html/disconnected_environments/about-disconnected-installation-mirroring)
- **OKD:** [Disconnected Installation](https://docs.okd.io/latest/installing/disconnected_install/installing-mirroring-installation-images.html)
- [Creating a Mirror Registry](https://docs.redhat.com/en/documentation/openshift_container_platform/4.20/html/disconnected_environments/installing-mirroring-creating-registry)
- [Mirroring images with oc-mirror](https://docs.redhat.com/en/documentation/openshift_container_platform/4.20/html/disconnected_environments/installing-mirroring-disconnected)

## Prerequisites

### Platform Compatibility

This guide applies to both:
- **OpenShift Container Platform** (4.12+)
- **OKD** (4.12+)

Both platforms use identical Operator Lifecycle Manager (OLM) and catalog mechanisms, so the installation process is the same.

### Gateway API Provider (Required)

Kuadrant requires a Gateway API implementation installed and functional:

**Istio (via Sail Operator):**
- Sail operator from Community Operators catalog
- Creates `istio` GatewayClass

**Envoy Gateway:**
- Envoy Gateway operator  
- Creates `eg` GatewayClass

**Note:** These must be mirrored and installed before Kuadrant. See their respective documentation for disconnected installation.

### cert-manager (Optional)

Required only if using TLSPolicy for certificate management. Must be mirrored and installed before Kuadrant if needed.

## Kuadrant Components

Kuadrant consists of four operators bundled in a single combined catalog:

| Operator | Purpose | Runtime Images |
|----------|---------|----------------|
| kuadrant-operator | Policy attachment, Gateway API integration | wasm-shim, console-plugin |
| authorino-operator | Authentication/authorization engine | authorino |
| limitador-operator | Rate limiting engine | limitador |
| dns-operator | Multi-cluster DNS management | coredns-kuadrant |

**Catalog:** `quay.io/kuadrant/kuadrant-operator-catalog:latest`

All four operators are available from this single catalog. The kuadrant-operator declares the other three as dependencies via OLM.

## Mirroring Kuadrant

### 1. Add Kuadrant Catalog to ImageSetConfiguration

```yaml
kind: ImageSetConfiguration
apiVersion: mirror.openshift.io/v2alpha1
mirror:
  operators:
    - catalog: quay.io/kuadrant/kuadrant-operator-catalog:latest
      full: true
```

This single catalog contains all four operators and their dependencies.

**Optional - CoreDNS Image:**

If you plan to use DNS Operator's CoreDNS deployment feature, manually add the CoreDNS image to your mirror:

```bash
# Add to your ImageSetConfiguration
additionalImages:
  - name: quay.io/kuadrant/coredns-kuadrant:latest
```

Or mirror directly:
```bash
oc image mirror quay.io/kuadrant/coredns-kuadrant:latest \
  your-mirror-registry/kuadrant/coredns-kuadrant:latest
```

### 2. Mirror to Your Registry

```bash
oc-mirror --config imageset-config.yaml docker://your-mirror-registry
```

oc-mirror will:
- Mirror the catalog image
- Discover all bundles in the catalog (kuadrant, authorino, limitador, dns operators)
- Mirror all images from each bundle's `relatedImages` section
- Generate ImageDigestMirrorSet for runtime images
- Generate ImageTagMirrorSet for catalog/bundle images  
- Generate CatalogSource manifest

### 3. Apply Generated Configuration

```bash
# Apply image mirror configuration
oc apply -f oc-mirror-workspace/results-*/imageDigestMirrorSet.yaml
oc apply -f oc-mirror-workspace/results-*/imageTagMirrorSet.yaml

# Wait for MachineConfigPool to update (may restart nodes)
oc wait mcp --all --for=condition=Updated --timeout=30m

# Apply CatalogSource
oc apply -f oc-mirror-workspace/results-*/catalogSource-*.yaml

# Wait for catalog to be ready
oc wait catalogsource -n openshift-marketplace --all --for=condition=Ready --timeout=5m
```

**Note:** Applying ImageDigestMirrorSet/ImageTagMirrorSet triggers MachineConfig updates which restart cluster nodes. See image configuration documentation for [OpenShift](https://docs.redhat.com/en/documentation/openshift_container_platform/4.20/html/images/managing-image-settings) or [OKD](https://docs.okd.io/latest/openshift_images/image-configuration.html) for details.

## Installation

### Create Namespace and OperatorGroup

```bash
oc new-project kuadrant-system

cat <<EOF | oc apply -f -
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: kuadrant-operator-group
  namespace: kuadrant-system
spec: {}
EOF
```

**Important:** The `spec: {}` (empty spec) is required. Kuadrant operators only support `AllNamespaces` install mode (cluster-scoped). Do not set `targetNamespaces`.

### Create Subscription

```bash
# Query the default channel from PackageManifest
CHANNEL=$(oc get packagemanifest kuadrant-operator -n openshift-marketplace \
  -o jsonpath='{.status.defaultChannel}')

cat <<EOF | oc apply -f -
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: kuadrant-operator
  namespace: kuadrant-system
spec:
  channel: ${CHANNEL}
  name: kuadrant-operator
  source: kuadrant-disconnected-catalog  # CatalogSource name from oc-mirror
  sourceNamespace: openshift-marketplace
  installPlanApproval: Automatic
EOF
```

OLM will automatically install the three dependent operators (authorino, limitador, dns) based on the dependencies declared in kuadrant-operator's bundle.

### Verify Installation

```bash
# Check all four operator CSVs
oc get csv -n kuadrant-system

# Expected output:
#   kuadrant-operator.vX.Y.Z
#   authorino-operator.vX.Y.Z
#   limitador-operator.vX.Y.Z
#   dns-operator.vX.Y.Z

# Check all pods are running
oc get pods -n kuadrant-system
```

### Deploy Kuadrant Instance

```bash
cat <<EOF | oc apply -f -
apiVersion: kuadrant.io/v1beta1
kind: Kuadrant
metadata:
  name: kuadrant
  namespace: kuadrant-system
spec: {}
EOF

# Wait for Kuadrant to be ready
oc wait kuadrant/kuadrant -n kuadrant-system --for=condition=Ready --timeout=5m
```

## Verification

### Verify Installation Success

```bash
# Check all pods are running
oc get pods -n kuadrant-system

# All pods should show Running status
```

### Verify No Image Pull Errors

If ImageDigestMirrorSet/ImageTagMirrorSet is working correctly, there should be no image pull failures:

```bash
# Check for ImagePullBackOff errors
oc get events -n kuadrant-system --field-selector reason=Failed

# Should show no image pull failures
```

**Note:** Pod specs will still show original image references (e.g., `quay.io/kuadrant/...`) even when images are pulled from your mirror registry. The redirection happens transparently at the CRI-O level via ImageDigestMirrorSet/ImageTagMirrorSet configuration.

### Test Policy Enforcement

Create a simple Gateway, HTTPRoute, and RateLimitPolicy to verify Kuadrant is functional:

```bash
# Assumes you have a GatewayClass (istio or eg) from prerequisite Gateway API provider
kubectl get gatewayclass

# Create test resources (see Kuadrant documentation for examples)
# https://docs.kuadrant.io/latest/kuadrant-operator/doc/user-guides/
```

## Troubleshooting

### ImagePullBackOff on Kuadrant Pods

**Symptom:** Pods stuck in ImagePullBackOff

**Cause:** Image not in mirror or ImageDigestMirrorSet not applied correctly

**Check:**
```bash
# Verify ImageDigestMirrorSet exists
oc get imagedigestmirrorset

# Check if specific image was mirrored
oc image info your-mirror-registry/kuadrant/kuadrant-operator@sha256:...

# Check pod events
oc describe pod <pod-name> -n kuadrant-system
```

### CatalogSource Not Ready

**Symptom:** CatalogSource shows READY=False

**Cause:** Catalog image not accessible from mirror

**Check:**
```bash
# Check catalog pod logs
oc logs -n openshift-marketplace -l olm.catalogSource=kuadrant-disconnected-catalog

# Verify catalog image exists in mirror
oc image info your-mirror-registry/kuadrant/kuadrant-operator-catalog:latest
```

### Dependent Operators Not Installing

**Symptom:** Only kuadrant-operator CSV appears, missing authorino/limitador/dns

**Cause:** Catalog missing digest references or dependencies not declared

**Check:**
```bash
# Verify all four operators available as PackageManifests
oc get packagemanifest -n openshift-marketplace | grep -E '(kuadrant|authorino|limitador|dns)-operator'

# Check InstallPlan shows all dependencies
oc get installplan -n kuadrant-system -o yaml

# Look for all four operators in the InstallPlan spec
```

## Additional Resources

**Kuadrant Documentation:**
- [Kuadrant Installation Guide](https://docs.kuadrant.io/latest/kuadrant-operator/doc/install/install-openshift/)
- [Kuadrant User Guides](https://docs.kuadrant.io/latest/kuadrant-operator/doc/user-guides/)

**OpenShift Disconnected Environments:**
- [Disconnected Installation Overview](https://docs.redhat.com/en/documentation/openshift_container_platform/4.20/html/disconnected_environments/about-disconnected-installation-mirroring)
- [Image Configuration](https://docs.redhat.com/en/documentation/openshift_container_platform/4.20/html/images/managing-image-settings)
- [Managing Custom Catalogs](https://docs.redhat.com/en/documentation/openshift_container_platform/4.20/html/operators/administrator-tasks#olm-managing-custom-catalogs)

**OKD Disconnected Environments:**
- [Disconnected Installation Overview](https://docs.okd.io/latest/installing/disconnected_install/installing-mirroring-installation-images.html)
- [Image Configuration](https://docs.okd.io/latest/openshift_images/image-configuration.html)
