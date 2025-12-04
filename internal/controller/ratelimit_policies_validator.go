package controllers

import (
	"context"
	"sync"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
)

type RateLimitPolicyValidator struct {
	isLimitadorOperatorInstalled bool
	isGatewayAPIInstalled        bool
	isGatewayProviderInstalled   bool
}

// RateLimitPolicyValidator subscribes to events with potential to flip the validity of rate limit policies
func (r *RateLimitPolicyValidator) Subscription() controller.Subscription {
	return controller.Subscription{
		ReconcileFunc: r.Validate,
		Events: []controller.ResourceEventMatcher{
			{Kind: &machinery.GatewayGroupKind},
			{Kind: &machinery.HTTPRouteGroupKind},
			{Kind: &kuadrantv1.RateLimitPolicyGroupKind, EventType: ptr.To(controller.CreateEvent)},
			{Kind: &kuadrantv1.RateLimitPolicyGroupKind, EventType: ptr.To(controller.UpdateEvent)},
		},
	}
}

func (r *RateLimitPolicyValidator) Validate(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("RateLimitPolicyValidator").WithValues("context", ctx)
	tracer := controller.TracerFromContext(ctx)

	policies := topology.Policies().Items(func(o machinery.Object) bool {
		return o.GroupVersionKind().GroupKind() == kuadrantv1.RateLimitPolicyGroupKind
	})

	logger.V(1).Info("validating rate limit policies", "policies", len(policies))
	defer logger.V(1).Info("finished validating rate limit policies")

	validationResults := make(map[string]error)

	for _, policy := range policies {
		p := policy.(*kuadrantv1.RateLimitPolicy)

		_, span := tracer.Start(ctx, "policy.RateLimitPolicy.validate")
		span.SetAttributes(
			attribute.String("policy.name", p.GetName()),
			attribute.String("policy.namespace", p.GetNamespace()),
			attribute.String("policy.kind", kuadrantv1.RateLimitPolicyGroupKind.Kind),
			attribute.String("policy.uid", string(p.GetUID())),
		)

		var err error
		if missingDepErr := r.isMissingDependency(); missingDepErr != nil {
			err = missingDepErr
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
			err = kuadrant.NewErrPolicyTargetNotFound(kuadrantv1.RateLimitPolicyGroupKind.Kind, ref, apierrors.NewNotFound(res, ref.GetName()))
			span.RecordError(err)
			span.SetStatus(codes.Error, "target not found")
		} else {
			span.AddEvent("policy validated successfully")
			span.SetStatus(codes.Ok, "")
		}

		validationResults[policy.GetLocator()] = err
		span.End()
	}

	state.Store(StateRateLimitPolicyValid, validationResults)

	return nil
}

func (r *RateLimitPolicyValidator) isMissingDependency() error {
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
