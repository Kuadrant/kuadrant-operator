# Change Log

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](http://keepachangelog.com/)
and this project adheres to [Semantic Versioning](http://semver.org/).

## [Unreleased]

## [0.3.0] - 2023-05-09

## What's Changed
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

## New Contributors
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
