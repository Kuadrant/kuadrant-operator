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
	RelatedImageConsolePluginPF5EnvVar    = "RELATED_IMAGE_CONSOLE_PLUGIN_PF5"

	// pf6VersionConstraint defines the minimum OpenShift version that requires PatternFly 6
	pf6VersionConstraint = ">= 4.20.0-0"
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

// GetConsolePluginImageForVersion returns the appropriate console plugin image based on OpenShift version.
// For OpenShift versions >= 4.20, it returns the latest (PatternFly 6) compatible image from RELATED_IMAGE_CONSOLE_PLUGIN_LATEST.
// For earlier versions, it returns the PF5 image from RELATED_IMAGE_CONSOLE_PLUGIN_PF5.
// This ensures proper mirroring in disconnected environments by using RELATED_IMAGE environment variables.
func GetConsolePluginImageForVersion(clusterVersion *configv1.ClusterVersion) (string, error) {
	openshiftVersion := clusterVersion.Status.Desired.Version

	if openshiftVersion == "" {
		return "", fmt.Errorf("OpenShift version is empty")
	}

	version, err := semver.NewVersion(openshiftVersion)
	if err != nil {
		return "", fmt.Errorf("failed to parse OpenShift version %q: %w", openshiftVersion, err)
	}

	constraint, err := semver.NewConstraint(pf6VersionConstraint)
	if err != nil {
		return "", fmt.Errorf("failed to parse version constraint %q: %w", pf6VersionConstraint, err)
	}

	var envVarName string
	if constraint.Check(version) {
		envVarName = RelatedImageConsolePluginLatestEnvVar
	} else {
		envVarName = RelatedImageConsolePluginPF5EnvVar
	}

	image := os.Getenv(envVarName)
	if image == "" {
		return "", fmt.Errorf("environment variable %s is not set", envVarName)
	}

	return image, nil
}
