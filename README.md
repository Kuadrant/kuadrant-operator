# kuadrant-controller

[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](http://www.apache.org/licenses/LICENSE-2.0)

## Overview
Kuadrant is a re-architecture of API Management using Cloud Native concepts and separating the components to be less coupled, more reusable and leverage the underlying platform.

## Service Discovery

When adding a new application that contains an API, to reduce the number of objects that a user has to author,
Kuadrant can leverage annotations. A good place to annotate is the Service used to drive traffic to the user API.

### Annotations:

- **discovery.kuadrant.io/scheme**: *OPTIONAL* Either HTTP or HTTPS specifies how the kuadrant gateway will connect to this API.
- **discovery.kuadrant.io/api-name**: *OPTIONAL* If not set, the name of the API can be matched with the service name.
- **discovery.kuadrant.io/tag**: *OPTIONAL* A special tag used to distinguish this deployment between several instances of the API.
- **discovery.kuadrant.io/port**: *OPTIONAL* Only required if there are multiple ports in the service. Either the Name of the port or the Number.
- **discovery.kuadrant.io/oas-configmap**: *OPTIONAL* Configmap that contains the OAS spec.
- **discovery.kuadrant.io/matchpath**: *OPTIONAL* Define a single specific path, prefix or regex. Defaults to `/`.
- **discovery.kuadrant.io/matchpath-type**: *OPTIONAL* Specifies how to match against the `matchpath` value. Accepted values are `Exact`, `Prefix` and `RegularExpression`. Defaults to `Prefix`.

### Labels:
- **discovery.kuadrant.io/enabled:**: *REQUIRED* true or false, marks the object to be discovered by kuadrant.


Example of a kuadrant annotated service providing OpenAPI spec in a configmap.

```yaml
apiVersion: v1
kind: Service
metadata:
  name: cats-api
  annotations:
    discovery.kuadrant.io/scheme: "http"
    discovery.kuadrant.io/api-name: "cats"
    discovery.kuadrant.io/tag: "production"
    discovery.kuadrant.io/port: "80"
    discovery.kuadrant.io/oas-configmap: "cats-oas"
  labels:
    discovery.kuadrant.io/enabled: "true"
spec:
  selector:
    svc: cats
  ports:
    - port: 80
      protocol: TCP
      targetPort: 3000
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: cats-oas
data:
  openapi.yaml: |
    openapi: "3.0.0"
    info:
      title: "toy API"
    version: "1.0.0"
    servers:
      - url: http://toys/
    paths:
      /toys:
        get:
          operationId: "getToys"
```

Example of a kuadrant annotated service with a `catch-all` match path.

```yaml
apiVersion: v1
kind: Service
metadata:
  name: dogs-api
  annotations:
    discovery.kuadrant.io/scheme: "http"
    discovery.kuadrant.io/api-name: "dogs"
    discovery.kuadrant.io/tag: "production"
    discovery.kuadrant.io/port: "80"
    discovery.kuadrant.io/matchpath: "/"
    discovery.kuadrant.io/matchpath-type: Prefix
  labels:
    discovery.kuadrant.io/enabled: "true"
spec:
  selector:
    svc: dogs
  ports:
    - port: 80
      protocol: TCP
      targetPort: 3000
```

### Service discovery: OAS or MatchPath

Kuadrant will protect the annotated service either by the OpenAPI spec or the MatchPath spec. 

* If `discovery.kuadrant.io/oas-configmap` annotation is found, the *matchpath* mechanism will be disabled and the *matchpath* annotations will be **ignored** by kuadrant controller.
* If `discovery.kuadrant.io/matchpath` is not found, the assigned value will be `/`.
* If `discovery.kuadrant.io/matchpath-type` is not found, the assigned type will be `Prefix`.

## Contributing
The [Development guide](doc/development.md) describes how to build the kuadrant controller and how to test your changes before submitting a patch or opening a PR.

## Licensing

This software is licensed under the [Apache 2.0 license](https://www.apache.org/licenses/LICENSE-2.0).

See the LICENSE and NOTICE files that should have been provided along with this software for details.
