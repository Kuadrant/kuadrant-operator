package types

import (
	"context"
	"fmt"

	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"

	"github.com/go-logr/logr"
	celref "github.com/google/cel-go/common/types/ref"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	extutils "github.com/kuadrant/kuadrant-operator/pkg/extension/utils"
)

const (
	PolicyConditionEnforced = kuadrant.PolicyConditionEnforced
)

type Domain int

const (
	DomainUnspecified Domain = iota
	DomainAuth
)

type Policy interface {
	GetName() string
	GetNamespace() string
	GetObjectKind() schema.ObjectKind
	GetTargetRefs() []gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName
}

type KuadrantCtx interface {
	Resolve(context.Context, Policy, string, bool) (celref.Val, error)
	ResolvePolicy(context.Context, Policy, string, bool) (Policy, error)
	AddDataTo(context.Context, Policy, Domain, string, string) error
	ReconcileObject(context.Context, client.Object, client.Object, MutateFn) (client.Object, error)
}

type ReconcileFn func(ctx context.Context, request reconcile.Request, kuadrant KuadrantCtx) (reconcile.Result, error)

// MutateFn is a function which mutates the existing object into it's desired state.
type MutateFn func(existing, desired client.Object) (bool, error)

type ExtensionBase struct {
	Logger logr.Logger
	Client client.Client
	Scheme *runtime.Scheme
}

func (eb *ExtensionBase) Configure(ctx context.Context) error {
	logger := extutils.LoggerFromContext(ctx)

	client, err := extutils.ClientFromContext(ctx)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	scheme, err := extutils.SchemeFromContext(ctx)
	if err != nil {
		return fmt.Errorf("failed to get scheme: %w", err)
	}

	eb.Logger = logger
	eb.Client = client
	eb.Scheme = scheme
	return nil
}
