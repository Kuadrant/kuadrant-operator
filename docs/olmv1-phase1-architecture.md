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

    SYNC["make sync-child-operator-charts"]

    AO -->|GITREF| SYNC
    LO -->|GITREF| SYNC
    DO -->|GITREF| SYNC
    MG -->|GITREF| SYNC

    subgraph local["charts/"]
        subgraph ao["authorino-operator/"]
            ao_c["crds/"] ~~~ ao_s["static/"] ~~~ ao_t["templates/"]
        end
        subgraph lo["limitador-operator/"]
            lo_c["crds/"] ~~~ lo_s["static/"] ~~~ lo_t["templates/"]
        end
        subgraph do["dns-operator/"]
            do_c["crds/"] ~~~ do_s["static/"] ~~~ do_t["templates/"]
        end
        subgraph mg["mcp-gateway/"]
            mg_c["crds/"] ~~~ mg_s["static/"] ~~~ mg_t["templates/"]
        end
    end

    SYNC --> ao
    SYNC --> lo
    SYNC --> do
    SYNC --> mg
```

```mermaid
graph LR
    subgraph local["Per child operator chart"]
        CRDs["crds/"]
        CR["static/"]
        TPL["templates/"]
    end

    subgraph deps["config/dependencies/<br/>child-operators/"]
        DEP["CRDs + ClusterRoles"]
    end

    CRDs --> DEP
    CR --> DEP

    DEP -->|"make bundle"| BUNDLE["OLM Bundle"]
    DEP -->|"make helm-build"| HELM["Helm Chart"]
    TPL -->|"used at runtime<br/>by operator"| RUNTIME["Helm renderer<br/>in operator"]
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

    KOP -->|"renders and applies"| AO["authorino-operator<br/>Deployment + SA + CRB"]
    KOP -->|"renders and applies"| LO["limitador-operator<br/>Deployment + SA + CRB"]
    KOP -->|"renders and applies"| DO["dns-operator<br/>Deployment + SA + CRB"]
    KOP -->|"renders and applies"| MG["mcp-gateway<br/>Deployment + SA + CRB"]
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
    subgraph helm_olm["Owned by Helm or OLM"]
        CRDs["All CRDs"]
        CR["Child operator ClusterRoles"]
        KOP["kuadrant-operator<br/>Deployment, SA, ClusterRole, CRB"]
    end

    subgraph kuadrant_op["Owned by kuadrant-operator (ownerRef → Kuadrant CR)"]
        CHILD["Per child operator:<br/>Deployment, SA, CRB,<br/>Roles, RoleBindings,<br/>ConfigMap, Service"]
        WRAPPER["Wrapper CRs<br/>Authorino CR, Limitador CR"]
    end

    subgraph child_op["Owned by child operators (ownerRef → wrapper CR)"]
        WORKLOAD["Workload resources<br/>Deployment, Service, ConfigMap"]
    end

    subgraph user["Owned by User"]
        KCR["Kuadrant CR"]
        POLICIES["Policies<br/>AuthPolicy, RateLimitPolicy<br/>DNSPolicy, TLSPolicy"]
    end
```
