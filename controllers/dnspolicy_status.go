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
		return newStatus
	}

	enforcedCondition := r.enforcedCondition(ctx, dnsPolicy)
	meta.SetStatusCondition(&newStatus.Conditions, *enforcedCondition)

	return newStatus
}

func (r *DNSPolicyReconciler) enforcedCondition(ctx context.Context, dnsPolicy *v1alpha1.DNSPolicy) *metav1.Condition {
	recordsList := &kuadrantdnsv1alpha1.DNSRecordList{}
	if err := r.Client().List(ctx, recordsList); err != nil {
		r.Logger().V(1).Error(err, "error listing dns records")
		return kuadrant.EnforcedCondition(dnsPolicy, kuadrant.NewErrUnknown(dnsPolicy.Kind(), err), false)
	}

	var controlled bool
	for _, record := range recordsList.Items {
		// check that DNS record is controller by this policy
		for _, reference := range record.GetOwnerReferences() {
			if reference.Controller != nil && *reference.Controller && reference.Name == dnsPolicy.Name && reference.UID == dnsPolicy.UID {
				controlled = true
				// if at least one record not ready the policy is not enforced
				for _, condition := range record.Status.Conditions {
					if condition.Type == string(v1alpha2.PolicyConditionAccepted) && condition.Status == metav1.ConditionFalse {
						return kuadrant.EnforcedCondition(dnsPolicy, nil, false)
					}
				}
				break
			}
		}
	}

	// at least one DNS record is controlled byt the policy
	// and all controlled records are accepted
	if controlled {
		return kuadrant.EnforcedCondition(dnsPolicy, nil, true)
	}
	// there are no controlled DNS records present
	return kuadrant.EnforcedCondition(dnsPolicy, kuadrant.NewErrUnknown(dnsPolicy.Kind(), errors.New("policy is not enforced on any dns record: no routes attached for listeners")), false)
}
