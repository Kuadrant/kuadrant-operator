package controllers

import (
	"context"
	"fmt"
	"reflect"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crlog "sigs.k8s.io/controller-runtime/pkg/log"
	externaldns "sigs.k8s.io/external-dns/endpoint"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantdnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/pkg/builder"

	"github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	reconcilerutils "github.com/kuadrant/kuadrant-operator/pkg/library/reconcilers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

var (
	ErrNoRoutes    = fmt.Errorf("no routes attached to any gateway listeners")
	ErrNoAddresses = fmt.Errorf("no valid status addresses to use on gateway")
)

func (r *DNSPolicyReconciler) reconcileDNSRecords(ctx context.Context, dnsPolicy *v1alpha1.DNSPolicy, gwDiffObj *reconcilerutils.GatewayDiffs) error {
	log := crlog.FromContext(ctx)
	log.V(3).Info("reconciling dns records")

	// Reconcile DNSRecords for each gateway directly referred by the policy (existing and new)
	for _, gw := range append(gwDiffObj.GatewaysWithValidPolicyRef, gwDiffObj.GatewaysMissingPolicyRef...) {
		log.V(1).Info("reconcileDNSRecords: gateway with valid or missing policy ref", "key", gw.Key())
		if err := r.reconcileGatewayDNSRecords(ctx, gw.Gateway, dnsPolicy); err != nil {
			return fmt.Errorf("reconciling dns records for gateway %v: error %w", gw.Gateway.Name, err)
		}
	}
	return nil
}

func (r *DNSPolicyReconciler) reconcileGatewayDNSRecords(ctx context.Context, gateway *gatewayapiv1.Gateway, dnsPolicy *v1alpha1.DNSPolicy) error {
	log := crlog.FromContext(ctx)
	clusterID, err := utils.GetClusterUID(ctx, r.Client())
	if err != nil {
		return fmt.Errorf("failed to generate cluster ID: %w", err)
	}
	gw := gateway.DeepCopy()
	gatewayWrapper := NewGatewayWrapper(gw)
	// modify the status addresses based on any that need to be excluded
	if err := gatewayWrapper.RemoveExcludedStatusAddresses(dnsPolicy); err != nil {
		return fmt.Errorf("failed to reconcile gateway dns records error: %w ", err)
	}

	if err = r.dnsHelper.removeDNSForDeletedListeners(ctx, gw); err != nil {
		log.V(3).Info("error removing DNS for deleted listeners")
		return err
	}

	log.V(3).Info("checking gateway for attached routes ", "gateway", gw.Name)
	var totalPolicyRecords int32
	var gatewayHasAttachedRoutes = false

	if len(gw.Status.Addresses) == 0 {
		return ErrNoAddresses
	}

	for _, listener := range gw.Spec.Listeners {
		if listener.Hostname == nil || *listener.Hostname == "" {
			log.Info("skipping listener no hostname assigned", "listener", listener.Name, "in ns ", gateway.Namespace)
			continue
		}

		hasAttachedRoute := false
		for _, statusListener := range gateway.Status.Listeners {
			if string(listener.Name) == string(statusListener.Name) {
				hasAttachedRoute = statusListener.AttachedRoutes > 0
			}
		}

		if hasAttachedRoute {
			gatewayHasAttachedRoutes = true
		}
		if !hasAttachedRoute {
			// delete record
			log.V(1).Info("no cluster gateways, deleting DNS record", " for listener ", listener.Name)
			if err := r.dnsHelper.deleteDNSRecordForListener(ctx, gw, listener); client.IgnoreNotFound(err) != nil {
				return fmt.Errorf("failed to delete dns record for listener %s : %w", listener.Name, err)
			}
			continue
		}

		dnsRecord, err := r.desiredDNSRecord(gw, clusterID, dnsPolicy, listener)
		if err != nil {
			return err
		}

		err = r.SetOwnerReference(dnsPolicy, dnsRecord)
		if err != nil {
			return err
		}

		if len(dnsRecord.Spec.Endpoints) == 0 {
			log.V(1).Info("no endpoint addresses for DNSRecord ", "removing any records for listener", listener)
			if err := r.dnsHelper.deleteDNSRecordForListener(ctx, gatewayWrapper, listener); client.IgnoreNotFound(err) != nil {
				return err
			}
			//return fmt.Errorf("no valid addresses for DNSRecord endpoints. Check allowedAddresses")
			continue
		}

		err = r.ReconcileResource(ctx, &kuadrantdnsv1alpha1.DNSRecord{}, dnsRecord, dnsRecordBasicMutator)
		if err != nil && !apierrors.IsAlreadyExists(err) {
			log.Error(err, "ReconcileResource failed to create/update DNSRecord resource")
			return err
		}
		totalPolicyRecords++
	}
	dnsPolicy.Status.TotalRecords = totalPolicyRecords
	if !gatewayHasAttachedRoutes {
		return ErrNoRoutes
	}
	return nil
}

func (r *DNSPolicyReconciler) desiredDNSRecord(gateway *gatewayapiv1.Gateway, clusterID string, dnsPolicy *v1alpha1.DNSPolicy, targetListener gatewayapiv1.Listener) (*kuadrantdnsv1alpha1.DNSRecord, error) {
	rootHost := string(*targetListener.Hostname)
	var healthCheckSpec *kuadrantdnsv1alpha1.HealthCheckSpec

	if dnsPolicy.Spec.HealthCheck != nil {
		healthCheckSpec = &kuadrantdnsv1alpha1.HealthCheckSpec{
			Path:                 dnsPolicy.Spec.HealthCheck.Path,
			Port:                 dnsPolicy.Spec.HealthCheck.Port,
			Protocol:             dnsPolicy.Spec.HealthCheck.Protocol,
			FailureThreshold:     dnsPolicy.Spec.HealthCheck.FailureThreshold,
			Interval:             dnsPolicy.Spec.HealthCheck.Interval,
			AdditionalHeadersRef: dnsPolicy.Spec.HealthCheck.AdditionalHeadersRef,
		}
	}
	dnsRecord := &kuadrantdnsv1alpha1.DNSRecord{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dnsRecordName(gateway.Name, string(targetListener.Name)),
			Namespace: dnsPolicy.Namespace,
			Labels:    commonDNSRecordLabels(client.ObjectKeyFromObject(gateway), dnsPolicy),
		},
		Spec: kuadrantdnsv1alpha1.DNSRecordSpec{
			RootHost: rootHost,
			ProviderRef: kuadrantdnsv1alpha1.ProviderRef{
				// Currently we only allow a single providerRef to be added. When that changes, we will need to update this to deal with multiple records.
				Name: dnsPolicy.Spec.ProviderRefs[0].Name,
			},
			HealthCheck: healthCheckSpec,
		},
	}
	dnsRecord.Labels[LabelListenerReference] = string(targetListener.Name)

	endpoints, err := buildEndpoints(clusterID, string(*targetListener.Hostname), gateway, dnsPolicy)
	if err != nil {
		return nil, fmt.Errorf("failed to generate dns record for a gateway %s in %s ns: %w", gateway.Name, gateway.Namespace, err)
	}
	dnsRecord.Spec.Endpoints = endpoints
	return dnsRecord, nil
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

func buildEndpoints(clusterID, hostname string, gateway *gatewayapiv1.Gateway, policy *v1alpha1.DNSPolicy) ([]*externaldns.Endpoint, error) {
	endpointBuilder := builder.NewEndpointsBuilder(NewGatewayWrapper(gateway), hostname)

	if policy.Spec.LoadBalancing != nil {
		endpointBuilder.WithLoadBalancingFor(
			clusterID,
			policy.Spec.LoadBalancing.Weight,
			policy.Spec.LoadBalancing.Geo,
			policy.Spec.LoadBalancing.DefaultGeo)
	}

	return endpointBuilder.Build()
}
