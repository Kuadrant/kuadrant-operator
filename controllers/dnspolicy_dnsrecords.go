package controllers

import (
	"context"
	"fmt"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crlog "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kuadrant/kuadrant-operator/pkg/reconcilers"

	kuadrantdnsv1alpha1 "github.com/kuadrant/kuadrant-dns-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/controllers/common"
	"github.com/kuadrant/kuadrant-operator/controllers/slice"
)

func (r *DNSPolicyReconciler) reconcileDNSRecords(ctx context.Context, dnsPolicy *v1alpha1.DNSPolicy, gwDiffObj *reconcilers.GatewayDiff) error {
	log := crlog.FromContext(ctx)

	log.V(3).Info("reconciling dns records")
	for _, gw := range gwDiffObj.GatewaysWithInvalidPolicyRef {
		log.V(1).Info("reconcileDNSRecords: gateway with invalid policy ref", "key", gw.Key())
		if err := r.deleteGatewayDNSRecords(ctx, gw.Gateway, dnsPolicy); err != nil {
			return fmt.Errorf("error deleting dns records for gw %v: %w", gw.Gateway.Name, err)
		}
	}

	// Reconcile DNSRecords for each gateway directly referred by the policy (existing and new)
	for _, gw := range append(gwDiffObj.GatewaysWithValidPolicyRef, gwDiffObj.GatewaysMissingPolicyRef...) {
		log.V(1).Info("reconcileDNSRecords: gateway with valid or missing policy ref", "key", gw.Key())
		if err := r.reconcileGatewayDNSRecords(ctx, gw.Gateway, dnsPolicy); err != nil {
			return fmt.Errorf("error reconciling dns records for gateway %v: %w", gw.Gateway.Name, err)
		}
	}
	return nil
}

func (r *DNSPolicyReconciler) reconcileGatewayDNSRecords(ctx context.Context, gw *gatewayapiv1.Gateway, dnsPolicy *v1alpha1.DNSPolicy) error {
	log := crlog.FromContext(ctx)

	gatewayWrapper := common.NewGatewayWrapper(gw)
	if err := gatewayWrapper.Validate(); err != nil {
		return err
	}

	if err := r.dnsHelper.removeDNSForDeletedListeners(ctx, gatewayWrapper.Gateway); err != nil {
		log.V(3).Info("error removing DNS for deleted listeners")
		return err
	}

	clusterGateways := gatewayWrapper.GetClusterGateways()

	log.V(3).Info("checking gateway for attached routes ", "gateway", gatewayWrapper.Name, "clusterGateways", clusterGateways)

	for _, listener := range gatewayWrapper.Spec.Listeners {
		var mz, err = r.dnsHelper.getManagedZoneForListener(ctx, gatewayWrapper.Namespace, listener)
		if err != nil {
			return err
		}
		listenerHost := *listener.Hostname
		if listenerHost == "" {
			log.Info("skipping listener no hostname assigned", listener.Name, "in ns ", gatewayWrapper.Namespace)
			continue
		}

		listenerGateways := slice.Filter(clusterGateways, func(cgw common.ClusterGateway) bool {
			hasAttachedRoute := false
			for _, statusListener := range cgw.Status.Listeners {
				if string(statusListener.Name) == string(listener.Name) {
					hasAttachedRoute = int(statusListener.AttachedRoutes) > 0
					break
				}
			}
			return hasAttachedRoute
		})

		if len(listenerGateways) == 0 {
			// delete record
			log.V(1).Info("no cluster gateways, deleting DNS record", " for listener ", listener.Name)
			if err := r.dnsHelper.deleteDNSRecordForListener(ctx, gatewayWrapper, listener); client.IgnoreNotFound(err) != nil {
				return fmt.Errorf("failed to delete dns record for listener %s : %s", listener.Name, err)
			}
			return nil
		}
		dnsRecord, err := r.dnsHelper.createDNSRecordForListener(ctx, gatewayWrapper.Gateway, dnsPolicy, mz, listener)
		if err := client.IgnoreAlreadyExists(err); err != nil {
			return fmt.Errorf("failed to create dns record for listener host %s : %s ", *listener.Hostname, err)
		}
		if k8serrors.IsAlreadyExists(err) {
			dnsRecord, err = r.dnsHelper.getDNSRecordForListener(ctx, listener, gatewayWrapper)
			if err != nil {
				return fmt.Errorf("failed to get dns record for host %s : %s ", listener.Name, err)
			}
		}

		mcgTarget, err := common.NewMultiClusterGatewayTarget(gatewayWrapper.Gateway, listenerGateways, dnsPolicy.Spec.LoadBalancing)
		if err != nil {
			return fmt.Errorf("failed to create multi cluster gateway target for listener %s : %s ", listener.Name, err)
		}

		log.Info("setting dns dnsTargets for gateway listener", "listener", dnsRecord.Name, "values", mcgTarget)
		probes, err := r.dnsHelper.getDNSHealthCheckProbes(ctx, mcgTarget.Gateway, dnsPolicy)
		if err != nil {
			return err
		}
		mcgTarget.RemoveUnhealthyGatewayAddresses(probes, listener)
		if err := r.dnsHelper.setEndpoints(ctx, mcgTarget, dnsRecord, listener, dnsPolicy.Spec.RoutingStrategy); err != nil {
			return fmt.Errorf("failed to add dns record dnsTargets %s %v", err, mcgTarget)
		}
	}
	return nil
}

func (r *DNSPolicyReconciler) deleteGatewayDNSRecords(ctx context.Context, gateway *gatewayapiv1.Gateway, dnsPolicy *v1alpha1.DNSPolicy) error {
	return r.deleteDNSRecordsWithLabels(ctx, commonDNSRecordLabels(client.ObjectKeyFromObject(gateway), client.ObjectKeyFromObject(dnsPolicy)), dnsPolicy.Namespace)
}

func (r *DNSPolicyReconciler) deleteDNSRecords(ctx context.Context, dnsPolicy *v1alpha1.DNSPolicy) error {
	return r.deleteDNSRecordsWithLabels(ctx, policyDNSRecordLabels(client.ObjectKeyFromObject(dnsPolicy)), dnsPolicy.Namespace)
}

func (r *DNSPolicyReconciler) deleteDNSRecordsWithLabels(ctx context.Context, lbls map[string]string, namespace string) error {
	log := crlog.FromContext(ctx)

	listOptions := &client.ListOptions{LabelSelector: labels.SelectorFromSet(lbls), Namespace: namespace}
	recordsList := &kuadrantdnsv1alpha1.DNSRecordList{}
	if err := r.Client().List(ctx, recordsList, listOptions); err != nil {
		return err
	}

	for _, record := range recordsList.Items {
		if err := r.DeleteResource(ctx, &record); client.IgnoreNotFound(err) != nil {
			log.Error(err, "failed to delete DNSRecord")
			return err
		}
	}
	return nil
}
