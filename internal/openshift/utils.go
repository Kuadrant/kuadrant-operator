package openshift

import (
	"fmt"

	"github.com/Masterminds/semver/v3"
	configv1 "github.com/openshift/api/config/v1"
	consolev1 "github.com/openshift/api/console/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/kuadrant/kuadrant-operator/internal/utils"
)

var (
	ConsolePluginGVK = schema.GroupVersionKind{
		Group:   consolev1.GroupName,
		Version: consolev1.GroupVersion.Version,
		Kind:    "ConsolePlugin",
	}
	ConsolePluginsResource = consolev1.SchemeGroupVersion.WithResource("consoleplugins")

	ClusterVersionGroupKind = schema.GroupVersionKind{
		Group:   configv1.GroupName,
		Version: configv1.GroupVersion.Version,
		Kind:    "ClusterVersion",
	}
	ClusterVersionResource = configv1.SchemeGroupVersion.WithResource("clusterversions")
)

func IsConsolePluginInstalled(restMapper meta.RESTMapper) (bool, error) {
	return utils.IsCRDInstalled(restMapper, ConsolePluginGVK.Group, ConsolePluginGVK.Kind, ConsolePluginGVK.Version)
}

func IsClusterVersionInstalled(restMapper meta.RESTMapper) (bool, error) {
	return utils.IsCRDInstalled(restMapper, ClusterVersionGroupKind.Group, ClusterVersionGroupKind.Kind, ClusterVersionGroupKind.Version)
}

// GetConsolePluginImageFromConfigMap returns the appropriate console plugin image from ConfigMap based on OpenShift version
func GetConsolePluginImageFromConfigMap(configMap *corev1.ConfigMap, clusterVersion *configv1.ClusterVersion) (string, error) {
	openshiftVersion := clusterVersion.Status.Desired.Version

	if configMap == nil || configMap.Data == nil {
		return "", fmt.Errorf("console plugin ConfigMap is nil or has no data")
	}

	var majorMinorVersion string
	if openshiftVersion != "" {
		version, err := semver.NewVersion(openshiftVersion)
		if err != nil {
			return "", fmt.Errorf("failed to parse OpenShift version %q: %w", openshiftVersion, err)
		}
		majorMinorVersion = fmt.Sprintf("%d.%d", version.Major(), version.Minor())

		if image, exists := configMap.Data[majorMinorVersion]; exists {
			return image, nil
		}
	}

	if image, exists := configMap.Data["default"]; exists {
		return image, nil
	}

	return "", fmt.Errorf("no console plugin image found for OpenShift version %q (major.minor: %q)", openshiftVersion, majorMinorVersion)
}
