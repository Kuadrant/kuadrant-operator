package controllers

import (
	"fmt"
	"net"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/env"
	externaldns "sigs.k8s.io/external-dns/endpoint"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantdnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/pkg/builder"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
)

const (
	LabelListenerReference = "kuadrant.io/listener-name"
)

func dnsPolicyDefaultTTL() (int, error) {
	ttl, err := env.GetInt("DNS_DEFAULT_TTL", builder.DefaultTTL)
	if err != nil {
		return ttl, fmt.Errorf("DNS_DEFAULT_TTL env value could not be parsed as int, default %d will be used: %w", builder.DefaultTTL, err)
	}
	return ttl, nil
}

func dnsPolicyDefaultCnameTTL() (int, error) {
	ttl, err := env.GetInt("DNS_DEFAULT_LB_TTL", builder.DefaultLoadBalancedTTL)
	if err != nil {
		return ttl, fmt.Errorf("DNS_DEFAULT_LB_TTL env value could not be parsed as int, default %d will be used: %w", builder.DefaultLoadBalancedTTL, err)
	}
	return ttl, nil
}

func dnsRecordName(gatewayName, listenerName string) string {
	return fmt.Sprintf("%s-%s", gatewayName, listenerName)
}

func desiredDNSRecord(gateway *gatewayapiv1.Gateway, clusterID string, dnsPolicy *kuadrantv1.DNSPolicy, targetListener gatewayapiv1.Listener, defaultTTL int, defaultLoadBalancedTTL int) (*kuadrantdnsv1alpha1.DNSRecord, error) {
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
			Labels:    CommonLabels(),
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       DNSRecordKind,
			APIVersion: kuadrantdnsv1alpha1.GroupVersion.String(),
		},
		Spec: kuadrantdnsv1alpha1.DNSRecordSpec{
			RootHost:    rootHost,
			HealthCheck: healthCheckSpec,
		},
	}

	if dnsPolicy.Spec.Delegate {
		dnsRecord.Spec.Delegate = true
	}

	// Currently we only allow a single providerRef to be added. When that changes, we will need to update this to deal with multiple records.
	if len(dnsPolicy.Spec.ProviderRefs) > 0 {
		dnsRecord.Spec.ProviderRef = &dnsPolicy.Spec.ProviderRefs[0]
	}

	dnsRecord.Labels[LabelListenerReference] = string(targetListener.Name)

	endpoints, err := buildEndpoints(clusterID, string(*targetListener.Hostname), gateway, dnsPolicy, defaultTTL, defaultLoadBalancedTTL)
	if err != nil {
		return nil, fmt.Errorf("failed to generate dns record for a gateway %s in %s ns: %w", gateway.Name, gateway.Namespace, err)
	}
	dnsRecord.Spec.Endpoints = endpoints
	return dnsRecord, nil
}

// GatewayWrapper is a wrapper for gateway to implement interface from the builder
type GatewayWrapper struct {
	*gatewayapiv1.Gateway
	excludedAddresses []string
}

func NewGatewayWrapper(gateway *gatewayapiv1.Gateway) *GatewayWrapper {
	return &GatewayWrapper{Gateway: gateway}
}

func (g *GatewayWrapper) GetAddresses() []builder.TargetAddress {
	addresses := make([]builder.TargetAddress, len(g.Status.Addresses))
	for i, address := range g.Status.Addresses {
		addresses[i] = builder.TargetAddress{
			Type:  builder.AddressType(*address.Type),
			Value: address.Value,
		}
	}
	return addresses
}

func (g *GatewayWrapper) RemoveExcludedStatusAddresses(p *kuadrantv1.DNSPolicy) error {
	g.excludedAddresses = p.Spec.ExcludeAddresses
	newAddresses := []gatewayapiv1.GatewayStatusAddress{}
	for _, address := range g.Status.Addresses {
		found := false
		for _, exclude := range p.Spec.ExcludeAddresses {
			//Only a CIDR will have  / in the address so attempt to parse fail if not valid
			if strings.Contains(exclude, "/") {
				_, network, err := net.ParseCIDR(exclude)
				if err != nil {
					return fmt.Errorf("could not parse the CIDR from the excludeAddresses field %w", err)
				}
				ip := net.ParseIP(address.Value)
				// only check addresses that are actually IPs
				if ip != nil && network.Contains(ip) {
					found = true
					break
				}
			}
			if exclude == address.Value {
				found = true
				break
			}
		}
		if !found {
			newAddresses = append(newAddresses, address)
		}
	}
	// setting this in memory only won't be saved to actual gateway
	g.Status.Addresses = newAddresses
	return nil
}

func buildEndpoints(clusterID, hostname string, gateway *gatewayapiv1.Gateway, policy *kuadrantv1.DNSPolicy, defaultTTL int, defaultLoadBalancedTTL int) ([]*externaldns.Endpoint, error) {
	gw := gateway.DeepCopy()
	gatewayWrapper := NewGatewayWrapper(gw)
	// modify the status addresses based on any that need to be excluded
	if err := gatewayWrapper.RemoveExcludedStatusAddresses(policy); err != nil {
		return nil, fmt.Errorf("failed to reconcile gateway dns records error: %w ", err)
	}
	endpointBuilder := builder.NewEndpointsBuilder(gatewayWrapper, hostname).
		SetDefaultTTL(defaultTTL).
		SetDefaultLoadBalancedTTL(defaultLoadBalancedTTL)

	if policy.Spec.LoadBalancing != nil {
		endpointBuilder.WithLoadBalancingFor(
			clusterID,
			policy.Spec.LoadBalancing.Weight,
			policy.Spec.LoadBalancing.Geo,
			policy.Spec.LoadBalancing.DefaultGeo)
	}

	return endpointBuilder.Build()
}
