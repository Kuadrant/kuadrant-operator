package openshift

import (
	"fmt"
	"os"

	"github.com/Masterminds/semver/v3"
	configv1 "github.com/openshift/api/config/v1"
	consolev1 "github.com/openshift/api/console/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/kuadrant/kuadrant-operator/internal/openshift/consoleplugin"
	"github.com/kuadrant/kuadrant-operator/internal/utils"
)

const (
	RelatedImageConsolePluginLatestEnvVar = "RELATED_IMAGE_CONSOLE_PLUGIN_LATEST"
	RelatedImageConsolePluginSDK1EnvVar   = "RELATED_IMAGE_CONSOLE_PLUGIN_SDK1"
	RelatedImageConsolePluginPF5EnvVar    = "RELATED_IMAGE_CONSOLE_PLUGIN_PF5"
	// ConsolePluginImageOverrideEnvVar allows development clusters without a
	// ClusterVersion API to opt in to the Console plugin.
	ConsolePluginImageOverrideEnvVar = "CONSOLE_PLUGIN_IMAGE_OVERRIDE"
	// ConsolePluginRuntimeOverrideEnvVar selects the runtime of a development
	// image override. An omitted value preserves the Go server default.
	ConsolePluginRuntimeOverrideEnvVar = "CONSOLE_PLUGIN_RUNTIME_OVERRIDE"
)

type consolePluginImageRule struct {
	When     string
	ImageRef string
	Runtime  consoleplugin.Runtime
}

// Evaluated top-down; first satisfied constraint wins. "*" is the fallback.
var consolePluginImageRules = []consolePluginImageRule{
	{When: ">= 4.22.0-0", ImageRef: RelatedImageConsolePluginLatestEnvVar, Runtime: consoleplugin.RuntimeGo},
	{When: ">= 4.20.0-0", ImageRef: RelatedImageConsolePluginSDK1EnvVar, Runtime: consoleplugin.RuntimeNginx},
	{When: "*", ImageRef: RelatedImageConsolePluginPF5EnvVar, Runtime: consoleplugin.RuntimeNginx},
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

// GetConsolePluginImageForVersion returns the image and its runtime based on OpenShift version.
// Rules are evaluated top-down; the first satisfied semver constraint wins.
func GetConsolePluginImageForVersion(clusterVersion *configv1.ClusterVersion) (consoleplugin.Image, error) {
	openshiftVersion := clusterVersion.Status.Desired.Version
	// Desired moves ahead of the Console during an update. Keep the previous
	// compatible stream until the new OpenShift release is fully applied.
	// History can be empty during initial cluster startup.
	for _, update := range clusterVersion.Status.History {
		if update.State == configv1.CompletedUpdate && update.Version != "" {
			openshiftVersion = update.Version
			break
		}
	}

	if openshiftVersion == "" {
		return consoleplugin.Image{}, fmt.Errorf("OpenShift version is empty")
	}

	version, err := semver.NewVersion(openshiftVersion)
	if err != nil {
		return consoleplugin.Image{}, fmt.Errorf("failed to parse OpenShift version %q: %w", openshiftVersion, err)
	}

	for _, rule := range consolePluginImageRules {
		if rule.When != "*" {
			constraint, err := semver.NewConstraint(rule.When)
			if err != nil {
				return consoleplugin.Image{}, fmt.Errorf("failed to parse version constraint %q: %w", rule.When, err)
			}
			if !constraint.Check(version) {
				continue
			}
		}

		image := os.Getenv(rule.ImageRef)
		if image == "" {
			return consoleplugin.Image{}, fmt.Errorf("environment variable %s is not set", rule.ImageRef)
		}
		return consoleplugin.Image{URL: image, Runtime: rule.Runtime}, nil
	}

	return consoleplugin.Image{}, fmt.Errorf("no console plugin image rule matched OpenShift version %q", openshiftVersion)
}

// GetConsolePluginImageOverride validates the runtime of an explicit image.
// Without an image the override is inactive, allowing removal to clean up
// development deployments even if their runtime setting is still present.
func GetConsolePluginImageOverride(image, runtime string) (consoleplugin.Image, error) {
	if image == "" {
		return consoleplugin.Image{}, nil
	}
	if runtime == "" {
		runtime = string(consoleplugin.RuntimeGo)
	}
	switch consoleplugin.Runtime(runtime) {
	case consoleplugin.RuntimeNginx, consoleplugin.RuntimeGo:
		return consoleplugin.Image{URL: image, Runtime: consoleplugin.Runtime(runtime)}, nil
	default:
		return consoleplugin.Image{}, fmt.Errorf("invalid %s %q: expected nginx or go", ConsolePluginRuntimeOverrideEnvVar, runtime)
	}
}
