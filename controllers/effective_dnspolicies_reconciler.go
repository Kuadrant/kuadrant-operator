package controllers

import (
	"context"
	"sync"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"k8s.io/client-go/dynamic"

	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
)

func NewEffectiveDNSPoliciesReconciler(client *dynamic.DynamicClient) *EffectiveDNSPoliciesReconciler {
	return &EffectiveDNSPoliciesReconciler{client: client}
}

type EffectiveDNSPoliciesReconciler struct {
	client *dynamic.DynamicClient
}

func (r *EffectiveDNSPoliciesReconciler) Subscription() controller.Subscription {
	return controller.Subscription{
		ReconcileFunc: r.reconcile,
		Events: []controller.ResourceEventMatcher{
			{Kind: &machinery.GatewayGroupKind},
			{Kind: &kuadrantv1alpha1.DNSPolicyGroupKind},
			{Kind: &DNSRecordGroupKind},
		},
	}
}

func (r *EffectiveDNSPoliciesReconciler) reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	return nil
}
