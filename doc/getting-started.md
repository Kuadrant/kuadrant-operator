# Getting Started

This guide lets you quickly evaluate Kuadrant.
Follow the steps to integrate kuadrant with your service with the minimal configuration needed.

## Table of contents

* [Requirements](#requirements)
* [Steps](#steps)
   * [Download kuadrantctl tool](#download-kuadrantctl-tool)
   * [Install kuadrant](#install-kuadrant)
   * [Deploy the upstream Toy Store API service](#deploy-the-upstream-toy-store-api-service)
   * [Create kuadrant API object](#create-kuadrant-api-object)
   * [Create kuadrant API Product object](#create-kuadrant-api-product-object)
   * [Test the Toy Store API](#test-the-toy-store-api)
   * [Next steps](#next-steps)

## Requirements

* Having a [Kubernetes](https://kubernetes.io/) (1.19, 1.20, 1.21, 1.22) cluster up and running.
* Permission from the Kubernetes cluster to create Custom Resource Definitions (CRDs) during kuadrant's installation. Cluster administrators can handle this requirement through the Kubernetes API Role-Based Access Control bindings.
* A deployed [kubernetes service](https://kubernetes.io/docs/concepts/services-networking/service/) in the cluster acting as the entry point for your API.
* Golang 1.16 environment. Download and install steps [here](https://golang.org/doc/install)
* Installed [`kubectl`](https://kubernetes.io/docs/tasks/tools/#kubectl) >= 1.19
* Installed [`jq`](https://stedolan.github.io/jq/download/) >= 1.5 utility to parse JSON objects

**NOTE**: You can easily have a local cluster setup using [Kind](https://kind.sigs.k8s.io/). In this case, the requirement is to have [Docker](https://docker.com/).

## Steps

### Download kuadrantctl tool

[`kuadrantctl`](https://github.com/Kuadrant/kuadrantctl) is the kuadrant configuration command line utility.
Currently `kuadrantctl install` command is the recommended installation method of kuadrant.

Download the latest release

```bash
go install github.com/kuadrant/kuadrantctl@latest
```

### Install kuadrant

The install command will create a namespace called `kuadrant-system` and deploy kuadrant services in that namespace.

```bash
kuadrantctl install
```

On successful command return, you should see the following deployments and pods created.

```bash
❯ kubectl get pods -n kuadrant-system
NAME                                                     READY   STATUS    RESTARTS   AGE
authorino-XXXXXXXXXXX-XXXX                               1/1     Running   0          2m33s
istiod-XXXXXXXXXX-XXXXX                                  1/1     Running   0          3m11s
kuadrant-controller-manager-XXXXXXXXXX-XXXX              2/2     Running   0          3m5s
kuadrant-gateway-XXXXXXXXXX-XXXX                         1/1     Running   0          3m5s
limitador-XXXXXXXXXX-XXXXX                               1/1     Running   0          2m13s
limitador-operator-controller-manager-XXXXXXXXXX-XXXXX   2/2     Running   0          3m6s


❯ kubectl get deployments -n kuadrant-system
NAME                                    READY   UP-TO-DATE   AVAILABLE   AGE
authorino                               1/1     1            1           4m51s
istiod                                  1/1     1            1           4m57s
kuadrant-controller-manager             1/1     1            1           4m50s
kuadrant-gateway                        1/1     1            1           4m51s
limitador                               1/1     1            1           3m58s
limitador-operator-controller-manager   1/1     1            1           4m51s


❯ kubectl get pods -n authorino-operator
NAME                                                     READY   STATUS    RESTARTS   AGE
authorino-operator-controller-manager-XXXXXXXXXXX-XXXX   2/2     Running   0          2m36s


❯ kubectl get deployments -n authorino-operator
NAME                                    READY   UP-TO-DATE   AVAILABLE   AGE
authorino-operator-controller-manager   1/1     1            1           2m36s

```

### Deploy the upstream Toy Store API service

Skip this section if you already have your service deployed.

The Toy Store API Service will be backed by a simple Echo API service.

```bash
❯ kubectl apply -n default -f https://raw.githubusercontent.com/kuadrant/kuadrant-controller/main/examples/toystore/toystore.yaml
deployment.apps/toystore created
service/toystore created
```

Verify that the Toy Store pod is up and running.

```bash
❯ kubectl get pods -n default --field-selector=status.phase==Running
NAME                        READY   STATUS    RESTARTS   AGE
toystore-XXXXXXXXXX-XXXXX   1/1     Running   0          2m56s
```

Verify that the Toy Store service has been created.

```bash
❯ kubectl get service toystore -n default
NAME       TYPE        CLUSTER-IP      EXTERNAL-IP   PORT(S)   AGE
toystore   ClusterIP   10.XX.XXX.XXX   <none>        80/TCP    4m28s
```

### Create kuadrant API object

The kuadrant API custom resource represents a specific kubernetes service.
It needs to be created for each service that is wanted to be protected by kuadrant.

There are two methods to make it easy for you to create kuadrant API objects:
* [kuadrantctl CLI](https://github.com/Kuadrant/kuadrantctl/blob/main/doc/api-apply.md) tool with the following command `kuadrantctl api apply --service-name <SERVICE>`
* The [kuadrant service discovery](service-discovery.md) system watches for services labeled with kuadrant

For this user guide, we will be using the [kuadrant service discovery](managing-apis.md#service-discovery).
To activate it, the upstream Toy Store API service needs to be labeled.

```bash
❯ kubectl -n default label service toystore discovery.kuadrant.io/enabled=true
service/toystore labeled
```

Verify that the Toy Store kuadrant API object has been created.

```yaml
❯ kubectl -n default get api toystore -o yaml
apiVersion: networking.kuadrant.io/v1beta1
kind: API
metadata:
  name: toystore
  namespace: default
spec:
  destination:
    schema: http
    serviceReference:
      name: toystore
      namespace: default
      port: 80
  mappings:
    HTTPPathMatch:
      type: Prefix
      value: /
```

*Note*: some kubernetes specific data has been removed from the snippet above just for clarity.

### Create kuadrant API Product object

The kuadrant API Product custom resource represents the kuadrant protection configuration for your service.
For this user guide, we will be creating the minimum configuration required to integrate kuadrant with your service.

```yaml
❯ kubectl -n default apply -f - <<EOF
---
apiVersion: networking.kuadrant.io/v1beta1
kind: APIProduct
metadata:
  name: toystore
  namespace: default
spec:
  hosts:
    - '*'
  APIs:
    - name: toystore
      namespace: default
EOF
```

Verify the APIProduct ready condition status is `true`

```jsonc
❯ kubectl get apiproduct toystore -n default -o jsonpath="{.status}" | jq '.'
{
  "conditions": [
    {
      "message": "Ready",
      "reason": "Ready",
      "status": "True",
      "type": "Ready"
    }
  ],
  "observedgen": 1
}
```

### Test the Toy Store API

Run kubectl port-forward in a different shell:

```bash
❯ kubectl port-forward -n kuadrant-system service/kuadrant-gateway 9080:80
Forwarding from [::1]:9080 -> 8080
```

The service can now be accessed at `http://localhost:9080` via a browser or any other client, like curl.

```jsonc
❯ curl localhost:9080/toys
{
  "method": "GET",
  "path": "/toys",
  "query_string": null,
  "body": "",
  "headers": {
    "HTTP_HOST": "localhost:9080",
    "HTTP_USER_AGENT": "curl/7.68.0",
    "HTTP_ACCEPT": "*/*",
    "HTTP_X_FORWARDED_FOR": "10.244.0.1",
    "HTTP_X_FORWARDED_PROTO": "http",
    "HTTP_X_ENVOY_INTERNAL": "true",
    "HTTP_X_B3_SAMPLED": "0",
    "HTTP_VERSION": "HTTP/1.1"
  },
  "uuid": "366b1500-0110-4770-a883-9eac384d5f3a"
}
```

### Next steps

Check out other [user guides](/README.md#user-guides) for other kuadrant capabilities like AuthN or rate limit.
