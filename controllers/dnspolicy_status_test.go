//go:build unit

package controllers

import (
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kuadrantdnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"

	"github.com/kuadrant/kuadrant-operator/api/v1alpha1"
)

func TestPropagateRecordConditions(t *testing.T) {
	healthyProviderCondition := metav1.Condition{
		Type:               "Ready",
		Status:             "True",
		ObservedGeneration: 1,
		LastTransitionTime: metav1.Now(),
		Reason:             "ProviderSuccess",
		Message:            "Provider ensured the dns record",
	}

	healthyProbesCondition := metav1.Condition{
		Type:               "healthProbesSynced",
		Status:             "True",
		ObservedGeneration: 1,
		LastTransitionTime: metav1.Now(),
		Reason:             "AllProbesSynced",
		Message:            "all 1 probes synced successfully",
	}

	healthyProbeCondition := metav1.Condition{
		Type:               "ProbeSynced",
		Status:             "True",
		ObservedGeneration: 1,
		LastTransitionTime: metav1.Now(),
		Reason:             "ProbeSyncSuccessful",
		Message:            "probe (id: 687918a1-7127-4822-87fa-43afec1922cd, address: 172.32.0.253, host: test.domain.com)  synced successfully",
	}

	unhealthyProbesCondition := metav1.Condition{
		Type:               "healthProbesSynced",
		Status:             "False",
		ObservedGeneration: 1,
		LastTransitionTime: metav1.Now(),
		Reason:             "UnsyncedProbes",
		Message:            "some probes have not yet successfully synced to the DNS Provider",
	}

	unhealthyProbeCondition := metav1.Condition{
		Type:               "ProbeSynced",
		Status:             "False",
		ObservedGeneration: 1,
		LastTransitionTime: metav1.Now(),
		Reason:             "DNSProviderError",
		Message:            "probe (id: , address: test.external.com, host: test.domain.com) error from DNS Provider: test.external.com is not a valid value of union type IPAddress.",
	}

	rootHost := "test.domain.com"

	tests := []struct {
		Name         string
		PolicyStatus *v1alpha1.DNSPolicyStatus
		Records      []*kuadrantdnsv1alpha1.DNSRecord
		Validate     func(*testing.T, *v1alpha1.DNSPolicyStatus)
	}{
		{
			Name: "Healthy conditions not propagated",
			Records: []*kuadrantdnsv1alpha1.DNSRecord{
				{
					Spec: kuadrantdnsv1alpha1.DNSRecordSpec{RootHost: rootHost},
					Status: kuadrantdnsv1alpha1.DNSRecordStatus{
						Conditions: []metav1.Condition{
							healthyProviderCondition,
						},
						HealthCheck: &kuadrantdnsv1alpha1.HealthCheckStatus{
							Conditions: []metav1.Condition{
								healthyProbesCondition,
							},
							Probes: []kuadrantdnsv1alpha1.HealthCheckStatusProbe{
								{
									Conditions: []metav1.Condition{
										healthyProbeCondition,
									},
								},
							},
						},
					},
				},
			},
			PolicyStatus: &v1alpha1.DNSPolicyStatus{},
			Validate: func(t *testing.T, policyStatus *v1alpha1.DNSPolicyStatus) {
				if conditions, ok := policyStatus.RecordConditions[rootHost]; ok {
					t.Fatalf("expected no probe conditions for root host, found %v", len(conditions))
				}
			},
		},
		{
			Name: "Unhealthy conditions are propagated",
			Records: []*kuadrantdnsv1alpha1.DNSRecord{
				{
					Spec: kuadrantdnsv1alpha1.DNSRecordSpec{RootHost: rootHost},
					Status: kuadrantdnsv1alpha1.DNSRecordStatus{
						Conditions: []metav1.Condition{
							healthyProviderCondition,
						},
						HealthCheck: &kuadrantdnsv1alpha1.HealthCheckStatus{
							Conditions: []metav1.Condition{
								unhealthyProbesCondition,
							},
							Probes: []kuadrantdnsv1alpha1.HealthCheckStatusProbe{
								{
									Conditions: []metav1.Condition{
										unhealthyProbeCondition,
									},
								},
							},
						},
					},
				},
			},
			PolicyStatus: &v1alpha1.DNSPolicyStatus{},
			Validate: func(t *testing.T, policyStatus *v1alpha1.DNSPolicyStatus) {
				if conditions, ok := policyStatus.RecordConditions[rootHost]; !ok {
					t.Fatalf("expected probe conditions for root host, found none")
				} else {
					if len(conditions) != 2 {
						t.Fatalf("expected 2 probe conditions on policy, found %v", len(conditions))
					}
					for _, c := range conditions {
						if !reflect.DeepEqual(c, unhealthyProbeCondition) && !reflect.DeepEqual(c, unhealthyProbesCondition) {
							t.Fatalf("unexpected condition: %v", c)
						}
					}
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			propagateRecordConditions(tt.Records, tt.PolicyStatus)
			tt.Validate(t, tt.PolicyStatus)
		})
	}
}
