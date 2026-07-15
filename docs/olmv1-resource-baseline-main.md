# Resource Baseline (main branch)

Captured from a `make local-setup` deployment on main with a Kuadrant CR created.
This serves as the compatibility reference for the umbrella operator migration.

## Deployments

| Name | Selector Labels | Pod Labels | ServiceAccount | Image |
|------|----------------|------------|----------------|-------|
| `kuadrant-operator-controller-manager` | `app: kuadrant`, `control-plane: controller-manager` | same as selector | `kuadrant-operator-controller-manager` | `quay.io/kuadrant/kuadrant-operator:dev` |
| `authorino-operator` | `control-plane: authorino-operator` | same as selector | `authorino-operator` | `quay.io/kuadrant/authorino-operator:latest` |
| `limitador-operator-controller-manager` | `control-plane: controller-manager` | `app: limitador-operator`, `control-plane: controller-manager` | `limitador-operator-controller-manager` | `quay.io/kuadrant/limitador-operator:latest` |
| `dns-operator-controller-manager` | `control-plane: dns-operator-controller-manager` | same as selector | `dns-operator-controller-manager` | `quay.io/kuadrant/dns-operator:latest` |
| `authorino` | `authorino-resource: authorino`, `control-plane: controller-manager` | selector + `kuadrant.io/managed: "true"`, `sidecar.istio.io/inject: "false"` | `authorino-authorino` | `quay.io/kuadrant/authorino:latest` |
| `limitador-limitador` | `app: limitador`, `limitador-resource: limitador` | selector + `kuadrant.io/managed: "true"`, `sidecar.istio.io/inject: "false"`, `app.kubernetes.io/*` labels | (none) | `quay.io/kuadrant/limitador:latest` |

### Known selector issue

`limitador-operator-controller-manager` uses the generic selector `control-plane: controller-manager` which also matches the `kuadrant-operator-controller-manager` pod. This is a pre-existing bug, not introduced by Phase 1.

## ServiceAccounts

| Name | Labels |
|------|--------|
| `kuadrant-operator-controller-manager` | `app: kuadrant` |
| `authorino-operator` | (none) |
| `authorino-authorino` | (none) |
| `limitador-operator-controller-manager` | `app: limitador-operator` |
| `dns-operator-controller-manager` | `app.kubernetes.io/part-of: dns-operator` + standard labels |
| `dns-operator-remote-cluster` | `app.kubernetes.io/part-of: dns-operator` + standard labels |
| `developer-portal-controller-manager` | `app.kubernetes.io/name: developer-portal-controller` |

## ClusterRoles

| Name | Labels |
|------|--------|
| `kuadrant-operator-manager-role` | `app: kuadrant` |
| `authorino-operator-manager` | (none) |
| `authorino-manager-role` | (none) |
| `authorino-manager-k8s-auth-role` | (none) |
| `authorino-authconfig-editor-role` | (none) |
| `authorino-authconfig-viewer-role` | (none) |
| `limitador-operator-manager-role` | (none) |
| `dns-operator-manager-role` | (none) |
| `dns-operator-remote-cluster-role` | (none) |

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

## Roles

| Name | Labels |
|------|--------|
| `kuadrant-operator-leader-election-role` | `app: kuadrant` |
| `authorino-operator-leader-election` | (none) |
| `authorino-leader-election-role` | (none) |
| `limitador-operator-leader-election-role` | (none) |
| `dns-operator-leader-election-role` | `app.kubernetes.io/part-of: dns-operator` + standard labels |
| `developer-portal-leader-election-role` | `app.kubernetes.io/name: developer-portal-controller` |

## RoleBindings

| Name | Role | ServiceAccount |
|------|------|----------------|
| `kuadrant-operator-leader-election-rolebinding` | `kuadrant-operator-leader-election-role` | `kuadrant-operator-controller-manager` |
| `authorino-operator-leader-election` | `authorino-operator-leader-election` | `authorino-operator` |
| `authorino-authorino-leader-election` | `authorino-leader-election-role` | `authorino-authorino` |
| `limitador-operator-leader-election-rolebinding` | `limitador-operator-leader-election-role` | `limitador-operator-controller-manager` |
| `dns-operator-leader-election-rolebinding` | `dns-operator-leader-election-role` | `dns-operator-controller-manager` |
| `developer-portal-leader-election-rolebinding` | `developer-portal-leader-election-role` | `developer-portal-controller-manager` |

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

## ConfigMaps

| Name | Labels |
|------|--------|
| `topology` | `kuadrant.io/topology: "true"` |
| `dns-operator-controller-env` | (none) |
| `limitador-limits-config-limitador` | `app: limitador`, `limitador-resource: limitador` + standard labels |
| `limitador-operator-manager-config` | (none) |
| `manager-config` | (none) |

## Wrapper CRs

| Kind | Name | Owner |
|------|------|-------|
| `Authorino` | `authorino` | `Kuadrant/kuadrant` |
| `Limitador` | `limitador` | `Kuadrant/kuadrant` |

## OwnerReference Chain

```
Kuadrant CR (user-created)
├── Authorino CR (ownerRef → Kuadrant)
│   └── Authorino Deployment (ownerRef → Authorino CR)
└── Limitador CR (ownerRef → Kuadrant)
    └── Limitador Deployment (ownerRef → Limitador CR)

No ownerRef (installed by kustomize/OLM):
├── kuadrant-operator-controller-manager Deployment
├── authorino-operator Deployment
├── limitador-operator-controller-manager Deployment
└── dns-operator-controller-manager Deployment
```
