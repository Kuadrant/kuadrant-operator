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
	FindGatewaysFor([]*v0.TargetRef) ([]*v0.Gateway, error)
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
	registry := getRegistryWithTypes()

	if l.dev {
		opts = append(opts,
			cel.Function("findGateways",
				cel.MemberOverload("gateways_for_policy",
					[]*cel.Type{
						cel.ObjectType("kuadrant.v0.Policy"),
					}, cel.ListType(cel.ObjectType("kuadrant.v0.Gateway")),
					cel.UnaryBinding(func(arg ref.Val) ref.Val {
						policy, err := refToProto[*v0.Policy](arg)
						if err != nil {
							return types.NewErr("pbError: %w", err)
						}
						gws, err := l.dag.FindGatewaysFor(policy.TargetRefs)
						if err != nil {
							return types.NewErr("cel-kuadrant(gateways_for_policy): %w", err)
						}
						list := make([]ref.Val, 0, len(gws))
						for _, gw := range gws {
							gateway :=
								registry.NativeToValue(gw)
							list = append(list, gateway)
						}
						return registry.NativeToValue(list)
					})),
				cel.MemberOverload("gateways_for_target",
					[]*cel.Type{
						cel.ObjectType("kuadrant.v0.TargetRef"),
					}, cel.ListType(cel.ObjectType("kuadrant.v0.Gateway")),
					cel.UnaryBinding(func(arg ref.Val) ref.Val {
						target, err := refToProto[*v0.TargetRef](arg)
						if err != nil {
							return types.NewErr("pbError: %w", err)
						}
						gws, err := l.dag.FindGatewaysFor([]*v0.TargetRef{target})
						if err != nil {
							return types.NewErr("cel-kuadrant(gateways_for_target): %w", err)
						}
						list := make([]ref.Val, 0, len(gws))
						for _, gw := range gws {
							gateway :=
								registry.NativeToValue(gw)
							list = append(list, gateway)
						}
						return registry.NativeToValue(list)
					})),
			),
		)
		constVersion = constVersion + "_dev"
	}

	opts = append(opts,
		cel.Constant("__KUADRANT_VERSION", cel.StringType, types.String(constVersion)),
	)

	opts = append(opts, cel.CustomTypeAdapter(registry))
	opts = append(opts, cel.CustomTypeProvider(registry))
	opts = append(opts, cel.Variable("self", cel.ObjectType("kuadrant.v0.Policy")))

	return opts
}

func (l kuadrantLib) ProgramOptions() []cel.ProgramOption {
	return []cel.ProgramOption{}
}

// LibraryName implements the SingletonLibrary interface method.
func (kuadrantLib) LibraryName() string {
	return "kuadrant.cel.ext.kuadrant"
}

func getRegistryWithTypes() *types.Registry {
	registry, _ := types.NewRegistry(
		// common.proto
		&v0.Metadata{},
		&v0.TargetRef{},
		&v0.Condition{},
		&v0.ConditionStatus{},

		// gateway_api.proto
		&v0.Gateway{},
		&v0.GatewaySpec{},
		&v0.Listener{},
		&v0.GatewayAddresses{},
		&v0.GatewayStatus{},
		&v0.ListenerStatus{},
		&v0.GatewayClass{},
		&v0.GatewayClassSpec{},
		&v0.GatewayClassStatus{},

		// policy.proto
		&v0.Policy{},
		&v0.PolicyStatus{},

		// kuadrant.proto
		&v0.PingRequest{},
		&v0.PongResponse{},
		&v0.ResolveRequest{},
		&v0.ResolveResponse{},
		&v0.Event{},
	)
	return registry
}
