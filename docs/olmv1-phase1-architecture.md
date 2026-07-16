# OLMv1 Phase 1 — Umbrella Operator Architecture

## Key

| Term | Meaning |
|------|---------|
| **SSA** | Server-Side Apply — Kubernetes apply method where the server tracks field ownership per manager |
| **CRB** | ClusterRoleBinding |
| **SA** | ServiceAccount |
| **CRD** | CustomResourceDefinition |
| **bind/escalate** | RBAC verbs that allow creating ClusterRoleBindings to named ClusterRoles without holding all their permissions |
| **GITREF** | Git reference (branch, tag, or SHA) used to pull charts from upstream repos |
| **Wrapper CR** | Authorino/Limitador custom resources created by kuadrant-operator and reconciled by child operators |

## Build-Time: Chart Sync and Packaging

```mermaid
graph LR
    subgraph upstream["Upstream Repos"]
        AO["Kuadrant/authorino-operator"]
        LO["Kuadrant/limitador-operator"]
        DO["Kuadrant/dns-operator"]
        MG["Kuadrant/mcp-gateway"]
    end

    SYNC["make sync-child-operator-charts<br/>Go tool: hack/sync-child-charts/"]

    AO -->|GITREF| SYNC
    LO -->|GITREF| SYNC
    DO -->|GITREF| SYNC
    MG -->|GITREF| SYNC

    subgraph local["config/child-operators/"]
        subgraph crds["crds/"]
            ao_c["authorino-operator.yaml"]
            lo_c["limitador-operator.yaml"]
            do_c["dns-operator.yaml"]
            mg_c["mcp-gateway.yaml"]
        end
        subgraph rbac["rbac/"]
            ao_r["authorino-operator.yaml"]
            lo_r["limitador-operator.yaml"]
            do_r["dns-operator.yaml"]
            mg_r["mcp-gateway.yaml"]
        end
        subgraph charts["charts/"]
            ao_t["authorino-operator/"]
            lo_t["limitador-operator/"]
            do_t["dns-operator/"]
            mg_t["mcp-gateway/"]
        end
    end

    SYNC --> crds
    SYNC --> rbac
    SYNC --> charts
```

```mermaid
graph LR
    subgraph local["config/child-operators/"]
        CRDs["crds/"]
        RBAC["rbac/"]
        CHARTS["charts/"]
    end

    CRDs -->|"make bundle"| BUNDLE["OLM Bundle"]
    RBAC -->|"make bundle"| BUNDLE
    CRDs -->|"make helm-build"| HELM["Helm Chart"]
    RBAC -->|"make helm-build"| HELM
    CHARTS -->|"copied to /charts/<br/>in container image"| RUNTIME["Helm renderer<br/>in operator"]
```

## Cluster State After Installation (no Kuadrant CR)

```mermaid
graph TB
    subgraph operator["1 Operator Deployment"]
        KOP["kuadrant-operator"]
    end

    subgraph crds["CRDs (all installed by kuadrant-operator bundle/chart)"]
        K_CRD["Kuadrant, AuthPolicy<br/>RateLimitPolicy, DNSPolicy<br/>TLSPolicy"]
        A_CRD["Authorino, AuthConfig"]
        L_CRD["Limitador"]
        D_CRD["DNSRecord<br/>DNSHealthCheckProbe"]
        M_CRD["MCPGatewayExtension<br/>MCPServerRegistration<br/>MCPVirtualServer"]
    end

    subgraph rbac["RBAC"]
        K_RBAC["kuadrant-operator<br/>SA + ClusterRole + CRB"]
        CHILD_CR["Child operator ClusterRoles<br/>(no SA or CRB yet)"]
    end

    KOP -.->|"waiting for<br/>Kuadrant CR"| IDLE["No child operators running<br/>until user creates Kuadrant CR"]
```

## Runtime: Reconciliation Chain

```mermaid
graph TB
    USER["User"] -->|"creates"| KCR["Kuadrant CR"]
    KCR -->|"triggers"| KOP["kuadrant-operator"]

    KOP -->|"renders /charts/authorino-operator"| AO["authorino-operator<br/>Deployment + SA + CRB"]
    KOP -->|"renders /charts/limitador-operator"| LO["limitador-operator<br/>Deployment + SA + CRB"]
    KOP -->|"renders /charts/dns-operator"| DO["dns-operator<br/>Deployment + SA + CRB"]
    KOP -->|"renders /charts/mcp-gateway"| MG["mcp-gateway<br/>Deployment + SA + CRB"]
    KOP -->|"creates wrapper CR"| ACR["Authorino CR"]
    KOP -->|"creates wrapper CR"| LCR["Limitador CR"]

    AO -->|"reconciles"| ACR
    LO -->|"reconciles"| LCR

    ACR --> AW["Authorino Deployment"]
    LCR --> LW["Limitador Deployment"]
    MG --> MGW["MCP broker + router"]
```

## RBAC Model

```mermaid
graph LR
    subgraph sa["Service Accounts"]
        KSA["kuadrant-operator SA"]
        ASA["authorino-operator SA"]
        LSA["limitador-operator SA"]
        DSA["dns-operator SA"]
        MSA["mcp-gateway SA"]
    end

    subgraph roles["ClusterRoles"]
        KR["kuadrant-operator-manager<br/>infrastructure perms<br/>+ bind/escalate on child roles"]
        AR["authorino-operator-manager<br/>authorino-manager-role<br/>authorino-manager-k8s-auth-role"]
        LR_["limitador-operator-manager-role"]
        DR["dns-operator-manager-role<br/>dns-operator-remote-cluster-role"]
        MR["mcp-gateway-controller"]
    end

    subgraph who["Created by"]
        OLM_H["Helm or OLM"]
        KUADRANT["kuadrant-operator<br/>at runtime"]
    end

    OLM_H -->|"ClusterRoleBinding"| KSA
    KSA --> KR

    KUADRANT -->|"ClusterRoleBinding<br/>using bind/escalate"| ASA
    KUADRANT -->|"ClusterRoleBinding<br/>using bind/escalate"| LSA
    KUADRANT -->|"ClusterRoleBinding<br/>using bind/escalate"| DSA
    KUADRANT -->|"ClusterRoleBinding<br/>using bind/escalate"| MSA
    ASA --> AR
    LSA --> LR_
    DSA --> DR
    MSA --> MR
```

## Resource Ownership

```mermaid
graph TB
    USER["User"] -->|"creates"| KCR["Kuadrant CR"]
    USER -->|"creates"| POLICIES["AuthPolicy, RateLimitPolicy<br/>DNSPolicy, TLSPolicy"]

    subgraph installer["Installed by Helm or OLM"]
        CRDs["All CRDs"]
        CR["Component ClusterRoles"]
        KOP_DEP["kuadrant-operator Deployment"]
        KOP_RBAC["kuadrant-operator SA, ClusterRole, CRB"]
    end

    KCR -->|"ownerRef"| AUTH_OP["authorino-operator Deployment"]
    KCR -->|"ownerRef"| LIM_OP["limitador-operator Deployment"]
    KCR -->|"ownerRef"| DNS_OP["dns-operator Deployment"]
    KCR -->|"ownerRef"| MCP_OP["mcp-gateway Deployment"]
    KCR -->|"ownerRef"| AUTH_CR["Authorino CR"]
    KCR -->|"ownerRef"| LIM_CR["Limitador CR"]

    AUTH_CR -->|"ownerRef"| AUTH_WL["Authorino Deployment"]
    LIM_CR -->|"ownerRef"| LIM_WL["Limitador Deployment"]
```
