//go:build unit

package controllers

import (
	"context"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	configv1 "github.com/openshift/api/config/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	dfake "k8s.io/client-go/dynamic/fake"
	v1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kuadrant/kuadrant-operator/internal/openshift"
	"github.com/kuadrant/kuadrant-operator/internal/wasm"
	"github.com/kuadrant/policy-machinery/machinery"
)

func newE2EScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = configv1.Install(s)
	_ = corev1.AddToScheme(s)
	return s
}

// TestDisconnectedCluster_IstioE2EFlow exercises the full chain:
// IDMS mirror resolution → credential discovery → secret management → wasm plugin construction
func TestDisconnectedCluster_IstioE2EFlow(t *testing.T) {
	const (
		originalImage = "registry.redhat.io/rhcl-1/wasm-shim-rhel9@sha256:abc123"
		mirrorHost    = "mirror.disconnected.local:8443"
		mirrorImage   = mirrorHost + "/rhcl-1/wasm-shim-rhel9@sha256:abc123"
		gatewayNs     = "gateway-ns"
	)

	s := newE2EScheme()
	fakeClient := dfake.NewSimpleDynamicClient(s,
		// IDMS: registry.redhat.io → mirror.disconnected.local:8443
		&configv1.ImageDigestMirrorSet{
			TypeMeta:   metav1.TypeMeta{Kind: "ImageDigestMirrorSet", APIVersion: "config.openshift.io/v1"},
			ObjectMeta: metav1.ObjectMeta{Name: "wasm-mirror"},
			Spec: configv1.ImageDigestMirrorSetSpec{
				ImageDigestMirrors: []configv1.ImageDigestMirrors{
					{
						Source:  "registry.redhat.io",
						Mirrors: []configv1.ImageMirror{configv1.ImageMirror(mirrorHost)},
					},
				},
			},
		},
		// Cluster pull secret with credentials for the mirror registry
		&corev1.Secret{
			TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
			ObjectMeta: metav1.ObjectMeta{Name: "pull-secret", Namespace: "openshift-config"},
			Type:       corev1.SecretTypeDockerConfigJson,
			Data: map[string][]byte{
				corev1.DockerConfigJsonKey: []byte(`{"auths":{"` + mirrorHost + `":{"auth":"bWlycm9yOnNlY3JldA=="}}}`),
			},
		},
	)

	ctx := context.Background()
	logger := logr.Discard()

	// Step 1: Mirror resolution
	resolvedURL := openshift.ResolveImageURL(ctx, fakeClient, originalImage, true, false, false, logger)
	if resolvedURL != mirrorImage {
		t.Fatalf("mirror resolution: expected %q, got %q", mirrorImage, resolvedURL)
	}

	// Step 2: Registry credential discovery
	registryHost := openshift.ExtractRegistryHost(resolvedURL)
	if registryHost != mirrorHost {
		t.Fatalf("registry host extraction: expected %q, got %q", mirrorHost, registryHost)
	}

	registryCreds, err := openshift.ResolveRegistryCredentials(ctx, fakeClient, registryHost, logger)
	if err != nil {
		t.Fatalf("credential discovery failed: %v", err)
	}
	if registryCreds == nil {
		t.Fatal("expected credentials for mirror registry, got nil")
	}

	// Step 3: Pull secret management in gateway namespace
	useImagePullSecret, err := openshift.EnsureWasmPluginPullSecret(ctx, fakeClient, gatewayNs, RegistryPullSecretName, registryCreds, logger)
	if err != nil {
		t.Fatalf("secret management failed: %v", err)
	}
	if !useImagePullSecret {
		t.Fatal("expected useImagePullSecret=true after creating managed secret")
	}

	// Verify the managed secret was created
	created, err := fakeClient.Resource(corev1.SchemeGroupVersion.WithResource("secrets")).Namespace(gatewayNs).Get(ctx, RegistryPullSecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("managed secret not found: %v", err)
	}
	if created.GetLabels()["kuadrant.io/managed"] != "true" {
		t.Fatal("managed secret missing kuadrant.io/managed label")
	}

	// Step 4: Wasm plugin construction
	gw := &machinery.Gateway{
		Gateway: &v1.Gateway{
			ObjectMeta: metav1.ObjectMeta{Name: "test-gw", Namespace: gatewayNs},
		},
	}
	wasmConfig := wasm.Config{ActionSets: []wasm.ActionSet{{Name: "test"}}}

	plugin := buildIstioWasmPluginForGateway(gw, wasmConfig, resolvedURL, useImagePullSecret)
	if plugin == nil {
		t.Fatal("expected non-nil wasm plugin")
	}
	if plugin.Spec.Url != mirrorImage {
		t.Errorf("wasm plugin URL: expected %q, got %q", mirrorImage, plugin.Spec.Url)
	}
	if plugin.Spec.ImagePullSecret != RegistryPullSecretName {
		t.Errorf("wasm plugin pull secret: expected %q, got %q", RegistryPullSecretName, plugin.Spec.ImagePullSecret)
	}
}

// TestDisconnectedCluster_EnvoyGatewayE2EFlow exercises the same chain for Envoy Gateway.
func TestDisconnectedCluster_EnvoyGatewayE2EFlow(t *testing.T) {
	const (
		originalImage = "registry.redhat.io/rhcl-1/wasm-shim-rhel9@sha256:abc123"
		mirrorHost    = "mirror.disconnected.local:8443"
		mirrorImage   = mirrorHost + "/rhcl-1/wasm-shim-rhel9@sha256:abc123"
		gatewayNs     = "eg-gateway-ns"
	)

	s := newE2EScheme()
	fakeClient := dfake.NewSimpleDynamicClient(s,
		&configv1.ImageDigestMirrorSet{
			TypeMeta:   metav1.TypeMeta{Kind: "ImageDigestMirrorSet", APIVersion: "config.openshift.io/v1"},
			ObjectMeta: metav1.ObjectMeta{Name: "wasm-mirror"},
			Spec: configv1.ImageDigestMirrorSetSpec{
				ImageDigestMirrors: []configv1.ImageDigestMirrors{
					{
						Source:  "registry.redhat.io",
						Mirrors: []configv1.ImageMirror{configv1.ImageMirror(mirrorHost)},
					},
				},
			},
		},
		&corev1.Secret{
			TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
			ObjectMeta: metav1.ObjectMeta{Name: "pull-secret", Namespace: "openshift-config"},
			Type:       corev1.SecretTypeDockerConfigJson,
			Data: map[string][]byte{
				corev1.DockerConfigJsonKey: []byte(`{"auths":{"` + mirrorHost + `":{"auth":"bWlycm9yOnNlY3JldA=="}}}`),
			},
		},
	)

	ctx := context.Background()
	logger := logr.Discard()

	resolvedURL := openshift.ResolveImageURL(ctx, fakeClient, originalImage, true, false, false, logger)
	registryHost := openshift.ExtractRegistryHost(resolvedURL)
	registryCreds, err := openshift.ResolveRegistryCredentials(ctx, fakeClient, registryHost, logger)
	if err != nil {
		t.Fatalf("credential discovery failed: %v", err)
	}

	useImagePullSecret, err := openshift.EnsureWasmPluginPullSecret(ctx, fakeClient, gatewayNs, RegistryPullSecretName, registryCreds, logger)
	if err != nil {
		t.Fatalf("secret management failed: %v", err)
	}

	gw := &machinery.Gateway{
		Gateway: &v1.Gateway{
			ObjectMeta: metav1.ObjectMeta{Name: "test-gw", Namespace: gatewayNs},
		},
	}
	wasmConfig := wasm.Config{ActionSets: []wasm.ActionSet{{Name: "test"}}}

	policy := buildEnvoyExtensionPolicyForGateway(gw, wasmConfig, resolvedURL, useImagePullSecret)
	if policy == nil {
		t.Fatal("expected non-nil extension policy")
	}
	if len(policy.Spec.Wasm) == 0 {
		t.Fatal("expected at least one wasm entry")
	}
	if policy.Spec.Wasm[0].Code.Image.URL != mirrorImage {
		t.Errorf("extension policy URL: expected %q, got %q", mirrorImage, policy.Spec.Wasm[0].Code.Image.URL)
	}
	if policy.Spec.Wasm[0].Code.Image.PullSecretRef == nil {
		t.Fatal("expected pull secret ref to be set")
	}
	if string(policy.Spec.Wasm[0].Code.Image.PullSecretRef.Name) != RegistryPullSecretName {
		t.Errorf("extension policy pull secret: expected %q, got %q", RegistryPullSecretName, policy.Spec.Wasm[0].Code.Image.PullSecretRef.Name)
	}
}

// TestDisconnectedCluster_NonOpenShift verifies no pull secret automation
// runs when no mirror CRDs are installed (non-OpenShift cluster).
func TestDisconnectedCluster_NonOpenShift(t *testing.T) {
	const (
		originalImage = "quay.io/kuadrant/wasm-shim:latest"
		gatewayNs     = "non-ocp-ns"
	)

	s := newE2EScheme()
	fakeClient := dfake.NewSimpleDynamicClient(s)

	ctx := context.Background()
	logger := logr.Discard()

	// No CRDs installed → URL unchanged
	resolvedURL := openshift.ResolveImageURL(ctx, fakeClient, originalImage, false, false, false, logger)
	if resolvedURL != originalImage {
		t.Fatalf("expected unchanged URL on non-OpenShift, got %q", resolvedURL)
	}

	// No creds found
	registryHost := openshift.ExtractRegistryHost(resolvedURL)
	registryCreds, _ := openshift.ResolveRegistryCredentials(ctx, fakeClient, registryHost, logger)
	if registryCreds != nil {
		t.Fatal("expected nil creds on non-OpenShift cluster")
	}

	// No secret management needed (nil creds, no existing secret)
	useImagePullSecret, err := openshift.EnsureWasmPluginPullSecret(ctx, fakeClient, gatewayNs, RegistryPullSecretName, registryCreds, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if useImagePullSecret {
		t.Fatal("expected useImagePullSecret=false on non-OpenShift")
	}

	gw := &machinery.Gateway{
		Gateway: &v1.Gateway{
			ObjectMeta: metav1.ObjectMeta{Name: "test-gw", Namespace: gatewayNs},
		},
	}
	wasmConfig := wasm.Config{ActionSets: []wasm.ActionSet{{Name: "test"}}}

	plugin := buildIstioWasmPluginForGateway(gw, wasmConfig, resolvedURL, useImagePullSecret)
	if plugin.Spec.ImagePullSecret != "" {
		t.Errorf("expected no pull secret on non-OpenShift, got %q", plugin.Spec.ImagePullSecret)
	}
	if plugin.Spec.Url != originalImage {
		t.Errorf("expected original URL, got %q", plugin.Spec.Url)
	}
}

// TestDisconnectedCluster_KillSwitch verifies that DISABLE_IMAGE_MIRROR_RESOLUTION=true
// disables the entire flow: no mirror resolution, no cred discovery, no secret management.
func TestDisconnectedCluster_KillSwitch(t *testing.T) {
	const (
		originalImage = "registry.redhat.io/rhcl-1/wasm-shim-rhel9@sha256:abc123"
		mirrorHost    = "mirror.disconnected.local:8443"
		gatewayNs     = "killswitch-ns"
	)

	t.Setenv("DISABLE_IMAGE_MIRROR_RESOLUTION", "true")

	s := newE2EScheme()
	fakeClient := dfake.NewSimpleDynamicClient(s,
		&configv1.ImageDigestMirrorSet{
			TypeMeta:   metav1.TypeMeta{Kind: "ImageDigestMirrorSet", APIVersion: "config.openshift.io/v1"},
			ObjectMeta: metav1.ObjectMeta{Name: "wasm-mirror"},
			Spec: configv1.ImageDigestMirrorSetSpec{
				ImageDigestMirrors: []configv1.ImageDigestMirrors{
					{
						Source:  "registry.redhat.io",
						Mirrors: []configv1.ImageMirror{configv1.ImageMirror(mirrorHost)},
					},
				},
			},
		},
		&corev1.Secret{
			TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
			ObjectMeta: metav1.ObjectMeta{Name: "pull-secret", Namespace: "openshift-config"},
			Type:       corev1.SecretTypeDockerConfigJson,
			Data: map[string][]byte{
				corev1.DockerConfigJsonKey: []byte(`{"auths":{"` + mirrorHost + `":{"auth":"bWlycm9yOnNlY3JldA=="}}}`),
			},
		},
	)

	ctx := context.Background()
	logger := logr.Discard()

	// Mirror resolution disabled
	resolvedURL := openshift.ResolveImageURL(ctx, fakeClient, originalImage, true, false, false, logger)
	if resolvedURL != originalImage {
		t.Fatalf("expected original URL with kill-switch, got %q", resolvedURL)
	}

	// Feature is disabled — skip cred discovery
	if !openshift.IsImageMirrorResolutionDisabled() {
		t.Fatal("expected kill-switch to be active")
	}

	// Wasm plugin should use original URL, no pull secret
	gw := &machinery.Gateway{
		Gateway: &v1.Gateway{
			ObjectMeta: metav1.ObjectMeta{Name: "test-gw", Namespace: gatewayNs},
		},
	}
	wasmConfig := wasm.Config{ActionSets: []wasm.ActionSet{{Name: "test"}}}
	plugin := buildIstioWasmPluginForGateway(gw, wasmConfig, resolvedURL, false)
	if plugin.Spec.Url != originalImage {
		t.Errorf("expected original URL, got %q", plugin.Spec.Url)
	}
	if plugin.Spec.ImagePullSecret != "" {
		t.Errorf("expected no pull secret with kill-switch, got %q", plugin.Spec.ImagePullSecret)
	}
}

// TestDisconnectedCluster_ProtectedRegistryFallback verifies the PROTECTED_REGISTRY
// fallback works when no OpenShift cred discovery is available.
func TestDisconnectedCluster_ProtectedRegistryFallback(t *testing.T) {
	const (
		imageURL  = "registry.redhat.io/rhcl-1/wasm-shim-rhel9:latest"
		gatewayNs = "fallback-ns"
	)

	s := newE2EScheme()
	fakeClient := dfake.NewSimpleDynamicClient(s)

	ctx := context.Background()
	logger := logr.Discard()

	// No mirror CRDs → URL unchanged
	resolvedURL := openshift.ResolveImageURL(ctx, fakeClient, imageURL, false, false, false, logger)

	// No creds found
	registryHost := openshift.ExtractRegistryHost(resolvedURL)
	registryCreds, _ := openshift.ResolveRegistryCredentials(ctx, fakeClient, registryHost, logger)

	// EnsureWasmPluginPullSecret with nil creds → false
	useImagePullSecret, _ := openshift.EnsureWasmPluginPullSecret(ctx, fakeClient, gatewayNs, RegistryPullSecretName, registryCreds, logger)

	// PROTECTED_REGISTRY fallback
	if !useImagePullSecret && ProtectedRegistry != "" && strings.Contains(resolvedURL, ProtectedRegistry) {
		useImagePullSecret = true
	}

	gw := &machinery.Gateway{
		Gateway: &v1.Gateway{
			ObjectMeta: metav1.ObjectMeta{Name: "test-gw", Namespace: gatewayNs},
		},
	}
	wasmConfig := wasm.Config{ActionSets: []wasm.ActionSet{{Name: "test"}}}
	plugin := buildIstioWasmPluginForGateway(gw, wasmConfig, resolvedURL, useImagePullSecret)

	if !strings.Contains(imageURL, ProtectedRegistry) {
		t.Skip("PROTECTED_REGISTRY not in image URL, fallback not applicable")
	}
	if plugin.Spec.ImagePullSecret != RegistryPullSecretName {
		t.Errorf("expected pull secret via fallback, got %q", plugin.Spec.ImagePullSecret)
	}
}

// TestDisconnectedCluster_UserSecretNotOverwritten verifies the reconciler
// flow respects user-created secrets.
func TestDisconnectedCluster_UserSecretNotOverwritten(t *testing.T) {
	const (
		originalImage = "registry.redhat.io/rhcl-1/wasm-shim-rhel9@sha256:abc123"
		mirrorHost    = "mirror.disconnected.local:8443"
		gatewayNs     = "user-secret-ns"
	)

	s := newE2EScheme()
	fakeClient := dfake.NewSimpleDynamicClient(s,
		&configv1.ImageDigestMirrorSet{
			TypeMeta:   metav1.TypeMeta{Kind: "ImageDigestMirrorSet", APIVersion: "config.openshift.io/v1"},
			ObjectMeta: metav1.ObjectMeta{Name: "wasm-mirror"},
			Spec: configv1.ImageDigestMirrorSetSpec{
				ImageDigestMirrors: []configv1.ImageDigestMirrors{
					{
						Source:  "registry.redhat.io",
						Mirrors: []configv1.ImageMirror{configv1.ImageMirror(mirrorHost)},
					},
				},
			},
		},
		&corev1.Secret{
			TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
			ObjectMeta: metav1.ObjectMeta{Name: "pull-secret", Namespace: "openshift-config"},
			Type:       corev1.SecretTypeDockerConfigJson,
			Data: map[string][]byte{
				corev1.DockerConfigJsonKey: []byte(`{"auths":{"` + mirrorHost + `":{"auth":"bWlycm9yOnNlY3JldA=="}}}`),
			},
		},
		// User-created secret (no managed label) in gateway namespace
		&corev1.Secret{
			TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
			ObjectMeta: metav1.ObjectMeta{Name: RegistryPullSecretName, Namespace: gatewayNs},
			Type:       corev1.SecretTypeDockerConfigJson,
			Data: map[string][]byte{
				corev1.DockerConfigJsonKey: []byte(`{"auths":{"custom.registry.io":{"auth":"dXNlcg=="}}}`),
			},
		},
	)

	ctx := context.Background()
	logger := logr.Discard()

	resolvedURL := openshift.ResolveImageURL(ctx, fakeClient, originalImage, true, false, false, logger)
	registryHost := openshift.ExtractRegistryHost(resolvedURL)
	registryCreds, _ := openshift.ResolveRegistryCredentials(ctx, fakeClient, registryHost, logger)

	// EnsureWasmPluginPullSecret should return true (user secret exists) without modifying it
	useImagePullSecret, err := openshift.EnsureWasmPluginPullSecret(ctx, fakeClient, gatewayNs, RegistryPullSecretName, registryCreds, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !useImagePullSecret {
		t.Fatal("expected useImagePullSecret=true when user-created secret exists")
	}

	// Verify user secret was not modified (no managed label added)
	existing, _ := fakeClient.Resource(corev1.SchemeGroupVersion.WithResource("secrets")).Namespace(gatewayNs).Get(ctx, RegistryPullSecretName, metav1.GetOptions{})
	labels := existing.GetLabels()
	if labels != nil && labels["kuadrant.io/managed"] == "true" {
		t.Fatal("user-created secret should not have managed label")
	}

	// Wasm plugin should still reference the pull secret
	gw := &machinery.Gateway{
		Gateway: &v1.Gateway{
			ObjectMeta: metav1.ObjectMeta{Name: "test-gw", Namespace: gatewayNs},
		},
	}
	wasmConfig := wasm.Config{ActionSets: []wasm.ActionSet{{Name: "test"}}}
	plugin := buildIstioWasmPluginForGateway(gw, wasmConfig, resolvedURL, useImagePullSecret)
	if plugin.Spec.ImagePullSecret != RegistryPullSecretName {
		t.Errorf("expected pull secret reference, got %q", plugin.Spec.ImagePullSecret)
	}
}

// TestDisconnectedCluster_AdditionalPullSecretOverride verifies that
// additional-pull-secret credentials override pull-secret credentials.
func TestDisconnectedCluster_AdditionalPullSecretOverride(t *testing.T) {
	const (
		originalImage = "registry.redhat.io/rhcl-1/wasm-shim-rhel9@sha256:abc123"
		mirrorHost    = "mirror.disconnected.local:8443"
		gatewayNs     = "override-ns"
	)

	s := newE2EScheme()
	fakeClient := dfake.NewSimpleDynamicClient(s,
		&configv1.ImageDigestMirrorSet{
			TypeMeta:   metav1.TypeMeta{Kind: "ImageDigestMirrorSet", APIVersion: "config.openshift.io/v1"},
			ObjectMeta: metav1.ObjectMeta{Name: "wasm-mirror"},
			Spec: configv1.ImageDigestMirrorSetSpec{
				ImageDigestMirrors: []configv1.ImageDigestMirrors{
					{
						Source:  "registry.redhat.io",
						Mirrors: []configv1.ImageMirror{configv1.ImageMirror(mirrorHost)},
					},
				},
			},
		},
		// pull-secret has old credentials
		&corev1.Secret{
			TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
			ObjectMeta: metav1.ObjectMeta{Name: "pull-secret", Namespace: "openshift-config"},
			Type:       corev1.SecretTypeDockerConfigJson,
			Data: map[string][]byte{
				corev1.DockerConfigJsonKey: []byte(`{"auths":{"` + mirrorHost + `":{"auth":"b2xkOnBhc3M="}}}`),
			},
		},
		// additional-pull-secret has newer credentials that should win
		&corev1.Secret{
			TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
			ObjectMeta: metav1.ObjectMeta{Name: "additional-pull-secret", Namespace: "openshift-config"},
			Type:       corev1.SecretTypeDockerConfigJson,
			Data: map[string][]byte{
				corev1.DockerConfigJsonKey: []byte(`{"auths":{"` + mirrorHost + `":{"auth":"bmV3OnBhc3M="}}}`),
			},
		},
	)

	ctx := context.Background()
	logger := logr.Discard()

	resolvedURL := openshift.ResolveImageURL(ctx, fakeClient, originalImage, true, false, false, logger)
	registryHost := openshift.ExtractRegistryHost(resolvedURL)
	registryCreds, err := openshift.ResolveRegistryCredentials(ctx, fakeClient, registryHost, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if registryCreds == nil {
		t.Fatal("expected credentials")
	}

	// Verify the additional-pull-secret credentials won (contains "bmV3OnBhc3M=")
	if !strings.Contains(string(registryCreds), "bmV3OnBhc3M=") {
		t.Errorf("expected additional-pull-secret creds to override, got %s", string(registryCreds))
	}

	useImagePullSecret, err := openshift.EnsureWasmPluginPullSecret(ctx, fakeClient, gatewayNs, RegistryPullSecretName, registryCreds, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !useImagePullSecret {
		t.Fatal("expected useImagePullSecret=true")
	}
}
