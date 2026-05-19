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

func validRequest() *extpb.RegisterActionMethodRequest {
	return &extpb.RegisterActionMethodRequest{
		Policy: testPolicy("DemoPolicy", "default", "demo",
			testTargetRef("gateway.networking.k8s.io", "HTTPRoute", "my-route", "default")),
		Name:    "assess-threat",
		Url:     "grpc://svc:8081",
		Service: "example.v1.ExampleService",
		Method:  "ExampleMethod",
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
		Name: "assess-threat",
	})
	if err == nil {
		t.Fatal("Expected error for missing URL")
	}
}

func TestRegisterActionMethod_InvalidScheme(t *testing.T) {
	svc := newTestExtensionService()
	req := validRequest()
	req.Url = "http://svc:8081"

	_, err := svc.RegisterActionMethod(context.Background(), req)
	if err == nil {
		t.Fatal("Expected error for non-grpc scheme")
	}
	if !strings.Contains(err.Error(), "scheme must be") {
		t.Errorf("Expected scheme error, got: %v", err)
	}
}

func TestRegisterActionMethod_NoTargetRefs(t *testing.T) {
	svc := newTestExtensionService()
	req := validRequest()
	req.Policy.TargetRefs = nil

	_, err := svc.RegisterActionMethod(context.Background(), req)
	if err == nil {
		t.Fatal("Expected error for no target refs")
	}
	if !strings.Contains(err.Error(), "target references") {
		t.Errorf("Expected target refs error, got: %v", err)
	}
}

func TestRegisterActionMethod_NilTargetRefElement(t *testing.T) {
	svc := newTestExtensionService()
	req := validRequest()
	req.Policy.TargetRefs = []*extpb.TargetRef{nil}

	_, err := svc.RegisterActionMethod(context.Background(), req)
	if err == nil {
		t.Fatal("Expected error for nil target ref element")
	}
	if !strings.Contains(err.Error(), "first target reference in policy is nil") {
		t.Errorf("Expected nil target ref error, got: %v", err)
	}
}

func TestRegisterActionMethod_MissingService(t *testing.T) {
	svc := newTestExtensionService()
	req := validRequest()
	req.Service = ""

	_, err := svc.RegisterActionMethod(context.Background(), req)
	if err == nil {
		t.Fatal("Expected error for missing service")
	}
	if !strings.Contains(err.Error(), "service must be specified") {
		t.Errorf("Expected service error, got: %v", err)
	}
}

func TestRegisterActionMethod_MissingMethod(t *testing.T) {
	svc := newTestExtensionService()
	req := validRequest()
	req.Method = ""

	_, err := svc.RegisterActionMethod(context.Background(), req)
	if err == nil {
		t.Fatal("Expected error for missing method")
	}
	if !strings.Contains(err.Error(), "method must be specified") {
		t.Errorf("Expected method error, got: %v", err)
	}
}

func TestRegisterActionMethod_Success(t *testing.T) {
	svc := newTestExtensionService()

	_, err := svc.RegisterActionMethod(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	key := RegisteredUpstreamKey{
		Policy:  ResourceID{Kind: "DemoPolicy", Namespace: "default", Name: "demo"},
		Name:    "assess-threat",
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
				Name:    "assess-threat",
				Url:     tt.url,
				Service: "example.v1.ExampleService",
				Method:  "ExampleMethod",
			}

			_, err := svc.RegisterActionMethod(context.Background(), req)
			if err != nil {
				t.Fatalf("Expected no error, got %v", err)
			}

			key := RegisteredUpstreamKey{
				Policy:  ResourceID{Kind: "DemoPolicy", Namespace: "default", Name: "demo"},
				Name:    "assess-threat",
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

func TestRegisterActionMethod_InvalidMethod(t *testing.T) {
	svc := newTestExtensionService()

	req := &extpb.RegisterActionMethodRequest{
		Policy: testPolicy("DemoPolicy", "default", "demo",
			testTargetRef("gateway.networking.k8s.io", "HTTPRoute", "my-route", "default")),
		Name:    "assess-threat",
		Url:     "grpc://svc:8081",
		Service: "example.v1.ExampleService",
		Method:  "NonExistentMethod",
	}

	_, err := svc.RegisterActionMethod(context.Background(), req)
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
	policy1Req := &extpb.RegisterActionMethodRequest{
		Policy: testPolicy("DemoPolicy", "default", "policy1",
			testTargetRef("gateway.networking.k8s.io", "HTTPRoute", "route1", "default")),
		Name:    "assess-threat",
		Url:     "grpc://svc:8081",
		Service: "example.v1.ExampleService",
		Method:  "ExampleMethod",
	}

	policy2Req := &extpb.RegisterActionMethodRequest{
		Policy: testPolicy("DemoPolicy", "default", "policy2",
			testTargetRef("gateway.networking.k8s.io", "HTTPRoute", "route2", "default")),
		Name:    "assess-threat",
		Url:     "grpc://svc:8081",
		Service: "example.v1.ExampleService",
		Method:  "AnotherMethod",
	}

	_, err := svc.RegisterActionMethod(context.Background(), policy1Req)
	if err != nil {
		t.Fatalf("Failed to register policy1: %v", err)
	}

	_, err = svc.RegisterActionMethod(context.Background(), policy2Req)
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

func TestRegisterActionMethod_MultipleMethodsSamePolicy(t *testing.T) {
	svc := newTestExtensionService()

	method1Req := &extpb.RegisterActionMethodRequest{
		Policy: testPolicy("DemoPolicy", "default", "demo",
			testTargetRef("gateway.networking.k8s.io", "HTTPRoute", "my-route", "default")),
		Name:    "assess-threat",
		Url:     "grpc://svc:8081",
		Service: "example.v1.ExampleService",
		Method:  "ExampleMethod",
	}

	method2Req := &extpb.RegisterActionMethodRequest{
		Policy: testPolicy("DemoPolicy", "default", "demo",
			testTargetRef("gateway.networking.k8s.io", "HTTPRoute", "my-route", "default")),
		Name:    "another-action",
		Url:     "grpc://svc:8081",
		Service: "example.v1.ExampleService",
		Method:  "AnotherMethod",
	}

	_, err := svc.RegisterActionMethod(context.Background(), method1Req)
	if err != nil {
		t.Fatalf("Failed to register first method: %v", err)
	}

	_, err = svc.RegisterActionMethod(context.Background(), method2Req)
	if err != nil {
		t.Fatalf("Failed to register second method: %v", err)
	}

	// Verify both methods are stored independently
	key1 := RegisteredUpstreamKey{
		Policy:  ResourceID{Kind: "DemoPolicy", Namespace: "default", Name: "demo"},
		Name:    "assess-threat",
		URL:     "grpc://svc:8081",
		Service: "example.v1.ExampleService",
		Method:  "ExampleMethod",
	}
	entry1, exists1 := svc.registeredData.GetUpstream(key1)
	if !exists1 {
		t.Fatal("Expected first method to be stored")
	}

	key2 := RegisteredUpstreamKey{
		Policy:  ResourceID{Kind: "DemoPolicy", Namespace: "default", Name: "demo"},
		Name:    "another-action",
		URL:     "grpc://svc:8081",
		Service: "example.v1.ExampleService",
		Method:  "AnotherMethod",
	}
	entry2, exists2 := svc.registeredData.GetUpstream(key2)
	if !exists2 {
		t.Fatal("Expected second method to be stored")
	}

	// Verify both share the same cluster name
	if entry1.ClusterName != entry2.ClusterName {
		t.Errorf("Expected same cluster name, got %q and %q", entry1.ClusterName, entry2.ClusterName)
	}

	// Verify both share the same proto cache entry
	cacheKey := ProtoCacheKey{
		ClusterName: entry1.ClusterName,
		Service:     "example.v1.ExampleService",
	}
	_, exists := svc.registeredData.GetProtoDescriptor(cacheKey)
	if !exists {
		t.Fatal("Expected shared proto cache entry to exist")
	}
}

func TestRegisterActionMethod_ReregistrationIdempotent(t *testing.T) {
	svc := newTestExtensionService()

	notifyCount := 0
	svc.changeNotifier = func(reason string) error {
		notifyCount++
		return nil
	}

	req := validRequest()

	// First registration
	_, err := svc.RegisterActionMethod(context.Background(), req)
	if err != nil {
		t.Fatalf("First registration failed: %v", err)
	}

	if notifyCount != 1 {
		t.Errorf("Expected 1 notification after first registration, got %d", notifyCount)
	}

	// Re-register the same method
	_, err = svc.RegisterActionMethod(context.Background(), req)
	if err != nil {
		t.Fatalf("Re-registration failed: %v", err)
	}

	if notifyCount != 2 {
		t.Errorf("Expected 2 notifications after re-registration, got %d", notifyCount)
	}

	// Verify only one entry exists (not duplicated)
	key := RegisteredUpstreamKey{
		Policy:  ResourceID{Kind: "DemoPolicy", Namespace: "default", Name: "demo"},
		Name:    "assess-threat",
		URL:     "grpc://svc:8081",
		Service: "example.v1.ExampleService",
		Method:  "ExampleMethod",
	}
	upstreams := svc.registeredData.GetAllUpstreams()
	count := 0
	for k := range upstreams {
		if k == key {
			count++
		}
	}

	if count != 1 {
		t.Errorf("Expected exactly 1 entry in upstreams map, found %d", count)
	}
}

func TestRegisterActionMethod_PartialFailure(t *testing.T) {
	svc := newTestExtensionService()

	notifyCount := 0
	svc.changeNotifier = func(reason string) error {
		notifyCount++
		return nil
	}

	// First registration succeeds
	validReq := &extpb.RegisterActionMethodRequest{
		Policy: testPolicy("DemoPolicy", "default", "demo",
			testTargetRef("gateway.networking.k8s.io", "HTTPRoute", "my-route", "default")),
		Name:    "assess-threat",
		Url:     "grpc://svc:8081",
		Service: "example.v1.ExampleService",
		Method:  "ExampleMethod",
	}

	_, err := svc.RegisterActionMethod(context.Background(), validReq)
	if err != nil {
		t.Fatalf("First registration should succeed, got error: %v", err)
	}

	if notifyCount != 1 {
		t.Errorf("Expected 1 notification after first registration, got %d", notifyCount)
	}

	// Second registration fails (invalid method)
	invalidReq := &extpb.RegisterActionMethodRequest{
		Policy: testPolicy("DemoPolicy", "default", "demo",
			testTargetRef("gateway.networking.k8s.io", "HTTPRoute", "my-route", "default")),
		Name:    "invalid-action",
		Url:     "grpc://svc:8081",
		Service: "example.v1.ExampleService",
		Method:  "NonExistentMethod",
	}

	_, err = svc.RegisterActionMethod(context.Background(), invalidReq)
	if err == nil {
		t.Fatal("Second registration should fail for non-existent method")
	}

	// Verify notifier was NOT called for the failed registration
	if notifyCount != 1 {
		t.Errorf("Expected notifier to fire only once (successful registration only), got %d", notifyCount)
	}

	// Verify first registration is still intact
	validKey := RegisteredUpstreamKey{
		Policy:  ResourceID{Kind: "DemoPolicy", Namespace: "default", Name: "demo"},
		Name:    "assess-threat",
		URL:     "grpc://svc:8081",
		Service: "example.v1.ExampleService",
		Method:  "ExampleMethod",
	}
	_, exists := svc.registeredData.GetUpstream(validKey)
	if !exists {
		t.Fatal("First registration should still exist after second registration failed")
	}

	// Verify failed registration left no partial entry
	invalidKey := RegisteredUpstreamKey{
		Policy:  ResourceID{Kind: "DemoPolicy", Namespace: "default", Name: "demo"},
		Name:    "invalid-action",
		URL:     "grpc://svc:8081",
		Service: "example.v1.ExampleService",
		Method:  "NonExistentMethod",
	}
	_, exists = svc.registeredData.GetUpstream(invalidKey)
	if exists {
		t.Fatal("Failed registration should not leave any entry in storage")
	}
}

func TestRegisterActionMethod_MissingName(t *testing.T) {
	svc := newTestExtensionService()
	req := validRequest()
	req.Name = ""

	_, err := svc.RegisterActionMethod(context.Background(), req)
	if err == nil {
		t.Fatal("Expected error for missing name")
	}
	if !strings.Contains(err.Error(), "name must be specified") {
		t.Errorf("Expected name error, got: %v", err)
	}
}

func TestRegisterActionMethod_WhitespaceOnlyName(t *testing.T) {
	svc := newTestExtensionService()
	req := validRequest()
	req.Name = "   "

	_, err := svc.RegisterActionMethod(context.Background(), req)
	if err == nil {
		t.Fatal("Expected error for whitespace-only name")
	}
	if !strings.Contains(err.Error(), "name must be specified") {
		t.Errorf("Expected name error, got: %v", err)
	}
}

func TestRegisterActionMethod_DuplicateNameSamePolicy(t *testing.T) {
	svc := newTestExtensionService()

	// First registration succeeds
	req1 := &extpb.RegisterActionMethodRequest{
		Policy: testPolicy("DemoPolicy", "default", "demo",
			testTargetRef("gateway.networking.k8s.io", "HTTPRoute", "my-route", "default")),
		Name:    "assess-threat",
		Url:     "grpc://svc:8081",
		Service: "example.v1.ExampleService",
		Method:  "ExampleMethod",
	}

	_, err := svc.RegisterActionMethod(context.Background(), req1)
	if err != nil {
		t.Fatalf("First registration should succeed: %v", err)
	}

	// Second registration with same name but different method should fail
	req2 := &extpb.RegisterActionMethodRequest{
		Policy: testPolicy("DemoPolicy", "default", "demo",
			testTargetRef("gateway.networking.k8s.io", "HTTPRoute", "my-route", "default")),
		Name:    "assess-threat",
		Url:     "grpc://svc:8081",
		Service: "example.v1.ExampleService",
		Method:  "AnotherMethod",
	}

	_, err = svc.RegisterActionMethod(context.Background(), req2)
	if err == nil {
		t.Fatal("Expected error for duplicate name within same policy")
	}
	if st, ok := grpcstatus.FromError(err); !ok || st.Code() != codes.AlreadyExists {
		t.Errorf("Expected AlreadyExists gRPC status, got: %v", err)
	}
	if !strings.Contains(err.Error(), "already registered") {
		t.Errorf("Expected duplicate name error, got: %v", err)
	}
}

func TestRegisterActionMethod_SameNameDifferentPolicies(t *testing.T) {
	svc := newTestExtensionService()

	// Two different policies can use the same name
	req1 := &extpb.RegisterActionMethodRequest{
		Policy: testPolicy("DemoPolicy", "default", "policy1",
			testTargetRef("gateway.networking.k8s.io", "HTTPRoute", "route1", "default")),
		Name:    "assess-threat",
		Url:     "grpc://svc:8081",
		Service: "example.v1.ExampleService",
		Method:  "ExampleMethod",
	}

	req2 := &extpb.RegisterActionMethodRequest{
		Policy: testPolicy("DemoPolicy", "default", "policy2",
			testTargetRef("gateway.networking.k8s.io", "HTTPRoute", "route2", "default")),
		Name:    "assess-threat",
		Url:     "grpc://svc:8081",
		Service: "example.v1.ExampleService",
		Method:  "ExampleMethod",
	}

	_, err := svc.RegisterActionMethod(context.Background(), req1)
	if err != nil {
		t.Fatalf("First policy registration should succeed: %v", err)
	}

	_, err = svc.RegisterActionMethod(context.Background(), req2)
	if err != nil {
		t.Fatalf("Second policy with same name should succeed: %v", err)
	}
}

func TestRegisterActionMethod_MessageTemplatePassthrough(t *testing.T) {
	svc := newTestExtensionService()

	req := validRequest()
	req.MessageTemplate = `ThreatRequest { uri: request.path, method: request.method }`

	_, err := svc.RegisterActionMethod(context.Background(), req)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	key := RegisteredUpstreamKey{
		Policy:  ResourceID{Kind: "DemoPolicy", Namespace: "default", Name: "demo"},
		Name:    "assess-threat",
		URL:     "grpc://svc:8081",
		Service: "example.v1.ExampleService",
		Method:  "ExampleMethod",
	}
	entry, exists := svc.registeredData.GetUpstream(key)
	if !exists {
		t.Fatal("Expected upstream to be stored")
	}
	if entry.MessageTemplate != `ThreatRequest { uri: request.path, method: request.method }` {
		t.Errorf("Expected MessageTemplate to be stored as-is, got %q", entry.MessageTemplate)
	}
}

func TestRegisterActionMethod_EmptyMessageTemplate(t *testing.T) {
	svc := newTestExtensionService()

	req := validRequest()
	// MessageTemplate is optional, empty is allowed

	_, err := svc.RegisterActionMethod(context.Background(), req)
	if err != nil {
		t.Fatalf("Expected no error with empty MessageTemplate, got %v", err)
	}

	key := RegisteredUpstreamKey{
		Policy:  ResourceID{Kind: "DemoPolicy", Namespace: "default", Name: "demo"},
		Name:    "assess-threat",
		URL:     "grpc://svc:8081",
		Service: "example.v1.ExampleService",
		Method:  "ExampleMethod",
	}
	entry, exists := svc.registeredData.GetUpstream(key)
	if !exists {
		t.Fatal("Expected upstream to be stored")
	}
	if entry.MessageTemplate != "" {
		t.Errorf("Expected empty MessageTemplate, got %q", entry.MessageTemplate)
	}
}

// --- PipelineCommit tests ---

func registerTestActionMethod(t *testing.T, svc *extensionService, policyName, methodName string) {
	t.Helper()
	req := &extpb.RegisterActionMethodRequest{
		Policy: testPolicy("DemoPolicy", "default", policyName,
			testTargetRef("gateway.networking.k8s.io", "HTTPRoute", "my-route", "default")),
		Name:    methodName,
		Url:     "grpc://svc:8081",
		Service: "example.v1.ExampleService",
		Method:  "ExampleMethod",
	}
	_, err := svc.RegisterActionMethod(context.Background(), req)
	if err != nil {
		t.Fatalf("Failed to register action method %q: %v", methodName, err)
	}
}

func testPipelinePolicy() *extpb.Policy {
	return testPolicy("DemoPolicy", "default", "demo",
		testTargetRef("gateway.networking.k8s.io", "HTTPRoute", "my-route", "default"))
}

func TestPipelineCommit_NilRequest(t *testing.T) {
	svc := newTestExtensionService()
	_, err := svc.PipelineCommit(context.Background(), nil)
	if err == nil {
		t.Fatal("Expected error for nil request")
	}
}

func TestPipelineCommit_NilPolicy(t *testing.T) {
	svc := newTestExtensionService()
	_, err := svc.PipelineCommit(context.Background(), &extpb.PipelineCommitRequest{})
	if err == nil {
		t.Fatal("Expected error for nil policy")
	}
}

func TestPipelineCommit_EmptyBothPhases(t *testing.T) {
	svc := newTestExtensionService()
	_, err := svc.PipelineCommit(context.Background(), &extpb.PipelineCommitRequest{
		Policy: testPipelinePolicy(),
	})
	if err != nil {
		t.Fatalf("Expected no error for empty commit, got %v", err)
	}

	policyID := ResourceID{Kind: "DemoPolicy", Namespace: "default", Name: "demo"}
	if actions := svc.registeredData.GetPipelineActions(policyID, PipelinePhaseRequest); len(actions) != 0 {
		t.Errorf("Expected 0 request actions, got %d", len(actions))
	}
	if actions := svc.registeredData.GetPipelineActions(policyID, PipelinePhaseResponse); len(actions) != 0 {
		t.Errorf("Expected 0 response actions, got %d", len(actions))
	}
}

func TestPipelineCommit_BothPhases(t *testing.T) {
	svc := newTestExtensionService()
	registerTestActionMethod(t, svc, "demo", "assess-threat")

	_, err := svc.PipelineCommit(context.Background(), &extpb.PipelineCommitRequest{
		Policy: testPipelinePolicy(),
		Actions: []*extpb.ActionEntry{
			{ActionType: extpb.ActionType_ACTION_TYPE_GRPC_METHOD, Phase: "request", Method: "assess-threat", Predicate: "true", Var: "threatResponse"},
			{ActionType: extpb.ActionType_ACTION_TYPE_DENY, Phase: "request", DenyWith: "403"},
			{ActionType: extpb.ActionType_ACTION_TYPE_ADD_HEADERS, Phase: "response", HeadersToAdd: `{"x-checked": "true"}`, Predicate: "true"},
			{ActionType: extpb.ActionType_ACTION_TYPE_FAILURE, Phase: "response", FailureCode: "500", FailureMessage: "internal error"},
		},
	})
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	policyID := ResourceID{Kind: "DemoPolicy", Namespace: "default", Name: "demo"}
	reqActions := svc.registeredData.GetPipelineActions(policyID, PipelinePhaseRequest)
	if len(reqActions) != 2 {
		t.Fatalf("Expected 2 request actions, got %d", len(reqActions))
	}
	if reqActions[0].ActionType != extpb.ActionType_ACTION_TYPE_GRPC_METHOD {
		t.Errorf("Expected first request action GRPC_METHOD, got %s", reqActions[0].ActionType)
	}
	if reqActions[0].Method != "assess-threat" {
		t.Errorf("Expected method 'assess-threat', got %q", reqActions[0].Method)
	}
	if reqActions[0].Var != "threatResponse" {
		t.Errorf("Expected var 'threatResponse', got %q", reqActions[0].Var)
	}
	if reqActions[1].ActionType != extpb.ActionType_ACTION_TYPE_DENY {
		t.Errorf("Expected second request action DENY, got %s", reqActions[1].ActionType)
	}
	if reqActions[1].DenyWith != "403" {
		t.Errorf("Expected DenyWith '403', got %q", reqActions[1].DenyWith)
	}

	respActions := svc.registeredData.GetPipelineActions(policyID, PipelinePhaseResponse)
	if len(respActions) != 2 {
		t.Fatalf("Expected 2 response actions, got %d", len(respActions))
	}
	if respActions[0].HeadersToAdd != `{"x-checked": "true"}` {
		t.Errorf("Expected headers_to_add, got %q", respActions[0].HeadersToAdd)
	}
	if respActions[1].FailureCode != "500" {
		t.Errorf("Expected failure code '500', got %q", respActions[1].FailureCode)
	}
	if respActions[1].FailureMessage != "internal error" {
		t.Errorf("Expected failure message 'internal error', got %q", respActions[1].FailureMessage)
	}
}

func TestPipelineCommit_InvalidPhase_RejectsAll(t *testing.T) {
	svc := newTestExtensionService()

	_, err := svc.PipelineCommit(context.Background(), &extpb.PipelineCommitRequest{
		Policy: testPipelinePolicy(),
		Actions: []*extpb.ActionEntry{
			{ActionType: extpb.ActionType_ACTION_TYPE_DENY, Phase: "request", DenyWith: "403"},
			{ActionType: extpb.ActionType_ACTION_TYPE_DENY, Phase: "invalid", DenyWith: "403"},
		},
	})
	if err == nil {
		t.Fatal("Expected error for invalid response action")
	}

	policyID := ResourceID{Kind: "DemoPolicy", Namespace: "default", Name: "demo"}
	if actions := svc.registeredData.GetPipelineActions(policyID, PipelinePhaseRequest); len(actions) != 0 {
		t.Errorf("Expected no request actions stored after response validation failure, got %d", len(actions))
	}
}

func TestPipelineCommit_NilActionEntry(t *testing.T) {
	svc := newTestExtensionService()
	_, err := svc.PipelineCommit(context.Background(), &extpb.PipelineCommitRequest{
		Policy: testPipelinePolicy(),
		Actions: []*extpb.ActionEntry{
			nil,
		},
	})
	if err == nil {
		t.Fatal("Expected error for nil action entry")
	}
	if !strings.Contains(err.Error(), "cannot be nil") {
		t.Errorf("Expected nil entry error, got: %v", err)
	}
}

func TestPipelineCommit_InvalidActionType(t *testing.T) {
	svc := newTestExtensionService()
	_, err := svc.PipelineCommit(context.Background(), &extpb.PipelineCommitRequest{
		Policy: testPipelinePolicy(),
		Actions: []*extpb.ActionEntry{
			{ActionType: extpb.ActionType_ACTION_TYPE_UNSPECIFIED, Phase: "request"},
		},
	})
	if err == nil {
		t.Fatal("Expected error for unspecified action type")
	}
	if !strings.Contains(err.Error(), "action_type must be specified") {
		t.Errorf("Expected action_type error, got: %v", err)
	}
}

func TestPipelineCommit_InvalidPredicate(t *testing.T) {
	svc := newTestExtensionService()
	_, err := svc.PipelineCommit(context.Background(), &extpb.PipelineCommitRequest{
		Policy: testPipelinePolicy(),
		Actions: []*extpb.ActionEntry{
			{ActionType: extpb.ActionType_ACTION_TYPE_DENY, Phase: "request", DenyWith: "403", Predicate: "!!!invalid cel"},
		},
	})
	if err == nil {
		t.Fatal("Expected error for invalid CEL predicate")
	}
	if !strings.Contains(err.Error(), "predicate") {
		t.Errorf("Expected predicate error, got: %v", err)
	}
}

func TestPipelineCommit_GRPCMethod_UnregisteredMethod(t *testing.T) {
	svc := newTestExtensionService()
	_, err := svc.PipelineCommit(context.Background(), &extpb.PipelineCommitRequest{
		Policy: testPipelinePolicy(),
		Actions: []*extpb.ActionEntry{
			{ActionType: extpb.ActionType_ACTION_TYPE_GRPC_METHOD, Phase: "request", Method: "nonexistent"},
		},
	})
	if err == nil {
		t.Fatal("Expected error for unregistered method")
	}
	if !strings.Contains(err.Error(), "not a registered action method") {
		t.Errorf("Expected registered method error, got: %v", err)
	}
}

func TestPipelineCommit_GRPCMethod_MissingMethod(t *testing.T) {
	svc := newTestExtensionService()
	_, err := svc.PipelineCommit(context.Background(), &extpb.PipelineCommitRequest{
		Policy: testPipelinePolicy(),
		Actions: []*extpb.ActionEntry{
			{ActionType: extpb.ActionType_ACTION_TYPE_GRPC_METHOD, Phase: "request"},
		},
	})
	if err == nil {
		t.Fatal("Expected error for missing method")
	}
	if !strings.Contains(err.Error(), "method must be specified") {
		t.Errorf("Expected method error, got: %v", err)
	}
}

func TestPipelineCommit_GRPCMethod_InvalidVarName(t *testing.T) {
	svc := newTestExtensionService()
	registerTestActionMethod(t, svc, "demo", "assess-threat")
	_, err := svc.PipelineCommit(context.Background(), &extpb.PipelineCommitRequest{
		Policy: testPipelinePolicy(),
		Actions: []*extpb.ActionEntry{
			{ActionType: extpb.ActionType_ACTION_TYPE_GRPC_METHOD, Phase: "request", Method: "assess-threat", Var: "invalid var!"},
		},
	})
	if err == nil {
		t.Fatal("Expected error for invalid var name")
	}
	if !strings.Contains(err.Error(), "var") {
		t.Errorf("Expected var name error, got: %v", err)
	}
}

func TestPipelineCommit_Deny_InvalidStatusCode(t *testing.T) {
	svc := newTestExtensionService()
	tests := []struct {
		name     string
		denyWith string
	}{
		{"empty", ""},
		{"not a number", "abc"},
		{"too low", "99"},
		{"too high", "600"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.PipelineCommit(context.Background(), &extpb.PipelineCommitRequest{
				Policy: testPipelinePolicy(),
				Actions: []*extpb.ActionEntry{
					{ActionType: extpb.ActionType_ACTION_TYPE_DENY, Phase: "request", DenyWith: tt.denyWith},
				},
			})
			if err == nil {
				t.Fatalf("Expected error for DenyWith=%q", tt.denyWith)
			}
		})
	}
}

func TestPipelineCommit_Failure_InvalidStatusCode(t *testing.T) {
	svc := newTestExtensionService()
	_, err := svc.PipelineCommit(context.Background(), &extpb.PipelineCommitRequest{
		Policy: testPipelinePolicy(),
		Actions: []*extpb.ActionEntry{
			{ActionType: extpb.ActionType_ACTION_TYPE_FAILURE, Phase: "request", FailureCode: "999", FailureMessage: "error"},
		},
	})
	if err == nil {
		t.Fatal("Expected error for invalid failure code")
	}
	if !strings.Contains(err.Error(), "failure_code") {
		t.Errorf("Expected failure_code error, got: %v", err)
	}
}

func TestPipelineCommit_AddHeaders_MissingHeadersToAdd(t *testing.T) {
	svc := newTestExtensionService()
	_, err := svc.PipelineCommit(context.Background(), &extpb.PipelineCommitRequest{
		Policy: testPipelinePolicy(),
		Actions: []*extpb.ActionEntry{
			{ActionType: extpb.ActionType_ACTION_TYPE_ADD_HEADERS, Phase: "response"},
		},
	})
	if err == nil {
		t.Fatal("Expected error for missing headers_to_add")
	}
	if !strings.Contains(err.Error(), "headers_to_add must be specified") {
		t.Errorf("Expected headers_to_add error, got: %v", err)
	}
}

func TestPipelineCommit_AddHeaders_InvalidCEL(t *testing.T) {
	svc := newTestExtensionService()
	_, err := svc.PipelineCommit(context.Background(), &extpb.PipelineCommitRequest{
		Policy: testPipelinePolicy(),
		Actions: []*extpb.ActionEntry{
			{ActionType: extpb.ActionType_ACTION_TYPE_ADD_HEADERS, Phase: "response", HeadersToAdd: "!!!invalid cel"},
		},
	})
	if err == nil {
		t.Fatal("Expected error for invalid CEL in headers_to_add")
	}
	if !strings.Contains(err.Error(), "headers_to_add") {
		t.Errorf("Expected headers_to_add error, got: %v", err)
	}
}

func testFDSWithMessages() *descriptorpb.FileDescriptorSet {
	return &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{
			{
				Name:    proto.String("test.proto"),
				Package: proto.String("example.v1"),
				Service: []*descriptorpb.ServiceDescriptorProto{
					{
						Name: proto.String("ExampleService"),
						Method: []*descriptorpb.MethodDescriptorProto{
							{
								Name:       proto.String("ExampleMethod"),
								InputType:  proto.String(".example.v1.ExampleRequest"),
								OutputType: proto.String(".example.v1.ExampleResponse"),
							},
						},
					},
				},
				MessageType: []*descriptorpb.DescriptorProto{
					{
						Name: proto.String("ExampleRequest"),
						Field: []*descriptorpb.FieldDescriptorProto{
							{Name: proto.String("query"), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()},
						},
					},
					{
						Name: proto.String("ExampleResponse"),
						Field: []*descriptorpb.FieldDescriptorProto{
							{Name: proto.String("threat_level"), Type: descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum()},
							{Name: proto.String("category"), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()},
						},
					},
				},
			},
		},
	}
}

func registerTestActionMethodWithFDS(t *testing.T, svc *extensionService, policyName, methodName string) {
	t.Helper()
	fds := testFDSWithMessages()
	svc.reflectionFetcher = func(_ context.Context, _, serviceName, method string) (*descriptorpb.FileDescriptorSet, error) {
		if !validateMethodExists(fds, serviceName, method) {
			return nil, fmt.Errorf("method %q not found in service %q", method, serviceName)
		}
		return fds, nil
	}
	req := &extpb.RegisterActionMethodRequest{
		Policy: testPolicy("DemoPolicy", "default", policyName,
			testTargetRef("gateway.networking.k8s.io", "HTTPRoute", "my-route", "default")),
		Name:    methodName,
		Url:     "grpc://svc:8081",
		Service: "example.v1.ExampleService",
		Method:  "ExampleMethod",
	}
	_, err := svc.RegisterActionMethod(context.Background(), req)
	if err != nil {
		t.Fatalf("Failed to register action method %q: %v", methodName, err)
	}
}

func TestPipelineCommit_CrossAction_ValidVarFieldAccess(t *testing.T) {
	svc := newTestExtensionService()
	registerTestActionMethodWithFDS(t, svc, "demo", "assess-threat")

	_, err := svc.PipelineCommit(context.Background(), &extpb.PipelineCommitRequest{
		Policy: testPipelinePolicy(),
		Actions: []*extpb.ActionEntry{
			{ActionType: extpb.ActionType_ACTION_TYPE_GRPC_METHOD, Phase: "request", Method: "assess-threat", Var: "threatResponse"},
			{ActionType: extpb.ActionType_ACTION_TYPE_DENY, Phase: "response", DenyWith: "403", Predicate: "threatResponse.threat_level >= 5"},
		},
	})
	if err != nil {
		t.Fatalf("Expected no error for valid field access, got: %v", err)
	}
}

func TestPipelineCommit_CrossAction_InvalidVarFieldAccess(t *testing.T) {
	svc := newTestExtensionService()
	registerTestActionMethodWithFDS(t, svc, "demo", "assess-threat")

	_, err := svc.PipelineCommit(context.Background(), &extpb.PipelineCommitRequest{
		Policy: testPipelinePolicy(),
		Actions: []*extpb.ActionEntry{
			{ActionType: extpb.ActionType_ACTION_TYPE_GRPC_METHOD, Phase: "request", Method: "assess-threat", Var: "threatResponse"},
			{ActionType: extpb.ActionType_ACTION_TYPE_DENY, Phase: "response", DenyWith: "403", Predicate: "threatResponse.nonexistent_field >= 5"},
		},
	})
	if err == nil {
		t.Fatal("Expected error for invalid field access on proto response")
	}
	if !strings.Contains(err.Error(), "nonexistent_field") {
		t.Errorf("Expected field name in error, got: %v", err)
	}
}

func TestPipelineCommit_AtomicReplacement(t *testing.T) {
	svc := newTestExtensionService()
	registerTestActionMethod(t, svc, "demo", "assess-threat")

	// First commit
	_, err := svc.PipelineCommit(context.Background(), &extpb.PipelineCommitRequest{
		Policy: testPipelinePolicy(),
		Actions: []*extpb.ActionEntry{
			{ActionType: extpb.ActionType_ACTION_TYPE_DENY, Phase: "request", DenyWith: "403"},
		},
	})
	if err != nil {
		t.Fatalf("First commit failed: %v", err)
	}

	// Second commit replaces, not appends
	_, err = svc.PipelineCommit(context.Background(), &extpb.PipelineCommitRequest{
		Policy: testPipelinePolicy(),
		Actions: []*extpb.ActionEntry{
			{ActionType: extpb.ActionType_ACTION_TYPE_DENY, Phase: "request", DenyWith: "401"},
		},
	})
	if err != nil {
		t.Fatalf("Second commit failed: %v", err)
	}

	policyID := ResourceID{Kind: "DemoPolicy", Namespace: "default", Name: "demo"}
	actions := svc.registeredData.GetPipelineActions(policyID, PipelinePhaseRequest)
	if len(actions) != 1 {
		t.Fatalf("Expected 1 action after replacement, got %d", len(actions))
	}
	if actions[0].DenyWith != "401" {
		t.Errorf("Expected replaced action with DenyWith '401', got %q", actions[0].DenyWith)
	}
}

func TestPipelineCommit_ChangeNotifier(t *testing.T) {
	svc := newTestExtensionService()

	notified := false
	svc.changeNotifier = func(reason string) error {
		notified = true
		return nil
	}

	_, err := svc.PipelineCommit(context.Background(), &extpb.PipelineCommitRequest{
		Policy: testPipelinePolicy(),
		Actions: []*extpb.ActionEntry{
			{ActionType: extpb.ActionType_ACTION_TYPE_DENY, Phase: "request", DenyWith: "403"},
		},
	})
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if !notified {
		t.Fatal("Expected change notifier to have been called")
	}
}
