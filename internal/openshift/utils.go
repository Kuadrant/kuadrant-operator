package openshift

import (
	"fmt"
	"os"

	"github.com/Masterminds/semver/v3"
	configv1 "github.com/openshift/api/config/v1"
	consolev1 "github.com/openshift/api/console/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/kuadrant/kuadrant-operator/internal/utils"
)

const (
	RelatedImageConsolePluginLatestEnvVar = "RELATED_IMAGE_CONSOLE_PLUGIN_LATEST"
	RelatedImageConsolePluginSDK1EnvVar   = "RELATED_IMAGE_CONSOLE_PLUGIN_SDK1"
	RelatedImageConsolePluginPF5EnvVar    = "RELATED_IMAGE_CONSOLE_PLUGIN_PF5"
	// ConsolePluginImageOverrideEnvVar allows development clusters without a
	// ClusterVersion API (for example OINC) to opt in to the Console plugin.
	ConsolePluginImageOverrideEnvVar = "CONSOLE_PLUGIN_IMAGE_OVERRIDE"
)

type consolePluginImageRule struct {
	When     string
	ImageRef string
}

// Evaluated top-down; first satisfied constraint wins. "*" is the fallback.
var consolePluginImageRules = []consolePluginImageRule{
	{When: ">= 4.22.0-0", ImageRef: RelatedImageConsolePluginLatestEnvVar},
	{When: ">= 4.20.0-0", ImageRef: RelatedImageConsolePluginSDK1EnvVar},
	{When: "*", ImageRef: RelatedImageConsolePluginPF5EnvVar},
}

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

// GetConsolePluginImageForVersion returns the appropriate console plugin image based on OpenShift version.
// Rules are evaluated top-down; the first satisfied semver constraint wins.
func GetConsolePluginImageForVersion(clusterVersion *configv1.ClusterVersion) (string, error) {
	openshiftVersion := clusterVersion.Status.Desired.Version

	if openshiftVersion == "" {
		return "", fmt.Errorf("OpenShift version is empty")
	}

	version, err := semver.NewVersion(openshiftVersion)
	if err != nil {
		return "", fmt.Errorf("failed to parse OpenShift version %q: %w", openshiftVersion, err)
	}

	for _, rule := range consolePluginImageRules {
		if rule.When != "*" {
			constraint, err := semver.NewConstraint(rule.When)
			if err != nil {
				return "", fmt.Errorf("failed to parse version constraint %q: %w", rule.When, err)
			}
			if !constraint.Check(version) {
				continue
			}
		}

		image := os.Getenv(rule.ImageRef)
		if image == "" {
			return "", fmt.Errorf("environment variable %s is not set", rule.ImageRef)
		}
		return image, nil
	}

	return "", fmt.Errorf("no console plugin image rule matched OpenShift version %q", openshiftVersion)
}
