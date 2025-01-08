# AuthPolicy Integration with Authzed/SpiceDB

This guide explains how to configure permission requests for a Google Zanzibar-based [Authzed/SpiceDB](https://authzed.com) instance using gRPC.

## Prerequisites

You have installed Kuadrant in a [kubernetes](https://docs.kuadrant.io/latest/kuadrant-operator/doc/install/install-kubernetes/) or [OpenShift](https://docs.kuadrant.io/latest/kuadrant-operator/doc/install/install-openshift/) cluster.

### Deploy Toy Store application

Deploy a simple HTTP application service that echoes back the request data: 

```sh
kubectl apply -f https://raw.githubusercontent.com/Kuadrant/kuadrant-operator/refs/heads/main/examples/toystore/toystore.yaml
```

### Expose the Application
Create an `HTTPRoute` to expose a `/posts` path for `GET` and `POST` requests to the application:

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
    - method: GET
      path:
        type: PathPrefix
        value: "/posts"
    - method: POST
      path:
        type: PathPrefix
        value: "/posts"
    backendRefs:
    - name: toystore
      port: 80
EOF
```

Export the gateway hostname and port for testing:

```sh
export INGRESS_HOST=$(kubectl get gtw kuadrant-ingressgateway -n gateway-system -o jsonpath='{.status.addresses[0].value}')
export INGRESS_PORT=$(kubectl get gtw kuadrant-ingressgateway -n gateway-system -o jsonpath='{.spec.listeners[?(@.name=="http")].port}')
export GATEWAY_URL=$INGRESS_HOST:$INGRESS_PORT
```

### Test the Unprotected Application
Test requests to the unprotected application:

```sh
curl -H 'Host: api.toystore.com' http://$GATEWAY_URL/posts -i
# HTTP/1.1 200 OK
```

### Create the permission database

Create the namespace for SpiceDB:

```sh
kubectl create namespace spicedb
```

Deploy the SpiceDB instance:

```sh
kubectl -n spicedb apply -f -<<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: spicedb
  labels:
    app: spicedb
spec:
  selector:
    matchLabels:
      app: spicedb
  template:
    metadata:
      labels:
        app: spicedb
    spec:
      containers:
      - name: spicedb
        image: authzed/spicedb
        args:
        - serve
        - "--grpc-preshared-key"
        - secret
        - "--http-enabled"
        ports:
        - containerPort: 50051
        - containerPort: 8443
  replicas: 1
---
apiVersion: v1
kind: Service
metadata:
  name: spicedb
spec:
  selector:
    app: spicedb
  ports:
    - name: grpc
      port: 50051
      protocol: TCP
    - name: http
      port: 8443
      protocol: TCP
EOF
```

Forward local request to the SpiceDB service inside the cluster:

```sh
kubectl -n spicedb port-forward service/spicedb 8443:8443 2>&1 >/dev/null &
```

Create the permission schema:

```sh
curl -X POST http://localhost:8443/v1/schema/write \
  -H 'Authorization: Bearer secret' \
  -H 'Content-Type: application/json' \
  -d @- << EOF
{
  "schema": "definition blog/user {}\ndefinition blog/post {\n\trelation reader: blog/user\n\trelation writer: blog/user\n\n\tpermission read = reader + writer\n\tpermission write = writer\n}"
}
EOF
```

Create the relationships:

- `blog/user:emilia` → `writer` of `blog/post:1`
- `blog/user:beatrice` → `reader` of `blog/post:1`

```sh
curl -X POST http://localhost:8443/v1/relationships/write \
  -H 'Authorization: Bearer secret' \
  -H 'Content-Type: application/json' \
  -d @- << EOF
{
  "updates": [
    {
      "operation": "OPERATION_CREATE",
      "relationship": {
        "resource": {
          "objectType": "blog/post",
          "objectId": "1"
        },
        "relation": "writer",
        "subject": {
          "object": {
            "objectType": "blog/user",
            "objectId": "emilia"
          }
        }
      }
    },
    {
      "operation": "OPERATION_CREATE",
      "relationship": {
        "resource": {
          "objectType": "blog/post",
          "objectId": "1"
        },
        "relation": "reader",
        "subject": {
          "object": {
            "objectType": "blog/user",
            "objectId": "beatrice"
          }
        }
      }
    }
  ]
}
EOF
```

### Create an `AuthPolicy`

Store the shared token for Authorino authentication with the SpiceDB instance (must be created in the same namespace as the Kuadrant CR):

```sh
kubectl -n kuadrant-system apply -f -<<EOF
apiVersion: v1
kind: Secret
metadata:
  name: spicedb
  labels:
    app: spicedb
stringData:
  grpc-preshared-key: secret
EOF
```

Create `AuthPolicy` custom resource declaring the auth rules to be enforced:

```sh
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1
kind: AuthPolicy
metadata:
  name: route-auth
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: toystore
  defaults:
    strategy: atomic
    rules:
      authentication:
        "blog-users":
          apiKey:
            selector:
              matchLabels:
                app: talker-api
            allNamespaces: true
          credentials:
            authorizationHeader:
              prefix: APIKEY
      authorization:
        "authzed-spicedb":
          spicedb:
            endpoint: spicedb.spicedb.svc.cluster.local:50051
            insecure: true
            sharedSecretRef:
              name: spicedb
              key: grpc-preshared-key
            subject:
              kind:
                value: blog/user
              name:
                selector: auth.identity.metadata.annotations.username
            resource:
              kind:
                value: blog/post
              name:
                selector: context.request.http.path.@extract:{"sep":"/","pos":2}
            permission:
              selector: context.request.http.method.@replace:{"old":"GET","new":"read"}.@replace:{"old":"POST","new":"write"}
EOF
```

### Create the API keys

For Emilia (writer):

```sh
kubectl apply -f -<<EOF
apiVersion: v1
kind: Secret
metadata:
  name: api-key-writer
  labels:
    authorino.kuadrant.io/managed-by: authorino
    app: talker-api
  annotations:
    username: emilia
stringData:
  api_key: IAMEMILIA
EOF
```

For Beatrice (reader):

```sh
kubectl apply -f -<<EOF
apiVersion: v1
kind: Secret
metadata:
  name: api-key-reader
  labels:
    authorino.kuadrant.io/managed-by: authorino
    app: talker-api
  annotations:
    username: beatrice
stringData:
  api_key: IAMBEATRICE
EOF
```

### Consume the API

As Emilia, send a `GET` request:

```sh
curl -H 'Host: api.toystore.com' -H 'Authorization: APIKEY IAMEMILIA' \
     -X GET \
     http://$GATEWAY_URL/posts/1 -i
# HTTP/1.1 200 OK
```

As Emilia, send a `POST` request:

```sh
curl -H 'Host: api.toystore.com' -H 'Authorization: APIKEY IAMEMILIA' \
     -X POST \
     http://$GATEWAY_URL/posts/1 -i
# HTTP/1.1 200 OK
```

As Beatrice, send a `GET` request:

```sh
curl -H 'Host: api.toystore.com' -H 'Authorization: APIKEY IAMBEATRICE' \
     -X GET \
     http://$GATEWAY_URL/posts/1 -i
# HTTP/1.1 200 OK
```

As Beatrice, send a `POST` request:

```sh
curl -H 'Host: api.toystore.com' -H 'Authorization: APIKEY IAMBEATRICE' \
     -X POST \
      http://$GATEWAY_URL/posts/1 -i
# HTTP/1.1 403 Forbidden
# x-ext-auth-reason: PERMISSIONSHIP_NO_PERMISSION;token=GhUKEzE2NzU3MDE3MjAwMDAwMDAwMDA=
```

## Cleanup

```sh
kubectl delete -f https://raw.githubusercontent.com/Kuadrant/kuadrant-operator/refs/heads/main/examples/toystore/toystore.yaml
kubectl delete httproute toystore
kubectl delete authpolicy route-auth
kubectl delete kuadrant kuadrant -n kuadrant-system
kubectl delete namespace spicedb
kubectl delete secret api-key-reader 
kubectl delete secret api-key-writer 
kubectl delete secret spicedb -n kuadrant-system
```
