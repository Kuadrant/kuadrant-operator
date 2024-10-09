package controllers

import (
	"context"
	"sync"

	"github.com/samber/lo"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"

	kuadrantdnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"

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

func (r *EffectiveDNSPoliciesReconciler) reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, _ *sync.Map) error {
	//ToDo Implement DNSRecord reconcile
	return r.deleteOrphanDNSRecords(ctx, topology)
}

// deleteOrphanDNSRecords deletes any DNSRecord resources that exist in the topology but have no parent targettable, policy or path back to the policy.
func (r *EffectiveDNSPoliciesReconciler) deleteOrphanDNSRecords(ctx context.Context, topology *machinery.Topology) error {
	logger := controller.LoggerFromContext(ctx).WithName("EffectiveDNSPoliciesReconciler")

	orphanRecords := lo.Filter(topology.Objects().Items(), func(item machinery.Object, _ int) bool {
		if item.GroupVersionKind().GroupKind() == DNSRecordGroupKind {
			pTargettables := topology.Targetables().Parents(item)
			pPolicies := topology.Policies().Parents(item)

			if logger.V(1).Enabled() {
				pPoliciesLocs := lo.Map(pPolicies, func(item machinery.Policy, _ int) string {
					return item.GetLocator()
				})
				pTargetablesLocs := lo.Map(pTargettables, func(item machinery.Targetable, _ int) string {
					return item.GetLocator()
				})
				logger.V(1).Info("dns record parents", "record", item.GetLocator(), "targetables", pTargetablesLocs, "polices", pPoliciesLocs)
			}

			//Target removed from topology
			if len(pTargettables) == 0 {
				return true
			}

			//Policy removed from topology
			if len(pPolicies) == 0 {
				return true
			}

			//Policy target ref changes
			if len(topology.All().Paths(pPolicies[0], item)) == 1 { //There will always be at least one DNSPolicy -> DNSRecord
				return true
			}

			return false
		}
		return false
	})

	for _, obj := range orphanRecords {
		record := obj.(*controller.RuntimeObject).Object.(*kuadrantdnsv1alpha1.DNSRecord)
		if record.GetDeletionTimestamp() != nil {
			continue
		}
		logger.Info("deleting orphan dns record", "record", obj.GetLocator())
		resource := r.client.Resource(DNSRecordResource).Namespace(record.GetNamespace())
		if err := resource.Delete(ctx, record.GetName(), metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			logger.Error(err, "failed to delete DNSRecord", "record", obj.GetLocator())
		}
	}

	return nil
}
