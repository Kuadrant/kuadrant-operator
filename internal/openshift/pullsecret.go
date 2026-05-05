package openshift

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
)

const (
	OpenshiftConfigNamespace = "openshift-config"
	ClusterPullSecretName    = "pull-secret"
	AdditionalPullSecretName = "additional-pull-secret"

	ManagedLabelKey   = "kuadrant.io/managed"
	ManagedLabelValue = "true"
)

var secretsResource = corev1.SchemeGroupVersion.WithResource("secrets")

type dockerConfigJSON struct {
	Auths map[string]json.RawMessage `json:"auths"`
}

// ExtractRegistryHost extracts the registry hostname (with optional port) from a container image URL.
func ExtractRegistryHost(imageURL string) string {
	if idx := strings.IndexByte(imageURL, '/'); idx >= 0 {
		return imageURL[:idx]
	}
	// No path separator — strip digest or tag
	if idx := strings.IndexByte(imageURL, '@'); idx >= 0 {
		return imageURL[:idx]
	}
	return imageURL
}

// ResolveRegistryCredentials reads the OpenShift cluster pull secrets (pull-secret and
// additional-pull-secret in openshift-config namespace), merges them, and returns a
// dockerconfigjson containing only the credentials for the specified registry host.
// Returns nil if no credentials are found or if the secrets cannot be read (e.g., on
// non-OpenShift clusters).
func ResolveRegistryCredentials(ctx context.Context, client dynamic.Interface, registryHost string, logger logr.Logger) ([]byte, error) {
	merged := make(map[string]json.RawMessage)

	readAndMerge(ctx, client, ClusterPullSecretName, merged, logger)
	// additional-pull-secret entries override pull-secret entries
	readAndMerge(ctx, client, AdditionalPullSecretName, merged, logger)

	return buildFilteredDockerConfigJSON(merged, registryHost)
}

func readAndMerge(ctx context.Context, client dynamic.Interface, secretName string, into map[string]json.RawMessage, logger logr.Logger) {
	un, err := client.Resource(secretsResource).Namespace(OpenshiftConfigNamespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if !k8serrors.IsNotFound(err) && !k8serrors.IsForbidden(err) {
			logger.V(1).Info("failed to read secret", "namespace", OpenshiftConfigNamespace, "name", secretName, "error", err)
		}
		return
	}

	var secret corev1.Secret
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(un.Object, &secret); err != nil {
		logger.V(1).Info("failed to parse secret", "name", secretName, "error", err)
		return
	}

	data, ok := secret.Data[corev1.DockerConfigJsonKey]
	if !ok {
		return
	}

	auths, err := parseDockerConfigAuths(data)
	if err != nil {
		logger.V(1).Info("failed to unmarshal dockerconfigjson", "name", secretName, "error", err)
		return
	}

	for k, v := range auths {
		into[k] = v
	}
}

func parseDockerConfigAuths(data []byte) (map[string]json.RawMessage, error) {
	var cfg dockerConfigJSON
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return cfg.Auths, nil
}

func buildFilteredDockerConfigJSON(auths map[string]json.RawMessage, registryHost string) ([]byte, error) {
	entry, ok := auths[registryHost]
	if !ok {
		return nil, nil
	}

	filtered := dockerConfigJSON{
		Auths: map[string]json.RawMessage{
			registryHost: entry,
		},
	}
	data, err := json.Marshal(filtered)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal filtered credentials: %w", err)
	}
	return data, nil
}

// EnsureWasmPluginPullSecret manages the lifecycle of the wasm-plugin-pull-secret in the
// given namespace. It creates or updates the secret with the provided credentials, or
// cleans it up if no credentials are needed.
//
// User-created secrets (those without the kuadrant.io/managed label) are never modified.
// If a user-created secret exists, this function returns true so that the caller still
// sets the imagePullSecret reference on the WasmPlugin / EnvoyExtensionPolicy.
//
// Returns true if the imagePullSecret reference should be set.
func EnsureWasmPluginPullSecret(ctx context.Context, client dynamic.Interface, namespace, secretName string, dockerConfigData []byte, logger logr.Logger) (bool, error) {
	resource := client.Resource(secretsResource).Namespace(namespace)

	existing, err := resource.Get(ctx, secretName, metav1.GetOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		return false, fmt.Errorf("failed to get secret %s/%s: %w", namespace, secretName, err)
	}

	secretExists := err == nil
	isManaged := false
	if secretExists {
		lbls := existing.GetLabels()
		isManaged = lbls != nil && lbls[ManagedLabelKey] == ManagedLabelValue
	}

	// User-created secret: never touch it, always reference it
	if secretExists && !isManaged {
		logger.V(1).Info("pull secret exists but is not managed by kuadrant, leaving it untouched",
			"namespace", namespace, "secret", secretName)
		return true, nil
	}

	if dockerConfigData == nil {
		if secretExists && isManaged {
			if err := resource.Delete(ctx, secretName, metav1.DeleteOptions{}); err != nil && !k8serrors.IsNotFound(err) {
				return false, fmt.Errorf("failed to delete managed secret %s/%s: %w", namespace, secretName, err)
			}
			logger.Info("deleted managed pull secret (no longer needed)", "namespace", namespace, "secret", secretName)
		}
		return false, nil
	}

	if !secretExists {
		desiredObj := buildSecretUnstructured(namespace, secretName, dockerConfigData)
		if _, err := resource.Create(ctx, desiredObj, metav1.CreateOptions{}); err != nil {
			return false, fmt.Errorf("failed to create secret %s/%s: %w", namespace, secretName, err)
		}
		logger.Info("created managed pull secret", "namespace", namespace, "secret", secretName)
		return true, nil
	}

	// Secret exists and is managed — update only if the raw data actually changed.
	// Convert via the typed API to get decoded []byte, avoiding any dependency on
	// how the API server or unstructured converter serializes base64.
	var existingSecret corev1.Secret
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(existing.Object, &existingSecret); err != nil {
		return false, fmt.Errorf("failed to parse existing secret %s/%s: %w", namespace, secretName, err)
	}
	if bytes.Equal(existingSecret.Data[corev1.DockerConfigJsonKey], dockerConfigData) {
		return true, nil
	}

	desiredObj := buildSecretUnstructured(namespace, secretName, dockerConfigData)
	desiredObj.SetResourceVersion(existing.GetResourceVersion())
	if _, err := resource.Update(ctx, desiredObj, metav1.UpdateOptions{}); err != nil {
		return false, fmt.Errorf("failed to update secret %s/%s: %w", namespace, secretName, err)
	}
	logger.Info("updated managed pull secret", "namespace", namespace, "secret", secretName)
	return true, nil
}

func buildSecretUnstructured(namespace, name string, dockerConfigData []byte) *unstructured.Unstructured {
	secret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				ManagedLabelKey: ManagedLabelValue,
			},
		},
		Type: corev1.SecretTypeDockerConfigJson,
		Data: map[string][]byte{
			corev1.DockerConfigJsonKey: dockerConfigData,
		},
	}

	obj, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(secret)
	return &unstructured.Unstructured{Object: obj}
}
