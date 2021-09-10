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

### Upgrade [Authorino](https://github.com/Kuadrant/authorino)

Define some environment variables.

```
$ export AUTHORINO_NAMESPACE=kuadrant-system
$ export AUTHORINO_IMAGE=quay.io/3scale/authorino:371d0408998f6223b3d7be170704688901647772
$ export AUTHORINO_DEPLOYMENT=cluster-wide-notls
# Replace KUADRANT_PROJECT
$ export AUTHORINO_MANIFEST_FILE=${KUADRANT_PROJECT}/utils/local-deployment/authorino.yaml
```

Generate manifest file.

```
$ git clone https://github.com/Kuadrant/authorino
$ cd authorino
$ cd deploy/base/ && kustomize edit set image authorino=${AUTHORINO_IMAGE}
$ cd -
$ cd deploy/overlays/${AUTHORINO_DEPLOYMENT} && kustomize edit set namespace ${AUTHORINO_NAMESPACE}
$ cd -
$ kustomize build install > ${AUTHORINO_MANIFEST_FILE}
$ echo "---" >> ${AUTHORINO_MANIFEST_FILE}
$ kustomize build deploy/overlays/${AUTHORINO_DEPLOYMENT} >> ${AUTHORINO_MANIFEST_FILE}
```

[git_tool]:https://git-scm.com/downloads
[operator-sdk]:https://github.com/operator-framework/operator-sdk
[go]:https://golang.org/
[kind]:https://kind.sigs.k8s.io/
[kubernetes]:https://kubernetes.io/
[kubectl]:https://kubernetes.io/docs/tasks/tools/#kubectl
