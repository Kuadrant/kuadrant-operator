# Secure, protect, and connect APIs with Kuadrant

## Overview

This guide walks you through using Kuadrant to secure, protect, and connect an API exposed by a Gateway (Kubernetes Gateway API) from the personas platform engineer and application developer. For more information on the different personas please see the [Gateway API documentation](https://gateway-api.sigs.k8s.io/concepts/roles-and-personas/#key-roles-and-personas)

## Prerequisites

- Completed the steps in [Install Kuadrant on an OpenShift cluster](../../install/install-openshift.md).
- [kubectl](https://kubernetes.io/docs/tasks/tools/#kubectl) command line tool.
- AWS/Azure or GCP with DNS capabilities.


### Set the environment variables

Set the following environment variables used for convenience in this guide:

```bash
export KUADRANT_GATEWAY_NS=api-gateway # Namespace for the example Gateway
export KUADRANT_GATEWAY_NAME=external # Name for the example Gateway
export KUADRANT_DEVELOPER_NS=toystore # Namespace for an example toystore app
export KUADRANT_AWS_ACCESS_KEY_ID=xxxx # AWS Key ID with access to manage the DNS Zone ID below
export KUADRANT_AWS_SECRET_ACCESS_KEY=xxxx # AWS Secret Access Key with access to manage the DNS Zone ID below
export KUADRANT_AWS_REGION=us-east-1 # Region to create the DNS resources in AWS
export KUADRANT_AWS_DNS_PUBLIC_ZONE_ID=xxxx # AWS Route 53 Zone ID for the Gateway
export KUADRANT_ZONE_ROOT_DOMAIN=example.com # Root domain associated with the Zone ID above
export KUADRANT_CLUSTER_ISSUER_NAME=lets-encrypt # Name for the ClusterIssuer
export KUADRANT_EMAIL=foo@example.com # Email address to associate with the example LetsEncrypt issuer
```

### Set up a DNS Provider

The DNS provider declares credentials to access the zone(s) that Kuadrant can use to set up DNS configuration. Ensure that this credential only has access to the zones you want Kuadrant to manage via `DNSPolicy`

Create the namespace the Gateway will be deployed in:

```bash
kubectl create ns ${KUADRANT_GATEWAY_NS}
```

Create the secret credentials in the same namespace as the Gateway - these will be used to configure DNS:

```bash
kubectl -n ${KUADRANT_GATEWAY_NS} create secret generic aws-credentials \
  --type=kuadrant.io/aws \
  --from-literal=AWS_ACCESS_KEY_ID=$KUADRANT_AWS_ACCESS_KEY_ID \
  --from-literal=AWS_SECRET_ACCESS_KEY=$KUADRANT_AWS_SECRET_ACCESS_KEY
```

Before adding a TLS issuer, create the secret credentials in the cert-manager namespace:

```bash
kubectl -n cert-manager create secret generic aws-credentials \
  --type=kuadrant.io/aws \
  --from-literal=AWS_ACCESS_KEY_ID=$KUADRANT_AWS_ACCESS_KEY_ID \
  --from-literal=AWS_SECRET_ACCESS_KEY=$KUADRANT_AWS_SECRET_ACCESS_KEY
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
  rules:
  - group: gateway.networking.k8s.io
    kind: Gateway
    name: ${KUADRANT_GATEWAY_NAME}
    namespace: ${KUADRANT_GATEWAY_NAMESPACE}
  hostnames:
  - "api.${KUADRANT_ZONE_ROOT_DOMAIN}"
  rules:
  - matches:
    - method: GET
      path:
        type: PathPrefix
        value: "/cars"
EOF
```
The status of the HTTPRoute will not be accepted or enforced, it will be unknown as the Gateway hasn't been created yet:

```bash
kubectl get httproute toystore -n ${KUADRANT_DEVELOPER_NS} -o=jsonpath='{.status.conditions[?(@.type=="Accepted")].message}{"\n"}{.status.conditions[?(@.type=="Enforced")].message}'

```
### Add a TLS issuer

To secure communication to the Gateways, define a TLS issuer for TLS certificates.

!!! note 
    This example uses Let's Encrypt, but you can use any issuer supported by `cert-manager`.

```bash
kubectl apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: ${KUADRANT_CLUSTER_ISSUER_NAME}
spec:
  acme:
    email: ${KUADRANT_EMAIL}
    privateKeySecretRef:
      name: le-secret
    server: https://acme-staging-v02.api.letsencrypt.org/directory
    solvers:
      - dns01:
          route53:
            hostedZoneID: ${KUADRANT_AWS_DNS_PUBLIC_ZONE_ID}
            region: ${KUADRANT_AWS_REGION}
            accessKeyIDSecretRef:
              key: AWS_ACCESS_KEY_ID
              name: aws-credentials
            secretAccessKeySecretRef:
              key: AWS_SECRET_ACCESS_KEY
              name: aws-credentials
EOF
```

Wait for the `ClusterIssuer` to become ready.

```bash
kubectl wait clusterissuer/${KUADRANT_CLUSTER_ISSUER_NAME} --for=condition=ready=true
```

### Deploy a Gateway

```bash
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
    - allowedRoutes:
        namespaces:
          from: All 
      hostname: "*.${KUADRANT_ZONE_ROOT_DOMAIN}"
      name: api
      port: 443
      protocol: HTTPS
      tls:
        certificateRefs:
        - group: ""
          kind: Secret
          name: api-${KUADRANT_GATEWAY_NAME}-tls
        mode: Terminate
EOF
```


Check the status of the `Gateway` ensuring the gateway is Accepted and Programmed:

```bash
kubectl get gateway ${KUADRANT_GATEWAY_NAME} -n ${KUADRANT_GATEWAY_NS} -o=jsonpath='{.status.conditions[?(@.type=="Accepted")].message}{"\n"}{.status.conditions[?(@.type=="Programmed")].message}'
```

Check the status of the listener, you will see that it is not yet programmed or ready to accept traffic due to bad TLS configuration. This will be fixed in the next step with the `TLSPolicy`:

```bash
kubectl get gateway ${KUADRANT_GATEWAY_NAME} -n ${KUADRANT_GATEWAY_NS} -o=jsonpath='{.status.listeners[0].conditions[?(@.type=="Programmed")].message}'
```

### Deploy the gateway TLS policy

```bash
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1
kind: TLSPolicy
metadata:
  name: ${KUADRANT_GATEWAY_NAME}-tls
  namespace: ${KUADRANT_GATEWAY_NS}
spec:
  targetRef:
    name: ${KUADRANT_GATEWAY_NAME}
    group: gateway.networking.k8s.io
    kind: Gateway
  issuerRef:
    group: cert-manager.io
    kind: ClusterIssuer
    name: ${KUADRANT_CLUSTER_ISSUER_NAME}
EOF
```

Check that the `TLSpolicy` has an Accepted and Enforced status:

```bash
kubectl get tlspolicy ${KUADRANT_GATEWAY_NAME}-tls -n ${KUADRANT_GATEWAY_NS} -o=jsonpath='{.status.conditions[?(@.type=="Accepted")].message}{"\n"}{.status.conditions[?(@.type=="Enforced")].message}'
```

### Secure and protect the Gateway with Auth, Rate Limit, and DNS policies.

While the `Gateway` is now deployed, it currently has exposed endpoints. The next steps will be defining an `AuthPolicy` to set up a default `403` response for any unprotected endpoints, as well as a `RateLimitPolicy` to set up a default unrealistic low global limit to further protect any exposed endpoints.

### Set the `Deny all` Gateway AuthPolicy

```bash
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1
kind: AuthPolicy
metadata:
  name: ${KUADRANT_GATEWAY_NAME}-auth
  namespace: ${KUADRANT_GATEWAY_NS}
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: ${KUADRANT_GATEWAY_NAME}
  defaults:
   rules:
    authorization:
      deny-all:
        opa:
          rego: "allow = false"
    response:
      unauthorized:
        headers:
          "content-type":
            value: application/json
        body:
          value: |
            {
              "error": "Forbidden",
              "message": "Access denied by default by the gateway operator. If you are the administrator of the service, create a specific auth policy for the route."
            }
EOF
```

Check that the `AuthPolicy` has Accepted and Enforced status:

```bash
kubectl get authpolicy ${KUADRANT_GATEWAY_NAME}-auth -n ${KUADRANT_GATEWAY_NS} -o=jsonpath='{.status.conditions[?(@.type=="Accepted")].message}{"\n"}{.status.conditions[?(@.type=="Enforced")].message}'

```

### Deploy the `low-limit` Gateway RateLimitPolicy

```bash
kubectl apply -f  - <<EOF
apiVersion: kuadrant.io/v1
kind: RateLimitPolicy
metadata:
  name: ${KUADRANT_GATEWAY_NAME}-rlp
  namespace: ${KUADRANT_GATEWAY_NS}
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: ${KUADRANT_GATEWAY_NAME}
  defaults:
    limits:
      "low-limit":
        rates:
        - limit: 1
          window: 10s
EOF
```

Check that the `RateLimitPolicy` has Accepted and Enforced status:

```bash
kubectl get ratelimitpolicy ${KUADRANT_GATEWAY_NAME}-rlp -n ${KUADRANT_GATEWAY_NS} -o=jsonpath='{.status.conditions[?(@.type=="Accepted")].message}{"\n"}{.status.conditions[?(@.type=="Enforced")].message}'
```

### Create the Gateway DNSPolicy

```bash
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1
kind: DNSPolicy
metadata:
  name: ${KUADRANT_GATEWAY_NAME}-dnspolicy
  namespace: ${KUADRANT_GATEWAY_NS}
spec:
  loadBalancing:
    defaultGeo: true
    geo: GEO-NA
    weight: 120
  targetRef:
    name: ${KUADRANT_GATEWAY_NAME}
    group: gateway.networking.k8s.io
    kind: Gateway
  providerRefs:
  - name: aws-credentials # Secret created earlier
EOF
```

Check that the `DNSPolicy` has been Accepted and Enforced:

```bash
kubectl get dnspolicy ${KUADRANT_GATEWAY_NAME}-dnspolicy -n ${KUADRANT_GATEWAY_NS} -o=jsonpath='{.status.conditions[?(@.type=="Accepted")].message}{"\n"}{.status.conditions[?(@.type=="Enforced")].message}'
```

### Test the `low-limit` and deny all policies

```bash
while :; do curl -k --write-out '%{http_code}\n' --silent --output /dev/null  "https://api.$KUADRANT_ZONE_ROOT_DOMAIN/cars" | grep -E --color "\b(429)\b|$"; sleep 1; done
```

### (Optional) Configure metrics to be scraped from the Gateway instance

If Prometheus is installed on the cluster, set up a `PodMonitor` to configure it to scrape metrics directly from the Gateway pod.
This must be done in the namespace where the Gateway is running. For a list of the metrics you'll get see the Kuadrant [docs](https://docs.kuadrant.io/0.11.0/kuadrant-operator/doc/observability/metrics/)

```bash
kubectl apply -f - <<EOF
apiVersion: monitoring.coreos.com/v1
kind: PodMonitor
metadata:
  name: istio-proxies-monitor
  namespace: ${KUADRANT_GATEWAY_NS}
spec:
  selector:
    matchExpressions:
      - key: istio-prometheus-ignore
        operator: DoesNotExist
  podMetricsEndpoints:
    - path: /stats/prometheus
      interval: 30s
      relabelings:
        - action: keep
          sourceLabels: ["__meta_kubernetes_pod_container_name"]
          regex: "istio-proxy"
        - action: keep
          sourceLabels:
            ["__meta_kubernetes_pod_annotationpresent_prometheus_io_scrape"]
        - action: replace
          regex: (\d+);(([A-Fa-f0-9]{1,4}::?){1,7}[A-Fa-f0-9]{1,4})
          replacement: "[\$2]:\$1"
          sourceLabels:
            [
              "__meta_kubernetes_pod_annotation_prometheus_io_port",
              "__meta_kubernetes_pod_ip",
            ]
          targetLabel: "__address__"
        - action: replace
          regex: (\d+);((([0-9]+?)(\.|$)){4})
          replacement: "\$2:\$1"
          sourceLabels:
            [
              "__meta_kubernetes_pod_annotation_prometheus_io_port",
              "__meta_kubernetes_pod_ip",
            ]
          targetLabel: "__address__"
        - action: labeldrop
          regex: "__meta_kubernetes_pod_label_(.+)"
        - sourceLabels: ["__meta_kubernetes_namespace"]
          action: replace
          targetLabel: namespace
        - sourceLabels: ["__meta_kubernetes_pod_name"]
          action: replace
          targetLabel: pod_name
EOF
```


### Override the Gateway's deny-all AuthPolicy 

### Set up API key auth flow

Set up an example API key for the new users:

```bash
kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: toystore-api-key
  namespace: ${KUADRANT_DEVELOPER_NS}
  labels:
    authorino.kuadrant.io/managed-by: authorino
    kuadrant.io/apikeys-by: api_key
stringData:
  api_key: secret
type: Opaque
EOF
```

Create a new AuthPolicy in a different namespace that overrides the `Deny all` created earlier:

```bash
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1
kind: AuthPolicy
metadata:
  name: toystore-auth
  namespace: ${KUADRANT_DEVELOPER_NS}
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

### Override `low-limit` RateLimitPolicy for specific users

Create a new `RateLimitPolicy` in a different namespace to override the default `RateLimitPolicy` created earlier:

```bash
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1
kind: RateLimitPolicy
metadata:
  name: toystore-rlp
  namespace: ${KUADRANT_DEVELOPER_NS}
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: toystore
  limits:
    "general-user":
      rates:
      - limit: 1
        window: 3s
      counters:
      - expression: auth.identity.userid
      when:
      - predicate: "auth.identity.userid != 'bob'"
    "bob-limit":
      rates:
      - limit: 2
        window: 3s
      when:
      - predicate: "auth.identity.userid == 'bob'"
EOF
```

The `RateLimitPolicy` should be Accepted and Enforced:

```bash
kubectl get ratelimitpolicy -n ${KUADRANT_DEVELOPER_NS} toystore-rlp -o=jsonpath='{.status.conditions}'
```

Check the status of the `HTTPRoute`, is now affected by the `RateLimitPolicy` in the same namespace:

```bash
kubectl get httproute toystore -n ${KUADRANT_DEVELOPER_NS} -o=jsonpath='{.status.parents[0].conditions[?(@.type=="kuadrant.io/RateLimitPolicyAffected")].message}'
```

### Test the new Rate limit and Auth policy

#### Send requests as Alice:

```bash
while :; do curl -k --write-out '%{http_code}\n' --silent --output /dev/null -H 'Authorization: APIKEY IAMALICE' "https://api.$KUADRANT_ZONE_ROOT_DOMAIN/cars" | grep -E --color "\b(429)\b|$"; sleep 1; done
```

#### Send requests as Bob:

```bash
while :; do curl -k --write-out '%{http_code}\n' --silent --output /dev/null -H 'Authorization: APIKEY IAMBOB' "https://api.$KUADRANT_ZONE_ROOT_DOMAIN/cars" | grep -E --color "\b(429)\b|$"; sleep 1; done
```

## Next Steps

- [mTLS Configuration](../../install/mtls-configuration.md)
To learn more about Kuadrant and see more how to guides, visit Kuadrant [documentation](https://docs.kuadrant.io)
