package controllers

import (
	"fmt"
	"net"
	"strings"

	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kuadrant/dns-operator/pkg/builder"

	"github.com/kuadrant/kuadrant-operator/api/v1alpha1"
)

const (
	LabelGatewayReference  = "kuadrant.io/gateway"
	LabelListenerReference = "kuadrant.io/listener-name"
)

func dnsRecordName(gatewayName, listenerName string) string {
	return fmt.Sprintf("%s-%s", gatewayName, listenerName)
}

// GatewayWrapper is a wrapper for gateway to implement interface form the builder
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

func (g *GatewayWrapper) RemoveExcludedStatusAddresses(p *v1alpha1.DNSPolicy) error {
	g.excludedAddresses = p.Spec.ExcludeAddresses
	newAddresses := []gatewayapiv1.GatewayStatusAddress{}
	for _, address := range g.Gateway.Status.Addresses {
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
	// setting this in memory only wont be saved to actual gateway
	g.Status.Addresses = newAddresses
	return nil
}
