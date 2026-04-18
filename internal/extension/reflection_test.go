//go:build unit

/*
Copyright 2026 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package extension

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestReflectionClient_Creation(t *testing.T) {
	client := NewReflectionClient()

	if client == nil {
		t.Fatal("Expected non-nil client")
	}

	if client.timeout != reflectionTimeout {
		t.Errorf("Expected timeout %v, got %v", reflectionTimeout, client.timeout)
	}

	if client.timeout != 30*time.Second {
		t.Errorf("Expected 30 second timeout, got %v", client.timeout)
	}
}

func TestReflectionClient_FetchServiceDescriptors_InvalidURL(t *testing.T) {
	client := NewReflectionClient().WithTimeout(100 * time.Millisecond)
	ctx := context.Background()

	// Test with various invalid URLs
	testCases := []struct {
		name string
		url  string
	}{
		{"empty URL", ""},
		{"invalid scheme", "http://test:8080"},
		{"non-existent host", "grpc://nonexistent-host-12345:8080"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := client.FetchServiceDescriptors(ctx, tc.url, "test.Service", "")
			if err == nil {
				t.Errorf("Expected error for %s, got nil", tc.name)
			}
		})
	}
}

func TestReflectionClient_FetchServiceDescriptors_Timeout(t *testing.T) {
	client := NewReflectionClient()

	// Create a context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.FetchServiceDescriptors(ctx, "grpc://localhost:50051", "test.Service", "")
	if err == nil {
		t.Error("Expected error for cancelled context")
	}
}

func TestValidateMethodExists(t *testing.T) {
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
					{
						Name: proto.String("OtherService"),
						Method: []*descriptorpb.MethodDescriptorProto{
							{Name: proto.String("OtherMethod")},
						},
					},
				},
			},
		},
	}

	testCases := []struct {
		name        string
		fds         *descriptorpb.FileDescriptorSet
		serviceName string
		methodName  string
		expected    bool
	}{
		{
			name:        "method exists",
			fds:         fds,
			serviceName: "example.v1.ExampleService",
			methodName:  "ExampleMethod",
			expected:    true,
		},
		{
			name:        "another method exists in same service",
			fds:         fds,
			serviceName: "example.v1.ExampleService",
			methodName:  "AnotherMethod",
			expected:    true,
		},
		{
			name:        "method does not exist in service",
			fds:         fds,
			serviceName: "example.v1.ExampleService",
			methodName:  "NonExistentMethod",
			expected:    false,
		},
		{
			name:        "service does not exist",
			fds:         fds,
			serviceName: "example.v1.NonExistentService",
			methodName:  "ExampleMethod",
			expected:    false,
		},
		{
			name:        "method exists in different service",
			fds:         fds,
			serviceName: "example.v1.OtherService",
			methodName:  "OtherMethod",
			expected:    true,
		},
		{
			name:        "empty file descriptor set",
			fds:         &descriptorpb.FileDescriptorSet{},
			serviceName: "example.v1.ExampleService",
			methodName:  "ExampleMethod",
			expected:    false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := validateMethodExists(tc.fds, tc.serviceName, tc.methodName)
			if result != tc.expected {
				t.Errorf("Expected %v, got %v", tc.expected, result)
			}
		})
	}
}
