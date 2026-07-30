# Payload Processor - Configurable Golang Envoy Filter for Streaming Body Inspection

This example demonstrates a **generalized, configurable streaming request body inspector** using an Envoy Golang filter that:

✅ **Processes body chunks as they arrive** (no full buffering in the plugin)<br/>
✅ **Blocks the filter chain** until data is extracted (prevents headers from reaching upstream prematurely)<br/>
✅ **Supports unlimited payload sizes** (uses a bounded rolling buffer)<br/>
✅ **Extracts data using regex patterns** (fully configurable via EnvoyFilter YAML)<br/>
✅ **Sets Envoy dynamic metadata or filter state** for downstream filters (e.g., Kuadrant AuthPolicy, RateLimitPolicy)<br/>
✅ **Configurable content-type filtering** (process JSON, JSONL, or custom formats)<br/>
✅ **Highly flexible** (extract model names, tenant IDs, user IDs, API versions, etc.)<br/>

**What "No Full Buffering in the Plugin" Means**: The plugin itself only maintains a small rolling buffer (default 512 bytes) to search for patterns chunk-by-chunk. While the plugin blocks the filter chain with `StopAndBuffer`, **Envoy buffers incoming chunks until the pattern is found** (or end of stream). The key distinction:
- **Plugin memory**: Bounded to `max_buffer_size` (512 bytes default)
- **Envoy memory**: Buffers up to the point where the pattern is found (best case: first few KB; worst case: entire request)

This differs from:
- Plugins that must read the entire buffer before making decisions (e.g., full JSON parsing)
- Plugins that don't block the chain, allowing headers to reach upstream while still processing the body

## Use Cases

This filter is designed for scenarios where authorization or rate limiting decisions depend on data in the request body:

### LLM API Gateways
- Extract `model` name from request payload
- Apply different rate limits per model (GPT-4 vs GPT-3.5)
- Enforce access control based on model type

### Multi-Tenancy
- Extract `tenant_id` from request body
- Route requests based on tenant
- Apply tenant-specific quotas and policies

### API Versioning
- Extract `api_version` from nested JSON
- Route to different backend versions
- Apply version-specific rate limits

### User Attribution
- Extract `user_id` for billing/analytics
- Enforce per-user rate limits
- Audit trail based on body content

## How It Works

### Architecture

```
Request → [Golang Filter] → [Kuadrant Wasm] → [Other Filters] → [Router] → [Upstream]
          (extracts value)   (enforces auth/RL
                              based on metadata)
```

**Critical: Filter Ordering**

The Golang filter **must** execute before the Kuadrant wasm filter. This is achieved by using `operation: INSERT_FIRST` in the EnvoyFilter configuration, which places it at the top of the HTTP filter chain.

```yaml
patch:
  operation: INSERT_FIRST  # Ensures this filter runs first
  value:
    name: envoy.filters.http.golang
    # ...
```

**What happens if the order is wrong?**

If Kuadrant wasm runs before the Golang filter:
```
Request → [Kuadrant Wasm] → [Golang Filter] → [Router]
          ❌ reads metadata  ✅ sets metadata
             (not set yet!)    (too late!)
```

Result:
- ❌ Kuadrant can't find the extracted value via request_data bindings
- ❌ AuthPolicy/RateLimitPolicy selectors return empty/null
- ❌ Policies can't make decisions based on request body content
- ✅ Golang filter still extracts and logs the value (but it's too late)

With correct order (`INSERT_FIRST`):
```
Request → [Golang Filter] → [Kuadrant Wasm] → [Router]
          ✅ sets metadata   ✅ reads metadata
                             (available!)
```

Result:
- ✅ Metadata available when Kuadrant wasm executes
- ✅ Policies can access the extracted value
- ✅ Auth/rate-limiting decisions work correctly

### Execution Flow

1. **DecodeHeaders Phase**:
   - Filter checks `Content-Type` matches configured `content_types`
   - If doesn't match or no body: Returns `Continue` → **Allows request to proceed normally**
   - If matches and has body: Returns `StopAndBuffer` → **Blocks the filter chain AND buffers the body**
   - Headers do NOT reach upstream yet
   - ⚠️ **Memory implication**: Envoy buffers chunks until the pattern is found or end of stream

2. **DecodeData Phase** (called for each buffered body chunk):
   - Appends chunk to rolling buffer (max `max_buffer_size` bytes)
   - Searches for configured regex `pattern`
   - If pattern found:
     - Evaluates `object_value` CEL expression with `self.extractedValue`
     - Exports to `dynamic_metadata` or `filter_state` based on `export_as`
     - Returns `Continue` → **Releases filter chain and buffered body**
   - If not found yet:
     - Returns `Continue` → **Continues processing next chunk**
   - If end of stream (pattern never found):
     - Returns `Continue` → **Releases filter chain anyway**

3. **Downstream Filters**:
   - Kuadrant wasm filter reads the exported value via `requestData` bindings
   - Enforces AuthPolicy/RateLimitPolicy based on extracted value

**Memory Trade-off (Envoy 1.38.0 Limitation)**: Due to limitations in Envoy 1.38.0's Golang filter API, this implementation must use `StopAndBuffer`, which causes Envoy to buffer incoming body chunks while the filter searches for the pattern. This means:
- **Best case**: Pattern found early → Envoy only buffers the first few chunks before releasing the chain
- **Worst case**: Pattern never found or appears late → Envoy buffers the entire request body
- **Memory usage** = (concurrent blocked requests) × (bytes buffered until pattern found)
- **Recommendation**: Monitor proxy memory usage and set appropriate resource limits
- **Future**: Newer Envoy versions may support `StopNoBuffer` or phase-specific status codes for streaming processing

**LLM API Request Size Reality**: While simple chat completions are small (< 10KB), LLM APIs often handle much larger payloads:
- **Simple prompts**: 1-10KB (text-only completions)
- **Conversation history**: 10KB-1MB (multi-turn conversations, especially with long context windows)
- **File uploads**: 1MB-100MB+ (PDFs, code files, images for vision models)
- **Assistants API**: Variable (depends on attached files and thread history)

**Recommendations for Production**:
1. **Pattern placement**: Ensure extractable data (like `model` field) appears in the **first few KB** of the request
2. **Content-Length checks**: Skip buffering for requests exceeding a size threshold (e.g., > 10MB)
3. **Early pattern detection**: Use `max_buffer_size` effectively to find patterns without buffering the entire body
4. **Resource limits**: Set strict memory limits on proxy containers to prevent OOM
5. **Request size limits**: Consider rejecting very large requests at the gateway level
6. **Monitoring**: Alert on proxy memory usage and request size distributions
7. **Alternative approach**: For APIs with file uploads, consider extracting metadata from headers/query params instead of body

## Configuration

The filter accepts the following configuration parameters via the EnvoyFilter `plugin_config`:

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `pattern` | string | `"model"\s*:\s*"([^"]+)"` | Regex pattern to search for. First capture group will be extracted as `self.extractedValue` in CEL. |
| `export_as` | string | `dynamic_metadata` | Where to export data: `dynamic_metadata` or `filter_state`. See note below about filter_state limitations. |
| `object_key` | string | `extracted_value` | Key name for the exported value. For `dynamic_metadata`, this is the key within `metadata_namespace`. For `filter_state`, this is the full filter state key. |
| `object_value` | string | `self.extractedValue` | CEL expression evaluated to produce the final value. Available variable: `self.extractedValue` (the regex match). Examples: `self.extractedValue`, `'"{" + self.extractedValue + "}"'` |
| `metadata_namespace` | string | `envoy.filters.http.golang.payload_processor` | *(Optional)* Namespace for dynamic metadata. Only used when `export_as=dynamic_metadata`. |
| `max_buffer_size` | int | `512` | Maximum size of the rolling buffer (bytes). Should be ≥ 2× expected pattern length. |
| `content_types` | []string | `["application/json"]` | List of content types to process. Requests with other content types are skipped. |

**Note on `filter_state` export**: While the Golang filter successfully sets filter_state values (verified with Lua filters), the current wasm-shim implementation cannot read filter_state values via the proxy-wasm `get_property()` API. Use `export_as: dynamic_metadata` for Kuadrant policy integration. See [envoyfilter-filter_state.yaml](envoyfilter-filter_state.yaml) for filter_state configuration examples.

### Configuration Examples

#### Extract LLM Model Name (Basic)
```yaml
plugin_config:
  # Extract just the model name
  pattern: '"model"\s*:\s*"([^"]+)"'
  export_as: dynamic_metadata
  metadata_namespace: "payload_processor"
  object_key: "model"
  object_value: self.extractedValue
  max_buffer_size: 512
  content_types:
    - "application/json"
```

Matches: `{"model":"gpt-4"}` → exports `payload_processor.model = "gpt-4"`

#### Extract with JSON Wrapping (CEL Transform)
```yaml
plugin_config:
  # Extract the entire "model":"value" pair and wrap in JSON object
  pattern: '("model"\s*:\s*"([^"]+)")'
  export_as: filter_state  # or dynamic_metadata
  object_key: "payload_processor"
  object_value: '"{" + self.extractedValue + "}"'
  max_buffer_size: 512
```

Matches: `{"model":"gpt-4"}` → exports `{"model":"gpt-4"}`

**Note**: This pattern uses two capture groups but only the first is extracted. See [envoyfilter-filter_state.yaml](envoyfilter-filter_state.yaml) for the complete example.

#### Extract Tenant ID
```yaml
plugin_config:
  pattern: '"tenant_id"\s*:\s*"([a-f0-9\-]+)"'
  export_as: dynamic_metadata
  metadata_namespace: "tenant_metadata"
  object_key: "tenant_id"
  object_value: self.extractedValue
  max_buffer_size: 1024
```

Matches: `{"tenant_id":"550e8400-e29b-41d4-a716-446655440000"}` → extracts UUID

#### Extract Numeric User ID
```yaml
plugin_config:
  pattern: '"user_id"\s*:\s*(\d+)'
  export_as: dynamic_metadata
  metadata_namespace: "user_metadata"
  object_key: "user_id"
  object_value: self.extractedValue
  max_buffer_size: 256
```

Matches: `{"user_id":12345}` → extracts `12345`

#### Extract Nested API Version
```yaml
plugin_config:
  pattern: '"version"\s*:\s*"(v[0-9]+)"'
  export_as: dynamic_metadata
  metadata_namespace: "api_metadata"
  object_key: "version"
  object_value: self.extractedValue
  max_buffer_size: 512
```

Matches: `{"api":{"version":"v2"}}` → extracts `v2`

#### Accept Multiple Content Types
```yaml
plugin_config:
  pattern: '"event_type"\s*:\s*"([^"]+)"'
  export_as: dynamic_metadata
  metadata_namespace: "event_metadata"
  object_key: "type"
  object_value: self.extractedValue
  max_buffer_size: 512
  content_types:
    - "application/json"
    - "application/x-ndjson"       # Newline-delimited JSON
    - "application/jsonlines"       # JSONL
    - "text/json"
```

Processes requests with any of the specified content types.

See the complete working examples:
- [envoyfilter-dynamic_metadata.yaml](envoyfilter-dynamic_metadata.yaml) - Dynamic metadata export (works with Kuadrant)
- [envoyfilter-filter_state.yaml](envoyfilter-filter_state.yaml) - Filter state export (experimental)
- [envoyfilter-examples.yaml](envoyfilter-examples.yaml) - Additional pattern examples

## Building the Filter

### Prerequisites

- Go 1.25+
- Docker or Podman (for cross-platform builds)
- Kubernetes cluster with Istio or Envoy Gateway
- kubectl configured to access the cluster

### Option 1: Build Locally

```bash
cd examples/payload-processor
make build
```

This produces `extract_model.so`.

### Option 2: Build with Docker

```bash
make docker-build
```

Ensures consistent build environment regardless of host OS.

### Verify the Build

```bash
make verify
```

Expected output:
```
✓ extract_model.so exists
extract_model.so: ELF 64-bit LSB shared object, x86-64
```

## Deployment

The deployment uses an **emptyDir volume + kubectl cp** approach:

1. Patches the gateway deployment to add an emptyDir volume at `/var/lib/golang-filters`
2. Copies the pre-built `.so` file to running pods using `kubectl cp`
3. Restarts the deployment so Envoy loads the filter
4. Works with Istio-managed gateways (which have read-only filesystems)

### Quick Start

```bash
cd examples/payload-processor

# Build filter and deploy
make build
./deploy.sh
```

### Manual Deployment

If you prefer to run the steps manually:

```bash
# 1. Build the .so file (must be Linux ELF format)
make build

# 2. Verify it's in the correct format
make verify

# 3. Deploy with custom namespace/gateway (optional)
NAMESPACE=my-namespace GATEWAY_NAME=my-gateway ./deploy.sh
```

The `deploy.sh` script automatically:
- ✅ Verifies `.so` file exists and is in ELF format
- ✅ Discovers the gateway deployment via label `gateway.networking.k8s.io/gateway-name`
- ✅ Patches deployment to add emptyDir volume (if not already present)
- ✅ Restarts deployment if volume already exists (to get fresh pods)
- ✅ Applies the EnvoyFilter configuration
- ✅ Copies `.so` file to all running pods
- ✅ Restarts deployment again to load the filter
- ✅ Copies `.so` file to final pods (since emptyDir clears on restart)

### Important Notes

⚠️ **Binary Format**: The `.so` file must be in Linux ELF format. The Makefile uses containerized builds to ensure correct format even when building on macOS.

⚠️ **Pod Restarts**: Since emptyDir is ephemeral, if pods restart you'll need to run `./deploy.sh` again to re-copy the file.

✅ **Production**: For production environments, consider building a custom gateway image with the `.so` file baked in, or use an init container to copy it on pod start.

## Kuadrant Operator Configuration

For Kuadrant policies (AuthPolicy, RateLimitPolicy) to access the extracted values from the request payload, you need to configure the Kuadrant Operator to inject request data bindings into the wasm configuration.

### Enable Request Data Bindings

After installing Kuadrant, configure the operator to expose the extracted metadata:

```bash
kubectl set env deployment/kuadrant-operator-controller-manager -n kuadrant-system \
  WASM_REQUEST_DATA="payload_processor=metadata.filter_metadata.payload_processor"
```

This command:
1. Sets the `WASM_REQUEST_DATA` environment variable on the Kuadrant Operator
2. Automatically restarts the operator pod
3. Makes the extracted values available to policies via the wasm configuration

**Format**: `WASM_REQUEST_DATA` accepts a comma-separated list of `key=value` pairs:
- **Key**: The binding name used in policy selectors
- **Value**: The path to the data (e.g., `metadata.filter_metadata.payload_processor`)

**Special handling**: If a value contains commas, wrap it in quotes:
```bash
WASM_REQUEST_DATA='key1=value1,key2="value,with,commas",key3=value3'
```

## Verifying Filter Order

Before testing, verify the Golang filter is correctly positioned in the filter chain:

```bash
# Get the filter chain configuration
kubectl exec -n gateway-system -l gateway.networking.k8s.io/gateway-name=kuadrant-ingressgateway -c istio-proxy -- \
  curl -s localhost:15000/config_dump > /tmp/proxy-config.json

# Extract HTTP filter names in order
cat /tmp/proxy-config.json | jq -r '
  .configs[].dynamic_listeners[]?.active_state.listener.filter_chains[]?.filters[]?.typed_config.http_filters[]?.name
' | grep -E "golang|wasm|router"
```

**Expected order:**
```
envoy.filters.http.golang                                     # ← Your filter (FIRST)
extensions.istio.io/wasmplugin/gateway-system.kuadrant-...    # ← Kuadrant wasm
envoy.filters.http.router                                     # ← Router (LAST)
```

**Wrong order (won't work):**
```
extensions.istio.io/wasmplugin/gateway-system.kuadrant-...    # ← Wasm runs first
envoy.filters.http.golang                                     # ← Golang runs second (too late!)
```

If the order is wrong:
1. Check that your EnvoyFilter uses `operation: INSERT_FIRST`
2. Verify the EnvoyFilter was applied: `kubectl get envoyfilter -n gateway-system`
3. Delete and reapply the EnvoyFilter, then restart the gateway pods

## Testing

### 1. Send Test Requests

**Extract LLM model:**
```bash
curl -X POST http://your-gateway/api/chat \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4",
    "messages": [{"role": "user", "content": "Hello"}],
    "max_tokens": 100
  }' \
  -v
```

**Extract tenant ID:**
```bash
curl -X POST http://your-gateway/api/data \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "acme-corp",
    "query": "SELECT * FROM orders"
  }' \
  -v
```

**Extract user ID:**
```bash
curl -X POST http://your-gateway/api/action \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": 12345,
    "action": "delete"
  }' \
  -v
```

### 2. Check Envoy Logs

```bash
kubectl logs -n gateway-system \
  -l gateway.networking.k8s.io/gateway-name=kuadrant-ingressgateway \
  -c istio-proxy \
  --tail=50 \
  -f | grep -E "Pattern found|Exported to"
```

Expected output:
```
[info] Request detected (content-type: application/json), blocking filter chain until pattern extracted
[info] Pattern found in payload: gpt-4
[info] Exported to dynamic_metadata[payload_processor][model] = gpt-4
```

Or for tenant extraction:
```
[info] Pattern found in payload: acme-corp
[info] Exported to dynamic_metadata[tenant_metadata][tenant_id] = acme-corp
```

### 3. Verify Metadata is Set

Check the Envoy logs to confirm the value was exported:

```bash
kubectl logs -n gateway-system -l gateway.networking.k8s.io/gateway-name=kuadrant-ingressgateway -c istio-proxy --tail=100 | grep "Exported to"
```

Expected output for `export_as: dynamic_metadata`:
```
Exported to dynamic_metadata[payload_processor][model] = gpt-4
```

Or for `export_as: filter_state`:
```
Exported to filter_state[payload_processor] = {"model":"gpt-4"} (StateType=Mutable, LifeSpan=Request, Sharing=None)
```

**Note**: If using `export_as: filter_state`, you can verify with the debug Lua filter from [envoyfilter-debug-lua.yaml](envoyfilter-debug-lua.yaml), but the wasm-shim currently cannot read filter_state values.

## Using with Kuadrant Policies

After configuring the `WASM_REQUEST_DATA` environment variable (see [Kuadrant Operator Configuration](#kuadrant-operator-configuration) above), you can reference the extracted payload values in your policies.

The examples below assume the following operator configuration:
```bash
WASM_REQUEST_DATA="payload_processor=metadata.filter_metadata.payload_processor"
```

**Example:** AuthPolicy that checks the extracted model against allowed models for the user:

```yaml
apiVersion: kuadrant.io/v1
kind: AuthPolicy
metadata:
  name: model-access-control
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: llm-api
  rules:
    authorization:
      "model-access":
        patternMatching:
          patterns:
          - predicate: metadata.filter_metadata.payload_processor.model in ["gpt-4", "claude-3-opus-20240229"]
```

## Advanced Configuration

### Tuning the Rolling Buffer Size

The `max_buffer_size` parameter controls how much data is kept in memory while searching for the pattern:

```yaml
max_buffer_size: 1024  # bytes
```

**Guidelines:**
- **Minimum**: 2× the maximum expected pattern length (to handle patterns split across chunks)
- **Typical values**: 256-1024 bytes
- **Large patterns**: Up to 4096 bytes for complex extraction

**Example**: If extracting `"model":"claude-3-opus-20240229"` (≈40 bytes), use at least 512 bytes.

### Writing Effective Regex Patterns

The filter uses Go's `regexp` package. The **first capture group** `(...)` is extracted.

**Pattern Tips:**
- Use `\s*` for optional whitespace (handles formatting variations)
- Use `[^"]+` to match "anything except quotes" for string values
- Use `\d+` for numeric values
- Use `[a-f0-9\-]+` for UUIDs
- Escape special chars: `\.` `\(` `\)` `\{` `\}`

**Examples:**

| Use Case | Pattern | Matches |
|----------|---------|---------|
| String value | `"key"\s*:\s*"([^"]+)"` | `"key":"value"` |
| Numeric value | `"count"\s*:\s*(\d+)` | `"count":42` |
| Float value | `"temp"\s*:\s*([0-9.]+)` | `"temp":0.7` |
| UUID | `"id"\s*:\s*"([a-f0-9\-]{36})"` | `"id":"550e8400-..."` |
| Nested key | `"version"\s*:\s*"(v\d+)"` | `{"api":{"version":"v2"}}` |

### Multiple Patterns

To extract multiple values from the same request, deploy the filter multiple times with different configurations:

```yaml
# Filter 1: extract model
- name: envoy.filters.http.golang.model
  typed_config:
    library_id: payload_processor_model
    plugin_config:
      pattern: '"model"\s*:\s*"([^"]+)"'
      export_as: dynamic_metadata
      metadata_namespace: "payload_processor"
      object_key: "model"
      object_value: self.extractedValue

# Filter 2: extract temperature
- name: envoy.filters.http.golang.temperature
  typed_config:
    library_id: payload_processor_temperature
    plugin_config:
      pattern: '"temperature"\s*:\s*([0-9.]+)'
      export_as: dynamic_metadata
      metadata_namespace: "payload_processor"
      object_key: "temperature"
      object_value: self.extractedValue
```

Both values end up in the same metadata namespace (`payload_processor.model` and `payload_processor.temperature`) and can be used by downstream policies.

### Content Type Filtering

The filter only processes requests with matching `Content-Type` headers. Configure via `content_types`:

```yaml
plugin_config:
  content_types:
    - "application/json"
    - "application/x-ndjson"
    - "text/plain"
```

**How matching works:**
- Case-insensitive substring match
- `"application/json"` matches `"application/json"`, `"application/json; charset=utf-8"`, etc.
- Requests with non-matching content types are skipped (filter returns early)

**Common content types:**
- `application/json` - Standard JSON
- `application/x-ndjson` - Newline-delimited JSON (streaming)
- `application/jsonlines` - JSONL format
- `text/json` - Legacy JSON MIME type
- `application/ld+json` - JSON-LD (linked data)
- `application/vnd.api+json` - JSON API specification

**Example: Support both JSON and JSONL**
```yaml
content_types:
  - "application/json"
  - "application/x-ndjson"
```

This allows the same pattern to extract from both regular JSON payloads and streaming JSONL (common in LLM streaming responses).

## Troubleshooting

### Filter not loading

**Symptom**: Envoy logs show `Failed to load library`

**Solution**: Check that:
1. The .so file is mounted at `/var/lib/golang-filters/extract_model.so`
2. The file has execute permissions
3. The architecture matches (amd64 vs arm64)

```bash
kubectl exec -n gateway-system -l gateway.networking.k8s.io/gateway-name=kuadrant-ingressgateway -c istio-proxy -- \
  ls -la /var/lib/golang-filters/
```

### Pattern not matching

**Symptom**: Logs show "Pattern not found in payload (end of stream)"

**Solution**:
1. Check the actual JSON structure with a debug filter
2. Adjust the regex pattern
3. Increase `max_buffer_size` if pattern might be split across chunks
4. Verify content type is in the `content_types` list

### Kuadrant policies not seeing extracted metadata

**Symptom**: RateLimitPolicy/AuthPolicy can't access the extracted value, even though logs show "Pattern found"

**Possible causes:**

1. **WASM_REQUEST_DATA not configured**
   - The Kuadrant Operator needs the `WASM_REQUEST_DATA` environment variable set
   - **Solution**: See [Kuadrant Operator Configuration](#kuadrant-operator-configuration) section
   - Run: `kubectl set env deployment/kuadrant-operator-controller-manager -n kuadrant-system WASM_REQUEST_DATA="payload_processor=metadata.filter_metadata.payload_processor"`

2. **Filter order is wrong** - Kuadrant wasm runs before Golang filter
   - **Solution**: Verify filter order (see "Verifying Filter Order" section above)
   - Ensure EnvoyFilter uses `operation: INSERT_FIRST`

3. **Metadata namespace/key mismatch**
   - Golang filter config: `metadata_namespace: "payload_processor"`, `object_key: "model"`
   - Operator config: `WASM_REQUEST_DATA="payload_processor=metadata.filter_metadata.payload_processor"`
   - Policy selector: `request.context.extensions.request_data.payload_processor.model`
   - **Solution**: Ensure all three match exactly

4. **Wrong selector path in policy**
   - ✅ Correct: `request.context.extensions.request_data.payload_processor.model`
   - ❌ Wrong: `metadata.filter_metadata.payload_processor.model` (old path, doesn't go through request_data)

5. **Using filter_state export with wasm-shim**
   - Filter state values are not currently readable by the wasm-shim via proxy-wasm API
   - **Solution**: Use `export_as: dynamic_metadata` instead
   - See the configuration note in the Configuration section above

**Debug:**
```bash
# Verify pattern extraction works
kubectl logs -n gateway-system -l gateway.networking.k8s.io/gateway-name=kuadrant-ingressgateway -c istio-proxy --tail=100 | \
  grep "Pattern found"

# Should see: "Pattern found in payload: gpt-4"
# And: "Exported to dynamic_metadata[payload_processor][model] = gpt-4"
```

### Filter chain still executing early

**Symptom**: Upstream receives headers before filter completes

**Solution**: Verify:
1. `DecodeHeaders` returns `StopAndBuffer` when blocking (not `Continue`)
2. EnvoyFilter uses `operation: INSERT_FIRST` (executes before all other filters)
3. Golang filter appears first in the filter chain (check with `istioctl proxy-config listeners`)

**Note**: If you see errors like "unexpected status: 5" or "unexpected status: 101" in logs, it means the filter tried to use unsupported status codes (`StopNoBuffer` or phase-specific codes). Envoy 1.38.0 only supports `Continue` (2) and `StopAndBuffer` (3).

**Debug filter order:**
```bash
kubectl exec -n gateway-system deploy/kuadrant-ingressgateway -c istio-proxy -- \
  curl -s localhost:15000/config_dump | jq '.configs[].dynamic_listeners[].active_state.listener.filter_chains[].filters[].http_filters[].name'
```

Expected output (Golang filter should be first):
```
"envoy.filters.http.golang"
"extensions.istio.io/wasmplugin/..."
"envoy.filters.http.router"
```

## Performance Considerations

- **Latency**: ~0.1-0.5ms per chunk (negligible compared to LLM latency)
- **Memory (plugin)**: Bounded to 512 bytes rolling buffer per request
- **Memory (Envoy)**: Buffers chunks until pattern found (worst case: full request body; see Limitations)
- **CPU**: Minimal (regex match on small buffer)
- **Scalability**: Same as Envoy's baseline (thousands of RPS)

For most LLM API use cases:
- Model parameter appears in first 100-200 bytes
- Filter processes 1-2 chunks before finding pattern
- Total overhead: <1ms

## Limitations

1. **Full body buffering (Envoy 1.38.0)**: Envoy buffers the entire request body in memory
   - Root cause: Golang filter API only supports `StopAndBuffer` and `Continue` status codes
   - Memory usage = (concurrent requests) × (request body size)
   - **Production concern**: Can exhaust proxy memory with concurrent large requests
   - **Suitable for**: Simple text-only LLM completions (< 10KB)
   - **Risk zone**: Conversation history, file uploads, vision models (1MB-100MB+)
   - **Not recommended**: APIs that regularly handle file uploads or large context windows without proper safeguards
   - **Future**: May be resolved in newer Envoy versions with streaming status code support

2. **Content-Type matching**: Only processes requests with matching `Content-Type` headers (configurable via `content_types`)

3. **Rolling buffer window**: Patterns split across more than `max_buffer_size` bytes may be missed
   - Default 512 bytes handles patterns up to ~256 bytes safely (2× factor for chunk boundaries)
   - Since body is buffered anyway, this is less critical than in streaming scenarios

4. **Regex matching**: Uses regex, not full JSON parser
   - **Advantage**: Much faster, works with any text format (JSON, JSONL, XML, plain text)
   - **Limitation**: Cannot handle complex nested structures or validate JSON syntax

## Next Steps & Enhancements

### Already Implemented ✅
- ✅ Configurable regex patterns
- ✅ CEL expression support for value transformation (`object_value`)
- ✅ Dual export modes: `dynamic_metadata` and `filter_state`
- ✅ Configurable metadata namespace and keys
- ✅ Configurable buffer size
- ✅ Generic value extraction (not LLM-specific)
- ✅ Multiple content type support

### Known Limitations
- ⚠️ Filter state export (`export_as: filter_state`) works with Lua filters but not with wasm-shim
  - Root cause: proxy-wasm `get_property()` API cannot read `Router::StringAccessorImpl` objects created by Golang filters
  - Workaround: Use `export_as: dynamic_metadata` for Kuadrant policy integration

### Potential Future Enhancements
- **Content-Length protection**: Skip buffering if Content-Length header exceeds a configurable threshold
- **Multi-pattern support**: Extract multiple values in a single filter instance (currently requires multiple filter deployments)
- **Structured parsing**: Use a JSON streaming parser instead of regex for complex extractions
- **Content type detection**: Support `application/x-protobuf`, `application/xml`, etc.
- **Conditional extraction**: Only extract if certain headers are present
- **Performance metrics**: Expose metrics on extraction success/failure rates, memory usage
- **Pattern validation**: Pre-validate regex patterns at config load time
- **Filter state compatibility**: Investigate using different filter state object types that are readable by proxy-wasm
- **Streaming support**: When newer Envoy versions support `StopNoBuffer`, implement true streaming without full body buffering

## References

- [Envoy Golang Filter API](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/golang_filter)
- [Kuadrant Policy Machinery](https://docs.kuadrant.io/latest/architecture/policy-machinery/)
- [Istio EnvoyFilter](https://istio.io/latest/docs/reference/config/networking/envoy-filter/)
