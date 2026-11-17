//go:build unit

package controllers

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	envoygatewayv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/internal/wasm"
	"github.com/kuadrant/policy-machinery/machinery"
	istioapinetworkingv1alpha3 "istio.io/api/networking/v1alpha3"
	istioclientgonetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "sigs.k8s.io/gateway-api/apis/v1"
)

var (
	defaultWasmImage  = WASMFilterImageURL
	registry          = "protected.registry.io"
	protectedRegImage = fmt.Sprintf("%s/kuadrant/wasm-shim:latest", registry)
	testGateway       = &machinery.Gateway{
		Gateway: &v1.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "test",
			},
		},
	}
	testWasmConfig = wasm.Config{
		ActionSets: []wasm.ActionSet{
			{
				Name: "test",
			},
		},
	}
)

// extractImagePullSecretFromEnvoyFilter extracts the image pull secret from EnvoyFilter's nested structure
func extractImagePullSecretFromEnvoyFilter(ef *istioclientgonetworkingv1alpha3.EnvoyFilter) string {
	if len(ef.Spec.ConfigPatches) == 0 {
		return ""
	}

	patchValue := ef.Spec.ConfigPatches[0].Patch.Value
	if patchValue == nil {
		return ""
	}

	valueJSON, err := patchValue.MarshalJSON()
	if err != nil {
		return ""
	}

	var filterConfig map[string]any
	if err := json.Unmarshal(valueJSON, &filterConfig); err != nil {
		return ""
	}

	// Navigate: typed_config -> value -> config -> vm_config -> code -> remote -> image_pull_secret
	typedConfig, ok := filterConfig["typed_config"].(map[string]any)
	if !ok {
		return ""
	}

	value, ok := typedConfig["value"].(map[string]any)
	if !ok {
		return ""
	}

	config, ok := value["config"].(map[string]any)
	if !ok {
		return ""
	}

	vmConfig, ok := config["vm_config"].(map[string]any)
	if !ok {
		return ""
	}

	code, ok := vmConfig["code"].(map[string]any)
	if !ok {
		return ""
	}

	remote, ok := code["remote"].(map[string]any)
	if !ok {
		return ""
	}

	imagePullSecret, ok := remote["image_pull_secret"].(string)
	if !ok {
		return ""
	}

	return imagePullSecret
}

// extractImageURLFromEnvoyFilter extracts the image URL from EnvoyFilter's nested structure
func extractImageURLFromEnvoyFilter(ef *istioclientgonetworkingv1alpha3.EnvoyFilter) string {
	if len(ef.Spec.ConfigPatches) == 0 {
		return ""
	}

	patchValue := ef.Spec.ConfigPatches[0].Patch.Value
	if patchValue == nil {
		return ""
	}

	valueJSON, err := patchValue.MarshalJSON()
	if err != nil {
		return ""
	}

	var filterConfig map[string]any
	if err := json.Unmarshal(valueJSON, &filterConfig); err != nil {
		return ""
	}

	// Navigate: typed_config -> value -> config -> vm_config -> code -> remote -> http_uri -> uri
	typedConfig, ok := filterConfig["typed_config"].(map[string]any)
	if !ok {
		return ""
	}

	value, ok := typedConfig["value"].(map[string]any)
	if !ok {
		return ""
	}

	config, ok := value["config"].(map[string]any)
	if !ok {
		return ""
	}

	vmConfig, ok := config["vm_config"].(map[string]any)
	if !ok {
		return ""
	}

	code, ok := vmConfig["code"].(map[string]any)
	if !ok {
		return ""
	}

	remote, ok := code["remote"].(map[string]any)
	if !ok {
		return ""
	}

	httpURI, ok := remote["http_uri"].(map[string]any)
	if !ok {
		return ""
	}

	uri, ok := httpURI["uri"].(string)
	if !ok {
		return ""
	}

	return uri
}

func Test_buildIstioEnvoyFilterForGateway(t *testing.T) {
	wasmURL := "http://kuadrant-operator-wasm.kuadrant-system.svc.cluster.local:8082/plugin.wasm"

	t.Run("ensure wasm URL is set in envoyfilter", func(t *testing.T) {
		envoyFilter, err := buildIstioEnvoyFilterForGateway(testGateway, testWasmConfig, wasmURL, "kuadrant-operator-wasm.kuadrant-system.svc.cluster.local", 8082, "")
		if err != nil {
			t.Fatalf("failed to build envoyfilter: %v", err)
		}
		if envoyFilter == nil {
			t.Fatalf("Expected an envoyfilter")
		}
		imageURL := extractImageURLFromEnvoyFilter(envoyFilter)
		if imageURL != wasmURL {
			t.Fatalf("Expected envoyfilter URI to be %s but got %s", wasmURL, imageURL)
		}
	})

	t.Run("adds OCSF access logger for exported OCSF filter state", func(t *testing.T) {
		config := wasm.Config{ActionSets: []wasm.ActionSet{{
			Name: "ocsf",
			Actions: []wasm.Action{&wasm.StoreAction{
				Path:         wasm.OCSFEventStorePath,
				ExportToHost: true,
				ExportFormat: wasm.StoreExportFormatJSON,
			}},
		}}}

		envoyFilter, err := buildIstioEnvoyFilterForGateway(testGateway, config, wasmURL, "kuadrant-operator-wasm.kuadrant-system.svc.cluster.local", 8082, "")
		if err != nil {
			t.Fatalf("failed to build envoyfilter: %v", err)
		}
		var accessLogPatches int
		for _, patch := range envoyFilter.Spec.ConfigPatches {
			if patch.ApplyTo == istioapinetworkingv1alpha3.EnvoyFilter_NETWORK_FILTER {
				accessLogPatches++
			}
		}
		if accessLogPatches != 1 {
			t.Fatalf("expected one OCSF access-log patch, got %d", accessLogPatches)
		}
	})

	t.Run("does not add OCSF access logger without exported OCSF filter state", func(t *testing.T) {
		envoyFilter, err := buildIstioEnvoyFilterForGateway(testGateway, testWasmConfig, wasmURL, "kuadrant-operator-wasm.kuadrant-system.svc.cluster.local", 8082, "")
		if err != nil {
			t.Fatalf("failed to build envoyfilter: %v", err)
		}
		for _, patch := range envoyFilter.Spec.ConfigPatches {
			if patch.ApplyTo == istioapinetworkingv1alpha3.EnvoyFilter_NETWORK_FILTER {
				t.Fatal("unexpected OCSF access-log patch")
			}
		}
	})
}

func Test_buildIstioEnvoyFilterForGateway_includesOCSFEventShapeWhenEnabled(t *testing.T) {
	config := wasm.Config{ActionSets: []wasm.ActionSet{{
		Name: "ocsf",
		Actions: []wasm.Action{
			&wasm.StoreAction{
				Path:         wasm.OCSFEventStorePath,
				Value:        `{"api": {"operation": "GET /tenant/model/get"}}`,
				ExportToHost: true,
				ExportFormat: wasm.StoreExportFormatJSON,
			},
		},
	}}}

	envoyFilter, err := buildIstioEnvoyFilterForGateway(testGateway, config, "http://wasm.example/plugin.wasm", "wasm.example", 8082, "")
	if err != nil {
		t.Fatalf("failed to build EnvoyFilter: %v", err)
	}

	var accessLogPatch *istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch
	for _, patch := range envoyFilter.Spec.ConfigPatches {
		if patch.ApplyTo != istioapinetworkingv1alpha3.EnvoyFilter_NETWORK_FILTER {
			continue
		}
		if accessLogPatch != nil {
			t.Fatal("expected exactly one OCSF access-log patch")
		}
		accessLogPatch = patch
	}
	if accessLogPatch == nil || accessLogPatch.Patch == nil || accessLogPatch.Patch.Value == nil {
		t.Fatal("expected OCSF access-log patch when OCSF filter state is exported")
	}

	raw, err := accessLogPatch.Patch.Value.MarshalJSON()
	if err != nil {
		t.Fatalf("failed to marshal OCSF access-log patch: %v", err)
	}
	var patchValue map[string]any
	if err := json.Unmarshal(raw, &patchValue); err != nil {
		t.Fatalf("failed to decode OCSF access-log patch: %v", err)
	}

	mapField := func(object map[string]any, field string) map[string]any {
		t.Helper()
		value, ok := object[field].(map[string]any)
		if !ok {
			t.Fatalf("expected %q to be an object, got %T", field, object[field])
		}
		return value
	}
	sliceField := func(object map[string]any, field string) []any {
		t.Helper()
		value, ok := object[field].([]any)
		if !ok {
			t.Fatalf("expected %q to be an array, got %T", field, object[field])
		}
		return value
	}

	httpConnectionManager := mapField(patchValue, "typed_config")
	accessLogs := sliceField(httpConnectionManager, "access_log")
	if len(accessLogs) != 1 {
		t.Fatalf("expected one access logger, got %d", len(accessLogs))
	}
	loggerObject, ok := accessLogs[0].(map[string]any)
	if !ok {
		t.Fatalf("expected access logger object, got %T", accessLogs[0])
	}
	logger := mapField(loggerObject, "typed_config")
	logFormat := mapField(logger, "log_format")
	textFormat := mapField(logFormat, "text_format_source")
	if textFormat["inline_string"] != "%FILTER_STATE(wasm.kuadrant.ocsf.event:PLAIN)%\n" {
		t.Fatalf("expected raw OCSF JSON filter-state log line, got %v", textFormat["inline_string"])
	}
	if strings.Contains(string(raw), "TYPED_CEL") || strings.Contains(string(raw), "json_format") || strings.Contains(string(raw), "envoy.formatter.cel") {
		t.Fatalf("OCSF access log patch must not use typed CEL or json_format: %s", raw)
	}
}

func Test_buildEnvoyExtensionPolicyForGateway(t *testing.T) {
	testCases := []struct {
		Name                    string
		WASMImageURLS           func() []string
		ProtectedRegistryPrefix string
		Assert                  func(t *testing.T, policy *envoygatewayv1alpha1.EnvoyExtensionPolicy)
	}{
		{
			Name: "ensure image pull secret is set in ExtensionPolicy for protected registry",
			WASMImageURLS: func() []string {
				return []string{protectedRegImage}
			},
			ProtectedRegistryPrefix: registry,
			Assert: func(t *testing.T, policy *envoygatewayv1alpha1.EnvoyExtensionPolicy) {
				if policy == nil {
					t.Fatalf("Expected a wasmplugin")
				}
				for _, w := range policy.Spec.Wasm {
					if w.Code.Image.PullSecretRef == nil {
						t.Fatalf("Expected extension to have imagePullSecret %v but no pullSecretRef", RegistryPullSecretName)
					}
					if w.Code.Image.PullSecretRef.Name != v1.ObjectName(RegistryPullSecretName) {
						t.Fatalf("expected the pull secret name to be %s but got %v", RegistryPullSecretName, w.Code.Image.PullSecretRef.Name)
					}
				}
			},
		},
		{
			Name: "ensure image pull secret is NOT set in wasmPlugin for unprotected registry",
			WASMImageURLS: func() []string {
				return []string{defaultWasmImage}
			},
			Assert: func(t *testing.T, policy *envoygatewayv1alpha1.EnvoyExtensionPolicy) {
				if policy == nil {
					t.Fatalf("Expected a wasmplugin")
				}
				for _, w := range policy.Spec.Wasm {
					if w.Code.Image.PullSecretRef != nil {
						t.Fatalf("Expected extension to have not imagePullSecret but got %v", w.Code.Image.PullSecretRef)
					}
				}
			},
		},
		{
			Name: "ensure image pull secret is set in extension for protected registry and unset for unprotected registry",
			WASMImageURLS: func() []string {
				return []string{ProtectedRegistry, WASMFilterImageURL}
			},
			Assert: func(t *testing.T, policy *envoygatewayv1alpha1.EnvoyExtensionPolicy) {
				if policy == nil {
					t.Fatalf("Expected a wasmplugin")
				}
				for _, w := range policy.Spec.Wasm {
					if w.Code.Image.PullSecretRef == nil && w.Code.Image.URL == protectedRegImage {
						t.Fatalf("Expected policy to have imagePullSecret set but got none")
					}
					if w.Code.Image.PullSecretRef != nil && w.Code.Image.URL == WASMFilterImageURL {
						t.Fatalf("Expected policy to not have imagePullSecret set but got %v", w.Code.Image.PullSecretRef)
					}
				}

			},
		},
	}

	for _, testCase := range testCases {
		images := testCase.WASMImageURLS()
		for _, image := range images {
			policy := buildEnvoyExtensionPolicyForGateway(testGateway, testWasmConfig, testCase.ProtectedRegistryPrefix, image)
			testCase.Assert(t, policy)
		}
	}
}
