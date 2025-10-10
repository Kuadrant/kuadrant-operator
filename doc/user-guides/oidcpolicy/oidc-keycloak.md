# Securing Application with OIDCPolicy and Keycloak as IDP

This tutorial demonstrates how to use the _OIDCPolicy_ extension to secure an application and authenticate a user implementing
the [OpenID Connect Authorization Code Flow](https://openid.net/specs/openid-connect-core-1_0.html#CodeFlowAuth) configuring [Keycloak](https://www.keycloak.org/) as the IDP.

## Overview

In this tutorial, you will:

1. Set up a basic Gateway and HTTPRoute
2. Deploy an example application
3. Deploy Keycloak and use it as the IDP
4. Apply an _OIDCPolicy_ to configure the AuthN/AuthZ for your application
5. Test the AuthN/AuthZ functionality

## Prerequisites

- Kubernetes cluster with Kuadrant operator installed. See our [Getting Started](/latest/getting-started) guide for more information.
- [kubectl](https://kubernetes.io/docs/tasks/tools/#kubectl) command line tool.

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
  gatewayClassName: istio # Replace with "eg" for Envoy gateway
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
  gatewayClassName: istio # Replace with "eg" for Envoy gateway
  listeners:
    - allowedRoutes:
        namespaces:
          from: All
      hostname: bakery.${INGRESS_IP}.nip.io
      name: baker
      port: 80
      protocol: HTTP
    - allowedRoutes:
        namespaces:
          from: Selector
          selector:
            matchLabels:
              kubernetes.io/metadata.name: keycloak
      hostname: keycloak.${INGRESS_IP}.nip.io
      name: keycloak
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

## Deploy the demo Keycloak instance and wire it to the Gateway

* Create namespace and deploy keycloak instance

```sh
kubectl create ns ${KUADRANT_KEYCLOAK_NS}
curl -sSL https://raw.githubusercontent.com/Kuadrant/kuadrant-operator/refs/heads/main/doc/user-guides/oidcpolicy/keycloak.yaml | envsubst | kubectl -n ${KUADRANT_KEYCLOAK_NS} apply -f -
```

* Connect the Keycloak instance to the Gateway with an HTTPRoute

```sh
kubectl -n ${KUADRANT_KEYCLOAK_NS} apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: keycloak-route
spec:
  parentRefs:
  - kind: Gateway
    name: ${KUADRANT_GATEWAY_NAME}
    namespace: ${KUADRANT_GATEWAY_NS}
    sectionName: keycloak
  rules:
  - matches:
    - path:
        value: /
    backendRefs:
    - kind: Service
      name: keycloak
      port: 8080
EOF
```

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
    authorizationEndpoint: http://keycloak.$INGRESS_IP.nip.io/realms/demo/protocol/openid-connect/auth
    clientID: oidc-demo
    issuerURL: http://keycloak.$INGRESS_IP.nip.io/realms/demo
    tokenEndpoint: http://keycloak.$INGRESS_IP.nip.io/realms/demo/protocol/openid-connect/token
EOF
```

* Open `http://bakery.$INGRESS_IP.nip.io/baker` in browser and login to Keycloak with _user/pass_ `jane/p`
