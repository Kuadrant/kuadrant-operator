package controlplane

import (
	"context"
	"fmt"
	"os"

	"github.com/go-logr/logr"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/utils/env"

	"github.com/kuadrant/kuadrant-operator/internal/helm"
)

var chartsBasePath = env.GetString("CHARTS_PATH", "/charts")

// Component describes a child operator deployed by the kuadrant-operator.
// The allComponents() registry is the single source of truth — OLM cleanup,
// deployment readiness, and CRD bootstrap all derive from these entries.
type Component struct {
	// Name identifies the component (e.g., "dns-operator"). Used in logging and status.
	Name string
	// DeploymentName is the expected name of the child operator's Deployment
	// in the operator namespace. Used for readiness checks and post-render patching.
	DeploymentName string
	// CRDNames lists the CRD names managed by this component. Used for
	// watch predicates and status reporting without needing to render the chart.
	CRDNames []string
	// OLMPackageName is the OLM package name used to identify orphaned
	// Subscriptions and CSVs during migration cleanup. Empty if the component
	// was never an OLM package (e.g., a new component added post-consolidation).
	OLMPackageName string
	// ImageEnvVar is the environment variable name for the RELATED_IMAGE override
	// (e.g., "RELATED_IMAGE_DNS_OPERATOR"). If empty, the chart default image is used.
	// Applies via post-render patching of containers[0].image on DeploymentName.
	ImageEnvVar string

	// ChartPath is the filesystem path to the Helm chart inside the container image.
	ChartPath string
	// ChartValues provides static Helm values for rendering. These are passed
	// directly to the Helm SDK as-is.
	ChartValues map[string]any
	// ChartValueOverrides applies dynamic overrides to Helm chart values at
	// render time. Each override reads from an env var and sets the value at
	// the specified chart value key. Keys support dotted paths for nested
	// values (e.g., "controller.image" sets values["controller"]["image"]).
	ChartValueOverrides []ChartValueOverride
}

type Deployer struct {
	client         dynamic.Interface
	discovery      discovery.DiscoveryInterface
	applier        *ResourceApplier
	namespace      string
	components     []Component
	chartVersions  map[string]string
	deployedImages map[string][]DeployedImage
	logger         logr.Logger
}

func NewDeployer(restConfig *rest.Config, namespace string, logger logr.Logger) (*Deployer, error) {
	client, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("creating dynamic client: %w", err)
	}
	disc, err := discovery.NewDiscoveryClientForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("creating discovery client: %w", err)
	}

	l := logger.WithName("deployer")
	return &Deployer{
		client:         client,
		discovery:      disc,
		applier:        NewResourceApplier(client, disc, namespace, l),
		namespace:      namespace,
		components:     allComponents(),
		chartVersions:  make(map[string]string),
		deployedImages: make(map[string][]DeployedImage),
		logger:         l,
	}, nil
}

func allComponents() []Component {
	return []Component{
		{
			Name:           "dns-operator",
			ChartPath:      chartsBasePath + "/dns-operator",
			ImageEnvVar:    "RELATED_IMAGE_DNS_OPERATOR",
			DeploymentName: "dns-operator-controller-manager",
			CRDNames:       []string{"dnsrecords.kuadrant.io", "dnshealthcheckprobes.kuadrant.io"},
			OLMPackageName: "dns-operator",
		},
		{
			Name:           "mcp-gateway",
			ChartPath:      chartsBasePath + "/mcp-gateway",
			DeploymentName: "mcp-gateway-controller",
			CRDNames:       []string{"mcpgatewayextensions.mcp.kuadrant.io", "mcpserverregistrations.mcp.kuadrant.io", "mcpvirtualservers.mcp.kuadrant.io"},
			OLMPackageName: "mcp-gateway",
			ChartValues: map[string]any{
				"mcpGatewayExtension": map[string]any{"create": false},
				"gateway":             map[string]any{"create": false},
			},
			ChartValueOverrides: []ChartValueOverride{
				&ImageSplitValue{ImageValue: ImageValue{EnvVar: "RELATED_IMAGE_MCP_GATEWAY", ValueKey: "imageController", Description: "controller"}},
				&ImageSplitValue{ImageValue: ImageValue{EnvVar: "RELATED_IMAGE_MCP_GATEWAY_BROKER", ValueKey: "image", Description: "broker"}},
			},
		},
	}
}

func (d *Deployer) DeploymentNames() []string {
	names := make([]string, 0, len(d.components))
	for _, c := range d.components {
		if c.DeploymentName != "" {
			names = append(names, c.DeploymentName)
		}
	}
	return names
}

func (d *Deployer) CRDNames() []string {
	var names []string
	for _, c := range d.components {
		names = append(names, c.CRDNames...)
	}
	return names
}

func (d *Deployer) OLMPackageNames() []string {
	names := make([]string, 0, len(d.components))
	for _, c := range d.components {
		if c.OLMPackageName != "" {
			names = append(names, c.OLMPackageName)
		}
	}
	return names
}

// EnabledComponents returns components that should be deployed.
// Currently, all components are always enabled.
// Future: filter based on KuadrantControlPlane spec.
func (d *Deployer) EnabledComponents() []Component {
	return d.components
}

func (d *Deployer) ComponentByName(name string) (Component, bool) {
	for _, c := range d.components {
		if c.Name == name {
			return c, true
		}
	}
	return Component{}, false
}

func (d *Deployer) Namespace() string {
	return d.namespace
}

func (d *Deployer) DynamicClient() dynamic.Interface {
	return d.client
}

func (d *Deployer) DiscoveryClient() discovery.DiscoveryInterface {
	return d.discovery
}

func (d *Deployer) ChartVersion(componentName string) string {
	return d.chartVersions[componentName]
}

type DeployedImage struct {
	Container string
	Image     string
}

func (d *Deployer) DeployedImages(componentName string) []DeployedImage {
	return d.deployedImages[componentName]
}

func (d *Deployer) ApplyCRDsForComponents(ctx context.Context, components []Component) error {
	applier := d.applier

	for _, component := range components {
		rendered, err := d.renderComponent(component)
		if err != nil {
			return fmt.Errorf("rendering %s for CRD bootstrap: %w", component.Name, err)
		}

		if len(rendered.CRDs) == 0 {
			continue
		}

		d.logger.Info("applying CRDs", "component", component.Name, "count", len(rendered.CRDs))
		if err := applier.ApplyResources(ctx, rendered.CRDs); err != nil {
			return fmt.Errorf("applying CRDs for %s: %w", component.Name, err)
		}
		if err := applier.WaitForCRDs(ctx, CRDNames(rendered.CRDs)); err != nil {
			return fmt.Errorf("waiting for CRDs for %s: %w", component.Name, err)
		}
	}
	return nil
}

func (d *Deployer) DeployComponent(ctx context.Context, component Component) error {
	applier := d.applier

	rendered, err := d.renderComponent(component)
	if err != nil {
		return fmt.Errorf("rendering %s: %w", component.Name, err)
	}

	d.chartVersions[component.Name] = rendered.ChartVersion

	if len(rendered.CRDs) > 0 {
		if err := applier.ApplyResources(ctx, rendered.CRDs); err != nil {
			return fmt.Errorf("applying CRDs for %s: %w", component.Name, err)
		}
		if err := applier.WaitForCRDs(ctx, CRDNames(rendered.CRDs)); err != nil {
			return fmt.Errorf("waiting for CRDs for %s: %w", component.Name, err)
		}
	}

	SortByInstallOrder(rendered.Resources)

	// Post-render image patching for charts without values-based image config.
	if component.ImageEnvVar != "" {
		image := os.Getenv(component.ImageEnvVar)
		if err := PatchDeploymentImage(rendered.Resources, image); err != nil {
			return fmt.Errorf("patching image for %s: %w", component.Name, err)
		}
	}

	d.deployedImages[component.Name] = extractDeploymentImages(rendered.Resources)

	if err := applier.ApplyResources(ctx, rendered.Resources); err != nil {
		return fmt.Errorf("applying resources for %s: %w", component.Name, err)
	}

	d.logger.Info("component deployed", "component", component.Name,
		"crds", len(rendered.CRDs), "resources", len(rendered.Resources))
	return nil
}

func (d *Deployer) renderComponent(component Component) (*helm.RenderedChart, error) {
	renderer := helm.NewRenderer(component.ChartPath)
	values := component.effectiveValues()
	return renderer.Render(component.Name, d.namespace, values)
}

// effectiveValues merges ChartValues with ChartValueOverrides.
// Static ChartValues are copied first, then each ChartValueOverride is applied
// (which may read from env vars and set/override chart values).
// ImageEnvVar-based post-render patching is handled separately in DeployComponent.
func (c Component) effectiveValues() map[string]any {
	if len(c.ChartValues) == 0 && len(c.ChartValueOverrides) == 0 {
		return nil
	}

	values := make(map[string]any)
	for k, v := range c.ChartValues {
		values[k] = v
	}

	for _, vm := range c.ChartValueOverrides {
		vm.Apply(values)
	}

	if len(values) == 0 {
		return nil
	}
	return values
}
