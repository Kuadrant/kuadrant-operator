# Tier 3: Authenticate clients with certificate in request header only

> [!WARNING] Security warning
> This configuration provides **L7-only validation** without defense-in-depth. Use Tier 3 **only** when gateway-level TLS validation is not feasible.

## When this approach is acceptable

Use Tier 3 X.509 authentication when gateway-level TLS validation is not feasible and you understand and accept L7-only validation.

**Required:**
- ✅ **Acknowledged trade-offs**: You understand that Authorino becomes the sole validation layer, without cryptographic proof of private key possession

**Highly recommended:**
- ⭐ **Trusted upstream proxy**: Certificate header originates from a trusted proxy that performed TLS validation and prevents header spoofing

**If you can enable gateway-level TLS validation, use [Tier 1](x509-tier1-gateway-api-validation.md) or [Tier 2](x509-tier2-provider-specific.md) instead for defense-in-depth security.**

## Security trade-offs

Understand what you give up with Tier 3 compared to Tier 1/2:

| Security aspect | Tier 1/2 | Tier 3 |
|-----------------|----------|--------|
| **Defense-in-depth** | ✅ TLS + Application validation | ❌ Application validation only |
| **Private key proof** | ✅ Cryptographic verification | ❌ No cryptographic proof |
| **Header spoofing risk** | ✅ Gateway sanitizes XFCC headers | ⚠️ Relies on header source |
| **TLS layer rejection** | ✅ Invalid certs rejected at handshake | ❌ All requests reach application |
| **Validation points** | 2 (TLS + Authorino) | 1 (Authorino only) |

**Key risk**: Without gateway-level TLS validation, there's no cryptographic proof that the client possesses the private key for the certificate. Authorino still validates the certificate chain against trusted CAs, but cannot prevent a client from presenting a valid certificate they don't own.

## How it works

Tier 3 implements **single-layer validation** at the application level:

**Certificate source**: Certificate arrives in a request header
- Ideally: Set by a trusted upstream proxy after TLS validation
- Alternatively: Set by the client directly (understand the security implications)
- Header formats: `X-Forwarded-Client-Cert` (XFCC), `Client-Cert` (RFC 9440), or custom

**Application Layer (L7)**: Authorino validates certificate from header
- Authorino extracts certificate from request header
- Validates certificate chain against trusted CAs
- Applies label selector-based trust rules
- Performs authorization based on certificate attributes

**Security considerations**:
- **Header spoofing prevention**: Use a trusted upstream proxy that validates certificates and prevents clients from setting arbitrary certificate headers
- **L7-only validation**: Authorino is the sole validator—no cryptographic proof of private key possession
- **Certificate validation**: Authorino still validates the certificate chain against trusted CAs

## Before you begin

**Required:**
- **Kuadrant Operator**: Installed with Kuadrant instance deployed
- **Security awareness**: Understanding of L7-only validation trade-offs
- **jq**: Command-line JSON processor (for URL-encoding certificates in test commands)

**Recommended:**
- **Trusted upstream proxy**: Configured for mTLS, certificate header forwarding, and header spoofing prevention
- **Security review**: Approval from security team for L7-only validation approach

## Certificate source options

Tier 3 supports three certificate sources, depending on your upstream proxy configuration.

### Option 1: XFCC header (client-provided)

**Use when**: Clients send certificate data in `X-Forwarded-Client-Cert` header format and the gateway is configured to forward (not sanitize) the header

**Certificate format**: [Envoy XFCC text format](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_conn_man/headers#text-format)

**AuthPolicy configuration**:

```yaml
apiVersion: kuadrant.io/v1
kind: AuthPolicy
metadata:
  name: x509-header-xfcc
  namespace: default
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: api-route
  rules:
    authentication:
      "x509-xfcc":
        x509:
          source:
            xfccHeader: "x-forwarded-client-cert"
          selector:
            matchLabels:
              app.kubernetes.io/name: trusted-client
```

**Gateway configuration to forward XFCC headers**:

For **Istio**, annotate the Gateway to prevent XFCC header sanitization:

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: ingress-gateway
  namespace: gateway-system
  annotations:
    proxy.istio.io/config: '{"gatewayTopology": {"forwardClientCertDetails": "ALWAYS_FORWARD_ONLY"}}'
spec:
  # ... gateway spec
```

For **Envoy Gateway**, use an EnvoyPatchPolicy to change the default behavior:

```yaml
apiVersion: gateway.envoyproxy.io/v1alpha1
kind: EnvoyPatchPolicy
metadata:
  name: forward-xfcc
  namespace: gateway-system
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: ingress-gateway
  type: JSONPatch
  jsonPatches:
  - type: "type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager"
    name: https
    operation:
      op: replace
      path: "/forward_client_cert_details"
      value: ALWAYS_FORWARD_ONLY
```

### Option 2: Client-Cert header (RFC 9440)

**Use when**: Certificate arrives in `Client-Cert` header (typically set by an upstream proxy implementing [RFC 9440](https://datatracker.ietf.org/doc/rfc9440/), but can also be client-provided)

**Certificate format**: DER format, base64-encoded

**AuthPolicy configuration**:

```yaml
apiVersion: kuadrant.io/v1
kind: AuthPolicy
metadata:
  name: x509-header-client-cert
  namespace: default
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: api-route
  rules:
    authentication:
      "x509-client-cert":
        x509:
          source:
            clientCertHeader: "client-cert"
          selector:
            matchLabels:
              app.kubernetes.io/name: trusted-client
```

**Example: Upstream proxy setting Client-Cert header** (RFC 9440 compliant):

```caddyfile
# Caddy example
example.com {
    tls {
        client_auth {
            mode require_and_verify
            trusted_ca_cert_file /path/to/ca.pem
        }
    }

    reverse_proxy upstream:8080 {
        header_up Client-Cert ":{tls_client_certificate_der_base64}:"
    }
}
```

The proxy extracts the client certificate from the TLS connection, encodes it in DER format, base64-encodes the bytes, and sets the `Client-Cert` header.

### Option 3: Custom header with CEL expression

**Use when**: Certificate arrives in a custom header (from client or upstream proxy with non-standard header name/format)

**Certificate format**: PEM-encoded, URL-encoded

**AuthPolicy configuration**:

```yaml
apiVersion: kuadrant.io/v1
kind: AuthPolicy
metadata:
  name: x509-header-custom
  namespace: default
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: api-route
  rules:
    authentication:
      "x509-custom":
        x509:
          source:
            expression: 'request.http.headers["x-custom-client-cert"]'
          selector:
            matchLabels:
              app.kubernetes.io/name: trusted-client
```

**CEL expression flexibility**:

```yaml
# Extract from specific header
expression: 'request.http.headers["x-ssl-client-cert"]'

# Extract from nested header (if using structured metadata)
expression: 'request.http.headers["x-ssl-info"].split("cert=")[1]'

# Conditional extraction
expression: |
  has(request.http.headers["x-client-cert"])
    ? request.http.headers["x-client-cert"]
    : ""
```

**Upstream proxy configuration**: Varies by implementation. Example:

```apache
# Apache example
RequestHeader set X-Custom-Client-Cert "%{SSL_CLIENT_CERT}s"
```

## Step-by-step walkthrough

This example demonstrates **Option 1 (XFCC header)** with the gateway configured to forward certificate headers.

### Step 1: Prepare CA and client certificates

For testing, generate self-signed certificates. For production, use certificates from your PKI or cert-manager.

```bash
# Generate CA private key and certificate
openssl req -x509 -sha512 -nodes \
  -days 365 \
  -newkey rsa:4096 \
  -subj "/CN=Test CA/O=Kuadrant/C=US" \
  -addext basicConstraints=CA:TRUE \
  -addext keyUsage=digitalSignature,keyCertSign \
  -keyout /tmp/ca.key \
  -out /tmp/ca.crt

# Create X.509 v3 extensions file for client certificate
cat > /tmp/x509v3.ext << EOF
authorityKeyIdentifier=keyid,issuer
basicConstraints=CA:FALSE
keyUsage=digitalSignature,nonRepudiation,keyEncipherment,dataEncipherment
extendedKeyUsage=clientAuth
EOF

# Generate client private key and certificate signed by the CA
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

### Step 2: Create CA certificate resource

Create the CA certificate as a Secret for Authorino validation:

```bash
# Secret for Authorino validation
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

> [!NOTE] Note
>  Unlike Tier 1/2, Tier 3 only needs the Secret for Authorino. There's no ConfigMap/Secret for gateway TLS validation since the gateway doesn't validate certificates in this configuration.

### Step 3: Configure Gateway to forward XFCC headers

Create the Gateway and configure it to forward (not sanitize) XFCC headers:

**For Istio**:

First, create the Gateway resources:
```bash
kubectl apply -f https://raw.githubusercontent.com/Kuadrant/kuadrant-operator/refs/heads/main/examples/x509-authentication/gateway-tier3.yaml
```

Then, annotate the Gateway to forward XFCC headers:
```bash
kubectl annotate gateway mtls-gateway -n gateway-system \
  proxy.istio.io/config='{"gatewayTopology": {"forwardClientCertDetails": "ALWAYS_FORWARD_ONLY"}}'
```

**For Envoy Gateway**: Same as Istio, adjusting `gatewayClassName` as needed. Then apply the EnvoyPatchPolicy shown in Option 1 above.

### Step 4: Deploy application and HTTPRoute

```bash
kubectl apply -f https://raw.githubusercontent.com/Kuadrant/kuadrant-operator/refs/heads/main/examples/x509-authentication/httpbin.yaml
kubectl apply -f https://raw.githubusercontent.com/Kuadrant/kuadrant-operator/refs/heads/main/examples/x509-authentication/httproute.yaml
```

### Step 5: Configure AuthPolicy

The AuthPolicy configuration is the same as Tier 1 - it extracts the certificate from the XFCC header and validates it:

```bash
kubectl apply -f https://raw.githubusercontent.com/Kuadrant/kuadrant-operator/refs/heads/main/examples/x509-authentication/authpolicy.yaml
```

This creates an AuthPolicy that:
- Extracts the certificate from the `x-forwarded-client-cert` header
- Validates the certificate chain against CA certificates labeled `app.kubernetes.io/name: trusted-client`
- Enforces authorization based on certificate Organization attribute
- Injects certificate attributes into request headers

## Verify L7-only validation

Test authentication scenarios by sending XFCC headers with certificate data:

### Test 1: Valid certificate in XFCC header

```bash
GATEWAY_IP=$(kubectl get gateway mtls-gateway -n gateway-system -o jsonpath='{.status.addresses[0].value}')

# Send request with XFCC header containing the certificate
curl -ik https://httpbin.$GATEWAY_IP.nip.io/get \
  -H "X-Forwarded-Client-Cert: Hash=$(openssl x509 -in /tmp/client.crt -outform DER | openssl dgst -sha256 | awk '{print $2}');Cert=\"$(cat /tmp/client.crt | jq -sRr @uri)\";Subject=\"CN=test-client/O=Kuadrant/C=US\";URI="
```

**Expected**: HTTP 200. Authorino validates the certificate chain against trusted CAs and authorization succeeds.

### Test 2: No certificate header

```bash
curl -ik https://httpbin.$GATEWAY_IP.nip.io/get
```

**Expected**: HTTP 401. No XFCC header, authentication fails.

### Test 3: Invalid certificate in XFCC header

```bash
# Generate self-signed certificate not signed by the CA
openssl req -x509 -newkey rsa:2048 -nodes \
  -keyout /tmp/untrusted.key -out /tmp/untrusted.crt -days 365 \
  -subj "/CN=untrusted-client/O=Untrusted/C=US"

# Try to connect
curl -ik https://httpbin.$GATEWAY_IP.nip.io/get \
  -H "X-Forwarded-Client-Cert: Hash=$(openssl x509 -in /tmp/untrusted.crt -outform DER | openssl dgst -sha256 | awk '{print $2}');Cert=\"$(cat /tmp/untrusted.crt | jq -sRr @uri)\";Subject=\"CN=untrusted-client/O=Untrusted/C=US\";URI="
```

**Expected**: HTTP 401. Certificate doesn't chain to trusted CA, Authorino rejects.

## Additional security measures

To improve security with Tier 3 configuration:
- Review upstream proxy certificate validation configuration
- Validate CA certificate rotation procedures
- Monitor for unusual authentication patterns
- Test certificate validation with expired/invalid certificates

## Multi-CA trust with Tier 3

Even with L7-only validation, you can still use multi-CA trust via label selectors:

```yaml
x509:
  source:
    xfccHeader: "x-forwarded-client-cert"
  selector:
    matchExpressions:
    - key: environment
      operator: In
      values: ["production", "staging"]
    - key: deprecated
      operator: DoesNotExist
```

Create CA Secrets with appropriate labels:

```bash
kubectl create secret tls production-ca \
  -n kuadrant-system \
  --cert=prod-ca.crt --key=prod-ca.key
kubectl label secret production-ca \
  -n kuadrant-system \
  authorino.kuadrant.io/managed-by=authorino \
  environment=production

kubectl create secret tls staging-ca \
  -n kuadrant-system \
  --cert=staging-ca.crt --key=staging-ca.key
kubectl label secret staging-ca \
  -n kuadrant-system \
  authorino.kuadrant.io/managed-by=authorino \
  environment=staging
```

## Troubleshooting

### Certificate header not found

**Symptoms**: Authorino rejects with "certificate not found" or "missing header"

**Possible causes**:
- Upstream proxy not setting header
- Header name mismatch
- Upstream proxy certificate validation failed

**Resolution**:
```bash
# Check request headers reaching the gateway
kubectl logs -n gateway-system -l gateway.networking.k8s.io/gateway-name=mtls-gateway | grep -i x-forwarded-client-cert

# Verify upstream proxy configuration
# (depends on your proxy implementation)

# Test header spoofing risk (shows why upstream proxy is recommended)
curl -ik https://httpbin.$GATEWAY_IP.nip.io/get \
  -H "x-forwarded-client-cert: Cert=\"...\""
```

### Certificate validation fails at Authorino

**Symptoms**: HTTP 401, Authorino logs show "invalid certificate"

**Possible causes**:
- Certificate doesn't chain to selected CA Secrets
- Certificate missing `extendedKeyUsage=clientAuth`
- Certificate expired
- Label selector doesn't match any CA Secrets

**Resolution**:
```bash
# Verify CA Secret labels
kubectl get secret -n kuadrant-system -l app.kubernetes.io/name=trusted-client --show-labels

# Check certificate EKU
openssl x509 -in /tmp/client.crt -noout -text | grep "TLS Web Client Authentication"

# Check Authorino logs
kubectl logs -n kuadrant-system -l authorino-resource=authorino | grep x509
```

## When to upgrade to Tier 1/2

Upgrade from Tier 3 to Tier 1 or Tier 2 if:

- Gateway API v1.5+ becomes available (→ Tier 1)
- You can configure gateway-level TLS validation (→ Tier 1 or Tier 2)
- Security requirements change to mandate defense-in-depth
- Compliance audit requires cryptographic proof of key possession
- You want to prevent header spoofing at the gateway level

**Migration path**: Follow [Tier 1](x509-tier1-gateway-api-validation.md) or [Tier 2](x509-tier2-provider-specific.md) guides, then remove Tier 3 configuration.

## See also

- [X.509 Authentication User Guides](x509-authentication.md) - Choose the right tier
- [Tier 1 Guide](x509-tier1-gateway-api-validation.md) - Recommended approach (upgrade when possible)
- [Tier 2 Guide](x509-tier2-provider-specific.md) - Defense-in-depth alternative
- [X.509 Authentication Overview](../../overviews/auth-x509.md) - Security architecture
- [RFC 9440: Client-Cert HTTP Header Field](https://www.rfc-editor.org/rfc/rfc9440.html)
- [Envoy XFCC Header](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_conn_man/headers#x-forwarded-client-cert)
