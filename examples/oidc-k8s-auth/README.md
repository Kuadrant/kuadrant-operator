# Protecting an API with JSON Web Tokens (JWTs) and Kubernetes authnz using Kuadrant

Example of protecting an API (the Toy Store API) with authentication based on ID tokens (signed JWTs) issued by an
OpenId Connect (OIDC) server (Keycloak) and alternative Kubernetes Service Account tokens, and authorization based on
Kubernetes RBAC, with permissions (bindings) stored as Kubernetes Roles and RoleBindings.

## Pre-requisites

- [Docker](https://www.docker.com/)
- [kubectl](https://kubernetes.io/docs/reference/kubectl/) command-line tool
- [jq](https://stedolan.github.io/jq/)

## Run the guide ❶ → ❻

### ❶ Setup the environment

Clone the project:

```sh
git clone https://github.com/Kuadrant/kuadrant-operator && cd kuadrant-operator
```

Spin-up the cluster with all dependencies installed:

```sh
make local-env-setup deploy
```

<details>
  <summary>🤔 What exactly does the step above do?</summary>

  1. Creates a containerized Kuberentes server using [Kind](https://kind.sigs.k8s.io/)
  2. Installs [Istio](https://istio.io)
  3. Installs Kuberentes [Gateway API](https://gateway-api.sigs.k8s.io/concepts/api-overview)
  4. Installs the Kuadrant system (CRDs and operators)
</details>

### ❷ Deploy the API

Deploy the application in the `default` namespace:

```sh
kubectl apply -f examples/toystore/toystore.yaml
```

Create the `HTTPRoute`:

```sh
kubectl apply -f examples/toystore/httproute.yaml
```

Expose the API:

```sh
kubectl port-forward -n istio-system service/istio-ingressgateway 9080:80 2>&1 >/dev/null &
```

#### API lifecycle

![Lifecycle](http://www.plantuml.com/plantuml/png/hP7DIWD1383l-nHXJ_PGtFuSIsaH1F5WGRtjPJgJjg6pcPB9WFNf7LrXV_Ickp0Gyf5yIJPHZMXgV17Fn1SZfW671vEylk2RRZqTkK5MiFb1wL4I4hkx88m2iwee1AqQFdg4ShLVprQt-tNDszq3K8J45mcQ0NGrj_yqVpNFgmgU7aim0sPKQzxMUaQRXFGAqPwmGJW40JqXv1urHpMA3eZ1C9JbDkbf5ppPQrdMV9CY2XmC-GWQmEGaif8rYfFEPLdDu9K_aq7e7TstLPyUcot-RERnI0fVVjxOSuGBIaCnKk21sWBkW-p9EUJMgnCTIot_Prs3kJFceEiu-VM2uLmKlIl2TFrZVQCu8yD9kg1Dvf8RP9SQ_m40)

#### Try the API unprotected

```sh
curl -H 'Host: api.toystore.com' http://localhost:9080/toy -i
# HTTP/1.1 200 OK
```

### ❸ Request the Kuadrant instance

```sh
kubectl apply -f -<<EOF
apiVersion: kuadrant.io/v1beta1
kind: Kuadrant
metadata:
  name: kuadrant
spec: {}
EOF
```

### ❹ Deploy Keycloak

Create the namesapce:

```sh
kubectl create namespace keycloak
```

Deploy Keycloak:

```sh
kubectl apply -n keycloak -f https://raw.githubusercontent.com/Kuadrant/authorino-examples/main/keycloak/keycloak-deploy.yaml
```

The step above deploys Keycloak with a [preconfigured](https://github.com/kuadrant/authorino-examples#keycloak) realm and a couple of clients and users created.

The Keycloak server may take a couple minutes to be ready.

### ❺ Create the `AuthPolicy`

```sh
kubectl apply -f -<<EOF
apiVersion: kuadrant.io/v1beta1
kind: AuthPolicy
metadata:
  name: toystore-protection
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: toystore
  authScheme:
    identity:
      - name: keycloak-users
        oidc:
          endpoint: http://keycloak.keycloak.svc.cluster.local:8080/auth/realms/kuadrant
      - name: k8s-service-accounts
        kubernetes:
          audiences:
            - https://kubernetes.default.svc.cluster.local
    authorization:
      - name: k8s-rbac
        kubernetes:
          user:
            valueFrom:
              authJSON: auth.identity.sub
EOF
```

#### Try the API missing authentication

```sh
curl -H 'Host: api.toystore.com' http://localhost:9080/toy -i
# HTTP/1.1 401 Unauthorized
# www-authenticate: Bearer realm="keycloak-users"
# www-authenticate: Bearer realm="k8s-service-accounts"
# x-ext-auth-reason: {"k8s-service-accounts":"credential not found","keycloak-users":"credential not found"}
```

#### Try the API without permission

Obtain an access token with the Keycloak server:

```sh
ACCESS_TOKEN=$(kubectl run token --attach --rm --restart=Never -q --image=curlimages/curl -- http://keycloak.keycloak.svc.cluster.local:8080/auth/realms/kuadrant/protocol/openid-connect/token -s -d 'grant_type=password' -d 'client_id=demo' -d 'username=john' -d 'password=p' | jq -r .access_token)
```

Send requests to the API as the Keycloak-authenticated user (missing permission):

```sh
curl -H "Authorization: Bearer $ACCESS_TOKEN" -H 'Host: api.toystore.com' http://localhost:9080/toy -i
# HTTP/1.1 403 Forbidden
```

Create a Kubernetes Service Account to represent a user belonging to the other source of identities:

```sh
kubectl apply -f -<<EOF
apiVersion: v1
kind: ServiceAccount
metadata:
  name: client-app-1
EOF
```

Obtain an aaccess token for the `client-app-1` service account:

```sh
SA_TOKEN=$(kubectl create token client-app-1)
```

Send requests to the API as the service account (missing permission):

```sh
curl -H "Authorization: Bearer $SA_TOKEN" -H 'Host: api.toystore.com' http://localhost:9080/toy -i
# HTTP/1.1 403 Forbidden
```

### ❻ Grant access to the API

Create the `toystore-reader` and `toystore-writer` roles:

```sh
kubectl apply -f -<<EOF
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

Add permissions to the users and service accounts:

```sh
kubectl apply -f -<<EOF
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
  name: $(jq -R -r 'split(".") | .[1] | @base64d | fromjson | .sub' <<< "$ACCESS_TOKEN")
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
  name: $(jq -R -r 'split(".") | .[1] | @base64d | fromjson | .sub' <<< "$ACCESS_TOKEN")
EOF
```

<details>
  <summary>🤔 Can I use <code>Roles</code> and <code>RoleBindings</code> instead of <code>ClusterRoles</code> and <code>ClusterRoleBindings</code>?</summary>

  Yes, you can.

  The example above is for non-resource URL Kubernetes roles. For using `Roles` and `RoleBindings` instead of
  `ClusterRoles` and `ClusterRoleBindings`, thus more flexible resource-based permissions to protect the API,
  see the spec for [Kubernetes SubjectAccessReview authorization](https://github.com/Kuadrant/authorino/blob/v0.5.0/docs/features.md#kubernetes-subjectaccessreview-authorizationkubernetes)
  in the Authorino docs.
</details>

#### Try the API with permission

Send requests to the API as the Keycloak-authenticated user:

```sh
curl -H "Authorization: Bearer $ACCESS_TOKEN" -H 'Host: api.toystore.com' http://localhost:9080/toy -i
# HTTP/1.1 200 OK
```

```sh
curl -H "Authorization: Bearer $ACCESS_TOKEN" -H 'Host: api.toystore.com' -X POST http://localhost:9080/admin/toy -i
# HTTP/1.1 200 OK
```

Send requests to the API as the service account (missing permission):

```sh
curl -H "Authorization: Bearer $SA_TOKEN" -H 'Host: api.toystore.com' http://localhost:9080/toy -i
# HTTP/1.1 200 OK
```

```sh
curl -H "Authorization: Bearer $SA_TOKEN" -H 'Host: api.toystore.com' -X POST http://localhost:9080/admin/toy -i
# HTTP/1.1 403 Forbidden
```

## Cleanup

```sh
make local-cleanup
```
