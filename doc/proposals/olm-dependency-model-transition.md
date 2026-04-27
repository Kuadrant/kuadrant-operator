# RFC: OLM Dependency Model Transition

- Feature Name: `olm_dependency_model_transition`
- Start Date: 2026-03-30
- RFC PR: [Kuadrant/architecture#0000](https://github.com/Kuadrant/architecture/pull/0000)
- Issue tracking: [Kuadrant/architecture#0000](https://github.com/Kuadrant/architecture/issues/0000)

# Summary
[summary]: #summary

Replace the OLMv0 dependency model (which automatically installs Authorino, Limitador, and DNS operators) with an umbrella operator that directly deploys and manages the lifecycle of all Kuadrant components. This dedicated installer operator preserves the single-action install experience while aligning with OLMv1's design, which explicitly removes automatic dependency installation. The umbrella operator pattern follows the approach recommended by the OLM team and used by OpenShift itself (e.g., [cluster-olm-operator](https://github.com/openshift/cluster-olm-operator)).

# Motivation
[motivation]: #motivation

The OLM (Operator Lifecycle Manager) dependency model is being deprecated as part of the transition to OLMv1 (ClusterExtensions). OLMv1 explicitly will not automatically install missing dependencies when a user requests an operator. The [OLMv1 design decisions](https://github.com/operator-framework/operator-controller/blob/main/docs/project/olmv1_design_decisions.md) state that it "will err on the side of predictability and cluster-administrator awareness."

Today, Kuadrant ships as a single OLM catalog that bundles four operators:

- **kuadrant-operator** (the main operator)
- **authorino-operator** (authentication/authorization)
- **limitador-operator** (rate limiting)
- **dns-operator** (DNS management)

Users add one CatalogSource, create one Subscription, and OLM's dependency resolution handles installing all four operators automatically. When the OLMv1 dependency model is removed, this single-action install experience breaks.

The target timeline for this transition is **end of 2026**.

### Requirements

1. **Preserve the single-action install experience** - users should not need to manually install four or more separate operators.
2. **Full lifecycle management of dependencies** - the umbrella operator manages versions, installation, upgrades, and removal of all Kuadrant components. Cluster admins do not independently upgrade these components.
3. **Targets OLMv1** - the solution needs to work with OLMv1 (ClusterExtensions API), which can be installed on both OpenShift and vanilla Kubernetes.
4. **Separation of concerns** - the umbrella operator handles deployment and lifecycle management exclusively. The kuadrant-operator remains focused on policy reconciliation and is unaware of OLMv1.
5. **Extensible to selective deployment** - the architecture must support future selective deployment of components based on which policies a user wants to enable. As the number of Kuadrant policies and extensions grows, users will need the ability to deploy only the components they require.

### OLMv1 Context

With OLMv1, installing an operator requires the user to explicitly create several resources ([getting started guide](https://operator-framework.github.io/operator-controller/getting-started/olmv1_getting_started/)):

1. **ClusterCatalog** - points to an image registry containing operator bundles
2. **Namespace** - where the operator will run
3. **ServiceAccount** - OLMv1 impersonates this SA to install bundle contents
4. **ClusterRoles/Roles + Bindings** - grant the SA permissions for everything the operator needs to create
5. **ClusterExtension** - the install intent, referencing the SA and catalog

Without dependency management, a user installing the full Kuadrant stack would need to repeat steps 2-5 for each operator (~20+ resources manually). The umbrella operator reduces this to a single ClusterExtension.

Key [OLMv1 design decisions](https://github.com/operator-framework/operator-controller/blob/main/docs/project/olmv1_design_decisions.md) that shape this proposal: no automatic dependency installation (the core driver), no cluster-admin permissions (OLMv1 uses user-provided ServiceAccounts), flexible bundle contents (we can ship dependency manifests in the umbrella operator bundle), and fine-grained version control per operator.

### Out of Scope

- Non-OLM environments (e.g., Helm installs on vanilla Kubernetes) are out of scope for this proposal. Existing Helm tooling will continue to handle dependency installation in those environments.
- Implementing selective component deployment at this stage. The architecture supports it, but the initial implementation deploys all components.

# Guide-level explanation
[guide-level-explanation]: #guide-level-explanation

### User Install Workflow

1. **Create a ClusterCatalog** pointing to the Kuadrant catalog image.
2. **Create the umbrella operator ClusterExtension** with its namespace, ServiceAccount, and RBAC. This is the only ClusterExtension the user creates.
3. OLMv1 deploys the umbrella operator.
4. The umbrella operator deploys all Kuadrant components (Deployments, CRDs, ServiceAccounts, RBAC) for kuadrant-operator, authorino-operator, limitador-operator, and dns-operator.
5. **Create the Kuadrant CR** - kuadrant-operator detects available dependencies via CRD presence and begins reconciling policies.

To uninstall, the user deletes the Kuadrant CR, then deletes the umbrella operator's ClusterExtension.

### Upgrade Experience

When the user upgrades the umbrella operator (via its ClusterExtension), the new image contains updated component image references. The umbrella operator rolls out updated Deployments for each component automatically. See [Upgrade Flow](#upgrade-flow) for details.

# Reference-level explanation
[reference-level-explanation]: #reference-level-explanation

### Component Manifest Management

Two viable approaches for how the umbrella operator obtains component manifests (Deployments, CRDs, RBAC, Services):

1. **Embedded manifests**: Vendored into the umbrella operator image at build time. Simpler build pipeline but requires rebuilding for any manifest change.

2. **Init container extraction** (cluster-olm-operator pattern): Init containers run from each component's image and copy manifests to a shared volume. More complex but allows component images to own their own manifests.

### Controller Types

The umbrella operator uses three types of controllers:

1. **Static Resource Controllers** - manage Namespaces, ServiceAccounts, ClusterRoles, ClusterRoleBindings, CRDs. Applied declaratively and reconciled to match desired state.

2. **Deployment Controllers** - manage component Deployments including image references, resource configuration, and rollout monitoring.

3. **Status Controller** - aggregates health from all managed Deployments and reports overall readiness.

### Version Pinning

Each umbrella operator release embeds component image references as environment variables or build-time constants:

```
KUADRANT_OPERATOR_IMAGE=quay.io/kuadrant/kuadrant-operator:v1.2.0
AUTHORINO_OPERATOR_IMAGE=quay.io/kuadrant/authorino-operator:v0.14.0
LIMITADOR_OPERATOR_IMAGE=quay.io/kuadrant/limitador-operator:v0.11.0
DNS_OPERATOR_IMAGE=quay.io/kuadrant/dns-operator:v0.8.0
```

### RBAC

The umbrella operator's ServiceAccount requires permissions to:

- Create/manage Namespaces, ServiceAccounts, Deployments, Services
- Create/manage ClusterRoles, ClusterRoleBindings, Roles, RoleBindings
- Create/manage CRDs
- Read Deployment/Pod status for health monitoring

These permissions are granted via the ClusterExtension's ServiceAccount at install time. Each component operator's RBAC is created by the umbrella operator as part of that component's manifest set.

### Upgrade Flow

1. OLMv1 deploys the new umbrella operator image containing updated component image references.
2. The umbrella operator detects image drift and updates dependency Deployments first (Authorino, Limitador, DNS), waiting for each to roll out successfully.
3. Once dependencies are healthy, kuadrant-operator is updated.

The umbrella operator **must** deploy components that are backwards-compatible with the previous kuadrant-operator version. There is an unavoidable window during upgrade where the old kuadrant-operator runs alongside new dependency versions. Backwards compatibility during this window must be enforced via release testing.

### Failure Scenarios

- **Deployment rollout failure** - reports a degraded condition and blocks further upgrades (e.g., will not upgrade kuadrant-operator if a dependency failed).
- **Image pull failure** - surfaced in status via the underlying Deployment's ImagePullBackOff condition.
- **CRD conflict** - if a CRD already exists on the cluster (e.g., from a standalone Authorino installation), the umbrella operator reports an error rather than silently overwriting.
- **Rollback** - the user reverts the umbrella operator's ClusterExtension to the previous version, which reconciles Deployments back to the old component image references.

# Drawbacks
[drawbacks]: #drawbacks

- **New operator to build and maintain** - introduces a new codebase, container image, build pipeline, and release process. This is ongoing maintenance overhead.
- **Broad RBAC requirements** - the umbrella operator needs permissions to create CRDs, ClusterRoles, and Deployments across namespaces. This is a wide permission set, though it is inherent to the umbrella operator pattern.
- **Version coordination** - the umbrella operator, kuadrant-operator, and all dependency operators must be released and tested together. The compatibility matrix must be maintained.
- **Two operators to debug** - when installation issues occur, users must distinguish between umbrella operator problems (deployment/lifecycle) and kuadrant-operator problems (policy reconciliation).

# Rationale and alternatives
[rationale-and-alternatives]: #rationale-and-alternatives

### Why this design

- **OLM team recommendation** - the OLM team explicitly recommended this pattern over creating ClusterExtension CRs at runtime.
- **Separation of concerns** - kuadrant-operator stays focused on policy reconciliation with no OLMv1 awareness.
- **No runtime OLMv1 dependency** - the umbrella operator manages Deployments directly, reducing the blast radius of OLMv1 issues.
- **Extensible** - each component is an independent Deployment, making selective deployment a natural future addition.
- **Proven pattern** - cluster-olm-operator demonstrates this at scale in production OpenShift clusters.

### Alternatives considered

**Kuadrant operator creates ClusterExtension CRs at runtime** - kuadrant-operator would detect OLMv1 and create ClusterExtension CRs for each dependency. Not chosen because the OLM team explicitly advised against this approach, and it couples policy reconciliation with deployment orchestration.

**Embedding all operators in a single OLMv1 bundle** - all four operator Deployments and CRDs bundled into one ClusterExtension. Not chosen because it is all-or-nothing and cannot support selective component deployment as the number of policies grows.

# Future possibilities
[future-possibilities]: #future-possibilities

### Selective Component Deployment

The architecture supports adding a CR API (e.g., `KuadrantInstall`) that maps enabled policies to required components. The umbrella operator would deploy only the components needed for selected policies, with safety checks to prevent removing components that have active policy CRs. This becomes increasingly valuable as Kuadrant extensions mature (OIDCPolicy, PlanPolicy, TelemetryPolicy).

### Other

- **Richer health diagnostics** beyond Deployment status (logs, metrics).
- **Cross-cluster consistency** ensuring matching component versions across clusters.

# Prior art
[prior-art]: #prior-art

- **OpenShift cluster-olm-operator** - the primary reference implementation for this pattern. Manages catalogd and operator-controller as sub-components using static resource controllers, deployment controllers, and init container manifest extraction. [Source](https://github.com/openshift/cluster-olm-operator).
- **OLMv0 dependency resolution** - the current model being replaced. OLMv0 resolves dependencies automatically via CRD-based dependency declarations in `bundle/metadata/dependencies.yaml`. [OLM Dependency Resolution](https://olm.operatorframework.io/docs/concepts/olm-architecture/dependency-resolution/).

# Unresolved questions
[unresolved-questions]: #unresolved-questions

- **Manifest management approach** - should the umbrella operator embed component manifests at build time, or use the init container extraction pattern from cluster-olm-operator? The tradeoff is build simplicity vs. component image ownership of their own manifests.
- **Umbrella operator repo location** - should this live in a new repository (e.g., `kuadrant-installer-operator`) or as a separate binary/module within the existing kuadrant-operator repository?
- **CRD ownership conflicts** - what is the migration path for users who have standalone installations of Kuadrant components (e.g., standalone Authorino) and want to transition to the umbrella operator?
- **Non-OLM usage** - could the umbrella operator also serve as the installer for non-OLM environments (replacing Helm), or should these remain separate installation paths?
- **Component operator CRD definitions** - should each dependency operator's CRDs be shipped in the umbrella operator bundle, or should the component operators create their own CRDs at startup?
