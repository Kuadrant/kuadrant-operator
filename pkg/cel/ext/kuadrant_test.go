package kuadrant

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/kuadrant/policy-machinery/machinery"

	v1 "github.com/kuadrant/kuadrant-operator/pkg/extension/grpc/v1"
)

var tests = []struct {
	expr string
	err  string
}{
	{expr: `__KUADRANT_VERSION == "1_dev"`},
	{expr: `self.findGateways().size() == 1`},
	{expr: `self.findGateways()[0].metadata.name == "kuadrant-gw"`},
	{expr: `self.findGateways()[0].spec.listeners.size() == 1`},
	{expr: `self.findGateways()[0].spec.listeners[0].hostname == "kuadrant.io"`},
	{expr: `self.findGateways()[0].metadata.name == self.targetRefs[0].findGateways()[0].metadata.name`},
	{expr: `self.findAuthPolicies().size() == 1`},
	{expr: `self.findAuthPolicies()[0].metadata.name == "kuadrant-auth"`},
	{expr: `self.findAuthPolicies()[0].metadata.namespace == "some-ns"`},
	{expr: `self.findAuthPolicies()[0].targetRefs[0].kind == "Gateway"`},
	{expr: `self.findAuthPolicies()[0].targetRefs[0].group == "gateway.networking.k8s.io"`},
	{expr: `self.findAuthPolicies()[0].targetRefs[0].name == "kuadrant-gw"`},
	{expr: `self.findAuthPolicies()[0].targetRefs[0].namespace == "some-ns"`},
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
				"self": &v1.Policy{
					TargetRefs: []*v1.TargetRef{
						{
							Name:  "foo",
							Group: "bar",
							Kind:  "baz",
						},
					},
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
	opts = append(baseOpts, opts...)
	env, err := cel.NewEnv(opts...)
	if err != nil {
		t.Fatalf("cel.NewEnv(CelExt()) failed: %v", err)
	}
	return env
}

type TestDAG struct {
}

func (d *TestDAG) FindGatewaysFor(targets []*v1.TargetRef) ([]*v1.Gateway, error) {
	if targets[0].Name == "foo" && targets[0].Group == "bar" && targets[0].Kind == "baz" {
		return []*v1.Gateway{
			{
				Metadata: &v1.Metadata{
					Name:      "kuadrant-gw",
					Namespace: "some-ns",
				},
				Spec: &v1.GatewaySpec{
					Listeners: []*v1.Listener{
						{
							Hostname: "kuadrant.io",
						},
					},
				},
			},
		}, nil
	}
	return nil, nil
}

func (d *TestDAG) FindPoliciesFor(targets []*v1.TargetRef, _ machinery.Policy) ([]*v1.Policy, error) {
	if targets[0].Name == "foo" && targets[0].Group == "bar" && targets[0].Kind == "baz" {
		return []*v1.Policy{
			{
				Metadata: &v1.Metadata{
					Name:      "kuadrant-auth",
					Namespace: "some-ns",
				},
				TargetRefs: []*v1.TargetRef{
					{
						Group:     "gateway.networking.k8s.io",
						Kind:      "Gateway",
						Name:      "kuadrant-gw",
						Namespace: "some-ns",
					},
				},
			},
		}, nil
	}
	return nil, nil
}
