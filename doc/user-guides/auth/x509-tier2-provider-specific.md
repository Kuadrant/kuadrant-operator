# Tier 2: Authenticate clients with provider-specific TLS validation

## When to use this approach

Use Tier 2 X.509 authentication when you need defense-in-depth security but don't have Gateway API v1.5+ yet:

- **Defense-in-depth security**: Same two-layer validation as Tier 1 (TLS + Application)
- **Older Gateway API versions**: Works with Gateway API versions prior to v1.5
- **Provider flexibility**: Supports Istio (via EnvoyFilter) and Envoy Gateway (via EnvoyPatchPolicy)
- **Migration path**: Clear upgrade path to Tier 1 when you adopt Gateway API v1.5+

This approach provides **the same security guarantees as Tier 1** but requires provider-specific configuration knowledge.

## How it works

Tier 2 implements the same two-layer validation model as Tier 1, but uses provider-specific resources for TLS configuration:

**Layer 1 (TLS/L4)**: Gateway validates client certificates during TLS handshake
- Configured via **EnvoyFilter** (Istio) or **EnvoyPatchPolicy** (Envoy Gateway)
- Client presents certificate during mTLS handshake
- Gateway validates against configured CA certificates
- Invalid, expired, or untrusted certificates are rejected
- Gateway sets `x-forwarded-client-cert` (XFCC) header with certificate details
- Incoming XFCC headers from clients are sanitized

**Layer 2 (Application/L7)**: Authorino validates certificates from XFCC header
- Identical to Tier 1: AuthPolicy extracts certificate from XFCC header
- Applies fine-grained validation using label selectors on CA Secrets
- Enables multi-CA trust scenarios

**Result**: Request proceeds only if both layers succeed.

## Before you begin

Ensure you have:

- **Kubernetes cluster**: Any supported version
- **Gateway API**: Any version (does not require v1.5)
- **Gateway implementation**: Istio (any version) or Envoy Gateway
- **Kuadrant Operator**: Installed with Kuadrant instance deployed
- **Provider knowledge**: Familiarity with EnvoyFilter (Istio) or EnvoyPatchPolicy (Envoy Gateway)

## Choose your provider

Select the guide for your gateway provider:

- **[Istio](#istio-envoyfilter-configuration)**: Configure using EnvoyFilter
- **[Envoy Gateway](#envoy-gateway-envoypatchpolicy-configuration)**: Configure using EnvoyPatchPolicy

Both approaches achieve the same security outcome and use identical AuthPolicy configuration.

---

## Istio: EnvoyFilter configuration

### Step 1: Prepare CA and client certificates

Generate or obtain CA and client certificates. Client certificates **must** include `extendedKeyUsage=clientAuth`.

```bash
# Generate CA
openssl req -x509 -sha512 -nodes \
  -days 365 \
  -newkey rsa:4096 \
  -subj "/CN=Test CA/O=Kuadrant/C=US" \
  -addext basicConstraints=CA:TRUE \
  -addext keyUsage=digitalSignature,keyCertSign \
  -keyout /tmp/ca.key \
  -out /tmp/ca.crt

# Create X.509 v3 extensions for client certificate
cat > /tmp/x509v3.ext << EOF
authorityKeyIdentifier=keyid,issuer
basicConstraints=CA:FALSE
keyUsage=digitalSignature,nonRepudiation,keyEncipherment,dataEncipherment
extendedKeyUsage=clientAuth
EOF

# Generate client certificate
openssl genrsa -out /tmp/client.key 4096
openssl req -new \
  -subj "/CN=test-client/O=Kuadrant/C=US" \
  -key /tmp/client.key \
  -out /tmp/client.csr
openssl x509 -req -sha512 \
  -days 365 \
  -CA /tmp/ca.crt \
  -CAkey /tmp/ca.key \
  -CAcreateserial \
  -extfile /tmp/x509v3.ext \
  -in /tmp/client.csr \
  -out /tmp/client.crt
```

> [!IMPORTANT] Important
> Client certificates **must** include `extendedKeyUsage=clientAuth` for Authorino validation to succeed.

### Step 2: Create CA certificate resources

```bash
# ConfigMap for Gateway TLS validation (Layer 1)
kubectl create configmap client-ca-bundle \
  -n gateway-system \
  --from-file=ca.crt=/tmp/ca.crt

# Secret for Authorino validation (Layer 2)
kubectl create secret tls trusted-client-ca \
  -n kuadrant-system \
  --cert=/tmp/ca.crt \
  --key=/tmp/ca.key

# Label the secret so Authorino can discover it
kubectl label secret trusted-client-ca \
  -n kuadrant-system \
  authorino.kuadrant.io/managed-by=authorino \
  app.kubernetes.io/name=trusted-client
```

### Step 3: Configure Gateway

Create a Gateway without frontend TLS validation (handled by EnvoyFilter instead):

```bash
kubectl apply -f https://raw.githubusercontent.com/Kuadrant/kuadrant-operator/refs/heads/main/examples/x509-authentication/gateway-tier2-istio.yaml
```

This creates:
- **cert-manager Issuer**: Self-signed certificate issuer for the gateway server certificate
- **TLSPolicy**: Kuadrant policy to manage the gateway's server TLS certificate
- **ConfigMap**: Infrastructure configuration to mount the CA certificate bundle into gateway pods
- **Gateway**: Standard Gateway with `infrastructure.parametersRef` pointing to the ConfigMap (client certificate validation handled by EnvoyFilter)

### Step 4: Create EnvoyFilter for mTLS validation

Create an EnvoyFilter to configure Envoy's DownstreamTlsContext for client certificate validation:

```sh
kubectl apply -f -<<EOF
apiVersion: networking.istio.io/v1alpha3
kind: EnvoyFilter
metadata:
  name: mtls-validation
  namespace: gateway-system
spec:
  targetRefs:
  - group: gateway.networking.k8s.io
    kind: Gateway
    name: mtls-gateway
  configPatches:
  - applyTo: FILTER_CHAIN
    match:
      context: GATEWAY
      listener:
        portNumber: 443
    patch:
      operation: MERGE
      value:
        transport_socket:
          name: envoy.transport_sockets.tls
          typed_config:
            "@type": type.googleapis.com/envoy.extensions.transport_sockets.tls.v3.DownstreamTlsContext
            commonTlsContext:
              validationContext:
                trustedCa:
                  filename: /etc/certs/ca.crt
            requireClientCertificate: true
EOF
```

### Step 5: Deploy application and create HTTPRoute

```bash
# Deploy httpbin application
kubectl apply -f https://raw.githubusercontent.com/Kuadrant/kuadrant-operator/refs/heads/main/examples/x509-authentication/httpbin.yaml

# Create HTTPRoute
kubectl apply -f https://raw.githubusercontent.com/Kuadrant/kuadrant-operator/refs/heads/main/examples/x509-authentication/httproute.yaml
```

### Step 6: Configure AuthPolicy

The AuthPolicy configuration is **identical to Tier 1** - it extracts the certificate from the XFCC header and validates it:

```bash
kubectl apply -f https://raw.githubusercontent.com/Kuadrant/kuadrant-operator/refs/heads/main/examples/x509-authentication/authpolicy.yaml
```

This creates an AuthPolicy that:
- Extracts the certificate from the `x-forwarded-client-cert` header
- Validates the certificate chain against CA certificates labeled `app.kubernetes.io/name: trusted-client`
- Enforces authorization based on certificate Organization attribute
- Injects certificate attributes into request headers

---

## Envoy Gateway: EnvoyPatchPolicy configuration

### Step 1: Prepare CA and client certificates

Same as Istio (see [Istio Step 1](#step-1-prepare-ca-and-client-certificates))

### Step 2: Create CA certificate resources

Same as Istio (see [Istio Step 2](#step-2-create-ca-certificate-resources))

### Step 3: Configure Gateway

Create a Gateway without frontend TLS validation (handled by EnvoyPatchPolicy instead):

```bash
kubectl apply -f https://raw.githubusercontent.com/Kuadrant/kuadrant-operator/refs/heads/main/examples/x509-authentication/gateway-tier2-envoygateway.yaml
```

This creates:
- **cert-manager Issuer**: Self-signed certificate issuer for the gateway server certificate
- **TLSPolicy**: Kuadrant policy to manage the gateway's server TLS certificate
- **EnvoyProxy**: Custom resource to mount the CA certificate bundle into gateway pods
- **Gateway**: Standard Gateway with `infrastructure.parametersRef` pointing to the EnvoyProxy resource (client certificate validation handled by EnvoyPatchPolicy)

> [!NOTE]
> Unlike Istio which uses a plain ConfigMap, Envoy Gateway requires an `EnvoyProxy` custom resource for infrastructure configuration. The EnvoyProxy resource defines pod volumes and container volume mounts.

### Step 4: Create EnvoyPatchPolicy for mTLS validation

Create an EnvoyPatchPolicy to configure client certificate validation:

```sh
kubectl apply -f -<<EOF
apiVersion: gateway.envoyproxy.io/v1alpha1
kind: EnvoyPatchPolicy
metadata:
  name: mtls-validation
  namespace: gateway-system
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: mtls-gateway
  type: JSONPatch
  jsonPatches:
  - type: "type.googleapis.com/envoy.config.listener.v3.Listener"
    name: https
    operation:
      op: add
      path: "/filter_chains/0/transport_socket/typed_config/commonTlsContext/validationContext"
      value:
        trustedCa:
          filename: /etc/certs/ca.crt
  - type: "type.googleapis.com/envoy.config.listener.v3.Listener"
    name: https
    operation:
      op: add
      path: "/filter_chains/0/transport_socket/typed_config/requireClientCertificate"
      value: true
EOF
```

### Step 5: Deploy application and create HTTPRoute

Same as Istio (see [Istio Step 5](#step-5-deploy-application-and-create-httproute))

### Step 6: Configure AuthPolicy

Same as Istio (see [Istio Step 6](#step-6-configure-authpolicy))

---

## Verify defense-in-depth security

Testing is **identical to Tier 1**. Run the same test scenarios:

```bash
GATEWAY_IP=$(kubectl get gateway mtls-gateway -n gateway-system -o jsonpath='{.status.addresses[0].value}')

# Test 1: Valid certificate → HTTP 200
curl -ik https://httpbin.$GATEWAY_IP.nip.io/get \
  --cert /tmp/client.crt \
  --key /tmp/client.key

# Test 2: No certificate → TLS handshake fails
curl -ik https://httpbin.$GATEWAY_IP.nip.io/get

# Test 3: Untrusted certificate → TLS handshake fails
curl -ik https://httpbin.$GATEWAY_IP.nip.io/get \
  --cert /tmp/untrusted.crt \
  --key /tmp/untrusted.key

# Test 4: Valid cert, wrong attributes → HTTP 403
curl -ik https://httpbin.$GATEWAY_IP.nip.io/get \
  --cert /tmp/unauthorized-client.crt \
  --key /tmp/unauthorized-client.key
```

## Migration to Tier 1

When you upgrade to Gateway API v1.5+ and your gateway provider supports `spec.tls.frontend.default.validation`, migrate from Tier 2 to Tier 1:

### Migration steps

1. **Update Gateway resource** to include frontend TLS validation:
   ```yaml
   spec:
     tls:
       frontend:
         default:
           validation:
             caCertificateRefs:
             - name: client-ca-bundle
               kind: ConfigMap
             mode: AllowValidOnly
   ```

2. **Remove provider-specific resource**:
   ```bash
   # For Istio
   kubectl delete envoyfilter mtls-validation -n gateway-system

   # For Envoy Gateway
   kubectl delete envoypatchpolicy mtls-validation -n gateway-system
   ```

3. **Keep AuthPolicy unchanged**: AuthPolicy configuration remains identical

4. **Verify**: Run the same test scenarios to confirm migration succeeded

### Why migrate?

- **Standardization**: Uses Gateway API standard instead of vendor-specific resources
- **Maintainability**: Simpler configuration, easier to understand
- **Portability**: Gateway API configuration is portable across providers
- **Future-proof**: Gateway API v1.5 is the recommended approach going forward

## Troubleshooting

### EnvoyFilter not applied

**Symptoms**: TLS handshake succeeds without client certificate

**Resolution**:
```bash
# Verify EnvoyFilter exists
kubectl get envoyfilter mtls-validation -n gateway-system

# Check targetRef points to gateway
kubectl get envoyfilter mtls-validation -n gateway-system -o yaml | grep -A 3 targetRefs

# Check Envoy configuration was patched
kubectl exec -n gateway-system \
  $(kubectl get pod -n gateway-system -l gateway.networking.k8s.io/gateway-name=mtls-gateway -o jsonpath='{.items[0].metadata.name}') \
  -- curl -s localhost:15000/config_dump?include_eds | grep -A 20 validation_context
```

### CA certificate not mounted

**Symptoms**: Gateway logs show "failed to load trusted CA"

**Resolution**:
```bash
# Verify ConfigMap exists
kubectl get configmap client-ca-bundle -n gateway-system

# Check volume mount in gateway pod
kubectl describe pod -n gateway-system -l gateway.networking.k8s.io/gateway-name=mtls-gateway | grep -A 5 Mounts

# Verify file exists in pod
kubectl exec -n gateway-system \
  $(kubectl get pod -n gateway-system -l gateway.networking.k8s.io/gateway-name=mtls-gateway -o jsonpath='{.items[0].metadata.name}') \
  -- cat /etc/certs/ca.crt
```

### XFCC header not set correctly

**Symptoms**: Authorino rejects with "certificate not found"

**Resolution**:
```bash
# Verify forward_client_cert_details configuration
kubectl exec -n gateway-system \
  $(kubectl get pod -n gateway-system -l gateway.networking.k8s.io/gateway-name=mtls-gateway -o jsonpath='{.items[0].metadata.name}') \
  -- curl -s localhost:15000/config_dump?include_eds | grep -i forward_client_cert

# Check Envoy access logs for XFCC
kubectl logs -n gateway-system -l gateway.networking.k8s.io/gateway-name=mtls-gateway | grep XFCC
```

## See also

- [X.509 Authentication User Guides](x509-authentication.md) - Choose the right tier
- [Tier 1 Guide](x509-tier1-gateway-api-validation.md) - Recommended approach with Gateway API v1.5+
- [X.509 Authentication Overview](../../overviews/auth-x509.md) - Architecture and security
- [Istio EnvoyFilter Documentation](https://istio.io/latest/docs/reference/config/networking/envoy-filter/)
- [Envoy Gateway EnvoyPatchPolicy](https://gateway.envoyproxy.io/latest/api/extension_types/#envoypatchpolicy)
- [Envoy DownstreamTlsContext](https://www.envoyproxy.io/docs/envoy/latest/api-v3/extensions/transport_sockets/tls/v3/tls.proto#envoy-v3-api-msg-extensions-transport-sockets-tls-v3-downstreamtlscontext)
