# How to release Kuadrant Operator

To release a version _“vX.Y.Z”_ of Kuadrant Operator in GitHub and Quay.io, there are two options, a manual and an automated process.
For both processes, first make sure every [Kuadrant Operator dependency](https://github.com/Kuadrant/kuadrant-operator/blob/main/RELEASE.md#kuadrant-operator-dependencies) has been already released.

## Automated Workflow

1. Run the GHA  [Automated Release](https://github.com/Kuadrant/kuadrant-operator/actions/workflows/automated-release.yaml)
   filling the following fields:
   - gitRef: Select the branch/tag/commit where you want to cut a release from.
   - kuadrantOperatorVersion: the [Semantic Version](https://semver.org/) of the desired release.
   - authorinoOperatorVersion: Authorino Operator bundle version (X.Y.Z)
   - limitadorOperatorVersion: Limitador Operator bundle version (X.Y.Z)
   - dnsOperatorVersion: DNS Operator bundle version (X.Y.Z)
   - wasmShimVersion: WASM Shim version (X.Y.Z)
   - consolePluginVersion: ConsolePlugin version (X.Y.Z)
   - olmChannel: This will set the OLM `channels` and `default-channel` annotations
2. The workflow will create a Pull Request that should be peer-reviewed and approved by a member of the Kuadrant team, focusing on the changes made in Kustomize config, OLM bundles and Helm Charts.
3. Once the PR is merged, a release workflow will be triggered tagging and publishing the [Github release](https://github.com/Kuadrant/kuadrant-operator/releases)
   it will also build the images and packages and publish them on Quay, Helm repository.

### Notes
* It's not possible to cherry-pick commits, the workflow will pick a branch/tag/commit and all the history behind to the PR.

## Manual Workflow

### Local steps

1. Create the `release-vX.Y` branch, if the branch does not already exist. 
2. Push the `release-vX.Y` to the remote (kuadrant/kuadrant-operator)
3. Create the `release-vX.Y.Z-rc(n)` branch with `release-vX.Y` as the base.
4. Cherry-pick commits to the `kudrant-vX.Y.Z-rc(n)` from the relevant sources, i.e. `main`.
5. Update the applicable version in the [release.yaml](https://github.com/Kuadrant/kuadrant-operator/blob/main/RELEASE.md#release-file-format).
6. Run `make prepare-release` on the `release-vX.Y.Z-rc(n)`. If you run into rate limit errors, set the env `GITHUB_TOKEN` with your PAT.

### Remote steps

1. Open a PR against the `release-vX.Y` branch with the changes from `release-vX.Y.Z-rc(n)` branch. 
2. PR verification checks will run.
3. Get manual review of PR with focus on changes in these areas:
   * `./bundle.Dockerfile`
   * `./bundle`
   * `./config`
   * `./charts/`
4. Merge PR
5. Run the Release Workflow on the `release-vX.Y`. This does the following:
   * Creates the GitHub release
   * Creates tags
6. Verify that the build [release tag workflow](https://github.com/Kuadrant/kuadrant-operator/actions/workflows/build-images-for-tag-release.yaml) is triggered and completes for the new tag.

## Release file format.
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
  dns-operator: "0.12.0"
  limitador-operator: "0.12.1"
  wasm-shim: "0.8.1"
```

The `kuadrant-operator` section relates to the release version of the kuadrant operator.
While the `olm` section relates to fields required for building the olm catalogs.
And the `dependencies` section relates to the released versions of the subcomponents that will be included in a release.
There are validation steps during the `make prepare-release` that require the dependencies to be release before generating the release of the Kuadrant operator.

## Kuadrant Operator Dependencies
   * [Authorino Operator](https://github.com/Kuadrant/authorino-operator/blob/main/RELEASE.md).
   * [Limitador Operator](https://github.com/Kuadrant/limitador-operator/blob/main/RELEASE.md).
   * [DNS Operator](https://github.com/Kuadrant/dns-operator/blob/main/docs/RELEASE.md).
   * [WASM Shim](https://github.com/Kuadrant/wasm-shim/).
   * [Console Plugin](https://github.com/Kuadrant/kuadrant-console-plugin).

## Verification 

### Verify OLM Deployment

1. Deploy the OLM catalog image following the [Deploy kuadrant operator using OLM](/doc/development.md#deploy-kuadrant-operator-using-olm) and providing the generated catalog image. For example:
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
      (i.e. remove /bundle from the paths)

