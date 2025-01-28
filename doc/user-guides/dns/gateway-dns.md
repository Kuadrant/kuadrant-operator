# Gateway DNS configuration for routes attached to a ingress gateway

This tutorial walks you through an example of how to configure DNS for all routes attached to an ingress gateway. 

## Prerequisites

- Kubernetes cluster with Kuadrant operator installed. See our [Getting Started](/getting-started) guide for more information.
- [kubectl](https://kubernetes.io/docs/tasks/tools/#kubectl) command line tool.
- AWS/Azure or GCP with DNS capabilities.

### Setup environment variables

Set the following environment variables used for convenience in this tutorial:

```bash
export KUADRANT_GATEWAY_NS=api-gateway # Namespace for the example Gateway
export KUADRANT_GATEWAY_NAME=external # Name for the example Gateway
export KUADRANT_DEVELOPER_NS=toystore # Namespace for an example toystore app
export KUADRANT_AWS_ACCESS_KEY_ID=xxxx # AWS Key ID with access to manage the DNS Zone ID below
export KUADRANT_AWS_SECRET_ACCESS_KEY=xxxx # AWS Secret Access Key with access to manage the DNS Zone ID below
export KUADRANT_AWS_DNS_PUBLIC_ZONE_ID=xxxx # AWS Route 53 Zone ID for the Gateway
export KUADRANT_ZONE_ROOT_DOMAIN=example.com # Root domain associated with the Zone ID above
```

Create the namespace the Gateway will be deployed in:

```bash
kubectl create ns ${KUADRANT_GATEWAY_NS}


### Create a DNS provider secret 
Create AWS provider secret. You should limit the permissions of this credential to only the zones you want us to access.

```bash
kubectl -n ${KUADRANT_GATEWAY_NS} create secret generic aws-credentials \
  --type=kuadrant.io/aws \
  --from-literal=AWS_ACCESS_KEY_ID=$KUADRANT_AWS_ACCESS_KEY_ID \
  --from-literal=AWS_SECRET_ACCESS_KEY=$KUADRANT_AWS_SECRET_ACCESS_KEY
```

### Create an Ingress Gateway

Create a gateway using your KUADRANT_ZONE_ROOT_DOMAIN as part of a listener hostname:

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
      hostname: "api.${KUADRANT_ZONE_ROOT_DOMAIN}"    
EOF
```

Check the status of the `Gateway` ensuring the gateway is Accepted and Programmed:

```bash
kubectl get gateway ${KUADRANT_GATEWAY_NAME} -n ${KUADRANT_GATEWAY_NS} -o=jsonpath='{.status.conditions[?(@.type=="Accepted")].message}{"\n"}{.status.conditions[?(@.type=="Programmed")].message}{"\n"}'
```

### Enable DNS on the gateway

Create a Kuadrant `DNSPolicy` to configure DNS:

```shell
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1
kind: DNSPolicy
metadata:
  name: ${KUADRANT_GATEWAY_NAME}-dns
  namespace: ${KUADRANT_GATEWAY_NS}
spec:
  targetRef:
    name: ${KUADRANT_GATEWAY_NAME}
    group: gateway.networking.k8s.io
    kind: Gateway
  providerRefs:  
    - name: aws-credentials
EOF
```

Check that the `DNSPolicy` has been Accepted and Enforced (This mat take a few minutes):

```bash
kubectl get dnspolicy ${KUADRANT_GATEWAY_NAME}-dns -n ${KUADRANT_GATEWAY_NS} -o=jsonpath='{.status.conditions[?(@.type=="Accepted")].message}{"\n"}{.status.conditions[?(@.type=="Enforced")].message}'
```

### Deploy the Toystore app

Create the namespace for the Toystore application:

```bash

kubectl create ns ${KUADRANT_DEVELOPER_NS}
```

Deploy the Toystore app to the developer namespace:

```bash
kubectl apply -f https://raw.githubusercontent.com/Kuadrant/Kuadrant-operator/main/examples/toystore/toystore.yaml -n ${KUADRANT_DEVELOPER_NS}
```

### Setup Toystore application HTTPRoute

```bash

kubectl apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: toystore
  namespace: ${KUADRANT_DEVELOPER_NS}
  labels:
    deployment: toystore
    service: toystore
spec:
  parentRefs:
  - name: ${KUADRANT_GATEWAY_NAME}
    namespace: ${KUADRANT_GATEWAY_NS}
  hostnames:
  - "api.${KUADRANT_ZONE_ROOT_DOMAIN}"
  rules:
  - matches:
    - method: GET
      path:
        type: PathPrefix
        value: "/cars"
    backendRefs:
    - name: toystore
      port: 80  
EOF
```


### Verify DNS works by sending requests

Verify DNS using dig you should see your IP address:

```shell
dig api.${KUADRANT_ZONE_ROOT_DOMAIN} +short
```

Verify DNS using curl you should get a status 200:

```shell
curl http://api.$KUADRANT_ZONE_ROOT_DOMAIN/cars -i
```
