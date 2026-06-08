# RFC: OLM Dependency Model Transition

- Feature Name: `olm_dependency_model_transition`
- Start Date: 2026-03-30
- RFC PR: [Kuadrant/architecture#0000](https://github.com/Kuadrant/architecture/pull/0000)
- Issue tracking: [Kuadrant/architecture#164](https://github.com/Kuadrant/architecture/issues/164)

# Summary
[summary]: #summary

Replace OLMv0's automatic dependency installation with a separate umbrella operator that deploys and manages all Kuadrant components. Helm charts become the single manifest source — consumed directly via `helm install` and by the umbrella operator via client-only rendering. The kuadrant-operator is simplified to focus exclusively on policy reconciliation; operator installation and operand creation move to the umbrella operator.

- OpenShift: Umbrella Operator domain
- Kubernetes: Helm domain

This follows the pattern recommended by the OLM team and used by OpenShift's [cluster-olm-operator](https://github.com/openshift/cluster-olm-operator).

# Motivation
[motivation]: #motivation

OLMv1 (ClusterExtensions) [explicitly removes automatic dependency installation](https://github.com/operator-framework/operator-controller/blob/main/docs/project/olmv1_design_decisions.md), breaking Kuadrant's current single-action install experience.

Target timeline: **end of 2026**.

### Requirements

1. **Single-action install** — users create one ClusterExtension, not four.
2. **Full lifecycle management** — the umbrella operator manages versions, upgrades, and removal of all components.
3. **OLMv1 compatible** — works with the ClusterExtensions API on both OpenShift and vanilla Kubernetes.
4. **Separation of concerns** — the umbrella operator handles deployment; kuadrant-operator handles policy reconciliation and has no OLMv1 awareness.
5. **Helm as single manifest source** — both installation paths (direct Helm (upstream plain kubernetes) and umbrella operator (OpenShift)) consume the same charts.
6. **Umbrella operator owns operands** — installs each dependency operator and creates their operands (Authorino, Limitador instances). The kuadrant-operator no longer performs these tasks.
7. **Extensible** — supports future selective deployment of components based on which policies are enabled.

### OLMv1 Context

OLMv1 requires explicit creation of several resources per operator ([getting started guide](https://operator-framework.github.io/operator-controller/getting-started/olmv1_getting_started/)): ClusterCatalog, Namespace, ServiceAccount, RBAC, and ClusterExtension. Key design decisions: no automatic dependency installation, no cluster-admin permissions (user-provided ServiceAccounts), flexible bundle contents, and per-operator version control.

### Out of Scope

- Selective component deployment. The architecture supports it and has this in mind for a future iteration, but the initial implementation deploys all components.

# Guide-level explanation
[guide-level-explanation]: #guide-level-explanation

### Install

1. Create a ClusterCatalog pointing to the Kuadrant catalog image.
2. Create the umbrella operator's ClusterExtension (with namespace, ServiceAccount, RBAC). This is the only ClusterExtension the user creates.
3. OLMv1 deploys the umbrella operator.
4. The umbrella operator renders the Helm charts and deploys all components: CRDs, RBAC, operator Deployments, and operand instances.
5. Create the Kuadrant CR (to configure) — kuadrant-operator begins reconciling policies.

Uninstall: delete the Kuadrant CR , then delete the ClusterExtension.

### Upgrade

#### Initial transition (from OLMv0 to umbrella operator)

1. OLMv1 deploys the umbrella operator.
2. The umbrella operator updates **kuadrant-operator first** — the new version stops reconciling operands, ceding that responsibility to the umbrella operator.
3. Once kuadrant-operator is healthy, dependency operators and operands are deployed under umbrella operator management.

This ordering is specific to the transition: kuadrant-operator must stop creating operands before the umbrella operator starts.

#### Steady-state upgrades

1. OLMv1 deploys the new umbrella operator image (containing updated charts and image references).
2. The umbrella operator re-renders charts, detects drift, and applies changes in order:
   - **CRDs, RBAC, ServiceAccounts** for all components first — ensures new API versions and permissions are in place before any controller tries to use them.
   - **Dependency operators** (Authorino, Limitador, DNS) — updated and waited on until healthy.
   - **kuadrant-operator** last — as the policy reconciler, it may depend on new CRD fields or operand capabilities introduced by the dependency updates. Updating it before dependencies would risk it referencing CRD properties that don't exist yet.

# Reference-level explanation
[reference-level-explanation]: #reference-level-explanation

### Helm as Unified Manifest Source

The umbrella operator renders Helm charts at runtime using Helm's Go SDK with `ClientOnly=true` and `DryRun=true` — pure template rendering with no Helm release tracking. Rendered manifests are applied via controller-runtime. Charts are embedded in the umbrella operator image at build time.

This is the same pattern used by [cluster-olm-operator](https://github.com/openshift/cluster-olm-operator), which uses Helm as a templating engine rather than a package manager.

### Helm Chart Refactoring

The current kuadrant-operator chart (`charts/kuadrant-operator/`) is a monolithic kustomize-generated blob — a single `manifests.yaml` of ~14,500 lines produced by `kustomize build config/helm`. This must be refactored to support per-component rendering, ordered deployment, and operand creation.

#### Current state

- One chart with a single `templates/manifests.yaml` containing all operators, CRDs, and RBAC
- `Chart.yaml` declares subcharts for authorino-operator, limitador-operator, and dns-operator (pulled from `https://kuadrant.io/helm-charts/`)
- `values.yaml` is nearly empty — no configurable values exposed
- Dependency operator manifests pulled via kustomize remote refs (e.g., `github.com/Kuadrant/authorino-operator/config/deploy?ref=main`)
- No operand templates — operand creation is handled by kuadrant-operator at runtime

#### Required changes

1. **Replace kustomize generation with per-component Helm templates** — the kuadrant-operator chart needs templates instead of a kustomize dump. One template per component (containing its Deployment, Service, RBAC, ServiceAccount). CRDs are the exception: they must be separated (via Helm's `crds/` directory or a dedicated template) since operand CRs cannot be applied before their CRD is established.
2. **Configurable values.yaml** — image references, namespaces, replica counts, and resource limits exposed as values. The umbrella operator injects version-pinned values at render time; direct Helm users override via `--set` or values files.
3. **Operand templates** — the chart includes templates for operand CRs (Authorino, Limitador instances). These are created at install time on both paths, replacing the current runtime creation by kuadrant-operator.
4. **Dependency operator subcharts** — these already exist in their respective repos. The parent chart's `Chart.yaml` dependencies remain.
5. **Ordered rendering support** — the umbrella operator renders and applies subcharts sequentially: CRDs first, then dependency operators, then kuadrant-operator.

### Kuadrant CR Scope Change

Today the Kuadrant CR serves two purposes: its existence triggers operand creation (Authorino, Limitador instances), and its spec configures runtime behaviour. With this proposal, these concerns are separated:

- **Operand creation** moves out of kuadrant-operator. On both install paths, operands are created at install time — by the umbrella operator (OLMv1) or by the Helm chart directly (`helm install`). The Kuadrant CR is no longer the install trigger.
- **Runtime configuration** stays with the Kuadrant CR. 

The Kuadrant CRD is deployed by the umbrella operator (as part of the Helm chart), but kuadrant-operator remains the controller that reconciles Kuadrant CRs. 

No breaking API change is required — the Kuadrant CR spec remains the same. The behavioural change is that operands exist before the Kuadrant CR is created, rather than being created in response to it. This applies to both install paths: the Helm chart includes operand CR templates, so `helm install` and the umbrella operator produce the same result. The kuadrant-operator no longer needs operand creation logic in either case.

### Version Pinning

Each umbrella operator release pins component versions via Helm values embedded at build time. Image references for all operators and operands are set through the charts' `values.yaml`, ensuring a single mechanism for version control across both install paths.

### RBAC

The umbrella operator's ServiceAccount requires broad permissions. These are granted via the ClusterExtension's ServiceAccount. Each component operator's RBAC is part of its rendered chart.

### Failure Scenarios

- **Deployment rollout failure** — reports degraded status and blocks further upgrades (e.g., kuadrant-operator is not upgraded if a dependency failed).
- **Image pull failure** — surfaced via the Deployment's ImagePullBackOff condition.
- **CRD conflict** — if a CRD already exists (e.g., standalone Authorino), reports an error rather than silently overwriting.
- **Rollback** — reverting the ClusterExtension to the previous version reconciles all Deployments back to prior image references.

# Drawbacks
[drawbacks]: #drawbacks

- **New operator to maintain** — new codebase, image, build pipeline, and release process.
- **Broad RBAC** — creating CRDs, ClusterRoles, and Deployments across namespaces is inherent to the pattern but is a wide permission set.
- **Version coordination** — all operators must be released and tested together; compatibility matrix must be maintained.
- **Two operators to debug** — users must distinguish umbrella operator problems (deployment) from kuadrant-operator problems (policy reconciliation).

# Rationale and alternatives
[rationale-and-alternatives]: #rationale-and-alternatives

### Why this design

- **Separation of concerns** — kuadrant-operator is simplified; deployment orchestration is cleanly separated.
- **No runtime OLMv1 dependency** — manages Deployments directly, reducing blast radius of OLMv1 issues.
- **Helm reuse** — existing charts from dependency operator repos are consumed as-is. No manifest duplication. Helm becomes more of a first class citizen
- **Proven pattern** — cluster-olm-operator uses Helm client-only rendering at scale in production.

### Alternatives considered

**Single OLMv1 bundle containing all operators** — all-or-nothing; cannot support selective deployment as Kuadrant grows. Doesn't put helm front and centre

# Future possibilities
[future-possibilities]: #future-possibilities

- **Selective component deployment** — a CR API (e.g., `KuadrantInstall`) mapping enabled policies to required components. Safety checks prevent removing components with active policy CRs.

# Prior art
[prior-art]: #prior-art

- **[cluster-olm-operator](https://github.com/openshift/cluster-olm-operator)** — primary reference. Manages OLMv1 sub-components using Helm charts rendered client-only, with static resource controllers and deployment controllers for applying the rendered manifests.
- **[OLMv0 dependency resolution](https://olm.operatorframework.io/docs/concepts/olm-architecture/dependency-resolution/)** — the current model being replaced. Resolves dependencies automatically via CRD-based declarations.

# Unresolved questions
[unresolved-questions]: #unresolved-questions

- **CRD ownership conflicts** — migration path for users with standalone installations (e.g., standalone Authorino) transitioning to the umbrella operator.
- **Installation status reporting** — today, installation status (component health, readiness) is written to the Kuadrant CR's status. With operand creation moving out of kuadrant-operator, where should installation status live?
