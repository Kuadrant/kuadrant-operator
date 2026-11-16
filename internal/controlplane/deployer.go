package controlplane

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/utils/env"

	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/internal/helm"
)

var chartsBasePath = env.GetString("CHARTS_PATH", "/charts")

// platformTelemetryEnabledEnvVar opts the operator out of OpenShift platform
// telemetry when set to false. Telemetry stays on by default on OpenShift.
const platformTelemetryEnabledEnvVar = "PLATFORM_TELEMETRY_ENABLED"

const (
	// PlatformTelemetryNamespace holds the ServiceMonitors and PrometheusRules
	// that OpenShift platform monitoring reads for component telemetry. It is
	// labelled for platform monitoring so the operator namespace never has to
	// be, which keeps that namespace under user workload monitoring.
	PlatformTelemetryNamespace = "kuadrant-telemetry"

	clusterMonitoringLabel           = "openshift.io/cluster-monitoring"
	platformTelemetryOwnerAnnotation = "kuadrant.io/platform-telemetry-owner-uid"
)

// Component describes a child operator deployed by the kuadrant-operator.
// The allComponents() registry is the single source of truth — deployment
// readiness and CRD bootstrap derive from these entries.
type Component struct {
	// Name identifies the component (e.g., "dns-operator"). Used in logging and status.
	Name string
	// DeploymentName is the expected name of the child operator's Deployment
	// in the operator namespace. Used for readiness checks and post-render patching.
	DeploymentName string
	// CRDNames lists the CRD names managed by this component. Used for
	// watch predicates and status reporting without needing to render the chart.
	CRDNames []string
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
	// RelatedImageEnvVars lists env var names (e.g. "RELATED_IMAGE_AUTHORINO")
	// read from kuadrant-operator's own environment and used to override
	// same-named env vars in containers[0] of DeploymentName post-render.
	// Stopgap for charts where a related image is a hardcoded literal in an
	// env var rather than a Helm value -- once a chart adds value-based
	// configurability for it, prefer ChartValueOverrides instead (see
	// mcp-gateway's broker image for that pattern).
	RelatedImageEnvVars []string
	// PlatformMonitoringNamespace, when set, is created and labelled before
	// telemetry resources are applied. It is not owned by the KCP so deletion
	// cannot garbage-collect unrelated objects later added to the namespace.
	PlatformMonitoringNamespace string
}

type Deployer struct {
	client             dynamic.Interface
	discovery          discovery.DiscoveryInterface
	applier            *ResourceApplier
	namespace          string
	components         []Component
	platformMonitoring bool
	telemetryDisabled  bool
	telemetryCondition metav1.Condition
	chartVersions      map[string]string
	deployedImages     map[string][]DeployedImage
	logger             logr.Logger
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
	telemetryEnabled, err := env.GetBool(platformTelemetryEnabledEnvVar, true)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", platformTelemetryEnabledEnvVar, err)
	}
	return &Deployer{
		client:            client,
		discovery:         disc,
		telemetryDisabled: !telemetryEnabled,
		applier:           NewResourceApplier(client, disc, namespace, l),
		namespace:         namespace,
		components:        allComponents(),
		chartVersions:     make(map[string]string),
		deployedImages:    make(map[string][]DeployedImage),
		logger:            l,
	}, nil
}

func (d *Deployer) RefreshPlatformMonitoring(ctx context.Context, ownerRef *metav1.OwnerReference) error {
	if d.discovery == nil {
		return nil
	}
	if d.telemetryDisabled {
		return d.disablePlatformTelemetry(ctx, ownerRef, "Disabled", platformTelemetryEnabledEnvVar+" is false")
	}
	enabled, err := hasPlatformMonitoring(d.discovery)
	if err != nil {
		d.setTelemetryCondition(metav1.ConditionFalse, "DiscoveryFailed", err.Error())
		return err
	}
	if enabled {
		if !d.platformMonitoring && d.applier != nil {
			d.applier.ResetRESTMapper()
		}
		d.platformMonitoring = true
		d.components = withPlatformTelemetry(allComponents())
		d.setTelemetryCondition(metav1.ConditionUnknown, "Pending", "platform telemetry setup has not completed")
		return nil
	}
	return d.disablePlatformTelemetry(ctx, ownerRef, "PlatformMonitoringUnavailable", "required OpenShift monitoring APIs are not served")
}

// disablePlatformTelemetry turns telemetry off and removes the telemetry
// resources this KuadrantControlPlane owns. The namespace is left in place.
func (d *Deployer) disablePlatformTelemetry(ctx context.Context, ownerRef *metav1.OwnerReference, reason, message string) error {
	d.platformMonitoring = false
	d.components = allComponents()
	if ownerRef != nil && d.client != nil {
		if err := d.cleanupPlatformTelemetry(ctx, ownerRef); err != nil {
			d.setTelemetryCondition(metav1.ConditionFalse, "TelemetryCleanupFailed", err.Error())
			return fmt.Errorf("cleaning platform telemetry resources: %w", err)
		}
	}
	d.setTelemetryCondition(metav1.ConditionFalse, reason, message)
	return nil
}

func (d *Deployer) cleanupPlatformTelemetry(ctx context.Context, ownerRef *metav1.OwnerReference) error {
	if ownerRef == nil {
		return fmt.Errorf("missing KuadrantControlPlane owner")
	}
	resources := []struct {
		gvr       schema.GroupVersionResource
		namespace string
		name      string
	}{
		{gvr: schema.GroupVersionResource{Version: "v1", Resource: "services"}, namespace: d.namespace, name: "mcp-gateway-controller-metrics"},
		{gvr: schema.GroupVersionResource{Group: "monitoring.coreos.com", Version: "v1", Resource: "servicemonitors"}, namespace: PlatformTelemetryNamespace, name: "mcp-gateway-controller"},
		{gvr: schema.GroupVersionResource{Group: "monitoring.coreos.com", Version: "v1", Resource: "prometheusrules"}, namespace: PlatformTelemetryNamespace, name: "mcp-gateway-telemetry"},
		{gvr: schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "roles"}, namespace: d.namespace, name: "mcp-gateway-prometheus-k8s"},
		{gvr: schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "rolebindings"}, namespace: d.namespace, name: "mcp-gateway-prometheus-k8s"},
		{gvr: schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "networkpolicies"}, namespace: d.namespace, name: "mcp-gateway-platform-metrics"},
	}
	var errs []error
	for _, item := range resources {
		var resource dynamic.ResourceInterface
		if item.namespace == "" {
			resource = d.client.Resource(item.gvr)
		} else {
			resource = d.client.Resource(item.gvr).Namespace(item.namespace)
		}
		object, err := resource.Get(ctx, item.name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("getting %s %s/%s: %w", item.gvr.Resource, item.namespace, item.name, err))
			continue
		}
		if !ownedByControlPlane(object, ownerRef) {
			continue
		}
		options := metav1.DeleteOptions{}
		if uid := object.GetUID(); uid != "" {
			options.Preconditions = &metav1.Preconditions{UID: &uid}
		}
		if err := resource.Delete(ctx, item.name, options); err != nil && !apierrors.IsNotFound(err) {
			errs = append(errs, fmt.Errorf("deleting %s %s/%s: %w", item.gvr.Resource, item.namespace, item.name, err))
		}
	}
	return errors.Join(errs...)
}

func ownedByControlPlane(object *unstructured.Unstructured, ownerRef *metav1.OwnerReference) bool {
	for _, existing := range object.GetOwnerReferences() {
		if existing.UID == ownerRef.UID && sameControlPlaneOwner(&existing, ownerRef) {
			return true
		}
	}
	return false
}

func (d *Deployer) TelemetryCondition() metav1.Condition {
	if d.telemetryCondition.Type == "" {
		return metav1.Condition{Type: kuadrantv1alpha1.ControlPlaneConditionTelemetryReady, Status: metav1.ConditionUnknown, Reason: "Pending", Message: "platform telemetry has not been detected"}
	}
	return d.telemetryCondition
}

func (d *Deployer) setTelemetryCondition(status metav1.ConditionStatus, reason, message string) {
	d.telemetryCondition = metav1.Condition{
		Type:               kuadrantv1alpha1.ControlPlaneConditionTelemetryReady,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	}
}

// hasPlatformMonitoring reports whether both OpenShift monitoring APIs are
// served. A missing API is a normal unsupported-cluster result; other errors
// remain retryable discovery failures.
func hasPlatformMonitoring(disc discovery.DiscoveryInterface) (bool, error) {
	for _, gv := range []string{"config.openshift.io/v1", "monitoring.coreos.com/v1"} {
		if _, err := disc.ServerResourcesForGroupVersion(gv); err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, fmt.Errorf("discover API group version %s: %w", gv, err)
		}
	}
	return true, nil
}

// withPlatformTelemetry enables mcp-gateway's protocol version telemetry for
// OpenShift platform monitoring. The chart puts its ServiceMonitor and
// PrometheusRule in PlatformTelemetryNamespace, scraping the controller in
// the operator namespace.
func withPlatformTelemetry(components []Component) []Component {
	for i := range components {
		if components[i].Name != "mcp-gateway" {
			continue
		}
		components[i].ChartValueOverrides = append(components[i].ChartValueOverrides, &StaticValue{
			ValueKey: "telemetry.openshiftPlatformMonitoring",
			Value:    map[string]any{"enabled": true, "namespace": PlatformTelemetryNamespace},
		})
		components[i].PlatformMonitoringNamespace = PlatformTelemetryNamespace
	}
	return components
}

// platformMonitoringNamespace builds a namespace labelled for OpenShift
// platform monitoring.
func platformMonitoringNamespace(name string) *unstructured.Unstructured {
	ns := &unstructured.Unstructured{}
	ns.SetAPIVersion("v1")
	ns.SetKind("Namespace")
	ns.SetName(name)
	ns.SetLabels(map[string]string{
		clusterMonitoringLabel:         "true",
		"app.kubernetes.io/managed-by": "kuadrant-operator",
	})
	return ns
}

func canManagePlatformTelemetryNamespace(namespace *unstructured.Unstructured, operatorNamespace string, ownerRef *metav1.OwnerReference) bool {
	if operatorNamespace == PlatformTelemetryNamespace {
		return false
	}
	if namespace == nil {
		return ownerRef != nil && ownerRef.UID != ""
	}
	if ownerRef == nil || ownerRef.UID == "" {
		return false
	}
	owners := namespace.GetOwnerReferences()
	if len(owners) == 0 {
		return namespace.GetAnnotations()[platformTelemetryOwnerAnnotation] != ""
	}
	return len(owners) == 1 && sameControlPlaneOwner(&owners[0], ownerRef)
}

func sameControlPlaneOwner(existing, expected *metav1.OwnerReference) bool {
	return existing.APIVersion == expected.APIVersion && existing.Kind == expected.Kind && existing.Name == expected.Name
}

func splitPlatformTelemetryResources(resources []*unstructured.Unstructured) ([]*unstructured.Unstructured, []*unstructured.Unstructured) {
	coreResources := make([]*unstructured.Unstructured, 0, len(resources))
	telemetryResources := make([]*unstructured.Unstructured, 0, 5)
	for _, resource := range resources {
		telemetry := false
		switch resource.GetKind() {
		case "Namespace":
			telemetry = resource.GetName() == PlatformTelemetryNamespace
		case "ServiceMonitor", "PrometheusRule":
			telemetry = resource.GetNamespace() == PlatformTelemetryNamespace
		case "Service":
			telemetry = strings.HasSuffix(resource.GetName(), "-controller-metrics")
		case "Role", "RoleBinding":
			telemetry = strings.HasSuffix(resource.GetName(), "-prometheus-k8s")
		}
		if telemetry {
			telemetryResources = append(telemetryResources, resource)
		} else {
			coreResources = append(coreResources, resource)
		}
	}
	return coreResources, telemetryResources
}

func setControllerRecreateStrategy(resources []*unstructured.Unstructured, deploymentName string) error {
	for _, resource := range resources {
		if resource.GetKind() != "Deployment" || resource.GetName() != deploymentName {
			continue
		}
		if err := unstructured.SetNestedMap(resource.Object, map[string]interface{}{"type": "Recreate"}, "spec", "strategy"); err != nil {
			return fmt.Errorf("setting deployment strategy: %w", err)
		}
		return nil
	}
	return nil
}

func platformPrometheusNetworkPolicy(namespace string, deployment *unstructured.Unstructured, policyName string) (*unstructured.Unstructured, error) {
	selector, found, err := unstructured.NestedStringMap(deployment.Object, "spec", "selector", "matchLabels")
	if err != nil {
		return nil, fmt.Errorf("reading %s pod selector: %w", deployment.GetName(), err)
	}
	if !found || len(selector) == 0 {
		return nil, fmt.Errorf("deployment %s has no pod selector", deployment.GetName())
	}

	policy := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "networking.k8s.io/v1",
		"kind":       "NetworkPolicy",
		"metadata": map[string]interface{}{
			"name":      policyName,
			"namespace": namespace,
			"labels": map[string]interface{}{
				"app.kubernetes.io/managed-by": "kuadrant-operator",
			},
		},
		"spec": map[string]interface{}{
			"podSelector": map[string]interface{}{
				"matchLabels": stringMapToInterfaceMap(selector),
			},
			"policyTypes": []interface{}{"Ingress"},
			"ingress": []interface{}{
				map[string]interface{}{
					"from": []interface{}{
						map[string]interface{}{
							"namespaceSelector": map[string]interface{}{
								"matchLabels": map[string]interface{}{
									"kubernetes.io/metadata.name": "openshift-monitoring",
								},
							},
							"podSelector": map[string]interface{}{
								"matchLabels": map[string]interface{}{
									"app.kubernetes.io/name":     "prometheus",
									"app.kubernetes.io/instance": "k8s",
								},
							},
						},
					},
					"ports": []interface{}{
						map[string]interface{}{"protocol": "TCP", "port": int64(8082)},
					},
				},
			},
		},
	}}
	return policy, nil
}

func stringMapToInterfaceMap(values map[string]string) map[string]interface{} {
	result := make(map[string]interface{}, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func (d *Deployer) ensurePlatformTelemetryNamespace(ctx context.Context, ownerRef *metav1.OwnerReference) bool {
	if d.namespace == PlatformTelemetryNamespace {
		d.setTelemetryCondition(metav1.ConditionFalse, "NamespaceConflict", "operator installation namespace cannot also be the platform telemetry namespace")
		d.logger.Error(fmt.Errorf("operator installation namespace conflicts with platform telemetry namespace"), "skipping platform telemetry")
		return false
	}
	if ownerRef == nil || ownerRef.UID == "" {
		d.setTelemetryCondition(metav1.ConditionFalse, "NamespaceOwnershipUnavailable", "cannot create telemetry namespace without a KuadrantControlPlane owner")
		return false
	}

	namespaceGVR := schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}
	resource := d.client.Resource(namespaceGVR)
	namespace, err := resource.Get(ctx, PlatformTelemetryNamespace, metav1.GetOptions{})
	if err == nil {
		if canManagePlatformTelemetryNamespace(namespace, d.namespace, ownerRef) {
			return d.ensurePlatformTelemetryNamespaceMetadata(ctx, resource, namespace, ownerRef)
		}
		d.setTelemetryCondition(metav1.ConditionFalse, "NamespaceOwnershipConflict", fmt.Sprintf("namespace %q already exists and is not owned by this KuadrantControlPlane", PlatformTelemetryNamespace))
		d.logger.Error(fmt.Errorf("refusing to adopt existing namespace"), "skipping platform telemetry", "namespace", PlatformTelemetryNamespace)
		return false
	}
	if !apierrors.IsNotFound(err) {
		d.setTelemetryCondition(metav1.ConditionFalse, "NamespaceLookupFailed", err.Error())
		d.logger.Error(err, "failed to check platform telemetry namespace", "namespace", PlatformTelemetryNamespace)
		return false
	}

	namespace = platformMonitoringNamespace(PlatformTelemetryNamespace)
	namespace.SetAnnotations(map[string]string{platformTelemetryOwnerAnnotation: string(ownerRef.UID)})
	// The namespace is deliberately not owned by the KCP; garbage collection
	// must not delete unrelated objects later added to this shared namespace.
	if _, err := resource.Create(ctx, namespace, metav1.CreateOptions{}); err == nil {
		return true
	} else if !apierrors.IsAlreadyExists(err) {
		d.setTelemetryCondition(metav1.ConditionFalse, "NamespaceCreateFailed", err.Error())
		d.logger.Error(err, "failed to create platform telemetry namespace", "namespace", PlatformTelemetryNamespace)
		return false
	}

	// Another actor created the namespace after the initial read. Verify
	// ownership before applying any telemetry resources into it.
	namespace, err = resource.Get(ctx, PlatformTelemetryNamespace, metav1.GetOptions{})
	if err != nil {
		d.setTelemetryCondition(metav1.ConditionFalse, "NamespaceLookupFailed", err.Error())
		d.logger.Error(err, "failed to verify platform telemetry namespace after create race")
		return false
	}
	if !canManagePlatformTelemetryNamespace(namespace, d.namespace, ownerRef) {
		d.setTelemetryCondition(metav1.ConditionFalse, "NamespaceOwnershipConflict", fmt.Sprintf("namespace %q was created by another owner", PlatformTelemetryNamespace))
		d.logger.Error(fmt.Errorf("refusing to adopt namespace created concurrently"), "skipping platform telemetry", "namespace", PlatformTelemetryNamespace)
		return false
	}
	return d.ensurePlatformTelemetryNamespaceMetadata(ctx, resource, namespace, ownerRef)
}

func (d *Deployer) ensurePlatformTelemetryNamespaceMetadata(ctx context.Context, resource dynamic.ResourceInterface, namespace *unstructured.Unstructured, ownerRef *metav1.OwnerReference) bool {
	labels := namespace.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	changed := false
	if labels[clusterMonitoringLabel] != "true" {
		labels[clusterMonitoringLabel] = "true"
		changed = true
	}
	if labels["app.kubernetes.io/managed-by"] != "kuadrant-operator" {
		labels["app.kubernetes.io/managed-by"] = "kuadrant-operator"
		changed = true
	}
	namespace.SetLabels(labels)

	annotations := namespace.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	if annotations[platformTelemetryOwnerAnnotation] != string(ownerRef.UID) {
		annotations[platformTelemetryOwnerAnnotation] = string(ownerRef.UID)
		changed = true
	}
	namespace.SetAnnotations(annotations)
	if len(namespace.GetOwnerReferences()) != 0 {
		namespace.SetOwnerReferences(nil)
		changed = true
	}
	if !changed {
		return true
	}
	if _, err := resource.Update(ctx, namespace, metav1.UpdateOptions{}); err != nil {
		d.setTelemetryCondition(metav1.ConditionFalse, "NamespaceUpdateFailed", err.Error())
		d.logger.Error(err, "failed to update platform telemetry namespace", "namespace", PlatformTelemetryNamespace)
		return false
	}
	return true
}

func (d *Deployer) applyPlatformTelemetry(ctx context.Context, component Component, coreResources, telemetryResources []*unstructured.Unstructured, ownerRef *metav1.OwnerReference) {
	if !d.ensurePlatformTelemetryNamespace(ctx, ownerRef) {
		return
	}
	nonNamespaceResources := telemetryResources[:0]
	for _, resource := range telemetryResources {
		if resource.GetKind() != "Namespace" {
			nonNamespaceResources = append(nonNamespaceResources, resource)
		}
	}
	telemetryResources = nonNamespaceResources

	var deployment *unstructured.Unstructured
	for _, resource := range coreResources {
		if resource.GetKind() == "Deployment" && resource.GetName() == component.DeploymentName {
			deployment = resource
			break
		}
	}
	if deployment == nil {
		d.setTelemetryCondition(metav1.ConditionFalse, "ControllerDeploymentMissing", fmt.Sprintf("deployment %q is not present in the rendered chart", component.DeploymentName))
		return
	}
	policy, err := platformPrometheusNetworkPolicy(d.namespace, deployment, "mcp-gateway-platform-metrics")
	if err != nil {
		d.setTelemetryCondition(metav1.ConditionFalse, "NetworkPolicyRenderFailed", err.Error())
		d.logger.Error(err, "failed to render platform Prometheus NetworkPolicy")
		return
	}
	telemetryResources = append(telemetryResources, policy)
	// Recheck ownership immediately before writing namespaced telemetry resources.
	if !d.ensurePlatformTelemetryNamespace(ctx, ownerRef) {
		return
	}
	SortByInstallOrder(telemetryResources)
	if err := d.applier.ApplyResources(ctx, telemetryResources, ownerRef); err != nil {
		d.setTelemetryCondition(metav1.ConditionFalse, "TelemetryApplyFailed", err.Error())
		d.logger.Error(err, "failed to apply platform telemetry resources")
		return
	}
	d.setTelemetryCondition(metav1.ConditionTrue, "PlatformTelemetryReady", "platform monitoring resources are installed")
}

func allComponents() []Component {
	return []Component{
		{
			Name:           "dns-operator",
			ChartPath:      chartsBasePath + "/dns-operator",
			ImageEnvVar:    "RELATED_IMAGE_DNS_OPERATOR",
			DeploymentName: "dns-operator-controller-manager",
			CRDNames:       []string{"dnsrecords.kuadrant.io", "dnshealthcheckprobes.kuadrant.io"},
		},
		{
			Name:           "mcp-gateway",
			ChartPath:      chartsBasePath + "/mcp-gateway",
			DeploymentName: "mcp-gateway-controller",
			CRDNames:       []string{"mcpgatewayextensions.mcp.kuadrant.io", "mcpserverregistrations.mcp.kuadrant.io", "mcpvirtualservers.mcp.kuadrant.io"},
			ChartValues: map[string]any{
				"mcpGatewayExtension": map[string]any{"create": false},
				"gateway":             map[string]any{"create": false},
			},
			ChartValueOverrides: []ChartValueOverride{
				&ImageSplitValue{ImageValue: ImageValue{EnvVar: "RELATED_IMAGE_MCP_GATEWAY", ValueKey: "imageController", Description: "controller"}},
				&ImageSplitValue{ImageValue: ImageValue{EnvVar: "RELATED_IMAGE_MCP_GATEWAY_BROKER", ValueKey: "image", Description: "broker"}},
			},
		},
		{
			Name:                "authorino-operator",
			ChartPath:           chartsBasePath + "/authorino-operator",
			ImageEnvVar:         "RELATED_IMAGE_AUTHORINO_OPERATOR",
			DeploymentName:      "authorino-operator",
			CRDNames:            []string{"authconfigs.authorino.kuadrant.io", "authorinos.operator.authorino.kuadrant.io"},
			RelatedImageEnvVars: []string{"RELATED_IMAGE_AUTHORINO"},
		},
		{
			Name:                "limitador-operator",
			ChartPath:           chartsBasePath + "/limitador-operator",
			ImageEnvVar:         "RELATED_IMAGE_LIMITADOR_OPERATOR",
			DeploymentName:      "limitador-operator-controller-manager",
			CRDNames:            []string{"limitadors.limitador.kuadrant.io"},
			RelatedImageEnvVars: []string{"RELATED_IMAGE_LIMITADOR"},
		},
		{
			Name:           "developer-portal-controller",
			ChartPath:      chartsBasePath + "/developer-portal-controller",
			DeploymentName: "developer-portal-controller",
			CRDNames: []string{
				"apiproducts.devportal.kuadrant.io",
				"apikeys.devportal.kuadrant.io",
				"apikeyrequests.devportal.kuadrant.io",
				"apikeyapprovals.devportal.kuadrant.io",
			},
			ChartValueOverrides: []ChartValueOverride{
				&ImageSplitValue{ImageValue: ImageValue{EnvVar: "RELATED_IMAGE_DEVELOPERPORTAL", ValueKey: "image", Description: "controller"}},
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

		d.logger.V(1).Info("applying CRDs", "component", component.Name, "count", len(rendered.CRDs))
		if err := applier.ApplyResources(ctx, rendered.CRDs, nil); err != nil {
			return fmt.Errorf("applying CRDs for %s: %w", component.Name, err)
		}
		if err := applier.WaitForCRDs(ctx, CRDNames(rendered.CRDs)); err != nil {
			return fmt.Errorf("waiting for CRDs for %s: %w", component.Name, err)
		}
	}
	return nil
}

// DeployComponent renders and applies a single component's chart. ownerRef,
// if non-nil, is set on every applied resource except CRDs (see
// ApplyResources), so deleting the KuadrantControlPlane CR cascade-deletes
// everything the deployer created for this component.
func (d *Deployer) DeployComponent(ctx context.Context, component Component, ownerRef *metav1.OwnerReference) error {
	applier := d.applier

	rendered, err := d.renderComponent(component)
	if err != nil {
		return fmt.Errorf("rendering %s: %w", component.Name, err)
	}

	d.chartVersions[component.Name] = rendered.ChartVersion

	if len(rendered.CRDs) > 0 {
		if err := applier.ApplyResources(ctx, rendered.CRDs, nil); err != nil {
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

	// Post-render env var patching for charts that bake a related image into
	// an env var as a hardcoded literal (see RelatedImageEnvVars doc comment).
	if len(component.RelatedImageEnvVars) > 0 {
		envVars := make(map[string]string, len(component.RelatedImageEnvVars))
		for _, name := range component.RelatedImageEnvVars {
			envVars[name] = os.Getenv(name)
		}
		if err := PatchContainerEnvVars(rendered.Resources, component.DeploymentName, envVars); err != nil {
			return fmt.Errorf("patching related image env vars for %s: %w", component.Name, err)
		}
	}

	if component.Name == "mcp-gateway" {
		if err := setControllerRecreateStrategy(rendered.Resources, component.DeploymentName); err != nil {
			return fmt.Errorf("setting %s deployment strategy: %w", component.DeploymentName, err)
		}
	}

	coreResources := rendered.Resources
	var telemetryResources []*unstructured.Unstructured
	if component.PlatformMonitoringNamespace != "" {
		coreResources, telemetryResources = splitPlatformTelemetryResources(rendered.Resources)
	}
	SortByInstallOrder(coreResources)
	d.deployedImages[component.Name] = extractDeploymentImages(coreResources)

	if err := applier.ApplyResources(ctx, coreResources, ownerRef); err != nil {
		if component.PlatformMonitoringNamespace != "" {
			d.setTelemetryCondition(metav1.ConditionFalse, "ComponentDeploymentFailed", err.Error())
		}
		return fmt.Errorf("applying resources for %s: %w", component.Name, err)
	}

	if component.PlatformMonitoringNamespace != "" {
		if len(telemetryResources) == 0 {
			d.setTelemetryCondition(metav1.ConditionFalse, "ChartNotSynchronized", "the mcp-gateway chart does not contain platform telemetry resources")
			d.logger.Error(fmt.Errorf("platform telemetry resources are missing from the component chart"), "skipping platform telemetry")
		} else {
			d.applyPlatformTelemetry(ctx, component, coreResources, telemetryResources, ownerRef)
		}
	}

	d.logger.V(1).Info("component deployed", "component", component.Name,
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
