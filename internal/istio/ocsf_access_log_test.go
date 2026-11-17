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
	for _, prohibited := range []string{"TYPED_CEL", "envoy.formatter.cel", "json_format", ".string_value", ".number_value", ".bool_value", ".list_value", ".struct_value", "proto.Unmarshal", "protojson", "protowire", "base64"} {
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
	if filterConfig["expression"] != `"wasm.kuadrant.ocsf.event" in filter_state` {
		t.Fatalf("expected logger to select only OCSF policy requests, got %v", filterConfig["expression"])
	}
	logFormat := ocsfMapField(t, ocsfMapField(t, logger, "typed_config"), "log_format")
	textFormat := ocsfMapField(t, logFormat, "text_format_source")
	if textFormat["inline_string"] != "%FILTER_STATE(wasm.kuadrant.ocsf.event:PLAIN)%\n" {
		t.Fatalf("expected one raw OCSF JSON filter-state line, got %v", textFormat["inline_string"])
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
