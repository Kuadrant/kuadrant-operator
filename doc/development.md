# Development Guide

## Technology stack required for development

* [operator-sdk] version v1.16.1
* [kind] version v0.11.1
* [git][git_tool]
* [go] version 1.16+
* [kubernetes] version v1.19+
* [kubectl] version v1.19+

## Build

```
$ make
```

## Run locally

You need an active session open to a kubernetes cluster.

Optionally, run kind and deploy kuadrant deps

```
$ make local-env-setup
```

Then, run the controller locally

```
$ make run
```

## Deploy the controller in a deployment object

```
$ make local-setup
```

List of tasks done by the command above:

* Create local cluster using kind
* Build kuadrant docker image from the current working directory
* Deploy Kuadrant control plane (including istio, authorino and limitador)

## Cleaning up

```
$ make local-cleanup
```

## Run tests

### Unittests

```
$ make test-unit
```

### Integration tests

You need an active session open to a kubernetes cluster.

Optionally, run kind and deploy kuadrant deps

```
$ make local-env-setup
```

Run integration tests

```
$ make test-integration
```

### All tests

You need an active session open to a kubernetes cluster.

Optionally, run kind and deploy kuadrant deps

```
$ make local-env-setup
```

Run all tests

```
$ make test
```

### Lint tests

```
$ make run-lint
```

## (Un)Install Kuadrant CRDs

You need an active session open to a kubernetes cluster.

```
$ make install
```

Remove CRDs

```
$ make uninstall
```

[git_tool]:https://git-scm.com/downloads
[operator-sdk]:https://github.com/operator-framework/operator-sdk
[go]:https://golang.org/
[kind]:https://kind.sigs.k8s.io/
[kubernetes]:https://kubernetes.io/
[kubectl]:https://kubernetes.io/docs/tasks/tools/#kubectl
