//go:build unit

package wasm

import (
	"net/url"
	"strings"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/ext"
	"gotest.tools/assert"
	"k8s.io/utils/ptr"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// celTestEnv builds a minimal CEL environment mirroring the subset of the wasm-shim runtime
// environment (request.headers/url_path/method/query, queryMap()) that predicateFrom* targets,
// so that generated predicates can actually be compiled and evaluated in these tests instead of
// only compared as strings. This is what lets the "injection" test cases below prove there is no
// bypass, rather than merely asserting on quoting style.
//
// queryMap percent-decodes keys/values (like a real query-string parser would) so that a raw
// query built from url.QueryEscape'd test values round-trips correctly, including when the
// decoded value itself contains "&" or "=".
func celTestEnv(t *testing.T) *cel.Env {
	t.Helper()
	env, err := cel.NewEnv(
		cel.Variable("request", cel.DynType),
		ext.Strings(),
		cel.Function("queryMap",
			cel.Overload("queryMap_string", []*cel.Type{cel.StringType}, cel.MapType(cel.StringType, cel.StringType),
				cel.UnaryBinding(func(val ref.Val) ref.Val {
					m := map[string]string{}
					for _, pair := range strings.Split(string(val.(types.String)), "&") {
						if pair == "" {
							continue
						}
						k, v, _ := strings.Cut(pair, "=")
						if dk, err := url.QueryUnescape(k); err == nil {
							k = dk
						}
						if dv, err := url.QueryUnescape(v); err == nil {
							v = dv
						}
						m[k] = v
					}
					return types.DefaultTypeAdapter.NativeToValue(m)
				}),
			),
		),
	)
	if err != nil {
		t.Fatalf("failed to build CEL test environment: %v", err)
	}
	return env
}

// evalPredicate compiles and evaluates a generated predicate against a simulated `request`
// object. Compile/eval failures fail the test outright (rather than being treated as "no
// match"), since a broken predicate string is exactly the failure mode this fix targets.
func evalPredicate(t *testing.T, env *cel.Env, predicate string, request map[string]any) bool {
	t.Helper()

	ast, iss := env.Compile(predicate)
	if iss.Err() != nil {
		t.Fatalf("predicate %q failed to compile: %v", predicate, iss.Err())
	}

	prg, err := env.Program(ast)
	if err != nil {
		t.Fatalf("predicate %q failed to build program: %v", predicate, err)
	}

	out, _, err := prg.Eval(map[string]any{"request": request})
	if err != nil {
		t.Fatalf("predicate %q failed to evaluate: %v", predicate, err)
	}

	b, ok := out.Value().(bool)
	if !ok {
		t.Fatalf("predicate %q did not evaluate to a bool: %v (%T)", predicate, out.Value(), out.Value())
	}
	return b
}

// assertCompiles proves the predicate is still a single, syntactically valid CEL expression —
// the core injection-safety property — without evaluating it. This matters for regex-context
// predicates (matches()): CEL only compiles the regex lazily inside Eval(), not at parse/check
// time, so a value that happens to be an invalid *regex* (e.g. containing unbalanced parens)
// will fail at Eval() for an unrelated, non-security reason even once CEL-injection is fixed.
// That failure mode (a contained, per-request regex error) is not a bypass; what must hold is
// that the attacker's value never gets interpreted as CEL syntax in the first place.
func assertCompiles(t *testing.T, env *cel.Env, predicate string) {
	t.Helper()
	if _, iss := env.Compile(predicate); iss.Err() != nil {
		t.Fatalf("predicate %q failed to compile: %v", predicate, iss.Err())
	}
}

// injectionPayload is the payload from the reported PoC: a header/path/query match value
// containing a single quote that, without escaping, closes the CEL string literal early, letting
// the attacker inject a boolean-AND-false clause and reopen a new string literal afterwards so
// the overall expression still parses, forcing the gating predicate to always be false.
const injectionPayload = `v') && false && ('' == '`

func TestPredicateFromHeader_SafeAgainstInjection(t *testing.T) {
	env := celTestEnv(t)

	testCases := []struct {
		name        string
		value       string
		headerValue string
		expectMatch bool
	}{
		{"legitimate value matches the same header value", "kuadrant", "kuadrant", true},
		{"legitimate value does not match a different header value", "kuadrant", "other", false},
		{"injection payload does not bypass the gate for an unrelated request", injectionPayload, "safe-value", false},
		{"injection payload behaves as an inert literal, matching only itself", injectionPayload, injectionPayload, true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			predicate := predicateFromHeader(gatewayapiv1.HTTPHeaderMatch{Name: "x-poc", Value: tc.value})
			match := evalPredicate(t, env, predicate, map[string]any{
				"headers": map[string]any{"x-poc": tc.headerValue},
			})
			assert.Equal(t, tc.expectMatch, match)
		})
	}
}

func TestPredicateFromGRPCHeader_SafeAgainstInjection(t *testing.T) {
	env := celTestEnv(t)

	testCases := []struct {
		name        string
		value       string
		headerValue string
		expectMatch bool
	}{
		{"legitimate value matches the same header value", "acme", "acme", true},
		{"legitimate value does not match a different header value", "acme", "other", false},
		{"injection payload does not bypass the gate for an unrelated request", injectionPayload, "safe-value", false},
		{"injection payload behaves as an inert literal, matching only itself", injectionPayload, injectionPayload, true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			predicate := predicateFromGRPCHeader(gatewayapiv1.GRPCHeaderMatch{Name: "x-tenant", Value: tc.value})
			match := evalPredicate(t, env, predicate, map[string]any{
				"headers": map[string]any{"x-tenant": tc.headerValue},
			})
			assert.Equal(t, tc.expectMatch, match)
		})
	}
}

func TestPredicateFromPathMatch_SafeAgainstInjection(t *testing.T) {
	env := celTestEnv(t)

	testCases := []struct {
		name        string
		matchType   gatewayapiv1.PathMatchType
		value       string
		urlPath     string
		expectMatch bool
	}{
		// Exact
		{"exact: legitimate path matches", gatewayapiv1.PathMatchExact, "/admin", "/admin", true},
		{"exact: legitimate path does not match a different path", gatewayapiv1.PathMatchExact, "/admin", "/other", false},
		{"exact: injection payload does not bypass for a normal request", gatewayapiv1.PathMatchExact, injectionPayload, "/app", false},
		{"exact: injection payload behaves as an inert literal", gatewayapiv1.PathMatchExact, injectionPayload, injectionPayload, true},
		// PathPrefix
		{"prefix: legitimate prefix matches", gatewayapiv1.PathMatchPathPrefix, "/admin", "/admin/users", true},
		{"prefix: legitimate prefix does not match unrelated path", gatewayapiv1.PathMatchPathPrefix, "/admin", "/public", false},
		{"prefix: injection payload does not bypass for a normal request", gatewayapiv1.PathMatchPathPrefix, injectionPayload, "/app", false},
		// RegularExpression
		{"regex: legitimate pattern matches", gatewayapiv1.PathMatchRegularExpression, "^/admin/[0-9]+$", "/admin/42", true},
		{"regex: legitimate pattern does not match unrelated path", gatewayapiv1.PathMatchRegularExpression, "^/admin/[0-9]+$", "/admin/x", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			value := tc.value
			predicate := predicateFromPathMatch(gatewayapiv1.HTTPPathMatch{Type: &tc.matchType, Value: &value})
			match := evalPredicate(t, env, predicate, map[string]any{"url_path": tc.urlPath})
			assert.Equal(t, tc.expectMatch, match)
		})
	}

	// The injection payload is not valid RE2 syntax (unbalanced parens), so for
	// PathMatchRegularExpression we can only assert the CEL syntax itself wasn't broken; see
	// assertCompiles' doc comment for why evaluating it further isn't a meaningful check here.
	t.Run("regex: injection payload does not break CEL syntax", func(t *testing.T) {
		regexType := gatewayapiv1.PathMatchRegularExpression
		value := injectionPayload
		predicate := predicateFromPathMatch(gatewayapiv1.HTTPPathMatch{Type: &regexType, Value: &value})
		assertCompiles(t, env, predicate)
	})
}

func TestPredicateFromQueryParam_SafeAgainstInjection(t *testing.T) {
	env := celTestEnv(t)

	testCases := []struct {
		name        string
		name_       string
		value       string
		rawQuery    string
		expectMatch bool
	}{
		{"legitimate name/value matches", "foo", "bar", "foo=bar", true},
		{"legitimate name/value does not match a different value", "foo", "bar", "foo=baz", false},
		{"injection payload in value does not bypass for a normal request", "foo", injectionPayload, "foo=bar", false},
		{"injection payload in value behaves as an inert literal", "foo", injectionPayload, "foo=" + url.QueryEscape(injectionPayload), true},
		{"injection payload in name does not bypass for a normal request", injectionPayload, "bar", "foo=bar", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			predicate := predicateFromQueryParam(gatewayapiv1.HTTPQueryParamMatch{
				Name:  gatewayapiv1.HTTPHeaderName(tc.name_),
				Value: tc.value,
			})
			match := evalPredicate(t, env, predicate, map[string]any{"query": tc.rawQuery})
			assert.Equal(t, tc.expectMatch, match)
		})
	}
}

func TestPredicateFromGRPCMethod_SafeAgainstInjection(t *testing.T) {
	env := celTestEnv(t)

	testCases := []struct {
		name        string
		methodMatch gatewayapiv1.GRPCMethodMatch
		urlPath     string
		expectMatch bool
	}{
		{
			name: "exact service+method: legitimate path matches",
			methodMatch: gatewayapiv1.GRPCMethodMatch{
				Type: ptr.To(gatewayapiv1.GRPCMethodMatchExact), Service: ptr.To("com.example.UserService"), Method: ptr.To("GetUser"),
			},
			urlPath:     "/com.example.UserService/GetUser",
			expectMatch: true,
		},
		{
			name: "exact service+method: does not match a different path",
			methodMatch: gatewayapiv1.GRPCMethodMatch{
				Type: ptr.To(gatewayapiv1.GRPCMethodMatchExact), Service: ptr.To("com.example.UserService"), Method: ptr.To("GetUser"),
			},
			urlPath:     "/com.example.UserService/DeleteUser",
			expectMatch: false,
		},
		{
			// RegularExpression is the only GRPCMethodMatch type without a CRD-enforced safe
			// character set (see XValidation on Exact in gateway-api's grpcroute_types.go), so
			// it's the branch that actually needs the injection-safety check.
			name: "regex service+method: legitimate pattern matches",
			methodMatch: gatewayapiv1.GRPCMethodMatch{
				Type: ptr.To(gatewayapiv1.GRPCMethodMatchRegularExpression), Service: ptr.To(`com\.example\..*Service`), Method: ptr.To("Get.*"),
			},
			urlPath:     "/com.example.UserService/GetUser",
			expectMatch: true,
		},
		{
			name: "regex service+method: does not match an unrelated path",
			methodMatch: gatewayapiv1.GRPCMethodMatch{
				Type: ptr.To(gatewayapiv1.GRPCMethodMatchRegularExpression), Service: ptr.To(`com\.example\..*Service`), Method: ptr.To("Get.*"),
			},
			urlPath:     "/other.Service/GetUser",
			expectMatch: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			predicate := predicateFromGRPCMethod(tc.methodMatch)
			match := evalPredicate(t, env, predicate, map[string]any{"url_path": tc.urlPath})
			assert.Equal(t, tc.expectMatch, match)
		})
	}

	// Same rationale as the PathMatchRegularExpression case above: the payload isn't valid RE2
	// syntax, so only CEL-syntax safety is checked here, not evaluation.
	t.Run("regex method only: injection payload does not break CEL syntax", func(t *testing.T) {
		predicate := predicateFromGRPCMethod(gatewayapiv1.GRPCMethodMatch{
			Type: ptr.To(gatewayapiv1.GRPCMethodMatchRegularExpression), Method: ptr.To(injectionPayload),
		})
		assertCompiles(t, env, predicate)
	})
}
