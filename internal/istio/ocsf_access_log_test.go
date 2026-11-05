//go:build unit

package istio

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/google/cel-go/cel"
	istioapinetworkingv1alpha3 "istio.io/api/networking/v1alpha3"
)

func TestBuildEnvoyFilterOCSFAccessLogPatch(t *testing.T) {
	patch, err := BuildEnvoyFilterOCSFAccessLogPatch()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if patch.ApplyTo != istioapinetworkingv1alpha3.EnvoyFilter_NETWORK_FILTER {
		t.Fatalf("expected NETWORK_FILTER patch, got %s", patch.ApplyTo)
	}
	if patch.Match.GetContext() != istioapinetworkingv1alpha3.EnvoyFilter_GATEWAY {
		t.Fatalf("expected GATEWAY context, got %s", patch.Match.GetContext())
	}
	if patch.Patch.GetOperation() != istioapinetworkingv1alpha3.EnvoyFilter_Patch_MERGE {
		t.Fatalf("expected MERGE operation, got %s", patch.Patch.GetOperation())
	}

	raw, err := patch.Patch.Value.MarshalJSON()
	if err != nil {
		t.Fatalf("failed to marshal rendered patch: %v", err)
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("failed to unmarshal rendered patch: %v", err)
	}
	for _, prohibited := range []string{".string_value", ".number_value", ".bool_value", ".list_value", ".struct_value", "proto.Unmarshal", "protojson", "protowire", "base64"} {
		if strings.Contains(string(raw), prohibited) {
			t.Fatalf("OCSF formatter must not project protobuf oneof fields or parse wire values; found %q", prohibited)
		}
	}

	typedConfig := ocsfMapField(t, value, "typed_config")
	accessLogs := ocsfSliceField(t, typedConfig, "access_log")
	if len(accessLogs) != 1 {
		t.Fatalf("expected one access logger, got %d", len(accessLogs))
	}
	logger, ok := accessLogs[0].(map[string]any)
	if !ok {
		t.Fatalf("expected access logger object, got %T", accessLogs[0])
	}
	extensionFilter := ocsfMapField(t, ocsfMapField(t, logger, "filter"), "extension_filter")
	filterConfig := ocsfMapField(t, extensionFilter, "typed_config")
	if filterConfig["expression"] != `"wasm.kuadrant.ocsf.resources" in filter_state` {
		t.Fatalf("expected logger to select only OCSF policy requests, got %v", filterConfig["expression"])
	}
	logFormat := ocsfMapField(t, ocsfMapField(t, logger, "typed_config"), "log_format")
	if logFormat["omit_empty_values"] != true {
		t.Fatal("expected empty optional OCSF values to be omitted")
	}
	jsonFormat := ocsfMapField(t, logFormat, "json_format")
	if jsonFormat["class_uid"] != float64(6003) {
		t.Fatalf("expected OCSF API Activity class_uid, got %v", jsonFormat["class_uid"])
	}
	if jsonFormat["status_code"] != "%CEL(response.code)%" {
		t.Fatalf("expected OCSF status_code to use the string-returning CEL formatter, got %v", jsonFormat["status_code"])
	}
	httpResponse := ocsfMapField(t, jsonFormat, "http_response")
	wantLatency := `%TYPED_CEL((request.duration.getSeconds() * 1000 + request.duration.getMilliseconds()))%`
	if httpResponse["latency"] != wantLatency {
		t.Fatalf("expected latency to use total request duration in milliseconds, got %v", httpResponse["latency"])
	}

	srcEndpoint := ocsfMapField(t, jsonFormat, "src_endpoint")
	if srcEndpoint["ip"] != "%DOWNSTREAM_DIRECT_REMOTE_ADDRESS_WITHOUT_PORT%" {
		t.Fatalf("expected source IP from the direct remote address provider, got %v", srcEndpoint["ip"])
	}
	if srcEndpoint["port"] != "%DOWNSTREAM_DIRECT_REMOTE_PORT%" {
		t.Fatalf("expected source port from the matching direct remote provider, got %v", srcEndpoint["port"])
	}
	dstEndpoint := ocsfMapField(t, jsonFormat, "dst_endpoint")
	if dstEndpoint["ip"] != "%UPSTREAM_REMOTE_ADDRESS_WITHOUT_PORT%" {
		t.Fatalf("expected destination IP without a port, got %v", dstEndpoint["ip"])
	}
	if dstEndpoint["port"] != "%UPSTREAM_REMOTE_PORT%" {
		t.Fatalf("expected destination port from the matching upstream provider, got %v", dstEndpoint["port"])
	}

	api := ocsfMapField(t, jsonFormat, "api")
	if api["operation"] != `%REQ(:METHOD)% %TYPED_CEL("wasm.kuadrant.ocsf.http_request.url.path" in filter_state ? filter_state["wasm.kuadrant.ocsf.http_request.url.path"] : "")%` {
		t.Fatalf("expected query-safe API operation, got %v", api["operation"])
	}
	httpRequest := ocsfMapField(t, jsonFormat, "http_request")
	if httpRequest["version"] != `%TYPED_CEL(request.protocol == "HTTP/1.0" ? "1.0" : request.protocol == "HTTP/1.1" ? "1.1" : request.protocol == "HTTP/2" ? "2.0" : request.protocol == "HTTP/3" ? "3.0" : request.protocol)%` {
		t.Fatalf("expected HTTP version to use CEL-compatible string operations, got %v", httpRequest["version"])
	}
	url := ocsfMapField(t, httpRequest, "url")
	if url["path"] != `%TYPED_CEL("wasm.kuadrant.ocsf.http_request.url.path" in filter_state ? filter_state["wasm.kuadrant.ocsf.http_request.url.path"] : "")%` {
		t.Fatalf("expected query-safe request path, got %v", url["path"])
	}

	actor := ocsfMapField(t, jsonFormat, "actor")
	wantActor := `%TYPED_CEL("wasm.kuadrant.ocsf.actor.user" in filter_state ? filter_state["wasm.kuadrant.ocsf.actor.user"] : {"type_id": 0})%`
	if actor["user"] != wantActor {
		t.Fatalf("expected actor user to be a grouped typed value with the OCSF unknown-user fallback, got %v", actor["user"])
	}

	wantResources := `%TYPED_CEL("wasm.kuadrant.ocsf.resources" in filter_state ? filter_state["wasm.kuadrant.ocsf.resources"] : [])%`
	if jsonFormat["resources"] != wantResources {
		t.Fatalf("expected resources to use the full typed filter-state value, got %v", jsonFormat["resources"])
	}
	wantAIModel := `%TYPED_CEL("wasm.kuadrant.ocsf.ai_model" in filter_state ? filter_state["wasm.kuadrant.ocsf.ai_model"] : {})%`
	if jsonFormat["ai_model"] != wantAIModel {
		t.Fatalf("expected AI model fields to use the full typed filter-state value, got %v", jsonFormat["ai_model"])
	}
}

func TestOCSFTypedFilterStateValuePreservesCELTypes(t *testing.T) {
	nested := []any{
		map[string]any{
			"name": "ocsf-test-group",
			"attributes": map[string]any{
				"priority": int64(7),
				"enabled":  true,
				"labels":   []any{"audit", "inference"},
			},
		},
	}
	tests := []struct {
		name     string
		key      string
		value    any
		want     any
		wantType string
		present  bool
	}{
		{name: "string", key: "test.string", value: "ocsf-test-user", want: "ocsf-test-user", wantType: "string", present: true},
		{name: "number", key: "test.number", value: int64(42), want: int64(42), wantType: "int", present: true},
		{name: "boolean", key: "test.boolean", value: true, want: true, wantType: "bool", present: true},
		{name: "nested list and map", key: "test.nested", value: nested, want: nested, wantType: "list", present: true},
		{name: "missing optional field", key: "test.missing", wantType: "null_type", present: false},
	}

	env, err := cel.NewEnv(cel.Variable("filter_state", cel.MapType(cel.StringType, cel.DynType)))
	if err != nil {
		t.Fatalf("create CEL environment: %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			formatted := ocsfTypedFilterStateValue(tt.key, "null")
			wantFormatter := "%TYPED_CEL(" + `"` + tt.key + `" in filter_state ? filter_state["` + tt.key + `"] : null)%`
			if formatted != wantFormatter {
				t.Fatalf("expected full filter-state value to be passed to TYPED_CEL, got %q", formatted)
			}

			expression := strings.TrimSuffix(strings.TrimPrefix(formatted, "%TYPED_CEL("), ")%")
			ast, issues := env.Compile(expression)
			if issues != nil && issues.Err() != nil {
				t.Fatalf("compile generated CEL expression %q: %v", expression, issues.Err())
			}
			program, err := env.Program(ast)
			if err != nil {
				t.Fatalf("build CEL program: %v", err)
			}

			filterState := map[string]any{}
			if tt.present {
				filterState[tt.key] = tt.value
			}
			got, _, err := program.Eval(map[string]any{"filter_state": filterState})
			if err != nil {
				t.Fatalf("evaluate generated CEL expression: %v", err)
			}
			if got.Type().TypeName() != tt.wantType {
				t.Fatalf("expected CEL type %q, got %q (%v)", tt.wantType, got.Type().TypeName(), got)
			}
			var native any
			if tt.present {
				native, err = got.ConvertToNative(reflect.TypeOf(tt.want))
				if err != nil {
					t.Fatalf("convert CEL value to its native type: %v", err)
				}
				if !reflect.DeepEqual(native, tt.want) {
					t.Fatalf("expected typed CEL value %#v, got %#v", tt.want, native)
				}
			}
			gotJSON, err := json.Marshal(native)
			if err != nil {
				t.Fatalf("marshal typed CEL value: %v", err)
			}
			wantJSON, err := json.Marshal(tt.want)
			if err != nil {
				t.Fatalf("marshal expected value: %v", err)
			}
			if string(gotJSON) != string(wantJSON) {
				t.Fatalf("expected JSON value %s, got %s", wantJSON, gotJSON)
			}
		})
	}
}

func ocsfMapField(t *testing.T, object map[string]any, field string) map[string]any {
	t.Helper()
	value, ok := object[field].(map[string]any)
	if !ok {
		t.Fatalf("expected %s to be an object, got %T", field, object[field])
	}
	return value
}

func ocsfSliceField(t *testing.T, object map[string]any, field string) []any {
	t.Helper()
	value, ok := object[field].([]any)
	if !ok {
		t.Fatalf("expected %s to be an array, got %T", field, object[field])
	}
	return value
}
