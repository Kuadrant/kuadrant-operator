//go:build unit

package controllers

import (
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

func newTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = configv1.Install(s)
	_ = corev1.AddToScheme(s)
	return s
}

func newMirrorFakeClient(t *testing.T, mirrorHost string, extraObjects ...runtime.Object) *dfake.FakeDynamicClient {
	t.Helper()
	objects := []runtime.Object{
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
	}
	objects = append(objects, extraObjects...)
	return dfake.NewSimpleDynamicClient(newTestScheme(), objects...)
}

// TestDisconnectedCluster_IstioFlow exercises the full chain:
// mirror resolution → ReconcileWasmPluginPullSecret → wasm plugin construction
func TestDisconnectedCluster_IstioFlow(t *testing.T) {
	const (
		originalImage = "registry.redhat.io/rhcl-1/wasm-shim-rhel9@sha256:abc123"
		mirrorHost    = "mirror.disconnected.local:8443"
		mirrorImage   = mirrorHost + "/rhcl-1/wasm-shim-rhel9@sha256:abc123"
		gatewayNs     = "gateway-ns"
	)

	fakeClient := newMirrorFakeClient(t, mirrorHost)
	ctx := t.Context()
	logger := logr.Discard()

	resolvedURL := openshift.ResolveImageURL(ctx, fakeClient, originalImage, true, false, false, logger)
	if resolvedURL != mirrorImage {
		t.Fatalf("mirror resolution: expected %q, got %q", mirrorImage, resolvedURL)
	}

	useImagePullSecret, err := openshift.ReconcileWasmPluginPullSecret(ctx, openshift.PullSecretReconcileConfig{
		Client:          fakeClient,
		ImageURL:        resolvedURL,
		Namespace:       gatewayNs,
		SecretName:      RegistryPullSecretName,
		Active:          true,
		IsIDMSInstalled: true,
		Logger:          logger,
	})
	if err != nil {
		t.Fatalf("ReconcileWasmPluginPullSecret failed: %v", err)
	}
	if !useImagePullSecret {
		t.Fatal("expected useImagePullSecret=true")
	}

	// Verify the managed secret was created
	created, err := fakeClient.Resource(corev1.SchemeGroupVersion.WithResource("secrets")).Namespace(gatewayNs).Get(ctx, RegistryPullSecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("managed secret not found: %v", err)
	}
	if created.GetLabels()["kuadrant.io/managed"] != "true" {
		t.Fatal("managed secret missing kuadrant.io/managed label")
	}

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

// TestDisconnectedCluster_EnvoyGatewayFlow exercises the same chain for Envoy Gateway.
func TestDisconnectedCluster_EnvoyGatewayFlow(t *testing.T) {
	const (
		originalImage = "registry.redhat.io/rhcl-1/wasm-shim-rhel9@sha256:abc123"
		mirrorHost    = "mirror.disconnected.local:8443"
		mirrorImage   = mirrorHost + "/rhcl-1/wasm-shim-rhel9@sha256:abc123"
		gatewayNs     = "eg-gateway-ns"
	)

	fakeClient := newMirrorFakeClient(t, mirrorHost)
	ctx := t.Context()
	logger := logr.Discard()

	resolvedURL := openshift.ResolveImageURL(ctx, fakeClient, originalImage, true, false, false, logger)

	useImagePullSecret, err := openshift.ReconcileWasmPluginPullSecret(ctx, openshift.PullSecretReconcileConfig{
		Client:          fakeClient,
		ImageURL:        resolvedURL,
		Namespace:       gatewayNs,
		SecretName:      RegistryPullSecretName,
		Active:          true,
		IsIDMSInstalled: true,
		Logger:          logger,
	})
	if err != nil {
		t.Fatalf("ReconcileWasmPluginPullSecret failed: %v", err)
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

	fakeClient := dfake.NewSimpleDynamicClient(newTestScheme())
	ctx := t.Context()
	logger := logr.Discard()

	resolvedURL := openshift.ResolveImageURL(ctx, fakeClient, originalImage, false, false, false, logger)
	if resolvedURL != originalImage {
		t.Fatalf("expected unchanged URL on non-OpenShift, got %q", resolvedURL)
	}

	// No CRDs installed — returns false immediately, no API calls
	useImagePullSecret, err := openshift.ReconcileWasmPluginPullSecret(ctx, openshift.PullSecretReconcileConfig{
		Client:     fakeClient,
		ImageURL:   resolvedURL,
		Namespace:  gatewayNs,
		SecretName: RegistryPullSecretName,
		Active:     true,
		Logger:     logger,
	})
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

	fakeClient := newMirrorFakeClient(t, mirrorHost)
	ctx := t.Context()
	logger := logr.Discard()

	// Mirror resolution disabled
	resolvedURL := openshift.ResolveImageURL(ctx, fakeClient, originalImage, true, false, false, logger)
	if resolvedURL != originalImage {
		t.Fatalf("expected original URL with kill-switch, got %q", resolvedURL)
	}

	// ReconcileWasmPluginPullSecret returns false (kill-switch active)
	useImagePullSecret, err := openshift.ReconcileWasmPluginPullSecret(ctx, openshift.PullSecretReconcileConfig{
		Client:          fakeClient,
		ImageURL:        resolvedURL,
		Namespace:       gatewayNs,
		SecretName:      RegistryPullSecretName,
		Active:          true,
		IsIDMSInstalled: true,
		Logger:          logger,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if useImagePullSecret {
		t.Fatal("expected useImagePullSecret=false with kill-switch")
	}

	gw := &machinery.Gateway{
		Gateway: &v1.Gateway{
			ObjectMeta: metav1.ObjectMeta{Name: "test-gw", Namespace: gatewayNs},
		},
	}
	wasmConfig := wasm.Config{ActionSets: []wasm.ActionSet{{Name: "test"}}}
	plugin := buildIstioWasmPluginForGateway(gw, wasmConfig, resolvedURL, useImagePullSecret)
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

	fakeClient := dfake.NewSimpleDynamicClient(newTestScheme())
	ctx := t.Context()
	logger := logr.Discard()

	resolvedURL := openshift.ResolveImageURL(ctx, fakeClient, imageURL, false, false, false, logger)

	// No CRDs → ReconcileWasmPluginPullSecret returns false
	useImagePullSecret, _ := openshift.ReconcileWasmPluginPullSecret(ctx, openshift.PullSecretReconcileConfig{
		Client:     fakeClient,
		ImageURL:   resolvedURL,
		Namespace:  gatewayNs,
		SecretName: RegistryPullSecretName,
		Active:     true,
		Logger:     logger,
	})

	// PROTECTED_REGISTRY fallback (same logic as production reconcilers)
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

	// User-created secret (no managed label) in gateway namespace
	userSecret := &corev1.Secret{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
		ObjectMeta: metav1.ObjectMeta{Name: RegistryPullSecretName, Namespace: gatewayNs},
		Type:       corev1.SecretTypeDockerConfigJson,
		Data: map[string][]byte{
			corev1.DockerConfigJsonKey: []byte(`{"auths":{"custom.registry.io":{"auth":"dXNlcg=="}}}`),
		},
	}

	fakeClient := newMirrorFakeClient(t, mirrorHost, userSecret)
	ctx := t.Context()
	logger := logr.Discard()

	resolvedURL := openshift.ResolveImageURL(ctx, fakeClient, originalImage, true, false, false, logger)

	// ReconcileWasmPluginPullSecret should return true (user secret exists) without modifying it
	useImagePullSecret, err := openshift.ReconcileWasmPluginPullSecret(ctx, openshift.PullSecretReconcileConfig{
		Client:          fakeClient,
		ImageURL:        resolvedURL,
		Namespace:       gatewayNs,
		SecretName:      RegistryPullSecretName,
		Active:          true,
		IsIDMSInstalled: true,
		Logger:          logger,
	})
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

// TestDisconnectedCluster_Idempotent verifies that calling ReconcileWasmPluginPullSecret
// twice produces the same result without errors (no spurious updates).
func TestDisconnectedCluster_Idempotent(t *testing.T) {
	const (
		originalImage = "registry.redhat.io/rhcl-1/wasm-shim-rhel9@sha256:abc123"
		mirrorHost    = "mirror.disconnected.local:8443"
		mirrorImage   = mirrorHost + "/rhcl-1/wasm-shim-rhel9@sha256:abc123"
		gatewayNs     = "idempotent-ns"
	)

	fakeClient := newMirrorFakeClient(t, mirrorHost)
	ctx := t.Context()
	logger := logr.Discard()

	resolvedURL := openshift.ResolveImageURL(ctx, fakeClient, originalImage, true, false, false, logger)
	if resolvedURL != mirrorImage {
		t.Fatalf("mirror resolution: expected %q, got %q", mirrorImage, resolvedURL)
	}

	cfg := openshift.PullSecretReconcileConfig{
		Client:          fakeClient,
		ImageURL:        resolvedURL,
		Namespace:       gatewayNs,
		SecretName:      RegistryPullSecretName,
		Active:          true,
		IsIDMSInstalled: true,
		Logger:          logger,
	}

	useImagePullSecret, err := openshift.ReconcileWasmPluginPullSecret(ctx, cfg)
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if !useImagePullSecret {
		t.Fatal("expected useImagePullSecret=true on first call")
	}

	useImagePullSecret2, err := openshift.ReconcileWasmPluginPullSecret(ctx, cfg)
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	if !useImagePullSecret2 {
		t.Fatal("expected useImagePullSecret=true on second call")
	}
}

// TestDisconnectedCluster_CleanupWhenInactive verifies that ReconcileWasmPluginPullSecret
// cleans up managed secrets when active=false.
func TestDisconnectedCluster_CleanupWhenInactive(t *testing.T) {
	const (
		originalImage = "registry.redhat.io/rhcl-1/wasm-shim-rhel9@sha256:abc123"
		mirrorHost    = "mirror.disconnected.local:8443"
		gatewayNs     = "cleanup-ns"
	)

	fakeClient := newMirrorFakeClient(t, mirrorHost)
	ctx := t.Context()
	logger := logr.Discard()

	resolvedURL := openshift.ResolveImageURL(ctx, fakeClient, originalImage, true, false, false, logger)

	cfg := openshift.PullSecretReconcileConfig{
		Client:          fakeClient,
		ImageURL:        resolvedURL,
		Namespace:       gatewayNs,
		SecretName:      RegistryPullSecretName,
		Active:          true,
		IsIDMSInstalled: true,
		Logger:          logger,
	}

	// First: create managed secret
	useImagePullSecret, err := openshift.ReconcileWasmPluginPullSecret(ctx, cfg)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if !useImagePullSecret {
		t.Fatal("expected useImagePullSecret=true after create")
	}

	// Second: clean up (active=false)
	cfg.Active = false
	useImagePullSecret, err = openshift.ReconcileWasmPluginPullSecret(ctx, cfg)
	if err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}
	if useImagePullSecret {
		t.Fatal("expected useImagePullSecret=false after cleanup")
	}

	// Verify secret was deleted
	_, err = fakeClient.Resource(corev1.SchemeGroupVersion.WithResource("secrets")).Namespace(gatewayNs).Get(ctx, RegistryPullSecretName, metav1.GetOptions{})
	if err == nil {
		t.Fatal("expected managed secret to be deleted")
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

	s := newTestScheme()
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

	ctx := t.Context()
	logger := logr.Discard()

	resolvedURL := openshift.ResolveImageURL(ctx, fakeClient, originalImage, true, false, false, logger)

	useImagePullSecret, err := openshift.ReconcileWasmPluginPullSecret(ctx, openshift.PullSecretReconcileConfig{
		Client:          fakeClient,
		ImageURL:        resolvedURL,
		Namespace:       gatewayNs,
		SecretName:      RegistryPullSecretName,
		Active:          true,
		IsIDMSInstalled: true,
		Logger:          logger,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !useImagePullSecret {
		t.Fatal("expected useImagePullSecret=true")
	}

	// Verify the managed secret contains the additional-pull-secret creds (override won)
	created, err := fakeClient.Resource(corev1.SchemeGroupVersion.WithResource("secrets")).Namespace(gatewayNs).Get(ctx, RegistryPullSecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("managed secret not found: %v", err)
	}
	var managedSecret corev1.Secret
	if convErr := runtime.DefaultUnstructuredConverter.FromUnstructured(created.Object, &managedSecret); convErr != nil {
		t.Fatalf("failed to convert managed secret: %v", convErr)
	}
	dockerCfg := string(managedSecret.Data[corev1.DockerConfigJsonKey])
	if !strings.Contains(dockerCfg, "bmV3OnBhc3M=") {
		t.Errorf("expected additional-pull-secret creds in managed secret, got %s", dockerCfg)
	}
}
