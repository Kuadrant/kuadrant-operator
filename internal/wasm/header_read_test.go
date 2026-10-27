//go:build unit

package wasm

import (
	"strings"
	"testing"

	"github.com/google/cel-go/cel"
)

func TestBareHeaderRead(t *testing.T) {
	tests := []struct {
		name      string
		expr      string
		wantName  string
		wantIndex bool
		wantOK    bool
	}{
		{name: "select form", expr: "request.headers.xtenant", wantName: "xtenant", wantOK: true},
		{name: "select form with underscore", expr: "request.headers.x_tenant", wantName: "x_tenant", wantOK: true},
		{name: "index form double quoted", expr: `request.headers["x-tenant"]`, wantName: "x-tenant", wantIndex: true, wantOK: true},
		{name: "index form single quoted", expr: `request.headers['x-tenant']`, wantName: "x-tenant", wantIndex: true, wantOK: true},
		{name: "index form with surrounding whitespace", expr: `  request.headers["x-tenant"]  `, wantName: "x-tenant", wantIndex: true, wantOK: true},
		{name: "headers by index then select", expr: `request["headers"].xtenant`, wantName: "xtenant", wantOK: true},
		{name: "headers by index then index", expr: `request["headers"]["x-tenant"]`, wantName: "x-tenant", wantIndex: true, wantOK: true},
		{name: "name with quote and backslash", expr: `request.headers["a\"b\\c"]`, wantName: `a"b\c`, wantIndex: true, wantOK: true},

		{name: "plain attribute", expr: "request.path"},
		{name: "compound", expr: `request.headers["x-tenant"] + "-" + request.path`},
		{name: "wrapped read", expr: "string(request.headers.xtenant)"},
		{name: "presence test", expr: "has(request.headers.xtenant)"},
		{name: "already guarded select", expr: `has(request.headers.xtenant) ? string(request.headers.xtenant) : ""`},
		{name: "already guarded index", expr: `"x-tenant" in request.headers ? string(request.headers["x-tenant"]) : ""`},
		{name: "auth attribute", expr: "auth.identity.user.username"},
		{name: "non-literal index", expr: "request.headers[request.path]"},
		{name: "headers map", expr: "request.headers"},
		{name: "member call on header", expr: "request.headers.xtenant.size()"},
		{name: "nested select on header", expr: "request.headers.xtenant.sub"},
		{name: "other map", expr: "metadata.headers.xtenant"},
		{name: "string literal", expr: `"static"`},
		{name: "does not parse", expr: "request."},
		{name: "empty", expr: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, index, ok := bareHeaderRead(tt.expr)
			if ok != tt.wantOK {
				t.Fatalf("bareHeaderRead(%q) ok = %v, want %v", tt.expr, ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if name != tt.wantName || index != tt.wantIndex {
				t.Errorf("bareHeaderRead(%q) = (%q, index=%v), want (%q, index=%v)", tt.expr, name, index, tt.wantName, tt.wantIndex)
			}
		})
	}
}

func TestGuardedHeaderReadCEL(t *testing.T) {
	tests := []struct {
		name   string
		header string
		index  bool
		want   string
	}{
		{name: "select form", header: "xtenant", want: `has(request.headers.xtenant) ? string(request.headers.xtenant) : ""`},
		{name: "index form", header: "x-tenant", index: true, want: `"x-tenant" in request.headers ? string(request.headers["x-tenant"]) : ""`},
		{name: "index form quotes the name", header: `x"); evil(`, index: true, want: `"x\"); evil(" in request.headers ? string(request.headers["x\"); evil("]) : ""`},
		{name: "index form escapes a backslash", header: `a\b`, index: true, want: `"a\\b" in request.headers ? string(request.headers["a\\b"]) : ""`},
	}

	env := celTestEnv(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := guardedHeaderReadCEL(tt.header, tt.index)
			if got != tt.want {
				t.Fatalf("guardedHeaderReadCEL(%q, %v) =\n%s\nwant\n%s", tt.header, tt.index, got, tt.want)
			}
			// The rendered guard must stay a single, valid CEL expression whatever the name holds.
			if _, iss := env.Compile(got); iss.Err() != nil {
				t.Fatalf("guarded read %q does not compile: %v", got, iss.Err())
			}
		})
	}
}

func TestMetadataFieldCEL_GuardHeaderRead(t *testing.T) {
	tests := []struct {
		name  string
		field MetadataFieldCEL
		want  string
	}{
		{
			name:  "guarded select form",
			field: MetadataFieldCEL{Key: "tenant", Expression: "request.headers.xtenant", GuardHeaderRead: true},
			want:  `google.protobuf.Value{string_value: has(request.headers.xtenant) ? string(request.headers.xtenant) : ""}`,
		},
		{
			name:  "guarded index form",
			field: MetadataFieldCEL{Key: "tenant", Expression: `request.headers["x-tenant"]`, GuardHeaderRead: true},
			want:  `google.protobuf.Value{string_value: "x-tenant" in request.headers ? string(request.headers["x-tenant"]) : ""}`,
		},
		{
			name:  "guarded index form normalises single quotes",
			field: MetadataFieldCEL{Key: "tenant", Expression: `request.headers['x-tenant']`, GuardHeaderRead: true},
			want:  `google.protobuf.Value{string_value: "x-tenant" in request.headers ? string(request.headers["x-tenant"]) : ""}`,
		},
		{
			name:  "plain attribute untouched",
			field: MetadataFieldCEL{Key: "path", Expression: "request.path", GuardHeaderRead: true},
			want:  `google.protobuf.Value{string_value: string(request.path)}`,
		},
		{
			name:  "compound expression untouched",
			field: MetadataFieldCEL{Key: "tenant", Expression: `request.headers["x-tenant"] + "-" + request.path`, GuardHeaderRead: true},
			want:  `google.protobuf.Value{string_value: string(request.headers["x-tenant"] + "-" + request.path)}`,
		},
		{
			name:  "already guarded expression untouched",
			field: MetadataFieldCEL{Key: "tenant", Expression: `has(request.headers.xtenant) ? string(request.headers.xtenant) : ""`, GuardHeaderRead: true},
			want:  `google.protobuf.Value{string_value: string(has(request.headers.xtenant) ? string(request.headers.xtenant) : "")}`,
		},
		{
			name:  "auth expression still deferred",
			field: MetadataFieldCEL{Key: "user", Expression: "auth.identity.user.username", GuardHeaderRead: true},
			want:  `google.protobuf.Value{struct_value: google.protobuf.Struct{fields: {"cel_expr": google.protobuf.Value{string_value: "auth.identity.user.username"}}}}`,
		},
		{
			name:  "header read without the flag untouched",
			field: MetadataFieldCEL{Key: "tenant", Expression: "request.headers.xtenant"},
			want:  `google.protobuf.Value{string_value: string(request.headers.xtenant)}`,
		},
	}

	env := celTestEnv(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.field.valueCEL()
			if got != tt.want {
				t.Fatalf("valueCEL() =\n%s\nwant\n%s", got, tt.want)
			}
			if _, iss := env.Compile(got); iss.Err() != nil {
				t.Fatalf("rendered value %q does not compile: %v", got, iss.Err())
			}
		})
	}
}

// evalString compiles and evaluates expr against the given request and returns the string result.
func evalString(t *testing.T, env *cel.Env, expr string, request map[string]any) (string, error) {
	t.Helper()
	ast, iss := env.Compile(expr)
	if iss.Err() != nil {
		t.Fatalf("expression %q failed to compile: %v", expr, iss.Err())
	}
	prg, err := env.Program(ast)
	if err != nil {
		t.Fatalf("expression %q failed to build program: %v", expr, err)
	}
	out, _, err := prg.Eval(map[string]any{"request": request})
	if err != nil {
		return "", err
	}
	s, ok := out.Value().(string)
	if !ok {
		t.Fatalf("expression %q did not evaluate to a string: %v (%T)", expr, out.Value(), out.Value())
	}
	return s, nil
}

func TestGuardedHeaderRead_EvaluatesWithoutError(t *testing.T) {
	env := celTestEnv(t)
	present := map[string]any{"headers": map[string]any{"xtenant": "acme", "x-tenant": "acme"}, "path": "/v1/models"}
	absent := map[string]any{"headers": map[string]any{"host": "example.com"}, "path": "/v1/models"}

	for _, tt := range []struct {
		name string
		expr string
	}{
		{name: "select form", expr: guardedHeaderReadCEL("xtenant", false)},
		{name: "index form", expr: guardedHeaderReadCEL("x-tenant", true)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := evalString(t, env, tt.expr, present)
			if err != nil || got != "acme" {
				t.Errorf("with header present: got (%q, %v), want (\"acme\", nil)", got, err)
			}
			got, err = evalString(t, env, tt.expr, absent)
			if err != nil || got != "" {
				t.Errorf("with header absent: got (%q, %v), want (\"\", nil)", got, err)
			}
		})
	}

	// The unguarded rendering is what fails the whole message build when the header is absent.
	for _, expr := range []string{"string(request.headers.xtenant)", `string(request.headers["x-tenant"])`} {
		if _, err := evalString(t, env, expr, absent); err == nil {
			t.Errorf("unguarded %q evaluated without error against a request lacking the header", expr)
		}
	}
}

func TestBuildMetadataContext_GuardsLoggingFieldsAndMetricLabels(t *testing.T) {
	bindings := []DataBinding{
		{Domain: "metrics.labels", Field: "user", Expression: "auth.identity.userid"},
		{Domain: "metrics.labels", Field: "tenant", Expression: `request.headers["x-tenant"]`},
		{Domain: "logging.fields", Field: "request_path", Expression: "request.path"},
		{Domain: "logging.fields", Field: "tenant_index", Expression: `request.headers["x-tenant"]`},
		{Domain: "logging.fields", Field: "client_identity", Expression: "auth.identity.user.username"},
		{Domain: "logging.fields", Field: "tenant_select", Expression: "request.headers.xtenant"},
		{Domain: "", Field: "region", Expression: "request.headers['x-region']"},
	}

	metadata := buildMetadataContext(bindings)

	// Domains sort alphabetically and fields by key then expression, whether guarded or not.
	wantDomains := []string{"io.kuadrant", "io.kuadrant.logging.fields", "io.kuadrant.metrics.labels"}
	gotDomains := make([]string, len(metadata.FilterMetadata))
	byDomain := make(map[string]FilterMetadataEntryCEL, len(metadata.FilterMetadata))
	for i, e := range metadata.FilterMetadata {
		gotDomains[i] = e.Domain
		byDomain[e.Domain] = e
	}
	if strings.Join(gotDomains, ",") != strings.Join(wantDomains, ",") {
		t.Fatalf("domain order = %v, want %v", gotDomains, wantDomains)
	}

	assertFields := func(domain string, wantKeys []string, wantGuard bool) FilterMetadataEntryCEL {
		t.Helper()
		entry := byDomain[domain]
		gotKeys := make([]string, len(entry.Fields))
		for i, f := range entry.Fields {
			gotKeys[i] = f.Key
			if f.GuardHeaderRead != wantGuard {
				t.Errorf("%s field %q has GuardHeaderRead=%v, want %v", domain, f.Key, f.GuardHeaderRead, wantGuard)
			}
		}
		if strings.Join(gotKeys, ",") != strings.Join(wantKeys, ",") {
			t.Errorf("%s field order = %v, want %v", domain, gotKeys, wantKeys)
		}
		return entry
	}
	logging := assertFields("io.kuadrant.logging.fields", []string{"client_identity", "request_path", "tenant_index", "tenant_select"}, true)
	metrics := assertFields("io.kuadrant.metrics.labels", []string{"tenant", "user"}, true)
	// The empty-domain io.kuadrant bindings keep today's rendering, which is what
	// TestActionSpecBuild_AuthWithBindings pins.
	custom := assertFields("io.kuadrant", []string{"region"}, false)

	assertRendered := func(entry FilterMetadataEntryCEL, wants ...string) {
		t.Helper()
		rendered := entry.ToCEL()
		for _, want := range wants {
			if !strings.Contains(rendered, want) {
				t.Errorf("rendered %s lacks %q:\n%s", entry.Domain, want, rendered)
			}
		}
	}
	assertRendered(logging,
		`"request_path": google.protobuf.Value{string_value: string(request.path)}`,
		`"tenant_index": google.protobuf.Value{string_value: "x-tenant" in request.headers ? string(request.headers["x-tenant"]) : ""}`,
		`"tenant_select": google.protobuf.Value{string_value: has(request.headers.xtenant) ? string(request.headers.xtenant) : ""}`,
		`"cel_expr": google.protobuf.Value{string_value: "auth.identity.user.username"}`,
	)
	assertRendered(metrics,
		`"tenant": google.protobuf.Value{string_value: "x-tenant" in request.headers ? string(request.headers["x-tenant"]) : ""}`,
		`"user": google.protobuf.Value{struct_value: google.protobuf.Struct{fields: {"cel_expr": google.protobuf.Value{string_value: "auth.identity.userid"}}}}`,
	)
	assertRendered(custom,
		`"region": google.protobuf.Value{string_value: string(request.headers['x-region'])}`,
	)
}

// TestBindingsToDescriptor_BareHeaderReadStaysUnguarded pins a known gap rather than blessing it.
// The rate-limit descriptor is a second rendering site for metric labels that this change leaves
// alone, so a bare header read there still fails the rate-limit action when the header is absent
// (failure mode allow: the request proceeds without rate limiting and without the label). Update
// this test when the guard is extended to descriptors or the shim evaluates fields individually.
func TestBindingsToDescriptor_BareHeaderReadStaysUnguarded(t *testing.T) {
	descriptor := bindingsToDescriptor([]DataBinding{
		{Domain: "metrics.labels", Field: "tenant", Expression: "request.headers.xtenant"},
	})
	if descriptor == nil || len(descriptor.Entries) != 1 {
		t.Fatalf("descriptor = %#v, want one entry", descriptor)
	}
	entry := descriptor.Entries[0]
	if entry.Key != "tenant" {
		t.Errorf("entry key = %q, want %q", entry.Key, "tenant")
	}
	if want := "string(request.headers.xtenant)"; entry.ValueCEL != want {
		t.Errorf("entry value = %q, want the unguarded %q", entry.ValueCEL, want)
	}
}
