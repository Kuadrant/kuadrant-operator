//go:build unit

package controllers

import (
	"fmt"
	"testing"

	envoygatewayv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/internal/wasm"
	"github.com/kuadrant/policy-machinery/machinery"
	istioclientgoextensionv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "sigs.k8s.io/gateway-api/apis/v1"
)

var (
	defaultWasmImage  = WASMFilterImageURL
	registry          = "protected.registry.io"
	protectedRegImage = fmt.Sprintf("oci://%s/kuadrant/wasm-shim:latest", registry)
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

func Test_buildIstioWasmPluginForGateway(t *testing.T) {
	testCases := []struct {
		Name                    string
		WASMImageURLS           func() []string
		ProtectedRegistryPrefix string
		Assert                  func(t *testing.T, plugin *istioclientgoextensionv1alpha1.WasmPlugin)
	}{
		{
			Name: "ensure image pull secret is set in wasmPlugin for protected registry",
			WASMImageURLS: func() []string {
				// note currently this is a package global
				return []string{protectedRegImage}
			},
			ProtectedRegistryPrefix: registry,
			Assert: func(t *testing.T, plugin *istioclientgoextensionv1alpha1.WasmPlugin) {
				if plugin == nil {
					t.Fatalf("Expected a wasmplugin")
				}
				if plugin.Spec.ImagePullSecret != RegistryPullSecretName {
					t.Fatalf("Expected wasm plugin to have imagePullSecret %s but got %s", RegistryPullSecretName, plugin.Spec.ImagePullSecret)
				}
			},
		},
		{
			Name: "ensure image pull secret is NOT set in wasmPlugin for unprotected registry",
			WASMImageURLS: func() []string {
				return []string{WASMFilterImageURL}
			},
			Assert: func(t *testing.T, plugin *istioclientgoextensionv1alpha1.WasmPlugin) {
				if plugin == nil {
					t.Fatalf("Expected a wasmplugin")
				}
				if plugin.Spec.ImagePullSecret != "" {
					t.Fatalf("Expected wasm plugin to NOT have imagePullSecret %v", plugin.Spec.ImagePullSecret)
				}
			},
		},
		{
			Name: "ensure image pull secret is set in wasmPlugin for protected registry and unset for unprotected registry",
			WASMImageURLS: func() []string {
				return []string{ProtectedRegistry, WASMFilterImageURL}
			},
			Assert: func(t *testing.T, plugin *istioclientgoextensionv1alpha1.WasmPlugin) {
				if plugin == nil {
					t.Fatalf("Expected a wasmplugin")
				}
				if plugin.Spec.Url == protectedRegImage && plugin.Spec.ImagePullSecret == "" {
					t.Fatalf("Expected wasm plugin to have imagePullSecret set but got none")
				}
				if plugin.Spec.Url == WASMFilterImageURL && plugin.Spec.ImagePullSecret != "" {
					t.Fatalf("Expected wasm plugin to not have imagePullSecret set but got %v", plugin.Spec.ImagePullSecret)
				}
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {
			images := testCase.WASMImageURLS()
			for _, image := range images {
				plugin := buildIstioWasmPluginForGateway(testGateway, testWasmConfig, testCase.ProtectedRegistryPrefix, image)
				testCase.Assert(t, plugin)
			}
		})
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
