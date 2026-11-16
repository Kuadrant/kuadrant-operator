package controlplane

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestWithPlatformTelemetry(t *testing.T) {
	components := withPlatformTelemetry(allComponents())

	for _, c := range components {
		if c.Name != "mcp-gateway" {
			if c.PlatformMonitoringNamespace != "" {
				t.Errorf("%s: PlatformMonitoringNamespace = %q, want empty", c.Name, c.PlatformMonitoringNamespace)
			}
			continue
		}
		if c.PlatformMonitoringNamespace != PlatformTelemetryNamespace {
			t.Errorf("PlatformMonitoringNamespace = %q, want %q", c.PlatformMonitoringNamespace, PlatformTelemetryNamespace)
		}
		telemetry, ok := c.effectiveValues()["telemetry"].(map[string]any)
		if !ok {
			t.Fatal("expected telemetry values for mcp-gateway")
		}
		pm, ok := telemetry["openshiftPlatformMonitoring"].(map[string]any)
		if !ok {
			t.Fatal("expected telemetry.openshiftPlatformMonitoring values")
		}
		if pm["enabled"] != true || pm["namespace"] != PlatformTelemetryNamespace {
			t.Errorf("openshiftPlatformMonitoring = %v", pm)
		}
		// existing static values are kept
		if gw, _ := c.effectiveValues()["gateway"].(map[string]any); gw["create"] != false {
			t.Errorf("gateway.create = %v, want false", gw["create"])
		}
	}
}

func TestDefaultComponentsHaveNoPlatformTelemetry(t *testing.T) {
	for _, c := range allComponents() {
		if c.PlatformMonitoringNamespace != "" {
			t.Errorf("%s: telemetry must only be enabled on OpenShift", c.Name)
		}
		if _, ok := c.effectiveValues()["telemetry"]; ok {
			t.Errorf("%s: unexpected telemetry values without OpenShift", c.Name)
		}
	}
}

func TestPlatformMonitoringNamespace(t *testing.T) {
	ns := platformMonitoringNamespace(PlatformTelemetryNamespace)
	if ns.GetKind() != "Namespace" || ns.GetName() != PlatformTelemetryNamespace {
		t.Errorf("got %s/%s", ns.GetKind(), ns.GetName())
	}
	if ns.GetLabels()[clusterMonitoringLabel] != "true" {
		t.Errorf("missing %s label: %v", clusterMonitoringLabel, ns.GetLabels())
	}
	if ns.GetNamespace() != "" {
		t.Errorf("namespace must be cluster-scoped, got namespace %q", ns.GetNamespace())
	}
}

func TestStaticValue(t *testing.T) {
	values := map[string]any{"telemetry": map[string]any{"interval": "5m"}}
	(&StaticValue{ValueKey: "telemetry.openshiftPlatformMonitoring", Value: map[string]any{"enabled": true}}).Apply(values)
	telemetry := values["telemetry"].(map[string]any)
	if telemetry["interval"] != "5m" {
		t.Errorf("existing sibling value lost: %v", telemetry)
	}
	if pm, _ := telemetry["openshiftPlatformMonitoring"].(map[string]any); pm["enabled"] != true {
		t.Errorf("openshiftPlatformMonitoring = %v", telemetry["openshiftPlatformMonitoring"])
	}
}

type discoveryStub struct {
	discovery.DiscoveryInterface
	resources map[string]*metav1.APIResourceList
	errors    map[string]error
}

func (d *discoveryStub) ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error) {
	if err := d.errors[groupVersion]; err != nil {
		return nil, err
	}
	if resources, ok := d.resources[groupVersion]; ok {
		return resources, nil
	}
	return nil, apierrors.NewNotFound(schema.GroupResource{Resource: "groupversions"}, groupVersion)
}

func TestHasPlatformMonitoringDistinguishesUnavailableFromFailure(t *testing.T) {
	tests := []struct {
		name      string
		discovery *discoveryStub
		want      bool
		wantErr   bool
	}{
		{
			name: "both required APIs are served",
			discovery: &discoveryStub{resources: map[string]*metav1.APIResourceList{
				"config.openshift.io/v1":   {},
				"monitoring.coreos.com/v1": {},
			}},
			want: true,
		},
		{
			name: "required API is absent",
			discovery: &discoveryStub{resources: map[string]*metav1.APIResourceList{
				"config.openshift.io/v1": {},
			}},
		},
		{
			name: "discovery failure is retryable",
			discovery: &discoveryStub{errors: map[string]error{
				"config.openshift.io/v1": errors.New("temporary API failure"),
			}},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := hasPlatformMonitoring(tt.discovery)
			if (err != nil) != tt.wantErr {
				t.Fatalf("hasPlatformMonitoring() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("hasPlatformMonitoring() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTelemetryNamespaceOwnership(t *testing.T) {
	owner := &metav1.OwnerReference{APIVersion: "kuadrant.io/v1alpha1", Kind: "KuadrantControlPlane", Name: "default", UID: types.UID("kcp-uid")}
	tests := []struct {
		name              string
		operatorNamespace string
		namespace         *unstructured.Unstructured
		wantManage        bool
	}{
		{name: "missing namespace", operatorNamespace: "kuadrant-system", wantManage: true},
		{name: "owned by this control plane", operatorNamespace: "kuadrant-system", namespace: namespaceWithOwner(owner), wantManage: true},
		{name: "legacy namespace from replaced default control plane", operatorNamespace: "kuadrant-system", namespace: namespaceWithOwner(&metav1.OwnerReference{APIVersion: owner.APIVersion, Kind: owner.Kind, Name: owner.Name, UID: "old-kcp-uid"}), wantManage: true},
		{name: "created by this installation", operatorNamespace: "kuadrant-system", namespace: namespaceWithMarker("kcp-uid"), wantManage: true},
		{name: "left by previous control plane", operatorNamespace: "kuadrant-system", namespace: namespaceWithMarker("old-kcp-uid"), wantManage: true},
		{name: "unowned namespace", operatorNamespace: "kuadrant-system", namespace: &unstructured.Unstructured{}, wantManage: false},
		{name: "another control plane owns it", operatorNamespace: "kuadrant-system", namespace: namespaceWithOwner(&metav1.OwnerReference{UID: "other"}), wantManage: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := canManagePlatformTelemetryNamespace(tt.namespace, tt.operatorNamespace, owner); got != tt.wantManage {
				t.Errorf("canManagePlatformTelemetryNamespace() = %v, want %v", got, tt.wantManage)
			}
		})
	}
}

func namespaceWithOwner(owner *metav1.OwnerReference) *unstructured.Unstructured {
	ns := &unstructured.Unstructured{}
	ns.SetOwnerReferences([]metav1.OwnerReference{*owner})
	return ns
}

func namespaceWithMarker(uid string) *unstructured.Unstructured {
	ns := &unstructured.Unstructured{}
	ns.SetAnnotations(map[string]string{platformTelemetryOwnerAnnotation: uid})
	ns.SetLabels(map[string]string{"app.kubernetes.io/managed-by": "kuadrant-operator"})
	return ns
}

func TestSplitPlatformTelemetryResources(t *testing.T) {
	resources := []*unstructured.Unstructured{
		telemetryTestResource("Deployment", "operator", "mcp-gateway-controller"),
		telemetryTestResource("Namespace", "", PlatformTelemetryNamespace),
		telemetryTestResource("ServiceMonitor", PlatformTelemetryNamespace, "mcp-gateway-controller"),
		telemetryTestResource("PrometheusRule", PlatformTelemetryNamespace, "mcp-gateway-telemetry"),
		telemetryTestResource("Service", "operator", "mcp-gateway-controller-metrics"),
		telemetryTestResource("Role", "operator", "mcp-gateway-prometheus-k8s"),
		telemetryTestResource("RoleBinding", "operator", "mcp-gateway-prometheus-k8s"),
	}
	coreResources, telemetryResources := splitPlatformTelemetryResources(resources)
	if len(coreResources) != 1 || coreResources[0].GetKind() != "Deployment" {
		t.Errorf("core resources = %v, want only the controller Deployment", objectKinds(coreResources))
	}
	if len(telemetryResources) != len(resources)-1 {
		t.Errorf("telemetry resource count = %d, want %d", len(telemetryResources), len(resources)-1)
	}
}

func telemetryTestResource(kind, namespace, name string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetKind(kind)
	obj.SetNamespace(namespace)
	obj.SetName(name)
	return obj
}

func objectKinds(objects []*unstructured.Unstructured) []string {
	kinds := make([]string, 0, len(objects))
	for _, obj := range objects {
		kinds = append(kinds, obj.GetKind())
	}
	return kinds
}

func TestRefreshPlatformMonitoringRetriesAfterDiscoveryFailure(t *testing.T) {
	discovery := &discoveryStub{
		errors: map[string]error{"config.openshift.io/v1": errors.New("temporary API failure")},
	}
	deployer := &Deployer{discovery: discovery, components: allComponents()}
	owner := &metav1.OwnerReference{APIVersion: "kuadrant.io/v1alpha1", Kind: "KuadrantControlPlane", Name: "default", UID: types.UID("kcp-uid")}
	if err := deployer.RefreshPlatformMonitoring(context.Background(), owner); err == nil {
		t.Fatal("RefreshPlatformMonitoring() error = nil, want transient discovery failure")
	}
	for _, component := range deployer.EnabledComponents() {
		if component.PlatformMonitoringNamespace != "" {
			t.Fatal("telemetry should remain disabled while discovery is failing")
		}
	}

	delete(discovery.errors, "config.openshift.io/v1")
	discovery.resources = map[string]*metav1.APIResourceList{
		"config.openshift.io/v1":   {},
		"monitoring.coreos.com/v1": {},
	}
	if err := deployer.RefreshPlatformMonitoring(context.Background(), owner); err != nil {
		t.Fatalf("RefreshPlatformMonitoring() after recovery: %v", err)
	}
	for _, component := range deployer.EnabledComponents() {
		if component.Name == "mcp-gateway" {
			if component.PlatformMonitoringNamespace != PlatformTelemetryNamespace {
				t.Errorf("MCP telemetry namespace = %q, want %q", component.PlatformMonitoringNamespace, PlatformTelemetryNamespace)
			}
			return
		}
	}
	t.Fatal("mcp-gateway component not found after discovery recovered")
}

func TestPlatformPrometheusNetworkPolicyIsNarrow(t *testing.T) {
	deployment := telemetryTestResource("Deployment", "kuadrant-system", "mcp-gateway-controller")
	if err := unstructured.SetNestedStringMap(deployment.Object, map[string]string{
		"app.kubernetes.io/name":     "mcp-gateway-controller",
		"app.kubernetes.io/instance": "mcp-gateway",
	}, "spec", "selector", "matchLabels"); err != nil {
		t.Fatal(err)
	}
	policy, err := platformPrometheusNetworkPolicy("kuadrant-system", deployment, "mcp-gateway-platform-metrics")
	if err != nil {
		t.Fatal(err)
	}
	target, found, err := unstructured.NestedStringMap(policy.Object, "spec", "podSelector", "matchLabels")
	if err != nil || !found || target["app.kubernetes.io/name"] != "mcp-gateway-controller" {
		t.Fatalf("target pod selector = %v, found=%v, err=%v", target, found, err)
	}
	ingress, found, err := unstructured.NestedSlice(policy.Object, "spec", "ingress")
	if err != nil || !found || len(ingress) != 1 {
		t.Fatalf("ingress = %v, found=%v, err=%v", ingress, found, err)
	}
	rule := ingress[0].(map[string]interface{})
	ports := rule["ports"].([]interface{})
	port := ports[0].(map[string]interface{})
	if port["protocol"] != "TCP" || port["port"] != int64(8082) {
		t.Errorf("allowed port = %v, want TCP/8082", port)
	}
	peers := rule["from"].([]interface{})
	peer := peers[0].(map[string]interface{})
	namespaceSelector := peer["namespaceSelector"].(map[string]interface{})["matchLabels"].(map[string]interface{})
	if namespaceSelector["kubernetes.io/metadata.name"] != "openshift-monitoring" {
		t.Errorf("source namespace selector = %v", namespaceSelector)
	}
	podSelector := peer["podSelector"].(map[string]interface{})["matchLabels"].(map[string]interface{})
	if podSelector["app.kubernetes.io/name"] != "prometheus" || podSelector["app.kubernetes.io/instance"] != "k8s" {
		t.Errorf("source pod selector = %v", podSelector)
	}
}

func TestApplyPlatformTelemetryDoesNotAdoptExistingNamespace(t *testing.T) {
	namespace := telemetryTestResource("Namespace", "", PlatformTelemetryNamespace)
	namespace.SetAPIVersion("v1")
	namespace.SetLabels(map[string]string{"customer.example/owner": "customer"})
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), namespace)
	deployer := &Deployer{client: client, namespace: "kuadrant-system"}
	owner := &metav1.OwnerReference{APIVersion: "kuadrant.io/v1alpha1", Kind: "KuadrantControlPlane", Name: "default", UID: types.UID("kcp-uid")}
	deployer.applyPlatformTelemetry(
		context.Background(),
		Component{PlatformMonitoringNamespace: PlatformTelemetryNamespace},
		[]*unstructured.Unstructured{telemetryTestResource("Deployment", "kuadrant-system", "mcp-gateway-controller")},
		[]*unstructured.Unstructured{telemetryTestResource("ServiceMonitor", PlatformTelemetryNamespace, "mcp-gateway-controller")},
		owner,
	)

	got, err := client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}).Get(context.Background(), PlatformTelemetryNamespace, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.GetOwnerReferences()) != 0 || got.GetLabels()["customer.example/owner"] != "customer" || got.GetLabels()[clusterMonitoringLabel] != "" {
		t.Errorf("pre-existing namespace was modified: owners=%v labels=%v", got.GetOwnerReferences(), got.GetLabels())
	}
	if condition := deployer.TelemetryCondition(); condition.Reason != "NamespaceOwnershipConflict" || condition.Status != metav1.ConditionFalse {
		t.Errorf("telemetry condition = %#v, want NamespaceOwnershipConflict/False", condition)
	}
}

func TestTelemetryNamespaceIsCreatedWithOwnerMarker(t *testing.T) {
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	deployer := &Deployer{client: dynamicClient, namespace: "kuadrant-system"}
	owner := &metav1.OwnerReference{APIVersion: "kuadrant.io/v1alpha1", Kind: "KuadrantControlPlane", Name: "default", UID: types.UID("kcp-uid")}
	if !deployer.ensurePlatformTelemetryNamespace(context.Background(), owner) {
		t.Fatalf("ensurePlatformTelemetryNamespace() = false, condition=%#v", deployer.TelemetryCondition())
	}
	namespace, err := dynamicClient.Resource(schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}).Get(context.Background(), PlatformTelemetryNamespace, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if namespace.GetLabels()[clusterMonitoringLabel] != "true" || len(namespace.GetOwnerReferences()) != 0 ||
		namespace.GetAnnotations()[platformTelemetryOwnerAnnotation] != string(owner.UID) {
		t.Errorf("created namespace metadata = owners %v, labels %v, annotations %v", namespace.GetOwnerReferences(), namespace.GetLabels(), namespace.GetAnnotations())
	}
}

func TestTelemetryNamespaceMarkerAndLabelDriftAreRepaired(t *testing.T) {
	namespace := telemetryTestResource("Namespace", "", PlatformTelemetryNamespace)
	namespace.SetAPIVersion("v1")
	namespace.SetAnnotations(map[string]string{platformTelemetryOwnerAnnotation: "old-kcp-uid"})
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), namespace)
	deployer := &Deployer{client: dynamicClient, namespace: "kuadrant-system"}
	owner := &metav1.OwnerReference{APIVersion: "kuadrant.io/v1alpha1", Kind: "KuadrantControlPlane", Name: "default", UID: types.UID("new-kcp-uid")}

	if !deployer.ensurePlatformTelemetryNamespace(context.Background(), owner) {
		t.Fatalf("ensurePlatformTelemetryNamespace() = false, condition=%#v", deployer.TelemetryCondition())
	}
	namespace, err := dynamicClient.Resource(schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}).Get(context.Background(), PlatformTelemetryNamespace, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if namespace.GetLabels()[clusterMonitoringLabel] != "true" ||
		namespace.GetAnnotations()[platformTelemetryOwnerAnnotation] != string(owner.UID) {
		t.Errorf("namespace metadata = labels %v, annotations %v", namespace.GetLabels(), namespace.GetAnnotations())
	}
}

func TestTelemetryNamespaceCreateRaceDoesNotAdoptNamespace(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	gvr := schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}
	racedNamespace := telemetryTestResource("Namespace", "", PlatformTelemetryNamespace)
	racedNamespace.SetAPIVersion("v1")
	racedNamespace.SetLabels(map[string]string{"customer.example/owner": "customer"})
	client.PrependReactor("create", "namespaces", func(clienttesting.Action) (bool, runtime.Object, error) {
		if err := client.Tracker().Create(gvr, racedNamespace, ""); err != nil {
			t.Fatal(err)
		}
		return true, nil, apierrors.NewAlreadyExists(schema.GroupResource{Resource: "namespaces"}, PlatformTelemetryNamespace)
	})
	deployer := &Deployer{client: client, namespace: "kuadrant-system"}
	owner := &metav1.OwnerReference{APIVersion: "kuadrant.io/v1alpha1", Kind: "KuadrantControlPlane", Name: "default", UID: types.UID("kcp-uid")}

	if deployer.ensurePlatformTelemetryNamespace(context.Background(), owner) {
		t.Fatal("ensurePlatformTelemetryNamespace() adopted a concurrently created namespace")
	}
	namespace, err := client.Resource(gvr).Get(context.Background(), PlatformTelemetryNamespace, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(namespace.GetOwnerReferences()) != 0 || namespace.GetLabels()["customer.example/owner"] != "customer" {
		t.Errorf("racing namespace was modified: owners=%v labels=%v", namespace.GetOwnerReferences(), namespace.GetLabels())
	}
}

func TestSyncedGatewayChartRendersTelemetryResources(t *testing.T) {
	components := withPlatformTelemetry(allComponents())
	var component Component
	for _, candidate := range components {
		if candidate.Name == "mcp-gateway" {
			component = candidate
			break
		}
	}
	if component.Name == "" {
		t.Fatal("mcp-gateway component not found")
	}
	component.ChartPath = findComponentChartForDeployer(t, "mcp-gateway")
	deployer := &Deployer{namespace: "kuadrant-system"}
	rendered, err := deployer.renderComponent(component)
	if err != nil {
		t.Fatalf("renderComponent() error = %v", err)
	}
	coreResources, telemetryResources := splitPlatformTelemetryResources(rendered.Resources)
	if len(coreResources) == 0 {
		t.Fatal("expected core gateway resources")
	}
	gotKinds := map[string]bool{}
	for _, resource := range telemetryResources {
		gotKinds[resource.GetKind()] = true
	}
	for _, kind := range []string{"Service", "ServiceMonitor", "PrometheusRule", "Role", "RoleBinding"} {
		if !gotKinds[kind] {
			t.Errorf("synced chart did not render telemetry %s", kind)
		}
	}
	if err := setControllerRecreateStrategy(coreResources, component.DeploymentName); err != nil {
		t.Fatalf("setControllerRecreateStrategy() error = %v", err)
	}
	for _, resource := range coreResources {
		if resource.GetKind() == "Deployment" && resource.GetName() == component.DeploymentName {
			strategy, found, err := unstructured.NestedMap(resource.Object, "spec", "strategy")
			if err != nil || !found || strategy["type"] != "Recreate" || len(strategy) != 1 {
				t.Errorf("controller strategy = %v, found=%v, err=%v", strategy, found, err)
			}
			return
		}
	}
	t.Fatal("controller Deployment missing from synced chart")
}

func TestDiscovery404CleansOnlyControlPlaneOwnedTelemetry(t *testing.T) {
	owner := &metav1.OwnerReference{APIVersion: "kuadrant.io/v1alpha1", Kind: "KuadrantControlPlane", Name: "default", UID: types.UID("kcp-uid")}
	ownedRole := telemetryTestResource("Role", "kuadrant-system", "mcp-gateway-prometheus-k8s")
	ownedRole.SetAPIVersion("rbac.authorization.k8s.io/v1")
	ownedRole.SetOwnerReferences([]metav1.OwnerReference{*owner})
	ownedMonitor := telemetryTestResource("ServiceMonitor", PlatformTelemetryNamespace, "mcp-gateway-controller")
	ownedMonitor.SetAPIVersion("monitoring.coreos.com/v1")
	ownedMonitor.SetOwnerReferences([]metav1.OwnerReference{*owner})
	foreignPolicy := telemetryTestResource("NetworkPolicy", "kuadrant-system", "mcp-gateway-platform-metrics")
	foreignPolicy.SetAPIVersion("networking.k8s.io/v1")
	foreignPolicy.SetOwnerReferences([]metav1.OwnerReference{{APIVersion: owner.APIVersion, Kind: owner.Kind, Name: owner.Name, UID: "another-kcp"}})
	namespace := namespaceWithMarker("kcp-uid")
	namespace.SetAPIVersion("v1")
	namespace.SetKind("Namespace")
	namespace.SetName(PlatformTelemetryNamespace)
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), ownedRole, ownedMonitor, foreignPolicy, namespace)
	deployer := &Deployer{
		client:     client,
		discovery:  &discoveryStub{resources: map[string]*metav1.APIResourceList{"config.openshift.io/v1": {}}},
		namespace:  "kuadrant-system",
		components: withPlatformTelemetry(allComponents()),
	}

	if err := deployer.RefreshPlatformMonitoring(context.Background(), owner); err != nil {
		t.Fatalf("RefreshPlatformMonitoring() error = %v", err)
	}
	if _, err := client.Resource(schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "roles"}).
		Namespace("kuadrant-system").Get(context.Background(), ownedRole.GetName(), metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Errorf("owned Role remains after API removal, error=%v", err)
	}
	if _, err := client.Resource(schema.GroupVersionResource{Group: "monitoring.coreos.com", Version: "v1", Resource: "servicemonitors"}).
		Namespace(PlatformTelemetryNamespace).Get(context.Background(), ownedMonitor.GetName(), metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Errorf("owned ServiceMonitor remains after API removal, error=%v", err)
	}
	if _, err := client.Resource(schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "networkpolicies"}).
		Namespace("kuadrant-system").Get(context.Background(), foreignPolicy.GetName(), metav1.GetOptions{}); err != nil {
		t.Errorf("foreign NetworkPolicy was deleted: %v", err)
	}
	if _, err := client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}).
		Get(context.Background(), PlatformTelemetryNamespace, metav1.GetOptions{}); err != nil {
		t.Errorf("telemetry namespace was unexpectedly deleted: %v", err)
	}
}

// findComponentChartForDeployer returns the synced chart for a component. The
// test is skipped until the synced chart carries the platform telemetry
// template, which arrives with a component chart sync.
func findComponentChartForDeployer(t *testing.T, name string) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		chart := filepath.Join(dir, "component-charts", name)
		if _, err := os.Stat(chart); err == nil {
			if _, err := os.Stat(filepath.Join(chart, "templates", "platform-monitoring.yaml")); err != nil {
				t.Skipf("synced %s chart has no platform telemetry template yet", name)
			}
			return chart
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("component chart %s not found", name)
		}
		dir = parent
	}
}

func TestRefreshPlatformMonitoringOptOut(t *testing.T) {
	d := &Deployer{
		discovery: &discoveryStub{resources: map[string]*metav1.APIResourceList{
			"config.openshift.io/v1":   {},
			"monitoring.coreos.com/v1": {},
		}},
		telemetryDisabled: true,
		components:        withPlatformTelemetry(allComponents()),
	}
	if err := d.RefreshPlatformMonitoring(context.Background(), nil); err != nil {
		t.Fatalf("RefreshPlatformMonitoring() error = %v", err)
	}
	for _, c := range d.components {
		if c.PlatformMonitoringNamespace != "" {
			t.Errorf("%s: telemetry still enabled after opt-out", c.Name)
		}
	}
	if cond := d.TelemetryCondition(); cond.Status != metav1.ConditionFalse || cond.Reason != "Disabled" {
		t.Errorf("condition = %s/%s, want False/Disabled", cond.Status, cond.Reason)
	}
}
