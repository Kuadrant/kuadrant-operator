# X.509 Client Certificate Authentication

## Verify cryptographic identity with client certificates

When your organization requires strong cryptographic identity verification—such as for zero-trust architectures, compliance requirements, or machine-to-machine communication with PKI—X.509 client certificate authentication provides defense-in-depth security with validation at both the TLS layer and application layer.

### When to use X.509 authentication

Choose X.509 client certificate authentication when:

- **Zero-trust architecture**: You need cryptographic proof of identity for every request
- **Compliance requirements**: Security standards (PCI DSS, HIPAA, FedRAMP) mandate mutual TLS (mTLS)
- **Machine-to-machine communication**: Services authenticate with long-lived certificates from your PKI
- **Multi-CA trust**: Different clients are issued certificates by different CAs, and you need fine-grained control over which CAs are trusted for which routes
- **No shared secrets**: You want to avoid the operational burden of rotating and distributing API keys or tokens

## How X.509 authentication works in Kuadrant

Kuadrant's X.509 authentication implements a two-layer validation model for defense-in-depth security:

**Layer 1 (TLS/L4)**: The Gateway validates client certificates during the TLS handshake against CA certificates configured in the Gateway resource. This cryptographically verifies that the client possesses the private key and that the certificate is trusted.

**Layer 2 (Application/L7)**: Authorino validates certificates extracted from the `X-Forwarded-Client-Cert` (XFCC) header using label selectors to support multi-CA trust and fine-grained validation rules.

Both layers must succeed for the request to be authenticated. This defense-in-depth approach provides:
- **Security**: Invalid, expired, or untrusted certificates are rejected at the TLS layer before reaching the application
- **Flexibility**: Application-layer validation supports multi-CA trust scenarios where different CAs are trusted for different routes or services
- **Auditability**: Certificate attributes (CN, Organization, etc.) are available for authorization decisions and request enrichment

## Configuration tiers

Kuadrant supports three configuration tiers for X.509 authentication, each with different security characteristics and requirements. Choose the tier that matches your Gateway API version, gateway provider, and security requirements.

### Tier 1: Gateway API v1.5+ frontend TLS validation (Recommended)

**Use Tier 1 when:** You have Gateway API v1.5+ and a compatible gateway implementation (Istio v1.28+, Envoy Gateway with v1.5 support).

Tier 1 uses the standard Gateway API `spec.tls.frontend.default.validation` field to configure client certificate validation at the Gateway level, providing automatic defense-in-depth security.

**Security characteristics:**
- ✅ Defense-in-depth: Both L4 (TLS) and L7 (Authorino) validation
- ✅ Cryptographic proof of private key possession
- ✅ Standard Gateway API configuration
- ✅ XFCC header automatically set by gateway after successful TLS validation
- ✅ Incoming XFCC headers automatically stripped to prevent spoofing

**Gateway configuration:**

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: mtls-gateway
  namespace: gateway-system
spec:
  gatewayClassName: istio
  listeners:
  - name: https
    protocol: HTTPS
    port: 443
    hostname: "*.example.com"
    tls:
      mode: Terminate
      certificateRefs:
      - name: gateway-tls-cert
  # Frontend TLS validation for client certificates
  tls:
    frontend:
      default:
        validation:
          # Reference ConfigMap(s) with trusted CA certificates
          caCertificateRefs:
          - name: client-ca-bundle
            kind: ConfigMap
          # Require valid client certificates
          mode: AllowValidOnly  # or AllowInsecureFallback for optional mTLS
```

**AuthPolicy configuration:**

```yaml
apiVersion: kuadrant.io/v1
kind: AuthPolicy
metadata:
  name: x509-auth
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: protected-api
  rules:
    authentication:
      "client-certificate":
        x509:
          # Extract certificate from XFCC header
          source:
            xfccHeader: "x-forwarded-client-cert" # cert in envoy XFCC text format, extracted from the header
          # Select trusted CA certificates using labels
          selector:
            matchLabels:
              app.kubernetes.io/name: trusted-client
```

See the [X.509 authentication user guides](../user-guides/auth/x509-authentication.md) for complete working examples for each tier.

### Tier 2: Provider-specific resources (Alternative for older Gateway API versions)

**Use Tier 2 when:** You don't have Gateway API v1.5+ but still want defense-in-depth security with L4 TLS validation.

Tier 2 uses provider-specific resources to configure TLS validation at the gateway level:
- **Istio**: EnvoyFilter resources
- **Envoy Gateway**: EnvoyPatchPolicy resources

**Security characteristics:**
- ✅ Defense-in-depth: Both L4 (TLS) and L7 (Authorino) validation
- ✅ Cryptographic proof of private key possession
- ✅ XFCC header set by gateway after successful TLS validation
- ⚠️ Requires provider-specific configuration knowledge
- ⚠️ Configuration varies by gateway provider

**Trade-offs:**
- More complex configuration compared to Tier 1
- Provider-specific knowledge required
- Same security guarantees as Tier 1

**When to migrate to Tier 1:** When you upgrade to Gateway API v1.5+ and your gateway provider supports frontend TLS validation, migrate from Tier 2 to Tier 1 for standardized configuration.

### Tier 3: Certificate in request header only (Exceptional cases)

**Use Tier 3 only when:** Gateway-level TLS validation is not feasible, and you have a trusted upstream proxy that performs TLS validation and populates the XFCC or Client-Cert header.

Tier 3 extracts certificates from request headers without gateway-level TLS validation. Authorino becomes the sole certificate validator.

**Security characteristics:**
- ❌ No defense-in-depth (single validation point at L7)
- ❌ No cryptographic proof of private key possession
- ❌ Potential spoofing risk if headers originate from untrusted sources
- ✅ Still validates certificate chain against trusted CAs
- ✅ Supports multi-CA trust via label selectors

**When Tier 3 is acceptable:**
- Certificate header originates from a trusted upstream proxy that performed TLS validation
- Network topology prevents direct client access to the gateway
- You understand and accept the security trade-offs

**AuthPolicy configuration with Client-Cert header (RFC 9440):**

```yaml
apiVersion: kuadrant.io/v1
kind: AuthPolicy
metadata:
  name: x509-header-only
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: api-route
  rules:
    authentication:
      "client-cert-header":
        x509:
          # Extract certificate from RFC 9440 Client-Cert header
          source:
            clientCertHeader: "client-cert" # cert in der format, base64-encoded, extracted from the header
          selector:
            matchLabels:
              tier: trusted-upstream
```

> [!WARNING] Important
> Use Tier 1 or Tier 2 whenever possible. Only use Tier 3 in exceptional cases where gateway-level TLS validation is not feasible and you have a trusted upstream proxy.

## Certificate source options

The `x509.source` field specifies where to extract the client certificate from:

| Source | Value | When to use | Security |
|--------|--------|-------------|----------|
| **`xfccHeader`** | Name of the Envoy `X-Forwarded-Client-Cert` header | Istio, Envoy Gateway (Tier 1 or 2) | ✅ Set by gateway after TLS validation |
| **`clientCertHeader`** | Name of the RFC 9440 `Client-Cert` header | Trusted upstream proxy implementing RFC 9440 (Tier 3) | ⚠️ Requires trusted source |
| **`expression`** | CEL expression that evaluates to a certificate | Custom header names or attribute paths (Tier 3) | ⚠️ Requires trusted source |

**Example with custom CEL expression:**

```yaml
x509:
  source:
    expression: 'request.http.headers["x-custom-client-cert"]' # cert in pem format, URL-encoded, extracted from a custom header using CEL
  selector:
    matchLabels:
      app: my-service
```

## Multi-CA trust with label selectors

Unlike traditional proxy-based client certificate validation that supports only a single CA bundle, Kuadrant's X.509 authentication allows you to trust multiple CAs and select different CA sets for different routes using Kubernetes label selectors.

**Example - Trust CA certificates labeled with specific environment:**

```yaml
x509:
  selector:
    matchLabels:
      environment: production
      tier: customer-facing
```

**Example - More complex selectors:**

```yaml
x509:
  selector:
    matchExpressions:
    - key: ca-tier
      operator: In
      values: ["tier-1", "tier-2"]
    - key: deprecated
      operator: DoesNotExist
```

This enables sophisticated trust models such as:
- Different CAs for production vs. staging environments
- Separate CAs for different business units or customers
- Gradual CA rotation by trusting both old and new CAs during migration
- Per-route CA trust policies for multi-tenant scenarios

**CA Secret requirements:**

Trusted CA certificates must be stored in Kubernetes TLS secrets in the Kuadrant namespace with appropriate labels:

```bash
kubectl create secret generic customer-a-ca \
  -n kuadrant-system \
  --from-file=ca.crt=customer-a-ca.crt

kubectl label secret customer-a-ca \
  -n kuadrant-system \
  authorino.kuadrant.io/managed-by=authorino \
  customer=customer-a \
  environment=production
```

> [!IMPORTANT] Cross-namespace trust
> By default, Authorino only trusts CA secrets in the Kuadrant namespace. To allow cross-namespace references, set `allNamespaces: true` in the x509 spec (`rules.authentication.x509.allNamespaces`), but use this with caution as it expands the trust scope.

## Certificate requirements and constraints

### Required certificate attributes

Client certificates must meet these requirements for Authorino validation to succeed:

1. **Extended Key Usage (EKU)**: Certificates must include the x509 v3 extension specifying **Client Authentication** (`1.3.6.1.5.5.7.3.2`) extended key usage

   **Example certificate generation with Client Auth EKU:**
   ```bash
   # Create X.509 v3 extensions file
   cat > client-cert-ext.cnf << EOF
   authorityKeyIdentifier=keyid,issuer
   basicConstraints=CA:FALSE
   keyUsage=digitalSignature,nonRepudiation,keyEncipherment,dataEncipherment
   extendedKeyUsage=clientAuth
   EOF

   # Generate certificate with Client Auth EKU
   openssl x509 -req -sha256 \
     -days 365 \
     -CA ca.crt -CAkey ca.key \
     -extfile client-cert-ext.cnf \
     -in client.csr -out client.crt
   ```

2. **Valid certificate chain**: Certificate must chain to a trusted CA configured in Authorino secrets

3. **Not expired**: Certificate must be within its validity period

4. **Proper encoding**:
   - XFCC header: [Envoy XFCC text format](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_conn_man/headers#text-format)
   - Client-Cert header ([RFC 9440](https://datatracker.ietf.org/doc/rfc9440/)): DER format, base64-encoded
   - CEL Expression: PEM-encoded, URL-encoded

### Certificate identity object

When X.509 authentication succeeds, the identity object (`auth.identity`) contains the certificate subject fields available for use in authorization rules and response transformations:

| Field | Type | OID | Description | Example |
|-------|------|-----|-------------|---------|
| `CommonName` | string | 2.5.4.3 | Common Name (CN) | `"api-client.example.com"` |
| `Country` | array of strings | 2.5.4.6 | Country (C) | `["US"]` |
| `Organization` | array of strings | 2.5.4.10 | Organization (O) | `["ACME Corp"]` |
| `OrganizationalUnit` | array of strings | 2.5.4.11 | Organizational Unit (OU) | `["Engineering", "Platform"]` |
| `Locality` | array of strings | 2.5.4.7 | Locality (L) | `["San Francisco"]` |
| `Province` | array of strings | 2.5.4.8 | Province/State (ST) | `["California"]` |
| `StreetAddress` | array of strings | 2.5.4.9 | Street Address | `["123 Main St"]` |
| `PostalCode` | array of strings | 2.5.4.17 | Postal Code | `["94105"]` |
| `SerialNumber` | string | 2.5.4.5 | Subject serial number | `"12345"` |

**Example - Use certificate attributes in authorization:**

```yaml
authorization:
  "verify-organization":
    patternMatching:
      patterns:
      - predicate: "auth.identity.Organization[0] == 'ACME Corp'"
      - predicate: "'Engineering' in auth.identity.OrganizationalUnit"
```

**Example - Inject certificate attributes into request headers:**

```yaml
response:
  success:
    headers:
      "x-client-cn":
        plain:
          expression: auth.identity.CommonName
      "x-client-org":
        plain:
          expression: auth.identity.Organization[0]
      "x-client-subject":
        json:
          properties:
            cn:
              expression: auth.identity.CommonName
            org:
              expression: auth.identity.Organization[0]
            ou:
              expression: auth.identity.OrganizationalUnit
```

## Security considerations

### XFCC header security

**Critical requirement:** The XFCC header must originate from a trusted source to prevent spoofing.

#### Tier 1 & 2: XFCC set by gateway after validation ✅

With gateway-level TLS validation, the proxy must be configured to:

1. **Strip incoming XFCC headers**: Any XFCC from incoming requests must be removed to prevent client spoofing
2. **Set XFCC only after validation**: The proxy sets the XFCC header exclusively after successful TLS validation

Envoy behavior with `forward_client_cert_details: SANITIZE_SET` (recommended):
- Strips any incoming XFCC headers from untrusted clients
- Sets the XFCC header only after TLS validation succeeds
- Provides cryptographic proof that the client possesses the private key

**Verification:** Ensure your Gateway configuration uses the recommended Envoy settings. For Tier 1 (Gateway API v1.5+), this is automatic. For Tier 2, verify your EnvoyFilter or provider-specific resource configuration.

#### Tier 3: XFCC forwarded from client ⚠️

With Tier 3 configuration, the proxy forwards the XFCC or Client-Cert header from the incoming request without validation.

**Risks:**
- Potential client spoofing of the certificate header
- No cryptographic proof of private key possession
- Single validation point (Authorino only)

**Acceptable scenarios:**
- Certificate header originates from a trusted upstream proxy that performed TLS validation
- Network topology prevents direct client access to the gateway
- You understand and accept the L7-only validation trade-offs

> [!WARNING] Warning
> Do not use Tier 3 configuration unless you trust the source of the certificate header and understand the security implications. When in doubt, use Tier 1 or Tier 2.

### CA certificate management

Proper CA certificate management is critical for maintaining security:

#### Gateway-level CA configuration (Tier 1 & 2)

**Responsibilities:**
- Securely manage CA certificates for gateway validation
- **Tier 1**: ConfigMaps referenced in Gateway `spec.tls.frontend.default.validation.caCertificateRefs`
- **Tier 2**: CA certificates in EnvoyFilter or provider-specific resources
- Certificate rotation requires ConfigMap updates and potential gateway pod restarts
- Namespace isolation through ReferenceGrant controls cross-namespace references

#### Authorino-level CA configuration (All tiers)

**Responsibilities:**
- Manage CA certificates in Kubernetes TLS secrets with appropriate labels
- Rotation requires updating secrets (Authorino watches for changes)
- Multi-CA trust via label selectors enables fine-grained revocation control
- Secrets must be in the Kuadrant namespace preferably (or use `allNamespaces: true` with caution)

**Key distinction:** Gateway CAs typically represent a single root CA validated at TLS handshake, while Authorino CAs can represent multiple intermediate CAs selected via labels, enabling granular trust management.

### Best practices

1. **Use Tier 1 whenever possible**: Gateway API v1.5+ frontend TLS validation provides standardized, secure configuration

2. **Automate certificate management**: Use [cert-manager](https://cert-manager.io/) for automatic CA certificate provisioning and rotation at both gateway and Authorino levels

3. **Implement certificate rotation strategies**:
   - Overlap periods where both old and new CAs are trusted
   - Use label selectors to gradually phase out old CAs
   - Monitor certificate expiration with alerting

4. **Monitor certificate health**:
   - Track certificate expiration timelines
   - Alert on approaching expiration (30, 14, 7 days)
   - Monitor authentication failure rates

5. **Consider separate CAs for different trust levels**:
   - Gateway-level: Broad trust (all certificates from organizational CA)
   - Authorino-level: Granular trust (specific CAs per route/service)

6. **Validate Client Auth EKU**: Ensure all client certificates include the `extendedKeyUsage=clientAuth` extension

7. **Header size considerations**:
   - Certificate chains in XFCC headers can exceed typical header limits
   - Consider using shorter certificate chains when possible
   - Monitor for header size-related failures

8. **Network segmentation**: For Tier 3 configurations, ensure network topology prevents direct client access to the gateway

## Troubleshooting

### Authentication fails with "invalid certificate"

**Possible causes:**
- Certificate doesn't chain to a trusted CA
- Certificate is expired
- Certificate doesn't have Client Authentication EKU
- CA secret not labeled correctly or not in Kuadrant namespace

**Resolution:**
```bash
# Verify certificate has Client Auth EKU
openssl x509 -in client.crt -noout -text | grep "TLS Web Client Authentication"

# Check certificate validity
openssl x509 -in client.crt -noout -dates

# Verify CA secret exists and has correct labels
kubectl get secret -n kuadrant-system -l authorino.kuadrant.io/managed-by=authorino

# Check Authorino logs
kubectl logs -n kuadrant-system -l authorino-resource-uid=<uid> | grep x509
```

### XFCC header not populated

**Possible causes:**
- Gateway not configured for frontend TLS validation (Tier 1)
- EnvoyFilter not applied correctly (Tier 2)
- Client didn't present certificate during TLS handshake

**Resolution:**
```bash
# For Tier 1: Check Gateway configuration
kubectl get gateway <gateway-name> -o yaml | grep -A 10 frontend

# Check if XFCC is in request (from Authorino perspective)
kubectl logs -n kuadrant-system -l authorino-resource-uid=<uid> | grep -i xfcc

# Test TLS handshake
openssl s_client -connect gateway.example.com:443 \
  -cert client.crt -key client.key -CAfile ca.crt
```

### Multi-CA trust not working

**Possible causes:**
- CA secrets don't have required labels
- Label selectors don't match any CA secrets
- Secrets in wrong namespace

**Resolution:**
```bash
# List CA secrets with labels
kubectl get secrets -n kuadrant-system \
  -l authorino.kuadrant.io/managed-by=authorino \
  --show-labels

# Verify selector matches secrets
kubectl get secrets -n kuadrant-system \
  -l app.kubernetes.io/name=trusted-client

# Check AuthPolicy selector configuration
kubectl get authpolicy <policy-name> -o yaml | grep -A 5 selector
```

## See also

- [X.509 authentication user guides](../user-guides/auth/x509-authentication.md) - Choose your tier and follow step-by-step guides
  - [Tier 1: Gateway API v1.5+](../user-guides/auth/x509-tier1-gateway-api-validation.md) - Recommended approach
  - [Tier 2: Provider-specific](../user-guides/auth/x509-tier2-provider-specific.md) - Alternative for older Gateway API
  - [Tier 3: Header-only](../user-guides/auth/x509-tier3-header-only.md) - Exceptional cases
- [Authentication overview](auth.md#choosing-the-right-authentication-method) - Comparison of all authentication methods
- [AuthPolicy API reference](../reference/authpolicy.md) - Complete AuthPolicy CRD specification
- [RFC 0015: X.509 Client Certificate Authentication](https://github.com/Kuadrant/architecture/blob/main/rfcs/0015-x509-client-cert-authpolicy.md) - Design document and architectural decisions
- [Authorino X.509 authentication](https://docs.kuadrant.io/latest/authorino/docs/features/#x509-client-certificate-authentication-authenticationx509) - Detailed Authorino feature documentation
- [Gateway API v1.5 TLS Frontend Validation](https://gateway-api.sigs.k8s.io/api-types/gateway/#gateway-api-v1-TLSFrontendValidation) - Gateway API specification
- [Envoy XFCC header specification](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_conn_man/headers#x-forwarded-client-cert) - Envoy documentation on XFCC header format and usage
- [RFC 9440: Client-Cert HTTP Header Field](https://www.rfc-editor.org/rfc/rfc9440.html) - Client-Cert header specification
