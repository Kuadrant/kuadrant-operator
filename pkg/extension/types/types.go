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
	// PolicyConditionEnforced mirrors the internal kuadrant enforced condition
	// type for convenience to extension authors.
	PolicyConditionEnforced = kuadrant.PolicyConditionEnforced
	// KuadrantMetricsPrefix is the prefix applied to metric bindings injected
	// via AddDataTo / KuadrantMetricBinding.
	KuadrantMetricsPrefix = "metrics.labels"
)

// Domain enumerates the supported logical domains for mutator injected data.
type Domain int

const (
	// DomainUnspecified indicates no specific domain (default behaviour).
	DomainUnspecified Domain = iota
	// DomainAuth domain for authentication/authorization enrichment.
	DomainAuth
	// DomainRequest domain for request/traffic related enrichment.
	DomainRequest
)

// Policy is an interface for the policy object to be implemented by the extension policy.
// Policy is the common interface a policy object must implement for the
// extension controller. Implementations are usually thin adapters over
// generated protobuf or Kubernetes types.
type Policy interface {
	GetName() string
	GetNamespace() string
	GetObjectKind() schema.ObjectKind
	GetTargetRefs() []gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName
}

// KuadrantCtx is passed to ReconcileFn providing access to CEL resolution,
// mutator registration and object reconciliation helpers.
type KuadrantCtx interface {
	Resolve(context.Context, Policy, string, bool) (celref.Val, error)
	ResolvePolicy(context.Context, Policy, string, bool) (Policy, error)
	AddDataTo(context.Context, Policy, Domain, string, string) error
	ReconcileObject(context.Context, client.Object, client.Object, MutateFn) (client.Object, error)
}

// ReconcileFn defines the signature of an extension reconcile function.
type ReconcileFn func(ctx context.Context, request reconcile.Request, kuadrant KuadrantCtx) (reconcile.Result, error)

// MutateFn is a function that mutates the existing object into it's desired state.
// MutateFn mutates the existing object into the desired state. It returns true
// when a change was applied requiring an update.
type MutateFn func(existing, desired client.Object) (bool, error)

// ExtensionBase is a base struct for the extension controllers.
// ExtensionBase is an embeddable struct providing common fields (logger,
// client, scheme) and a helper Configure method for extension controllers.
type ExtensionBase struct {
	Logger logr.Logger
	Client client.Client
	Scheme *runtime.Scheme
}

// Configure the extension base from the context.
// Configure populates the base fields (Logger, Client, Scheme) from a context
// carrying those values.
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

// KuadrantMetricBinding creates a fully qualified binding name for metrics
// enrichment.
func KuadrantMetricBinding(binding string) string {
	return fmt.Sprintf("%s.%s", KuadrantMetricsPrefix, binding)
}
