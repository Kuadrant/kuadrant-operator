package kuadrant

import (
	"fmt"
	"strings"
	"testing"

	v0 "github.com/kuadrant/kuadrant-operator/pkg/extension/grpc/v0"

	"github.com/google/cel-go/cel"
)

var tests = []struct {
	expr string
	err  string
}{
	{expr: `__KUADRANT_VERSION == "1_dev"`},
	{expr: `self.findGateways() == "foo-bar-baz"`},
}

func TestKuadrantExt(t *testing.T) {
	env := testKuadrantEnv(t)
	for i, tst := range tests {
		tc := tst
		t.Run(fmt.Sprintf("[%d]", i), func(t *testing.T) {
			pAst, iss := env.Parse(tc.expr)
			if iss.Err() != nil {
				t.Fatalf("env.Parse(%v) failed: %v", tc.expr, iss.Err())
			}
			cAst, iss := env.Check(pAst)
			if iss.Err() != nil {
				t.Fatalf("env.Check(%v) failed: %v", tc.expr, iss.Err())
			}
			prg, err := env.Program(cAst)
			if err != nil {
				t.Fatal(err)
			}
			out, _, err := prg.Eval(map[string]any{
				"self": &v0.NGK{
					Name:  "foo",
					Group: "bar",
					Kind:  "baz",
				},
			})
			if tc.err != "" {
				if err == nil {
					t.Fatalf("got value %v, wanted error %s for expr: %s",
						out.Value(), tc.err, tc.expr)
				}
				if !strings.Contains(err.Error(), tc.err) {
					t.Errorf("got %q, expected error to contain %q for expr: %s", err, tc.err, tc.expr)
				}
			} else if err != nil {
				t.Fatal(err)
			} else if out.Value() != true {
				t.Errorf("got %v, wanted true for expr: %s", out.Value(), tc.expr)
			}
		})
	}
}

func testKuadrantEnv(t *testing.T, opts ...cel.EnvOption) *cel.Env {
	t.Helper()
	baseOpts := []cel.EnvOption{
		CelExt(&TestDAG{}),
	}
	env, err := cel.NewEnv(append(baseOpts, opts...)...)
	if err != nil {
		t.Fatalf("cel.NewEnv(CelExt()) failed: %v", err)
	}
	return env
}

type TestDAG struct {
}

func (d *TestDAG) FindGatewaysFor(name, group, kind string) (string, error) {
	return fmt.Sprintf("%s-%s-%s", name, group, kind), nil
}
