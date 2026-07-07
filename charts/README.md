# Helm Charts POC for OLMv1 Consolidation

This directory contains proof-of-concept Helm charts for deploying Authorino and Limitador workloads directly from kuadrant-operator, without requiring separate operator CRs.

## Context

Part of [Spike #183](https://github.com/Kuadrant/architecture/issues/183) - investigating consolidated operator architecture for OLMv1.

## Architecture

### Current (Separate Operators)
```
Kuadrant CR → kuadrant-operator → creates Authorino CR
                                 ↓
                    authorino-operator → creates Deployment/Services
```

### POC (Consolidated with Helm)
```
Kuadrant CR → kuadrant-operator → renders Helm chart
                                 ↓
                          creates Deployment/Services directly
```

## Charts

### `authorino/`
Minimal Helm chart for Authorino workload deployment.

**Resources created:**
- Deployment (authorino binary)
- Service (auth - gRPC/HTTP)
- Service (oidc - HTTP)
- ServiceAccount

**Configurable values:**
- `image.repository`, `image.tag` - Authorino image
- `replicas` - Number of replicas
- `resources` - Resource limits/requests
- `args` - Command-line arguments (hardcoded defaults for POC)
- `tls.enabled`, `tls.certSecretName` - TLS configuration

**Known limitations:**
- Simplified args (operator builds ~20 conditional args from CR spec)
- Missing 3rd service (`controller-metrics`)
- No volume support (Spec.Volumes.Items)
- No backwards compatibility (env vars for old Authorino versions)

### `limitador/`
Minimal Helm chart for Limitador workload deployment.

**Resources created:**
- Deployment (limitador-server binary)
- Service (HTTP + gRPC)
- ServiceAccount
- ConfigMap (limits configuration)

**Configurable values:**
- `image.repository`, `image.tag` - Limitador image
- `replicas` - Number of replicas
- `resources` - Resource limits/requests
- `storage.type` - Storage backend (memory, redis, disk)

**Known limitations:**
- Missing health probes (LivenessProbe, ReadinessProbe)
- Missing PodDisruptionBudget
- Service should be headless (ClusterIP: None)
- No affinity support
- Simplified storage config (no volume mounts for disk, no Redis env vars)

## Implementation

### Helm Renderer (`pkg/helm/`)

Wrapper around `helm.sh/helm/v3` library:
- `renderer.go` - Renders charts to Unstructured Kubernetes objects
- Uses Helm **as templating only** (`ClientOnly: true`, `DryRun: true`)
- NO `helm install/upgrade` - just template rendering

### Reconcilers (`internal/controller/`)

**HelmAuthorinoReconciler:**
- Renders `charts/authorino/` on Kuadrant CR create/update
- Applies rendered resources via Server-Side Apply
- Sets ownerReferences to Kuadrant CR

**HelmLimitadorReconciler:**
- Renders `charts/limitador/` on Kuadrant CR create/update
- Applies rendered resources via Server-Side Apply
- Sets ownerReferences to Kuadrant CR

## What This POC Proves

✅ Helm charts can render successfully  
✅ Operator can use Helm library for templating  
✅ Rendered manifests can be applied via Server-Side Apply  
✅ Resources get proper ownerReferences for cleanup  
✅ Chart versioning is independent (Chart.yaml version != app version)  

## What This POC Defers

❌ Full feature parity with current operators  
❌ Orphaned resource cleanup (when chart removes resources)  
❌ Cluster-scoped resources (ClusterRole/CRDs - handled by OLM bundle)  
❌ Chart sourcing strategy (embedded vs runtime vs build-time)  
❌ Integration testing with real cluster  

## Testing

```bash
# Render charts manually
helm template test-authorino charts/authorino/ --namespace kuadrant-system
helm template test-limitador charts/limitador/ --namespace kuadrant-system

# Unit tests
go test ./pkg/helm/... -v                    # Chart rendering
go test ./internal/controller -run TestHelm  # Reconciler logic
```

## Next Steps

See [olmv1-resource-cleanup-concern.md](/workspace/architecture/docs/design/olmv1-resource-cleanup-concern.md) for discussion of namespaced resource cleanup strategies.

For production implementation, choices needed:
1. **Full Helm charts vs Go code** - Replicate all operator logic in Helm templates OR vendor Go resource builders
2. **Chart sourcing** - Embedded in image, initContainers (cluster-olm-operator pattern), or build-time fetch
3. **Cleanup strategy** - Inventory tracking, label-based pruning, or Helm upgrade (3-way merge)
