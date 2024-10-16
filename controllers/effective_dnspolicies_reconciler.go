package controllers

import (
	"context"
	"fmt"
	"sync"

	"github.com/samber/lo"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
	return r.deleteOrphanDNSPolicyRecords(ctx, topology)
}

// deleteOrphanDNSPolicyRecords deletes any DNSRecord resources created by a DNSPolicy but no longer have a valid path in the topology to that policy.
func (r *EffectiveDNSPoliciesReconciler) deleteOrphanDNSPolicyRecords(ctx context.Context, topology *machinery.Topology) error {
	logger := controller.LoggerFromContext(ctx).WithName("effectiveDNSPoliciesReconciler")
	logger.Info("deleting orphan policy DNS records")

	orphanRecords := lo.FilterMap(topology.Objects().Items(), func(item machinery.Object, _ int) (machinery.Object, bool) {
		if item.GroupVersionKind().GroupKind() == DNSRecordGroupKind {
			policyOwnerRef := getObjectPolicyOwnerReference(item, kuadrantv1alpha1.DNSPolicyGroupKind)

			// Ignore all DNSRecords that weren't created by a DNSPolicy
			if policyOwnerRef == nil {
				return nil, false
			}

			// Any DNSRecord that does not have a link in the topology back to its owner DNSPolicy should be removed
			if len(topology.All().Paths(policyOwnerRef, item)) == 0 {
				logger.Info(fmt.Sprintf("dnsrecord object is no longer linked to it's policy owner, dnsrecord: %v, policy: %v", item.GetLocator(), policyOwnerRef.GetLocator()))
				return item, true
			}
		}
		return nil, false
	})

	for i := range orphanRecords {
		record := orphanRecords[i].(*controller.RuntimeObject).Object.(*kuadrantdnsv1alpha1.DNSRecord)
		if record.GetDeletionTimestamp() != nil {
			continue
		}
		logger.Info(fmt.Sprintf("deleting DNSRecord: %v", orphanRecords[i].GetLocator()))
		resource := r.client.Resource(DNSRecordResource).Namespace(record.GetNamespace())
		if err := resource.Delete(ctx, record.GetName(), metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			logger.Error(err, "failed to delete DNSRecord")
		}
	}

	return nil
}

type PolicyOwnerReference struct {
	metav1.OwnerReference
	PolicyNamespace string
	GVK             schema.GroupVersionKind
}

func (o *PolicyOwnerReference) SetGroupVersionKind(gvk schema.GroupVersionKind) {
	o.GVK = gvk
}

func (o *PolicyOwnerReference) GroupVersionKind() schema.GroupVersionKind {
	return o.GVK
}

func (o *PolicyOwnerReference) GetNamespace() string {
	return o.PolicyNamespace
}

func (o *PolicyOwnerReference) GetName() string {
	return o.OwnerReference.Name
}

func (o *PolicyOwnerReference) GetLocator() string {
	return machinery.LocatorFromObject(o)
}

var _ machinery.PolicyTargetReference = &PolicyOwnerReference{}

func getObjectPolicyOwnerReference(item machinery.Object, gk schema.GroupKind) *PolicyOwnerReference {
	var ownerRef *PolicyOwnerReference
	for _, o := range item.(*controller.RuntimeObject).GetOwnerReferences() {
		oGV, err := schema.ParseGroupVersion(o.APIVersion)
		if err != nil {
			continue
		}

		if oGV.Group == gk.Group && o.Kind == gk.Kind {
			ownerRef = &PolicyOwnerReference{
				OwnerReference:  o,
				PolicyNamespace: item.GetNamespace(),
				GVK: schema.GroupVersionKind{
					Group: gk.Group,
					Kind:  gk.Kind,
				},
			}
			break
		}
	}
	return ownerRef
}
