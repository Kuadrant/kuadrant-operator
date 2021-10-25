# HTTP routing rules from OpenAPI served by the service

This guide shows how to define the routing rules
from the [OpenAPI Specification (OAS) 3.x](https://github.com/OAI/OpenAPI-Specification/blob/main/versions/3.0.2.md)
served by the upstream service.

## Table of contents

* [Preparation](#preparation)
* [Deploy the upstream Pet Store API service](#deploy-the-upstream-pet-store-api-service)
* [Activate the service discovery](#activate-the-service-discovery)
* [Create kuadrant API Product object](#create-kuadrant-api-product-object)
* [Test the Pet Store API](#test-the-pet-store-api)
* [Next steps](#next-steps)

## Preparation

Follow [Getting Started](/doc/getting-started.md) to kuadrant up and running.

## Deploy the upstream Pet Store API service

The Pet Store API service serves the openapi document at the port **9090** in the `/openapi` path.
The actual API services are served at the port **8080**.

```bash
❯ kubectl apply -n default -f https://raw.githubusercontent.com/kuadrant/kuadrant-controller/main/examples/openapi-served-service/petstore.yaml
deployment.apps/petstore created
service/petstore created
```

Verify that the Pet Store pod is up and running.

```bash
❯ kubectl get pods -n default --field-selector=status.phase==Running
NAME                        READY   STATUS    RESTARTS   AGE
petstore-XXXXXXXXXX-XXXXX   1/1     Running   0          111s
```

Verify that the Pet Store service has been created.

```bash
❯ kubectl get service petstore -n default
NAME       TYPE        CLUSTER-IP      EXTERNAL-IP   PORT(S)             AGE
petstore   ClusterIP   10.XX.XXX.XXX   <none>        8080/TCP,9090/TCP   2m41s
```

Run kubectl port-forward in a different shell:

```bash
❯ kubectl port-forward -n default service/petstore 9090:9090
Forwarding from 127.0.0.1:9090 -> 9090
Forwarding from [::1]:9090 -> 9090
```

Verify that the openapi is being served

```yaml
❯ curl localhost:9090/openapi
---
openapi: "3.0.0"
info:
  title: "Petstore Service"
  version: "1.0.0"
servers:
  - url: https://petstore.swagger.io/v1
paths:
  /pets:
    get:
      operationId: "getPets"
      responses:
        405:
          description: "invalid input"
```

## Activate the service discovery

In order to activate the service discovery, the upstream Pet Store API service needs to be labeled.

```bash
❯ kubectl -n default label service petstore discovery.kuadrant.io/enabled=true
service/petstore labeled
```

We need to add some annotations to the Pet Store service.
The annotations will tell how to access the served OpenAPI document
and which port to access the actual API.

```bash
❯ kubectl -n default annotate service petstore discovery.kuadrant.io/oas-path="/openapi" \
                        discovery.kuadrant.io/oas-name-port=openapi \
                        discovery.kuadrant.io/port=8080
```

Verify that the Pet Store kuadrant API object has been created with the OpenAPI document.

```yaml
❯ kubectl -n default get api petstore -o yaml
apiVersion: networking.kuadrant.io/v1beta1
kind: API
metadata:
  name: petstore
  namespace: default
spec:
  destination:
    schema: http
    serviceReference:
      name: petstore
      namespace: default
      port: 8080
  mappings:
    OAS: |
      ---
      openapi: "3.0.0"
      info:
        title: "Petstore Service"
        version: "1.0.0"
      servers:
        - url: https://petstore.swagger.io/v1
      paths:
        /pets:
          get:
            operationId: "getPets"
            responses:
              405:
                description: "invalid input"
```

## Create kuadrant API Product object

The kuadrant API Product custom resource represents the kuadrant protection configuration for your service.
For this user guide, we will be creating the minimum configuration required to integrate kuadrant with your service.

```yaml
❯ kubectl -n default apply -f - <<EOF
---
apiVersion: networking.kuadrant.io/v1beta1
kind: APIProduct
metadata:
  name: petstore
  namespace: default
spec:
  hosts:
    - '*'
  APIs:
    - name: petstore
      namespace: default
EOF
```

Verify the APIProduct ready condition status is `true`

```jsonc
❯ kubectl get apiproduct petstore -n default -o jsonpath="{.status}" | jq '.'
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

## Test the Pet Store API

Run kubectl port-forward in a different shell:

```bash
❯ kubectl port-forward -n kuadrant-system service/kuadrant-gateway 9080:80
Forwarding from [::1]:9080 -> 8080
```

The service can now be accessed at `http://localhost:9080` via a browser or any other client, like curl.

As the OpenAPI doc specifies, requesting `GET /pets` should work:

```bash
❯ curl localhost:9080/pets
[{"name":"micky"}, {"name":"minnie"}]
```

On the other hand, any other request should be rejected.

```bash
❯ curl --write-out '%{http_code}' --silent --output /dev/null localhost:9080/toy
404
```

## Next steps

Check out other [user guides](/README.md#user-guides) for other kuadrant capabilities like AuthN or rate limit.
