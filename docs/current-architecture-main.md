# Current Architecture (main branch)

## Key

| Term | Meaning |
|------|---------|
| **OLM** | Operator Lifecycle Manager — installs and manages operators on OpenShift/Kubernetes |
| **dependencies.yaml** | OLM metadata declaring required child operator packages |
| **CRB** | ClusterRoleBinding |
| **SA** | ServiceAccount |
| **CRD** | CustomResourceDefinition |
| **Wrapper CR** | Authorino/Limitador custom resources created by kuadrant-operator and reconciled by child operators |

## Build-Time: Bundle and Chart Packaging

```mermaid
graph LR
    subgraph upstream["Upstream Repos"]
        AO["Kuadrant/authorino-operator"]
        LO["Kuadrant/limitador-operator"]
        DO["Kuadrant/dns-operator"]
    end

    subgraph bundle["OLM Bundle"]
        DEP["dependencies.yaml<br/>requires: authorino-operator<br/>requires: limitador-operator<br/>requires: dns-operator"]
        CSV["CSV + Kuadrant CRDs"]
    end

    subgraph catalog["OLM Catalog (4 packages)"]
        C_KOP["kuadrant-operator bundle"]
        C_AO["authorino-operator bundle"]
        C_LO["limitador-operator bundle"]
        C_DO["dns-operator bundle"]
    end

    subgraph helm["Helm Chart"]
        CHART["charts/kuadrant-operator/<br/>Chart.yaml"]
        H_DEP["dependencies:<br/>- authorino-operator<br/>- limitador-operator<br/>- dns-operator<br/>repository: kuadrant.io/helm-charts"]
    end

    AO -->|"bundle image"| C_AO
    LO -->|"bundle image"| C_LO
    DO -->|"bundle image"| C_DO
    CSV --> C_KOP

    CHART --> H_DEP
```

## Cluster State After Installation (no Kuadrant CR)

```mermaid
graph TB
    subgraph operators["4 Independent Operator Deployments"]
        KOP["kuadrant-operator"]
        AO["authorino-operator"]
        LO["limitador-operator"]
        DO["dns-operator"]
    end

    subgraph crds["CRDs (installed by each operator's bundle/chart)"]
        K_CRD["Kuadrant, AuthPolicy<br/>RateLimitPolicy, DNSPolicy<br/>TLSPolicy"]
        A_CRD["Authorino, AuthConfig"]
        L_CRD["Limitador"]
        D_CRD["DNSRecord<br/>DNSHealthCheckProbe"]
    end

    subgraph rbac["RBAC (per operator)"]
        K_RBAC["kuadrant-operator<br/>SA + ClusterRole + CRB"]
        A_RBAC["authorino-operator<br/>SA + ClusterRole + CRB"]
        L_RBAC["limitador-operator<br/>SA + ClusterRole + CRB"]
        D_RBAC["dns-operator<br/>SA + ClusterRole + CRB"]
    end

    KOP -.->|"waiting for<br/>Kuadrant CR"| IDLE["Nothing else happens<br/>until user creates Kuadrant CR"]
    AO -.->|"waiting for<br/>Authorino CR"| IDLE
    LO -.->|"waiting for<br/>Limitador CR"| IDLE
```

## Installation: OLM Path

```mermaid
graph TB
    OLM["OLM"] -->|"reads"| CAT["Catalog (4 packages)"]
    CAT -->|"resolves dependencies"| INSTALL

    subgraph INSTALL["OLM installs 4 separate operators"]
        KOP["kuadrant-operator<br/>Deployment, SA, ClusterRole, CRB"]
        AO["authorino-operator<br/>Deployment, SA, ClusterRole, CRB"]
        LO["limitador-operator<br/>Deployment, SA, ClusterRole, CRB"]
        DO["dns-operator<br/>Deployment, SA, ClusterRole, CRB"]
    end
```

## Installation: Helm Path

```mermaid
graph TB
    HI["helm install"] -->|"pulls dependencies"| INSTALL

    subgraph INSTALL["Helm installs 4 charts"]
        KOP["kuadrant-operator chart"]
        AO["authorino-operator chart"]
        LO["limitador-operator chart"]
        DO["dns-operator chart"]
    end
```

## Runtime: Reconciliation Chain

```mermaid
graph TB
    USER["User"] -->|"creates"| KCR["Kuadrant CR"]
    KCR -->|"triggers"| KOP["kuadrant-operator"]

    subgraph wrapper_crs["Wrapper CRs (created by kuadrant-operator)"]
        ACR["Authorino CR"]
        LCR["Limitador CR"]
        DR["DNSRecord CRs"]
    end

    KOP --> ACR
    KOP --> LCR
    KOP --> DR

    subgraph child_operators["Child Operators (pre-installed by OLM/Helm)"]
        AO["authorino-operator"]
        LO["limitador-operator"]
        DO["dns-operator"]
    end

    AO -->|"reconciles"| ACR
    LO -->|"reconciles"| LCR
    DO -->|"reconciles"| DR

    subgraph workloads["Workloads"]
        AW["Authorino Deployment"]
        LW["Limitador Deployment"]
    end

    ACR --> AW
    LCR --> LW
```

## RBAC Model

```mermaid
graph LR
    subgraph sa["Service Accounts"]
        KSA["kuadrant-operator SA"]
        ASA["authorino-operator SA"]
        LSA["limitador-operator SA"]
        DSA["dns-operator SA"]
    end

    subgraph roles["ClusterRoles"]
        KR["kuadrant-operator-manager<br/>manages wrapper CRs directly<br/>no bind/escalate"]
        AR["authorino-operator ClusterRoles"]
        LR_["limitador-operator ClusterRoles"]
        DR["dns-operator ClusterRoles"]
    end

    OLM["OLM creates all<br/>ClusterRoleBindings"]

    OLM --> KSA
    OLM --> ASA
    OLM --> LSA
    OLM --> DSA

    KSA --> KR
    ASA --> AR
    LSA --> LR_
    DSA --> DR
```

## Resource Ownership

```mermaid
graph TB
    subgraph olm_owned["Owned by OLM (per operator)"]
        OPS["Per operator:<br/>Deployment, SA,<br/>ClusterRole, CRB"]
        CRDs["Each operator's CRDs"]
    end

    subgraph kuadrant_op["Owned by kuadrant-operator"]
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
