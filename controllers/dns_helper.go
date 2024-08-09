package controllers

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/net/publicsuffix"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantdnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"

	"github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

const (
	LabelGatewayReference  = "kuadrant.io/gateway"
	LabelGatewayNSRef      = "kuadrant.io/gateway-namespace"
	LabelListenerReference = "kuadrant.io/listener-name"
)

var (
	//ErrUnknownRoutingStrategy = fmt.Errorf("unknown routing strategy")
	ErrNoManagedZoneForHost = fmt.Errorf("no managed zone for host")
)

type dnsHelper struct {
	client.Client
}

func findMatchingManagedZone(originalHost, host string, zones []kuadrantdnsv1alpha1.ManagedZone) (*kuadrantdnsv1alpha1.ManagedZone, string, error) {
	if len(zones) == 0 {
		return nil, "", fmt.Errorf("%w : %s", ErrNoManagedZoneForHost, host)
	}
	host = strings.ToLower(host)
	//get the TLD from this host
	tld, _ := publicsuffix.PublicSuffix(host)

	//The host is a TLD, so we now know `originalHost` can't possibly have a valid `ManagedZone` available.
	if host == tld {
		return nil, "", fmt.Errorf("no valid zone found for host: %v", originalHost)
	}

	hostParts := strings.SplitN(host, ".", 2)
	if len(hostParts) < 2 {
		return nil, "", fmt.Errorf("no valid zone found for host: %s", originalHost)
	}
	parentDomain := hostParts[1]

	// We do not currently support creating records for Apex domains, and a ManagedZone represents an Apex domain, as such
	// we should never be trying to find a managed zone that matches the `originalHost` exactly. Instead, we just continue
	// on to the next possible valid host to try i.e. the parent domain.
	if host == originalHost {
		return findMatchingManagedZone(originalHost, parentDomain, zones)
	}

	zone, ok := utils.Find(zones, func(zone kuadrantdnsv1alpha1.ManagedZone) bool {
		return strings.ToLower(zone.Spec.DomainName) == host
	})

	if ok {
		subdomain := strings.Replace(strings.ToLower(originalHost), "."+strings.ToLower(zone.Spec.DomainName), "", 1)
		return zone, subdomain, nil
	}
	return findMatchingManagedZone(originalHost, parentDomain, zones)
}

func commonDNSRecordLabels(gwKey client.ObjectKey, p *v1alpha1.DNSPolicy) map[string]string {
	commonLabels := map[string]string{}
	for k, v := range policyDNSRecordLabels(p) {
		commonLabels[k] = v
	}
	for k, v := range gatewayDNSRecordLabels(gwKey) {
		commonLabels[k] = v
	}
	return commonLabels
}

func policyDNSRecordLabels(p *v1alpha1.DNSPolicy) map[string]string {
	return map[string]string{
		p.DirectReferenceAnnotationName():                              p.Name,
		fmt.Sprintf("%s-namespace", p.DirectReferenceAnnotationName()): p.Namespace,
	}
}

func gatewayDNSRecordLabels(gwKey client.ObjectKey) map[string]string {
	return map[string]string{
		LabelGatewayNSRef:     gwKey.Namespace,
		LabelGatewayReference: gwKey.Name,
	}
}

// removeDNSForDeletedListeners remove any DNSRecords that are associated with listeners that no longer exist in this gateway
func (dh *dnsHelper) removeDNSForDeletedListeners(ctx context.Context, upstreamGateway *gatewayapiv1.Gateway) error {
	dnsList := &kuadrantdnsv1alpha1.DNSRecordList{}
	//List all dns records that belong to this gateway
	labelSelector := &client.MatchingLabels{
		LabelGatewayReference: upstreamGateway.Name,
	}
	if err := dh.List(ctx, dnsList, labelSelector, &client.ListOptions{Namespace: upstreamGateway.Namespace}); err != nil {
		return err
	}

	for i, dnsRecord := range dnsList.Items {
		listenerExists := false
		for _, listener := range upstreamGateway.Spec.Listeners {
			if listener.Name == gatewayapiv1.SectionName(dnsRecord.Labels[LabelListenerReference]) {
				listenerExists = true
				break
			}
		}
		if !listenerExists {
			if err := dh.Delete(ctx, &dnsList.Items[i], &client.DeleteOptions{}); client.IgnoreNotFound(err) != nil {
				return err
			}
		}
	}
	return nil
}

func (dh *dnsHelper) getManagedZoneForListener(ctx context.Context, ns string, listener gatewayapiv1.Listener) (*kuadrantdnsv1alpha1.ManagedZone, error) {
	var managedZones kuadrantdnsv1alpha1.ManagedZoneList
	if err := dh.List(ctx, &managedZones, client.InNamespace(ns)); err != nil {
		log.FromContext(ctx).Error(err, "unable to list managed zones for gateway ", "in ns", ns)
		return nil, err
	}
	host := string(*listener.Hostname)
	mz, _, err := findMatchingManagedZone(host, host, managedZones.Items)
	return mz, err
}

func dnsRecordName(gatewayName, listenerName string) string {
	return fmt.Sprintf("%s-%s", gatewayName, listenerName)
}

func (dh *dnsHelper) deleteDNSRecordForListener(ctx context.Context, owner metav1.Object, listener gatewayapiv1.Listener) error {
	recordName := dnsRecordName(owner.GetName(), string(listener.Name))
	dnsRecord := kuadrantdnsv1alpha1.DNSRecord{
		ObjectMeta: metav1.ObjectMeta{
			Name:      recordName,
			Namespace: owner.GetNamespace(),
		},
	}
	return dh.Delete(ctx, &dnsRecord, &client.DeleteOptions{})
}
