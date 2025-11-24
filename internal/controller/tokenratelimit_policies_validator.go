package controllers

import (
	"context"
	"errors"
	"sync"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
)

type TokenRateLimitPolicyValidator struct {
	isLimitadorOperatorInstalled bool
	isGatewayAPIInstalled        bool
	isGatewayProviderInstalled   bool
}

// TokenRateLimitPolicyValidator subscribes to events with potential to flip the validity of token rate limit policies
func (r *TokenRateLimitPolicyValidator) Subscription() controller.Subscription {
	return controller.Subscription{
		ReconcileFunc: r.Validate,
		Events: []controller.ResourceEventMatcher{
			{Kind: &machinery.GatewayGroupKind},
			{Kind: &machinery.HTTPRouteGroupKind},
			{Kind: &kuadrantv1alpha1.TokenRateLimitPolicyGroupKind, EventType: ptr.To(controller.CreateEvent)},
			{Kind: &kuadrantv1alpha1.TokenRateLimitPolicyGroupKind, EventType: ptr.To(controller.UpdateEvent)},
		},
	}
}

func (r *TokenRateLimitPolicyValidator) Validate(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("TokenRateLimitPolicyValidator").WithValues("context", ctx)
	tracer := otel.Tracer("kuadrant-operator")

	policies := topology.Policies().Items(func(o machinery.Object) bool {
		return o.GroupVersionKind().GroupKind() == kuadrantv1alpha1.TokenRateLimitPolicyGroupKind
	})

	logger.V(1).Info("validating token rate limit policies", "policies", len(policies))
	defer logger.V(1).Info("finished validating token rate limit policies")

	missingDependencyErr := r.isMissingDependency()

	validationResults := make(map[string]error)

	for _, policy := range policies {
		p := policy.(*kuadrantv1alpha1.TokenRateLimitPolicy)

		_, span := tracer.Start(ctx, "policy.TokenRateLimitPolicy.validate")
		span.SetAttributes(
			attribute.String("policy.name", p.GetName()),
			attribute.String("policy.namespace", p.GetNamespace()),
			attribute.String("policy.kind", kuadrantv1alpha1.TokenRateLimitPolicyGroupKind.Kind),
			attribute.String("policy.uid", string(p.GetUID())),
		)

		var err error
		if missingDependencyErr != nil {
			err = missingDependencyErr
			span.RecordError(err)
			span.SetStatus(codes.Error, "missing dependency")
		} else if len(policy.GetTargetRefs()) > 0 && len(topology.Targetables().Children(policy)) == 0 {
			ref := policy.GetTargetRefs()[0]
			var res schema.GroupResource
			switch ref.GroupVersionKind().Kind {
			case machinery.GatewayGroupKind.Kind:
				res = controller.GatewaysResource.GroupResource()
			case machinery.HTTPRouteGroupKind.Kind:
				res = controller.HTTPRoutesResource.GroupResource()
			}
			err = kuadrant.NewErrPolicyTargetNotFound(kuadrantv1alpha1.TokenRateLimitPolicyGroupKind.Kind, ref, apierrors.NewNotFound(res, ref.GetName()))
			span.RecordError(err)
			span.SetStatus(codes.Error, "target not found")
		} else {
			span.AddEvent("policy validated successfully")
			span.SetStatus(codes.Ok, "")
		}

		validationResults[policy.GetLocator()] = err
		span.End()
	}

	state.Store(StateTokenRateLimitPolicyValid, validationResults)

	return nil
}

func (r *TokenRateLimitPolicyValidator) isMissingDependency() error {
	isMissingDependency := false
	var missingDependencies []string

	if !r.isGatewayAPIInstalled {
		isMissingDependency = true
		missingDependencies = append(missingDependencies, "Gateway API")
	}
	if !r.isGatewayProviderInstalled {
		isMissingDependency = true
		missingDependencies = append(missingDependencies, "Gateway API provider (istio / envoy gateway)")
	}
	if !r.isLimitadorOperatorInstalled {
		isMissingDependency = true
		missingDependencies = append(missingDependencies, "Limitador Operator")
	}

	if isMissingDependency {
		return kuadrant.NewErrDependencyNotInstalled(missingDependencies...)
	}

	return nil
}

const StateTokenRateLimitPolicyValid = "TokenRateLimitPolicyValid"

func tokenRateLimitPolicyAcceptedStatusFunc(state *sync.Map) func(policy *kuadrantv1alpha1.TokenRateLimitPolicy) (bool, error) {
	stateMap, validated := state.Load(StateTokenRateLimitPolicyValid)
	if !validated {
		// fallback to reading the policy's existing status conditions to avoid flapping during state initialization
		return tokenRateLimitPolicyAcceptedStatus
	}
	m := stateMap.(map[string]error)

	return func(policy *kuadrantv1alpha1.TokenRateLimitPolicy) (bool, error) {
		err, validated := m[policy.GetLocator()]
		if validated {
			return err == nil || apierrors.IsNotFound(err), err
		}
		return tokenRateLimitPolicyAcceptedStatus(policy)
	}
}

func tokenRateLimitPolicyAcceptedStatus(policy *kuadrantv1alpha1.TokenRateLimitPolicy) (accepted bool, err error) {
	if condition := meta.FindStatusCondition(policy.Status.Conditions, string(gatewayapiv1alpha2.PolicyConditionAccepted)); condition != nil {
		accepted = condition.Status == metav1.ConditionTrue
		if !accepted {
			err = errors.New(condition.Message)
		}
		return
	}
	return
}

func isTokenRateLimitPolicyAcceptedAndNotDeletedFunc(state *sync.Map) func(machinery.Policy) bool {
	f := isTokenRateLimitPolicyAcceptedFunc(state)
	return func(policy machinery.Policy) bool {
		p, object := policy.(metav1.Object)
		return object && f(policy) && p.GetDeletionTimestamp() == nil
	}
}

func isTokenRateLimitPolicyAcceptedFunc(state *sync.Map) func(machinery.Policy) bool {
	f := tokenRateLimitPolicyAcceptedStatusFunc(state)
	return func(policy machinery.Policy) bool {
		if tokenPolicy, ok := policy.(*kuadrantv1alpha1.TokenRateLimitPolicy); ok {
			accepted, _ := f(tokenPolicy)
			return accepted
		}
		return false
	}
}
