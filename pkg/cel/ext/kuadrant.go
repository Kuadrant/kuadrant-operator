package kuadrant

import (
	"math"

	v0 "github.com/kuadrant/kuadrant-operator/pkg/extension/grpc/v0"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

func CelExt(dag DAG) cel.EnvOption {
	l := &kuadrantLib{
		dag:     dag,
		version: math.MaxUint32,
		dev:     true,
	}
	return cel.Lib(l)
}

type DAG interface {
	FindGatewaysFor(name, group, kind string) (string, error)
}

type kuadrantLib struct {
	dag     DAG
	version uint32
	dev     bool
}

func (l kuadrantLib) CompileOptions() []cel.EnvOption {
	opts := []cel.EnvOption{}

	constVersion := "0"

	// eventually add functions for v1 here:
	if l.version >= 1 {
		constVersion = "1"
	}

	// only dev adds anything for now really
	if l.dev {
		opts = append(opts,
			cel.Function("findGateways",
				cel.MemberOverload("gateways_for_ngk",
					[]*cel.Type{
						cel.ObjectType("kuadrant.v0.NGK"),
					}, cel.StringType,
					cel.UnaryBinding(func(arg ref.Val) ref.Val {
						if ngk, err := refToProto[*v0.NGK](arg); err != nil {
							return types.NewErr(err.Error())
						} else {
							return stringOrError(l.dag.FindGatewaysFor(ngk.Name, ngk.Group, ngk.Kind))
						}
					})),
				//cel.MemberOverload("gateways_for_route", []*cel.Type{cel.StringType, cel.StringType, cel.IntType}, cel.IntType,
				//	cel.FunctionBinding(func(args ...ref.Val) ref.Val {
				//		name := args[0].(types.String)
				//		group := args[1].(types.String)
				//		kind := args[2].(types.String)
				//
				//		return l.dag.findGatewaysFor(string(name), string(group), string(kind)))
				//	})),
			),
		)
		constVersion = constVersion + "_dev"
	}

	opts = append(opts,
		cel.Constant("__KUADRANT_VERSION", cel.StringType, types.String(constVersion)),
	)

	opts = append(opts, cel.Container("kuadrant.v0"))
	opts = append(opts, cel.Types(&v0.NGK{}))
	opts = append(opts, cel.Variable("self", cel.ObjectType("kuadrant.v0.NGK")))

	return opts
}

func (l kuadrantLib) ProgramOptions() []cel.ProgramOption {
	return []cel.ProgramOption{}
}

// LibraryName implements the SingletonLibrary interface method.
func (kuadrantLib) LibraryName() string {
	return "kuadrant.cel.ext.kuadrant"
}
