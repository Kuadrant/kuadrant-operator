# Wrapper CRD Fields - What We Lost

This document compares the fields available in Authorino and Limitador wrapper CRs vs. what we currently expose through Helm values.

## Authorino CR Fields

### Currently Extracted by buildHelmValues()

| Wrapper CR Field | Chart Value | Status |
|------------------|-------------|--------|
| `spec.image` | `image.repository` + `image.tag` | ✅ Supported |
| `spec.replicas` | `replicas` | ✅ Supported (conditional) |
| `spec.clusterWide` | `clusterWide` | ✅ Supported |
| `spec.listener.tls.enabled` | `tls.enabled`, `args` | ✅ Supported |
| `spec.oidcServer.tls.enabled` | `tls.enabled`, `args` | ✅ Supported |

### NOT Extracted (Lost Functionality)

| Wrapper CR Field | Description | Impact |
|------------------|-------------|--------|
| `spec.imagePullPolicy` | Image pull policy (Always, IfNotPresent, Never) | ⚠️ **Medium** - Hardcoded to IfNotPresent in buildHelmValues |
| `spec.volumes` | Custom volumes and mounts (ConfigMaps, Secrets) | ❌ **HIGH** - No way to mount custom configs |
| `spec.logLevel` | Log verbosity (debug, info, error) | ❌ **HIGH** - Hardcoded to "info" in args |
| `spec.logMode` | Log format (production, development) | ❌ **HIGH** - Hardcoded to "production" in args |
| `spec.authConfigLabelSelectors` | Custom label selectors for AuthConfigs | ❌ **HIGH** - Hardcoded to "authorino.kuadrant.io/managed-by=authorino" |
| `spec.secretLabelSelectors` | Custom label selectors for Secrets | ❌ **MEDIUM** - Not set at all |
| `spec.supersedingHostSubsets` | Host subset matching behavior | ❌ **MEDIUM** - Uses Authorino default (false) |
| `spec.evaluatorCacheSize` | Size of expression evaluator cache | ❌ **MEDIUM** - Uses Authorino default |
| `spec.tracing.endpoint` | OpenTelemetry tracing endpoint | ❌ **MEDIUM** - No tracing configuration |
| `spec.tracing.tags` | Custom tracing tags | ❌ **MEDIUM** - No tracing configuration |
| `spec.tracing.insecure` | Use insecure tracing connection | ❌ **MEDIUM** - No tracing configuration |
| `spec.metrics.port` | Metrics port number | ❌ **MEDIUM** - Uses Authorino default (8080) |
| `spec.metrics.deep` | Deep metrics enabled | ⚠️ **LOW** - Hardcoded to true in args |
| `spec.healthz.port` | Health probe port | ❌ **LOW** - Uses Authorino default |
| `spec.listener.port` | GRPC listener port | ❌ **MEDIUM** - Uses Authorino default |
| `spec.listener.ports.grpc` | GRPC port override | ❌ **MEDIUM** - Uses Authorino default |
| `spec.listener.ports.http` | HTTP port override | ❌ **MEDIUM** - Uses Authorino default |
| `spec.listener.timeout` | Auth request timeout | ⚠️ **LOW** - Hardcoded to 0 (no timeout) in args |
| `spec.listener.maxHttpRequestBodySize` | Max request body size | ❌ **LOW** - Uses Authorino default |
| `spec.oidcServer.port` | OIDC server port | ⚠️ **LOW** - Hardcoded to 8083 in args |
| `spec.listener.tls.certSecret` | Custom TLS cert secret name | ⚠️ **MEDIUM** - Hardcoded to "authorino-oidc-server-cert" |
| `spec.oidcServer.tls.certSecret` | Custom OIDC TLS cert secret | ⚠️ **MEDIUM** - Uses same cert as listener |

### Hardcoded Values in buildHelmValues()

```go
// These are ALWAYS set to these values:
"--auth-config-label-selector=authorino.kuadrant.io/managed-by=authorino"
"--deep-metrics-enabled=true"
"--log-level=info"           // Should be configurable!
"--log-mode=production"      // Should be configurable!
"--oidc-http-port=8083"
"--timeout=0"
"certSecretName": "authorino-oidc-server-cert"
```

## Limitador CR Fields

### Currently Extracted by buildHelmValues()

| Wrapper CR Field | Chart Value | Status |
|------------------|-------------|--------|
| `spec.image` | `image.repository` + `image.tag` | ✅ Supported |
| `spec.replicas` | `replicas` | ✅ Supported (conditional) |
| `spec.storage.redis` | `storage.type: redis` | ✅ Supported |
| `spec.storage.redisCached` | `storage.type: redis-cached` | ✅ Supported |
| `spec.storage.disk` | `storage.type: disk` | ✅ Supported |

### NOT Extracted (Lost Functionality)

| Wrapper CR Field | Description | Impact |
|------------------|-------------|--------|
| `spec.affinity` | Pod affinity/anti-affinity rules | ❌ **HIGH** - No way to control pod placement |
| `spec.listener.http.port` | HTTP API port | ❌ **MEDIUM** - Uses default (8080) |
| `spec.listener.grpc.port` | gRPC RLS port | ❌ **MEDIUM** - Uses default (8081) |
| `spec.rateLimitHeaders` | Rate limit header format (DRAFT_VERSION_03, etc.) | ❌ **MEDIUM** - Uses Limitador default |
| `spec.telemetry` | Prometheus metrics configuration | ❌ **MEDIUM** - Uses defaults |
| `spec.tracing.endpoint` | OpenTelemetry tracing endpoint | ❌ **MEDIUM** - No tracing configuration |
| `spec.limits` | Static rate limits in CR | ❌ **LOW** - Limits come from RateLimitPolicy anyway |
| `spec.pdb` | PodDisruptionBudget configuration | ❌ **HIGH** - No PDB support |
| `spec.resourceRequirements` | CPU/memory limits and requests | ❌ **HIGH** - No resource limits set (see CONNLINK-1022 discussion) |
| `spec.verbosity` | Log verbosity level (1-4) | ❌ **MEDIUM** - Uses Limitador default |
| `spec.imagePullSecrets` | Image pull secrets | ❌ **MEDIUM** - No private registry support |
| `spec.version` | (Deprecated) Version tag | ✅ N/A - Use spec.image instead |

### Default Storage When No Storage Specified

```go
"storage": {
    "type": "memory"  // Always in-memory if no wrapper CR
}
```

## Impact Analysis

### Critical (Blocking Production Use)

1. **Authorino volumes** - No way to mount custom certificates, OIDC configs, etc.
2. **Limitador affinity** - Can't control pod placement for HA setups
3. **Limitador PDB** - No disruption budget protection
4. **Resource requirements** - Both operators lack resource limits (memory leak risk)

### High Priority

1. **Authorino log level/mode** - Hardcoded, can't enable debug logging
2. **Authorino label selectors** - Hardcoded, limits multi-tenancy scenarios
3. **Limitador rate limit headers** - Can't configure header format for different clients
4. **Image pull secrets** - Can't use private registries

### Medium Priority

1. **Port configurations** - All hardcoded to defaults
2. **Tracing configuration** - No observability integration
3. **TLS cert secret names** - Hardcoded names limit flexibility
4. **Timeout settings** - Can't tune for different workloads

### Low Priority

1. **Metrics configuration** - Defaults are usually fine
2. **Health probe ports** - Rarely need customization
3. **Static limits in Limitador CR** - RateLimitPolicy provides this

## Recommendations

### Short Term (POC Complete)

Document the limitations clearly:
- ✅ **Works for:** Basic installs with defaults
- ❌ **Doesn't work for:** Production HA, custom configs, observability, multi-tenancy

### Medium Term (Next Spike)

Add `spec.authorino` and `spec.limitador` to Kuadrant CR API:

```yaml
apiVersion: kuadrant.io/v1beta1
kind: Kuadrant
spec:
  authorino:
    # All Authorino CR fields here
    logLevel: debug
    volumes:
      items:
        - name: custom-certs
          mountPath: /etc/ssl/custom
          secrets: ["my-cert"]
    tracing:
      endpoint: "tempo.observability.svc:4317"
  
  limitador:
    # All Limitador CR fields here
    affinity:
      podAntiAffinity:
        requiredDuringSchedulingIgnoredDuringExecution:
          - topologyKey: kubernetes.io/hostname
    pdb:
      minAvailable: 1
    resourceRequirements:
      limits:
        memory: 512Mi
```

### Long Term (Production Ready)

**Option A: Full passthrough**
- Kuadrant CR exposes ALL wrapper CR fields
- buildHelmValues() translates everything to Helm values
- Comprehensive, but large API surface

**Option B: Common fields only**
- Kuadrant CR exposes ~20 most common fields
- Users who need advanced config use direct Deployment patches
- Smaller API, 80/20 rule

**Option C: Hybrid**
- Kuadrant CR has common fields + `spec.authorino.extraArgs` / `spec.limitador.extraEnv`
- Escape hatch for power users
- Balance between simplicity and flexibility

## Migration Risk Assessment

### Risk: Existing Production Customizations Lost

**Scenario:** Production cluster has customized Authorino/Limitador wrapper CRs:

```yaml
# Users may have manually edited these in production
kubectl edit authorino authorino -n kuadrant-system
kubectl edit limitador limitador -n kuadrant-system
```

**What happens during migration:**

1. **Existing cluster upgrade (wrapper CRs present):**
   - HelmAuthorinoReconciler detects wrapper CR
   - Calls buildHelmValues(wrapperCR) 
   - **Only extracts 5-10 fields, ignores rest**
   - Applies to Deployment with Force: false
   - Existing Deployment fields preserved (SSA)
   - ⚠️ **Config preserved SHORT TERM** (existing Deployment not recreated)

2. **After wrapper CR deletion:**
   - Wrapper CR deleted
   - Deployment cascade-deleted (owner reference)
   - HelmAuthorinoReconciler recreates from scratch
   - Calls buildDefaultHelmValues() (no wrapper CR)
   - **❌ ALL customizations LOST**

3. **Fresh install or disaster recovery:**
   - No existing Deployment to preserve fields
   - buildDefaultHelmValues() used
   - **❌ ALL customizations LOST**

**Fields at risk:**
- **Critical:** volumes (custom certs), affinity (HA), pdb, resourceRequirements
- **High:** logLevel, tracing, authConfigLabelSelectors, imagePullSecrets
- **Medium:** ports, timeout, metrics config, verbosity

**Impact:** **Production outages** if custom certificates, HA affinity, or resource limits were set.

### Pre-Migration Audit Required

**Before migrating to OLMv1:**

```bash
# 1. Backup current wrapper CRs
kubectl get authorino authorino -n kuadrant-system -o yaml > authorino-backup.yaml
kubectl get limitador limitador -n kuadrant-system -o yaml > limitador-backup.yaml

# 2. Check for customizations
# Compare against defaults to find what was changed

# 3. Document required manual patches
# These must be applied AFTER migration
```

**Migration blockers:**
- If custom volumes exist → Must be manually patched to Deployment
- If custom affinity exists → Must be manually patched to Deployment  
- If custom PDB exists → Must be manually created
- If tracing configured → Currently lost (not extracted by buildHelmValues!)

**Note:** Even though `Force: false` preserves fields on existing Deployments, those customizations are NOT in the Helm chart, so they won't appear on fresh installs or recreations.

## Current Workarounds

Until Kuadrant CR API is extended, users can:

1. **Resource limits**: Use `kubectl set resources deployment`
2. **Custom volumes**: Use `kubectl patch deployment` (but requires careful SSA field ownership)
3. **Tracing/observability**: Configure via environment variables post-deploy
4. **Log level**: Not possible without editing Deployment directly

**Warning:** All manual changes will persist due to `Force: false`, but:
- Won't survive Kuadrant CR deletion/recreation
- Won't survive Deployment deletion/recreation
- Harder to manage (imperative vs declarative)
- No validation/schema
- **Not migrated from wrapper CRs** during upgrade
