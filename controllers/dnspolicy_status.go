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
	"context"
	"errors"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantdnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"

	"github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
)

var NegativePolarityConditions []string

func (r *DNSPolicyReconciler) reconcileStatus(ctx context.Context, dnsPolicy *v1alpha1.DNSPolicy, specErr error) (ctrl.Result, error) {
	newStatus := r.calculateStatus(ctx, dnsPolicy, specErr)

	equalStatus := equality.Semantic.DeepEqual(newStatus, dnsPolicy.Status)
	if equalStatus && dnsPolicy.Generation == dnsPolicy.Status.ObservedGeneration {
		return reconcile.Result{}, nil
	}

	newStatus.ObservedGeneration = dnsPolicy.Generation

	dnsPolicy.Status = *newStatus
	updateErr := r.Client().Status().Update(ctx, dnsPolicy)
	if updateErr != nil {
		// Ignore conflicts, resource might just be outdated.
		if apierrors.IsConflict(updateErr) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, updateErr
	}

	if kuadrant.IsTargetNotFound(specErr) {
		return ctrl.Result{Requeue: true}, nil
	}

	return ctrl.Result{}, nil
}

func (r *DNSPolicyReconciler) calculateStatus(ctx context.Context, dnsPolicy *v1alpha1.DNSPolicy, specErr error) *v1alpha1.DNSPolicyStatus {
	newStatus := &v1alpha1.DNSPolicyStatus{
		// Copy initial conditions. Otherwise, status will always be updated
		Conditions:         slices.Clone(dnsPolicy.Status.Conditions),
		ObservedGeneration: dnsPolicy.Status.ObservedGeneration,
	}

	acceptedCond := kuadrant.AcceptedCondition(dnsPolicy, specErr)
	meta.SetStatusCondition(&newStatus.Conditions, *acceptedCond)

	// Do not set enforced condition if Accepted condition is false
	if meta.IsStatusConditionFalse(newStatus.Conditions, string(v1alpha2.PolicyConditionAccepted)) {
		meta.RemoveStatusCondition(&newStatus.Conditions, string(kuadrant.PolicyConditionEnforced))
		return newStatus
	}

	recordsList := &kuadrantdnsv1alpha1.DNSRecordList{}
	controlledRecords := &kuadrantdnsv1alpha1.DNSRecordList{}

	var enforcedCondition *metav1.Condition
	if err := r.Client().List(ctx, recordsList); err != nil {
		enforcedCondition = kuadrant.EnforcedCondition(dnsPolicy, kuadrant.NewErrUnknown(dnsPolicy.Kind(), err), false)
	} else {
		enforcedCondition = r.enforcedCondition(recordsList, dnsPolicy)
	}

	meta.SetStatusCondition(&newStatus.Conditions, *enforcedCondition)

	for _, record := range recordsList.Items {
		for _, reference := range record.GetOwnerReferences() {
			if reference.Controller != nil && *reference.Controller && reference.Name == dnsPolicy.Name && reference.UID == dnsPolicy.UID {
				controlledRecords.Items = append(controlledRecords.Items, record)
			}
		}
	}

	propagateRecordConditions(controlledRecords, newStatus)

	return newStatus
}

func (r *DNSPolicyReconciler) enforcedCondition(recordsList *kuadrantdnsv1alpha1.DNSRecordList, dnsPolicy *v1alpha1.DNSPolicy) *metav1.Condition {
	var controlled bool
	for _, record := range recordsList.Items {
		// check that DNS record is controller by this policy
		for _, reference := range record.GetOwnerReferences() {
			if reference.Controller != nil && *reference.Controller && reference.Name == dnsPolicy.Name && reference.UID == dnsPolicy.UID {
				controlled = true
				// if at least one record not ready the policy is not enforced
				for _, condition := range record.Status.Conditions {
					if condition.Type == string(kuadrantdnsv1alpha1.ConditionTypeReady) && condition.Status == metav1.ConditionFalse {
						return kuadrant.EnforcedCondition(dnsPolicy, nil, false)
					}
				}
				break
			}
		}
	}

	// at least one DNS record is controlled by the policy
	// and all controlled records are accepted
	if controlled {
		return kuadrant.EnforcedCondition(dnsPolicy, nil, true)
	}
	// there are no controlled DNS records present
	return kuadrant.EnforcedCondition(dnsPolicy, kuadrant.NewErrUnknown(dnsPolicy.Kind(), errors.New("policy is not enforced on any dns record: no routes attached for listeners")), false)
}

func propagateRecordConditions(records *kuadrantdnsv1alpha1.DNSRecordList, policyStatus *v1alpha1.DNSPolicyStatus) {
	//reset conditions
	policyStatus.RecordConditions = map[string][]metav1.Condition{}

	for _, record := range records.Items {
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
