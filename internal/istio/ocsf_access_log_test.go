//go:build unit

package istio

import (
	"encoding/json"
	"strings"
	"testing"

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
	for _, prohibited := range []string{"typed_json_format", "text_format_source", "FILTER_STATE(", ":PLAIN)", "proto.Unmarshal", "protojson", "protowire", "base64", ".struct_value", ".number_value", ".string_value", ".bool_value", ".list_value"} {
		if strings.Contains(string(raw), prohibited) {
			t.Fatalf("OCSF formatter must use direct typed CEL projections in json_format; found %q", prohibited)
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
	if filterConfig["expression"] != `"wasm.kuadrant.ocsf.event" in filter_state` {
		t.Fatalf("expected logger to select only OCSF policy requests, got %v", filterConfig["expression"])
	}

	logFormat := ocsfMapField(t, ocsfMapField(t, logger, "typed_config"), "log_format")
	jsonFormat := ocsfMapField(t, logFormat, "json_format")
	field := func(parts ...string) string {
		return ocsfTypedCEL(ocsfEventFieldPath(parts...))
	}
	assertFormat := func(key string, want string) {
		t.Helper()
		if got := jsonFormat[key]; got != want {
			t.Fatalf("%s formatter = %v, want %q", key, got, want)
		}
	}
	assertFormat("class_uid", field("class_uid"))
	assertFormat("time", ocsfTypedCEL(`(request.time != null ? int(request.time) * 1000 + request.time.getMilliseconds() : 0)`))
	metadata := ocsfMapField(t, jsonFormat, "metadata")
	if got, want := metadata["logged_time"], ocsfTypedCEL(`(request.time != null && request.duration != null ? int(request.time + request.duration) * 1000 + (request.time + request.duration).getMilliseconds() : 0)`); got != want {
		t.Fatalf("metadata.logged_time formatter = %v, want %q", got, want)
	}
	httpResponse := ocsfMapField(t, jsonFormat, "http_response")
	for key, want := range map[string]string{
		"code":        ocsfTypedCEL(`(response.code != null ? response.code : 0)`),
		"body_length": ocsfTypedCEL(`(response.size != null ? response.size : 0)`),
		"latency":     ocsfTypedCEL(`(request.duration != null ? request.duration.getMilliseconds() : 0)`),
	} {
		if got := httpResponse[key]; got != want {
			t.Fatalf("http_response.%s formatter = %v, want %q", key, got, want)
		}
	}
	assertFormat("status_id", ocsfTypedCEL(`((response.code != null && response.code >= 200 && response.code < 300)) ? 1 : 2`))
	assertFormat("status", ocsfTypedCEL(`((response.code != null && response.code >= 200 && response.code < 300)) ? "Success" : "Failure"`))
	assertFormat("status_code", ocsfTypedCEL(`response.headers[":status"]`))
	if got, want := ocsfMapField(t, jsonFormat, "unmapped")["kuadrant_response_succeeded"], ocsfTypedCEL(`(response.code != null && response.code >= 200 && response.code < 300)`); got != want {
		t.Fatalf("response success formatter = %v, want direct boolean projection %q", got, want)
	}
	if got, want := ocsfMapField(t, jsonFormat, "actor")["user"], field("actor", "user"); got != want {
		t.Fatalf("actor.user formatter = %v, want direct object projection %q", got, want)
	}
	if got, want := jsonFormat["resources"], field("resources"); got != want {
		t.Fatalf("resources formatter = %v, want direct list projection %q", got, want)
	}
	if got := ocsfSliceField(t, metadata, "profiles"); len(got) != 1 || got[0] != "ai_operation" {
		t.Fatalf("metadata.profiles formatter = %v, want [ai_operation]", got)
	}
	if strings.Count(string(raw), "TYPED_CEL(") < 15 {
		t.Fatalf("expected typed CEL projection for the OCSF fields, found %d", strings.Count(string(raw), "TYPED_CEL("))
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
