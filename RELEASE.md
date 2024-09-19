# How to release Kuadrant Operator

## Process


### Regular Minor Release
Before releasing a new minor version of the Kuadrant Operator each dependency (see below) should also have a minor release if it has changes since the last release.

For each component, compare the git commit on the latest release against main if it has changed, beyond just version bumps, follow the release process for that dependency (linked below) before doing a release of Kuadrant-Operator. You will need to gather the new released versions as you go to use with the release of the kuadrant-operator.

To release a version _“v0.W.Z”_ of Kuadrant Operator in GitHub and Quay.io, follow these steps:

1. Kuadrant dependencies need to be released first:
   * [Authorino Operator](https://github.com/Kuadrant/authorino-operator/blob/main/RELEASE.md).
   * [Limitador Operator](https://github.com/Kuadrant/limitador-operator/blob/main/RELEASE.md).
   * [DNS Operator](https://github.com/Kuadrant/dns-operator/blob/main/docs/RELEASE.md).
   * [WASM Shim](https://github.com/Kuadrant/wasm-shim/).

2. Run the GHA [Release operator](https://github.com/Kuadrant/kuadrant-operator/actions/workflows/release.yaml); make
   sure to fill all the fields:

    * Branch containing the release workflow file – default: `main`
    * Commit SHA or branch name of the operator to release – usually: `main`
    * Operator version to release (without prefix) – i.e. `0.W.Z`
    * Kuadrant dependencies (WASM Shim, Authorino, Limitador and DNS operators) versions (without prefix) – i.e. `0.X.Y`
    * Operator replaced version (without prefix) – i.e. `0.P.Q`
    * If the release is a prerelease

3. Run the GHA [Build and push images](https://github.com/Kuadrant/kuadrant-operator/actions/workflows/build-images-base.yaml)
   specifying ‘Kuadrant operator version’ and its dependencies equals to the previously released versions _“0.X.Y”_. This will cause the
   new images (bundle and catalog included) to be built and pushed to the corresponding repos in
   [quay.io/kuadrant](https://quay.io/organization/kuadrant).


### Publishing the Operator in OpenShift Community Operators
Open a PR in the [OpenShift Community Operators repo](http://github.com/redhat-openshift-ecosystem/community-operators-prod)
([example](https://github.com/redhat-openshift-ecosystem/community-operators-prod/pull/1595) |
[docs](https://redhat-openshift-ecosystem.github.io/community-operators-prod/operator-release-process/)).

The usual steps are:

1. Start a new branch named `kuadrant-operator-v0.W.Z`

2. Create a new directory `operators/kuadrant-operator/0.W.Z` containing:

    * Copy the bundle files from `github.com/kuadrant/kuadrant-operator/tree/v0.W.Z/bundle`
    * Copy `github.com/kuadrant/kuadrant-operator/tree/v0.W.Z/bundle.Dockerfile` with the proper fix to the COPY commands
      (i.e. remove /bundle from the paths)

### Publishing the Operator in Kubernetes Community Operators (OperatorHub.io)

1. Open a PR in the [Kubernetes Community Operators repo](https://github.com/k8s-operatorhub/community-operators)
   ([example](https://github.com/k8s-operatorhub/community-operators/pull/1655) | [docs](https://operatorhub.io/contribute)).

2. The usual steps are the same as for the
   [OpenShift Community Operators](https://docs.google.com/document/d/1tLveyv8Zwe0wKyfUTWOlEnFeMB5aVGqIVDUjVYWax0U/edit#heading=h.b5tapxn4sbk5)
   hub.
