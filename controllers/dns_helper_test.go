package controllers_test

import (
	"testing"

	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/controllers"
)

func TestRemoveExcludedStatusAddresses(t *testing.T) {
	ipaddress := gatewayapiv1.IPAddressType
	hostaddress := gatewayapiv1.HostnameAddressType
	testCases := []struct {
		Name      string
		Gateway   *gatewayapiv1.Gateway
		DNSPolicy *v1alpha1.DNSPolicy
		Validate  func(t *testing.T, g *gatewayapiv1.GatewayStatus)
		ExpectErr bool
	}{
		{
			Name: "ensure addresses in ingore are are removed from status",
			Gateway: &gatewayapiv1.Gateway{
				Status: gatewayapiv1.GatewayStatus{
					Addresses: []gatewayapiv1.GatewayStatusAddress{
						{
							Type:  &ipaddress,
							Value: "1.1.1.1",
						},
						{
							Type:  &hostaddress,
							Value: "example.com",
						},
					},
				},
			},
			DNSPolicy: &v1alpha1.DNSPolicy{
				Spec: v1alpha1.DNSPolicySpec{
					ExcludeAddresses: []string{
						"1.1.1.1",
					},
				},
			},
			Validate: func(t *testing.T, g *gatewayapiv1.GatewayStatus) {
				if len(g.Addresses) != 1 {
					t.Fatalf("expected a single address but got %v ", len(g.Addresses))
				}
				for _, addr := range g.Addresses {
					if addr.Value == "1.1.1.1" {
						t.Fatalf("did not expect address %s to be present", "1.1.1.1")
					}
				}
			},
		},
		{
			Name: "ensure all addresses if nothing ignored",
			Gateway: &gatewayapiv1.Gateway{
				Status: gatewayapiv1.GatewayStatus{
					Addresses: []gatewayapiv1.GatewayStatusAddress{
						{
							Type:  &ipaddress,
							Value: "1.1.1.1",
						},
						{
							Type:  &hostaddress,
							Value: "example.com",
						},
					},
				},
			},
			DNSPolicy: &v1alpha1.DNSPolicy{
				Spec: v1alpha1.DNSPolicySpec{
					ExcludeAddresses: []string{},
				},
			},
			Validate: func(t *testing.T, g *gatewayapiv1.GatewayStatus) {
				if len(g.Addresses) != 2 {
					t.Fatalf("expected a both address but got %v ", len(g.Addresses))
				}
			},
		},
		{
			Name: "ensure addresses removed if CIDR is set and hostname",
			Gateway: &gatewayapiv1.Gateway{
				Status: gatewayapiv1.GatewayStatus{
					Addresses: []gatewayapiv1.GatewayStatusAddress{
						{
							Type:  &ipaddress,
							Value: "1.1.1.1",
						},
						{
							Type:  &hostaddress,
							Value: "example.com",
						},
						{
							Type:  &ipaddress,
							Value: "81.17.21.22",
						},
					},
				},
			},
			DNSPolicy: &v1alpha1.DNSPolicy{
				Spec: v1alpha1.DNSPolicySpec{
					ExcludeAddresses: []string{
						"1.1.0.0/16",
						"10.0.0.1/32",
						"example.com",
					},
				},
			},
			Validate: func(t *testing.T, g *gatewayapiv1.GatewayStatus) {
				if len(g.Addresses) != 1 {
					t.Fatalf("expected only a single address but got %v %v ", len(g.Addresses), g.Addresses)
				}
				if g.Addresses[0].Value != "81.17.21.22" {
					t.Fatalf("expected the only remaining address to be 81.17.21.22 but got %s", g.Addresses[0].Value)
				}
			},
		},
		{
			Name:      "ensure invalid CIDR causes error",
			ExpectErr: true,
			Gateway: &gatewayapiv1.Gateway{
				Status: gatewayapiv1.GatewayStatus{
					Addresses: []gatewayapiv1.GatewayStatusAddress{
						{
							Type:  &ipaddress,
							Value: "1.1.1.1",
						},
						{
							Type:  &hostaddress,
							Value: "example.com",
						},
						{
							Type:  &ipaddress,
							Value: "81.17.21.22",
						},
					},
				},
			},
			DNSPolicy: &v1alpha1.DNSPolicy{
				Spec: v1alpha1.DNSPolicySpec{
					ExcludeAddresses: []string{
						"1.1.0.0/161",
						"example.com",
					},
				},
			},
			Validate: func(t *testing.T, g *gatewayapiv1.GatewayStatus) {},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			gw := controllers.NewGatewayWrapper(tc.Gateway)
			err := gw.RemoveExcludedStatusAddresses(tc.DNSPolicy)
			if err != nil && !tc.ExpectErr {
				t.Fatalf("unexpected error %s", err)
			}
			if tc.ExpectErr && err == nil {
				t.Fatalf("expected an error but got none")
			}
			tc.Validate(t, &gw.Status)
		})
	}
}
