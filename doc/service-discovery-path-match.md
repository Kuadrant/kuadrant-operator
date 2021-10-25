# HTTP routing rules with path matching

This guide shows how to define the routing rules based on path matching expressions.

## Table of contents

* [Preparation](#preparation)
* [Activate the service discovery](#activate-the-service-discovery)
* [Create kuadrant API Product object](#create-kuadrant-api-product-object)
* [Test the Toy Store API](#test-the-toy-store-api)
* [Next steps](#next-steps)

## Preparation

Follow [Getting Started](/doc/getting-started.md) to have the Toy Store service
being protected by kuadrant.

## Activate the service discovery

We need to add an annotation to the Toy Store service.
The annotation will have the path matching expression.

```bash
❯ kubectl -n default annotate service toystore discovery.kuadrant.io/matchpath="/v1"
service/toystore annotated
```

Verify that the Toy Store kuadrant API object has been created with the path matching config.

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
      value: /v1
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

## Test the Toy Store API

Run kubectl port-forward in a different shell:

```bash
❯ kubectl port-forward -n kuadrant-system service/kuadrant-gateway 9080:80
Forwarding from [::1]:9080 -> 8080
```

The service can now be accessed at `http://localhost:9080` via a browser or any other client, like curl.

As the path match expression allows, requesting `GET /v1/something` should work:

```jsonc
❯ curl localhost:9080/v1/something
{
  "method": "GET",
  "path": "/v1/something",
  "query_string": null,
  "body": "",
  "headers": {
    "HTTP_HOST": "localhost:9080",
    "HTTP_USER_AGENT": "curl/7.68.0",
    "HTTP_ACCEPT": "*/*",
    "HTTP_X_FORWARDED_FOR": "10.244.0.1",
    "HTTP_X_FORWARDED_PROTO": "http",
    "HTTP_X_B3_SAMPLED": "0",
    "HTTP_VERSION": "HTTP/1.1"
  },
  "uuid": "5352c275-40b0-4999-bc73-865e4c46c152"
}

```

On the other hand, any other request missing `/v1` path prefix should fail.

```bash
❯ curl --write-out '%{http_code}' --silent --output /dev/null localhost:9080/something
404
```

## Next steps

Check out other [user guides](/README.md#user-guides) for other kuadrant capabilities like AuthN or rate limit.
