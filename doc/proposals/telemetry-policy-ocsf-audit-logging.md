# OCSF audit logging for TelemetryPolicy

## Status

Proposal for discussion. This document describes the gateway-side work needed to make
TelemetryPolicy audit logging produce a canonical OCSF record. It does not change the existing
`logging.default.fields` contract, which continues to enrich Authorino decision logs.

## Problem

TelemetryPolicy logging fields are evaluated before the protected upstream request completes and
are attached to Authorino's authorization decision record. That record is useful supporting
telemetry, but it cannot be the canonical request audit event because Authorino cannot observe the
final upstream response status, response size, or end-to-end latency.

RHOAI requires the LLMInferenceService path to use the same event contract as the InferenceService
path: OCSF 1.9.0 API Activity (`class_uid: 6003`) with the `ai_operation` profile. The event must be
emitted once, when request processing finishes at the inference gateway.

## Decision

The canonical OCSF event is emitted by Envoy's access logger at stream completion.

Authorino remains responsible for authentication and authorization. The wasm shim already exports
Authorino's dynamic metadata to Envoy filter state under `wasm.kuadrant.auth`. The TelemetryPolicy
pipeline projects only the approved scalar values needed by OCSF into dedicated
`wasm.kuadrant.ocsf.*` filter-state keys; the access logger reads those keys for the authenticated
actor and target. TelemetryPolicy configures the audit profile and the operator renders the
provider-specific access-log configuration for the targeted Gateway.

This keeps the responsibilities separate:

- Authorino supplies authenticated identity and the authorization decision.
- Envoy owns request-completion data and emits the canonical OCSF event.
- ClusterLogForwarder forwards the gateway container log without constructing or transforming the
  event.
- Existing Authorino `custom.*` fields remain available for diagnostics and correlation, but are not
  described as OCSF records.

The wasm shim must not emit the record through its ordinary host logger. Proxy-Wasm log calls are
mixed with Envoy operational logs and do not provide the raw JSON-lines audit stream required by the
collection contract.

## API direction

OCSF output is an explicit, opt-in preset separate from configurable Authorino fields. A preliminary
shape is:

```yaml
apiVersion: extensions.kuadrant.io/v1alpha1
kind: TelemetryPolicy
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: openshift-ai-inference
  logging:
    default:
      fields:
        request_id: request.id
      ocsf:
        profile: aiOperation
        actor:
          identity: auth.identity.user.username
          uid: auth.identity.user.uid
          groups: auth.identity.user.groups
        target:
          resourceType: LLMInferenceService
          requestPath:
            namespaceSegment: 0
            nameSegment: 1
          aiProvider: KServe
```

Path segments are counted after removing the leading slash. The example therefore maps
`/<namespace>/<llmisvc>/v1/...` to the target namespace and name without inspecting a request body or
query string.

The identity expression is restricted to the `auth.*` namespace. Target extraction uses explicit
path-segment indexes rather than arbitrary CEL. The OCSF preset does not accept arbitrary headers or
generic field expressions, so it cannot be configured to copy an authorization token, cookie,
payload, or query string into the audit record.

The target and AI provider are required for the `aiOperation` preset so the emitted `resources` and
`ai_model` objects are complete. Before the API is finalized, the implementation should verify whether a stable Gateway API route
attribute can identify the target resource. If it can, route metadata should replace the
KServe-specific path mapping above.

## OCSF mapping

The emitted event matches the InferenceService audit contract:

| OCSF field | Value |
| --- | --- |
| `activity_id`, `activity_name` | `99`, `Inference` |
| `category_uid`, `category_name` | `6`, `Application Activity` |
| `class_uid`, `class_name` | `6003`, `API Activity` |
| `type_uid` | `600399` |
| `severity_id`, `severity` | `1`, `Informational` |
| `metadata.version` | `1.9.0` |
| `metadata.profiles` | `["ai_operation"]` |
| `metadata.product` | `kuadrant-gateway` and the Kuadrant release version |
| `actor.user` | name, optional configured UID/groups, and type ID inferred from the identity |
| `api.operation` | HTTP method plus path |
| `http_request` | method, protocol version, and path only |
| `http_response` | final status code, response bytes, and latency |
| `src_endpoint`, `dst_endpoint` | downstream peer and selected upstream endpoint when Envoy exposes them |
| `status_id`, `status`, `status_code` | derived from the final response code |
| `resources` | configured target resource name, namespace, type, and target role |
| `ai_model` | target name and configured provider, when both are known |

The request path excludes the query string. Authorization headers, cookies, arbitrary headers,
request and response bodies, and tokens are never part of the preset.

## Provider implementation

The first implementation target is Istio because the RHOAI LLMInferenceService gateway uses Istio.
The operator appends an access logger to the existing Gateway-scoped EnvoyFilter and selects the
gateway workload with `gateway.networking.k8s.io/gateway-name`. A CEL access-log filter requires the
dedicated `wasm.kuadrant.ocsf.resource.type` marker, so requests for routes without an attached OCSF
TelemetryPolicy do not produce this event.

Envoy's JSON access-log formatter uses `%TYPED_CEL(...)%` expressions over the dedicated
`wasm.kuadrant.ocsf.*` filter-state keys. CEL emits JSON-native values instead of serializing the
filter-state protobuf wrappers. It also supplies typed conditionals for OCSF status fields and derives
epoch milliseconds from the request timestamp. Request method, path, protocol, response code, byte
count, latency, and connection endpoints come from Envoy access-log operators rather than from
user-provided expressions. The query-free URL path is projected from
`request.url_path` by the TelemetryPolicy into filter state, because the deployed Envoy access-log
formatter does not support `REQ_WITHOUT_QUERY`.

The access logger must:

1. run after the Kuadrant wasm filter has exported `wasm.kuadrant.auth`;
2. use structured JSON output, with typed values instead of stringified numbers;
3. render separate success and failure variants when Envoy substitution format cannot express the
   status conditional;
4. emit the OCSF unknown-user representation when authentication did not produce an identity;
5. preserve any access logger configured by the platform instead of replacing it; and
6. emit exactly one OCSF event per selected request.

As in kube-rbac-proxy, actor UID and groups are emitted only when explicitly selected from
Authorino `auth.*` metadata. The current gateway formatter records the direct downstream peer;
trusted `X-Forwarded-For` parsing is not enabled because the Envoy path does not yet provide the
same validated-address-chain behavior as kube-rbac-proxy.

Provider-neutral API behavior is required, but unsupported Gateway providers must report a clear
policy condition instead of silently accepting the OCSF stanza.

## Validation

Unit tests cover API validation and the rendered access-log configuration. An integration test then
drives all of these cases through an Istio Gateway:

- allowed request with a successful upstream response;
- authentication failure;
- authorization denial;
- upstream 4xx/5xx and connection failure;
- streaming response, verifying final byte count and latency;
- target resource and AI model mapping; and
- a request containing bearer credentials, cookies, query parameters, and sensitive payload fields,
  verifying none appear anywhere in the emitted event.

The final RHOAI E2E test validates the complete OCSF record from the gateway container and separately
checks that ClusterLogForwarder makes the same JSON event queryable in Loki.

## Delivery split

1. Confirm the dedicated scalar filter-state projection in a live Envoy and determine whether a
   stable Gateway API route attribute can replace request-path target extraction.
2. Finalize the TelemetryPolicy API and CRD validation.
3. Render the Istio access logger and add unit and integration coverage.
4. Add other Gateway providers or explicit unsupported-provider status.
5. Update KServe CI, E2E assertions, and documentation to consume gateway OCSF records instead of
   treating Authorino decision records as canonical audit events.
