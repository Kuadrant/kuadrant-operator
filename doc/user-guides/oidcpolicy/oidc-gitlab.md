# Securing Application with OIDCPolicy and Gitlab as IDP


This tutorial demonstrates how to use the _OIDCPolicy_ extension to secure an application and authenticate a user implementing
the [OpenID Connect Authorization Code Flow](https://openid.net/specs/openid-connect-core-1_0.html#CodeFlowAuth) configuring [Gitlab](https://about.gitlab.com) as the IDP.

## Overview

In this tutorial, you will:

1. Set up a basic Gateway and HTTPRoute
2. Deploy an example application
3. Apply an _OIDCPolicy_ to configure the AuthN/AuthZ for your application
4. Test the AuthN/AuthZ functionality

## Prerequisites

- Kubernetes cluster with Kuadrant operator installed. See our [Getting Started](/latest/getting-started) guide for more information.
- [kubectl](https://kubernetes.io/docs/tasks/tools/#kubectl) command line tool.
- An Application configured for [OIDC Provider in Gitlab](https://docs.gitlab.co.jp/ee/integration/openid_connect_provider.html)

## Notes

- We'll be using [nip.io wildcard DNS](https://nip.io/) in order to simplify the guide without the need to set up DNS instances

### Setup environment variables

Set environment variables for convenience:

```sh
export KUADRANT_GATEWAY_NS=api-gateway      # Namespace for the Gateway
export KUADRANT_GATEWAY_NAME=ingressgateway # Name for the Gateway
export KUADRANT_DEVELOPER_NS=bakery         # Namespace for the app
export KUADRANT_KEYCLOAK_NS=keycloak        # Namespace for keycloak
```

## Configure Kuadrant Operator Deployment

* In order to extend auth service timeout to allow sufficient time to go through authorino to IDP and back

```sh
kubectl -n kuadrant-system patch deployment kuadrant-operator-controller-manager \
  --type='json' \
  -p='[{"op": "add", "path": "/spec/template/spec/containers/0/env/-", "value": {"name": "AUTH_SERVICE_TIMEOUT", "value": "5s"}}]'
```

## Deploy Kuadrant and Gateway

* Deploy the Kuadrant instance:

```sh
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1beta1
kind: Kuadrant
metadata:
  name: kuadrant
  namespace: kuadrant-system
EOF
```

* Create the gateway namespace and deploy the Gateway:

```sh
kubectl create ns ${KUADRANT_GATEWAY_NS}
kubectl apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: ${KUADRANT_GATEWAY_NAME}
  namespace: ${KUADRANT_GATEWAY_NS}
  labels:
    kuadrant.io/gateway: "true"
spec:
  gatewayClassName: istio # Replace with `eg` for Envoy gateway
  listeners:
    - name: http
      protocol: HTTP
      port: 80
      allowedRoutes:
        namespaces:
          from: All
EOF
```

* Export the assigned IP to the gateway:

```sh
export INGRESS_IP=$(kubectl get gateway/${KUADRANT_GATEWAY_NAME} -n ${KUADRANT_GATEWAY_NS} -o jsonpath='{.status.addresses[0].value}')
```

* Configure the gateway with the required listeners:

```sh
kubectl apply -n ${KUADRANT_GATEWAY_NS} -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: ${KUADRANT_GATEWAY_NAME}
spec:
  gatewayClassName: istio # Replace with `eg` for Envoy gateway
  listeners:
    - allowedRoutes:
        namespaces:
          from: All
      hostname: bakery.${INGRESS_IP}.nip.io
      name: baker
      port: 80
      protocol: HTTP
EOF
```

## Deploy the Demo application and wire it to the Gateway

* Create namespace and deploy the app

```sh
kubectl create ns ${KUADRANT_DEVELOPER_NS}
kubectl apply -n ${KUADRANT_DEVELOPER_NS} -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: baker
spec:
  selector:
    matchLabels:
      app: baker
  template:
    metadata:
      labels:
        app: baker
    spec:
      containers:
      - name: baker-app
        image: quay.io/kuadrant/authorino-examples:baker-app
        imagePullPolicy: IfNotPresent
        ports:
        - containerPort: 8000
  replicas: 1
---
apiVersion: v1
kind: Service
metadata:
  name: baker
spec:
  selector:
    app: baker
  ports:
    - port: 8000
      protocol: TCP
EOF
```

* Connect the app to the Gateway with an HTTPRoute

```sh
kubectl apply -n ${KUADRANT_DEVELOPER_NS} -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: baker-route
spec:
  parentRefs:
  - kind: Gateway
    name: ${KUADRANT_GATEWAY_NAME}
    namespace: ${KUADRANT_GATEWAY_NS}
    sectionName: baker
  rules:
  - matches:
    - path:
        value: /baker
    backendRefs:
    - kind: Service
      name: baker
      port: 8000
EOF
```

## Configure your Gitlab Application

* Edit your Application Callback URL to `http://bakery.${INGRESS_IP}.nip.io/auth/callback` making sure to replace the `${INGRESS_IP}` for the Gateway IP.
* Also, check the options `openid` and `profile`

## (Optional) Try to reach the app before we apply the _OIDCPolicy_

* Open `http://bakery.$INGRESS_IP.nip.io/baker` in browser

## Secure the application with an _OIDCPolicy_

* Apply the OIDCPolicy

```sh
kubectl apply -n ${KUADRANT_DEVELOPER_NS} -f -<<EOF
apiVersion: extensions.kuadrant.io/v1alpha1
kind: OIDCPolicy
metadata:
  name: baker-auth
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: baker-route
  provider:
    clientID: [YOUR_GITLAB_CLIENT_ID]
    issuerURL: [YOUR_GITLAB_ISSUER_URL]
EOF
```

* Open `http://bakery.$INGRESS_IP.nip.io/baker` in browser and login to your Gitlab account and authorize the app.

## Try Authorization

One could add Authorization rules based on claims, for example, add `groups_direct` claim and the group your user is a direct member of.

* Apply the following updated _OIDCPolicy_

```sh
kubectl apply -n ${KUADRANT_DEVELOPER_NS} -f -<<EOF
apiVersion: extensions.kuadrant.io/v1alpha1
kind: OIDCPolicy
metadata:
  name: baker-auth
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: baker-route
  provider:
    clientID: [YOUR_GITLAB_CLIENT_ID]
    issuerURL: [YOUR_GITLAB_ISSUER_URL]
  auth:
    claims:
      groups_direct: [THE_GROUP_YOUR_USER_BELONGS]
EOF
```
