# AuthN based on API key

For this user guide, we will be creating the configuration required to protect the Toy Store service
with a simple API key authentication method.

Each API key is stored in a [Kubernetes Secret](https://kubernetes.io/docs/concepts/configuration/secret/)
resource that contains an `api_key` entry and kuadrant labels for future references.

## Table of contents

* [Preparation](#preparation)
* [Create one secret with the API key](#create-one-secret-with-the-api-key)
* [Create kuadrant API Product object](#create-kuadrant-api-product-object)
* [Test the Toy Store API](#test-the-toy-store-api)
* [Next steps](#next-steps)

## Preparation

Follow [Getting Started](/doc/getting-started.md) to have the Toy Store service
being protected by kuadrant.

## Create one secret with the API key

```bash
❯ kubectl -n default create secret generic toystore-api-key --from-literal=api_key=JUSTFORDEMOSOBVIOUSLY
secret/toystore-api-key created
```

We will add some kuadrant secret labels for later reference.

```bash
❯ kubectl -n default label secret toystore-api-key secret.kuadrant.io/managed-by="authorino" api=toystore
secret/toystore-api-key labeled
```

## Create kuadrant API Product object

The kuadrant API Product custom resource represents the kuadrant protection configuration for your service.
For this user guide, the minimal API Product custom resource will be extended to contain `securityScheme`
section with the configuration needed to protect the upstream APIs with API keys.
The configuration will include a `credential_source` section with label selector to select
the secrets having the desired API keys.


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
    - toystore.127.0.0.1.nip.io
  APIs:
    - name: toystore
      namespace: default
  securityScheme:
    - name: MyAPIKey
      apiKeyAuth:
        location: authorization_header
        name: APIKEY
        credential_source:
          labelSelectors:
            secret.kuadrant.io/managed-by: authorino
            api: toystore
EOF
```

Note that, according to the configuration, the API key is expected be in the authorization header,
with a key selector `APIKEY` followed by the actual API key.

For a full list of available options, check out the [APIProduct reference](/apis/networking/v1beta1/apiproduct_types.go).

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

Without any API key, the request should fail with `401 Unauthorized`:

```bash
❯ curl --write-out '%{http_code}' --silent --output /dev/null -H "Host: toystore.127.0.0.1.nip.io" localhost:9080/toy
401
```

On the other hand, adding the API key to the request, the request should reach the Toy Store service.

```jsonc
❯ curl -H "Host: toystore.127.0.0.1.nip.io" -H "Authorization: APIKEY JUSTFORDEMOSOBVIOUSLY" localhost:9080/something
{
  "method": "GET",
  "path": "/something",
  "query_string": null,
  "body": "",
  "headers": {
    "HTTP_HOST": "toystore.127.0.0.1.nip.io",
    "HTTP_USER_AGENT": "curl/7.68.0",
    "HTTP_ACCEPT": "*/*",
    "HTTP_AUTHORIZATION": "APIKEY JUSTFORDEMOSOBVIOUSLY",
    "HTTP_X_FORWARDED_FOR": "10.244.0.1",
    "HTTP_X_B3_SAMPLED": "0",
    "HTTP_VERSION": "HTTP/1.1"
  },
  "uuid": "23687be1-825c-44e4-b390-b9762753799e"
}
```

## Next steps

Check out other [user guides](/README.md#user-guides) for other kuadrant capabilities like AuthN or rate limit.
