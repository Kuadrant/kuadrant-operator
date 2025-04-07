//go:build unit

package controllers

import (
	"context"
	"testing"

	externaldns "sigs.k8s.io/external-dns/endpoint"

	kuadrantdnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"
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
