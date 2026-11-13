package controlplane

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakediscovery "k8s.io/client-go/discovery/fake"
	clienttesting "k8s.io/client-go/testing"
)

func fakeDiscovery(groupVersions ...string) *fakediscovery.FakeDiscovery {
	d := &fakediscovery.FakeDiscovery{Fake: &clienttesting.Fake{}}
	for _, gv := range groupVersions {
		d.Resources = append(d.Resources, &metav1.APIResourceList{GroupVersion: gv})
	}
	return d
}

func TestHasPlatformMonitoring(t *testing.T) {
	tests := []struct {
		name string
		gvs  []string
		want bool
	}{
		{name: "openshift with monitoring", gvs: []string{"config.openshift.io/v1", "monitoring.coreos.com/v1"}, want: true},
		{name: "kubernetes with prometheus operator", gvs: []string{"monitoring.coreos.com/v1"}, want: false},
		{name: "openshift api without monitoring", gvs: []string{"config.openshift.io/v1"}, want: false},
		{name: "plain kubernetes", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasPlatformMonitoring(fakeDiscovery(tt.gvs...)); got != tt.want {
				t.Errorf("hasPlatformMonitoring() = %v, want %v", got, tt.want)
			}
		})
	}
}

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
