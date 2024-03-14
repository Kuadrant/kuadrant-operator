package controllers

import (
	"context"
	"fmt"
	"reflect"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crlog "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantdnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"

	reconcilerutils "github.com/kuadrant/kuadrant-operator/pkg/library/reconcilers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"

	"github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/pkg/multicluster"
)

func (r *DNSPolicyReconciler) reconcileDNSRecords(ctx context.Context, dnsPolicy *v1alpha1.DNSPolicy, gwDiffObj *reconcilerutils.GatewayDiffs) error {
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

	gatewayWrapper := multicluster.NewGatewayWrapper(gw)
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

		listenerGateways := utils.Filter(clusterGateways, func(cgw multicluster.ClusterGateway) bool {
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
				return fmt.Errorf("failed to delete dns record for listener %s : %w", listener.Name, err)
			}
			return nil
		}

		dnsRecord, err := r.desiredDNSRecord(gatewayWrapper.Gateway, dnsPolicy, listener, listenerGateways, mz)
		if err != nil {
			return err
		}

		err = r.SetOwnerReference(dnsPolicy, dnsRecord)
		if err != nil {
			return err
		}

		err = r.ReconcileResource(ctx, &kuadrantdnsv1alpha1.DNSRecord{}, dnsRecord, dnsRecordBasicMutator)
		if err != nil && !apierrors.IsAlreadyExists(err) {
			log.Error(err, "ReconcileResource failed to create/update DNSRecord resource")
			return err
		}
	}
	return nil
}

func (r *DNSPolicyReconciler) desiredDNSRecord(gateway *gatewayapiv1.Gateway, dnsPolicy *v1alpha1.DNSPolicy, targetListener gatewayapiv1.Listener, clusterGateways []multicluster.ClusterGateway, managedZone *kuadrantdnsv1alpha1.ManagedZone) (*kuadrantdnsv1alpha1.DNSRecord, error) {
	dnsRecord := &kuadrantdnsv1alpha1.DNSRecord{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dnsRecordName(gateway.Name, string(targetListener.Name)),
			Namespace: managedZone.Namespace,
			Labels:    commonDNSRecordLabels(client.ObjectKeyFromObject(gateway), dnsPolicy),
		},
		Spec: kuadrantdnsv1alpha1.DNSRecordSpec{
			ManagedZoneRef: &kuadrantdnsv1alpha1.ManagedZoneReference{
				Name: managedZone.Name,
			},
		},
	}
	dnsRecord.Labels[LabelListenerReference] = string(targetListener.Name)

	mcgTarget, err := multicluster.NewGatewayTarget(gateway, clusterGateways, dnsPolicy.Spec.LoadBalancing)
	if err != nil {
		return nil, fmt.Errorf("failed to create multi cluster gateway target for listener %s : %w", targetListener.Name, err)
	}

	if err = r.dnsHelper.setEndpoints(mcgTarget, dnsRecord, targetListener, dnsPolicy.Spec.RoutingStrategy); err != nil {
		return nil, fmt.Errorf("failed to add dns record dnsTargets %w %v", err, mcgTarget)
	}

	return dnsRecord, nil
}

func (r *DNSPolicyReconciler) deleteGatewayDNSRecords(ctx context.Context, gateway *gatewayapiv1.Gateway, dnsPolicy *v1alpha1.DNSPolicy) error {
	return r.deleteDNSRecordsWithLabels(ctx, commonDNSRecordLabels(client.ObjectKeyFromObject(gateway), dnsPolicy), dnsPolicy.Namespace)
}

func (r *DNSPolicyReconciler) deleteDNSRecords(ctx context.Context, dnsPolicy *v1alpha1.DNSPolicy) error {
	return r.deleteDNSRecordsWithLabels(ctx, policyDNSRecordLabels(dnsPolicy), dnsPolicy.Namespace)
}

func (r *DNSPolicyReconciler) deleteDNSRecordsWithLabels(ctx context.Context, lbls map[string]string, namespace string) error {
	log := crlog.FromContext(ctx)

	listOptions := &client.ListOptions{LabelSelector: labels.SelectorFromSet(lbls), Namespace: namespace}
	recordsList := &kuadrantdnsv1alpha1.DNSRecordList{}
	if err := r.Client().List(ctx, recordsList, listOptions); err != nil {
		return err
	}

	for i := range recordsList.Items {
		if err := r.DeleteResource(ctx, &recordsList.Items[i]); client.IgnoreNotFound(err) != nil {
			log.Error(err, "failed to delete DNSRecord")
			return err
		}
	}
	return nil
}

func dnsRecordBasicMutator(existingObj, desiredObj client.Object) (bool, error) {
	existing, ok := existingObj.(*kuadrantdnsv1alpha1.DNSRecord)
	if !ok {
		return false, fmt.Errorf("%T is not an *kuadrantdnsv1alpha1.DNSRecord", existingObj)
	}
	desired, ok := desiredObj.(*kuadrantdnsv1alpha1.DNSRecord)
	if !ok {
		return false, fmt.Errorf("%T is not an *kuadrantdnsv1alpha1.DNSRecord", desiredObj)
	}

	if reflect.DeepEqual(existing.Spec, desired.Spec) {
		return false, nil
	}

	existing.Spec = desired.Spec

	return true, nil
}
