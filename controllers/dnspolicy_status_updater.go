package controllers

import (
	"context"
	"slices"
	"sync"

	"github.com/samber/lo"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"

	kuadrantdnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"

	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
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

func (r *DNSPolicyStatusUpdater) updateStatus(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("DNSPolicyStatusUpdater")

	policies := lo.FilterMap(topology.Policies().Items(), func(item machinery.Policy, index int) (*kuadrantv1alpha1.DNSPolicy, bool) {
		p, ok := item.(*kuadrantv1alpha1.DNSPolicy)
		return p, ok
	})

	policyAcceptedFunc := dnsPolicyAcceptedStatusFunc(state)

	logger.V(1).Info("updating dns policy statuses", "policies", len(policies))

	for _, policy := range policies {
		if policy.GetDeletionTimestamp() != nil {
			logger.V(1).Info("policy marked for deletion, skipping", "name", policy.Name, "namespace", policy.Namespace)
			continue
		}

		// copy initial conditions, otherwise status will always be updated
		newStatus := &kuadrantv1alpha1.DNSPolicyStatus{
			Conditions:         slices.Clone(policy.Status.Conditions),
			ObservedGeneration: policy.Status.ObservedGeneration,
		}

		accepted, err := policyAcceptedFunc(policy)
		meta.SetStatusCondition(&newStatus.Conditions, *kuadrant.AcceptedCondition(policy, err))

		// do not set enforced condition if Accepted condition is false
		if !accepted {
			meta.RemoveStatusCondition(&newStatus.Conditions, string(kuadrant.PolicyConditionEnforced))
		} else {
			policyRecords := lo.FilterMap(topology.Objects().Items(), func(item machinery.Object, _ int) (*kuadrantdnsv1alpha1.DNSRecord, bool) {
				if rObj, isObj := item.(*controller.RuntimeObject); isObj {
					if record, isRec := rObj.Object.(*kuadrantdnsv1alpha1.DNSRecord); isRec {
						return record, lo.ContainsBy(topology.Policies().Parents(item), func(item machinery.Policy) bool {
							return item.GetLocator() == policy.GetLocator()
						})
					}
				}
				return nil, false
			})

			enforcedCond := enforcedCondition(policyRecords, policy)
			meta.SetStatusCondition(&newStatus.Conditions, *enforcedCond)

			//ToDo: Deal with messages, these should probably be retrieved from state after the reconciliation task
			// add some additional user friendly context
			//if errors.Is(specErr, ErrNoAddresses) && !strings.Contains(eCond.Message, ErrNoAddresses.Error()) {
			//	eCond.Message = fmt.Sprintf("%s : %s", eCond.Message, ErrNoAddresses.Error())
			//}
			//if errors.Is(specErr, ErrNoRoutes) && !strings.Contains(eCond.Message, ErrNoRoutes.Error()) {
			//	eCond.Message = fmt.Sprintf("%s : %s", eCond.Message, ErrNoRoutes)
			//}

			propagateRecordConditions(policyRecords, newStatus)

			newStatus.TotalRecords = int32(len(policyRecords))
		}

		equalStatus := equality.Semantic.DeepEqual(newStatus, policy.Status)
		if equalStatus && policy.Generation == policy.Status.ObservedGeneration {
			logger.V(1).Info("policy status unchanged, skipping update")
			continue
		}
		newStatus.ObservedGeneration = policy.Generation
		policy.Status = *newStatus

		obj, err := controller.Destruct(policy)
		if err != nil {
			logger.Error(err, "unable to destruct policy") // should never happen
			continue
		}

		_, err = r.client.Resource(kuadrantv1alpha1.DNSPoliciesResource).Namespace(policy.GetNamespace()).UpdateStatus(ctx, obj, metav1.UpdateOptions{})
		if err != nil {
			logger.Error(err, "unable to update status for policy", "name", policy.GetName(), "namespace", policy.GetNamespace())
		}

		emitConditionMetrics(policy)
	}

	return nil
}
