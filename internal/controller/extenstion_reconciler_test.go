//go:build unit

package controllers

import (
	"testing"

	envoygatewayv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/internal/wasm"
	"github.com/kuadrant/policy-machinery/machinery"
	istioclientgoextensionv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "sigs.k8s.io/gateway-api/apis/v1"
)

var (
	testGateway = &machinery.Gateway{
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
		Name               string
		ImageURL           string
		UseImagePullSecret bool
		Assert             func(t *testing.T, plugin *istioclientgoextensionv1alpha1.WasmPlugin)
	}{
		{
			Name:               "ensure image pull secret is set when useImagePullSecret is true",
			ImageURL:           "registry.redhat.io/rhcl-1/wasm-shim-rhel9:latest",
			UseImagePullSecret: true,
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
			Name:               "ensure image pull secret is NOT set when useImagePullSecret is false",
			ImageURL:           WASMFilterImageURL,
			UseImagePullSecret: false,
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
			Name:               "ensure image URL is set correctly regardless of pull secret",
			ImageURL:           "mirror.disconnected.local/kuadrant/wasm-shim:latest",
			UseImagePullSecret: false,
			Assert: func(t *testing.T, plugin *istioclientgoextensionv1alpha1.WasmPlugin) {
				if plugin == nil {
					t.Fatalf("Expected a wasmplugin")
				}
				if plugin.Spec.ImagePullSecret != "" {
					t.Fatalf("Expected wasm plugin to NOT have imagePullSecret but got %v", plugin.Spec.ImagePullSecret)
				}
				if plugin.Spec.Url != "mirror.disconnected.local/kuadrant/wasm-shim:latest" {
					t.Fatalf("Expected wasm plugin URL to be %s but got %s", "mirror.disconnected.local/kuadrant/wasm-shim:latest", plugin.Spec.Url)
				}
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {
			plugin := buildIstioWasmPluginForGateway(testGateway, testWasmConfig, testCase.ImageURL, testCase.UseImagePullSecret)
			testCase.Assert(t, plugin)
		})
	}

}

func Test_buildEnvoyExtensionPolicyForGateway(t *testing.T) {
	testCases := []struct {
		Name               string
		ImageURL           string
		UseImagePullSecret bool
		Assert             func(t *testing.T, policy *envoygatewayv1alpha1.EnvoyExtensionPolicy)
	}{
		{
			Name:               "ensure image pull secret is set when useImagePullSecret is true",
			ImageURL:           "registry.redhat.io/rhcl-1/wasm-shim-rhel9:latest",
			UseImagePullSecret: true,
			Assert: func(t *testing.T, policy *envoygatewayv1alpha1.EnvoyExtensionPolicy) {
				if policy == nil {
					t.Fatalf("Expected an extension policy")
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
			Name:               "ensure image pull secret is NOT set when useImagePullSecret is false",
			ImageURL:           WASMFilterImageURL,
			UseImagePullSecret: false,
			Assert: func(t *testing.T, policy *envoygatewayv1alpha1.EnvoyExtensionPolicy) {
				if policy == nil {
					t.Fatalf("Expected an extension policy")
				}
				for _, w := range policy.Spec.Wasm {
					if w.Code.Image.PullSecretRef != nil {
						t.Fatalf("Expected extension to have not imagePullSecret but got %v", w.Code.Image.PullSecretRef)
					}
				}
			},
		},
		{
			Name:               "ensure image URL is set correctly regardless of pull secret",
			ImageURL:           "mirror.disconnected.local/kuadrant/wasm-shim:latest",
			UseImagePullSecret: false,
			Assert: func(t *testing.T, policy *envoygatewayv1alpha1.EnvoyExtensionPolicy) {
				if policy == nil {
					t.Fatalf("Expected an extension policy")
				}
				for _, w := range policy.Spec.Wasm {
					if w.Code.Image.PullSecretRef != nil {
						t.Fatalf("Expected extension to NOT have imagePullSecret but got %v", w.Code.Image.PullSecretRef)
					}
					if w.Code.Image.URL != "mirror.disconnected.local/kuadrant/wasm-shim:latest" {
						t.Fatalf("Expected extension image URL to be %s but got %s", "mirror.disconnected.local/kuadrant/wasm-shim:latest", w.Code.Image.URL)
					}
				}
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {
			policy := buildEnvoyExtensionPolicyForGateway(testGateway, testWasmConfig, testCase.ImageURL, testCase.UseImagePullSecret)
			testCase.Assert(t, policy)
		})
	}
}
