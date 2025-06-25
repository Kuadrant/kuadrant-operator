# Authenticated Rate Limiting with JWTs and Kubernetes RBAC

This tutorial walks you through an example of how to use Kuadrant to protect an application with policies to enforce: 

- authentication based OpenId Connect (OIDC) ID tokens (signed JWTs), issued by a Keycloak server;
- alternative authentication method by Kubernetes Service Account tokens;
- authorization delegated to Kubernetes RBAC system;
- rate limiting by user ID.

In this example, we will protect a sample REST API called **Toy Store**. In reality, this API is just an echo service that echoes back to the user whatever attributes it gets in the request.

The API listens to requests at the hostnames `*.toystore.com`, where it exposes the endpoints `GET /toy*`, `POST /admin/toy` and `DELETE /amind/toy`, respectively, to mimic operations of reading, creating, and deleting toy records.

Any authenticated user/service account can send requests to the Toy Store API, by providing either a valid Keycloak-issued access token or Kubernetes token.

Privileges to execute the requested operation (read, create or delete) will be granted according to the following RBAC rules, stored in the Kubernetes authorization system:

| Operation | Endpoint            | Required role     |
| --------- | ------------------- | ----------------- |
| Read      | `GET /toy*`         | `toystore-reader` |
| Create    | `POST /admin/toy`   | `toystore-write`  |
| Delete    | `DELETE /admin/toy` | `toystore-write`  |

Each user will be entitled to a maximum of 5rp10s (5 requests every 10 seconds).

## Prerequisites

- Kubernetes cluster with Kuadrant operator installed. See our [Getting Started](/1.2.x/getting-started) guide for more information.
- [kubectl](https://kubernetes.io/docs/tasks/tools/#kubectl) command line tool.

### Setup environment variables

Set the following environment variables used for convenience in this tutorial:

```bash
export KUADRANT_GATEWAY_NS=api-gateway # Namespace for the example Gateway
export KUADRANT_GATEWAY_NAME=external # Name for the example Gateway
export KUADRANT_DEVELOPER_NS=toystore # Namespace for an example toystore app
```

### Create an Ingress Gateway

Create the namespace the Gateway will be deployed in:

```bash
kubectl create ns ${KUADRANT_GATEWAY_NS}
```

Create a gateway using toystore as the listener hostname:

```sh
kubectl apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: ${KUADRANT_GATEWAY_NAME}
  namespace: ${KUADRANT_GATEWAY_NS}
  labels:
    kuadrant.io/gateway: "true"
spec:
  gatewayClassName: istio
  listeners:
    - name: http
      protocol: HTTP
      port: 80
      allowedRoutes:
        namespaces:
          from: All
EOF
```

Check the status of the `Gateway` ensuring the gateway is Accepted and Programmed:

```bash
kubectl get gateway ${KUADRANT_GATEWAY_NAME} -n ${KUADRANT_GATEWAY_NS} -o=jsonpath='{.status.conditions[?(@.type=="Accepted")].message}{"\n"}{.status.conditions[?(@.type=="Programmed")].message}{"\n"}'
```
### Deploy the Toy Store API

Create the namespace for the Toystore application:

```bash

kubectl create ns ${KUADRANT_DEVELOPER_NS}
```

Deploy the Toystore app to the developer namespace:

```bash
kubectl apply -f https://raw.githubusercontent.com/Kuadrant/Kuadrant-operator/main/examples/toystore/toystore.yaml -n ${KUADRANT_DEVELOPER_NS}
```
Create a HTTPRoute to route traffic to the service via Istio Ingress Gateway:

```sh
kubectl apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: toystore
  namespace: ${KUADRANT_DEVELOPER_NS}
spec:
  parentRefs:
  - name: ${KUADRANT_GATEWAY_NAME}
    namespace: ${KUADRANT_GATEWAY_NS}
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
#### API lifecycle

![Lifecycle](http://www.plantuml.com/plantuml/png/hP7DIWD1383l-nHXJ_PGtFuSIsaH1F5WGRtjPJgJjg6pcPB9WFNf7LrXV_Ickp0Gyf5yIJPHZMXgV17Fn1SZfW671vEylk2RRZqTkK5MiFb1wL4I4hkx88m2iwee1AqQFdg4ShLVprQt-tNDszq3K8J45mcQ0NGrj_yqVpNFgmgU7aim0sPKQzxMUaQRXFGAqPwmGJW40JqXv1urHpMA3eZ1C9JbDkbf5ppPQrdMV9CY2XmC-GWQmEGaif8rYfFEPLdDu9K_aq7e7TstLPyUcot-RERnI0fVVjxOSuGBIaCnKk21sWBkW-p9EUJMgnCTIot_Prs3kJFceEiu-VM2uLmKlIl2TFrZVQCu8yD9kg1Dvf8RP9SQ_m40)

#### Try the API unprotected

Export the gateway hostname and port:

```sh
export KUADRANT_INGRESS_HOST=$(kubectl get gtw ${KUADRANT_GATEWAY_NAME} -n ${KUADRANT_GATEWAY_NS} -o jsonpath='{.status.addresses[0].value}')
export KUADRANT_INGRESS_PORT=$(kubectl get gtw ${KUADRANT_GATEWAY_NAME} -n ${KUADRANT_GATEWAY_NS} -o jsonpath='{.spec.listeners[?(@.name=="http")].port}')
export KUADRANT_GATEWAY_URL=${KUADRANT_INGRESS_HOST}:${KUADRANT_INGRESS_PORT}
```

```sh
curl -H 'Host: api.toystore.com' http://$KUADRANT_GATEWAY_URL/toy -i
# HTTP/1.1 200 OK
```

It should return `200 OK`.

> **Note**: If the command above fails to hit the Toy Store API on your environment, try forwarding requests to the service and accessing over localhost:
>
> ```sh
> kubectl port-forward -n ${KUADRANT_GATEWAY_NS} service/${KUADRANT_GATEWAY_NS}-istio 9080:80 >/dev/null 2>&1 &
> export KUADRANT_GATEWAY_URL=localhost:9080
> ```
>
> ```sh
> curl -H 'Host: api.toystore.com' http://$KUADRANT_GATEWAY_URL/toy -i
> # HTTP/1.1 200 OK
> ```

### Deploy Keycloak

Create the namespace for Keycloak:

```sh
kubectl create namespace keycloak
```

Deploy Keycloak with a [bootstrap](https://github.com/kuadrant/authorino-examples#keycloak) realm, users, and clients:

```sh
kubectl apply -n keycloak -f https://raw.githubusercontent.com/Kuadrant/authorino-examples/main/keycloak/keycloak-deploy.yaml
```

> **Note:** The Keycloak server may take a couple of minutes to be ready.

### Enforce authentication and authorization for the Toy Store API

Create a Kuadrant `AuthPolicy` to configure authentication and authorization:

```sh
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1
kind: AuthPolicy
metadata:
  name: toystore-protection
  namespace: ${KUADRANT_DEVELOPER_NS}
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: toystore
  rules:
    authentication:
      "keycloak-users":
        jwt:
          issuerUrl: http://keycloak.keycloak.svc.cluster.local:8080/realms/kuadrant
      "k8s-service-accounts":
        kubernetesTokenReview:
          audiences:
          - https://kubernetes.default.svc.cluster.local
        overrides:
          "sub":
            selector: auth.identity.user.username
    authorization:
      "k8s-rbac":
        kubernetesSubjectAccessReview:
          user:
            selector: auth.identity.sub
    response:
      success:
        filters:
          "identity":
            json:
              properties:
                "userid":
                  selector: auth.identity.sub
EOF
```

#### Try the API missing authentication

```sh
curl -H 'Host: api.toystore.com' http://$KUADRANT_GATEWAY_URL/toy -i
# HTTP/1.1 401 Unauthorized
# www-authenticate: Bearer realm="keycloak-users"
# www-authenticate: Bearer realm="k8s-service-accounts"
# x-ext-auth-reason: {"k8s-service-accounts":"credential not found","keycloak-users":"credential not found"}
```

#### Try the API without permission

Obtain an access token with the Keycloak server:

```sh
KUADRANT_ACCESS_TOKEN=$(kubectl run token --attach --rm --restart=Never -q --image=curlimages/curl -- http://keycloak.keycloak.svc.cluster.local:8080/realms/kuadrant/protocol/openid-connect/token -s -d 'grant_type=password' -d 'client_id=demo' -d 'username=john' -d 'password=p' -d 'scope=openid' | jq -r .KUADRANT_ACCESS_TOKEN)
```

Send a request to the API as the Keycloak-authenticated user while still missing permissions:

```sh
curl -H "Authorization: Bearer $KUADRANT_ACCESS_TOKEN" -H 'Host: api.toystore.com' http://$KUADRANT_GATEWAY_URL/toy -i
# HTTP/1.1 403 Forbidden
```

Create a Kubernetes Service Account to represent a consumer of the API associated with the alternative source of identities `k8s-service-accounts`:

```sh
kubectl apply -f - <<EOF
apiVersion: v1
kind: ServiceAccount
metadata:
  name: client-app-1
EOF
```

Obtain an access token for the `client-app-1` service account:

```sh
KUADRANT_SA_TOKEN=$(kubectl create token client-app-1)
```

Send a request to the API as the service account while still missing permissions:

```sh
curl -H "Authorization: Bearer $KUADRANT_SA_TOKEN" -H 'Host: api.toystore.com' http://$KUADRANT_GATEWAY_URL/toy -i
# HTTP/1.1 403 Forbidden
```

### Grant access to the Toy Store API for user and service account

Create the `toystore-reader` and `toystore-writer` roles:

```sh
kubectl apply -f - <<EOF
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: toystore-reader
rules:
- nonResourceURLs: ["/toy*"]
  verbs: ["get"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: toystore-writer
rules:
- nonResourceURLs: ["/admin/toy"]
  verbs: ["post", "delete"]
EOF
```

Add permissions to the user and service account:

| User         | Kind                        | Roles                                |
| ------------ | --------------------------- | ------------------------------------ |
| john         | User registered in Keycloak | `toystore-reader`, `toystore-writer` |
| client-app-1 | Kuberentes Service Account  | `toystore-reader`                    |

```sh
kubectl apply -f - <<EOF
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: toystore-readers
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: toystore-reader
subjects:
- kind: User
  name: $(jq -R -r 'split(".") | .[1] | @base64d | fromjson | .sub' <<< "$KUADRANT_ACCESS_TOKEN")
- kind: ServiceAccount
  name: client-app-1
  namespace: default
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: toystore-writers
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: toystore-writer
subjects:
- kind: User
  name: $(jq -R -r 'split(".") | .[1] | @base64d | fromjson | .sub' <<< "$KUADRANT_ACCESS_TOKEN")
EOF
```

<details markdown="1">
  <summary><i>Q:</i> Can I use <code>Roles</code> and <code>RoleBindings</code> instead of <code>ClusterRoles</code> and <code>ClusterRoleBindings</code>?</summary>

Yes, you can.

The example above is for non-resource URL Kubernetes roles. For using `Roles` and `RoleBindings` instead of
`ClusterRoles` and `ClusterRoleBindings`, thus more flexible resource-based permissions to protect the API,
see the spec for [Kubernetes SubjectAccessReview authorization](https://docs.kuadrant.io/latest/authorino/docs/features/#kubernetes-subjectaccessreview-authorizationkubernetessubjectaccessreview)
in the Authorino docs.

</details>

#### Try the API with permission

Send requests to the API as the Keycloak-authenticated user:

```sh
curl -H "Authorization: Bearer $KUADRANT_ACCESS_TOKEN" -H 'Host: api.toystore.com' http://$KUADRANT_GATEWAY_URL/toy -i
# HTTP/1.1 200 OK
```

```sh
curl -H "Authorization: Bearer $KUADRANT_ACCESS_TOKEN" -H 'Host: api.toystore.com' -X POST http://$KUADRANT_GATEWAY_URL/admin/toy -i
# HTTP/1.1 200 OK
```

Send requests to the API as the Kubernetes service account:

```sh
curl -H "Authorization: Bearer $KUADRANT_SA_TOKEN" -H 'Host: api.toystore.com' http://$KUADRANT_GATEWAY_URL/toy -i
# HTTP/1.1 200 OK
```

```sh
curl -H "Authorization: Bearer $KUADRANT_SA_TOKEN" -H 'Host: api.toystore.com' -X POST http://$KUADRANT_GATEWAY_URL/admin/toy -i
# HTTP/1.1 403 Forbidden
```

### Enforce rate limiting on requests to the Toy Store API

Create a Kuadrant `RateLimitPolicy` to configure rate limiting:

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
    "per-user":
      rates:
      - limit: 5
        window: 10s
      counters:
      - expression: auth.identity.userid
EOF
```

> **Note:** It may take a couple of minutes for the RateLimitPolicy to be applied depending on your cluster.

#### Try the API rate limited

Each user should be entitled to a maximum of 5 requests every 10 seconds.

> **Note:** If the tokens have expired, you may need to refresh them first.

Send requests as the Keycloak-authenticated user:

```sh
while :; do curl --write-out '%{http_code}\n' --silent --output /dev/null -H "Authorization: Bearer $KUADRANT_ACCESS_TOKEN" -H 'Host: api.toystore.com' http://$KUADRANT_GATEWAY_URL/toy | grep -E --color "\b(429)\b|$"; sleep 1; done
```

Send requests as the Kubernetes service account:

```sh
while :; do curl --write-out '%{http_code}\n' --silent --output /dev/null -H "Authorization: Bearer $KUADRANT_SA_TOKEN" -H 'Host: api.toystore.com' http://$KUADRANT_GATEWAY_URL/toy | grep -E --color "\b(429)\b|$"; sleep 1; done
```
