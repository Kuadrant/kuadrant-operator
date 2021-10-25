# Rate limiting

For this user guide, we will be creating the configuration required to protect the Toy Store service
with rate limit configuration for authenticated users. Then, we will create API keys for two users,
`bob` and `alice`. `bob` is very greedy and rate limit will be rejecting some of his requests.
On the other hand, `alice`'s traffic does not exceed rate limitations and her traffic will not be throttled.

## Table of contents

* [Preparation](#preparation)
* [Create API keys](#create-api-keys)
* [Create kuadrant API Product object](#create-kuadrant-api-product-object)
* [Test the Toy Store API](#test-the-toy-store-api)
* [Next steps](#next-steps)

## Preparation

Follow [Getting Started](/doc/getting-started.md) to have the Toy Store service
being protected by kuadrant.

## Create API keys

We will create two API keys for the users `bob` and `alice`.

```yaml
❯ kubectl -n default apply -f - <<EOF
---
apiVersion: v1
kind: Secret
metadata:
  name: bob-api-key
  annotations:
    secret.kuadrant.io/user-id: bob
  labels:
    secret.kuadrant.io/managed-by: authorino
    api: toystore
stringData:
  api_key: BOB.KEY
type: Opaque
EOF

❯ kubectl -n default apply -f - <<EOF
---
apiVersion: v1
kind: Secret
metadata:
  name: alice-api-key
  annotations:
    secret.kuadrant.io/user-id: alice
  labels:
    secret.kuadrant.io/managed-by: authorino
    api: toystore
stringData:
  api_key: ALICE.KEY
type: Opaque
EOF
```

## Create kuadrant API Product object

The kuadrant API Product custom resource represents the kuadrant protection configuration for your service.
For this user guide, the minimal API Product custom resource will be extended to contain:
* `securityScheme` section with the configuration needed to protect the upstream APIs with API keys.
* `rateLimit` section with the configuration needed to apply authenticated rate limit for traffic up to 3 request every 5 sec.

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
  rateLimit:
    authenticated:
      maxValue: 3
      period: 5
EOF
```

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

The configured rate limit is: **3** request max for a period of time of **5** seconds.
We will start running `bob`'s and `alice` traffic **concurrently**, but at different pace.
* `bob` will source traffic with 5 request every 5 seconds.
* `alice` will source traffic with 1 request every 5 seconds.


```bash
❯ bash <<EOF
run_alice_traffic(){
    for i in {1..4}; do
        curl --write-out 'ALICE %{http_code}' --silent --output /dev/null -H "Authorization: APIKEY ALICE.KEY" -H "Host: toystore.127.0.0.1.nip.io" localhost:9080/toys
        printf "\n"
        sleep 5
    done
}
run_bob_traffic(){
    for i in {1..20}; do
        curl --write-out 'BOB %{http_code}' --silent --output /dev/null -H "Authorization: APIKEY BOB.KEY" -H "Host: toystore.127.0.0.1.nip.io" localhost:9080/toys
        printf "\n"
        sleep 1
    done
}
run_bob_traffic &
run_alice_traffic &
wait
EOF

ALICE 200
BOB 200
BOB 200
BOB 200
BOB 429
BOB 429
ALICE 200
BOB 200
BOB 200
BOB 200
BOB 429
BOB 429
```

Both `alice` and `bob`'s requests will arrive concurrently and the expected behavior is that
`bob`'s traffic will be limited (some requests getting `429 Too Many Requests`)
while `alice`'s traffic will not be limited (all requests getting `200 OK`).

## Next steps

Check out other [user guides](/README.md#user-guides) for other kuadrant capabilities like AuthN or rate limit.
