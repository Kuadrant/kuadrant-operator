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
	"fmt"
	"slices"

	certmanv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/reconcilers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

func (r *TLSPolicyReconciler) reconcileStatus(ctx context.Context, tlsPolicy *v1alpha1.TLSPolicy, targetNetworkObject client.Object, specErr error) (ctrl.Result, error) {
	newStatus := r.calculateStatus(ctx, tlsPolicy, targetNetworkObject, specErr)

	equalStatus := equality.Semantic.DeepEqual(newStatus, tlsPolicy.Status)
	if equalStatus && tlsPolicy.Generation == tlsPolicy.Status.ObservedGeneration {
		return reconcile.Result{}, nil
	}

	newStatus.ObservedGeneration = tlsPolicy.Generation

	tlsPolicy.Status = *newStatus
	updateErr := r.Client().Status().Update(ctx, tlsPolicy)
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

func (r *TLSPolicyReconciler) calculateStatus(ctx context.Context, tlsPolicy *v1alpha1.TLSPolicy, targetNetworkObject client.Object, specErr error) *v1alpha1.TLSPolicyStatus {
	newStatus := &v1alpha1.TLSPolicyStatus{
		// Copy initial conditions. Otherwise, status will always be updated
		Conditions:         slices.Clone(tlsPolicy.Status.Conditions),
		ObservedGeneration: tlsPolicy.Status.ObservedGeneration,
	}

	acceptedCond := kuadrant.AcceptedCondition(tlsPolicy, specErr)
	meta.SetStatusCondition(&newStatus.Conditions, *acceptedCond)

	// Do not set enforced condition if Accepted condition is false
	if meta.IsStatusConditionFalse(newStatus.Conditions, string(gatewayapiv1alpha2.PolicyReasonAccepted)) {
		meta.RemoveStatusCondition(&newStatus.Conditions, string(kuadrant.PolicyConditionEnforced))
		return newStatus
	}

	enforcedCond := r.enforcedCondition(ctx, tlsPolicy, targetNetworkObject)
	meta.SetStatusCondition(&newStatus.Conditions, *enforcedCond)

	return newStatus
}

func (r *TLSPolicyReconciler) enforcedCondition(ctx context.Context, tlsPolicy *v1alpha1.TLSPolicy, targetNetworkObject client.Object) *metav1.Condition {
	if err := r.isIssuerReady(ctx, tlsPolicy); err != nil {
		return kuadrant.EnforcedCondition(tlsPolicy, kuadrant.NewErrUnknown(tlsPolicy.Kind(), err), false)
	}

	if err := r.isCertificatesReady(ctx, tlsPolicy, targetNetworkObject); err != nil {
		return kuadrant.EnforcedCondition(tlsPolicy, kuadrant.NewErrUnknown(tlsPolicy.Kind(), err), false)
	}

	return kuadrant.EnforcedCondition(tlsPolicy, nil, true)
}

func (r *TLSPolicyReconciler) isIssuerReady(ctx context.Context, tlsPolicy *v1alpha1.TLSPolicy) error {
	var conditions []certmanv1.IssuerCondition

	switch tlsPolicy.Spec.IssuerRef.Kind {
	case "", certmanv1.IssuerKind:
		issuer := &certmanv1.Issuer{}
		if err := r.Client().Get(ctx, client.ObjectKey{Name: tlsPolicy.Spec.IssuerRef.Name, Namespace: tlsPolicy.Namespace}, issuer); err != nil {
			return err
		}
		conditions = issuer.Status.Conditions
	case certmanv1.ClusterIssuerKind:
		issuer := &certmanv1.ClusterIssuer{}
		if err := r.Client().Get(ctx, client.ObjectKey{Name: tlsPolicy.Spec.IssuerRef.Name}, issuer); err != nil {
			return err
		}
		conditions = issuer.Status.Conditions
	default:
		return fmt.Errorf(`invalid value %q for issuerRef.kind. Must be empty, %q or %q`, tlsPolicy.Spec.IssuerRef.Kind, certmanv1.IssuerKind, certmanv1.ClusterIssuerKind)
	}

	transformedCond := utils.Map(conditions, func(c certmanv1.IssuerCondition) metav1.Condition {
		return metav1.Condition{Reason: c.Reason, Status: metav1.ConditionStatus(c.Status), Type: string(c.Type), Message: c.Message}
	})

	if meta.IsStatusConditionFalse(transformedCond, string(certmanv1.IssuerConditionReady)) {
		return errors.New("issuer not ready")
	}

	return nil
}

func (r *TLSPolicyReconciler) isCertificatesReady(ctx context.Context, tlsPolicy *v1alpha1.TLSPolicy, targetNetworkObject client.Object) error {
	gwDiffObj, err := reconcilers.ComputeGatewayDiffs(ctx, r.Client(), tlsPolicy, targetNetworkObject)
	if err != nil {
		return err
	}

	if len(gwDiffObj.GatewaysWithValidPolicyRef) == 0 {
		return errors.New("no valid gateways found")
	}

	for _, gw := range gwDiffObj.GatewaysWithValidPolicyRef {
		expectedCertificates := r.expectedCertificatesForGateway(ctx, gw.Gateway, tlsPolicy)

		for _, cert := range expectedCertificates {
			c := &certmanv1.Certificate{}
			if err := r.Client().Get(ctx, client.ObjectKeyFromObject(cert), c); err != nil {
				return err
			}
			conditions := utils.Map(c.Status.Conditions, func(c certmanv1.CertificateCondition) metav1.Condition {
				return metav1.Condition{Reason: c.Reason, Status: metav1.ConditionStatus(c.Status), Type: string(c.Type), Message: c.Message}
			})

			if meta.IsStatusConditionFalse(conditions, string(certmanv1.CertificateConditionReady)) {
				return fmt.Errorf("certificate %s not ready", cert.Name)
			}
		}
	}

	return nil
}
