package ext

import (
	"math"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/kuadrant/policy-machinery/machinery"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	v1 "github.com/kuadrant/kuadrant-operator/pkg/extension/grpc/v1"
)

// CelExt returns a cel.EnvOption that installs the Kuadrant CEL library.
//
// The returned library contributes:
//   - Member functions on kuadrant.v1.Policy and kuadrant.v1.TargetRef:
//     self.findGateways() / targetRef.findGateways() -> []*kuadrant.v1.Gateway
//     self.findAuthPolicies() -> []*kuadrant.v1.Policy (AuthPolicy subset)
//   - A constant __KUADRANT_VERSION describing the feature level (e.g. "1_dev").
//
// The provided DAG implementation is invoked to satisfy these functions. Only
// the small surface in the DAG interface is required, allowing callers to
// supply lightweight test doubles.
func CelExt(dag DAG) cel.EnvOption {
	l := &kuadrantLib{
		dag:     dag,
		version: math.MaxUint32,
		dev:     true,
	}
	return cel.Lib(l)
}

// DAG defines the minimal graph queries required by the CEL extension. It is
// intentionally small so that extensions and tests can easily supply an
// implementation without depending on internal Kuadrant graph types.
type DAG interface {
	// FindGatewaysFor returns all Gateways that match the provided target
	// references.
	FindGatewaysFor([]*v1.TargetRef) ([]*v1.Gateway, error)
	// FindPoliciesFor returns policies of the requested type matching the
	// target references.
	FindPoliciesFor([]*v1.TargetRef, machinery.Policy) ([]*v1.Policy, error)
}

// kuadrantLib implements cel.Library / cel.SingletonLibrary adding Kuadrant
// specific functions, constants and type adapters.
type kuadrantLib struct {
	dag     DAG
	version uint32
	dev     bool
}

// CompileOptions implements cel.Library. It wires the function overloads and
// constant version symbol plus custom type adapter/provider into the CEL env.
func (l kuadrantLib) CompileOptions() []cel.EnvOption {
	opts := []cel.EnvOption{
		cel.OptionalTypes(),
	}

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
						cel.ObjectType("kuadrant.v1.Policy"),
					}, cel.ListType(cel.ObjectType("kuadrant.v1.Gateway")),
					cel.UnaryBinding(func(arg ref.Val) ref.Val {
						policy, err := refToProto[*v1.Policy](arg)
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
						cel.ObjectType("kuadrant.v1.TargetRef"),
					}, cel.ListType(cel.ObjectType("kuadrant.v1.Gateway")),
					cel.UnaryBinding(func(arg ref.Val) ref.Val {
						target, err := refToProto[*v1.TargetRef](arg)
						if err != nil {
							return types.NewErr("pbError: %w", err)
						}
						gws, err := l.dag.FindGatewaysFor([]*v1.TargetRef{target})
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
			cel.Function("findAuthPolicies",
				cel.MemberOverload("authpolicies_for_policy",
					[]*cel.Type{
						cel.ObjectType("kuadrant.v1.Policy"),
					}, cel.ListType(cel.ObjectType("kuadrant.v1.Policy")),
					cel.UnaryBinding(func(arg ref.Val) ref.Val {
						policy, err := refToProto[*v1.Policy](arg)
						if err != nil {
							return types.NewErr("pbError: %w", err)
						}
						policies, err := l.dag.FindPoliciesFor(policy.TargetRefs, &kuadrantv1.AuthPolicy{})
						if err != nil {
							return types.NewErr("cel-kuadrant(authpolicies_for_policy): %w", err)
						}
						list := make([]ref.Val, 0, len(policies))
						for _, pol := range policies {
							outPolicy := registry.NativeToValue(pol)
							list = append(list, outPolicy)
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
	opts = append(opts, cel.Variable("self", cel.ObjectType("kuadrant.v1.Policy")))

	return opts
}

// ProgramOptions implements cel.Library. No program-level options are required
// currently so an empty slice is returned.
func (l kuadrantLib) ProgramOptions() []cel.ProgramOption {
	return []cel.ProgramOption{}
}

// LibraryName implements cel.SingletonLibrary.
func (kuadrantLib) LibraryName() string {
	return "kuadrant.cel.ext.kuadrant"
}

// getRegistryWithTypes constructs a types.Registry containing the protobuf
// message descriptors required by the library's functions (policy, gateway
// and common metadata types).
func getRegistryWithTypes() *types.Registry {
	registry, _ := types.NewRegistry(
		// common.proto
		&v1.Metadata{},
		&v1.TargetRef{},
		&v1.Condition{},
		&v1.ConditionStatus{},

		// gateway_api.proto
		&v1.Gateway{},
		&v1.GatewaySpec{},
		&v1.Listener{},
		&v1.GatewayAddresses{},
		&v1.GatewayStatus{},
		&v1.ListenerStatus{},
		&v1.GatewayClass{},
		&v1.GatewayClassSpec{},
		&v1.GatewayClassStatus{},

		// policy.proto
		&v1.Policy{},
		&v1.PolicyStatus{},
	)
	return registry
}
