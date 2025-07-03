package openshift

import (
	"context"
	"fmt"

	"github.com/Masterminds/semver/v3"
	configv1 "github.com/openshift/api/config/v1"
	consolev1 "github.com/openshift/api/console/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/env"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kuadrant/kuadrant-operator/internal/utils"
)

var (
	ConsolePluginGVK schema.GroupVersionKind = schema.GroupVersionKind{
		Group:   consolev1.GroupName,
		Version: consolev1.GroupVersion.Version,
		Kind:    "ConsolePlugin",
	}
	ConsolePluginsResource = consolev1.SchemeGroupVersion.WithResource("consoleplugins")

	ClusterVersionGroupKind = schema.GroupKind{
		Group: "config.openshift.io",
		Kind:  "ClusterVersion",
	}
	ClusterVersionResource = configv1.SchemeGroupVersion.WithResource("clusterversions")

	ConsolePluginImageURL    = env.GetString("RELATED_IMAGE_CONSOLEPLUGIN", "quay.io/kuadrant/console-plugin:latest")
	ConsolePluginImageURLOld = env.GetString("RELATED_IMAGE_CONSOLEPLUGIN_OLD", "quay.io/kuadrant/console-plugin:v4.19")
)

func IsConsolePluginInstalled(restMapper meta.RESTMapper) (bool, error) {
	return utils.IsCRDInstalled(restMapper, ConsolePluginGVK.Group, ConsolePluginGVK.Kind, ConsolePluginGVK.Version)
}

// GetConsolePluginImageForVersion returns the appropriate console plugin image based on OpenShift version
func GetConsolePluginImageForVersion(ctx context.Context, k8sClient client.Client) (string, error) {
	clusterVersion := &configv1.ClusterVersion{}

	if err := k8sClient.Get(ctx, types.NamespacedName{Name: "version"}, clusterVersion); err != nil {
		return "", fmt.Errorf("failed to get cluster version: %w", err)
	}

	versionStr := clusterVersion.Status.Desired.Version
	if versionStr == "" {
		return ConsolePluginImageURL, nil
	}

	currentVersion, err := semver.NewVersion(versionStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse cluster version %q: %w", versionStr, err)
	}

	constraint, err := semver.NewConstraint("< 4.20.0")
	if err != nil {
		return "", fmt.Errorf("failed to parse semver constraing: %w", err)
	}

	// Use constraint rather than compare to avoid issues with pre-release versions
	if constraint.Check(currentVersion) {
		return ConsolePluginImageURLOld, nil
	}

	return ConsolePluginImageURL, nil
}
