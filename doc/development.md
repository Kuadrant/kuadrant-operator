# Development Guide

## Technology stack required for development

* [operator-sdk] version v1.16.1
* [kind] version v0.10.0
* [git][git_tool]
* [go] version 1.16+
* [kubernetes] version v1.19+
* [kubectl] version v1.19+

## Local setup

```
$ make local-setup
```

List of tasks done by the command above:

* Create local cluster using kind
* Build kuadrant docker image
* Deploy **ingress provider** (currently [Istio](https://istio.io)) 
* Deploy Kuadrant control plane
* Deploy EchoAPI

### Cleaning up

```
$ make local-cleanup
```

[git_tool]:https://git-scm.com/downloads
[operator-sdk]:https://github.com/operator-framework/operator-sdk
[go]:https://golang.org/
[kind]:https://kind.sigs.k8s.io/
[kubernetes]:https://kubernetes.io/
[kubectl]:https://kubernetes.io/docs/tasks/tools/#kubectl
