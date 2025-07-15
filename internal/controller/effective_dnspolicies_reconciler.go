package controllers

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/samber/lo"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kuadrantdnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	"github.com/kuadrant/kuadrant-operator/internal/utils"
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
			{Kind: &kuadrantv1.DNSPolicyGroupKind},
			{Kind: &DNSRecordGroupKind},
		},
	}
}

func isWildCardListener(l *machinery.Listener) bool {
	return l != nil && (l.Hostname == nil || *l.Hostname == "" || strings.HasPrefix(string(*l.Hostname), "*."))
}

// DNSNamesForGateway returns a set of dnstargets to create records for keyed against a listener location
func dnsNamesForGatewayFromRoutes(ctx context.Context, topology *machinery.Topology, gateway *machinery.Gateway) map[string][]dnsTarget {
	logger := controller.LoggerFromContext(ctx).WithName("DNSNamesForGateway")
	// this will give us all routes targeting the sectionName + any targeting the gateway directly
	gatewayListeners := lo.FilterMap(topology.All().Children(gateway), func(item machinery.Object, _ int) (*machinery.Listener, bool) {
		v, ok := item.(*machinery.Listener)
		return v, ok
	})
	//TODO is there a better way to do this?
	var gatewayHTTPRoutes = []*machinery.HTTPRoute{}
	for _, gl := range gatewayListeners {
		// duplicated TODO clean up
		hasAttachedRoute := false
		for _, statusListener := range gateway.Status.Listeners {
			if string(gl.Name) == string(statusListener.Name) {
				hasAttachedRoute = statusListener.AttachedRoutes > 0
				break
			}
		}
		if !hasAttachedRoute {
			continue
		}

		listenerRoutes := lo.FilterMap(topology.All().Children(gl), func(item machinery.Object, _ int) (*machinery.HTTPRoute, bool) {
			v, ok := item.(*machinery.HTTPRoute)
			if ok {
				if !slices.Contains(gatewayHTTPRoutes, v) {
					return v, ok
				}
				return v, false
			}
			return v, ok
		})
		gatewayHTTPRoutes = append(gatewayHTTPRoutes, listenerRoutes...)
	}

	names := map[string][]dnsTarget{}
	for _, route := range gatewayHTTPRoutes {
		for _, routeHost := range route.Spec.Hostnames {
			logger.V(1).Info(route.GetLocator(), "with hostname", routeHost)
			var hostMatch, listernerID, listenerName string
			for _, listener := range gatewayListeners {
				if !isWildCardListener(listener) {
					//exact match were done
					if *listener.Hostname == routeHost {
						logger.V(1).Info("exact listener host match found", "listener:", listener.GetLocator(), "route host", routeHost)
						listernerID = listener.GetLocator()
						listenerName = string(listener.Listener.Name)
						break
					}
				}
				if isWildCardListener(listener) {
					logger.V(1).Info("wildcard:", "listener:", listener.GetLocator())
					if hostMatch == "" && (listener.Hostname == nil || *listener.Hostname == "") {
						hostMatch = ".*" // shortest possible match
						listernerID = listener.GetLocator()
						listenerName = string(listener.Listener.Name)
						logger.V(1).Info("set listener as match", "listener host", "default wildcard listener", "route host", routeHost)
					}
					if listener.Hostname != nil {
						subDomain := strings.ReplaceAll(string(*listener.Hostname), "*.", "")
						if strings.HasSuffix(string(routeHost), subDomain) {
							if len(hostMatch) < len(subDomain) {
								hostMatch = subDomain
								listernerID = listener.GetLocator()
								listenerName = string(listener.Listener.Name)
								logger.V(1).Info("set listener as match", "listener host", *listener.Hostname, "route host", routeHost)
							}
						}
					}
				}
			}

			if listernerID != "" && listenerName != "" {
				t := dnsTarget{hostname: routeHost, listenerName: listenerName, isHTTPRouteHost: true}
				if !slices.Contains(names[listernerID], t) {
					names[listernerID] = append(names[listernerID], t)
				}
			}
		}
	}
	return names
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

			listenerHasHost := listener.Hostname != nil && *listener.Hostname != ""

			if !policy.UseHTTPRouteHosts() && !listenerHasHost {
				lLogger.Info("listener has no hostname and not using httproute hosts, skipping")
				continue
			}
			// dns targets for this specific listener
			listenerDNSTargets := []dnsTarget{}

			if policy.UseHTTPRouteHosts() {
				lLogger.V(1).Info("policy targeting listener uses routes for dns names")
				listenerToDNSTargets := dnsNamesForGatewayFromRoutes(ctx, topology, listener.Gateway)
				targets, ok := listenerToDNSTargets[listener.GetLocator()]
				if !ok {
					continue
				}

				listenerDNSTargets = targets
			}
			if listenerHasHost && !policy.UseHTTPRouteHosts() {
				listenerDNSTargets = append(listenerDNSTargets, dnsTarget{
					hostname:     *listener.Hostname,
					listenerName: string(listener.Name),
				})
			}

			//isolate any existing records that are not part of the calculated dns targets ready for removal
			staleListenerRecords := lo.FilterMap(topology.Objects().Children(listener), func(o machinery.Object, _ int) (*kuadrantdnsv1alpha1.DNSRecord, bool) {
				dns, ok := o.(*controller.RuntimeObject).Object.(*kuadrantdnsv1alpha1.DNSRecord)
				if !ok {
					return dns, ok
				}
				stale := !slices.ContainsFunc(listenerDNSTargets, func(t dnsTarget) bool {
					return dns.Spec.RootHost == string(t.hostname)
				})
				return dns, stale
			})

			allListenerTargetsReady := true

			for _, target := range listenerDNSTargets {
				desiredRecord, err := desiredDNSRecord(gateway.Gateway, clusterID, policy, target)
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
					dns, ok := o.(*controller.RuntimeObject).Object.(*kuadrantdnsv1alpha1.DNSRecord)
					return ok && o.GetNamespace() == listener.GetNamespace() && dns.Spec.RootHost == string(target.hostname)
				})

				if len(desiredRecord.Spec.Endpoints) == 0 {
					policyErrors[policy.GetLocator()] = ErrNoAddresses
				}

				//Update
				if recordExists {
					rLogger := lLogger.WithValues("record exists:", existingRecordObj.GetLocator())

					existingRecord := existingRecordObj.(*controller.RuntimeObject).Object.(*kuadrantdnsv1alpha1.DNSRecord)
					if !meta.IsStatusConditionTrue(existingRecord.Status.Conditions, "Ready") {
						allListenerTargetsReady = false
					}

					// Deal with the potential deletion of a record first
					if !hasAttachedRoute || len(desiredRecord.Spec.Endpoints) == 0 {
						if !hasAttachedRoute {
							rLogger.V(1).Info("listener has no attached routes, deleting record for listener")
						} else {
							rLogger.V(1).Info("no endpoint addresses for DNSRecord, deleting record for listener")
						}
						// remove the new record and any stale records
						r.deleteRecord(ctx, existingRecord)
						for _, stale := range staleListenerRecords {
							r.deleteRecord(ctx, stale)
						}
						continue
					}

					if !canUpdateDNSRecord(ctx, existingRecord, desiredRecord) {
						rLogger.V(1).Info("unable to update record, deleting record for listener and re-creating")
						r.deleteRecord(ctx, existingRecord)
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
			// if all the targets are ready remove any stale records

			if allListenerTargetsReady {
				lLogger.Info("stale: all listener dns records are ready, ")
				for _, stale := range staleListenerRecords {
					lLogger.Info("stale: all listener targets are ready deleting any stale records ")
					r.deleteRecord(ctx, stale)
				}
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

	orphanRecords := lo.FilterMap(topology.Objects().Items(), func(item machinery.Object, _ int) (*kuadrantdnsv1alpha1.DNSRecord, bool) {
		dns, ok := item.(*controller.RuntimeObject).Object.(*kuadrantdnsv1alpha1.DNSRecord)
		if ok {
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
				rLogger.Info("dns record has no parent targetable, deleting")
				return dns, true
			}

			//Policy removed from topology
			if len(pPolicies) == 0 {
				rLogger.Info("dns record has no parent policy, deleting")
				return dns, true
			}

			//Policy target ref changes
			if len(topology.All().Paths(pPolicies[0], item)) == 1 { //There will always be at least one DNSPolicy -> DNSRecord
				rLogger.Info("dns record has no path through a targetable to the policy, deleting", "policy", pPolicies[0])
				return dns, true
			}

			return dns, false
		}
		return dns, false
	})

	for _, obj := range orphanRecords {
		r.deleteRecord(ctx, obj)
	}

	return nil
}

func (r *EffectiveDNSPoliciesReconciler) deleteRecord(ctx context.Context, record *kuadrantdnsv1alpha1.DNSRecord) {
	logger := controller.LoggerFromContext(ctx)

	//record := obj.(*controller.RuntimeObject).Object.(*kuadrantdnsv1alpha1.DNSRecord)
	if record.GetDeletionTimestamp() != nil {
		return
	}
	logger.Info("deleting dns record", "record", record.Name)

	resource := r.client.Resource(DNSRecordResource).Namespace(record.GetNamespace())
	if err := resource.Delete(ctx, record.GetName(), metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		logger.Error(err, "failed to delete DNSRecord", "record", record.Name)
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
		logger.V(1).Info("root host for existing record has changed", "from", current.Spec.RootHost, "to", desired.Spec.RootHost)
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
