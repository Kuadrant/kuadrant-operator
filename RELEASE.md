# How to release Kuadrant Operator

## Kuadrant Operator Release

To release a version _“v0.W.Z”_ of Kuadrant Operator in GitHub and Quay.io, follow these steps:

### Release file format.
This example of the `release.yaml` file uses tag `v1.0.1` as reference.

```yaml
# FILE: ./release.yaml
kuadrant:
  release: "1.0.1"
olm:
 channels:
 - "alpha"
 default_channel: "alpha"
dependencies:
 Authorino: "0.16.0"
 Console_plugin: "0.0.14"
 DNS: "0.12.0"
 Limitador: "0.12.1"
 Wasm_shim: "0.8.1"
```
The `kuadrant` section relates to the release version of the kuadrant operator.
While the `olm` section relates to fields required for building the olm catalogs.
And the `dependencies` section relates to the released versions of the subcomponents that will be included in a release.
There are validation steps during the `make prepare-release` that require the dependencies to be release before generating the release of the Kuadrant operator.


### Local steps

1. Create the `release-vX.Y` branch, if the branch does not already exist. 
2. Push the `release-vX.Y` to the remote (kuadrant/kuadrant-operator)
3. Create the `release-vX.Y.Z-rc(n)` branch with `release-vX.Y` as the base.
4. Cherry-pick commits to the `kudrant-vX.Y.Z-rc(n)` from the relevant sources, i.e. `main`.
5. Update the applicable version in the `release.yaml`.
6. Run `make prepare-release` on the `release-vX.Y.Z-rc(n)` 


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

## Kuadrant Operator Dependencies

1. Release Kuadrant dependencies as required:
   * [Authorino Operator](https://github.com/Kuadrant/authorino-operator/blob/main/RELEASE.md).
   * [Limitador Operator](https://github.com/Kuadrant/limitador-operator/blob/main/RELEASE.md).
   * [DNS Operator](https://github.com/Kuadrant/dns-operator/blob/main/docs/RELEASE.md).
   * [WASM Shim](https://github.com/Kuadrant/wasm-shim/).
   * [Console Plugin](https://github.com/Kuadrant/kuadrant-console-plugin).

2. Update the `release.yaml` with the versions of required dependencies.



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

