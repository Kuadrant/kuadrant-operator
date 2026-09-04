# AccessPolicy Extension

The **AccessPolicy** extension provides granular, protocol-aware authorization for agentic traffic (such as Agent-to-Agent or Agent-to-Tool communication using protocols like Model Context Protocol - MCP) on Kuadrant-managed Gateways.

It implements the experimental specification defined by [kubernetes-sigs/kube-agentic-networking](https://github.com/kubernetes-sigs/kube-agentic-networking).

---

## Overview

Traditional network policies operate at Layer 3/4, and standard HTTP authorization policies operate primarily on HTTP paths and methods. In agentic AI architectures, AI agents invoke tools, read resources, and execute tasks via application-level RPC protocols (e.g. JSON-RPC over MCP).

The `AccessPolicy` extension allows operators to enforce identity-based access controls (`ServiceAccount` or `SPIFFE` identities) down to specific agentic operations (e.g., restricting an AI agent to only invoke `tools/call` for specific tools like `get-sum`).

---

## How It Works

### Integration Architecture

1. **AccessPolicy**: Entry point for defining source identities and authorization rules for target Gateways.
2. **Extension Reconciler**: Subscribes to Gateway and AccessPolicy updates via Kuadrant's Extension SDK.
3. **AuthPolicy Generation**: Translates `AccessPolicy` rules into managed Authorino `AuthPolicy` resources targeting the Gateway.
4. **Data Plane Enforcement**: Authorino evaluates identity (via Kubernetes TokenReview or SPIFFE client certificates) and enforces CEL/OPA authorization logic on inbound requests.

```
+-------------------+       targets        +-------------------+
|   AccessPolicy    | -------------------> |    Gateway API    |
+-------------------+                      |      Gateway      |
          |                                +-------------------+
          | reconciles & translates                  ^
          v                                          | targets
+-------------------+                                |
| Kuadrant AuthPolicy| ------------------------------+
+-------------------+
```

---

## AccessPolicy Custom Resource

### Spec Field Overview

- **`targetRefs`** (*required*): Array of target references. Currently supports targeting `Gateway` resources (`gateway.networking.k8s.io`).
- **`action`** (*required*): Action to take when rules match: `Allow` or `ExternalAuth`.
- **`externalAuth`** (*optional*): External authorization filter configuration. (Execution is deferred; marked unsupported if specified).
- **`rules`** (*optional*): Array of authorization rules (`AccessRule`).

### AccessRule Fields

- **`name`** (*required*): Unique name for the rule.
- **`source`** (*optional*): Source identity criteria:
  - `type: ServiceAccount`: Matches Kubernetes Service Accounts (`system:serviceaccount:<namespace>:<name>`).
  - `type: SPIFFE`: Matches SPIFFE IDs (`spiffe://<trust_domain>/<workload>`).
- **`authorization`** (*optional*): Protocol or CEL matching criteria:
  - `type: Inline`: Inline matching for MCP attributes (`methods`, `mcpBaseProtocolMethodsOption`) and HTTP attributes (`methods`, `paths`, `headers`, `hosts`, `ports`).
  - `type: CEL`: Custom Common Expression Language rule evaluated against the request.

---

## Examples

### 1. Service Account Authentication with MCP Tool Restriction

Allow a specific Kubernetes Service Account (`agent-sa` in `default` namespace) to execute only the `get-sum` tool via MCP `tools/call`:

```yaml
apiVersion: extensions.kuadrant.io/v1alpha1
kind: AccessPolicy
metadata:
  name: mcp-tool-access-policy
  namespace: default
spec:
  targetRefs:
    - group: gateway.networking.k8s.io
      kind: Gateway
      name: agent-gateway
  action: Allow
  rules:
    - name: allow-calculator-agent
      source:
        type: ServiceAccount
        serviceAccount:
          namespace: default
          name: agent-sa
      authorization:
        type: Inline
        mcp:
          mcpBaseProtocolMethodsOption: MATCH_BASE_PROTOCOL_METHODS
          methods:
            - name: tools/call
              params:
                - get-sum
```

### 2. SPIFFE Identity Authentication with Custom CEL Expression

Allow a workload with a specific SPIFFE identity to invoke tools matching a CEL condition:

```yaml
apiVersion: extensions.kuadrant.io/v1alpha1
kind: AccessPolicy
metadata:
  name: spiffe-access-policy
  namespace: default
spec:
  targetRefs:
    - group: gateway.networking.k8s.io
      kind: Gateway
      name: agent-gateway
  action: Allow
  rules:
    - name: allow-spiffe-workload
      source:
        type: SPIFFE
        spiffe: spiffe://cluster.local/ns/default/sa/trusted-agent
      authorization:
        type: CEL
        cel:
          expression: "request.mcp.tool_name in ['get-sum', 'query-db']"
```

---

## Prerequisites

Before using `AccessPolicy`:
1. **Kuadrant Operator** installed with extensions enabled.
2. **Authorino Operator** deployed in the cluster for authentication/authorization reconciliation.
3. Gateway API `Gateway` resource created and managed by Kuadrant.

---

## Scope & Known Limitations

- **Target Reference**: Currently supports targeting Gateway API `Gateway` resources. Target references for `XBackend` are out of scope.
- **ExternalAuth Action**: The `ExternalAuth` spec field is supported in the API schema for upstream compatibility, but execution is deferred to a future release (`Accepted=False` condition set if specified).
- **Default Policy Behavior**: A fail-close rule (`allow = false`) is automatically appended to enforce deny-by-default for unmatched traffic.
