/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kuadrantdnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"

	"github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

var NegativePolarityConditions []string

func emitConditionMetrics(dnsPolicy *v1alpha1.DNSPolicy) {
	readyStatus := meta.FindStatusCondition(dnsPolicy.Status.Conditions, ReadyConditionType)
	if readyStatus == nil {
		dnsPolicyReady.WithLabelValues(dnsPolicy.Name, dnsPolicy.Namespace, "true").Set(0)
		dnsPolicyReady.WithLabelValues(dnsPolicy.Name, dnsPolicy.Namespace, "false").Set(0)
		dnsPolicyReady.WithLabelValues(dnsPolicy.Name, dnsPolicy.Namespace, "unknown").Set(1)
	} else if readyStatus.Status != metav1.ConditionTrue {
		dnsPolicyReady.WithLabelValues(dnsPolicy.Name, dnsPolicy.Namespace, "true").Set(0)
		dnsPolicyReady.WithLabelValues(dnsPolicy.Name, dnsPolicy.Namespace, "false").Set(1)
		dnsPolicyReady.WithLabelValues(dnsPolicy.Name, dnsPolicy.Namespace, "unknown").Set(0)
	} else {
		dnsPolicyReady.WithLabelValues(dnsPolicy.Name, dnsPolicy.Namespace, "true").Set(1)
		dnsPolicyReady.WithLabelValues(dnsPolicy.Name, dnsPolicy.Namespace, "false").Set(0)
		dnsPolicyReady.WithLabelValues(dnsPolicy.Name, dnsPolicy.Namespace, "unknown").Set(0)
	}
}

func enforcedCondition(records []*kuadrantdnsv1alpha1.DNSRecord, dnsPolicy *v1alpha1.DNSPolicy) *metav1.Condition {
	// there are no controlled DNS records present
	if len(records) == 0 {
		cond := kuadrant.EnforcedCondition(dnsPolicy, nil, true)
		cond.Message = "DNSPolicy has been successfully enforced : no DNSRecords created based on policy and gateway configuration"
		return cond
	}

	// filter not ready records
	notReadyRecords := utils.Filter(records, func(record *kuadrantdnsv1alpha1.DNSRecord) bool {
		return meta.IsStatusConditionFalse(record.Status.Conditions, string(kuadrantdnsv1alpha1.ConditionTypeReady))
	})

	// if there are records and none of the records are ready
	if len(records) > 0 && len(notReadyRecords) == len(records) {
		return kuadrant.EnforcedCondition(dnsPolicy, kuadrant.NewErrUnknown(dnsPolicy.Kind(), errors.New("policy is not enforced on any DNSRecord: not a single DNSRecord is ready")), false)
	}

	// some of the records are not ready
	if len(notReadyRecords) > 0 {
		additionalMessage := ". Not ready DNSRecords are: "
		for _, record := range notReadyRecords {
			additionalMessage += fmt.Sprintf("%s ", record.Name)
		}
		cond := kuadrant.EnforcedCondition(dnsPolicy, nil, false)
		cond.Message += additionalMessage
		return cond
	}
	// all records are ready
	return kuadrant.EnforcedCondition(dnsPolicy, nil, true)
}

func propagateRecordConditions(records []*kuadrantdnsv1alpha1.DNSRecord, policyStatus *v1alpha1.DNSPolicyStatus) {
	//reset conditions
	policyStatus.RecordConditions = map[string][]metav1.Condition{}

	for _, record := range records {
		var allConditions []metav1.Condition
		allConditions = append(allConditions, record.Status.Conditions...)
		if record.Status.HealthCheck != nil {
			allConditions = append(allConditions, record.Status.HealthCheck.Conditions...)

			if record.Status.HealthCheck.Probes != nil {
				for _, probeStatus := range record.Status.HealthCheck.Probes {
					allConditions = append(allConditions, probeStatus.Conditions...)
				}
			}
		}

		for _, condition := range allConditions {
			//skip healthy negative polarity conditions
			if slices.Contains(NegativePolarityConditions, condition.Type) &&
				strings.ToLower(string(condition.Status)) == "false" {
				continue
			}
			//skip healthy positive polarity conditions
			if !slices.Contains(NegativePolarityConditions, condition.Type) &&
				strings.ToLower(string(condition.Status)) == "true" {
				continue
			}

			policyStatus.RecordConditions[record.Spec.RootHost] = append(
				policyStatus.RecordConditions[record.Spec.RootHost],
				condition)
		}
	}
}
