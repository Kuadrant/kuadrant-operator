package controllers

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/util/validation/field"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
)

const (
	StateEffectiveTLSPolicies = "EffectiveTLSPolicies"
)

type EffectiveTLSPolicy struct {
	Path []machinery.Targetable
	Spec kuadrantv1.TLSPolicy
}

type EffectiveTLSPolicies map[string]EffectiveTLSPolicy

type EffectiveTLSPoliciesReconciler struct{}

func NewEffectiveTLSPoliciesReconciler() *EffectiveTLSPoliciesReconciler {
	return &EffectiveTLSPoliciesReconciler{}
}

func (t *EffectiveTLSPoliciesReconciler) Subscription() *controller.Subscription {
	return &controller.Subscription{
		Events: []controller.ResourceEventMatcher{
			{Kind: &machinery.GatewayGroupKind},
			{Kind: &kuadrantv1.TLSPolicyGroupKind},
			{Kind: &CertManagerCertificateKind},
		},
		ReconcileFunc: t.Reconcile,
	}
}

func (t *EffectiveTLSPoliciesReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, s *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("EffectiveTLSPoliciesReconciler")
	logger.V(1).Info("generate effective tls policy", "status", "started")
	defer logger.V(1).Info("generate effective tls policy", "status", "completed")

	effectivePolicies := t.calculateEffectivePolicies(ctx, topology, s)

	s.Store(StateEffectiveTLSPolicies, effectivePolicies)

	return nil
}

func (t *EffectiveTLSPoliciesReconciler) calculateEffectivePolicies(ctx context.Context, topology *machinery.Topology, state *sync.Map) EffectiveTLSPolicies {
	logger := controller.LoggerFromContext(ctx).WithName("EffectiveTLSPoliciesReconciler").WithName("calculateEffectivePolicies")

	targetables := topology.Targetables()
	gateways := targetables.Items(func(o machinery.Object) bool {
		_, ok := o.(*machinery.Gateway)
		return ok
	})

	effectivePolicies := EffectiveTLSPolicies{}

	for _, gateway := range gateways {
		listeners := targetables.Children(gateway)
		for _, listener := range listeners {
			l, _ := listener.(*machinery.Listener)
			if err := validateGatewayListenerBlock(field.NewPath(""), *l.Listener, l.Gateway).ToAggregate(); err != nil {
				logger.V(1).Info("Skipped a listener block: " + err.Error())
				continue
			}
			paths := targetables.Paths(gateway, listener)
			for i := range paths {
				effectivePolicy := kuadrantv1.EffectivePolicyForPath[*kuadrantv1.TLSPolicy](paths[i], func(p machinery.Policy) bool {
					tlsPolicy, ok := p.(*kuadrantv1.TLSPolicy)
					if !ok {
						return false
					}
					if tlsPolicy.DeletionTimestamp != nil {
						logger.V(1).Info("policy is marked for deletion, nothing to do", "name", tlsPolicy.Name, "namespace", tlsPolicy.Namespace, "uid", tlsPolicy.GetUID())
						return false
					}

					isValid, _ := IsTLSPolicyValid(ctx, state, tlsPolicy)

					return isValid
				})
				if effectivePolicy != nil {
					pathID := kuadrantv1.PathID(paths[i])
					effectivePolicies[pathID] = EffectiveTLSPolicy{
						Path: paths[i],
						Spec: **effectivePolicy,
					}
					if logger.V(1).Enabled() {
						jsonEffectivePolicy, _ := json.Marshal(effectivePolicy)
						pathLocators := lo.Map(paths[i], machinery.MapTargetableToLocatorFunc)
						logger.V(1).Info("effective policy", "kind", kuadrantv1.TLSPolicyGroupKind.Kind, "pathID", pathID, "path", pathLocators, "effectivePolicy", string(jsonEffectivePolicy))
					}
				}
			}
		}
	}

	logger.V(1).Info("finished calculating effective tls policies", "effectivePolicies", len(effectivePolicies))

	return effectivePolicies
}
