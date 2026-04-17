# RBAC for Gateway API Personas

This guide demonstrates how to configure Role-Based Access Control (RBAC) for different personas working with Kuadrant and Gateway API resources. It aligns with the [Gateway API roles and personas](https://gateway-api.sigs.k8s.io/concepts/roles-and-personas/) model.

## Overview

Gateway API defines three primary personas, each with distinct responsibilities:

1. **Infrastructure Provider (Ian)** - Manages infrastructure and platforms across multiple clusters
2. **Cluster Operator (Chihiro)** - Manages clusters, policies, and governance
3. **Application Developer (Ana)** - Manages applications and their configurations

This guide focuses on the **Cluster Operator** and **Application Developer** personas, as they directly interact with Kuadrant policies and resources.

## Understanding Policy Scope

Kuadrant policies (AuthPolicy, RateLimitPolicy, TokenRateLimitPolicy) can target different Gateway API resources:

- **Gateway-level policies**: Target a Gateway resource and affect all routes attached to that Gateway
  - Managed by Cluster Operators (cluster-wide governance)
  - Example: Rate limiting all traffic through a shared ingress gateway
  
- **Route-level policies**: Target HTTPRoute or GRPCRoute resources and affect only that specific route
  - Managed by Application Developers (application-specific configuration)
  - Example: Authentication requirements for a specific API endpoint

This separation enables:

- **Cluster Operators** to enforce baseline security and rate limiting across all applications using a Gateway
- **Application Developers** to add additional, route-specific policies for their applications
- Policy inheritance and merging when both Gateway and Route policies exist

## Personas and Their Kuadrant Responsibilities

### Infrastructure Provider

The Infrastructure Provider typically manages the underlying Kubernetes infrastructure, Gateway API CRDs, and gateway controller deployments. They generally do not directly manage Kuadrant resources but may install and operate the Kuadrant Operator itself.

**Kuadrant-related responsibilities:**

- Installing the Kuadrant Operator
- Managing operator deployments and configurations
- Setting up multi-cluster infrastructure (if applicable)

### Cluster Operator

The Cluster Operator manages cluster-level resources and establishes governance policies that affect multiple applications and tenants.

**Kuadrant-related responsibilities:**

- Managing Gateways (`gateways`) - creating and configuring shared ingress gateways
- Managing the Kuadrant instance (`kuadrants`)
- Managing infrastructure components:
  - Limitador instances (`limitadors`)
  - Authorino instances (`authorinos`)
- Managing DNS configuration:
  - DNS policies (`dnspolicies`)
  - Viewing DNS records (`dnsrecords`) - automatically created by the operator
  - DNS provider credentials (Secrets)
- Managing TLS policies (`tlspolicies`)
- Managing cert-manager resources (Issuers, ClusterIssuers)
- Managing Gateway-level policies (policies that target Gateways):
  - Authentication policies (`authpolicies`) targeting Gateways
  - Rate limiting policies (`ratelimitpolicies`) targeting Gateways
  - Token rate limiting policies (`tokenratelimitpolicies`) targeting Gateways
- Viewing and monitoring all policies and routes across namespaces

### Application Developer

The Application Developer manages application-level policies that control traffic behavior, authentication, and rate limiting for their specific applications.

**Kuadrant-related responsibilities:**

- Creating and managing application routes (`httproutes`, `grpcroutes`)
- Managing route-level policies (policies that target HTTPRoutes/GRPCRoutes):
  - Authentication policies (`authpolicies`) targeting HTTPRoutes
  - Rate limiting policies (`ratelimitpolicies`) targeting HTTPRoutes
  - Token rate limiting policies (`tokenratelimitpolicies`) targeting HTTPRoutes
- Managing extension policies:
  - OIDC policies (`oidcpolicies`)
  - Plan policies (`planpolicies`)
  - Telemetry policies (`telemetrypolicies`)
  - Threat policies (`threatpolicies`)
- Attaching routes to shared Gateways (managed by Cluster Operators)
- Viewing Gateways and cluster-level policies (read-only)

## RBAC Configuration

### Cluster Operator Roles

The Cluster Operator needs permissions to manage infrastructure-level Kuadrant resources.

#### ClusterRole for Kuadrant Infrastructure Management

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: kuadrant-cluster-operator
rules:
# Kuadrant instance management
- apiGroups:
  - kuadrant.io
  resources:
  - kuadrants
  verbs:
  - create
  - delete
  - get
  - list
  - patch
  - update
  - watch
- apiGroups:
  - kuadrant.io
  resources:
  - kuadrants/status
  verbs:
  - get
  - watch

# Limitador instance management
- apiGroups:
  - limitador.kuadrant.io
  resources:
  - limitadors
  verbs:
  - create
  - delete
  - get
  - list
  - patch
  - update
  - watch
- apiGroups:
  - limitador.kuadrant.io
  resources:
  - limitadors/status
  verbs:
  - get
  - watch

# Authorino instance management
- apiGroups:
  - operator.authorino.kuadrant.io
  resources:
  - authorinos
  verbs:
  - create
  - delete
  - get
  - list
  - patch
  - update
  - watch
- apiGroups:
  - operator.authorino.kuadrant.io
  resources:
  - authorinos/status
  verbs:
  - get
  - watch

# DNS policy management
- apiGroups:
  - kuadrant.io
  resources:
  - dnspolicies
  verbs:
  - create
  - delete
  - get
  - list
  - patch
  - update
  - watch
- apiGroups:
  - kuadrant.io
  resources:
  - dnspolicies/status
  verbs:
  - get
  - watch

# View DNS records (created automatically by DNSPolicy controller)
- apiGroups:
  - kuadrant.io
  resources:
  - dnsrecords
  verbs:
  - get
  - list
  - watch
- apiGroups:
  - kuadrant.io
  resources:
  - dnsrecords/status
  verbs:
  - get
  - watch

# TLS policy management
- apiGroups:
  - kuadrant.io
  resources:
  - tlspolicies
  verbs:
  - create
  - delete
  - get
  - list
  - patch
  - update
  - watch
- apiGroups:
  - kuadrant.io
  resources:
  - tlspolicies/status
  verbs:
  - get
  - watch

# View cert-manager resources
- apiGroups:
  - cert-manager.io
  resources:
  - certificates
  - issuers
  - clusterissuers
  verbs:
  - get
  - list
  - watch

# Gateway management
- apiGroups:
  - gateway.networking.k8s.io
  resources:
  - gateways
  verbs:
  - create
  - delete
  - get
  - list
  - patch
  - update
  - watch
- apiGroups:
  - gateway.networking.k8s.io
  resources:
  - gateways/status
  verbs:
  - get
  - watch

# View Gateway API resources (managed by others)
- apiGroups:
  - gateway.networking.k8s.io
  resources:
  - gatewayclasses  # Managed by Infrastructure Providers
  - httproutes      # Managed by Application Developers
  - grpcroutes      # Managed by Application Developers
  verbs:
  - get
  - list
  - watch

# Gateway-level policy management (policies targeting Gateways)
- apiGroups:
  - kuadrant.io
  resources:
  - authpolicies
  - ratelimitpolicies
  - tokenratelimitpolicies
  verbs:
  - create
  - delete
  - get
  - list
  - patch
  - update
  - watch
- apiGroups:
  - kuadrant.io
  resources:
  - authpolicies/status
  - ratelimitpolicies/status
  - tokenratelimitpolicies/status
  verbs:
  - get
  - watch

# View extension policies (typically managed by Application Developers)
- apiGroups:
  - extensions.kuadrant.io
  resources:
  - oidcpolicies
  - planpolicies
  - telemetrypolicies
  - threatpolicies
  verbs:
  - get
  - list
  - watch
- apiGroups:
  - extensions.kuadrant.io
  resources:
  - oidcpolicies/status
  - planpolicies/status
  - telemetrypolicies/status
  - threatpolicies/status
  verbs:
  - get
  - watch
```

#### Example: Grant Cluster Operator Permissions

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: chihiro-cluster-operator
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: kuadrant-cluster-operator
subjects:
- kind: User
  name: chihiro@example.com
  apiGroup: rbac.authorization.k8s.io
# Or for a group
- kind: Group
  name: cluster-operators
  apiGroup: rbac.authorization.k8s.io
```

With these permissions, Cluster Operators can:

- Create and manage shared Gateways for multiple applications
- Deploy and manage Kuadrant infrastructure (Kuadrant CR, Limitador, Authorino)
- Configure DNS and TLS for Gateways
- Create Gateway-level policies (AuthPolicy, RateLimitPolicy targeting Gateways)
- Monitor all policies and routes across the cluster

### Application Developer Roles

The Application Developer needs permissions to manage application-level policies within their namespace(s).

#### ClusterRole for Application Policy Management

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: kuadrant-application-developer
rules:
# Authentication policy management
- apiGroups:
  - kuadrant.io
  resources:
  - authpolicies
  verbs:
  - create
  - delete
  - get
  - list
  - patch
  - update
  - watch
- apiGroups:
  - kuadrant.io
  resources:
  - authpolicies/status
  verbs:
  - get
  - watch

# Rate limiting policy management
- apiGroups:
  - kuadrant.io
  resources:
  - ratelimitpolicies
  verbs:
  - create
  - delete
  - get
  - list
  - patch
  - update
  - watch
- apiGroups:
  - kuadrant.io
  resources:
  - ratelimitpolicies/status
  verbs:
  - get
  - watch

# Token rate limiting policy management
- apiGroups:
  - kuadrant.io
  resources:
  - tokenratelimitpolicies
  verbs:
  - create
  - delete
  - get
  - list
  - patch
  - update
  - watch
- apiGroups:
  - kuadrant.io
  resources:
  - tokenratelimitpolicies/status
  verbs:
  - get
  - watch

# Extension policies management
- apiGroups:
  - extensions.kuadrant.io
  resources:
  - oidcpolicies
  - planpolicies
  - telemetrypolicies
  - threatpolicies
  verbs:
  - create
  - delete
  - get
  - list
  - patch
  - update
  - watch
- apiGroups:
  - extensions.kuadrant.io
  resources:
  - oidcpolicies/status
  - planpolicies/status
  - telemetrypolicies/status
  - threatpolicies/status
  verbs:
  - get
  - watch

# HTTPRoute and GRPCRoute management
- apiGroups:
  - gateway.networking.k8s.io
  resources:
  - httproutes
  - grpcroutes
  verbs:
  - create
  - delete
  - get
  - list
  - patch
  - update
  - watch
- apiGroups:
  - gateway.networking.k8s.io
  resources:
  - httproutes/status
  - grpcroutes/status
  verbs:
  - get
  - watch

# View Gateways (managed by Cluster Operators)
- apiGroups:
  - gateway.networking.k8s.io
  resources:
  - gateways
  verbs:
  - get
  - list
  - watch

# View Kuadrant instances
- apiGroups:
  - kuadrant.io
  resources:
  - kuadrants
  verbs:
  - get
  - list
  - watch
```

#### Example: Grant Application Developer Permissions (Namespace-Scoped)

For namespace-scoped permissions:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: ana-application-developer
  namespace: my-application
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: kuadrant-application-developer
subjects:
- kind: User
  name: ana@example.com
  apiGroup: rbac.authorization.k8s.io
# Or for a service account
- kind: ServiceAccount
  name: app-deployment-sa
  namespace: my-application
```

For cluster-wide permissions (multi-namespace applications):

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: platform-team-application-developer
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: kuadrant-application-developer
subjects:
- kind: Group
  name: platform-team
  apiGroup: rbac.authorization.k8s.io
```

## Viewer Roles

For read-only access (useful for monitoring, debugging, or auditing):

### Kuadrant Viewer

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: kuadrant-viewer
rules:
# View all Kuadrant resources
- apiGroups:
  - kuadrant.io
  resources:
  - kuadrants
  - dnspolicies
  - dnsrecords
  - tlspolicies
  - authpolicies
  - ratelimitpolicies
  - tokenratelimitpolicies
  verbs:
  - get
  - list
  - watch
- apiGroups:
  - kuadrant.io
  resources:
  - kuadrants/status
  - dnspolicies/status
  - dnsrecords/status
  - tlspolicies/status
  - authpolicies/status
  - ratelimitpolicies/status
  - tokenratelimitpolicies/status
  verbs:
  - get
  - watch

# View infrastructure components
- apiGroups:
  - limitador.kuadrant.io
  resources:
  - limitadors
  verbs:
  - get
  - list
  - watch
- apiGroups:
  - operator.authorino.kuadrant.io
  resources:
  - authorinos
  verbs:
  - get
  - list
  - watch

# View extension policies
- apiGroups:
  - extensions.kuadrant.io
  resources:
  - oidcpolicies
  - planpolicies
  - telemetrypolicies
  - threatpolicies
  verbs:
  - get
  - list
  - watch
- apiGroups:
  - extensions.kuadrant.io
  resources:
  - oidcpolicies/status
  - planpolicies/status
  - telemetrypolicies/status
  - threatpolicies/status
  verbs:
  - get
  - watch

# View Gateway API resources
- apiGroups:
  - gateway.networking.k8s.io
  resources:
  - gateways
  - gatewayclasses
  - httproutes
  - grpcroutes
  verbs:
  - get
  - list
  - watch
```

## Practical Examples

### Example 1: Multi-Namespace Application Developer

An application developer manages multiple namespaces but should only manage policies in those namespaces:

```yaml
---
# Create the ClusterRole (if not already created)
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: kuadrant-application-developer
# ... (rules as defined above)

---
# Grant permissions for namespace: frontend
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: developer-team-policies
  namespace: frontend
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: kuadrant-application-developer
subjects:
- kind: Group
  name: frontend-developers
  apiGroup: rbac.authorization.k8s.io

---
# Grant permissions for namespace: backend
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: developer-team-policies
  namespace: backend
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: kuadrant-application-developer
subjects:
- kind: Group
  name: backend-developers
  apiGroup: rbac.authorization.k8s.io
```

### Example 2: Cluster Operator Creating and Managing a Gateway

A cluster operator creates a shared Gateway and enforces a baseline rate limit on all traffic:

```yaml
---
# Step 1: Cluster Operator creates a Gateway
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: shared-gateway
  namespace: gateway-system
spec:
  gatewayClassName: istio
  listeners:
  - name: http
    protocol: HTTP
    port: 80
    allowedRoutes:
      namespaces:
        from: All  # Allow routes from all namespaces

---
# Step 2: Cluster Operator creates a Gateway-level RateLimitPolicy
apiVersion: kuadrant.io/v1
kind: RateLimitPolicy
metadata:
  name: gateway-global-limit
  namespace: gateway-system
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: shared-gateway
  limits:
    "global":
      rates:
      - limit: 1000
        window: 1s
```

This setup provides:

- A shared Gateway that Application Developers can attach their HTTPRoutes to
- A baseline rate limit enforced on ALL traffic through this Gateway
- Application Developers can still add route-specific policies for additional controls

### Example 3: Cluster Operator Managing DNS

A cluster operator needs to configure DNS for multiple gateways across the cluster. First, create the DNS provider secret:

```yaml
---
# DNS provider secret (contains cloud credentials)
apiVersion: v1
kind: Secret
metadata:
  name: aws-credentials
  namespace: gateway-system
type: kuadrant.io/aws
stringData:
  AWS_ACCESS_KEY_ID: "AKIAIOSFODNN7EXAMPLE"
  AWS_SECRET_ACCESS_KEY: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
  AWS_REGION: "us-east-1"

---
# DNSPolicy targeting a Gateway
apiVersion: kuadrant.io/v1
kind: DNSPolicy
metadata:
  name: gateway-dns
  namespace: gateway-system
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: shared-gateway
  providerRefs:
    - name: aws-credentials
```

Grant Cluster Operator permissions:

```yaml
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: dns-admin
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: kuadrant-cluster-operator
subjects:
- kind: User
  name: dns-admin@example.com
  apiGroup: rbac.authorization.k8s.io
```

This user can now:

- Create and manage DNSPolicy resources
- View DNSRecord resources created by the operator
- Monitor DNS health and status across all gateways

**Note**: DNS provider secrets (containing cloud credentials) should be created by Infrastructure Providers. The Cluster Operator role does not include secret management permissions.

### Example 4: Application Developer Creating Routes and Policies

An Application Developer creates an HTTPRoute that attaches to the shared Gateway and adds route-specific policies:

```yaml
---
# Application Developer creates an HTTPRoute (attaches to Cluster Operator's Gateway)
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: my-app-route
  namespace: my-app
spec:
  parentRefs:
  - name: shared-gateway
    namespace: gateway-system
  hostnames:
  - myapp.example.com
  rules:
  - matches:
    - path:
        type: PathPrefix
        value: /api
    backendRefs:
    - name: my-app-service
      port: 8080

---
# Application Developer adds route-specific AuthPolicy
apiVersion: kuadrant.io/v1
kind: AuthPolicy
metadata:
  name: my-app-auth
  namespace: my-app
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: my-app-route
  rules:
    authentication:
      "jwt":
        jwt:
          issuerUrl: https://auth.example.com

---
# Application Developer adds route-specific RateLimitPolicy
apiVersion: kuadrant.io/v1
kind: RateLimitPolicy
metadata:
  name: my-app-ratelimit
  namespace: my-app
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: my-app-route
  limits:
    "per-user":
      rates:
      - limit: 100
        window: 1m
      counters:
      - expression: request.headers.user-id
```

Note: The Application Developer:

- Creates HTTPRoutes in their own namespace (`my-app`)
- Attaches routes to the shared Gateway created by the Cluster Operator
- Adds route-specific policies that layer on top of Gateway-level policies
- Cannot modify the Gateway or create Gateway-level policies

### Example 5: Service Account for CI/CD

A CI/CD pipeline needs to deploy applications with routes and policies:

```yaml
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: cicd-deployer
  namespace: my-app

---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: cicd-policy-management
  namespace: my-app
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: kuadrant-application-developer
subjects:
- kind: ServiceAccount
  name: cicd-deployer
  namespace: my-app
```

This service account can deploy HTTPRoutes, AuthPolicies, and RateLimitPolicies in the `my-app` namespace.

## Security Best Practices

### Principle of Least Privilege

- Grant only the minimum permissions necessary for each role
- Use namespace-scoped RoleBindings when possible instead of ClusterRoleBindings
- Separate cluster-level infrastructure management from application policy management

### Resource Management Separation

**Gateway Management:**

- **Cluster Operators**: Create and manage Gateways (shared infrastructure)
- **Application Developers**: Create HTTPRoutes/GRPCRoutes that attach to Gateways
- **Infrastructure Providers**: Manage GatewayClasses (provider configuration)

**Policy Attachment Restrictions:**

- Application Developers should NOT be able to create or modify Gateways
- Application Developers should NOT be able to create policies targeting Gateways (only HTTPRoutes/GRPCRoutes)
- Cluster Operators can create both Gateway-level and Route-level policies
- Use Gateway API's ReferenceGrant for cross-namespace route attachment

### Gateway-level vs Route-level Policies

**When to use Gateway-level policies (Cluster Operator):**

- Enforcing organization-wide security baselines
- Setting global rate limits for infrastructure protection
- Applying default authentication requirements for all services
- Managing DNS and TLS for the entire gateway

**When to use Route-level policies (Application Developer):**

- Implementing application-specific authentication flows
- Setting API endpoint-specific rate limits
- Applying route-specific authorization rules
- Adding telemetry or observability for specific services

**Policy merging:** When both Gateway and Route policies exist, Kuadrant merges them according to defined merge strategies, allowing Cluster Operators to set baselines while Application Developers add specifics.

### Audit and Monitoring

- Enable audit logging for policy changes
- Monitor who creates and modifies policies
- Set up alerts for unexpected policy changes
- Use the kuadrant-viewer role for security teams to audit configurations

## Related Resources

- [Gateway API Roles and Personas](https://gateway-api.sigs.k8s.io/concepts/roles-and-personas/)
- [Kubernetes RBAC Documentation](https://kubernetes.io/docs/reference/access-authn-authz/rbac/)

## Troubleshooting

### Permission Denied Errors

If a user receives "forbidden" errors when creating policies:

1. Verify the user has the correct ClusterRole or Role assigned
2. Check that the RoleBinding or ClusterRoleBinding references the correct subject
3. Ensure the namespace matches (for RoleBindings)
4. Use `kubectl auth can-i` to test permissions:

```bash
# Test if a user can create an AuthPolicy
kubectl auth can-i create authpolicies.kuadrant.io --as=ana@example.com -n my-namespace

# Test if a user can manage DNSPolicies
kubectl auth can-i create dnspolicies.kuadrant.io --as=chihiro@example.com
```
