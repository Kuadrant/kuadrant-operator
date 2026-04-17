//go:build unit

package extension

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-logr/logr"

	extpb "github.com/kuadrant/kuadrant-operator/pkg/extension/grpc/v1"
)

func successDialer(_ context.Context, _ string) error {
	return nil
}

func failDialer(_ context.Context, target string) error {
	return fmt.Errorf("connection refused to %s", target)
}

func newTestExtensionService() *extensionService {
	return &extensionService{
		registeredData: NewRegisteredDataStore(),
		upstreamDialer: successDialer,
		logger:         logr.Discard(),
	}
}

func testPolicy(kind, namespace, name string, targetRefs ...*extpb.TargetRef) *extpb.Policy {
	return &extpb.Policy{
		Metadata: &extpb.Metadata{
			Kind:      kind,
			Namespace: namespace,
			Name:      name,
		},
		TargetRefs: targetRefs,
	}
}

func testTargetRef(group, kind, name, namespace string) *extpb.TargetRef {
	return &extpb.TargetRef{
		Group:     group,
		Kind:      kind,
		Name:      name,
		Namespace: namespace,
	}
}

func validRequest() *extpb.RegisterActionMethodRequest {
	return &extpb.RegisterActionMethodRequest{
		Policy: testPolicy("DemoPolicy", "default", "demo",
			testTargetRef("gateway.networking.k8s.io", "HTTPRoute", "my-route", "default")),
		Url: "grpc://svc:8081",
	}
}

func TestRegisterActionMethod_NilRequest(t *testing.T) {
	svc := newTestExtensionService()
	_, err := svc.RegisterActionMethod(context.Background(), nil)
	if err == nil {
		t.Fatal("Expected error for nil request")
	}
}

func TestRegisterActionMethod_NilPolicy(t *testing.T) {
	svc := newTestExtensionService()
	_, err := svc.RegisterActionMethod(context.Background(), &extpb.RegisterActionMethodRequest{})
	if err == nil {
		t.Fatal("Expected error for nil policy")
	}
}

func TestRegisterActionMethod_NilMetadata(t *testing.T) {
	svc := newTestExtensionService()
	_, err := svc.RegisterActionMethod(context.Background(), &extpb.RegisterActionMethodRequest{
		Policy: &extpb.Policy{},
	})
	if err == nil {
		t.Fatal("Expected error for nil metadata")
	}
}

func TestRegisterActionMethod_MissingPolicyFields(t *testing.T) {
	svc := newTestExtensionService()
	_, err := svc.RegisterActionMethod(context.Background(), &extpb.RegisterActionMethodRequest{
		Policy: testPolicy("", "ns", "name"),
		Url:    "grpc://svc:8081",
	})
	if err == nil {
		t.Fatal("Expected error for missing policy kind")
	}
}

func TestRegisterActionMethod_MissingURL(t *testing.T) {
	svc := newTestExtensionService()
	_, err := svc.RegisterActionMethod(context.Background(), &extpb.RegisterActionMethodRequest{
		Policy: testPolicy("DemoPolicy", "default", "demo",
			testTargetRef("gateway.networking.k8s.io", "HTTPRoute", "my-route", "default")),
	})
	if err == nil {
		t.Fatal("Expected error for missing URL")
	}
}

func TestRegisterActionMethod_InvalidScheme(t *testing.T) {
	svc := newTestExtensionService()
	_, err := svc.RegisterActionMethod(context.Background(), &extpb.RegisterActionMethodRequest{
		Policy: testPolicy("DemoPolicy", "default", "demo",
			testTargetRef("gateway.networking.k8s.io", "HTTPRoute", "my-route", "default")),
		Url: "http://svc:8081",
	})
	if err == nil {
		t.Fatal("Expected error for non-grpc scheme")
	}
}

func TestRegisterActionMethod_NoTargetRefs(t *testing.T) {
	svc := newTestExtensionService()
	_, err := svc.RegisterActionMethod(context.Background(), &extpb.RegisterActionMethodRequest{
		Policy: testPolicy("DemoPolicy", "default", "demo"),
		Url:    "grpc://svc:8081",
	})
	if err == nil {
		t.Fatal("Expected error for no target refs")
	}
}

func TestRegisterActionMethod_DialFailure(t *testing.T) {
	svc := newTestExtensionService()
	svc.upstreamDialer = failDialer

	_, err := svc.RegisterActionMethod(context.Background(), validRequest())
	if err == nil {
		t.Fatal("Expected error for unreachable upstream")
	}
}

func TestRegisterActionMethod_Success(t *testing.T) {
	svc := newTestExtensionService()

	_, err := svc.RegisterActionMethod(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	key := RegisteredUpstreamKey{
		Policy: ResourceID{Kind: "DemoPolicy", Namespace: "default", Name: "demo"},
		URL:    "grpc://svc:8081",
	}
	entry, exists := svc.registeredData.GetUpstream(key)
	if !exists {
		t.Fatal("Expected upstream to be stored")
	}
	if entry.ClusterName != "ext-svc-8081" {
		t.Errorf("Expected cluster name %q, got %q", "ext-svc-8081", entry.ClusterName)
	}
	if entry.TargetRef.Kind != "HTTPRoute" {
		t.Errorf("Expected target ref kind %q, got %q", "HTTPRoute", entry.TargetRef.Kind)
	}
}

func TestRegisterActionMethod_ClusterNameGeneration(t *testing.T) {
	tests := []struct {
		name            string
		url             string
		expectedCluster string
	}{
		{
			name:            "simple host and port",
			url:             "grpc://my-service:8081",
			expectedCluster: "ext-my-service-8081",
		},
		{
			name:            "FQDN with dots",
			url:             "grpc://auth.kuadrant-system.svc.cluster.local:50051",
			expectedCluster: "ext-auth-kuadrant-system-svc-cluster-local-50051",
		},
		{
			name:            "no port",
			url:             "grpc://my-service",
			expectedCluster: "ext-my-service",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestExtensionService()

			req := &extpb.RegisterActionMethodRequest{
				Policy: testPolicy("DemoPolicy", "default", "demo",
					testTargetRef("gateway.networking.k8s.io", "HTTPRoute", "my-route", "default")),
				Url: tt.url,
			}

			_, err := svc.RegisterActionMethod(context.Background(), req)
			if err != nil {
				t.Fatalf("Expected no error, got %v", err)
			}

			key := RegisteredUpstreamKey{
				Policy: ResourceID{Kind: "DemoPolicy", Namespace: "default", Name: "demo"},
				URL:    tt.url,
			}
			entry, exists := svc.registeredData.GetUpstream(key)
			if !exists {
				t.Fatal("Expected upstream to be stored")
			}
			if entry.ClusterName != tt.expectedCluster {
				t.Errorf("Expected cluster name %q, got %q", tt.expectedCluster, entry.ClusterName)
			}
		})
	}
}

func TestRegisterActionMethod_ChangeNotifier(t *testing.T) {
	svc := newTestExtensionService()

	notified := false
	svc.changeNotifier = func(reason string) error {
		notified = true
		return nil
	}

	_, err := svc.RegisterActionMethod(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if !notified {
		t.Fatal("Expected change notifier to have been called")
	}
}
