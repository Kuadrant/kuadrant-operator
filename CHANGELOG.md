# Change Log

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](http://keepachangelog.com/)
and this project adheres to [Semantic Versioning](http://semver.org/).

## [Unreleased]

## [0.5.0] - 2023-12-05

### What's Changed
* refactor: controller-runtime v0.16.3 by @KevFan in https://github.com/Kuadrant/kuadrant-operator/pull/246
* Update gatewayapi to v1.0.0 by @adam-cattermole in https://github.com/Kuadrant/kuadrant-operator/pull/286
* Add mandatory Gateway API label to the policy CRDs by @guicassolato in https://github.com/Kuadrant/kuadrant-operator/pull/295
* Update Keycloak examples by @guicassolato in https://github.com/Kuadrant/kuadrant-operator/pull/311
* Gh 639 policy controller by @maleck13 in https://github.com/Kuadrant/kuadrant-operator/pull/293
* Update bundle (policy-controller) by @mikenairn in https://github.com/Kuadrant/kuadrant-operator/pull/313
* Update istio to 1.20 by @didierofrivia in https://github.com/Kuadrant/kuadrant-operator/pull/303
* Allow the coverage to drop by 3% by @eguzki in https://github.com/Kuadrant/kuadrant-operator/pull/316
* fix authconfig hosts when targeting gateway by @eguzki in https://github.com/Kuadrant/kuadrant-operator/pull/310
* Using Limitador CR condition ready by @didierofrivia in https://github.com/Kuadrant/kuadrant-operator/pull/324
* Upgrading operator-sdk to v1.32.0 by @didierofrivia in https://github.com/Kuadrant/kuadrant-operator/pull/319
* Enhanced observability for the limitador instance by @eguzki in https://github.com/Kuadrant/kuadrant-operator/pull/301
* Maintenance/docs by @Boomatang in https://github.com/Kuadrant/kuadrant-operator/pull/294
* Update google.golang.org/grpc by @alexsnaps in https://github.com/Kuadrant/kuadrant-operator/pull/329
* Again:  name things by @alexsnaps in https://github.com/Kuadrant/kuadrant-operator/pull/336
* rlp e2e tests: fix sync issues by @eguzki in https://github.com/Kuadrant/kuadrant-operator/pull/337
* Updating istio dependencies by @didierofrivia in https://github.com/Kuadrant/kuadrant-operator/pull/331
* Fix nil pointer in parentRef namespace dereference by @adam-cattermole in https://github.com/Kuadrant/kuadrant-operator/pull/335
* fix integration tests: wait for route to be accepted by @eguzki in https://github.com/Kuadrant/kuadrant-operator/pull/339
* remove deployment of policy-controller for now by @maleck13 in https://github.com/Kuadrant/kuadrant-operator/pull/338
* Add better information for OperatorHub by @alexsnaps in https://github.com/Kuadrant/kuadrant-operator/pull/330
* fix authconfig reconciliation by @eguzki in https://github.com/Kuadrant/kuadrant-operator/pull/341
* docs: TLS and DNS Policy user guides by @mikenairn in https://github.com/Kuadrant/kuadrant-operator/pull/322
* Fix Istio AuthorizationPolicy mutate check by @guicassolato in https://github.com/Kuadrant/kuadrant-operator/pull/351
* Include missing unit test by @Boomatang in https://github.com/Kuadrant/kuadrant-operator/pull/344
* Dry-run resource update before comparing changes by @guicassolato in https://github.com/Kuadrant/kuadrant-operator/pull/356

### New Contributors
* @maleck13 made their first contribution in https://github.com/Kuadrant/kuadrant-operator/pull/293

## [0.4.1] - 2023-11-08

### What's Changed
* docs: fix user guide authenticated rl for app devs based on authpolicy/v1beta2 by @guicassolato in https://github.com/Kuadrant/kuadrant-operator/pull/282
* Indentation fix by @Ygnas in https://github.com/Kuadrant/kuadrant-operator/pull/284
* rename controller files by @guicassolato in https://github.com/Kuadrant/kuadrant-operator/pull/285
* Fix bug in response validation rules by @adam-cattermole in https://github.com/Kuadrant/kuadrant-operator/pull/287
* Bump google.golang.org/grpc from 1.54.0 to 1.56.3 by @dependabot in https://github.com/Kuadrant/kuadrant-operator/pull/288
* Propagate REPLACES_VERSION param when generating catalog files by @guicassolato in https://github.com/Kuadrant/kuadrant-operator/pull/291

### New Contributors
* @Ygnas made their first contribution in https://github.com/Kuadrant/kuadrant-operator/pull/284
* @dependabot made their first contribution in https://github.com/Kuadrant/kuadrant-operator/pull/288

## [0.4.0] - 2023-11-01

### What's Changed
* [test] Optimizations, improvements, and unit tests for common/common.go (part 1 of 3) by @art-tapin in https://github.com/Kuadrant/kuadrant-operator/pull/176
* Istio (v1.17.2) and Gateway API (v0.6.2) version bump by @guicassolato in https://github.com/Kuadrant/kuadrant-operator/pull/166
* [test] Optimizations, improvements, and unit tests for common/common.go (part 2 of 3) by @art-tapin in https://github.com/Kuadrant/kuadrant-operator/pull/182
* [fix: integration-tests] Ensure Istio gateways are ready by @guicassolato in https://github.com/Kuadrant/kuadrant-operator/pull/185
* changelog v0.3.0 by @eguzki in https://github.com/Kuadrant/kuadrant-operator/pull/184
* fix permissions by @eguzki in https://github.com/Kuadrant/kuadrant-operator/pull/189
* Optimize common package functions for improved performance by @art-tapin in https://github.com/Kuadrant/kuadrant-operator/pull/187
* fix e2e tests: do not use istio-ingressgateway in tests by @eguzki in https://github.com/Kuadrant/kuadrant-operator/pull/188
* [test] Unit-tests for common/k8s_utils.go (part 1 of 3) by @art-tapin in https://github.com/Kuadrant/kuadrant-operator/pull/190
* Build images with replaces image by @didierofrivia in https://github.com/Kuadrant/kuadrant-operator/pull/193
* Building the catalog with the replaces directive by @didierofrivia in https://github.com/Kuadrant/kuadrant-operator/pull/194
* upgrade operator-sdk v1.28.1 by @eguzki in https://github.com/Kuadrant/kuadrant-operator/pull/196
* Create a ServiceMeshMember, rather than mutating the ServiceMeshMembe… by @alexsnaps in https://github.com/Kuadrant/kuadrant-operator/pull/198
* [test] Unit-tests for common/k8s_utils.go (part 2 of 3) by @art-tapin in https://github.com/Kuadrant/kuadrant-operator/pull/191
* Update gateway-api module to v0.6.2 by @mikenairn in https://github.com/Kuadrant/kuadrant-operator/pull/153
* [test] Unit-tests for common/k8s_utils.go (part 3 of 3) & Unit-tests and improvements for common/yaml_decoder.go by @art-tapin in https://github.com/Kuadrant/kuadrant-operator/pull/195
* Istio external authorizer available not only for IstioOperator by @didierofrivia in https://github.com/Kuadrant/kuadrant-operator/pull/192
* kind: bump to 0.20.0 and pin image to kindest/node:v1.27.3 by @KevFan in https://github.com/Kuadrant/kuadrant-operator/pull/209
* Upgrade Authorino and Authorino Operator by @guicassolato in https://github.com/Kuadrant/kuadrant-operator/pull/211
* workflow: use go1.19 to align with go.mod go version used by @KevFan in https://github.com/Kuadrant/kuadrant-operator/pull/213
* docs: minor improvements by @KevFan in https://github.com/Kuadrant/kuadrant-operator/pull/217
* ignore: vendor directory by @KevFan in https://github.com/Kuadrant/kuadrant-operator/pull/219
* [gh workflow] Add CodeCov integration by @art-tapin in https://github.com/Kuadrant/kuadrant-operator/pull/212
* codecov, fix: Fix the ignoring path regex pattern in codecov.yaml (#175) by @art-tapin in https://github.com/Kuadrant/kuadrant-operator/pull/221
* workflow: pin yq version to v4.34.2 by @KevFan in https://github.com/Kuadrant/kuadrant-operator/pull/229
* codecov: do not fail ci on error by @KevFan in https://github.com/Kuadrant/kuadrant-operator/pull/228
* Update workflow actions by @adam-cattermole in https://github.com/Kuadrant/kuadrant-operator/pull/224
* RLP v1beta2 by @alexsnaps in https://github.com/Kuadrant/kuadrant-operator/pull/230
* Replace superseded protobuf package by @grzpiotrowski in https://github.com/Kuadrant/kuadrant-operator/pull/231
* Update RLP docs and examples for v1beta2 by @guicassolato in https://github.com/Kuadrant/kuadrant-operator/pull/233
* Remove rlp watcher for gateway rlps by @eguzki in https://github.com/Kuadrant/kuadrant-operator/pull/242
* feat: upgrade to Go 1.20 by @KevFan in https://github.com/Kuadrant/kuadrant-operator/pull/239
* Add new issues workflow by @adam-cattermole in https://github.com/Kuadrant/kuadrant-operator/pull/235
* [workflow] Adding channels input for bundle creation by @didierofrivia in https://github.com/Kuadrant/kuadrant-operator/pull/250
* doc: #kuadrant channel on kubernetes.slack.com by @KevFan in https://github.com/Kuadrant/kuadrant-operator/pull/252
* Limitador cluster EnvoyFilter controller by @eguzki in https://github.com/Kuadrant/kuadrant-operator/pull/243
* Install Cert Manager by @Boomatang in https://github.com/Kuadrant/kuadrant-operator/pull/258
* kuadrant gateway controller to annotate gateways by @eguzki in https://github.com/Kuadrant/kuadrant-operator/pull/260
* workflow: fix flaky install of operator sdk on mac os by @KevFan in https://github.com/Kuadrant/kuadrant-operator/pull/268
* fix: install operator-sdk on macos local dev env by @guicassolato in https://github.com/Kuadrant/kuadrant-operator/pull/273
* Update controller-gen to v0.13.0 by @guicassolato in https://github.com/Kuadrant/kuadrant-operator/pull/277
* We need to update the GWs finalizers by @alexsnaps in https://github.com/Kuadrant/kuadrant-operator/pull/280
* [authpolicy-v2] AuthPolicy v1beta2 by @guicassolato in https://github.com/Kuadrant/kuadrant-operator/pull/249

### New Contributors
* @KevFan made their first contribution in https://github.com/Kuadrant/kuadrant-operator/pull/209
* @adam-cattermole made their first contribution in https://github.com/Kuadrant/kuadrant-operator/pull/224
* @grzpiotrowski made their first contribution in https://github.com/Kuadrant/kuadrant-operator/pull/231
* @Boomatang made their first contribution in https://github.com/Kuadrant/kuadrant-operator/pull/258

## [0.3.0] - 2023-05-09

### What's Changed
* [refactor] GW utils for all types of policies by @guicassolato in https://github.com/Kuadrant/kuadrant-operator/pull/134
* wasm shim image env var name does not match deployment var name by @eguzki in https://github.com/Kuadrant/kuadrant-operator/pull/136
* fix: `ComputeGatewayDiffs` when missing target HTTPRoute by @guicassolato in https://github.com/Kuadrant/kuadrant-operator/pull/139
* Istio workload selector fetched from the gateway service spec by @guicassolato in https://github.com/Kuadrant/kuadrant-operator/pull/143
* Improve policy constraint error message by @didierofrivia in https://github.com/Kuadrant/kuadrant-operator/pull/145
* Doc install operator by @eguzki in https://github.com/Kuadrant/kuadrant-operator/pull/148
* Simplify RateLimitPolicy by @eguzki in https://github.com/Kuadrant/kuadrant-operator/pull/144
* RLP conditions and variables order does not matter by @eguzki in https://github.com/Kuadrant/kuadrant-operator/pull/147
* Update limitador api to 0.4.0 by @eguzki in https://github.com/Kuadrant/kuadrant-operator/pull/150
* Bump Kind version from 0.11.1 to 0.17.0 by @art-tapin in https://github.com/Kuadrant/kuadrant-operator/pull/152
* Makefile: fix installing kind tool by @eguzki in https://github.com/Kuadrant/kuadrant-operator/pull/154
* Schedule build images with git sha reference by @didierofrivia in https://github.com/Kuadrant/kuadrant-operator/pull/149
* Fix GH Workflow inputs error by @didierofrivia in https://github.com/Kuadrant/kuadrant-operator/pull/155
* Inheriting all secrets from caller workflow by @didierofrivia in https://github.com/Kuadrant/kuadrant-operator/pull/157
* Fix bundle generation by @didierofrivia in https://github.com/Kuadrant/kuadrant-operator/pull/158
* Refactoring Github workflows by @didierofrivia in https://github.com/Kuadrant/kuadrant-operator/pull/161
* Update kind-cluster config by @art-tapin in https://github.com/Kuadrant/kuadrant-operator/pull/160
* Fix update action variables for dependencies by @didierofrivia in https://github.com/Kuadrant/kuadrant-operator/pull/162
* Fix scheduled build by @didierofrivia in https://github.com/Kuadrant/kuadrant-operator/pull/165
* Storing all dependencies sha by @didierofrivia in https://github.com/Kuadrant/kuadrant-operator/pull/170
* Fix inclusion of related wasm shim image by @didierofrivia in https://github.com/Kuadrant/kuadrant-operator/pull/171
* [test] Improve test coverage and performance in apimachinery_status_conditions by @art-tapin in https://github.com/Kuadrant/kuadrant-operator/pull/172
* [test] Add tests for authorino_conditions.go in common package by @art-tapin in https://github.com/Kuadrant/kuadrant-operator/pull/173
* [test] Add tests for hostname.go in common package by @art-tapin in https://github.com/Kuadrant/kuadrant-operator/pull/174
* Fixing image repo URL by @didierofrivia in https://github.com/Kuadrant/kuadrant-operator/pull/177
* Removing release workflow by @didierofrivia in https://github.com/Kuadrant/kuadrant-operator/pull/178

### New Contributors
* @art-tapin made their first contribution in https://github.com/Kuadrant/kuadrant-operator/pull/152

## [0.2.0] - 2022-12-16

### What's Changed
* update resource requirements by @eguzki in https://github.com/Kuadrant/kuadrant-operator/pull/35
* Automate CSV generation by @didierofrivia in https://github.com/Kuadrant/kuadrant-operator/pull/37
* pin dev k8s cluster to 1.22.7 by @eguzki in https://github.com/Kuadrant/kuadrant-operator/pull/36
* update kuadrant core controller manifests by @eguzki in https://github.com/Kuadrant/kuadrant-operator/pull/42
* update kuadrant core controller manifests by @eguzki in https://github.com/Kuadrant/kuadrant-operator/pull/44
* reduce dev env resource requirements by @eguzki in https://github.com/Kuadrant/kuadrant-operator/pull/45
* wasm shim image from env var by @eguzki in https://github.com/Kuadrant/kuadrant-operator/pull/41
* update kuadrant core controller manifests by @eguzki in https://github.com/Kuadrant/kuadrant-operator/pull/46
* remove unused permissions by @eguzki in https://github.com/Kuadrant/kuadrant-operator/pull/43
* Kubebuilder-tools workaround for darwin/arm64 arch by @didierofrivia in https://github.com/Kuadrant/kuadrant-operator/pull/49
* Kuadrant API by @didierofrivia in https://github.com/Kuadrant/kuadrant-operator/pull/50
* Kuadrant controllers by @didierofrivia in https://github.com/Kuadrant/kuadrant-operator/pull/53
* Kuadrant reconciling by @didierofrivia in https://github.com/Kuadrant/kuadrant-operator/pull/54
* Fixing tests by @didierofrivia in https://github.com/Kuadrant/kuadrant-operator/pull/55
* Kuadrant merge docs by @didierofrivia in https://github.com/Kuadrant/kuadrant-operator/pull/63
* Fixing linting tasks by @didierofrivia in https://github.com/Kuadrant/kuadrant-operator/pull/67
* Fixing user guides by @didierofrivia in https://github.com/Kuadrant/kuadrant-operator/pull/70
* Kuadrant Merge by @didierofrivia in https://github.com/Kuadrant/kuadrant-operator/pull/48
* Change codeowners to team engineering by @didierofrivia in https://github.com/Kuadrant/kuadrant-operator/pull/101
* remove duplicated crds by @eguzki in https://github.com/Kuadrant/kuadrant-operator/pull/100
* kap remove hosts from authscheme by @eguzki in https://github.com/Kuadrant/kuadrant-operator/pull/99
* GH ACtions: multi arch images by @eguzki in https://github.com/Kuadrant/kuadrant-operator/pull/108
* Fix dependencies namespace propagation by @didierofrivia in https://github.com/Kuadrant/kuadrant-operator/pull/109
* local operator catalog raw file based format by @eguzki in https://github.com/Kuadrant/kuadrant-operator/pull/107
* Ossm merge by @alexsnaps in https://github.com/Kuadrant/kuadrant-operator/pull/112


**Full Changelog**: https://github.com/Kuadrant/kuadrant-operator/compare/v0.1.0...v0.2.0

## [0.1.0] - 2022-08-24

### What's Changed
* Create LICENSE by @mikenairn in https://github.com/Kuadrant/kuadrant-operator/pull/1
* Add GitHub Actions by @mikenairn in https://github.com/Kuadrant/kuadrant-operator/pull/2
* Generate bundle by @mikenairn in https://github.com/Kuadrant/kuadrant-operator/pull/3
* Add verify CI tests and SDK download by @mikenairn in https://github.com/Kuadrant/kuadrant-operator/pull/4
* Add kuadrant dependencies to OLM installation by @mikenairn in https://github.com/Kuadrant/kuadrant-operator/pull/6
* Add image build GH action by @mikenairn in https://github.com/Kuadrant/kuadrant-operator/pull/8
* Add istio dependency by @mikenairn in https://github.com/Kuadrant/kuadrant-operator/pull/7
* Add Test Scripts CI job by @mikenairn in https://github.com/Kuadrant/kuadrant-operator/pull/5
* Update controller manifests by @mikenairn in https://github.com/Kuadrant/kuadrant-operator/pull/9
* Update authorino kustomization by @mikenairn in https://github.com/Kuadrant/kuadrant-operator/pull/10
* Update controller manifests by @mikenairn in https://github.com/Kuadrant/kuadrant-operator/pull/11
* Update controller manifests by @mikenairn in https://github.com/Kuadrant/kuadrant-operator/pull/12
* Update controller manifests by @mikenairn in https://github.com/Kuadrant/kuadrant-operator/pull/13
* Update controller manifests by @mikenairn in https://github.com/Kuadrant/kuadrant-operator/pull/14
* Update controller manifests by @mikenairn in https://github.com/Kuadrant/kuadrant-operator/pull/15
* Update kuadrant controller by @mikenairn in https://github.com/Kuadrant/kuadrant-operator/pull/16
* Istio ext authz service config by @eguzki in https://github.com/Kuadrant/kuadrant-operator/pull/17
* gitignore .vscode by @guicassolato in https://github.com/Kuadrant/kuadrant-operator/pull/20
* Deploy limitador authorino and kuadrant-controller by @eguzki in https://github.com/Kuadrant/kuadrant-operator/pull/18
* Update kuadrant controller by @eguzki in https://github.com/Kuadrant/kuadrant-operator/pull/25
* Updating tooling by @didierofrivia in https://github.com/Kuadrant/kuadrant-operator/pull/29
* Providing required permissions by @didierofrivia in https://github.com/Kuadrant/kuadrant-operator/pull/30
* Updating kuadrant manifests by @didierofrivia in https://github.com/Kuadrant/kuadrant-operator/pull/31
* Propagating Limitador's env vars by @didierofrivia in https://github.com/Kuadrant/kuadrant-operator/pull/22
* remove kube-rbac-proxy sidecar by @eguzki in https://github.com/Kuadrant/kuadrant-operator/pull/33
* Updating readme by @didierofrivia in https://github.com/Kuadrant/kuadrant-operator/pull/34

### New Contributors
* @mikenairn made their first contribution in https://github.com/Kuadrant/kuadrant-operator/pull/1
* @eguzki made their first contribution in https://github.com/Kuadrant/kuadrant-operator/pull/17
* @guicassolato made their first contribution in https://github.com/Kuadrant/kuadrant-operator/pull/20
* @didierofrivia made their first contribution in https://github.com/Kuadrant/kuadrant-operator/pull/29

**Full Changelog**: https://github.com/Kuadrant/kuadrant-operator/commits/v0.1.0

[Unreleased]: https://github.com/Kuadrant/kuadrant-operator/compare/v0.3.0...HEAD
[0.3.0]: https://github.com/Kuadrant/kuadrant-operator/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/Kuadrant/kuadrant-operator/compare/v0.1.0...v0.2.0
