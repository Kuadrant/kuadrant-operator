# How to release Kuadrant Operator

To release a version _"vX.Y.Z"_ of Kuadrant Operator in GitHub and Quay.io, first make sure every
[Kuadrant Operator dependency](https://github.com/Kuadrant/kuadrant-operator/blob/main/RELEASE.md#kuadrant-operator-dependencies)
has already been released.

## Overview

A release follows two sequential PR phases:

1. **RC PR** (`release-X.Y.Z-rc(n)` → `release-X.Y`): Requires **Engineering** approval.
   Merging this PR triggers release candidate images to be built and published to Quay for QE testing.

2. **GA PR** (`prepare-release-X.Y.Z` → `release-X.Y`): Requires **QE** approval.
   QE's approval of this PR serves as their sign-off on the release candidate.
   Merging this PR triggers the final GitHub release, tags, and images published to Quay and the Helm repository.

## Creating a Release Candidate (RC)

### When to create a new RC branch

- For **RC1 of a new point release** (e.g. `1.4.0-rc1`), the [Automated Workflow](#automated-workflow)
  can create the RC PR automatically. All subsequent RCs or patch releases must use the [Manual Workflow](#manual-workflow).
- For **RC2+** or any **patch release RC**, always use the [Manual Workflow](#manual-workflow).

### Automated Workflow

> _Only usable for RC1 of a new point release (e.g. `v1.4.0-rc1`).
> The underlying [GitHub action](https://github.com/peter-evans/create-pull-request) does not support
> creating a pull request where the initial base branch is not the target branch,
> so this path cannot be used for patch releases or RC2+._
> See [slack thread](https://kubernetes.slack.com/archives/C05J0D0V525/p1744192632523239?thread_ts=1744093102.246619&cid=C05J0D0V525).

1. Run the GHA [Automated Release](https://github.com/Kuadrant/kuadrant-operator/actions/workflows/automated-release.yaml)
   filling the following fields:
   - `gitRef`: branch/tag/commit to cut the release from.
   - `kuadrantOperatorVersion`: the [Semantic Version](https://semver.org/) of the desired release (e.g. `1.4.0-rc1`).
   - `authorinoOperatorVersion`: Authorino Operator version (X.Y.Z)
   - `limitadorOperatorVersion`: Limitador Operator version (X.Y.Z)
   - `dnsOperatorVersion`: DNS Operator version (X.Y.Z)
   - `wasmShimVersion`: WASM Shim version (X.Y.Z)
   - `consolePluginVersion`: ConsolePlugin version (X.Y.Z)
   - `developerPortalControllerVersion`: Developer Portal Controller version (X.Y.Z)
   - `olmChannel`: sets the OLM `channels` and `default-channel` annotations

2. The workflow opens a PR (`release-X.Y.Z-rc(n)` → `release-X.Y`).
   An **Engineering** team member must review and approve it, focusing on changes in Kustomize config,
   OLM bundles, and Helm Charts.

3. Once merged, the RC build workflow triggers automatically, building and publishing RC images to Quay.

> **Note:** It's not possible to cherry-pick commits via this workflow.
> The workflow picks the selected branch/tag/commit and all history behind it.

### Manual Workflow

### Local steps

1. Create the `release-X.Y` branch if it does not already exist, then push it to the remote (`kuadrant/kuadrant-operator`).
2. Create the `release-X.Y.Z-rc(n)` branch with `release-X.Y` as the base.
3. Cherry-pick commits from the relevant sources (e.g. `main`) onto `release-X.Y.Z-rc(n)`.
4. Update the applicable versions in the [release.yaml](#release-file-format).
5. Run `make prepare-release` on the `release-X.Y.Z-rc(n)` branch.
   If you hit rate-limit errors, set `GITHUB_TOKEN` to your PAT.
6. Commit and push the branch.

### Remote steps

1. Open a PR from `release-X.Y.Z-rc(n)` targeting `release-X.Y`.
   ([Example PR](https://github.com/Kuadrant/kuadrant-operator/pull/2058))
2. CI verification checks will run.
3. An **Engineering** team member must review and approve the PR, focusing on:
   * `./bundle.Dockerfile`
   * `./bundle`
   * `./config`
   * `./charts/`
4. Merge the PR. The RC build workflow triggers automatically, building and publishing RC images to Quay.

## Promoting a Release Candidate to GA

Once the RC PR is merged, open the GA PR immediately. QE's approval of that PR serves as their sign-off.

### Local steps

1. Create a `prepare-release-X.Y.Z` branch based on `release-X.Y`.
   Example: `git checkout -B prepare-release-1.4.5 release-1.4`
2. Update `release.yaml`: set the GA version (remove the `-rc(n)` suffix).
   Only update dependency versions if required to resolve a blocking issue found during RC testing.
3. Run `make prepare-release` and commit the changes.
4. Push the branch.

### Remote steps

1. Open a PR from `prepare-release-X.Y.Z` targeting `release-X.Y`.
   ([Example PR](https://github.com/Kuadrant/kuadrant-operator/pull/2059))
2. CI verification checks will run.
3. **At least one QE team member must review and approve the PR** (their approval is the RC sign-off),
   focusing on:
   * `./bundle.Dockerfile`
   * `./bundle`
   * `./config`
   * `./charts/`
4. Merge the PR. This triggers the release workflow on `release-X.Y`, which:
   * Creates the GitHub release
   * Creates tags
   * Builds and publishes final images and packages to Quay and the Helm repository.
5. Verify that the [build release tag workflow](https://github.com/Kuadrant/kuadrant-operator/actions/workflows/build-images-for-tag-release.yaml)
   is triggered and completes for the new tag.

> **Note:** Release notes may be incorrectly generated and need to be manually regenerated.
> Edit the release notes in the GitHub UI and select the correct previous tag.
> Resolution is tracked in [#1211](https://github.com/Kuadrant/kuadrant-operator/issues/1211).

## Creating RC2+

If a blocking issue is found during RC testing, create a new RC:

1. Create a local branch based on the targeted release branch.
   Example: `git checkout -B release-1.2.0-rc2 release-1.2`
2. Cherry-pick the commits that fix the blocking issue.
   Example: `git cherry-pick <commit sha>`
3. Update `release.yaml` with the new RC version of kuadrant-operator.
   Only update dependency versions if required to resolve the issue.
4. Run `make prepare-release` and commit the changes.
5. Follow [Manual Workflow / Remote steps](#remote-steps) above (Engineering approval required).

## Release file format

This example of the `release.yaml` file uses tag `v1.0.1` as reference.

```yaml
# FILE: ./release.yaml
kuadrant-operator:
  version: "1.0.1"
olm:
  default-channel: "stable"
  channels:
    - "stable"
dependencies:
  authorino-operator: "0.16.0"
  console-plugin: "0.0.14"
  developer-portal-controller: "0.1.0"
  dns-operator: "0.12.0"
  limitador-operator: "0.12.1"
  wasm-shim: "0.8.1"
```

The `kuadrant-operator` section relates to the release version of the kuadrant operator.
The `olm` section relates to fields required for building the OLM catalogs.
The `dependencies` section relates to the released versions of the subcomponents included in the release.

> There are validation steps during `make prepare-release` that require dependencies to be released
> before generating the Kuadrant Operator release.

## Kuadrant Operator Dependencies

   * [Authorino Operator](https://github.com/Kuadrant/authorino-operator/blob/main/RELEASE.md)
   * [Limitador Operator](https://github.com/Kuadrant/limitador-operator/blob/main/RELEASE.md)
   * [DNS Operator](https://github.com/Kuadrant/dns-operator/blob/main/docs/RELEASE.md)
   * [WASM Shim](https://github.com/Kuadrant/wasm-shim/)
   * [Console Plugin](https://github.com/Kuadrant/kuadrant-console-plugin)
   * [Developer Portal Controller](https://github.com/Kuadrant/developer-portal-controller/blob/main/RELEASE.md)

## Verification

### Verify OLM Deployment

1. Deploy the OLM catalog image following [Deploy kuadrant operator using OLM](/doc/overviews/development.md#deploy-kuadrant-operator-using-olm),
   providing the generated catalog image. For example:
```sh
make deploy-catalog CATALOG_IMG=quay.io/kuadrant/kuadrant-operator-catalog:v1.0.0-rc4
```

2. Wait for deployment:
```sh
kubectl -n kuadrant-system wait --timeout=60s --for=condition=Available deployments --all
```

The output should be:

```
deployment.apps/authorino-operator condition met
deployment.apps/dns-operator-controller-manager condition met
deployment.apps/kuadrant-operator-controller-manager condition met
deployment.apps/limitador-operator-controller-manager condition met
deployment.apps/kuadrant-operator-controller-manager condition met
```

3. Check the logs:
```sh
kubectl -n kuadrant-system logs -f deployment/kuadrant-operator-controller-manager
```

4. Check the version of the components deployed:
```sh
kubectl -n kuadrant-system get deployment -o yaml | grep "image:"
```
The output should be something like:

```
image: quay.io/kuadrant/authorino-operator:v0.14.0
image: quay.io/kuadrant/dns-operator:v0.8.0
image: quay.io/kuadrant/kuadrant-operator:v1.0.0-rc4
image: quay.io/kuadrant/limitador-operator:v0.12.0
```

## Release Community Operator Index Catalogs

- [Operatorhub Community Operators](https://github.com/k8s-operatorhub/community-operators)
- [Openshift Community Operators](http://github.com/redhat-openshift-ecosystem/community-operators-prod)

Open a PR on each index catalog ([example](https://github.com/redhat-openshift-ecosystem/community-operators-prod/pull/1595) |
[docs](https://redhat-openshift-ecosystem.github.io/community-operators-prod/operator-release-process/)).

The usual steps are:

1. Start a new branch named `kuadrant-operator-v0.W.Z`

2. Create a new directory `operators/kuadrant-operator/0.W.Z` containing:

    * Copy the bundle files from `github.com/kuadrant/kuadrant-operator/tree/v0.W.Z/bundle`
    * Copy `github.com/kuadrant/kuadrant-operator/tree/v0.W.Z/bundle.Dockerfile` with the proper fix to the COPY commands
      (i.e. remove `/bundle` from the paths)
