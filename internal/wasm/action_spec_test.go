//go:build unit

package wasm

import (
	"testing"
)

func TestEscapeCELString(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`hello`, `hello`},
		{`test"scope`, `test\"scope`},
		{"line\nbreak", `line\nbreak`},
		{`back\slash`, `back\\slash`},
	}
	for _, tc := range tests {
		got := escapeCELString(tc.input)
		if got != tc.expected {
			t.Errorf("escapeCELString(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestJoinPredicates(t *testing.T) {
	tests := []struct {
		name       string
		predicates []string
		op         string
		expected   string
	}{
		{"empty", nil, "&&", "true"},
		{"single", []string{"x == 1"}, "&&", "x == 1"},
		{"multiple AND", []string{"a", "b"}, "&&", "(a) && (b)"},
		{"multiple OR", []string{"a", "b", "c"}, "||", "(a) || (b) || (c)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := joinPredicates(tc.predicates, tc.op)
			if got != tc.expected {
				t.Errorf("joinPredicates(%v, %q) = %q, want %q", tc.predicates, tc.op, got, tc.expected)
			}
		})
	}
}

func TestDomainAndFieldName(t *testing.T) {
	tests := []struct {
		input          string
		expectedDomain string
		expectedField  string
	}{
		{"auth.identity.user", "auth.identity", "user"},
		{"simple", "", "simple"},
		{"a.b.c.d", "a.b.c", "d"},
	}
	for _, tc := range tests {
		domain, field := DomainAndFieldName(tc.input)
		if domain != tc.expectedDomain || field != tc.expectedField {
			t.Errorf("DomainAndFieldName(%q) = (%q, %q), want (%q, %q)",
				tc.input, domain, field, tc.expectedDomain, tc.expectedField)
		}
	}
}

func TestBuildDescriptorPredicate(t *testing.T) {
	tests := []struct {
		name     string
		data     []ConditionalData
		expected string
	}{
		{"empty", nil, "true"},
		{
			"unconditional",
			[]ConditionalData{{Data: []DataType{{Value: &Static{Static: StaticSpec{Key: "limit", Value: "10"}}}}}},
			"true",
		},
		{
			"single predicate",
			[]ConditionalData{{Predicates: []string{"auth.identity.user == 'alice'"}}},
			"auth.identity.user == 'alice'",
		},
		{
			"multiple predicates single block",
			[]ConditionalData{{Predicates: []string{"auth.identity.user == 'alice'", "request.method == 'POST'"}}},
			"((auth.identity.user == 'alice') && (request.method == 'POST'))",
		},
		{
			"multiple blocks",
			[]ConditionalData{
				{Predicates: []string{"auth.identity.role == 'admin'"}},
				{Predicates: []string{"auth.identity.role == 'user'"}},
			},
			"auth.identity.role == 'admin' || auth.identity.role == 'user'",
		},
		{
			"mixed conditional and unconditional",
			[]ConditionalData{
				{Predicates: []string{"auth.identity.role == 'admin'"}},
				{Predicates: []string{}},
			},
			"true",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildDescriptorPredicate(tc.data)
			if got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

func TestMetadataCEL_ToCEL(t *testing.T) {
	tests := []struct {
		name     string
		metadata MetadataCEL
		expected string
	}{
		{
			"empty",
			MetadataCEL{},
			"envoy.config.core.v3.Metadata{}",
		},
		{
			"single field default domain",
			MetadataCEL{FilterMetadata: []FilterMetadataEntryCEL{{
				Domain: "io.kuadrant",
				Fields: []MetadataFieldCEL{{Key: "userid", Expression: "auth.identity.userid"}},
			}}},
			`envoy.config.core.v3.Metadata{filter_metadata: {"io.kuadrant": google.protobuf.Struct{fields: {"userid": google.protobuf.Value{struct_value: google.protobuf.Struct{fields: {"cel_expr": google.protobuf.Value{string_value: "auth.identity.userid"}}}}}}}}`,
		},
		{
			"multiple domains",
			MetadataCEL{FilterMetadata: []FilterMetadataEntryCEL{
				{
					Domain: "io.kuadrant",
					Fields: []MetadataFieldCEL{{Key: "userid", Expression: "auth.identity.userid"}},
				},
				{
					Domain: "io.kuadrant.custom",
					Fields: []MetadataFieldCEL{{Key: "role", Expression: "auth.identity.role"}},
				},
			}},
			`envoy.config.core.v3.Metadata{filter_metadata: {"io.kuadrant": google.protobuf.Struct{fields: {"userid": google.protobuf.Value{struct_value: google.protobuf.Struct{fields: {"cel_expr": google.protobuf.Value{string_value: "auth.identity.userid"}}}}}}, "io.kuadrant.custom": google.protobuf.Struct{fields: {"role": google.protobuf.Value{struct_value: google.protobuf.Struct{fields: {"cel_expr": google.protobuf.Value{string_value: "auth.identity.role"}}}}}}}}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.metadata.ToCEL()
			if got != tc.expected {
				t.Errorf("got:\n%s\nwant:\n%s", got, tc.expected)
			}
		})
	}
}

func TestCheckRequestCEL_ToCEL(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		req := CheckRequestCEL{Scope: "test-scope"}
		got := req.ToCEL()
		expected := `envoy.service.auth.v3.CheckRequest {
  attributes: envoy.service.auth.v3.AttributeContext {
    request: envoy.service.auth.v3.AttributeContext.Request {
      time: request.time,
      http: envoy.service.auth.v3.AttributeContext.HttpRequest {
        host: request.host,
        method: request.method,
        scheme: request.scheme,
        path: request.path,
        protocol: request.protocol,
        headers: request.headers
      }
    },
    destination: envoy.service.auth.v3.AttributeContext.Peer {
      address: envoy.config.core.v3.Address {
        socket_address: envoy.config.core.v3.SocketAddress {
          address: destination.address,
          port_value: uint(destination.port)
        }
      }
    },
    source: envoy.service.auth.v3.AttributeContext.Peer {
      address: envoy.config.core.v3.Address {
        socket_address: envoy.config.core.v3.SocketAddress {
          address: source.address,
          port_value: uint(source.port)
        }
      }
    },
    context_extensions: {"host": "test-scope"},
    metadata_context: envoy.config.core.v3.Metadata{}
  }
}`
		if got != expected {
			t.Errorf("got:\n%s\nwant:\n%s", got, expected)
		}
	})

	t.Run("with metadata", func(t *testing.T) {
		req := CheckRequestCEL{
			Scope: "my-scope",
			MetadataContext: MetadataCEL{FilterMetadata: []FilterMetadataEntryCEL{{
				Domain: "io.kuadrant",
				Fields: []MetadataFieldCEL{{Key: "userid", Expression: "auth.identity.userid"}},
			}}},
		}
		got := req.ToCEL()
		expected := `envoy.service.auth.v3.CheckRequest {
  attributes: envoy.service.auth.v3.AttributeContext {
    request: envoy.service.auth.v3.AttributeContext.Request {
      time: request.time,
      http: envoy.service.auth.v3.AttributeContext.HttpRequest {
        host: request.host,
        method: request.method,
        scheme: request.scheme,
        path: request.path,
        protocol: request.protocol,
        headers: request.headers
      }
    },
    destination: envoy.service.auth.v3.AttributeContext.Peer {
      address: envoy.config.core.v3.Address {
        socket_address: envoy.config.core.v3.SocketAddress {
          address: destination.address,
          port_value: uint(destination.port)
        }
      }
    },
    source: envoy.service.auth.v3.AttributeContext.Peer {
      address: envoy.config.core.v3.Address {
        socket_address: envoy.config.core.v3.SocketAddress {
          address: source.address,
          port_value: uint(source.port)
        }
      }
    },
    context_extensions: {"host": "my-scope"},
    metadata_context: envoy.config.core.v3.Metadata{filter_metadata: {"io.kuadrant": google.protobuf.Struct{fields: {"userid": google.protobuf.Value{struct_value: google.protobuf.Struct{fields: {"cel_expr": google.protobuf.Value{string_value: "auth.identity.userid"}}}}}}}}
  }
}`
		if got != expected {
			t.Errorf("got:\n%s\nwant:\n%s", got, expected)
		}
	})
}

func TestRateLimitRequestCEL_ToCEL(t *testing.T) {
	t.Run("simple empty", func(t *testing.T) {
		req := RateLimitRequestCEL{Domain: `"my-ratelimit"`, HitsAddend: "1u"}
		got := req.ToCEL()
		expected := `envoy.service.ratelimit.v3.RateLimitRequest {
    domain: "my-ratelimit",
    hits_addend: 1u,
    descriptors: []
}`
		if got != expected {
			t.Errorf("got:\n%s\nwant:\n%s", got, expected)
		}
	})

	t.Run("with conditional descriptor", func(t *testing.T) {
		req := RateLimitRequestCEL{
			Domain:     `"my-ratelimit"`,
			HitsAddend: "1u",
			Descriptors: []RateLimitDescriptorCEL{{
				Entries:   []DescriptorEntryCEL{{Key: "tier", ValueCEL: `"gold"`}},
				Predicate: "auth.identity.user == 'alice'",
			}},
		}
		got := req.ToCEL()
		expected := `envoy.service.ratelimit.v3.RateLimitRequest {
    domain: "my-ratelimit",
    hits_addend: 1u,
    descriptors: [envoy.extensions.common.ratelimit.v3.RateLimitDescriptor { entries: ((auth.identity.user == 'alice') ? [envoy.extensions.common.ratelimit.v3.RateLimitDescriptor.Entry { key: "tier", value: "gold" }] : []) }]
}`
		if got != expected {
			t.Errorf("got:\n%s\nwant:\n%s", got, expected)
		}
	})
}

func TestActionSpecBuild_Auth(t *testing.T) {
	spec := ActionSpec{
		ServiceName: AuthServiceName,
		Scope:       "my-auth",
		Sources:     []string{"AuthPolicy/default/my-policy"},
	}
	action := spec.Build()

	grpc, ok := action.(*GrpcAction)
	if !ok {
		t.Fatalf("expected *GrpcAction, got %T", action)
	}
	if grpc.Var != authResponseVar {
		t.Errorf("var = %q, want %q", grpc.Var, authResponseVar)
	}
	if grpc.Label != "auth" {
		t.Errorf("label = %q, want %q", grpc.Label, "auth")
	}
	if grpc.Predicate != "true" {
		t.Errorf("predicate = %q, want %q", grpc.Predicate, "true")
	}
	if !grpc.IsGuard {
		t.Error("expected isGuard=true")
	}
	if len(grpc.OnReply) != 5 {
		t.Fatalf("onReply length = %d, want 5", len(grpc.OnReply))
	}
	if grpc.OnReply[0].ActionType() != ActionKindDeny {
		t.Errorf("onReply[0] type = %s, want deny", grpc.OnReply[0].ActionType())
	}
	if grpc.OnReply[2].ActionType() != ActionKindStore {
		t.Errorf("onReply[2] type = %s, want store", grpc.OnReply[2].ActionType())
	}
	if grpc.OnReply[3].ActionType() != ActionKindHeaders {
		t.Errorf("onReply[3] type = %s, want headers", grpc.OnReply[3].ActionType())
	}
}

func TestActionSpecBuild_AuthWithBindings(t *testing.T) {
	spec := ActionSpec{
		ServiceName: AuthServiceName,
		Scope:       "my-auth",
		Sources:     []string{"AuthPolicy/default/my-policy"},
		Bindings: []DataBinding{
			{Domain: "identity", Field: "userid", Expression: "auth.identity.userid"},
			{Domain: "", Field: "region", Expression: "request.headers['x-region']"},
		},
	}
	action := spec.Build()

	grpc, ok := action.(*GrpcAction)
	if !ok {
		t.Fatalf("expected *GrpcAction, got %T", action)
	}

	expected := `envoy.service.auth.v3.CheckRequest {
  attributes: envoy.service.auth.v3.AttributeContext {
    request: envoy.service.auth.v3.AttributeContext.Request {
      time: request.time,
      http: envoy.service.auth.v3.AttributeContext.HttpRequest {
        host: request.host,
        method: request.method,
        scheme: request.scheme,
        path: request.path,
        protocol: request.protocol,
        headers: request.headers
      }
    },
    destination: envoy.service.auth.v3.AttributeContext.Peer {
      address: envoy.config.core.v3.Address {
        socket_address: envoy.config.core.v3.SocketAddress {
          address: destination.address,
          port_value: uint(destination.port)
        }
      }
    },
    source: envoy.service.auth.v3.AttributeContext.Peer {
      address: envoy.config.core.v3.Address {
        socket_address: envoy.config.core.v3.SocketAddress {
          address: source.address,
          port_value: uint(source.port)
        }
      }
    },
    context_extensions: {"host": "my-auth"},
    metadata_context: envoy.config.core.v3.Metadata{filter_metadata: {"io.kuadrant": google.protobuf.Struct{fields: {"region": google.protobuf.Value{string_value: string(request.headers['x-region'])}}}, "io.kuadrant.identity": google.protobuf.Struct{fields: {"userid": google.protobuf.Value{struct_value: google.protobuf.Struct{fields: {"cel_expr": google.protobuf.Value{string_value: "auth.identity.userid"}}}}}}}}
  }
}`
	if grpc.MessageBuilder != expected {
		t.Errorf("messageBuilder:\ngot:\n%s\nwant:\n%s", grpc.MessageBuilder, expected)
	}
}

func TestActionSpecBuild_RateLimit(t *testing.T) {
	spec := ActionSpec{
		ServiceName: RateLimitServiceName,
		Scope:       "my-ratelimit",
		Sources:     []string{"RateLimitPolicy/default/my-rlp"},
	}
	action := spec.Build()

	grpc, ok := action.(*GrpcAction)
	if !ok {
		t.Fatalf("expected *GrpcAction, got %T", action)
	}
	if grpc.Var != rateLimitResponseVar {
		t.Errorf("var = %q, want %q", grpc.Var, rateLimitResponseVar)
	}
	if grpc.Label != "ratelimit" {
		t.Errorf("label = %q, want %q", grpc.Label, "ratelimit")
	}
	if !grpc.IsGuard {
		t.Error("expected isGuard=true")
	}
	if len(grpc.OnReply) != 4 {
		t.Fatalf("onReply length = %d, want 4", len(grpc.OnReply))
	}
	if grpc.OnReply[0].ActionType() != ActionKindDeny {
		t.Errorf("onReply[0] type = %s, want deny", grpc.OnReply[0].ActionType())
	}
	completionStore, ok := grpc.OnReply[3].(*StoreAction)
	if !ok {
		t.Fatalf("onReply[3] type = %s, want store", grpc.OnReply[3].ActionType())
	}
	if completionStore.Path != RateLimitCompleteSignal {
		t.Errorf("completion store path = %q, want %q", completionStore.Path, RateLimitCompleteSignal)
	}
}

func TestActionSpecBuild_Report(t *testing.T) {
	spec := ActionSpec{
		ServiceName: RateLimitReportServiceName,
		Scope:       "my-report",
		Sources:     []string{"TokenRateLimitPolicy/default/my-trlp"},
	}
	action := spec.Build()

	grpc, ok := action.(*GrpcAction)
	if !ok {
		t.Fatalf("expected *GrpcAction, got %T", action)
	}
	if grpc.Var != reportResponseVar {
		t.Errorf("var = %q, want %q", grpc.Var, reportResponseVar)
	}
	if grpc.Label != "ratelimit_report" {
		t.Errorf("label = %q, want %q", grpc.Label, "ratelimit_report")
	}
	if grpc.IsGuard {
		t.Error("expected isGuard=false for report")
	}
	if len(grpc.OnReply) != 1 {
		t.Fatalf("onReply length = %d, want 1", len(grpc.OnReply))
	}
	if grpc.OnReply[0].ActionType() != ActionKindFail {
		t.Errorf("onReply[0] type = %s, want fail", grpc.OnReply[0].ActionType())
	}
}

func TestActionSpecBuild_ReportWithResponseBodyBindings(t *testing.T) {
	spec := ActionSpec{
		ServiceName: RateLimitReportServiceName,
		Scope:       "my-report",
		ConditionalData: []ConditionalData{{
			Data: []DataType{
				{Value: &Expression{ExpressionItem: ExpressionItem{Key: "model", Value: "responseBodyJSON('/model')"}}},
			},
		}},
		Bindings: []DataBinding{
			{Domain: "", Field: "zone", Expression: `"east"`},
		},
	}
	action := spec.Build()

	grpc := action.(*GrpcAction)
	expected := `envoy.service.ratelimit.v3.RateLimitRequest {
    domain: "my-report",
    hits_addend: 1u,
    descriptors: [envoy.extensions.common.ratelimit.v3.RateLimitDescriptor { entries: [envoy.extensions.common.ratelimit.v3.RateLimitDescriptor.Entry { key: "model", value: string(responseBodyJSON('/model')) }] }, envoy.extensions.common.ratelimit.v3.RateLimitDescriptor { entries: [envoy.extensions.common.ratelimit.v3.RateLimitDescriptor.Entry { key: "zone", value: string("east") }] }]
}`
	if grpc.MessageBuilder != expected {
		t.Errorf("messageBuilder:\ngot:\n%s\nwant:\n%s", grpc.MessageBuilder, expected)
	}
}

func TestActionSpecBuild_RateLimitFiltersResponseBodyBindings(t *testing.T) {
	spec := ActionSpec{
		ServiceName: RateLimitServiceName,
		Scope:       "my-rl",
		Bindings: []DataBinding{
			{Domain: "", Field: "zone", Expression: `"east"`},
			{Domain: "", Field: "tokens", Expression: "responseBodyJSON('/usage/total_tokens')"},
		},
	}
	action := spec.Build()

	grpc := action.(*GrpcAction)
	expected := `envoy.service.ratelimit.v3.RateLimitRequest {
    domain: "my-rl",
    hits_addend: 1u,
    descriptors: [envoy.extensions.common.ratelimit.v3.RateLimitDescriptor { entries: [envoy.extensions.common.ratelimit.v3.RateLimitDescriptor.Entry { key: "zone", value: string("east") }] }]
}`
	if grpc.MessageBuilder != expected {
		t.Errorf("messageBuilder:\ngot:\n%s\nwant:\n%s", grpc.MessageBuilder, expected)
	}
}

func TestActionSpecBuild_RateLimitFullWithConditionalDataAndBindings(t *testing.T) {
	spec := ActionSpec{
		ServiceName: RateLimitServiceName,
		Scope:       "rlp-full",
		ConditionalData: []ConditionalData{
			{
				Predicates: []string{"auth.identity.role == 'admin'"},
				Data:       []DataType{{Value: &Static{Static: StaticSpec{Key: "tier", Value: "gold"}}}},
			},
			{
				Data: []DataType{{Value: &Expression{ExpressionItem: ExpressionItem{Key: "method", Value: "request.method"}}}},
			},
		},
		Bindings: []DataBinding{{Domain: "", Field: "env", Expression: `"production"`}},
	}
	action := spec.Build()

	grpc := action.(*GrpcAction)
	expected := `envoy.service.ratelimit.v3.RateLimitRequest {
    domain: "rlp-full",
    hits_addend: 1u,
    descriptors: [envoy.extensions.common.ratelimit.v3.RateLimitDescriptor { entries: ((auth.identity.role == 'admin') ? [envoy.extensions.common.ratelimit.v3.RateLimitDescriptor.Entry { key: "tier", value: "gold" }] : []) + [envoy.extensions.common.ratelimit.v3.RateLimitDescriptor.Entry { key: "method", value: string(request.method) }] }, envoy.extensions.common.ratelimit.v3.RateLimitDescriptor { entries: [envoy.extensions.common.ratelimit.v3.RateLimitDescriptor.Entry { key: "env", value: string("production") }] }]
}`
	if grpc.MessageBuilder != expected {
		t.Errorf("messageBuilder:\ngot:\n%s\nwant:\n%s", grpc.MessageBuilder, expected)
	}
}

func TestActionSpecHasAuthAccess(t *testing.T) {
	noAuth := ActionSpec{
		ConditionalData: []ConditionalData{{
			Predicates: []string{"source.address != '127.0.0.1'"},
			Data:       []DataType{{Value: &Static{Static: StaticSpec{Key: "limit", Value: "1"}}}},
		}},
	}
	if noAuth.HasAuthAccess() {
		t.Error("expected no auth access")
	}

	authInPredicate := ActionSpec{
		ConditionalData: []ConditionalData{{
			Predicates: []string{"auth.something != '127.0.0.1'"},
		}},
	}
	if !authInPredicate.HasAuthAccess() {
		t.Error("expected auth access from predicate")
	}

	authInExpression := ActionSpec{
		ConditionalData: []ConditionalData{{
			Data: []DataType{{Value: &Expression{ExpressionItem: ExpressionItem{Key: "user", Value: "auth.identity.user"}}}},
		}},
	}
	if !authInExpression.HasAuthAccess() {
		t.Error("expected auth access from expression")
	}
}
