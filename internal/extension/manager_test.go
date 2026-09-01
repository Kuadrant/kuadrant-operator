//go:build unit

package extension

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

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
		sessionStore:      NewSessionStore(logr.Discard()),
		authenticator:     newTokenAuthenticator(fake.NewSimpleClientset(), logr.Discard()),
		reflectionFetcher: successReflectionFetcher,
		logger:            logr.Discard(),
	}
}

// fakeAuthClient returns a clientset whose TokenReview authenticates as the
// given identity/audiences and whose SubjectAccessReview allows registration
// only of the given policy kind.
func fakeAuthClient(identity string, audiences []string, allowedKind string) *fake.Clientset {
	client := fake.NewSimpleClientset()
	client.PrependReactor("create", "tokenreviews", func(action clienttesting.Action) (bool, runtime.Object, error) {
		review := action.(clienttesting.CreateAction).GetObject().(*authenticationv1.TokenReview)
		review.Status = authenticationv1.TokenReviewStatus{
			Authenticated: true,
			User:          authenticationv1.UserInfo{Username: identity},
			Audiences:     audiences,
		}
		return true, review, nil
	})
	client.PrependReactor("create", "subjectaccessreviews", func(action clienttesting.Action) (bool, runtime.Object, error) {
		review := action.(clienttesting.CreateAction).GetObject().(*authorizationv1.SubjectAccessReview)
		review.Status.Allowed = review.Spec.ResourceAttributes.Name == allowedKind
		return true, review, nil
	})
	return client
}

// fakeGroupAuthClient returns a clientset whose TokenReview authenticates as
// the given identity carrying the given groups, and whose SubjectAccessReview
// allows registration only when the request carries the given group
func fakeGroupAuthClient(identity string, audiences, groups []string, allowedGroup string) *fake.Clientset {
	client := fake.NewSimpleClientset()
	client.PrependReactor("create", "tokenreviews", func(action clienttesting.Action) (bool, runtime.Object, error) {
		review := action.(clienttesting.CreateAction).GetObject().(*authenticationv1.TokenReview)
		review.Status = authenticationv1.TokenReviewStatus{
			Authenticated: true,
			User:          authenticationv1.UserInfo{Username: identity, Groups: groups},
			Audiences:     audiences,
		}
		return true, review, nil
	})
	client.PrependReactor("create", "subjectaccessreviews", func(action clienttesting.Action) (bool, runtime.Object, error) {
		review := action.(clienttesting.CreateAction).GetObject().(*authorizationv1.SubjectAccessReview)
		for _, group := range review.Spec.Groups {
			if group == allowedGroup {
				review.Status.Allowed = true
				break
			}
		}
		return true, review, nil
	})
	return client
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

func TestHandshake_MissingPolicyKind(t *testing.T) {
	svc := newTestExtensionService()

	resp, err := svc.Handshake(context.Background(), &extpb.HandshakeRequest{})
	if err != nil {
		t.Fatalf("expected no gRPC error, got: %v", err)
	}
	if resp.Accepted {
		t.Fatal("expected handshake to be rejected for missing policy_kind")
	}
	if resp.Reason != "policy_kind is required" {
		t.Fatalf("expected reason %q, got %q", "policy_kind is required", resp.Reason)
	}
}

func TestHandshake_Builtin_Success(t *testing.T) {
	svc := newTestExtensionService()
	cred := validCredential()
	svc.sessionStore.SetCredential("test-ext", cred)

	resp, err := svc.Handshake(context.Background(), &extpb.HandshakeRequest{
		Version:    "1.0.0",
		Token:      cred,
		PolicyKind: "TestPolicy",
	})
	if err != nil {
		t.Fatalf("expected no gRPC error, got: %v", err)
	}
	if !resp.Accepted {
		t.Fatalf("expected handshake to be accepted, got reason: %s", resp.Reason)
	}
	if resp.SessionToken == "" {
		t.Fatal("expected non-empty session token")
	}
}

func TestHandshake_Standalone_Success(t *testing.T) {
	svc := newTestExtensionService()
	svc.authenticator = newTokenAuthenticator(
		fakeAuthClient("system:serviceaccount:ns:standalone", []string{extensionTokenAudience}, "StandalonePolicy"),
		logr.Discard(),
	)

	resp, err := svc.Handshake(context.Background(), &extpb.HandshakeRequest{
		Token:      []byte("standalone-service-account-token"),
		PolicyKind: "StandalonePolicy",
	})
	if err != nil {
		t.Fatalf("expected no gRPC error, got: %v", err)
	}
	if !resp.Accepted {
		t.Fatalf("expected standalone handshake to be accepted, got reason: %s", resp.Reason)
	}
	if resp.SessionToken == "" {
		t.Fatal("expected non-empty session token")
	}
}

func TestHandshake_Standalone_Unauthorized(t *testing.T) {
	svc := newTestExtensionService()
	svc.authenticator = newTokenAuthenticator(
		fakeAuthClient("system:serviceaccount:ns:standalone", []string{extensionTokenAudience}, "OtherPolicy"),
		logr.Discard(),
	)

	resp, err := svc.Handshake(context.Background(), &extpb.HandshakeRequest{
		Token:      []byte("standalone-service-account-token"),
		PolicyKind: "StandalonePolicy",
	})
	if err != nil {
		t.Fatalf("expected no gRPC error, got: %v", err)
	}
	if resp.Accepted {
		t.Fatal("expected standalone handshake to be rejected when not authorized")
	}
	if resp.Reason != "handshake failed" {
		t.Fatalf("expected generic reason %q, got %q", "handshake failed", resp.Reason)
	}
}

func TestHandshake_Standalone_AuthorizedByGroup(t *testing.T) {
	svc := newTestExtensionService()
	svc.authenticator = newTokenAuthenticator(
		fakeGroupAuthClient(
			"system:serviceaccount:ns:standalone",
			[]string{extensionTokenAudience},
			[]string{"system:authenticated", "kuadrant:extensions"},
			"kuadrant:extensions",
		),
		logr.Discard(),
	)

	resp, err := svc.Handshake(context.Background(), &extpb.HandshakeRequest{
		Token:      []byte("standalone-service-account-token"),
		PolicyKind: "StandalonePolicy",
	})
	if err != nil {
		t.Fatalf("expected no gRPC error, got: %v", err)
	}
	if !resp.Accepted {
		t.Fatalf("expected standalone handshake to be accepted via group membership, got reason: %s", resp.Reason)
	}
	if resp.SessionToken == "" {
		t.Fatal("expected non-empty session token")
	}
}

func TestHandshake_Standalone_RejectedWhenGroupMissing(t *testing.T) {
	svc := newTestExtensionService()
	svc.authenticator = newTokenAuthenticator(
		fakeGroupAuthClient(
			"system:serviceaccount:ns:standalone",
			[]string{extensionTokenAudience},
			[]string{"system:authenticated"},
			"kuadrant:extensions",
		),
		logr.Discard(),
	)

	resp, err := svc.Handshake(context.Background(), &extpb.HandshakeRequest{
		Token:      []byte("standalone-service-account-token"),
		PolicyKind: "StandalonePolicy",
	})
	if err != nil {
		t.Fatalf("expected no gRPC error, got: %v", err)
	}
	if resp.Accepted {
		t.Fatal("expected standalone handshake to be rejected when the authorizing group is absent")
	}
}

func TestHandshake_UnauthenticatedToken_GenericReason(t *testing.T) {
	svc := newTestExtensionService()

	resp, err := svc.Handshake(context.Background(), &extpb.HandshakeRequest{
		Token:      []byte("bogus-token"),
		PolicyKind: "TestPolicy",
	})
	if err != nil {
		t.Fatalf("expected no gRPC error, got: %v", err)
	}
	if resp.Accepted {
		t.Fatal("expected handshake to be rejected for an unauthenticated token")
	}
	if resp.Reason != "handshake failed" {
		t.Fatalf("expected generic reason %q, got %q", "handshake failed", resp.Reason)
	}
}

func TestHandshake_WarmupGate_RejectsStandaloneDuringWarmup(t *testing.T) {
	svc := newTestExtensionService()
	svc.authenticator = newTokenAuthenticator(
		fakeAuthClient("system:serviceaccount:ns:standalone", []string{extensionTokenAudience}, "StandalonePolicy"),
		logr.Discard(),
	)
	svc.sessionStore.SetCredential("builtin", validCredential())
	svc.sessionStore.BeginWarmup([]string{"builtin"}, time.Minute)

	resp, err := svc.Handshake(context.Background(), &extpb.HandshakeRequest{
		Token:      []byte("standalone-service-account-token"),
		PolicyKind: "StandalonePolicy",
	})
	if err != nil {
		t.Fatalf("expected no gRPC error, got: %v", err)
	}
	if resp.Accepted {
		t.Fatal("expected standalone handshake to be rejected while warmup is in progress")
	}
	if resp.Reason != "warmup in progress" {
		t.Fatalf("expected reason %q, got %q", "warmup in progress", resp.Reason)
	}
}

func TestHandshake_WarmupGate_AllowsStandaloneAfterBuiltinRegisters(t *testing.T) {
	svc := newTestExtensionService()
	svc.authenticator = newTokenAuthenticator(
		fakeAuthClient("system:serviceaccount:ns:standalone", []string{extensionTokenAudience}, "StandalonePolicy"),
		logr.Discard(),
	)
	builtinCred := validCredential()
	svc.sessionStore.SetCredential("builtin", builtinCred)
	svc.sessionStore.BeginWarmup([]string{"builtin"}, time.Minute)

	builtinResp, err := svc.Handshake(context.Background(), &extpb.HandshakeRequest{
		Token:      builtinCred,
		PolicyKind: "BuiltinPolicy",
	})
	if err != nil {
		t.Fatalf("expected no gRPC error for built-in handshake, got: %v", err)
	}
	if !builtinResp.Accepted {
		t.Fatalf("expected built-in handshake to be accepted during warmup, got reason: %s", builtinResp.Reason)
	}

	resp, err := svc.Handshake(context.Background(), &extpb.HandshakeRequest{
		Token:      []byte("standalone-service-account-token"),
		PolicyKind: "StandalonePolicy",
	})
	if err != nil {
		t.Fatalf("expected no gRPC error, got: %v", err)
	}
	if !resp.Accepted {
		t.Fatalf("expected standalone handshake to be accepted after warmup completed, got reason: %s", resp.Reason)
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
			{ActionType: extpb.ActionType_ACTION_TYPE_DENY, Phase: "request", WithStatus: 403},
			{ActionType: extpb.ActionType_ACTION_TYPE_ADD_HEADERS, Phase: "response", HeadersToAdd: `{"x-checked": "true"}`, Predicate: "true"},
			{ActionType: extpb.ActionType_ACTION_TYPE_FAIL, Phase: "response", LogMessage: "internal error"},
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
	if reqActions[1].WithStatus != 403 {
		t.Errorf("Expected WithStatus 403, got %d", reqActions[1].WithStatus)
	}

	respActions := svc.registeredData.GetPipelineActions(policyID, PipelinePhaseResponse)
	if len(respActions) != 2 {
		t.Fatalf("Expected 2 response actions, got %d", len(respActions))
	}
	if respActions[0].HeadersToAdd != `{"x-checked": "true"}` {
		t.Errorf("Expected headers_to_add, got %q", respActions[0].HeadersToAdd)
	}
	if respActions[1].LogMessage != "internal error" {
		t.Errorf("Expected log message 'internal error', got %q", respActions[1].LogMessage)
	}
}

func TestPipelineCommit_InvalidPhase_RejectsAll(t *testing.T) {
	svc := newTestExtensionService()

	_, err := svc.PipelineCommit(context.Background(), &extpb.PipelineCommitRequest{
		Policy: testPipelinePolicy(),
		Actions: []*extpb.ActionEntry{
			{ActionType: extpb.ActionType_ACTION_TYPE_DENY, Phase: "request", WithStatus: 403},
			{ActionType: extpb.ActionType_ACTION_TYPE_DENY, Phase: "invalid", WithStatus: 403},
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
			{ActionType: extpb.ActionType_ACTION_TYPE_DENY, Phase: "request", WithStatus: 403, Predicate: "!!!invalid cel"},
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
		name       string
		withStatus int32
	}{
		{"too low", 99},
		{"too high", 600},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.PipelineCommit(context.Background(), &extpb.PipelineCommitRequest{
				Policy: testPipelinePolicy(),
				Actions: []*extpb.ActionEntry{
					{ActionType: extpb.ActionType_ACTION_TYPE_DENY, Phase: "request", WithStatus: tt.withStatus},
				},
			})
			if err == nil {
				t.Fatalf("Expected error for WithStatus=%d", tt.withStatus)
			}
		})
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
			{ActionType: extpb.ActionType_ACTION_TYPE_DENY, Phase: "response", WithStatus: 403, Predicate: "threatResponse.threat_level >= 5"},
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
			{ActionType: extpb.ActionType_ACTION_TYPE_DENY, Phase: "response", WithStatus: 403, Predicate: "threatResponse.nonexistent_field >= 5"},
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
			{ActionType: extpb.ActionType_ACTION_TYPE_DENY, Phase: "request", WithStatus: 403},
		},
	})
	if err != nil {
		t.Fatalf("First commit failed: %v", err)
	}

	// Second commit replaces, not appends
	_, err = svc.PipelineCommit(context.Background(), &extpb.PipelineCommitRequest{
		Policy: testPipelinePolicy(),
		Actions: []*extpb.ActionEntry{
			{ActionType: extpb.ActionType_ACTION_TYPE_DENY, Phase: "request", WithStatus: 401},
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
	if actions[0].WithStatus != 401 {
		t.Errorf("Expected replaced action with WithStatus 401, got %d", actions[0].WithStatus)
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
			{ActionType: extpb.ActionType_ACTION_TYPE_DENY, Phase: "request", WithStatus: 403},
		},
	})
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if !notified {
		t.Fatal("Expected change notifier to have been called")
	}
}

func TestPipelineCommit_ChangeNotifierError(t *testing.T) {
	svc := newTestExtensionService()

	svc.changeNotifier = func(reason string) error {
		return fmt.Errorf("no Kuadrant resources found in cluster")
	}

	_, err := svc.PipelineCommit(context.Background(), &extpb.PipelineCommitRequest{
		Policy: testPipelinePolicy(),
		Actions: []*extpb.ActionEntry{
			{ActionType: extpb.ActionType_ACTION_TYPE_DENY, Phase: "request", WithStatus: 403},
		},
	})
	if err == nil {
		t.Fatal("Expected error when change notifier fails, got nil")
	}
	if !strings.Contains(err.Error(), "failed to trigger reconciliation") {
		t.Errorf("Expected error about failed reconciliation, got: %v", err)
	}
}

func TestWarmupTimeout(t *testing.T) {
	testCases := []struct {
		name     string
		value    string
		expected time.Duration
	}{
		{"seconds", "5s", 5 * time.Second},
		{"minutes", "1m", time.Minute},
		{"hours", "1h", time.Hour},
		{"unset uses default", "", defaultWarmupTimeout},
		{"zero uses default", "0s", defaultWarmupTimeout},
		{"negative uses default", "-5s", defaultWarmupTimeout},
		{"unparsable uses default", "notaduration", defaultWarmupTimeout},
		{"bare number uses default", "30", defaultWarmupTimeout},
		{"overflowing value uses default", "9999999999999h", defaultWarmupTimeout},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.value == "" {
				t.Setenv("EXTENSIONS_WARMUP_TIMEOUT", "")
				os.Unsetenv("EXTENSIONS_WARMUP_TIMEOUT")
			} else {
				t.Setenv("EXTENSIONS_WARMUP_TIMEOUT", tc.value)
			}

			if got := warmupTimeout(logr.Discard()); got != tc.expected {
				t.Fatalf("expected %v, got %v", tc.expected, got)
			}
		})
	}
}

func TestSessionTTL(t *testing.T) {
	testCases := []struct {
		name     string
		value    string
		expected time.Duration
	}{
		{"seconds", "30s", 30 * time.Second},
		{"minutes", "2m", 2 * time.Minute},
		{"unset uses default", "", defaultSessionTTL},
		{"zero uses default", "0s", defaultSessionTTL},
		{"negative uses default", "-5s", defaultSessionTTL},
		{"unparsable uses default", "notaduration", defaultSessionTTL},
		{"bare number uses default", "45", defaultSessionTTL},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.value == "" {
				t.Setenv("EXTENSIONS_SESSION_TTL", "")
				os.Unsetenv("EXTENSIONS_SESSION_TTL")
			} else {
				t.Setenv("EXTENSIONS_SESSION_TTL", tc.value)
			}

			if got := sessionTTL(logr.Discard()); got != tc.expected {
				t.Fatalf("expected %v, got %v", tc.expected, got)
			}
		})
	}
}

func TestReaperInterval(t *testing.T) {
	testCases := []struct {
		name     string
		ttl      time.Duration
		expected time.Duration
	}{
		{"default ttl", 45 * time.Second, 15 * time.Second},
		{"short ttl floored", 2 * time.Second, minReaperInterval},
		{"exactly at floor", 3 * time.Second, 1 * time.Second},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := reaperInterval(tc.ttl); got != tc.expected {
				t.Fatalf("expected %v, got %v", tc.expected, got)
			}
		})
	}
}

func TestManager_Reaper_RevokesStaleSessions(t *testing.T) {
	t.Setenv("EXTENSIONS_SESSION_TTL", "1s")

	store := newTestSessionStore()
	clock := &fakeClock{t: time.Unix(0, 0)}
	store.now = clock.now

	token, err := store.CreateSession("stale-extension", "StalePolicy")
	if err != nil {
		t.Fatalf("expected session creation to succeed, got: %v", err)
	}
	clock.advance(2 * time.Second)

	m := &Manager{sessionStore: store, logger: logr.Discard()}
	m.startReaper()
	defer m.stopReaper()

	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, ok := store.ValidateSession(token); !ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("expected reaper to revoke the stale session")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
