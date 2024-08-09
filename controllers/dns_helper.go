package controllers

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantdnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/pkg/builder"

	"github.com/kuadrant/kuadrant-operator/api/v1alpha1"
)

const (
	LabelGatewayReference  = "kuadrant.io/gateway"
	LabelGatewayNSRef      = "kuadrant.io/gateway-namespace"
	LabelListenerReference = "kuadrant.io/listener-name"
)

type dnsHelper struct {
	client.Client
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
		rootHostMatches := false
		for _, listener := range upstreamGateway.Spec.Listeners {
			if listener.Name == gatewayapiv1.SectionName(dnsRecord.Labels[LabelListenerReference]) {
				listenerExists = true
				rootHostMatches = string(*listener.Hostname) == dnsRecord.Spec.RootHost
				break
			}
		}
		if !listenerExists || !rootHostMatches {
			if err := dh.Delete(ctx, &dnsList.Items[i], &client.DeleteOptions{}); client.IgnoreNotFound(err) != nil {
				return err
			}
		}
	}
	return nil
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

// GatewayWrapper is a wrapper for gateway to implement interface form the builder
type GatewayWrapper struct {
	*gatewayapiv1.Gateway
}

func NewGatewayWrapper(gateway *gatewayapiv1.Gateway) *GatewayWrapper {
	return &GatewayWrapper{Gateway: gateway}
}

func (g GatewayWrapper) GetAddresses() []builder.TargetAddress {
	addresses := make([]builder.TargetAddress, len(g.Status.Addresses))
	for i, address := range g.Status.Addresses {
		addresses[i] = builder.TargetAddress{
			Type:  builder.AddressType(*address.Type),
			Value: address.Value,
		}
	}
	return addresses
}
