//go:build unit

package extension

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"

	extpb "github.com/kuadrant/kuadrant-operator/pkg/extension/grpc/v1"
)

func successReflectionFetcher(_ context.Context, _, serviceName, methodName string) (*descriptorpb.FileDescriptorSet, error) {
	fds := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{
			{
				Name:    proto.String("test.proto"),
				Package: proto.String("example.v1"),
				Service: []*descriptorpb.ServiceDescriptorProto{
					{
						Name: proto.String("ExampleService"),
						Method: []*descriptorpb.MethodDescriptorProto{
							{Name: proto.String("ExampleMethod")},
							{Name: proto.String("AnotherMethod")},
						},
					},
				},
			},
		},
	}

	// Validate method exists if method name is provided
	if methodName != "" && !validateMethodExists(fds, serviceName, methodName) {
		return nil, fmt.Errorf("method %q not found in service %q", methodName, serviceName)
	}

	return fds, nil
}

func newTestExtensionService() *extensionService {
	return &extensionService{
		registeredData:    NewRegisteredDataStore(),
		reflectionFetcher: successReflectionFetcher,
		logger:            logr.Discard(),
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

func validRequest() *extpb.RegisterUpstreamMethodRequest {
	return &extpb.RegisterUpstreamMethodRequest{
		Policy: testPolicy("DemoPolicy", "default", "demo",
			testTargetRef("gateway.networking.k8s.io", "HTTPRoute", "my-route", "default")),
		Url:     "grpc://svc:8081",
		Service: "example.v1.ExampleService",
		Method:  "ExampleMethod",
	}
}

func TestRegisterUpstreamMethod_NilRequest(t *testing.T) {
	svc := newTestExtensionService()
	_, err := svc.RegisterUpstreamMethod(context.Background(), nil)
	if err == nil {
		t.Fatal("Expected error for nil request")
	}
}

func TestRegisterUpstreamMethod_NilPolicy(t *testing.T) {
	svc := newTestExtensionService()
	_, err := svc.RegisterUpstreamMethod(context.Background(), &extpb.RegisterUpstreamMethodRequest{})
	if err == nil {
		t.Fatal("Expected error for nil policy")
	}
}

func TestRegisterUpstreamMethod_NilMetadata(t *testing.T) {
	svc := newTestExtensionService()
	_, err := svc.RegisterUpstreamMethod(context.Background(), &extpb.RegisterUpstreamMethodRequest{
		Policy: &extpb.Policy{},
	})
	if err == nil {
		t.Fatal("Expected error for nil metadata")
	}
}

func TestRegisterUpstreamMethod_MissingPolicyFields(t *testing.T) {
	svc := newTestExtensionService()
	_, err := svc.RegisterUpstreamMethod(context.Background(), &extpb.RegisterUpstreamMethodRequest{
		Policy: testPolicy("", "ns", "name"),
		Url:    "grpc://svc:8081",
	})
	if err == nil {
		t.Fatal("Expected error for missing policy kind")
	}
}

func TestRegisterUpstreamMethod_MissingURL(t *testing.T) {
	svc := newTestExtensionService()
	_, err := svc.RegisterUpstreamMethod(context.Background(), &extpb.RegisterUpstreamMethodRequest{
		Policy: testPolicy("DemoPolicy", "default", "demo",
			testTargetRef("gateway.networking.k8s.io", "HTTPRoute", "my-route", "default")),
	})
	if err == nil {
		t.Fatal("Expected error for missing URL")
	}
}

func TestRegisterUpstreamMethod_InvalidScheme(t *testing.T) {
	svc := newTestExtensionService()
	req := validRequest()
	req.Url = "http://svc:8081"

	_, err := svc.RegisterUpstreamMethod(context.Background(), req)
	if err == nil {
		t.Fatal("Expected error for non-grpc scheme")
	}
	if !strings.Contains(err.Error(), "scheme must be") {
		t.Errorf("Expected scheme error, got: %v", err)
	}
}

func TestRegisterUpstreamMethod_NoTargetRefs(t *testing.T) {
	svc := newTestExtensionService()
	req := validRequest()
	req.Policy.TargetRefs = nil

	_, err := svc.RegisterUpstreamMethod(context.Background(), req)
	if err == nil {
		t.Fatal("Expected error for no target refs")
	}
	if !strings.Contains(err.Error(), "target references") {
		t.Errorf("Expected target refs error, got: %v", err)
	}
}

func TestRegisterUpstreamMethod_MissingService(t *testing.T) {
	svc := newTestExtensionService()
	req := validRequest()
	req.Service = ""

	_, err := svc.RegisterUpstreamMethod(context.Background(), req)
	if err == nil {
		t.Fatal("Expected error for missing service")
	}
	if !strings.Contains(err.Error(), "service must be specified") {
		t.Errorf("Expected service error, got: %v", err)
	}
}

func TestRegisterUpstreamMethod_MissingMethod(t *testing.T) {
	svc := newTestExtensionService()
	req := validRequest()
	req.Method = ""

	_, err := svc.RegisterUpstreamMethod(context.Background(), req)
	if err == nil {
		t.Fatal("Expected error for missing method")
	}
	if !strings.Contains(err.Error(), "method must be specified") {
		t.Errorf("Expected method error, got: %v", err)
	}
}

func TestRegisterUpstreamMethod_Success(t *testing.T) {
	svc := newTestExtensionService()

	_, err := svc.RegisterUpstreamMethod(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	key := RegisteredUpstreamKey{
		Policy:  ResourceID{Kind: "DemoPolicy", Namespace: "default", Name: "demo"},
		URL:     "grpc://svc:8081",
		Service: "example.v1.ExampleService",
		Method:  "ExampleMethod",
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

func TestRegisterUpstreamMethod_ClusterNameGeneration(t *testing.T) {
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

			req := &extpb.RegisterUpstreamMethodRequest{
				Policy: testPolicy("DemoPolicy", "default", "demo",
					testTargetRef("gateway.networking.k8s.io", "HTTPRoute", "my-route", "default")),
				Url:     tt.url,
				Service: "example.v1.ExampleService",
				Method:  "ExampleMethod",
			}

			_, err := svc.RegisterUpstreamMethod(context.Background(), req)
			if err != nil {
				t.Fatalf("Expected no error, got %v", err)
			}

			key := RegisteredUpstreamKey{
				Policy:  ResourceID{Kind: "DemoPolicy", Namespace: "default", Name: "demo"},
				URL:     tt.url,
				Service: "example.v1.ExampleService",
				Method:  "ExampleMethod",
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

func TestRegisterUpstreamMethod_ChangeNotifier(t *testing.T) {
	svc := newTestExtensionService()

	notified := false
	svc.changeNotifier = func(reason string) error {
		notified = true
		return nil
	}

	_, err := svc.RegisterUpstreamMethod(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if !notified {
		t.Fatal("Expected change notifier to have been called")
	}
}

func TestRegisterUpstreamMethod_InvalidMethod(t *testing.T) {
	svc := newTestExtensionService()

	req := &extpb.RegisterUpstreamMethodRequest{
		Policy: testPolicy("DemoPolicy", "default", "demo",
			testTargetRef("gateway.networking.k8s.io", "HTTPRoute", "my-route", "default")),
		Url:     "grpc://svc:8081",
		Service: "example.v1.ExampleService",
		Method:  "NonExistentMethod",
	}

	_, err := svc.RegisterUpstreamMethod(context.Background(), req)
	if err == nil {
		t.Fatal("Expected error for non-existent method")
	}

	if grpcstatus.Code(err) != codes.FailedPrecondition {
		t.Errorf("Expected FailedPrecondition status code, got: %v", grpcstatus.Code(err))
	}
	if !strings.Contains(err.Error(), "method \"NonExistentMethod\" not found") {
		t.Errorf("Expected error message about method not found, got: %v", err)
	}
}

func TestClearPolicy_ProtoCacheCleanup(t *testing.T) {
	svc := newTestExtensionService()

	// Register the same upstream service from two different policies
	policy1Req := &extpb.RegisterUpstreamMethodRequest{
		Policy: testPolicy("DemoPolicy", "default", "policy1",
			testTargetRef("gateway.networking.k8s.io", "HTTPRoute", "route1", "default")),
		Url:     "grpc://svc:8081",
		Service: "example.v1.ExampleService",
		Method:  "ExampleMethod",
	}

	policy2Req := &extpb.RegisterUpstreamMethodRequest{
		Policy: testPolicy("DemoPolicy", "default", "policy2",
			testTargetRef("gateway.networking.k8s.io", "HTTPRoute", "route2", "default")),
		Url:     "grpc://svc:8081",
		Service: "example.v1.ExampleService",
		Method:  "AnotherMethod",
	}

	_, err := svc.RegisterUpstreamMethod(context.Background(), policy1Req)
	if err != nil {
		t.Fatalf("Failed to register policy1: %v", err)
	}

	_, err = svc.RegisterUpstreamMethod(context.Background(), policy2Req)
	if err != nil {
		t.Fatalf("Failed to register policy2: %v", err)
	}

	cacheKey := ProtoCacheKey{
		ClusterName: "ext-svc-8081",
		Service:     "example.v1.ExampleService",
	}

	// Verify cache entry exists
	_, exists := svc.registeredData.GetProtoDescriptor(cacheKey)
	if !exists {
		t.Fatal("Expected cache entry to exist after registration")
	}

	// Clear policy1
	_, err = svc.ClearPolicy(context.Background(), &extpb.ClearPolicyRequest{
		Policy: policy1Req.Policy,
	})
	if err != nil {
		t.Fatalf("Failed to clear policy1: %v", err)
	}

	// Cache entry should still exist because policy2 references it
	_, exists = svc.registeredData.GetProtoDescriptor(cacheKey)
	if !exists {
		t.Fatal("Expected cache entry to still exist after clearing policy1")
	}

	// Clear policy2
	_, err = svc.ClearPolicy(context.Background(), &extpb.ClearPolicyRequest{
		Policy: policy2Req.Policy,
	})
	if err != nil {
		t.Fatalf("Failed to clear policy2: %v", err)
	}

	// Cache entry should now be deleted
	_, exists = svc.registeredData.GetProtoDescriptor(cacheKey)
	if exists {
		t.Fatal("Expected cache entry to be deleted after clearing all referencing policies")
	}
}

func TestGetServiceDescriptors_Success(t *testing.T) {
	svc := newTestExtensionService()

	// Populate cache with test descriptors
	cacheKey1 := ProtoCacheKey{
		ClusterName: "ext-svc1-8081",
		Service:     "example.v1.Service1",
	}
	cacheKey2 := ProtoCacheKey{
		ClusterName: "ext-svc2-8082",
		Service:     "example.v1.Service2",
	}
	fds1 := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{
			{Name: proto.String("service1.proto")},
		},
	}
	fds2 := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{
			{Name: proto.String("service2.proto")},
		},
	}
	svc.registeredData.protoCache.Set(cacheKey1, fds1)
	svc.registeredData.protoCache.Set(cacheKey2, fds2)

	req := &extpb.GetServiceDescriptorsRequest{
		Services: []*extpb.ServiceRef{
			{ClusterName: "ext-svc1-8081", Service: "example.v1.Service1"},
			{ClusterName: "ext-svc2-8082", Service: "example.v1.Service2"},
		},
	}

	resp, err := svc.GetServiceDescriptors(context.Background(), req)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(resp.Descriptors) != 2 {
		t.Fatalf("Expected 2 descriptors, got %d", len(resp.Descriptors))
	}

	// Verify first descriptor
	if resp.Descriptors[0].ClusterName != "ext-svc1-8081" {
		t.Errorf("Expected cluster name %q, got %q", "ext-svc1-8081", resp.Descriptors[0].ClusterName)
	}
	if resp.Descriptors[0].Service != "example.v1.Service1" {
		t.Errorf("Expected service %q, got %q", "example.v1.Service1", resp.Descriptors[0].Service)
	}
	if len(resp.Descriptors[0].FileDescriptorSet) == 0 {
		t.Error("Expected non-empty file descriptor set")
	}

	// Verify second descriptor
	if resp.Descriptors[1].ClusterName != "ext-svc2-8082" {
		t.Errorf("Expected cluster name %q, got %q", "ext-svc2-8082", resp.Descriptors[1].ClusterName)
	}
	if resp.Descriptors[1].Service != "example.v1.Service2" {
		t.Errorf("Expected service %q, got %q", "example.v1.Service2", resp.Descriptors[1].Service)
	}
}

func TestGetServiceDescriptors_NotFound(t *testing.T) {
	svc := newTestExtensionService()

	req := &extpb.GetServiceDescriptorsRequest{
		Services: []*extpb.ServiceRef{
			{ClusterName: "ext-nonexistent-8081", Service: "example.v1.NonexistentService"},
		},
	}

	_, err := svc.GetServiceDescriptors(context.Background(), req)
	if err == nil {
		t.Fatal("Expected error for missing descriptor")
	}
}

func TestGetServiceDescriptors_NilRequest(t *testing.T) {
	svc := newTestExtensionService()

	_, err := svc.GetServiceDescriptors(context.Background(), nil)
	if err == nil {
		t.Fatal("Expected error for nil request")
	}
}

func TestGetServiceDescriptors_MissingClusterName(t *testing.T) {
	svc := newTestExtensionService()

	req := &extpb.GetServiceDescriptorsRequest{
		Services: []*extpb.ServiceRef{
			{Service: "example.v1.Service1"},
		},
	}

	_, err := svc.GetServiceDescriptors(context.Background(), req)
	if err == nil {
		t.Fatal("Expected error for missing cluster_name")
	}
}

func TestGetServiceDescriptors_MissingService(t *testing.T) {
	svc := newTestExtensionService()

	req := &extpb.GetServiceDescriptorsRequest{
		Services: []*extpb.ServiceRef{
			{ClusterName: "ext-svc1-8081"},
		},
	}

	_, err := svc.GetServiceDescriptors(context.Background(), req)
	if err == nil {
		t.Fatal("Expected error for missing service")
	}
}
