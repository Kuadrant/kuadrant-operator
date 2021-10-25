# AuthN based on OpenID Connect

For this user guide, we will be creating the configuration required to protect the Toy Store service
with a more robust identity and access managament (IAM) system.
Users will be authenticated by via [OpenID Connect (OIDC)](https://openid.net/connect/).
The API consumer needs to obtain the OIDC tokens (JWTs).
Kuadrant will validate those tokens and allow API access only for valid tokens.

## Table of contents

* [Table of contents](#table-of-contents)
* [Preparation](#preparation)
* [Get OIDC token](#get-oidc-token)
* [Create kuadrant API Product object](#create-kuadrant-api-product-object)
* [Test the Toy Store API](#test-the-toy-store-api)
* [Next steps](#next-steps)

## Preparation

Follow [Getting Started](/doc/getting-started.md) to have the Toy Store service
being protected by kuadrant.

## Get OIDC token

It is out of scope of this guide to document OpenID Connect provider installation and configuration.
Any OpenID Connect compliant provider should be a valid candidate to be used in this guide.

The procedure to obtain the OIDC token may vary depending on the provider configuration.
For this guide, we will be assuming that the OIDC client is configured to:
* Access Type to `public`.
* `Direct Access Grants Enabled` (Client Credentials Flow) to ON.

With this configuration, token can be obtained directly with the user credentials (user and password).

Get an access token issued by the OIDC server to a user of the 'basic' realm:

```bash
❯ export ACCESS_TOKEN=$(curl -k -H "Content-Type: application/x-www-form-urlencoded" \
        -d 'grant_type=password' \
        -d 'client_id=my-client' \
        -d 'username=bob' \
        -d 'password=p' "https://myoidcprovider.example.com/auth/realms/basic/protocol/openid-connect/token" | jq -r '.access_token')
```

## Create kuadrant API Product object

The kuadrant API Product custom resource represents the kuadrant protection configuration for your service.
For this user guide, the minimal API Product custom resource will be extended to contain `securityScheme`
section with the configuration needed to protect the upstream APIs with OIDC authN.

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
    - name: MyOIDCAuth
      openIDConnectAuth:
        url: https://myoidcprovider.example.com/auth/realms/basic
EOF
```

Note that kuadrant does not need OIDC client credentials.
Kuadrant will discover the OpenID Connect configuration, so the ID tokens (JWTs) issued
by the OIDC server (directly to the users) can be verified and validated by Kuadrant.

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

Without any access token, the request should fail with `401 Unauthorized`:

```bash
❯ curl --write-out '%{http_code}' --silent --output /dev/null -H "Host: toystore.127.0.0.1.nip.io" localhost:9080/something
401
```

On the other hand, adding the access token to the request, the request should reach the Toy Store service.

```jsonc
❯ curl -H "Host: toystore.127.0.0.1.nip.io" -H "Authorization: Bearer $ACCESS_TOKEN" localhost:9080/something
{
  "method": "GET",
  "path": "/something",
  "query_string": null,
  "body": "",
  "headers": {
    "HTTP_HOST": "toystore.127.0.0.1.nip.io",
    "HTTP_USER_AGENT": "curl/7.68.0",
    "HTTP_ACCEPT": "*/*",
    "HTTP_AUTHORIZATION": "Bearer eyJhbGciOiJSUzI1NiIsInR5cCIgOiAiSldUIiwia2lkIiA6ICJuLUw4RjZhQVEydUNud0NLZWtZY3RWMDlmdEtvLVhGY0ZMSmNCS3I0b3hVIn0.eyJleHAiOjE2MzQ1Njc1MDQsImlhdCI6MTYzNDU2NzIwNCwianRpIjoiYWRjNzgzZDItMWIzMi00YjczLThlYjgtOGFkMzUzMTg1NTdlIiwiaXNzIjoiaHR0cHM6Ly9rZXljbG9hay1lZ3V6a2kuYXBwcy5kZXYtZW5nLW9jcDQtNi1vcGVyYXRvci5kZXYuM3NjYS5uZXQvYXV0aC9yZWFsbXMvYmFzaWMiLCJhdWQiOiJhY2NvdW50Iiwic3ViIjoiZTQ0MzY5NGMtMTQyYy00ZTUyLTllYWItMTM4ODIzNGFjNGMzIiwidHlwIjoiQmVhcmVyIiwiYXpwIjoicGF0eGktY2xpZW50Iiwic2Vzc2lvbl9zdGF0ZSI6ImViODhiYmYyLWQyMWUtNDFhMS1iMTRmLWNmZDYzYjk2OGFkYSIsImFjciI6IjEiLCJyZWFsbV9hY2Nlc3MiOnsicm9sZXMiOlsib2ZmbGluZV9hY2Nlc3MiLCJ1bWFfYXV0aG9yaXphdGlvbiJdfSwicmVzb3VyY2VfYWNjZXNzIjp7ImFjY291bnQiOnsicm9sZXMiOlsibWFuYWdlLWFjY291bnQiLCJtYW5hZ2UtYWNjb3VudC1saW5rcyIsInZpZXctcHJvZmlsZSJdfX0sInNjb3BlIjoiZW1haWwgcHJvZmlsZSIsImVtYWlsX3ZlcmlmaWVkIjp0cnVlLCJwcmVmZXJyZWRfdXNlcm5hbWUiOiJlZ3V6a2kiLCJlbWFpbCI6ImVhc3RpemxlK2tleWNsb2FrQHJlZGhhdC5jb20ifQ.SLVZ5hdb-YRBQlh3HAlAQdoxzfmua1PxvR_9TSj2fw1YQjrE61kCRDlR0JL5WXTR73nmRf6mQKqXxmRCzQyXoGB0HuKFWYZj4ItBL6stmrSRiByQlooXcImE8HDes7UgsgFR_NKf955WualvbjQ88Tl-z8PCGymaD_wzd1cNA11i-UWnE9kODrdLFSUqhjT7Fxboj_SN_UM5dmCQiu3AXMNFfO-UdKiWCnZrQout3xAL73dYMvkej0_ZQfKMMxha0ewtWs17rP1ynvTEoO5hiNicqpQMOWvWjInrOzq8z034ZgqWvYpjyYGeg7-NTCxXenaeYa8ZJjGn0_vxXLH0hw",
    "HTTP_X_FORWARDED_FOR": "10.244.0.1",
    "HTTP_X_B3_SAMPLED": "0",
    "HTTP_VERSION": "HTTP/1.1"
  },
  "uuid": "ddfc2858-65e0-47f2-bc2e-048f8a392655"
}
```

## Next steps

Check out other [user guides](/README.md#user-guides) for other kuadrant capabilities like AuthN or rate limit.
