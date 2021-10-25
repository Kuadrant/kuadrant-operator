# Kuadrant Service Discovery

## Table of contents

* [Overview](#overview)
* [HTTP Routing Rules](#http-routing-rules)
   * [HTTP routing rules from the OpenAPI Spec](#http-routing-rules-from-the-openapi-spec)
   * [HTTP routing rules from request path matchers](#http-routing-rules-from-request-path-matchers)
* [Service Discovery Annotations](#service-discovery-annotations)
* [Service Discovery Labels](#service-discovery-labels)
* [An alternative way to the kuadrant service discovery](#an-alternative-way-to-the-kuadrant-service-discovery)

## Overview

The kuadrant API custom resource represents a specific kubernetes service.
One kuadrant API object needs to be created for each service that is wanted to be protected by kuadrant.
While you can always create the API object manually, Kuadrant Service Discovery watches
for new services and creates kuadrant API objects automatically.

When adding a new application that contains an API, to reduce the number of objects that a user has to author,
Kuadrant can leverage kubernetes [annotations](https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations/)
and [labels](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/) for a seamless integration.
A good place to annotate is the kubernetes service used to drive traffic to the user's API.

To make your application service "visible" for the Kuadrant Service Discovery system,
just one label is required to be included in the service:

```
discovery.kuadrant.io/enabled=true
```

An example of a service called `toystore` being labeled with the kuadrant discovery system label:

```bash
❯ kubectl label service toystore discovery.kuadrant.io/enabled=true
service/toystore labeled
```

## HTTP Routing Rules

By default, kuadrant will configure the `catch-all` routing rule. That means that all HTTP requests
will be routed to the upstream service.

However, the user may want to add some the routing rules. Kuadrant SErvice Discovery system will
be watching for some specific kubernetes service
[annotations](https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations/) to read routing rules.
Basically there are two mechanisms to declare the routing rules:

* [OpenAPI Specification (OAS) 3.x](https://github.com/OAI/OpenAPI-Specification/blob/main/versions/3.0.2.md)
* Path match based routing

### HTTP routing rules from the OpenAPI Spec

A valid [OpenAPI Specification (OAS) 3.x](https://github.com/OAI/OpenAPI-Specification/blob/main/versions/3.0.2.md)
is required.

The Kuadrant Service Discovery will read the OAS document, parse it and configure one HTTP routing
rule per each [OAS operation](https://github.com/OAI/OpenAPI-Specification/blob/main/versions/3.0.2.md#operationObject)
found in the document.

*Note*: If the OpenAPI has a non empty `servers` array, the *first* object will be used to rewrite the authority (`Host` header).

There are two ways to expose the OpenAPI document to be read by the Kuadrant Service Discovery.

* OpenAPI document stored in a [kubernetes ConfigMap](https://kubernetes.io/docs/concepts/configuration/configmap/)

The name of the configmap will be referenced in the service annotation `discovery.kuadrant.io/oas-configmap`

For example:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: toystore
  annotations:
    discovery.kuadrant.io/oas-configmap: "toystore-oas"
  labels:
    discovery.kuadrant.io/enabled: "true"
```

Follow this step-by-step [guide](service-discovery-oas-configmap.md) to see this method in action.

* OpenAPI document served from the service

There are use cases where the OpenAPI document is exposed in the upstream service.
For this case, kuadrant will fetch the OpenAPI document directly from the service via HTTP request.

The service annotation `discovery.kuadrant.io/oas-path` can be used to specify the HTTP path
where the OpenAPI doc is exposed.

For example, let's say the OpenAPI doc can be fetched from `/openapi` path:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: toystore
  annotations:
    discovery.kuadrant.io/oas-path: "/openapi"
  labels:
    discovery.kuadrant.io/enabled: "true"
```

Follow this step-by-step [guide](service-discovery-oas-service.md) to see this method in action.

### HTTP routing rules from request path matchers

The Kuadrant Service Discovery will use the following annotations to configure HTTP routing rules.

* Match path annotation

Defines a single specific path, prefix or regex (default "/")

```
discovery.kuadrant.io/matchpath
```

* Match path type annotation

Specifies how to match against the matchpath value. Accepted values are `Exact`, `Prefix`
and `RegularExpression`. Defaults to `Prefix`.

```
discovery.kuadrant.io/matchpath-type
```

For example, route all requests with paths prefixed by `/v1`:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: toystore
  annotations:
    discovery.kuadrant.io/matchpath: "/v1"
    discovery.kuadrant.io/matchpath-type: Prefix
  labels:
    discovery.kuadrant.io/enabled: "true"
```

Follow this step-by-step [guide](service-discovery-matching-rules.md) to see this method in action.

## Service Discovery Annotations

- **discovery.kuadrant.io/scheme**: *OPTIONAL* Either HTTP or HTTPS specifies how the kuadrant gateway will connect to this API.
- **discovery.kuadrant.io/api-name**: *OPTIONAL* If not set, the name of the API can be matched with the service name.
- **discovery.kuadrant.io/tag**: *OPTIONAL* A special tag used to distinguish this deployment between several instances of the API.
- **discovery.kuadrant.io/port**: *OPTIONAL* Only required if there are multiple ports in the service. Either the Name of the port or the Number.
- **discovery.kuadrant.io/oas-configmap**: *OPTIONAL* Configmap that contains the OAS spec.
- **discovery.kuadrant.io/matchpath**: *OPTIONAL* Define a single specific path, prefix or regex. Defaults to `/`.
- **discovery.kuadrant.io/matchpath-type**: *OPTIONAL* Specifies how to match against the `matchpath` value. Accepted values are `Exact`, `Prefix` and `RegularExpression`. Defaults to `Prefix`.
- **discovery.kuadrant.io/oas-path**: *OPTIONAL* Define a specific path for retrieving the config from the service itself.
- **discovery.kuadrant.io/oas-name-port**: *OPTIONAL* The port to be used to retrieve the OAS config, if not defined, it will used the first one

## Service Discovery Labels
- **discovery.kuadrant.io/enabled**: *REQUIRED* true or false, marks the object to be discovered by kuadrant.

## An alternative way to the kuadrant service discovery

If for any reason, the kuadrant service discovery mechanism cannot be used,
the alternative way provided by Kuadrant is the use of the
[kuadrantctl](https://github.com/Kuadrant/kuadrantctl/blob/main/doc/api-apply.md) CLI.
In some use cases, the kubernetes service is not owned or cannot be labeled and annotated.
Hence, the Kuadrant service discovery cannot be used.
The `kuadrantctl api apply --service-name <SERVICE>` command will read the user's service to create and maintain the associated
kuaddrant API.
