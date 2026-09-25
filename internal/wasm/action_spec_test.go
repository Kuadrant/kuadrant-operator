//go:build unit

package wasm

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
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

func TestReserveRequestCEL_ToCEL(t *testing.T) {
	t.Run("with ttl", func(t *testing.T) {
		req := ReserveRequestCEL{Domain: `"my-scope"`, Amount: "uint(5000)", TTL: `duration("30s")`}
		got := req.ToCEL()
		expected := `kuadrant.service.ratelimit.v1.ReserveRequest {
    domain: "my-scope",
    descriptors: [],
    amount: uint(5000),
    ttl: duration("30s")
}`
		if got != expected {
			t.Errorf("got:\n%s\nwant:\n%s", got, expected)
		}
	})

	t.Run("ttl omitted when empty", func(t *testing.T) {
		req := ReserveRequestCEL{Domain: `"my-scope"`, Amount: "uint(5000)"}
		got := req.ToCEL()
		expected := `kuadrant.service.ratelimit.v1.ReserveRequest {
    domain: "my-scope",
    descriptors: [],
    amount: uint(5000)
}`
		if got != expected {
			t.Errorf("got:\n%s\nwant:\n%s", got, expected)
		}
	})
}

func TestCommitRequestCEL_ToCEL(t *testing.T) {
	req := CommitRequestCEL{
		Domain:        `"my-scope"`,
		ReservationID: "kuadrant.internal.tokenratelimit.reservation.tokenlimit_foo__abcd",
		ActualAmount:  `uint(responseBodyJSON("/usage/total_tokens"))`,
	}
	got := req.ToCEL()
	expected := `kuadrant.service.ratelimit.v1.CommitRequest {
    domain: "my-scope",
    descriptors: [],
    reservation_id: kuadrant.internal.tokenratelimit.reservation.tokenlimit_foo__abcd,
    actual_amount: uint(responseBodyJSON("/usage/total_tokens"))
}`
	if got != expected {
		t.Errorf("got:\n%s\nwant:\n%s", got, expected)
	}
}

func TestActionSpecBuild_Reserve(t *testing.T) {
	spec := ActionSpec{
		ServiceName: RateLimitReserveServiceName,
		Scope:       "my-scope",
		Sources:     []string{"TokenRateLimitPolicy/default/my-trlp"},
		ConditionalData: []ConditionalData{{
			Data: []DataType{
				{Value: &Expression{ExpressionItem: ExpressionItem{Key: "tokenlimit.foo__abcd", Value: "1"}}},
			},
		}},
		Reservation: &ReservationSpec{
			ID:     "tokenlimit.foo__abcd",
			Amount: "5000",
			TTL:    `duration("30s")`,
		},
	}
	action := spec.Build()

	grpc, ok := action.(*GrpcAction)
	if !ok {
		t.Fatalf("expected *GrpcAction, got %T", action)
	}
	if grpc.Var != reserveResponseVar {
		t.Errorf("var = %q, want %q", grpc.Var, reserveResponseVar)
	}
	if grpc.Label != "ratelimit_reserve" {
		t.Errorf("label = %q, want %q", grpc.Label, "ratelimit_reserve")
	}
	if !grpc.IsGuard {
		t.Error("expected isGuard=true (reserve runs in request phase)")
	}
	// amount is wrapped in uint(), ttl passed through, reservation.id is not a descriptor entry
	for _, want := range []string{"amount: uint(5000)", `ttl: duration("30s")`, `key: "tokenlimit.foo__abcd"`} {
		if !strings.Contains(grpc.MessageBuilder, want) {
			t.Errorf("messageBuilder missing %q:\n%s", want, grpc.MessageBuilder)
		}
	}
	if strings.Contains(grpc.MessageBuilder, "reservation.id") {
		t.Errorf("reservation.id should not appear as a descriptor entry:\n%s", grpc.MessageBuilder)
	}
	// onReply: deny(code==2), store(reservation_id), fail(unknown code)
	if len(grpc.OnReply) != 3 {
		t.Fatalf("onReply length = %d, want 3", len(grpc.OnReply))
	}
	deny, ok := grpc.OnReply[0].(*DenyAction)
	if !ok {
		t.Fatalf("onReply[0] = %T, want *DenyAction", grpc.OnReply[0])
	}
	wantDenyBody := `body: "{\"error\": {\"message\": \"Too Many Requests\", \"type\": \"rate_limit_exceeded\", \"code\": 429}}"`
	if !strings.Contains(deny.DenyWith, wantDenyBody) {
		t.Errorf("deny body = %q, want it to contain %q", deny.DenyWith, wantDenyBody)
	}
	if !strings.Contains(deny.DenyWith, `["content-type", "application/json"]`) {
		t.Errorf("deny headers = %q, want a content-type: application/json header", deny.DenyWith)
	}
	store, ok := grpc.OnReply[1].(*StoreAction)
	if !ok {
		t.Fatalf("onReply[1] = %T, want *StoreAction", grpc.OnReply[1])
	}
	wantPath := "kuadrant.internal.tokenratelimit.reservation.tokenlimit_foo__abcd"
	if store.Path != wantPath {
		t.Errorf("store path = %q, want %q", store.Path, wantPath)
	}
	if grpc.OnReply[2].ActionType() != ActionKindFail {
		t.Errorf("onReply[2] type = %s, want fail", grpc.OnReply[2].ActionType())
	}
}

func TestActionSpecBuild_Commit(t *testing.T) {
	spec := ActionSpec{
		ServiceName: RateLimitCommitServiceName,
		Scope:       "my-scope",
		Sources:     []string{"TokenRateLimitPolicy/default/my-trlp"},
		ConditionalData: []ConditionalData{{
			Data: []DataType{
				{Value: &Expression{ExpressionItem: ExpressionItem{Key: "tokenlimit.foo__abcd", Value: "1"}}},
			},
		}},
		Reservation: &ReservationSpec{
			ID:           "tokenlimit.foo__abcd",
			ActualAmount: `responseBodyJSON("/usage/total_tokens")`,
		},
	}
	action := spec.Build()

	grpc, ok := action.(*GrpcAction)
	if !ok {
		t.Fatalf("expected *GrpcAction, got %T", action)
	}
	if grpc.Var != commitResponseVar {
		t.Errorf("var = %q, want %q", grpc.Var, commitResponseVar)
	}
	if grpc.Label != "ratelimit_commit" {
		t.Errorf("label = %q, want %q", grpc.Label, "ratelimit_commit")
	}
	if grpc.IsGuard {
		t.Error("expected isGuard=false (commit runs in response phase)")
	}
	// commit is skipped only when neither a reservation was held nor the usage
	// is parseable (RFC 0021); otherwise it always runs.
	wantPath := "kuadrant.internal.tokenratelimit.reservation.tokenlimit_foo__abcd"
	hasReservation := fmt.Sprintf("%s != null", wantPath)
	amountKnown := `(responseBodyJSON("/usage/total_tokens")) != null`
	wantPredicate := fmt.Sprintf("(%s) || (%s)", hasReservation, amountKnown)
	if grpc.Predicate != wantPredicate {
		t.Errorf("predicate = %q, want %q", grpc.Predicate, wantPredicate)
	}
	// no reservation held -> empty reservation_id, which Limitador treats like a plain Report
	wantReservationID := fmt.Sprintf(`(%s) ? %s : ""`, hasReservation, wantPath)
	if !strings.Contains(grpc.MessageBuilder, "reservation_id: "+wantReservationID) {
		t.Errorf("messageBuilder should read back reservation id from store path:\n%s", grpc.MessageBuilder)
	}
	// unparseable usage -> commit 0 so a held reservation is still released without charging an unknown amount
	wantActualAmount := fmt.Sprintf(`(%s) ? uint(responseBodyJSON("/usage/total_tokens")) : 0u`, amountKnown)
	if !strings.Contains(grpc.MessageBuilder, "actual_amount: "+wantActualAmount) {
		t.Errorf("messageBuilder missing actual_amount:\n%s", grpc.MessageBuilder)
	}
	if len(grpc.OnReply) != 1 {
		t.Fatalf("onReply length = %d, want 1", len(grpc.OnReply))
	}
	if grpc.OnReply[0].ActionType() != ActionKindFail {
		t.Errorf("onReply[0] type = %s, want fail", grpc.OnReply[0].ActionType())
	}
}

func TestActionSpecBuild_Commit_NoActualAmountData(t *testing.T) {
	// Defensive case: a Commit spec with no ActualAmount set at all (shouldn't
	// happen from the reconciler, which always sets one, but the builder must
	// not panic and must still gate correctly on whether a reservation was held).
	spec := ActionSpec{
		ServiceName: RateLimitCommitServiceName,
		Scope:       "my-scope",
		Sources:     []string{"TokenRateLimitPolicy/default/my-trlp"},
		Reservation: &ReservationSpec{
			ID: "tokenlimit.foo__abcd",
		},
	}
	action := spec.Build()

	grpc, ok := action.(*GrpcAction)
	if !ok {
		t.Fatalf("expected *GrpcAction, got %T", action)
	}

	wantPath := "kuadrant.internal.tokenratelimit.reservation.tokenlimit_foo__abcd"
	wantPredicate := fmt.Sprintf("(%s != null) || (false)", wantPath)
	if grpc.Predicate != wantPredicate {
		t.Errorf("predicate = %q, want %q", grpc.Predicate, wantPredicate)
	}
	if !strings.Contains(grpc.MessageBuilder, "actual_amount: 0u") {
		t.Errorf("messageBuilder should default actual_amount to 0u:\n%s", grpc.MessageBuilder)
	}
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
	if len(grpc.OnReply) != 3 {
		t.Fatalf("onReply length = %d, want 3", len(grpc.OnReply))
	}
	deny, ok := grpc.OnReply[0].(*DenyAction)
	if !ok {
		t.Fatalf("onReply[0] = %T, want *DenyAction", grpc.OnReply[0])
	}
	// plain RateLimitPolicy keeps the plain-text body; the JSON error body is
	// only used for TokenRateLimitPolicy denials (Reserve, Optimistic's Check).
	if !strings.Contains(deny.DenyWith, `body: "Too Many Requests\n"`) {
		t.Errorf("deny body = %q, want plain-text \"Too Many Requests\"", deny.DenyWith)
	}
	if grpc.OnReply[1].ActionType() != ActionKindHeaders {
		t.Errorf("onReply[1] type = %s, want headers", grpc.OnReply[1].ActionType())
	}
	if grpc.OnReply[2].ActionType() != ActionKindFail {
		t.Errorf("onReply[2] type = %s, want fail", grpc.OnReply[2].ActionType())
	}
}

func TestActionSpecBuild_Check_DenyBody(t *testing.T) {
	spec := ActionSpec{
		ServiceName: RateLimitCheckServiceName,
		Scope:       "my-scope",
		Sources:     []string{"TokenRateLimitPolicy/default/my-trlp"},
	}
	action := spec.Build()

	grpc, ok := action.(*GrpcAction)
	if !ok {
		t.Fatalf("expected *GrpcAction, got %T", action)
	}
	deny, ok := grpc.OnReply[0].(*DenyAction)
	if !ok {
		t.Fatalf("onReply[0] = %T, want *DenyAction", grpc.OnReply[0])
	}
	wantDenyBody := `body: "{\"error\": {\"message\": \"Too Many Requests\", \"type\": \"rate_limit_exceeded\", \"code\": 429}}"`
	if !strings.Contains(deny.DenyWith, wantDenyBody) {
		t.Errorf("deny body = %q, want it to contain %q", deny.DenyWith, wantDenyBody)
	}
	if !strings.Contains(deny.DenyWith, `["content-type", "application/json"]`) {
		t.Errorf("deny headers = %q, want a content-type: application/json header", deny.DenyWith)
	}
	// the rate limit response's own response_headers_to_add must still be present
	if !strings.Contains(deny.DenyWith, "ratelimit_response.response_headers_to_add") {
		t.Errorf("deny headers = %q, want ratelimit_response.response_headers_to_add preserved", deny.DenyWith)
	}
}

func TestActionSpecBuild_RateLimitSequentialExecution(t *testing.T) {
	spec := ActionSpec{
		ServiceName: RateLimitServiceName,
		Scope:       "my-ratelimit",
		Sources:     []string{"RateLimitPolicy/default/my-rlp"},
		Execution:   ExecutionSequential,
	}
	action := spec.Build()

	grpc, ok := action.(*GrpcAction)
	if !ok {
		t.Fatalf("expected *GrpcAction, got %T", action)
	}
	if grpc.Execution != ExecutionSequential {
		t.Errorf("execution = %q, want %q", grpc.Execution, ExecutionSequential)
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

func TestBodyRefFieldName(t *testing.T) {
	tests := []struct {
		pointer  string
		expected string
	}{
		{"/usage/total_tokens", "total_tokens"},
		{"/model", "model"},
		{"/a/b/c", "c"},
		{"single", "single"},
		{"/usage/total-tokens", "total_tokens"},
		{"/usage/weird]key", "weird_key"},
		{"/2024", "_2024"},
		{"/usage/2tokens", "_2tokens"},
	}
	for _, tc := range tests {
		got := bodyRefFieldName(tc.pointer)
		if got != tc.expected {
			t.Errorf("bodyRefFieldName(%q) = %q, want %q", tc.pointer, got, tc.expected)
		}
	}
}

func TestSanitizePointer(t *testing.T) {
	tests := []struct {
		pointer  string
		expected string
	}{
		{"/usage/total_tokens", "usage_total_tokens"},
		{"/model", "model"},
		{"/a/b/c", "a_b_c"},
		{"/a/b" + pointerListKeySep + "/c/d" + pointerListKeySep + "number", "a_b__c_d_number"},
		{"/2tokens", "_2tokens"},
	}
	for _, tc := range tests {
		got := sanitizePointer(tc.pointer)
		if got != tc.expected {
			t.Errorf("sanitizePointer(%q) = %q, want %q", tc.pointer, got, tc.expected)
		}
	}
}

func TestExtractBodyRefs(t *testing.T) {
	t.Run("no refs", func(t *testing.T) {
		refs := extractBodyRefs("request.method == 'GET'")
		if len(refs) != 0 {
			t.Errorf("expected no refs, got %d", len(refs))
		}
	})

	t.Run("single response ref double quotes", func(t *testing.T) {
		refs := extractBodyRefs(`responseBodyJSON("/usage/total_tokens")`)
		if len(refs) != 1 {
			t.Fatalf("expected 1 ref, got %d", len(refs))
		}
		if refs[0].Direction != "response" {
			t.Errorf("direction = %q, want %q", refs[0].Direction, "response")
		}
		if refs[0].FieldName != "total_tokens" {
			t.Errorf("fieldName = %q, want %q", refs[0].FieldName, "total_tokens")
		}
		if refs[0].Pointer != "/usage/total_tokens" {
			t.Errorf("pointer = %q, want %q", refs[0].Pointer, "/usage/total_tokens")
		}
	})

	t.Run("single response ref single quotes", func(t *testing.T) {
		refs := extractBodyRefs("responseBodyJSON('/model')")
		if len(refs) != 1 {
			t.Fatalf("expected 1 ref, got %d", len(refs))
		}
		if refs[0].FieldName != "model" {
			t.Errorf("fieldName = %q, want %q", refs[0].FieldName, "model")
		}
	})

	t.Run("request ref", func(t *testing.T) {
		refs := extractBodyRefs(`requestBodyJSON("/prompt")`)
		if len(refs) != 1 {
			t.Fatalf("expected 1 ref, got %d", len(refs))
		}
		if refs[0].Direction != "request" {
			t.Errorf("direction = %q, want %q", refs[0].Direction, "request")
		}
	})

	t.Run("deduplicates", func(t *testing.T) {
		refs := extractBodyRefs(`responseBodyJSON("/model") + responseBodyJSON("/model")`)
		if len(refs) != 1 {
			t.Errorf("expected 1 deduplicated ref, got %d", len(refs))
		}
	})

	t.Run("list of candidates with type hint", func(t *testing.T) {
		refs := extractBodyRefs(`responseBodyJSON(["/usage/total_tokens", "/usageMetadata/totalTokenCount"], "number")`)
		if len(refs) != 1 {
			t.Fatalf("expected 1 ref, got %d", len(refs))
		}
		if refs[0].Direction != "response" {
			t.Errorf("direction = %q, want %q", refs[0].Direction, "response")
		}
		if refs[0].FieldName != "total_tokens" {
			t.Errorf("fieldName = %q, want %q (leaf of first candidate)", refs[0].FieldName, "total_tokens")
		}
		expectedPointer := "/usage/total_tokens" + pointerListKeySep + "/usageMetadata/totalTokenCount" + pointerListKeySep + "number"
		if refs[0].Pointer != expectedPointer {
			t.Errorf("pointer = %q, want %q", refs[0].Pointer, expectedPointer)
		}
	})

	t.Run("list of candidates without type hint", func(t *testing.T) {
		refs := extractBodyRefs(`responseBodyJSON(["/a", "/b"])`)
		if len(refs) != 1 {
			t.Fatalf("expected 1 ref, got %d", len(refs))
		}
		expectedPointer := "/a" + pointerListKeySep + "/b" + pointerListKeySep
		if refs[0].Pointer != expectedPointer {
			t.Errorf("pointer = %q, want %q", refs[0].Pointer, expectedPointer)
		}
	})

	t.Run("single-candidate list with type hint", func(t *testing.T) {
		refs := extractBodyRefs(`requestBodyJSON(['/prompt'], "string")`)
		if len(refs) != 1 {
			t.Fatalf("expected 1 ref, got %d", len(refs))
		}
		if refs[0].Direction != "request" {
			t.Errorf("direction = %q, want %q", refs[0].Direction, "request")
		}
		if refs[0].FieldName != "prompt" {
			t.Errorf("fieldName = %q, want %q", refs[0].FieldName, "prompt")
		}
	})

	t.Run("distinguishes lists differing only in order", func(t *testing.T) {
		refs := extractBodyRefs(`responseBodyJSON(["/a", "/b"], "number") + responseBodyJSON(["/b", "/a"], "number")`)
		if len(refs) != 2 {
			t.Fatalf("expected 2 distinct refs (order matters), got %d", len(refs))
		}
	})

	t.Run("list candidate containing a literal ']'", func(t *testing.T) {
		// RFC 6901 reference tokens may legally contain "]", so the list matcher must not
		// terminate at the first "]" it sees. FieldName is sanitized to "a_b" (rather than
		// truncated to "a") to prove the full pointer "/usage/a]b" was captured before the
		// CEL-identifier-safe substitution was applied.
		refs := extractBodyRefs(`responseBodyJSON(["/usage/a]b"], "number")`)
		if len(refs) != 1 {
			t.Fatalf("expected 1 ref, got %d", len(refs))
		}
		if refs[0].FieldName != "a_b" {
			t.Errorf("fieldName = %q, want %q", refs[0].FieldName, "a_b")
		}
	})

	t.Run("single-quoted candidate containing a literal ']'", func(t *testing.T) {
		refs := extractBodyRefs(`responseBodyJSON(['/usage/a]b', '/other'], "number")`)
		if len(refs) != 1 {
			t.Fatalf("expected 1 ref, got %d", len(refs))
		}
		expectedPointer := "/usage/a]b" + pointerListKeySep + "/other" + pointerListKeySep + "number"
		if refs[0].Pointer != expectedPointer {
			t.Errorf("pointer = %q, want %q", refs[0].Pointer, expectedPointer)
		}
	})
}

func TestBuildActions_NoBodyRefs(t *testing.T) {
	specs := []ActionSpec{
		{
			ServiceName: AuthServiceName,
			Scope:       "my-auth",
			Sources:     []string{"AuthPolicy/default/my-policy"},
		},
		{
			ServiceName: RateLimitServiceName,
			Scope:       "my-rl",
			Sources:     []string{"RateLimitPolicy/default/my-rlp"},
		},
	}
	actions := BuildActions(specs)

	if len(actions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(actions))
	}
	if actions[0].ActionType() != ActionKindGrpc {
		t.Errorf("actions[0] type = %s, want grpc", actions[0].ActionType())
	}
	if actions[1].ActionType() != ActionKindGrpc {
		t.Errorf("actions[1] type = %s, want grpc", actions[1].ActionType())
	}
}

func TestBuildActions_TokenLimitHitsAddend(t *testing.T) {
	specs := []ActionSpec{
		{
			ServiceName: RateLimitCheckServiceName,
			Scope:       "my-scope",
			Sources:     []string{"TokenRateLimitPolicy/default/my-trlp"},
			ConditionalData: []ConditionalData{{
				Data: []DataType{
					{Value: &Expression{ExpressionItem: ExpressionItem{Key: "ratelimit.hits_addend", Value: "0"}}},
				},
			}},
		},
		{
			ServiceName: RateLimitReportServiceName,
			Scope:       "my-scope",
			Sources:     []string{"TokenRateLimitPolicy/default/my-trlp"},
			ConditionalData: []ConditionalData{{
				Data: []DataType{
					{Value: &Expression{ExpressionItem: ExpressionItem{
						Key:   "ratelimit.hits_addend",
						Value: `responseBodyJSON("/usage/total_tokens")`,
					}}},
				},
			}},
		},
	}
	actions := BuildActions(specs)

	// Expect: store action + check action + report action
	if len(actions) != 3 {
		t.Fatalf("expected 3 actions, got %d", len(actions))
	}

	// First action should be the store
	store, ok := actions[0].(*StoreAction)
	if !ok {
		t.Fatalf("actions[0] type = %s, want store", actions[0].ActionType())
	}
	if store.Path != responseBodyStorePath {
		t.Errorf("store path = %q, want %q", store.Path, responseBodyStorePath)
	}
	expectedValue := `{"total_tokens": responseBodyJSON("/usage/total_tokens")}`
	if store.Value != expectedValue {
		t.Errorf("store value = %q, want %q", store.Value, expectedValue)
	}
	if store.IsGuard {
		t.Error("store action should not be a guard")
	}

	// Check action should be unchanged (no body refs in check spec)
	checkGrpc, ok := actions[1].(*GrpcAction)
	if !ok {
		t.Fatalf("actions[1] type = %s, want grpc", actions[1].ActionType())
	}
	if !checkGrpc.IsGuard {
		t.Error("check action should be a guard")
	}

	// Report action should reference the store path instead of responseBodyJSON
	reportGrpc, ok := actions[2].(*GrpcAction)
	if !ok {
		t.Fatalf("actions[2] type = %s, want grpc", actions[2].ActionType())
	}
	if reportGrpc.IsGuard {
		t.Error("report action should not be a guard")
	}
	expectedStorePath := responseBodyStorePath + ".total_tokens"
	expectedHitsAddend := "uint(" + expectedStorePath + ")"
	if !strings.Contains(reportGrpc.MessageBuilder, expectedHitsAddend) {
		t.Errorf("report message should contain %q, got:\n%s", expectedHitsAddend, reportGrpc.MessageBuilder)
	}
	if strings.Contains(reportGrpc.MessageBuilder, "responseBodyJSON") {
		t.Error("report message should not contain responseBodyJSON after replacement")
	}
}

// TestBuildActions_ReservationActualAmount mirrors TestBuildActions_TokenLimitHitsAddend
// for Reservation mode: Reservation.ActualAmount (not ConditionalData) is where a
// responseBodyJSON(...) reference lives for Commit, so BuildActions/replaceBodyRefs
// must hoist and rewrite it there too.
func TestBuildActions_ReservationActualAmount(t *testing.T) {
	specs := []ActionSpec{
		{
			ServiceName: RateLimitReserveServiceName,
			Scope:       "my-scope",
			Sources:     []string{"TokenRateLimitPolicy/default/my-trlp"},
			Reservation: &ReservationSpec{ID: "tokenlimit.foo__abcd", Amount: "5000"},
		},
		{
			ServiceName: RateLimitCommitServiceName,
			Scope:       "my-scope",
			Sources:     []string{"TokenRateLimitPolicy/default/my-trlp"},
			Reservation: &ReservationSpec{
				ID:           "tokenlimit.foo__abcd",
				ActualAmount: `responseBodyJSON("/usage/total_tokens")`,
			},
		},
	}
	actions := BuildActions(specs)

	// Expect: store action + reserve action + commit action
	if len(actions) != 3 {
		t.Fatalf("expected 3 actions, got %d", len(actions))
	}

	store, ok := actions[0].(*StoreAction)
	if !ok {
		t.Fatalf("actions[0] type = %s, want store", actions[0].ActionType())
	}
	if store.Path != responseBodyStorePath {
		t.Errorf("store path = %q, want %q", store.Path, responseBodyStorePath)
	}
	expectedValue := `{"total_tokens": responseBodyJSON("/usage/total_tokens")}`
	if store.Value != expectedValue {
		t.Errorf("store value = %q, want %q", store.Value, expectedValue)
	}

	// Reserve action should be unchanged (no body refs in its Reservation data)
	reserveGrpc, ok := actions[1].(*GrpcAction)
	if !ok {
		t.Fatalf("actions[1] type = %s, want grpc", actions[1].ActionType())
	}
	if !reserveGrpc.IsGuard {
		t.Error("reserve action should be a guard")
	}
	if strings.Contains(reserveGrpc.MessageBuilder, "responseBodyJSON") {
		t.Error("reserve message should not reference responseBodyJSON")
	}

	// Commit action should reference the store path instead of responseBodyJSON
	commitGrpc, ok := actions[2].(*GrpcAction)
	if !ok {
		t.Fatalf("actions[2] type = %s, want grpc", actions[2].ActionType())
	}
	if commitGrpc.IsGuard {
		t.Error("commit action should not be a guard")
	}
	expectedStorePath := responseBodyStorePath + ".total_tokens"
	if !strings.Contains(commitGrpc.MessageBuilder, expectedStorePath) {
		t.Errorf("commit message should contain %q, got:\n%s", expectedStorePath, commitGrpc.MessageBuilder)
	}
	if strings.Contains(commitGrpc.MessageBuilder, "responseBodyJSON") {
		t.Error("commit message should not contain responseBodyJSON after replacement")
	}
}

func TestBuildActions_ListBodyRef(t *testing.T) {
	specs := []ActionSpec{
		{
			ServiceName: RateLimitCheckServiceName,
			Scope:       "my-scope",
			Sources:     []string{"TokenRateLimitPolicy/default/my-trlp"},
			ConditionalData: []ConditionalData{{
				Data: []DataType{
					{Value: &Expression{ExpressionItem: ExpressionItem{Key: "ratelimit.hits_addend", Value: "0"}}},
				},
			}},
		},
		{
			ServiceName: RateLimitReportServiceName,
			Scope:       "my-scope",
			Sources:     []string{"TokenRateLimitPolicy/default/my-trlp"},
			ConditionalData: []ConditionalData{{
				Data: []DataType{
					{Value: &Expression{ExpressionItem: ExpressionItem{
						Key:   "ratelimit.hits_addend",
						Value: `responseBodyJSON(["/usage/total_tokens", "/usageMetadata/totalTokenCount"], "number")`,
					}}},
				},
			}},
		},
	}
	actions := BuildActions(specs)

	if len(actions) != 3 {
		t.Fatalf("expected 3 actions, got %d", len(actions))
	}

	store, ok := actions[0].(*StoreAction)
	if !ok {
		t.Fatalf("actions[0] type = %s, want store", actions[0].ActionType())
	}
	expectedValue := `{"total_tokens": responseBodyJSON(["/usage/total_tokens", "/usageMetadata/totalTokenCount"], "number")}`
	if store.Value != expectedValue {
		t.Errorf("store value = %q, want %q", store.Value, expectedValue)
	}

	reportGrpc, ok := actions[2].(*GrpcAction)
	if !ok {
		t.Fatalf("actions[2] type = %s, want grpc", actions[2].ActionType())
	}
	if strings.Contains(reportGrpc.MessageBuilder, "responseBodyJSON") {
		t.Error("report message should not contain responseBodyJSON after replacement")
	}
	expectedStorePath := responseBodyStorePath + ".total_tokens"
	if !strings.Contains(reportGrpc.MessageBuilder, "uint("+expectedStorePath+")") {
		t.Errorf("report message should reference store path for total_tokens, got:\n%s", reportGrpc.MessageBuilder)
	}
}

func TestBuildActions_MergedBodyRefs(t *testing.T) {
	specs := []ActionSpec{
		{
			ServiceName: RateLimitReportServiceName,
			Scope:       "my-scope",
			Sources:     []string{"TokenRateLimitPolicy/default/my-trlp"},
			ConditionalData: []ConditionalData{{
				Data: []DataType{
					{Value: &Expression{ExpressionItem: ExpressionItem{
						Key:   "ratelimit.hits_addend",
						Value: `responseBodyJSON("/usage/total_tokens")`,
					}}},
				},
			}},
			Bindings: []DataBinding{
				{Domain: "metrics.labels", Field: "model", Expression: "responseBodyJSON('/model')"},
			},
		},
	}
	actions := BuildActions(specs)

	// Expect: 1 store action + 1 grpc action
	if len(actions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(actions))
	}

	store, ok := actions[0].(*StoreAction)
	if !ok {
		t.Fatalf("actions[0] type = %s, want store", actions[0].ActionType())
	}
	if store.Path != responseBodyStorePath {
		t.Errorf("store path = %q, want %q", store.Path, responseBodyStorePath)
	}
	// Map should contain both fields (sorted: model before total_tokens)
	if !strings.Contains(store.Value, `"model": responseBodyJSON('/model')`) {
		t.Errorf("store value should contain model field, got: %s", store.Value)
	}
	if !strings.Contains(store.Value, `"total_tokens": responseBodyJSON("/usage/total_tokens")`) {
		t.Errorf("store value should contain total_tokens field, got: %s", store.Value)
	}

	grpc, ok := actions[1].(*GrpcAction)
	if !ok {
		t.Fatalf("actions[1] type = %s, want grpc", actions[1].ActionType())
	}
	if strings.Contains(grpc.MessageBuilder, "responseBodyJSON") {
		t.Error("grpc message should not contain responseBodyJSON after replacement")
	}
	if !strings.Contains(grpc.MessageBuilder, "kuadrant.internal.response.body.total_tokens") {
		t.Errorf("grpc message should reference store path for total_tokens, got:\n%s", grpc.MessageBuilder)
	}
	if !strings.Contains(grpc.MessageBuilder, "kuadrant.internal.response.body.model") {
		t.Errorf("grpc message should reference store path for model, got:\n%s", grpc.MessageBuilder)
	}
}

func TestBuildActions_CollidingLeafFieldNames(t *testing.T) {
	specs := []ActionSpec{
		{
			ServiceName: RateLimitReportServiceName,
			Scope:       "my-scope",
			Sources:     []string{"TokenRateLimitPolicy/default/my-trlp"},
			ConditionalData: []ConditionalData{{
				Data: []DataType{
					{Value: &Expression{ExpressionItem: ExpressionItem{
						Key:   "ratelimit.hits_addend",
						Value: `responseBodyJSON("/usage/total_tokens")`,
					}}},
				},
			}},
			Bindings: []DataBinding{
				{Domain: "metrics.labels", Field: "tokens", Expression: `responseBodyJSON("/metadata/total_tokens")`},
			},
		},
	}
	actions := BuildActions(specs)

	if len(actions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(actions))
	}

	store, ok := actions[0].(*StoreAction)
	if !ok {
		t.Fatalf("actions[0] type = %s, want store", actions[0].ActionType())
	}

	// Both refs share leaf "total_tokens" so should use sanitized pointer as key
	if !strings.Contains(store.Value, `"usage_total_tokens": responseBodyJSON("/usage/total_tokens")`) {
		t.Errorf("store value should contain usage_total_tokens field, got: %s", store.Value)
	}
	if !strings.Contains(store.Value, `"metadata_total_tokens": responseBodyJSON("/metadata/total_tokens")`) {
		t.Errorf("store value should contain metadata_total_tokens field, got: %s", store.Value)
	}

	grpc, ok := actions[1].(*GrpcAction)
	if !ok {
		t.Fatalf("actions[1] type = %s, want grpc", actions[1].ActionType())
	}
	if strings.Contains(grpc.MessageBuilder, "responseBodyJSON") {
		t.Error("grpc message should not contain responseBodyJSON after replacement")
	}
	if !strings.Contains(grpc.MessageBuilder, "kuadrant.internal.response.body.usage_total_tokens") {
		t.Errorf("grpc message should reference store path for usage_total_tokens, got:\n%s", grpc.MessageBuilder)
	}
	if !strings.Contains(grpc.MessageBuilder, "kuadrant.internal.response.body.metadata_total_tokens") {
		t.Errorf("grpc message should reference store path for metadata_total_tokens, got:\n%s", grpc.MessageBuilder)
	}
}

func TestBuildActions_SanitizedKeyCollision(t *testing.T) {
	// "/a" and "/_/a" both have leaf field name "a" (triggering the sanitized-pointer
	// fallback), and sanitizePointer itself maps both to "a" (folding "_" the same way
	// it folds "/"). Without disambiguation, one of the two extracted values would
	// silently share the other's store path and be lost.
	specs := []ActionSpec{
		{
			ServiceName: RateLimitReportServiceName,
			Scope:       "my-scope",
			Sources:     []string{"TokenRateLimitPolicy/default/my-trlp"},
			ConditionalData: []ConditionalData{{
				Data: []DataType{
					{Value: &Expression{ExpressionItem: ExpressionItem{
						Key:   "field1",
						Value: `responseBodyJSON("/a")`,
					}}},
					{Value: &Expression{ExpressionItem: ExpressionItem{
						Key:   "field2",
						Value: `responseBodyJSON("/_/a")`,
					}}},
				},
			}},
		},
	}
	actions := BuildActions(specs)

	if len(actions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(actions))
	}

	store, ok := actions[0].(*StoreAction)
	if !ok {
		t.Fatalf("actions[0] type = %s, want store", actions[0].ActionType())
	}

	// Sorted pointer order ("/_/a" < "/a") claims "a" first; "/a" must be
	// disambiguated to a distinct key rather than colliding on "a" too.
	if !strings.Contains(store.Value, `"a": responseBodyJSON("/_/a")`) {
		t.Errorf("store value should contain \"a\" mapped to /_/a, got: %s", store.Value)
	}
	if !strings.Contains(store.Value, `"a_2": responseBodyJSON("/a")`) {
		t.Errorf("store value should contain a disambiguated key for /a, got: %s", store.Value)
	}

	grpc, ok := actions[1].(*GrpcAction)
	if !ok {
		t.Fatalf("actions[1] type = %s, want grpc", actions[1].ActionType())
	}
	if strings.Contains(grpc.MessageBuilder, "responseBodyJSON") {
		t.Error("grpc message should not contain responseBodyJSON after replacement")
	}
	storePathPattern := regexp.MustCompile(`kuadrant\.internal\.response\.body\.[A-Za-z0-9_]+`)
	gotPaths := make(map[string]bool)
	for _, p := range storePathPattern.FindAllString(grpc.MessageBuilder, -1) {
		gotPaths[p] = true
	}
	wantPaths := map[string]bool{
		"kuadrant.internal.response.body.a":   true,
		"kuadrant.internal.response.body.a_2": true,
	}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Errorf("grpc message store paths = %v, want %v (message:\n%s)", gotPaths, wantPaths, grpc.MessageBuilder)
	}
}

// TestBuildActions_PunctuationInPointer guards against a regression where a JSON Pointer's
// last segment contains characters that are legal in RFC 6901 (e.g. "-") but unsafe in the
// generated CEL dot-access store path (e.g. "kuadrant.internal.response.body.total-tokens",
// where CEL parses "-" as subtraction rather than part of an identifier). Such a pointer is
// accepted by ResponseDataExtraction's CRD validation, so BuildActions must sanitize it.
func TestBuildActions_PunctuationInPointer(t *testing.T) {
	specs := []ActionSpec{
		{
			ServiceName: RateLimitCommitServiceName,
			Scope:       "my-scope",
			Sources:     []string{"TokenRateLimitPolicy/default/my-trlp"},
			Reservation: &ReservationSpec{
				ID:           "limit-a",
				ActualAmount: `responseBodyJSON(["/usage/total-tokens"], "number")`,
			},
		},
	}
	actions := BuildActions(specs)

	store, ok := actions[0].(*StoreAction)
	if !ok {
		t.Fatalf("actions[0] type = %s, want store", actions[0].ActionType())
	}
	if !strings.Contains(store.Value, `"total_tokens":`) {
		t.Errorf("store value should use a sanitized key, got: %s", store.Value)
	}

	grpc, ok := actions[1].(*GrpcAction)
	if !ok {
		t.Fatalf("actions[1] type = %s, want grpc", actions[1].ActionType())
	}
	expectedStorePath := responseBodyStorePath + ".total_tokens"
	if !strings.Contains(grpc.MessageBuilder, expectedStorePath) {
		t.Errorf("commit message should reference sanitized store path %q, got:\n%s", expectedStorePath, grpc.MessageBuilder)
	}
	if strings.Contains(grpc.MessageBuilder, "total-tokens") {
		t.Errorf("commit message must not contain the unsanitized punctuation-bearing key, got:\n%s", grpc.MessageBuilder)
	}
}

func TestAttachBindingsThenBuildActions_FullPipeline(t *testing.T) {
	specs := []ActionSpec{
		{
			ServiceName: AuthServiceName,
			Scope:       "my-scope",
			Sources:     []string{"AuthPolicy/default/my-ap"},
		},
		{
			ServiceName: RateLimitCheckServiceName,
			Scope:       "my-scope",
			Sources:     []string{"TokenRateLimitPolicy/default/my-trlp"},
			ConditionalData: []ConditionalData{{
				Data: []DataType{
					{Value: &Expression{ExpressionItem: ExpressionItem{Key: "ratelimit.hits_addend", Value: "0"}}},
				},
			}},
		},
		{
			ServiceName: RateLimitReportServiceName,
			Scope:       "my-scope",
			Sources:     []string{"TokenRateLimitPolicy/default/my-trlp"},
			ConditionalData: []ConditionalData{{
				Data: []DataType{
					{Value: &Expression{ExpressionItem: ExpressionItem{
						Key:   "ratelimit.hits_addend",
						Value: `responseBodyJSON("/usage/total_tokens")`,
					}}},
				},
			}},
		},
	}

	bindings := []DataBinding{
		{Domain: "metrics.labels", Field: "model", Expression: "responseBodyJSON('/model')"},
		{Domain: "metrics.labels", Field: "user", Expression: "auth.identity.userid"},
	}

	AttachBindings(specs, bindings)
	actions := BuildActions(specs)

	// Expect: store action + auth + check + report
	if len(actions) != 4 {
		t.Fatalf("expected 4 actions, got %d", len(actions))
	}

	// Store action should contain body refs from report spec's ConditionalData and bindings
	store, ok := actions[0].(*StoreAction)
	if !ok {
		t.Fatalf("actions[0] type = %s, want store", actions[0].ActionType())
	}
	if !strings.Contains(store.Value, `"model": responseBodyJSON('/model')`) {
		t.Errorf("store value should contain model field, got: %s", store.Value)
	}
	if !strings.Contains(store.Value, `"total_tokens": responseBodyJSON("/usage/total_tokens")`) {
		t.Errorf("store value should contain total_tokens field, got: %s", store.Value)
	}

	// Auth action: should have auth.identity.userid but NOT body ref bindings
	authGrpc, ok := actions[1].(*GrpcAction)
	if !ok {
		t.Fatalf("actions[1] type = %s, want grpc", actions[1].ActionType())
	}
	if strings.Contains(authGrpc.MessageBuilder, "responseBodyJSON") {
		t.Error("auth message should not contain responseBodyJSON")
	}
	if strings.Contains(authGrpc.MessageBuilder, "kuadrant.internal.response.body") {
		t.Error("auth message should not contain body store path")
	}
	if !strings.Contains(authGrpc.MessageBuilder, "auth.identity.userid") {
		t.Error("auth message should contain auth binding")
	}

	// Check action: should have auth.identity.userid but NOT body ref bindings
	checkGrpc, ok := actions[2].(*GrpcAction)
	if !ok {
		t.Fatalf("actions[2] type = %s, want grpc", actions[2].ActionType())
	}
	if strings.Contains(checkGrpc.MessageBuilder, "responseBodyJSON") {
		t.Error("check message should not contain responseBodyJSON")
	}
	if strings.Contains(checkGrpc.MessageBuilder, "kuadrant.internal.response.body") {
		t.Error("check message should not contain body store path")
	}
	if !strings.Contains(checkGrpc.MessageBuilder, "auth.identity.userid") {
		t.Error("check message should contain auth binding")
	}

	// Report action: should have both auth and body store path bindings
	reportGrpc, ok := actions[3].(*GrpcAction)
	if !ok {
		t.Fatalf("actions[3] type = %s, want grpc", actions[3].ActionType())
	}
	if strings.Contains(reportGrpc.MessageBuilder, "responseBodyJSON") {
		t.Error("report message should not contain responseBodyJSON after replacement")
	}
	if !strings.Contains(reportGrpc.MessageBuilder, "kuadrant.internal.response.body.total_tokens") {
		t.Errorf("report message should reference store path for total_tokens")
	}
	if !strings.Contains(reportGrpc.MessageBuilder, "kuadrant.internal.response.body.model") {
		t.Errorf("report message should reference store path for model")
	}
	if !strings.Contains(reportGrpc.MessageBuilder, "auth.identity.userid") {
		t.Error("report message should contain auth binding")
	}
}

func TestProducedStorePaths(t *testing.T) {
	tests := []struct {
		name        string
		serviceName string
		expected    []string
	}{
		{"auth", AuthServiceName, []string{AuthStorePath}},
		{"ratelimit", RateLimitServiceName, nil},
		{"ratelimit check", RateLimitCheckServiceName, nil},
		{"ratelimit report", RateLimitReportServiceName, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spec := ActionSpec{ServiceName: tc.serviceName}
			got := spec.ProducedStorePaths()
			if len(got) != len(tc.expected) {
				t.Fatalf("got %v, want %v", got, tc.expected)
			}
			for i := range got {
				if got[i] != tc.expected[i] {
					t.Errorf("got[%d] = %q, want %q", i, got[i], tc.expected[i])
				}
			}
		})
	}
}

func TestAttachBindings_PreAuthExcludesAuthBindings(t *testing.T) {
	specs := []ActionSpec{
		{ServiceName: RateLimitServiceName, Scope: "infra-rl"},
		{ServiceName: AuthServiceName, Scope: "my-auth"},
		{ServiceName: RateLimitCheckServiceName, Scope: "post-auth-rl"},
	}
	bindings := []DataBinding{
		{Field: "region", Expression: "request.headers['x-region']"},
		{Field: "userid", Expression: "auth.identity.userid"},
	}

	AttachBindings(specs, bindings)

	// Pre-auth RL: should only get the request binding, not auth
	if len(specs[0].Bindings) != 1 {
		t.Fatalf("pre-auth spec: expected 1 binding, got %d", len(specs[0].Bindings))
	}
	if specs[0].Bindings[0].Field != "region" {
		t.Errorf("pre-auth spec: expected region binding, got %q", specs[0].Bindings[0].Field)
	}

	// Auth: should get both bindings (auth wraps as cel_expr)
	if len(specs[1].Bindings) != 2 {
		t.Fatalf("auth spec: expected 2 bindings, got %d", len(specs[1].Bindings))
	}

	// Post-auth RL: should get both bindings
	if len(specs[2].Bindings) != 2 {
		t.Fatalf("post-auth spec: expected 2 bindings, got %d", len(specs[2].Bindings))
	}
}

func TestAttachBindings_NoAuth(t *testing.T) {
	specs := []ActionSpec{
		{ServiceName: RateLimitServiceName, Scope: "rl"},
	}
	bindings := []DataBinding{
		{Field: "region", Expression: "request.headers['x-region']"},
		{Field: "method", Expression: "request.method"},
	}

	AttachBindings(specs, bindings)

	if len(specs[0].Bindings) != 2 {
		t.Fatalf("expected 2 bindings, got %d", len(specs[0].Bindings))
	}
}

func TestAttachBindings_NoBindings(t *testing.T) {
	specs := []ActionSpec{
		{ServiceName: AuthServiceName, Scope: "my-auth"},
		{ServiceName: RateLimitServiceName, Scope: "my-rl"},
	}

	AttachBindings(specs, nil)

	for i, spec := range specs {
		if len(spec.Bindings) != 0 {
			t.Errorf("specs[%d]: expected 0 bindings, got %d", i, len(spec.Bindings))
		}
	}
}
