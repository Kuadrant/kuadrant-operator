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
	for _, prohibited := range []string{".string_value", ".number_value", ".bool_value", ".list_value", ".struct_value", "base64"} {
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
	if filterConfig["expression"] != `"wasm.kuadrant.ocsf.resource.type" in filter_state && filter_state["wasm.kuadrant.ocsf.resource.type"] != ""` {
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
	if jsonFormat["status_code"] != "%RESPONSE_CODE%" {
		t.Fatalf("expected status code to use Envoy native formatter, got %v", jsonFormat["status_code"])
	}

	api := ocsfMapField(t, jsonFormat, "api")
	if api["operation"] != `%REQ(:METHOD)% %TYPED_CEL("wasm.kuadrant.ocsf.http_request.url.path" in filter_state ? filter_state["wasm.kuadrant.ocsf.http_request.url.path"] : null)%` {
		t.Fatalf("expected query-safe API operation, got %v", api["operation"])
	}
	httpRequest := ocsfMapField(t, jsonFormat, "http_request")
	if httpRequest["version"] != `%TYPED_CEL(request.protocol == "HTTP/1.0" ? "1.0" : request.protocol == "HTTP/1.1" ? "1.1" : request.protocol == "HTTP/2" ? "2.0" : request.protocol == "HTTP/3" ? "3.0" : request.protocol)%` {
		t.Fatalf("expected HTTP version to use CEL-compatible string operations, got %v", httpRequest["version"])
	}
	url := ocsfMapField(t, httpRequest, "url")
	if url["path"] != `%TYPED_CEL("wasm.kuadrant.ocsf.http_request.url.path" in filter_state ? filter_state["wasm.kuadrant.ocsf.http_request.url.path"] : null)%` {
		t.Fatalf("expected query-safe request path, got %v", url["path"])
	}
	actor := ocsfMapField(t, ocsfMapField(t, jsonFormat, "actor"), "user")
	if actor["name"] != `%TYPED_CEL("wasm.kuadrant.ocsf.actor.user.name" in filter_state ? filter_state["wasm.kuadrant.ocsf.actor.user.name"] : null)%` {
		t.Fatalf("expected actor to use the projected identity, got %v", actor["name"])
	}
	if actor["uid"] != `%TYPED_CEL("wasm.kuadrant.ocsf.actor.user.uid" in filter_state ? filter_state["wasm.kuadrant.ocsf.actor.user.uid"] : null)%` {
		t.Fatalf("expected optional actor UID to be safely omitted when absent, got %v", actor["uid"])
	}
	if actor["type_id"] != `%TYPED_CEL("wasm.kuadrant.ocsf.actor.user.name" in filter_state ? (filter_state["wasm.kuadrant.ocsf.actor.user.name"].startsWith("system:serviceaccount:") ? 4 : 1) : null)%` {
		t.Fatalf("expected actor type to be derived only when the identity is present, got %v", actor["type_id"])
	}
	if actor["groups"] != `%TYPED_CEL("wasm.kuadrant.ocsf.actor.user.groups" in filter_state ? filter_state["wasm.kuadrant.ocsf.actor.user.groups"] : null)%` {
		t.Fatalf("expected OCSF groups to use the pre-projected filter-state value, got %v", actor["groups"])
	}

	resources := ocsfSliceField(t, jsonFormat, "resources")
	if len(resources) != 1 {
		t.Fatalf("expected one OCSF target resource, got %d", len(resources))
	}
	resource, ok := resources[0].(map[string]any)
	if !ok {
		t.Fatalf("expected OCSF target resource object, got %T", resources[0])
	}
	for field, key := range map[string]string{
		"name":      "wasm.kuadrant.ocsf.resource.name",
		"namespace": "wasm.kuadrant.ocsf.resource.namespace",
		"type":      "wasm.kuadrant.ocsf.resource.type",
	} {
		want := `%TYPED_CEL("` + key + `" in filter_state ? filter_state["` + key + `"] : null)%`
		if resource[field] != want {
			t.Fatalf("expected resource %s to pass the complete filter-state value to TYPED_CEL, got %v", field, resource[field])
		}
	}
	aiModel := ocsfMapField(t, jsonFormat, "ai_model")
	for field, key := range map[string]string{
		"name":        "wasm.kuadrant.ocsf.resource.name",
		"ai_provider": "wasm.kuadrant.ocsf.ai_model.ai_provider",
	} {
		want := `%TYPED_CEL("` + key + `" in filter_state ? filter_state["` + key + `"] : null)%`
		if aiModel[field] != want {
			t.Fatalf("expected ai_model %s to pass the complete filter-state value to TYPED_CEL, got %v", field, aiModel[field])
		}
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
			formatted := ocsfTypedFilterStateValue(tt.key)
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
