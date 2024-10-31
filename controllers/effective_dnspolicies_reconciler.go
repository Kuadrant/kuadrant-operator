package controllers

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	"github.com/samber/lo"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kuadrantdnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"

	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

var (
	ErrNoRoutes    = fmt.Errorf("no routes attached to any gateway listeners")
	ErrNoAddresses = fmt.Errorf("no valid status addresses to use on gateway")
)

func NewEffectiveDNSPoliciesReconciler(client *dynamic.DynamicClient, scheme *runtime.Scheme) *EffectiveDNSPoliciesReconciler {
	return &EffectiveDNSPoliciesReconciler{
		client: client,
		scheme: scheme,
	}
}

type EffectiveDNSPoliciesReconciler struct {
	client *dynamic.DynamicClient
	scheme *runtime.Scheme
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
	logger := controller.LoggerFromContext(ctx).WithName("EffectiveDNSPoliciesReconciler")

	policyTypeFilterFunc := dnsPolicyTypeFilterFunc()
	policyAcceptedFunc := dnsPolicyAcceptedStatusFunc(state)

	policies := lo.FilterMap(topology.Policies().Items(), policyTypeFilterFunc)

	policyErrors := map[string]error{}

	logger.V(1).Info("updating dns policies", "policies", len(policies))

	clusterID, err := utils.GetClusterUID(ctx, r.client)
	if err != nil {
		return fmt.Errorf("failed to generate cluster ID: %w", err)
	}

	for _, policy := range policies {
		pLogger := logger.WithValues("policy", policy.GetLocator())

		if policy.GetDeletionTimestamp() != nil {
			pLogger.V(1).Info("policy marked for deletion, skipping")
			continue
		}

		if accepted, _ := policyAcceptedFunc(policy); !accepted {
			pLogger.V(1).Info("policy not accepted, skipping")
			continue
		}

		listeners := listenersForPolicy(ctx, topology, policy, policyTypeFilterFunc)

		if logger.V(1).Enabled() {
			listenerLocators := lo.Map(listeners, func(item *machinery.Listener, _ int) string {
				return item.GetLocator()
			})
			pLogger.V(1).Info("reconciling policy for gateway listeners", "listeners", listenerLocators)
		}

		var gatewayHasAttachedRoutes = false
		var gatewayHasAddresses = false

		for _, listener := range listeners {
			lLogger := pLogger.WithValues("listener", listener.GetLocator())

			gateway := listener.Gateway
			if listener.Hostname == nil || *listener.Hostname == "" {
				lLogger.Info("listener has no hostname assigned, skipping")
				continue
			}

			if len(gateway.Status.Addresses) > 0 {
				gatewayHasAddresses = true
			}

			hasAttachedRoute := false
			for _, statusListener := range gateway.Status.Listeners {
				if string(listener.Name) == string(statusListener.Name) {
					hasAttachedRoute = statusListener.AttachedRoutes > 0
					break
				}
			}
			if hasAttachedRoute {
				gatewayHasAttachedRoutes = true
			}

			desiredRecord, err := desiredDNSRecord(gateway.Gateway, clusterID, policy, *listener.Listener)
			if err != nil {
				lLogger.Error(err, "failed to build desired dns record")
				continue
			}
			if err = controllerutil.SetControllerReference(policy, desiredRecord, r.scheme); err != nil {
				lLogger.Error(err, "failed to set owner reference on desired record")
				continue
			}

			resource := r.client.Resource(DNSRecordResource).Namespace(desiredRecord.GetNamespace())

			existingRecordObj, recordExists := lo.Find(topology.Objects().Children(listener), func(o machinery.Object) bool {
				_, ok := o.(*controller.RuntimeObject).Object.(*kuadrantdnsv1alpha1.DNSRecord)
				return ok && o.GetNamespace() == listener.GetNamespace() && o.GetName() == dnsRecordName(listener.Gateway.Name, string(listener.Name))
			})

			if len(desiredRecord.Spec.Endpoints) == 0 {
				policyErrors[policy.GetLocator()] = ErrNoAddresses
			}

			//Update
			if recordExists {
				rLogger := lLogger.WithValues("record", existingRecordObj.GetLocator())

				existingRecord := existingRecordObj.(*controller.RuntimeObject).Object.(*kuadrantdnsv1alpha1.DNSRecord)

				// Deal with the potential deletion of a record first
				if !hasAttachedRoute || len(desiredRecord.Spec.Endpoints) == 0 {
					if !hasAttachedRoute {
						rLogger.V(1).Info("listener has no attached routes, deleting record for listener")
					} else {
						rLogger.V(1).Info("no endpoint addresses for DNSRecord, deleting record for listener")
					}
					r.deleteRecord(ctx, existingRecordObj)
					continue
				}

				if !canUpdateDNSRecord(ctx, existingRecord, desiredRecord) {
					rLogger.V(1).Info("unable to update record, deleting record for listener and re-creating")
					r.deleteRecord(ctx, existingRecordObj)
					break
				}

				if reflect.DeepEqual(existingRecord.Spec, desiredRecord.Spec) {
					rLogger.V(1).Info("dns record is up to date, nothing to do")
					continue
				}
				existingRecord.Spec = desiredRecord.Spec

				un, err := controller.Destruct(existingRecord)
				if err != nil {
					lLogger.Error(err, "unable to destruct dns record")
					continue
				}

				rLogger.V(1).Info("updating record for listener")
				if _, uErr := resource.Update(ctx, un, metav1.UpdateOptions{}); uErr != nil {
					rLogger.Error(uErr, "unable to update dns record")
				}
				continue
			}

			if !hasAttachedRoute {
				lLogger.V(1).Info("listener has no attached routes, skipping record create for listener")
				continue
			}

			if len(desiredRecord.Spec.Endpoints) == 0 {
				lLogger.V(1).Info("record for listener has no addresses, skipping record create for listener")
				continue
			}

			un, err := controller.Destruct(desiredRecord)
			if err != nil {
				lLogger.Error(err, "unable to destruct dns record")
				continue
			}

			//Create
			lLogger.V(1).Info("creating DNS record for listener")
			if _, cErr := resource.Create(ctx, un, metav1.CreateOptions{}); cErr != nil && !apierrors.IsAlreadyExists(cErr) {
				lLogger.Error(cErr, "unable to create dns record")
			}
		}

		if !gatewayHasAddresses {
			pLogger.V(1).Info("gateway has no addresses")
			policyErrors[policy.GetLocator()] = ErrNoAddresses
		} else if !gatewayHasAttachedRoutes {
			pLogger.V(1).Info("gateway has no attached routes")
			policyErrors[policy.GetLocator()] = ErrNoRoutes
		}
	}

	state.Store(StateDNSPolicyErrorsKey, policyErrors)

	return r.deleteOrphanDNSRecords(controller.LoggerIntoContext(ctx, logger), topology)
}

// deleteOrphanDNSRecords deletes any DNSRecord resources that exist in the topology but have no parent targettable, policy or path back to the policy.
func (r *EffectiveDNSPoliciesReconciler) deleteOrphanDNSRecords(ctx context.Context, topology *machinery.Topology) error {
	logger := controller.LoggerFromContext(ctx).WithName("deleteOrphanDNSRecords")

	orphanRecords := lo.Filter(topology.Objects().Items(), func(item machinery.Object, _ int) bool {
		if item.GroupVersionKind().GroupKind() == DNSRecordGroupKind {
			rLogger := logger.WithValues("record", item.GetLocator())

			pTargettables := topology.Targetables().Parents(item)
			pPolicies := topology.Policies().Parents(item)

			if logger.V(1).Enabled() {
				pPoliciesLocs := lo.Map(pPolicies, func(item machinery.Policy, _ int) string {
					return item.GetLocator()
				})
				pTargetablesLocs := lo.Map(pTargettables, func(item machinery.Targetable, _ int) string {
					return item.GetLocator()
				})
				rLogger.V(1).Info("dns record parents", "targetables", pTargetablesLocs, "polices", pPoliciesLocs)
			}

			//Target removed from topology
			if len(pTargettables) == 0 {
				rLogger.Info("dns record has not parent targetable, deleting")
				return true
			}

			//Policy removed from topology
			if len(pPolicies) == 0 {
				rLogger.Info("dns record has not parent policy, deleting")
				return true
			}

			//Policy target ref changes
			if len(topology.All().Paths(pPolicies[0], item)) == 1 { //There will always be at least one DNSPolicy -> DNSRecord
				rLogger.Info("dns record has no path through a targetable to the policy, deleting", "policy", pPolicies[0])
				return true
			}

			return false
		}
		return false
	})

	for _, obj := range orphanRecords {
		r.deleteRecord(ctx, obj)
	}

	return nil
}

func (r *EffectiveDNSPoliciesReconciler) deleteRecord(ctx context.Context, obj machinery.Object) {
	logger := controller.LoggerFromContext(ctx)

	record := obj.(*controller.RuntimeObject).Object.(*kuadrantdnsv1alpha1.DNSRecord)
	if record.GetDeletionTimestamp() != nil {
		return
	}
	logger.Info("deleting dns record", "record", obj.GetLocator())

	resource := r.client.Resource(DNSRecordResource).Namespace(record.GetNamespace())
	if err := resource.Delete(ctx, record.GetName(), metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		logger.Error(err, "failed to delete DNSRecord", "record", obj.GetLocator())
	}
}

// listenersForPolicy returns an array of listeners that are targeted by the given policy.
// If the target is a Listener a single element array containing that listener is returned.
// If the target is a Gateway all listeners that do not have a DNS policy explicitly attached are returned.
func listenersForPolicy(_ context.Context, topology *machinery.Topology, policy machinery.Policy, policyTypeFilterFunc dnsPolicyTypeFilter) []*machinery.Listener {
	return lo.Flatten(lo.FilterMap(topology.Targetables().Children(policy), func(t machinery.Targetable, _ int) ([]*machinery.Listener, bool) {
		if l, ok := t.(*machinery.Listener); ok {
			return []*machinery.Listener{l}, true
		}
		if g, ok := t.(*machinery.Gateway); ok {
			listeners := lo.FilterMap(topology.Targetables().Children(g), func(t machinery.Targetable, _ int) (*machinery.Listener, bool) {
				l, lok := t.(*machinery.Listener)
				lPolicies := lo.FilterMap(l.Policies(), policyTypeFilterFunc)
				return l, lok && len(lPolicies) == 0
			})
			return listeners, true
		}

		return nil, false
	}))
}

// canUpdateDNSRecord returns true if the current record can be updated to the desired.
func canUpdateDNSRecord(ctx context.Context, current, desired *kuadrantdnsv1alpha1.DNSRecord) bool {
	logger := controller.LoggerFromContext(ctx)

	// DNSRecord doesn't currently support rootHost changes
	if current.Spec.RootHost != desired.Spec.RootHost {
		logger.V(1).Info("root host for existing record has changed")
		return false
	}

	// DNSRecord doesn't currently support record type changes due to a limitation of the dns operator
	// https://github.com/Kuadrant/dns-operator/issues/287
	for _, curEp := range current.Spec.Endpoints {
		for _, desEp := range desired.Spec.Endpoints {
			if curEp.DNSName == desEp.DNSName {
				if curEp.RecordType != desEp.RecordType {
					logger.V(1).Info("record type for existing endpoint has changed",
						"dnsName", curEp.DNSName, "current", curEp.RecordType, "desired", desEp.RecordType)
					return false
				}
			}
		}
	}

	return true
}
