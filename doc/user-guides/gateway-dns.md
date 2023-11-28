# Gateway DNS for Cluster Operators

This user guide walks you through an example of how to configure DNS for all routes attached to an ingress gateway.

<br/>

## Requisites

- [Docker](https://docker.io)
- [Rout53 Hosted Zone](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/CreatingHostedZone.html)

### Setup

This step uses tooling from the Kuadrant Operator component to create a containerized Kubernetes server locally using [Kind](https://kind.sigs.k8s.io),
where it installs Istio, Kubernetes Gateway API and Kuadrant itself.

Clone the project:

```shell
git clone https://github.com/Kuadrant/kuadrant-operator && cd kuadrant-operator
```

Setup the environment:

```shell
make local-setup
```

Deploy policy controller and install DNSPolicy CRD:
```shell
make deploy-policy-controller
```

Install metallb:
```shell
make install-metallb
```

Create a namespace:
```shell
kubectl create namespace my-gateways
```

Export a root domain and hosted zone id:
```shell
export ROOT_DOMAIN=<ROOT_DOMAIN>
export AWS_HOSTED_ZONE_ID=<AWS_HOSTED_ZONE_ID>
```

> **Note:** ROOT_DOMAIN and AWS_HOSTED_ZONE_ID should be set to your AWS hosted zone *name* and *id* respectively.

### Create a ManagedZone

Create AWS credentials secret
```shell
export AWS_ACCESS_KEY_ID=<AWS_ACCESS_KEY_ID> AWS_SECRET_ACCESS_KEY=<AWS_SECRET_ACCESS_KEY>

kubectl -n my-gateways create secret generic aws-credentials \
  --type=kuadrant.io/aws \
  --from-literal=AWS_ACCESS_KEY_ID=$AWS_ACCESS_KEY_ID \
  --from-literal=AWS_SECRET_ACCESS_KEY=$AWS_SECRET_ACCESS_KEY
```

Create a ManagedZone
```sh
kubectl -n my-gateways apply -f - <<EOF
apiVersion: kuadrant.io/v1alpha1
kind: ManagedZone
metadata:
  name: $ROOT_DOMAIN
spec:
  id: $AWS_HOSTED_ZONE_ID
  domainName: $ROOT_DOMAIN
  description: "my managed zone"
  dnsProviderSecretRef:
    name: aws-credentials
    namespace: my-gateways
EOF
```

Check it's ready
```shell
kubectl get managedzones -n my-gateways
```

### Create an ingress gateway

Create a gateway using your ROOT_DOMAIN as part of a listener hostname:
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
      hostname: "*.$ROOT_DOMAIN"
      port: 80
      protocol: HTTP
EOF
```

Check gateway status:
```shell
kubectl get gateway prod-web -n my-gateways
```
Response:
```shell
NAME       CLASS   ADDRESS        PROGRAMMED   AGE
prod-web   istio   172.18.200.0   True         25s
```

### Enable DNS on the gateway

Create a Kuadrant `DNSPolicy` to configure DNS:
```shell
kubectl -n my-gateways apply -f - <<EOF
apiVersion: kuadrant.io/v1alpha1
kind: DNSPolicy
metadata:
  name: prod-web
spec:
  targetRef:
    name: prod-web
    group: gateway.networking.k8s.io
    kind: Gateway
  routingStrategy: simple
EOF
```

Check policy status:
```shell
kubectl get dnspolicy -n my-gateways
```
Response:
```shell
NAME       READY
prod-web   True
```

### Deploy a sample API to test DNS

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
  - "*.$ROOT_DOMAIN"
  rules:
  - backendRefs:
    - name: toystore
      port: 80
EOF
```

Verify a DNSRecord resource is created:
```shell
kubectl get dnsrecords -n my-gateways
NAME           READY
prod-web-api   True
```

### Verify DNS works by sending requests

Verify DNS using dig:
```shell
dig foo.$ROOT_DOMAIN +short
```
Response:
```shell
172.18.200.0
```

Verify DNS using curl:

```shell
curl http://api.$ROOT_DOMAIN
```

## Cleanup

```shell
make local-cleanup
```
