# App ID and App Key Authentication

This tutorial demonstrates how to implement [3scale-style App ID and App Key pair authentication](https://docs.redhat.com/en/documentation/red_hat_3scale_api_management/2.14/html/administering_the_api_gateway/authentication-patterns#app_id_and_app_key_pair) in Kuadrant using [AuthPolicy](https://docs.kuadrant.io/1.4.x/kuadrant-operator/doc/reference/authpolicy/).

## Overview

In 3scale API Management, the App ID and App Key pair authentication pattern provides a two-factor credential system where:
- **App ID** identifies the application making the request
- **App Key** serves as the secret credential for that application

This pattern is more secure than single API key authentication because it separates identity (App ID) from the authentication secret (App Key), allowing for easier credential rotation and audit trails.

This tutorial shows how to replicate this authentication pattern in Kuadrant using Kubernetes Secrets to store application credentials.

## Architecture

```text
┌ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ┐
  HashiCorp Vault        ← Optional: Source of truth for production
└ ─ ─ ─ ─ ┬ ─ ─ ─ ─ ─ ─ ┘
         ╎ sync
         ╎
┌ ─ ─ ─ ─ ▼ ─ ─ ─ ─ ─ ─ ┐
  External Secrets        ← Optional: Automatic credential sync
│ Operator               │
└ ─ ─ ─ ─ ┬ ─ ─ ─ ─ ─ ─ ┘
         ╎ creates
         ╎
         ▼
┌────────────────────────┐
│ Kubernetes Secrets     │ ← API key credentials with app-id annotation
└────────┬───────────────┘
         │ references
         ▼
┌────────────────────────┐
│ Kuadrant AuthPolicy    │ ← Validates app_key + app_id headers
└────────────────────────┘
```

## Prerequisites

- Kubernetes cluster. Could be locally used with [Kind](https://kind.sigs.k8s.io)
- LoadBalancer installed like [MetalLB](https://metallb.io)
- [Gateway API](https://kubernetes.io/docs/concepts/services-networking/gateway/) and a Gateway provider ([Istio](https://istio.io) or [Envoy Gateway](https://gateway.envoyproxy.io))
- [kubectl](https://kubernetes.io/docs/tasks/tools/#kubectl) command line tool.

## Step 0: Install Kuadrant

Set up environment variables for the tutorial

```sh
export KUADRANT_GATEWAY_NS=gateway-system
export KUADRANT_GATEWAY_NAME=demo-gateway
export KUADRANT_SYSTEM_NS=kuadrant-system
export APP_NS=demo
```

```sh
helm repo add kuadrant https://kuadrant.io/helm-charts/ --force-update
helm install kuadrant-operator kuadrant/kuadrant-operator \
  --namespace ${KUADRANT_SYSTEM_NS} \
  --create-namespace

kubectl create -n ${KUADRANT_SYSTEM_NS} -f - <<EOF
apiVersion: kuadrant.io/v1beta1
kind: Kuadrant
metadata:
  name: kuadrant
spec: {}
EOF
```

## Step 1: Create an Ingress Gateway

Create the namespace the Gateway will be deployed in:

```sh
kubectl create ns ${KUADRANT_GATEWAY_NS}
```

Create an ingress gateway

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
  gatewayClassName: istio # for Envoy Gateway replace with eg
  listeners:
    - name: http
      protocol: HTTP
      port: 80
      allowedRoutes:
        namespaces:
          from: All
EOF
```

Wait for the Gateway to be ready and export the gateway URL:

```sh
kubectl wait --for=condition=Programmed gateway/${KUADRANT_GATEWAY_NAME} -n ${KUADRANT_GATEWAY_NS} --timeout=300s
```

Export the ingress gateway IP address to an env var
```sh
export INGRESS_IP=$(kubectl get gateway/${KUADRANT_GATEWAY_NAME} -n ${KUADRANT_GATEWAY_NS} -o jsonpath='{.status.addresses[0].value}')
export GATEWAY_URL=http://api.${INGRESS_IP}.nip.io
```

## Step 2: Create API Key Secrets

Create Kubernetes Secrets to store the App ID and App Key pairs for each application. Each Secret contains:
- The API key in the `api_key` data field
- The App ID in an annotation for validation
- Labels for Authorino to discover the secrets

```sh
kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: api-key-app-123
  namespace: kuadrant-system
  labels:
    authorino.kuadrant.io/managed-by: authorino
    app-credentials: "true"
  annotations:
    app-id: app123
type: Opaque
stringData:
  api_key: iamafriend
---
apiVersion: v1
kind: Secret
metadata:
  name: api-key-app-456
  namespace: kuadrant-system
  labels:
    authorino.kuadrant.io/managed-by: authorino
    app-credentials: "true"
  annotations:
    app-id: app456
type: Opaque
stringData:
  api_key: supersecret
EOF
```

**Key Points:**
- Label `authorino.kuadrant.io/managed-by: authorino` - Marks the secret for Authorino discovery
- Label `app-credentials: "true"` - Used by AuthPolicy selector to identify API key secrets
- Annotation `app-id` - Stores the App ID for validation against the request header
- `stringData.api_key` - The API key credential (automatically base64 encoded)

### Verify Secrets

Verify that the Secrets were created successfully:

```sh
kubectl get secrets -n kuadrant-system -l app-credentials=true

kubectl describe secret api-key-app-123 -n kuadrant-system
```

You should see both Secrets with the correct labels and annotations.

> **Note**: For production environments, consider using an external secret management system. See the [Using External Secret Management](#using-external-secret-management) section below for details on delegating credential management to _Vault_ services with External Secrets Operator.

## Step 3: Deploy Your Application

### 3.1 Create Application Namespace

```sh
kubectl create namespace ${APP_NS}
```

### 3.2 Deploy Demo API

Deploy a sample API service that will be protected by authentication:

```sh
kubectl apply -n ${APP_NS} -f - <<EOF
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: demo-api
  labels:
    app: demo-api
spec:
  replicas: 1
  selector:
    matchLabels:
      app: demo-api
  template:
    metadata:
      labels:
        app: demo-api
    spec:
      containers:
        - name: demo-api
          image: quay.io/3scale/authorino:echo-api
          env:
            - name: PORT
              value: "3000"
          ports:
            - containerPort: 3000
              name: http
          resources:
            requests:
              cpu: 100m
              memory: 128Mi
            limits:
              cpu: 200m
              memory: 256Mi
---
apiVersion: v1
kind: Service
metadata:
  name: demo-api
spec:
  selector:
    app: demo-api
  ports:
    - name: http
      port: 80
      protocol: TCP
      targetPort: 3000
EOF
```

This deploys the Authorino echo API, which echoes back request information useful for testing.

## Step 4: Configure Gateway and Route to your App

### 4.1 Configure hostname in your Ingress Gateway

Will use nip.io hostname in order to skip DNS configuration and use it locally:
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
  gatewayClassName: istio # for Envoy Gateway replace with eg
  listeners:
    - name: http
      hostname: api.$INGRESS_IP.nip.io
      protocol: HTTP
      port: 80
      allowedRoutes:
        namespaces:
          from: All
EOF
```

### 4.2 Create HTTPRoute

Create an HTTPRoute that exposes your API:

```sh
kubectl apply -n ${APP_NS} -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: demo-api-route
spec:
  parentRefs:
  - name: ${KUADRANT_GATEWAY_NAME}
    namespace: ${KUADRANT_GATEWAY_NS}
  rules:
  - matches:
    - path:
        type: PathPrefix
        value: /demo
    backendRefs:
    - name: demo-api
      port: 80
EOF
```

### 4.3 Test Unauthenticated Access

Before applying authentication, verify the API is accessible:

```sh
curl ${GATEWAY_URL}/demo -i
# HTTP/1.1 200 OK
```

You should receive a successful response from the echo API.

## Step 5: Apply AuthPolicy

Now apply an AuthPolicy that enforces App ID and App Key authentication:

```sh
kubectl apply -n ${APP_NS} -f - <<EOF
apiVersion: kuadrant.io/v1
kind: AuthPolicy
metadata:
  name: app-id-key-auth
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: demo-api-route
  rules:
    authentication:
      "api-key-authentication":
        apiKey:
          selector:
            matchLabels:
              app-credentials: "true"
          allNamespaces: true
        credentials:
          customHeader:
            name: app_key
    authorization:
      "app-id-validation":
        patternMatching:
          patterns:
            - predicate: request.headers['app_id'] == auth.identity.metadata.annotations['app-id']
    response:
      success:
        filters:
          "identity":
            json:
              properties:
                "appid":
                  selector: auth.identity.metadata.annotations.app-id

EOF
```

### How It Works

The AuthPolicy implements a two-stage validation:

1. **Authentication Stage** (`api-key-authentication`):
   - Selects Secrets with label `app-credentials: "true"` (synced from Vault)
   - Looks for the API key in the `app_key` custom header
   - Validates the provided key against stored secrets
   - Authorino loads the secret's annotations into `auth.identity.metadata.annotations`

2. **Authorization Stage** (`app-id-validation`):
   - Validates that the `app_id` request header matches the `app-id` annotation from the authenticated Secret
   - This ensures both the App ID and App Key are correct and belong to the same application
   - Uses pattern matching to compare `request.headers['app_id']` with `auth.identity.metadata.annotations['app-id']`

3. **Response Customization:**
   -  Success: Exposes `auth.identity.appid` for later rate limiting use.

## Step 6: Test Authentication

### 6.1 Test Without Credentials

```sh
curl ${GATEWAY_URL}/demo -i
```

Expected response:
```text
HTTP/1.1 401 Unauthorized
...
```

### 6.2 Test With API Key Only

```sh
curl -H "app_key: iamafriend" ${GATEWAY_URL}/demo -i
```

Expected response:
```text
HTTP/1.1 403 Forbidden
...
```

### 6.3 Test With Wrong App ID

```sh
curl -H "app_key: iamafriend" -H "app_id: wrong-id" ${GATEWAY_URL}/demo -i
```

Expected response:
```text
HTTP/1.1 403 Forbidden
...
```

### 6.4 Test With Valid Credentials

```sh
curl -H "app_key: iamafriend" -H "app_id: app123" ${GATEWAY_URL}/demo -i
```

Expected response:
```text
HTTP/1.1 200 OK
...
```

### 6.5 Test Second Application

```sh
curl -H "app_key: supersecret" -H "app_id: app456" ${GATEWAY_URL}/demo -i
```

Expected response:
```text
HTTP/1.1 200 OK
...
```

## Using External Secret Management

Instead of manually creating Kubernetes Secrets, you can delegate credential management to external secret providers like [HashiCorp Vault](https://www.vaultproject.io/) and automatically synchronize them to your cluster using the [External Secrets Operator (ESO)](https://external-secrets.io/).

This approach provides:
- **Centralized credential management** - Store all application credentials in Vault as the single source of truth
- **Automatic synchronization** - The _ESO_ continuously syncs credentials from Vault to Kubernetes Secrets
- **Simplified credential rotation** - Update credentials in Vault and the _ESO_ automatically updates the Kubernetes Secrets
- **Enhanced security** - Leverage Vault's advanced features like dynamic secrets, encryption, and audit logging
- **GitOps-friendly** - Manage ExternalSecret resources via Git while keeping sensitive data in Vault

To implement this:
1. Set up [HashiCorp Vault](https://developer.hashicorp.com/vault/tutorials/kubernetes) and store your App ID and App Key pairs
2. Install the [External Secrets Operator](https://external-secrets.io/latest/introduction/getting-started/) in your cluster
3. Create a `SecretStore` resource that connects to your Vault instance
4. Create `ExternalSecret` resources that define which Vault secrets to sync and how to template them as Kubernetes Secrets with the required labels and annotations

The ExternalSecret would create the same Kubernetes Secrets shown in Step 1, but automatically from Vault data. For detailed configuration examples, see the [External Secrets Operator provider documentation](https://external-secrets.io/latest/provider/hashicorp-vault/).

### Combining with RateLimitPolicy

You can combine App ID/Key authentication with rate limiting based on the authenticated application:

```sh
kubectl apply -n ${APP_NS}  -f -<<EOF
apiVersion: kuadrant.io/v1
kind: RateLimitPolicy
metadata:
  name: app-rate-limits
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: demo-api-route
  limits:
    "per-app":
      rates:
      - limit: 5
        window: 10s
      counters:
      - expression: auth.identity.appid
EOF
```

This applies a rate limit of 5 requests per 10 seconds per authenticated App ID.

## Cleanup

To clean up the resources created in this tutorial:

```sh
# Delete the AuthPolicy
kubectl delete authpolicy app-id-key-auth -n ${APP_NS}

# Delete the HTTPRoute
kubectl delete httproute demo-api-route -n ${APP_NS}

# Delete the demo API application
kubectl delete deployment demo-api -n ${APP_NS}
kubectl delete service demo-api -n ${APP_NS}

# Delete the Gateway
kubectl delete gateway ${KUADRANT_GATEWAY_NAME} -n ${KUADRANT_GATEWAY_NS}

# Delete the API key Secrets
kubectl delete secret api-key-app-123 -n ${KUADRANT_SYSTEM_NS}
kubectl delete secret api-key-app-456 -n ${KUADRANT_SYSTEM_NS}

# Delete the application namespace
kubectl delete namespace ${APP_NS}
```

## Security Considerations

1. **Use HTTPS in Production**: Always use TLS for Vault connections and API endpoints
2. **Secret Encryption at Rest**: Enable Kubernetes secret encryption
3. **RBAC**: Restrict access to Secrets and ExternalSecrets using Kubernetes RBAC
4. **Vault Policies**: Follow principle of least privilege for Vault policies
5. **Credential Rotation**: Implement regular credential rotation schedules
6. **Audit Logging**: Enable audit logging in both Vault and Kubernetes
7. **Network Policies**: Restrict network access to Vault and API endpoints

## Next Steps

- Explore [Kuadrant RateLimitPolicy](https://docs.kuadrant.io/latest/kuadrant-operator/doc/reference/ratelimitpolicy/) to add more rate limiting capabilities
- Learn about [TLSPolicy](https://docs.kuadrant.io/latest/kuadrant-operator/doc/reference/tlspolicy/) for automated certificate management
- Review [AuthPolicy documentation](https://docs.kuadrant.io/latest/kuadrant-operator/doc/reference/authpolicy/) for advanced authentication patterns
- Set up [multi-cluster DNS management](https://docs.kuadrant.io/latest/kuadrant-operator/doc/reference/dnspolicy/) with DNSPolicy

## Conclusion

This tutorial demonstrated how to implement 3scale-style App ID and App Key authentication in Kuadrant using AuthPolicy and Kubernetes Secrets. This approach provides:

- **Authentication** with separate App ID and App Key credentials
- **Flexible credential management** using Kubernetes Secrets that could be synced from a vault service
- **Simple credential rotation** by updating Secret resources syncing from their source of truth
- **Cloud-native standards** using Gateway API and Kubernetes-native patterns
- **Rate limiting per application** or by any other configuration, combining Secret annotations and AuthPolicy/RateLimitPolicy
- **Optional external secret management** via Vault services and External Secrets Operator

This provides a clear migration path from 3scale to Kuadrant while maintaining security best practices and enabling GitOps workflows.
