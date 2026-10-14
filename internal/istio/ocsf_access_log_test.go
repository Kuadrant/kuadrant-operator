//go:build unit

package istio

import (
	"encoding/json"
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
	if filterConfig["expression"] != `"wasm.kuadrant.ocsf.resource.type" in filter_state` {
		t.Fatalf("expected logger to select only OCSF policy requests, got %v", filterConfig["expression"])
	}
	logFormat := ocsfMapField(t, ocsfMapField(t, logger, "typed_config"), "log_format")
	if logFormat["omit_empty_values"] != true {
		t.Fatal("expected empty optional OCSF values to be omitted")
	}
	jsonFormat := ocsfMapField(t, logFormat, "typed_json_format")
	if jsonFormat["class_uid"] != float64(6003) {
		t.Fatalf("expected OCSF API Activity class_uid, got %v", jsonFormat["class_uid"])
	}

	api := ocsfMapField(t, jsonFormat, "api")
	if api["operation"] != "%REQ(:METHOD)% %REQ_WITHOUT_QUERY(:PATH)%" {
		t.Fatalf("expected query-safe API operation, got %v", api["operation"])
	}
	httpRequest := ocsfMapField(t, jsonFormat, "http_request")
	url := ocsfMapField(t, httpRequest, "url")
	if url["path"] != "%REQ_WITHOUT_QUERY(:PATH)%" {
		t.Fatalf("expected query-safe request path, got %v", url["path"])
	}
	actor := ocsfMapField(t, ocsfMapField(t, jsonFormat, "actor"), "user")
	if actor["name"] != "%TYPED_CEL(\"wasm.kuadrant.ocsf.actor.user.name\" in filter_state && filter_state[\"wasm.kuadrant.ocsf.actor.user.name\"] != \"\" ? filter_state[\"wasm.kuadrant.ocsf.actor.user.name\"] : \"anonymous\")%" {
		t.Fatalf("expected actor to use the projected identity or anonymous, got %v", actor["name"])
	}
	if actor["groups"] != `%TYPED_CEL("wasm.kuadrant.ocsf.actor.user.groups" in filter_state ? filter_state["wasm.kuadrant.ocsf.actor.user.groups"].map(group, {"name": group}) : [])%` {
		t.Fatalf("expected OCSF groups to be emitted as group objects, got %v", actor["groups"])
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
