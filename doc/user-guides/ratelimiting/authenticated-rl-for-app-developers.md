# Authenticated Rate Limiting for Application developers

For more info on the different personas see [Gateway API](https://gateway-api.sigs.k8s.io/concepts/roles-and-personas/#key-roles-and-personas)

This user guide walks you through an example of how to configure authenticated rate limiting for an application using Kuadrant.

Authenticated rate limiting rate limits the traffic directed to an application based on attributes of the client user, who is authenticated by some authentication method. A few examples of authenticated rate limiting use cases are:

- User A can send up to 50rps ("requests per second"), while User B can send up to 100rps.
- Each user can send up to 20rpm ("request per minute").
- Admin users (members of the 'admin' group) can send up to 100rps, while regular users (non-admins) can send up to 20rpm and no more than 5rps.

In this guide, we will rate limit a sample REST API called **Toy Store**. In reality, this API is just an echo service that echoes back to the user whatever attributes it gets in the request. The API exposes an endpoint at `GET http://api.toystore.com/toy`, to mimic an operation of reading toy records.

We will define 2 users of the API, which can send requests to the API at different rates, based on their user IDs. The authentication method used is **API key**.

| User ID | Rate limit                             |
| ------- | -------------------------------------- |
| alice   | 5rp10s ("5 requests every 10 seconds") |
| bob     | 2rp10s ("2 requests every 10 seconds") |

### Setup the environment

Follow this [setup doc](https://github.com/Kuadrant/kuadrant-operator/blob/main/doc/install/install-make.md) to set up your environment before continuing with this doc.

### Deploy the Toy Store API

Create the deployment:

```sh
kubectl apply -f examples/toystore/toystore.yaml
```

Create a HTTPRoute to route traffic to the service via Istio Ingress Gateway:

![](https://i.imgur.com/rdN8lo3.png)

```sh
kubectl apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: toystore
spec:
  parentRefs:
  - name: kuadrant-ingressgateway
    namespace: gateway-system
  hostnames:
  - api.toystore.com
  rules:
  - matches:
    - path:
        type: Exact
        value: "/toy"
      method: GET
    backendRefs:
    - name: toystore
      port: 80
EOF
```

Export the gateway hostname and port:

```sh
export INGRESS_HOST=$(kubectl get gtw kuadrant-ingressgateway -n gateway-system -o jsonpath='{.status.addresses[0].value}')
export INGRESS_PORT=$(kubectl get gtw kuadrant-ingressgateway -n gateway-system -o jsonpath='{.spec.listeners[?(@.name=="http")].port}')
export GATEWAY_URL=$INGRESS_HOST:$INGRESS_PORT
```

Verify the route works:

```sh
curl -H 'Host: api.toystore.com' http://$GATEWAY_URL/toy -i
# HTTP/1.1 200 OK
```

> **Note**: If the command above fails to hit the Toy Store API on your environment, try forwarding requests to the service and accessing over localhost:
>
> ```sh
> kubectl port-forward -n gateway-system service/kuadrant-ingressgateway-istio 9080:80 >/dev/null 2>&1 &
> export GATEWAY_URL=localhost:9080
> ```
>
> ```sh
> curl -H 'Host: api.toystore.com' http://$GATEWAY_URL/toy -i
> # HTTP/1.1 200 OK
> ```

### Enforce authentication on requests to the Toy Store API

Create a Kuadrant `AuthPolicy` to configure the authentication:

```sh
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1
kind: AuthPolicy
metadata:
  name: toystore
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: toystore
  rules:
    authentication:
      "api-key-users":
        apiKey:
          selector:
            matchLabels:
              app: toystore
          allNamespaces: true
        credentials:
          authorizationHeader:
            prefix: APIKEY
    response:
      success:
        filters:
          "identity":
            json:
              properties:
                "userid":
                  selector: auth.identity.metadata.annotations.secret\.kuadrant\.io/user-id
EOF
```

Verify the authentication works by sending a request to the Toy Store API without API key:

```sh
curl -H 'Host: api.toystore.com' http://$GATEWAY_URL/toy -i
# HTTP/1.1 401 Unauthorized
# www-authenticate: APIKEY realm="api-key-users"
# x-ext-auth-reason: "credential not found"
```

Create API keys for users `alice` and `bob` to authenticate:

> **Note:** Kuadrant stores API keys as Kubernetes Secret resources. User metadata can be stored in the annotations of the resource.

```sh
kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: bob-key
  labels:
    authorino.kuadrant.io/managed-by: authorino
    app: toystore
  annotations:
    secret.kuadrant.io/user-id: bob
stringData:
  api_key: IAMBOB
type: Opaque
---
apiVersion: v1
kind: Secret
metadata:
  name: alice-key
  labels:
    authorino.kuadrant.io/managed-by: authorino
    app: toystore
  annotations:
    secret.kuadrant.io/user-id: alice
stringData:
  api_key: IAMALICE
type: Opaque
EOF
```

### Enforce authenticated rate limiting on requests to the Toy Store API

Create a Kuadrant `RateLimitPolicy` to configure rate limiting:

![](https://i.imgur.com/2A9sXXs.png)

```sh
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1
kind: RateLimitPolicy
metadata:
  name: toystore
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: toystore
  limits:
    "alice-limit":
      rates:
      - limit: 5
        window: 10s
      when:
      - predicate: "auth.identity.userid == 'alice'"
    "bob-limit":
      rates:
      - limit: 2
        window: 10s
      when:
      - predicate: "auth.identity.userid == 'bob'"
EOF
```

> **Note:** It may take a couple of minutes for the RateLimitPolicy to be applied depending on your cluster.

Verify the rate limiting works by sending requests as Alice and Bob.

Up to 5 successful (`200 OK`) requests every 10 seconds allowed for Alice, then `429 Too Many Requests`:

```sh
while :; do curl --write-out '%{http_code}\n' --silent --output /dev/null -H 'Authorization: APIKEY IAMALICE' -H 'Host: api.toystore.com' http://$GATEWAY_URL/toy | grep -E --color "\b(429)\b|$"; sleep 1; done
```

Up to 2 successful (`200 OK`) requests every 10 seconds allowed for Bob, then `429 Too Many Requests`:

```sh
while :; do curl --write-out '%{http_code}\n' --silent --output /dev/null -H 'Authorization: APIKEY IAMBOB' -H 'Host: api.toystore.com' http://$GATEWAY_URL/toy | grep -E --color "\b(429)\b|$"; sleep 1; done
```

## Cleanup

```sh
make local-cleanup
```
