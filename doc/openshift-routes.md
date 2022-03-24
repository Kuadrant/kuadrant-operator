This guide shows how to apply API protection (authZ and rate limiting) on Openshift Routes.
The protected API service must be part of the Istio's mesh injecting a sidecar in the service's pod.

### Installing Istio in openshift

[Official doc: Istio on Openshift](https://istio.io/latest/docs/setup/platform-setup/openshift/)

```
oc adm policy add-scc-to-group anyuid system:serviceaccounts:istio-system
```

```
istioctl install --set profile=openshift
```

### Install kuadrant

Patch istio with Authorino as ext authZ
```
kubectl edit configmap istio -n istio-system
```

In the editor, add the extension provider definitions
```yaml
data:
  mesh: |-
    # Add the following content to define the external authorizers.
    extensionProviders:
    - name: "kuadrant-authorization"
      envoyExtAuthzGrpc:
        service: "authorino-authorino-authorization.kuadrant-system.svc.cluster.local"
        port: "50051"
```

Restart Istiod to allow the change to take effect with the following command:
```
kubectl rollout restart deployment/istiod -n istio-system
```

Install kuadrant components

```
export KUADRANT_NAMESPACE="kuadrant-system"
oc new-project "${KUADRANT_NAMESPACE}"

# Authorino
kubectl apply -f utils/local-deployment/authorino-operator.yaml
kubectl apply -n "${KUADRANT_NAMESPACE}" -f utils/local-deployment/authorino.yaml

# Limitador
kubectl apply -n "${KUADRANT_NAMESPACE}" -f utils/local-deployment/limitador-operator.yaml
kubectl apply -n "${KUADRANT_NAMESPACE}" -f utils/local-deployment/limitador.yaml

kubectl -n "${KUADRANT_NAMESPACE}" wait --timeout=300s --for=condition=Available deployments --all

# Run locally the kuadrant controller
make run
```

### User Guide

#### Deploy Toystore app

```
oc new-project myns

oc adm policy add-scc-to-group anyuid system:serviceaccounts:myns

oc apply -n myns -f - <<EOF
apiVersion: "k8s.cni.cncf.io/v1"
kind: NetworkAttachmentDefinition
metadata:
  name: istio-cni
EOF

oc apply -f examples/toystore/toystore.yaml
```

#### Inject sidecar

```
kubectl patch deployment toystore --patch '{"spec": {"template": {"metadata": {"labels": {"sidecar.istio.io/inject": "true"}}}}}'
```

#### Create the route

```
export TOYSTORE_DOMAIN="toystore.example.com"
oc expose svc/toystore --hostname "${TOYSTORE_DOMAIN}"
```

Verify that the route has been created

```
oc get route toystore -o yaml
```

Verify that we can reach our example deployment

```
curl -v http://${TOYSTORE_DOMAIN}/toy
```

#### Add authZ

Add kuadrant label to the route

```
oc label route toystore kuadrant.io/managed=true
```

Create AuthConfig for Authorino external authz provider

```yaml
k apply -f - <<EOF
---
apiVersion: authorino.kuadrant.io/v1beta1
kind: AuthConfig
metadata:
  name: toystore
spec:
  hosts:
    - ${TOYSTORE_DOMAIN}
  identity:
    - apiKey:
        labelSelectors:
          app: toystore
          authorino.kuadrant.io/managed-by: authorino
      credentials:
        in: authorization_header
        keySelector: APIKEY
      name: apikey
EOF
```

Create secret with API key `KEY_FOR_DEMO`

```yaml
k apply -f - <<EOF
---
apiVersion: v1
kind: Secret
metadata:
  labels:
    app: toystore
    authorino.kuadrant.io/managed-by: authorino
  name: toystore-apikey
stringData:
  api_key: KEY_FOR_DEMO
type: Opaque
EOF
```

Annotate the route with Kuadrant auth annotation

```
kubectl annotate route/toystore kuadrant.io/auth-provider=kuadrant-authorization
```

Verify that unauthenticated request cannot reach toystore API

```
curl -v http://${TOYSTORE_DOMAIN}/toy
```

Verify that authenticated request can reach toystore API

```
curl -v -H 'Authorization: APIKEY KEY_FOR_DEMO' http://${TOYSTORE_DOMAIN}/toy
```

Disable authZ

```
k annotate route toystore kuadrant.io/auth-provider-
```

Verify that we can reach our example deployment

```
curl -v http://${TOYSTORE_DOMAIN}/toy
```

#### Add rate limit

Create RateLimitPolicy for ratelimiting

```yaml
k apply -f - <<EOF
---
apiVersion: apim.kuadrant.io/v1alpha1
kind: RateLimitPolicy
metadata:
  name: toystore
spec:
  rateLimits:
    - stage: PREAUTH
      actions:
        - generic_key:
            descriptor_key: vhaction
            descriptor_value: "yes"
        - request_headers:
            descriptor_key: "path"
            header_name: ":path"
        - remote_address: {}
  limits:
    - conditions: ["vhaction == yes"]
      max_value: 2
      namespace: preauth
      seconds: 10
      variables: []
EOF
```

Annotate the route with RateLimitPolicy name to trigger EnvoyFilters creation.

```
kubectl annotate route toystore kuadrant.io/ratelimitpolicy=toystore
```

Verify rate limit. 2 times and should be rate limited

```
curl -v http://${TOYSTORE_DOMAIN}/toy
```

### Clean up resources

```
oc adm policy remove-scc-from-group anyuid system:serviceaccounts:myns
oc delete project myns
```
