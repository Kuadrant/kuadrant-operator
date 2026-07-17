//go:build unit

/*
Copyright 2025 Red Hat, Inc.

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

package v1

import (
	"testing"
)

func TestRegisterActionMethodRequest_FieldAccessors(t *testing.T) {
	policy := &Policy{
		Metadata: &Metadata{
			Kind:      "MyPolicy",
			Namespace: "my-ns",
			Name:      "my-policy",
		},
	}

	req := &RegisterActionMethodRequest{
		Policy:          policy,
		Url:             "grpc://my-service.my-ns.svc.cluster.local:8081",
		Service:         "envoy.service.auth.v3.Authorization",
		Method:          "Check",
		Name:            "assess-threat",
		MessageTemplate: `ThreatRequest { uri: request.path }`,
	}

	if req.GetPolicy() != policy {
		t.Errorf("GetPolicy() returned unexpected value")
	}
	if req.GetUrl() != "grpc://my-service.my-ns.svc.cluster.local:8081" {
		t.Errorf("GetUrl() = %q, want %q", req.GetUrl(), "grpc://my-service.my-ns.svc.cluster.local:8081")
	}
	if req.GetService() != "envoy.service.auth.v3.Authorization" {
		t.Errorf("GetService() = %q, want %q", req.GetService(), "envoy.service.auth.v3.Authorization")
	}
	if req.GetMethod() != "Check" {
		t.Errorf("GetMethod() = %q, want %q", req.GetMethod(), "Check")
	}
	if req.GetName() != "assess-threat" {
		t.Errorf("GetName() = %q, want %q", req.GetName(), "assess-threat")
	}
	if req.GetMessageTemplate() != `ThreatRequest { uri: request.path }` {
		t.Errorf("GetMessageTemplate() = %q, want %q", req.GetMessageTemplate(), `ThreatRequest { uri: request.path }`)
	}
}

func TestRegisterActionMethodRequest_NilSafeGetters(t *testing.T) {
	var req *RegisterActionMethodRequest

	if req.GetPolicy() != nil {
		t.Errorf("GetPolicy() on nil receiver should return nil")
	}
	if req.GetUrl() != "" {
		t.Errorf("GetUrl() on nil receiver should return empty string")
	}
	if req.GetService() != "" {
		t.Errorf("GetService() on nil receiver should return empty string")
	}
	if req.GetMethod() != "" {
		t.Errorf("GetMethod() on nil receiver should return empty string")
	}
	if req.GetName() != "" {
		t.Errorf("GetName() on nil receiver should return empty string")
	}
	if req.GetMessageTemplate() != "" {
		t.Errorf("GetMessageTemplate() on nil receiver should return empty string")
	}
}

func TestRegisterActionMethodRequest_ZeroValues(t *testing.T) {
	req := &RegisterActionMethodRequest{}

	if req.GetPolicy() != nil {
		t.Errorf("GetPolicy() on zero-value request should return nil")
	}
	if req.GetUrl() != "" {
		t.Errorf("GetUrl() on zero-value request should return empty string")
	}
	if req.GetService() != "" {
		t.Errorf("GetService() on zero-value request should return empty string")
	}
	if req.GetMethod() != "" {
		t.Errorf("GetMethod() on zero-value request should return empty string")
	}
	if req.GetName() != "" {
		t.Errorf("GetName() on zero-value request should return empty string")
	}
	if req.GetMessageTemplate() != "" {
		t.Errorf("GetMessageTemplate() on zero-value request should return empty string")
	}
}

func TestRegisterActionMethod_FullMethodName(t *testing.T) {
	expected := "/kuadrant.v1.ExtensionService/RegisterActionMethod"
	if ExtensionService_RegisterActionMethod_FullMethodName != expected {
		t.Errorf("FullMethodName = %q, want %q", ExtensionService_RegisterActionMethod_FullMethodName, expected)
	}
}
