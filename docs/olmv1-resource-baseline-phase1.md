# Resource Baseline (Phase 1 branch)

Captured from a `make local-setup-helm` deployment on `olmv1-umbrella-poc-phase1` with a Kuadrant CR created.

## Deployments

| Name | Selector Labels | Pod Labels | ServiceAccount | Image | OwnerRef |
|------|----------------|------------|----------------|-------|----------|
| `kuadrant-operator-controller-manager` | `app: kuadrant`, `control-plane: controller-manager` | same as selector | `kuadrant-operator-controller-manager` | `quay.io/kuadrant/kuadrant-operator:dev` | (none) |
| `authorino-operator` | `control-plane: authorino-operator` | same as selector | `authorino-operator` | `quay.io/kuadrant/authorino-operator:latest` | `Kuadrant/kuadrant` |
| `limitador-operator-controller-manager` | `control-plane: limitador-operator-controller-manager` | `app: limitador-operator`, `control-plane: limitador-operator-controller-manager` | `limitador-operator-controller-manager` | `quay.io/kuadrant/limitador-operator:latest` | `Kuadrant/kuadrant` |
| `dns-operator-controller-manager` | `control-plane: dns-operator-controller-manager` | same as selector + `app.kubernetes.io/managed-by: helm` | `dns-operator-controller-manager` | `quay.io/kuadrant/dns-operator:latest` | `Kuadrant/kuadrant` |
| `mcp-gateway-controller` | `app.kubernetes.io/instance: mcp-gateway`, `app.kubernetes.io/name: mcp-gateway-controller` | same as selector | `mcp-gateway-controller` | `ghcr.io/kuadrant/mcp-controller:latest` | `Kuadrant/kuadrant` |
| `authorino` | `authorino-resource: authorino`, `control-plane: controller-manager` | selector + `kuadrant.io/managed: "true"`, `sidecar.istio.io/inject: "false"` | `authorino-authorino` | `quay.io/kuadrant/authorino:latest` | `Authorino/authorino` |
| `limitador-limitador` | `app: limitador`, `limitador-resource: limitador` | selector + `kuadrant.io/managed: "true"`, `sidecar.istio.io/inject: "false"`, `app.kubernetes.io/*` labels | (none) | `quay.io/kuadrant/limitador:latest` | `Limitador/limitador` |

## ServiceAccounts

| Name | Labels |
|------|--------|
| `kuadrant-operator-controller-manager` | `app: kuadrant` |
| `authorino-operator` | (none) |
| `authorino-authorino` | (none) |
| `limitador-operator-controller-manager` | `app: limitador-operator`, `app.kubernetes.io/managed-by: helm` |
| `dns-operator-controller-manager` | `app.kubernetes.io/part-of: dns-operator`, `app.kubernetes.io/managed-by: helm` |
| `dns-operator-remote-cluster` | `app.kubernetes.io/part-of: dns-operator`, `app.kubernetes.io/managed-by: helm` |
| `mcp-gateway-controller` | `app.kubernetes.io/name: mcp-gateway`, `app.kubernetes.io/managed-by: Helm` |

## ClusterRoles

| Name | Labels |
|------|--------|
| `kuadrant-operator-manager-role` | `app: kuadrant` |
| `authorino-operator-manager` | (none) |
| `authorino-manager-role` | (none) |
| `authorino-manager-k8s-auth-role` | (none) |
| `authorino-authconfig-editor-role` | (none) |
| `authorino-authconfig-viewer-role` | (none) |
| `limitador-operator-manager-role` | `app.kubernetes.io/managed-by: helm` |
| `dns-operator-manager-role` | `app.kubernetes.io/managed-by: helm` |
| `dns-operator-remote-cluster-role` | `app.kubernetes.io/managed-by: helm` |
| `mcp-gateway-controller` | (none) |

## ClusterRoleBindings

| Name | ClusterRole | ServiceAccount |
|------|-------------|----------------|
| `kuadrant-operator-manager-rolebinding` | `kuadrant-operator-manager-role` | `kuadrant-operator-controller-manager` |
| `authorino-operator-manager` | `authorino-operator-manager` | `authorino-operator` |
| `authorino-authorino` | `authorino-manager-role` | `authorino-authorino` |
| `authorino-authorino-k8s-auth` | `authorino-manager-k8s-auth-role` | `authorino-authorino` |
| `limitador-operator-manager-rolebinding` | `limitador-operator-manager-role` | `limitador-operator-controller-manager` |
| `dns-operator-manager-rolebinding` | `dns-operator-manager-role` | `dns-operator-controller-manager` |
| `dns-operator-remote-cluster-rolebinding` | `dns-operator-remote-cluster-role` | `dns-operator-remote-cluster` |
| `mcp-gateway-controller` | `mcp-gateway-controller` | `mcp-gateway-controller` |

## Roles

| Name | Labels |
|------|--------|
| `kuadrant-operator-leader-election-role` | `app: kuadrant` |
| `authorino-operator-leader-election` | (none) |
| `authorino-leader-election-role` | (none) |
| `limitador-operator-leader-election-role` | `app.kubernetes.io/managed-by: helm` |
| `dns-operator-leader-election-role` | `app.kubernetes.io/part-of: dns-operator`, `app.kubernetes.io/managed-by: helm` |

## RoleBindings

| Name | Role | ServiceAccount |
|------|------|----------------|
| `kuadrant-operator-leader-election-rolebinding` | `kuadrant-operator-leader-election-role` | `kuadrant-operator-controller-manager` |
| `authorino-operator-leader-election` | `authorino-operator-leader-election` | `authorino-operator` |
| `authorino-authorino-leader-election` | `authorino-leader-election-role` | `authorino-authorino` |
| `limitador-operator-leader-election-rolebinding` | `limitador-operator-leader-election-role` | `limitador-operator-controller-manager` |
| `dns-operator-leader-election-rolebinding` | `dns-operator-leader-election-role` | `dns-operator-controller-manager` |

## Services

| Name | Selector | Ports |
|------|----------|-------|
| `kuadrant-operator-metrics` | `app: kuadrant`, `control-plane: controller-manager` | `metrics:8080` |
| `kuadrant-operator-grpc` | same | `grpc:50051` |
| `kuadrant-operator-wasm` | same | `wasm:8082` |
| `authorino-operator-metrics` | `control-plane: authorino-operator` | `metrics:8080` |
| `authorino-authorino-authorization` | `authorino-resource: authorino`, `control-plane: controller-manager` | `grpc:50051`, `http:5001` |
| `authorino-authorino-oidc` | same | `http:8083` |
| `authorino-controller-metrics` | same | `http:8080` |
| `limitador-operator-metrics` | `app: limitador-operator`, `control-plane: controller-manager` | `metrics:8080` |
| `limitador-limitador` | `app: limitador`, `limitador-resource: limitador` | `http:8080`, `grpc:8081` |
| `dns-operator-controller-manager-metrics-service` | `control-plane: dns-operator-controller-manager` | `metrics:8080`, `pprof:8082` |

## OwnerReference Chain

```
Kuadrant CR (user-created)
├── Authorino CR (ownerRef → Kuadrant)
│   └── Authorino Deployment (ownerRef → Authorino CR)
├── Limitador CR (ownerRef → Kuadrant)
│   └── Limitador Deployment (ownerRef → Limitador CR)
├── authorino-operator Deployment (ownerRef → Kuadrant)
├── limitador-operator-controller-manager Deployment (ownerRef → Kuadrant)
├── dns-operator-controller-manager Deployment (ownerRef → Kuadrant)
└── mcp-gateway-controller Deployment (ownerRef → Kuadrant)

No ownerRef (installed by Helm/OLM):
└── kuadrant-operator-controller-manager Deployment
```
