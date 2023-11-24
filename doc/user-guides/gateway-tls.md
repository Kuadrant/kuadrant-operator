# Gateway TLS for Cluster Operators

This user guide walks you through an example of how to configure TLS for all routes attached to an ingress gateway.

<br/>

## Requisites

- [Docker](https://docker.io)

### Setup

This step uses tooling from the Kuadrant Operator component to create a containerized Kubernetes server locally using [Kind](https://kind.sigs.k8s.io),
where it installs Istio, Kubernetes Gateway API, CertManager and Kuadrant itself.

Clone the project:

```shell
git clone https://github.com/Kuadrant/kuadrant-operator && cd kuadrant-operator
```

Setup the environment:

```shell
make local-setup
```

Deploy policy controller and install TLSPolicy CRD:
```shell
make deploy-policy-controller
```

Install metallb:
```shell
make install-metallb
```

Fetch the current kind networks subnet:
```shell
docker network inspect kind -f '{{ (index .IPAM.Config 0).Subnet }}'
```
Response:
```shell
"172.18.0.0/16"
```

Create IPAddressPool within kind network(Fetched by the command above) e.g. 172.18.200
```shell
kubectl -n metallb-system apply -f -<<EOF
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: kuadrant-local
spec:
  addresses:
  - 172.18.200.0/24
---
apiVersion: metallb.io/v1beta1
kind: L2Advertisement
metadata:
  name: empty
EOF
```

Create a namespace:
```shell
kubectl create namespace my-gateways
```

### Create an ingress gateway

Create a gateway:
```sh
kubectl -n my-gateways apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: prod-web
spec:
  gatewayClassName: istio
  listeners:
    - allowedRoutes:
        namespaces:
          from: All
      name: api
      hostname: "*.toystore.local"
      port: 443
      protocol: HTTPS
      tls:
        mode: Terminate
        certificateRefs:
          - name: toystore-local-tls
            kind: Secret
EOF
```

### Enable TLS on the gateway

The TLSPolicy requires a reference to an existing [CertManager Issuer](https://cert-manager.io/docs/configuration/).

Create a CertManager Issuer:
```shell
kubectl apply -n my-gateways -f - <<EOF
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: selfsigned-issuer
spec:
  selfSigned: {}
EOF
```

> **Note:** We are using a [self-signed](https://cert-manager.io/docs/configuration/selfsigned/) issuer here but any supported CerManager issuer or cluster issuer can be used.

```shell
kubectl get issuer selfsigned-issuer -n my-gateways
```
Response:
```shell
NAME                        READY   AGE
selfsigned-issuer   True    18s
```

Create a Kuadrant `TLSPolicy` to configure TLS:
```sh
kubectl apply -n my-gateways -f - <<EOF
apiVersion: kuadrant.io/v1alpha1
kind: TLSPolicy
metadata:
  name: prod-web
spec:
  targetRef:
    name: prod-web
    group: gateway.networking.k8s.io
    kind: Gateway
  issuerRef:
    group: cert-manager.io
    kind: Issuer
    name: selfsigned-issuer
EOF
```

Check policy status:
```shell
kubectl get tlspolicy -n my-gateways
```
Response:
```shell

NAME       READY
prod-web   True
```

Check a Certificate resource was created:
```shell
kubectl get certificates -n my-gateways
```
Response
```shell
NAME                 READY   SECRET               AGE
toystore-local-tls   True    toystore-local-tls   7m30s

```

Check a TLS Secret resource was created:
```shell
kubectl get secrets -n my-gateways --field-selector="type=kubernetes.io/tls"
```
Response:
```shell
NAME                 TYPE                DATA   AGE
toystore-local-tls   kubernetes.io/tls   3      7m42s
```

### Deploy a sample API to test TLS

Deploy the sample API:
```shell
kubectl -n my-gateways apply -f examples/toystore/toystore.yaml
kubectl -n my-gateways wait --for=condition=Available deployments toystore --timeout=60s
```

Route traffic to the API from our gateway:
```shell
kubectl -n my-gateways apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: toystore
spec:
  parentRefs:
  - name: prod-web
    namespace: my-gateways
  hostnames:
  - "*.toystore.local"
  rules:
  - backendRefs:
    - name: toystore
      port: 80
EOF
```

### Verify TLS works by sending requests

Verify we can access the service via TLS:
```shell
curl -k https://api.toystore.local --resolve 'api.toystore.local:443:172.18.200.0'
```

## Cleanup

```shell
make local-cleanup
```
