package openshift

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

const (
	openshiftConfigNamespace = "openshift-config"
	clusterPullSecretName    = "pull-secret"
	additionalPullSecretName = "additional-pull-secret"

	managedLabelKey   = "kuadrant.io/managed"
	managedLabelValue = "true"
)

var secretsResource = corev1.SchemeGroupVersion.WithResource("secrets")

type dockerConfigJSON struct {
	Auths map[string]json.RawMessage `json:"auths"`
}

// GatewayOwnerRef identifies a Gateway for owner reference purposes.
type GatewayOwnerRef struct {
	Name string
	UID  types.UID
}

// PullSecretReconcileConfig holds the parameters for ReconcileWasmPluginPullSecret.
type PullSecretReconcileConfig struct {
	Client          dynamic.Interface
	ImageURL        string
	Namespace       string
	SecretName      string
	Active          bool
	IsIDMSInstalled bool
	IsITMSInstalled bool
	IsICPInstalled  bool
	GatewayOwner    GatewayOwnerRef
	Logger          logr.Logger
}

// ReconcileWasmPluginPullSecret is the single entry point for pull secret automation.
// It checks whether the feature is enabled (kill-switch off and at least one mirror CRD
// installed), then either discovers credentials and manages the secret (when active) or
// cleans up any managed secret (when not active).
//
// Returns true if the WasmPlugin/EnvoyExtensionPolicy should reference a pull secret.
func ReconcileWasmPluginPullSecret(ctx context.Context, cfg PullSecretReconcileConfig) (bool, error) {
	if IsImageMirrorResolutionDisabled() || (!cfg.IsIDMSInstalled && !cfg.IsITMSInstalled && !cfg.IsICPInstalled) {
		return false, nil
	}
	if !cfg.Active {
		return ensureWasmPluginPullSecret(ctx, cfg.Client, cfg.Namespace, cfg.SecretName, nil, cfg.GatewayOwner, cfg.Logger)
	}
	registryHost := extractRegistryHost(cfg.ImageURL)
	registryCreds, err := resolveRegistryCredentials(ctx, cfg.Client, registryHost, cfg.Logger)
	if err != nil {
		cfg.Logger.V(1).Info("failed to resolve registry credentials", "registry", registryHost, "error", err)
	}
	return ensureWasmPluginPullSecret(ctx, cfg.Client, cfg.Namespace, cfg.SecretName, registryCreds, cfg.GatewayOwner, cfg.Logger)
}

// extractRegistryHost extracts the registry hostname (with optional port) from a container image URL.
func extractRegistryHost(imageURL string) string {
	if idx := strings.IndexByte(imageURL, '/'); idx >= 0 {
		return imageURL[:idx]
	}
	// No path separator — strip digest or tag
	if idx := strings.IndexByte(imageURL, '@'); idx >= 0 {
		return imageURL[:idx]
	}
	return imageURL
}

// resolveRegistryCredentials reads the OpenShift cluster pull secrets (pull-secret and
// additional-pull-secret in openshift-config namespace), merges them, and returns a
// dockerconfigjson containing only the credentials for the specified registry host.
// Returns nil if no credentials are found or if the secrets cannot be read (e.g., on
// non-OpenShift clusters).
func resolveRegistryCredentials(ctx context.Context, client dynamic.Interface, registryHost string, logger logr.Logger) ([]byte, error) {
	merged := make(map[string]json.RawMessage)

	readAndMerge(ctx, client, clusterPullSecretName, merged, logger)
	// additional-pull-secret entries override pull-secret entries
	readAndMerge(ctx, client, additionalPullSecretName, merged, logger)

	return buildFilteredDockerConfigJSON(merged, registryHost)
}

func readAndMerge(ctx context.Context, client dynamic.Interface, secretName string, into map[string]json.RawMessage, logger logr.Logger) {
	un, err := client.Resource(secretsResource).Namespace(openshiftConfigNamespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if !k8serrors.IsNotFound(err) && !k8serrors.IsForbidden(err) {
			logger.V(1).Info("failed to read secret", "namespace", openshiftConfigNamespace, "name", secretName, "error", err)
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

// ensureWasmPluginPullSecret manages the lifecycle of the wasm-plugin-pull-secret in the
// given namespace. It creates or updates the secret with the provided credentials, or
// cleans it up if no credentials are needed.
//
// User-created secrets (those without the kuadrant.io/managed label) are never modified.
// If a user-created secret exists, this function returns true so that the caller still
// sets the imagePullSecret reference on the WasmPlugin / EnvoyExtensionPolicy.
//
// Returns true if the imagePullSecret reference should be set.
func ensureWasmPluginPullSecret(ctx context.Context, client dynamic.Interface, namespace, secretName string, dockerConfigData []byte, owner GatewayOwnerRef, logger logr.Logger) (bool, error) {
	resource := client.Resource(secretsResource).Namespace(namespace)

	existing, err := resource.Get(ctx, secretName, metav1.GetOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		return false, fmt.Errorf("failed to get secret %s/%s: %w", namespace, secretName, err)
	}

	secretExists := err == nil
	isManaged := false
	if secretExists {
		lbls := existing.GetLabels()
		isManaged = lbls != nil && lbls[managedLabelKey] == managedLabelValue
	}

	// User-created secret: never touch it, always reference it
	if secretExists && !isManaged {
		logger.V(1).Info("pull secret exists but is not managed by kuadrant, leaving it untouched",
			"namespace", namespace, "secret", secretName)
		return true, nil
	}

	if dockerConfigData == nil {
		if secretExists && isManaged {
			owners := removeOwnerRef(existing.GetOwnerReferences(), owner.UID)
			if len(owners) > 0 {
				existing.SetOwnerReferences(owners)
				if _, err := resource.Update(ctx, existing, metav1.UpdateOptions{}); err != nil {
					return false, fmt.Errorf("failed to update owner references on secret %s/%s: %w", namespace, secretName, err)
				}
				logger.V(1).Info("removed gateway owner reference from managed pull secret",
					"namespace", namespace, "secret", secretName, "gateway", owner.Name)
				return false, nil
			}
			if err := resource.Delete(ctx, secretName, metav1.DeleteOptions{}); err != nil && !k8serrors.IsNotFound(err) {
				return false, fmt.Errorf("failed to delete managed secret %s/%s: %w", namespace, secretName, err)
			}
			logger.Info("deleted managed pull secret (no longer needed)", "namespace", namespace, "secret", secretName)
		}
		return false, nil
	}

	ownerRef := gatewayOwnerReference(owner)

	if !secretExists {
		desiredObj := buildSecretUnstructured(namespace, secretName, dockerConfigData)
		desiredObj.SetOwnerReferences([]metav1.OwnerReference{ownerRef})
		if _, err := resource.Create(ctx, desiredObj, metav1.CreateOptions{}); err != nil {
			return false, fmt.Errorf("failed to create secret %s/%s: %w", namespace, secretName, err)
		}
		logger.Info("created managed pull secret", "namespace", namespace, "secret", secretName)
		return true, nil
	}

	// Secret exists and is managed — check if data or owner references need updating.
	var existingSecret corev1.Secret
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(existing.Object, &existingSecret); err != nil {
		return false, fmt.Errorf("failed to parse existing secret %s/%s: %w", namespace, secretName, err)
	}

	dataChanged := !dockerConfigEqual(existingSecret.Data[corev1.DockerConfigJsonKey], dockerConfigData)
	ownersChanged := addOwnerRef(existing, ownerRef)

	if !dataChanged && !ownersChanged {
		return true, nil
	}

	if dataChanged {
		desiredObj := buildSecretUnstructured(namespace, secretName, dockerConfigData)
		desiredObj.SetResourceVersion(existing.GetResourceVersion())
		desiredObj.SetOwnerReferences(existing.GetOwnerReferences())
		if _, err := resource.Update(ctx, desiredObj, metav1.UpdateOptions{}); err != nil {
			return false, fmt.Errorf("failed to update secret %s/%s: %w", namespace, secretName, err)
		}
		logger.Info("updated managed pull secret", "namespace", namespace, "secret", secretName)
	} else {
		if _, err := resource.Update(ctx, existing, metav1.UpdateOptions{}); err != nil {
			return false, fmt.Errorf("failed to update owner references on secret %s/%s: %w", namespace, secretName, err)
		}
	}
	return true, nil
}

func gatewayOwnerReference(owner GatewayOwnerRef) metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion: "gateway.networking.k8s.io/v1",
		Kind:       "Gateway",
		Name:       owner.Name,
		UID:        owner.UID,
	}
}

func addOwnerRef(obj *unstructured.Unstructured, ref metav1.OwnerReference) bool {
	owners := obj.GetOwnerReferences()
	for _, o := range owners {
		if o.UID == ref.UID {
			return false
		}
	}
	obj.SetOwnerReferences(append(owners, ref))
	return true
}

func removeOwnerRef(owners []metav1.OwnerReference, uid types.UID) []metav1.OwnerReference {
	return slices.DeleteFunc(owners, func(o metav1.OwnerReference) bool {
		return o.UID == uid
	})
}

// dockerConfigEqual compares two dockerconfigjson blobs semantically.
// When both are valid JSON, it deserializes and compares the structure
// so that formatting differences don't cause spurious updates. Falls back
// to byte comparison when either side is not valid JSON.
func dockerConfigEqual(a, b []byte) bool {
	var aVal, bVal interface{}
	if json.Unmarshal(a, &aVal) != nil || json.Unmarshal(b, &bVal) != nil {
		return bytes.Equal(a, b)
	}
	return reflect.DeepEqual(aVal, bVal)
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
				managedLabelKey: managedLabelValue,
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
