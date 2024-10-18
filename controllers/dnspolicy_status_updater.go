package controllers

import (
	"context"
	"sync"

	"k8s.io/client-go/dynamic"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"

	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
)

func NewDNSPolicyStatusUpdater(client *dynamic.DynamicClient) *DNSPolicyStatusUpdater {
	return &DNSPolicyStatusUpdater{client: client}
}

type DNSPolicyStatusUpdater struct {
	client *dynamic.DynamicClient
}

func (r *DNSPolicyStatusUpdater) Subscription() controller.Subscription {
	return controller.Subscription{
		ReconcileFunc: r.updateStatus,
		Events: []controller.ResourceEventMatcher{
			{Kind: &machinery.GatewayGroupKind},
			{Kind: &kuadrantv1alpha1.DNSPolicyGroupKind},
			{Kind: &DNSRecordGroupKind},
		},
	}
}

func (r *DNSPolicyStatusUpdater) updateStatus(_ context.Context, _ []controller.ResourceEvent, _ *machinery.Topology, _ error, _ *sync.Map) error {
	//ToDo Implement implement me !!!
	return nil
}
