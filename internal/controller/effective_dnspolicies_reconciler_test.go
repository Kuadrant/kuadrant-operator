package controllers

import (
	"context"
	"fmt"
	"slices"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	externaldns "sigs.k8s.io/external-dns/endpoint"
	v1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantdnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/policy-machinery/machinery"
)

func Test_canUpdateDNSRecord(t *testing.T) {
	tests := []struct {
		name    string
		current *kuadrantdnsv1alpha1.DNSRecord
		desired *kuadrantdnsv1alpha1.DNSRecord
		want    bool
	}{
		{
			name: "different root hosts",
			current: &kuadrantdnsv1alpha1.DNSRecord{
				Spec: kuadrantdnsv1alpha1.DNSRecordSpec{
					RootHost: "foo.example.com",
				},
			},
			desired: &kuadrantdnsv1alpha1.DNSRecord{
				Spec: kuadrantdnsv1alpha1.DNSRecordSpec{
					RootHost: "bar.example.com",
				},
			},
			want: false,
		},
		{
			name: "same root hosts",
			current: &kuadrantdnsv1alpha1.DNSRecord{
				Spec: kuadrantdnsv1alpha1.DNSRecordSpec{
					RootHost: "foo.example.com",
				},
			},
			desired: &kuadrantdnsv1alpha1.DNSRecord{
				Spec: kuadrantdnsv1alpha1.DNSRecordSpec{
					RootHost: "foo.example.com",
				},
			},
			want: true,
		},
		{
			name: "different record type same dnsnames",
			current: &kuadrantdnsv1alpha1.DNSRecord{
				Spec: kuadrantdnsv1alpha1.DNSRecordSpec{
					Endpoints: []*externaldns.Endpoint{
						{
							DNSName:    "foo.example.com",
							RecordType: "A",
						},
					},
				},
			},
			desired: &kuadrantdnsv1alpha1.DNSRecord{
				Spec: kuadrantdnsv1alpha1.DNSRecordSpec{
					Endpoints: []*externaldns.Endpoint{
						{
							DNSName:    "foo.example.com",
							RecordType: "CNAME",
						},
					},
				},
			},
			want: false,
		},
		{
			name: "same record type same dnsnames",
			current: &kuadrantdnsv1alpha1.DNSRecord{
				Spec: kuadrantdnsv1alpha1.DNSRecordSpec{
					Endpoints: []*externaldns.Endpoint{
						{
							DNSName:    "foo.example.com",
							RecordType: "A",
						},
					},
				},
			},
			desired: &kuadrantdnsv1alpha1.DNSRecord{
				Spec: kuadrantdnsv1alpha1.DNSRecordSpec{
					Endpoints: []*externaldns.Endpoint{
						{
							DNSName:    "foo.example.com",
							RecordType: "A",
						},
					},
				},
			},
			want: true,
		},
		{
			name: "multiple endpoints",
			current: &kuadrantdnsv1alpha1.DNSRecord{
				Spec: kuadrantdnsv1alpha1.DNSRecordSpec{
					Endpoints: []*externaldns.Endpoint{
						{
							DNSName:    "foo.example.com",
							RecordType: "A",
						},
						{
							DNSName:    "baz.example.com",
							RecordType: "CNAME",
						},
					},
				},
			},
			desired: &kuadrantdnsv1alpha1.DNSRecord{
				Spec: kuadrantdnsv1alpha1.DNSRecordSpec{
					Endpoints: []*externaldns.Endpoint{
						{
							DNSName:    "foo.example.com",
							RecordType: "A",
						},
						{
							DNSName:    "bar.example.com",
							RecordType: "CNAME",
						},
					},
				},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := canUpdateDNSRecord(context.Background(), tt.current, tt.desired); got != tt.want {
				t.Errorf("canUpdateDNSRecord() = %v, want %v", got, tt.want)
			}
		})
	}
}

type gatewayListeners struct {
	hostname         v1.Hostname
	hasAttachedRoute bool
}

func GatewayWithHosts(listenerHosts []gatewayListeners) *v1.Gateway {
	g := &v1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       "Gateway",
			APIVersion: v1.GroupVersion.String(),
		},
		Spec: v1.GatewaySpec{},
	}

	for i, gwl := range listenerHosts {
		lname := v1.SectionName(fmt.Sprintf("listener-%d", i))
		g.Spec.Listeners = append(g.Spec.Listeners, v1.Listener{
			Name:     lname,
			Hostname: &gwl.hostname,
			Protocol: v1.HTTPProtocolType,
		})
		if gwl.hasAttachedRoute {
			g.Status.Listeners = []v1.ListenerStatus{
				{
					AttachedRoutes: 1,
					Name:           lname,
				},
			}
		}
	}
	return g
}

func buildHTTPRoutForTestGateway(name, sectionName string, hostnames []v1.Hostname) *v1.HTTPRoute {
	r := &v1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       "HTTPRoute",
			APIVersion: v1.GroupVersion.String(),
		},
		Spec: v1.HTTPRouteSpec{
			Hostnames: hostnames,
			CommonRouteSpec: v1.CommonRouteSpec{
				ParentRefs: []v1.ParentReference{
					{
						Name:      v1.ObjectName("test"),
						Namespace: ptr.To(v1.Namespace("default")),
					},
				},
			},
		},
	}

	if sectionName != "" {
		r.Spec.ParentRefs[0].SectionName = (*v1.SectionName)(&sectionName)
	}
	return r
}

func TestDNSNamesForGateway(t *testing.T) {
	expected := map[string][]v1.Hostname{
		"gateway.gateway.networking.k8s.io:default/test#listener-0": {"exact.ie"},
		"gateway.gateway.networking.k8s.io:default/test#listener-1": {"api.wildcard.com", "shop.wildcard.com", "catalog.wildcard.com", "checkout.wildcard.com"},
	}

	gateways := []*v1.Gateway{GatewayWithHosts([]gatewayListeners{{hostname: v1.Hostname("exact.ie"), hasAttachedRoute: true}, {hostname: v1.Hostname("*.wildcard.com"), hasAttachedRoute: true}, {hostname: v1.Hostname("noroutes.net"), hasAttachedRoute: false}})}

	httpRoutes := []*v1.HTTPRoute{
		buildHTTPRoutForTestGateway("test1", "", []v1.Hostname{"exact.ie", "api.wildcard.com"}),
		buildHTTPRoutForTestGateway("test2", "", []v1.Hostname{"api.doesntexist.com", "shop.wildcard.com", "catalog.wildcard.com"}),
		buildHTTPRoutForTestGateway("test3", "", []v1.Hostname{"exact.ie"}),
		//section name set.
		buildHTTPRoutForTestGateway("test4", "listener-1", []v1.Hostname{"exact.ie", "checkout.wildcard.com"}),
	}

	opts := []machinery.GatewayAPITopologyOptionsFunc{
		machinery.WithGateways(gateways...),
		machinery.WithHTTPRoutes(httpRoutes...),
		machinery.ExpandGatewayListeners(),
	}
	topology, err := machinery.NewGatewayAPITopology(opts...)
	if err != nil {
		t.Fatalf("%s", err)
	}

	ctx := context.TODO()

	names := dnsNamesForGatewayFromRoutes(ctx, topology, &machinery.Gateway{Gateway: gateways[0]})
	for listener, hosts := range expected {
		v, ok := names[listener]
		if !ok {
			t.Fatalf("expected listener %s to be present in %v", listener, names)
		}
		matchedHosts := []v1.Hostname{}
		for _, matched := range v {
			matchedHosts = append(matchedHosts, matched.hostname)
		}
		if !slices.Equal(hosts, matchedHosts) {
			t.Fatalf("expected the host for listener %s to match %v but got %v", listener, hosts, matchedHosts)
		}
	}
	if _, ok := names["gateway.gateway.networking.k8s.io:default/test#listener-3"]; ok {
		t.Fatalf("did not expect any results for listener 3 as there are no httproutes that should be attached to this listener")
	}
}
