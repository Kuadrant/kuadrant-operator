# OLMv1 Phase 1 — OLM Dependencies Deprecation

## Goal

Remove OLM operator dependencies (`dependencies.yaml`) from kuadrant-operator. OLMv1 no longer supports automatic installation of dependent operators, requiring the kuadrant-operator to manage component controller lifecycle directly.

## Approach: Umbrella Operator

The kuadrant-operator becomes an umbrella operator that deploys component controllers (authorino-operator, limitador-operator, dns-operator, mcp-gateway) itself at runtime, rather than relying on OLM to install them as separate operators.

**Before Phase 1**: 4 OLM operators (kuadrant, authorino, limitador, dns) — each with its own CSV, bundle image, and catalog entry. OLM manages all lifecycles independently. dns-operator was additionally deployed via kustomize remote refs separately from the Kuadrant CR lifecycle.

**After Phase 1**: 1 OLM operator (kuadrant-operator) — single CSV, single bundle, single catalog entry. Component controllers are no longer OLM packages. They become internal controller deployments whose lifecycle is managed by the kuadrant-operator. No component controller bundles, catalogs, or CSVs need to be maintained.

## Key Architectural Decisions

### 1. All component controllers deployed via Helm chart rendering, triggered by Kuadrant CR

- When a user creates a Kuadrant CR, the umbrella operator renders each component controller's Helm chart and applies the resources (Deployment, ServiceAccount, ClusterRoleBinding, etc.) via Server-Side Apply
- **All** component controllers now hang off the Kuadrant CR — including dns-operator, which on main was deployed via kustomize remote refs independently of the Kuadrant CR lifecycle
- Charts are pulled from the upstream component repos and committed to the kuadrant-operator repo
- A generic `make sync-child-operator-charts` target handles both simple charts (dns/authorino/limitador) and mature charts with full Helm templating (mcp-gateway)

### 2. RBAC: bind/escalate only — no permission duplication

- The umbrella operator does NOT need to duplicate component controller permissions
- Kubernetes `bind` verb allows creating ClusterRoleBindings to named ClusterRoles without holding all their permissions
- Only ClusterRole **names** need tracking, not their contents — when a component controller changes its RBAC, no umbrella operator change is needed unless a role is added or renamed
- Tested and confirmed working on a live cluster

### 3. Two supported installation methods

- **Helm**: Single `helm install` deploys everything — CRDs, ClusterRoles, and the operator. `make local-setup-helm` for local dev
- **OLM**: Single operator in the catalog, bundle includes all component controller CRDs and ClusterRoles. No component controller bundles in the catalog
- Both paths share the same environment setup (`local-env-setup`), differing only in how the operator is deployed

### 4. Clear separation of cluster-scoped and namespaced resources

- **Cluster-scoped resources** (CRDs, ClusterRoles) are managed by the installation method (Helm or OLM) — they exist before the operator starts and before any Kuadrant CR is created
- **Namespaced resources** (Deployments, ServiceAccounts, Roles, RoleBindings, ConfigMaps, Services) and **ClusterRoleBindings** are managed by the kuadrant-operator at runtime when a Kuadrant CR is created
- CRDs and ClusterRoles are extracted from component charts during `make sync-child-operator-charts` and included in both the Helm chart and OLM bundle

### 5. Simplified OLM bundle/catalog pipeline

Previously the kuadrant-operator bundle/catalog pipeline had to manage four operator bundles — it pulled, validated, and assembled bundles for authorino-operator, limitador-operator, and dns-operator alongside its own. This required version variables, bundle image references, and dependency injection for each component.

Now the pipeline only deals with a single bundle (kuadrant-operator itself):

- Removed all component controller bundle variables, catalog channel entries, and dependency injection
- Catalog now contains only the kuadrant-operator package (was 4 packages)
- Removed `operator-sdk` and `opm` as bundle prerequisites
- Component controller CRDs and ClusterRoles are included in the kuadrant-operator bundle directly, rather than being pulled from separate bundle images

### 6. Wrapper CRs preserved (minimal migration risk)

- Authorino CR and Limitador CR are still created by kuadrant-operator
- Component controllers reconcile these wrapper CRs to create workloads — no change to this flow
- Users see no difference in behaviour
- During migration, control plane resources (component controller Deployments, SAs, ClusterRoleBindings) can be safely deleted and recreated by the umbrella operator — these don't affect the data plane
- The data plane workloads (Authorino Deployment, Limitador Deployment) are owned by the wrapper CRs via ownerReference. As long as the wrapper CRs are not deleted, workloads continue running uninterrupted
- The only risk window is the brief period where component controllers aren't running and therefore not reconciling — but existing workloads remain healthy and serving traffic

## What Changes in Component Repos

**No changes are required** in component repos for Phase 1 to work. The kuadrant-operator pulls charts from upstream repos as-is via `make sync-child-operator-charts`.

However, since component repos no longer need to produce their own OLM bundles or catalogs, the following OLM-specific artefacts become unnecessary and **could be removed** to reduce maintenance burden:

- `bundle/`, `catalog/`, `config/manifests/`, `config/deploy/olm/`, `config/scorecard/`
- `bundle.Dockerfile`, `make/catalog.mk`, `generate-catalog.sh`
- All OLM-related make targets, variables, and CI jobs
- `operator-sdk` and `opm` tool dependencies

Each component repo retains its Helm chart, kustomize layers, and application code. The kustomize layers (`config/crd/`, `config/rbac/`, `config/manager/`, `config/default/`) are shared between the Helm chart and local dev paths — removing the OLM overlay doesn't affect them.

A reference cleanup has been done on the dns-operator `olmv1-umbrella-poc-phase1` branch, removing ~1,800 lines. Similar reductions are expected for authorino-operator and limitador-operator. See [Child Operator Cleanup](olmv1-child-operator-cleanup.md) for the full audit.

## Cluster State

**After installation (no Kuadrant CR):**

- 1 operator deployment (kuadrant-operator)
- All CRDs installed (kuadrant + component CRDs)
- Component controller ClusterRoles installed (no SAs or bindings yet)

**After Kuadrant CR creation:**

- 4 component controller deployments (authorino-operator, limitador-operator, dns-operator, mcp-gateway)
- Component controller SAs and ClusterRoleBindings created via bind/escalate
- Authorino and Limitador workloads deployed by their respective controllers

## Migration Considerations

Moving from the current multi-operator OLM model to the umbrella operator requires handling several areas:

### CRD ownership transfer

Currently, each child operator's OLM bundle "owns" its CRDs. After migration, the kuadrant-operator bundle owns all CRDs. OLM tracks ownership via annotations — these will need updating during the transition. OLM does not delete CRDs when removing operators, so existing CRDs will persist but their ownership metadata needs to reflect the new single-bundle model.

### Orphaned OLM resources

When the kuadrant-operator bundle no longer declares `olm.package.required` dependencies, OLM will not automatically uninstall the previously resolved child operators. Their Subscriptions, CSVs, and associated resources become orphaned and must be explicitly cleaned up as part of the migration.

### Control plane resource conflicts

The umbrella operator creates component controller Deployments, ServiceAccounts, and ClusterRoleBindings that may already exist from the previous OLM installation. These resources may have different field managers (OLM vs kuadrant-operator SSA). The safest approach is to delete the old control plane resources before or during upgrade — this does not affect the data plane.

### Data plane continuity

Wrapper CRs (Authorino CR, Limitador CR) must be preserved throughout the migration. These own the data plane workloads (Authorino/Limitador Deployments) via ownerReference. As long as the wrapper CRs are not deleted, workloads continue serving traffic. The only impact is a brief window where component controllers aren't reconciling, but existing workloads remain healthy.

### Resource naming consistency

To minimise migration friction, resource names produced by the Helm chart rendering should match the names currently used by OLM-installed operators as closely as possible. Where upstream repos have renamed resources (e.g. `limitador-operator-manager` → `limitador-operator-manager-role`), we should assess whether the Helm chart can preserve the original names to avoid orphaned resources and reduce cleanup requirements during migration.

### Helm vs OLM upgrade path

- **OLM users**: Upgrade the kuadrant-operator subscription to the new bundle version, then clean up orphaned child operator subscriptions
- **Helm users**: `helm upgrade` replaces the old chart (which had child operator chart dependencies) with the new chart (which includes everything). Cleaner path — Helm manages the full lifecycle

### Consistent labelling of managed resources

Since the umbrella operator now controls the deployment of all component controllers, we can apply consistent labels to every resource it creates. This addresses existing gaps — for example, the Authorino Deployment currently lacks distinguishing labels, which prevents it from being filtered in the kuadrant-operator topology (only Limitador Deployment is tracked today). With consistent labels (e.g. `kuadrant.io/managed-by: kuadrant-operator`, `kuadrant.io/component: authorino`) applied at render time, all managed resources become discoverable, filterable, and trackable in the topology DAG.

## Future Phases

The following items are out of scope for Phase 1 but represent known improvements that build on the umbrella operator foundation. Phase 1 focuses solely on removing OLM dependencies — these items address further simplification, flexibility, and maintainability.

### Remove intermediate operator layers

Currently, the path from Kuadrant CR to workload goes through two layers: kuadrant-operator → child operator (e.g. authorino-operator) → workload (e.g. Authorino). The child operator layer exists primarily to reconcile wrapper CRs into Deployments. In a future phase, the kuadrant-operator could deploy workloads directly, removing the child operator Deployments entirely and reducing the number of running controllers.

### Wrapper CR removal

The Authorino CR and Limitador CR are intermediate resources that the child operators reconcile into workloads. If the intermediate operator layer is removed, wrapper CRs become unnecessary — the kuadrant-operator would manage workload Deployments, ConfigMaps, and Services directly. This simplifies the resource ownership chain but requires migrating any user customisations currently applied via wrapper CR fields.

### Selective component installation

Add a `spec.components` field to the Kuadrant CRD allowing users to enable/disable individual components (e.g. install Kuadrant without DNS, or without MCP Gateway). The umbrella operator already deploys components independently — this would add user-facing control over which components are deployed when the Kuadrant CR is created.

### Centralised Helm chart repository

Move component Helm charts from individual repos (dns-operator, authorino-operator, etc.) to a shared repository (e.g. `Kuadrant/helm-charts`). This would centralise chart maintenance, simplify the `sync-child-operator-charts` process, and provide a single source for all Kuadrant Helm charts. The kuadrant-operator would pull from one repo instead of four.

### Improved Helm chart templating for simple components

The dns-operator, limitador-operator, and authorino-operator charts currently use minimal templating — a single `manifests.yaml` generated by kustomize with only `{{ .Release.Namespace }}` substitution. These could be improved with proper Helm templating (configurable values, helpers, conditionals) similar to the mcp-gateway chart, enabling more flexible configuration when deployed by the umbrella operator.

## Architecture Diagrams

See [OLMv1 Phase 1 Architecture](olmv1-phase1-architecture.md) for diagrams covering build-time chart sync, cluster state, runtime reconciliation chain, RBAC model, and resource ownership.

For comparison with the current architecture, see [Current Architecture (main branch)](current-architecture-main.md).

## Branches

- `kuadrant-operator`: `olmv1-umbrella-poc-phase1`
- `dns-operator`: `olmv1-umbrella-poc-phase1` (OLM artefact cleanup reference)
