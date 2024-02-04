/*
Copyright 2023 The MultiCluster Traffic Controller Authors.

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
	"reflect"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	crlog "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantdnsv1alpha1 "github.com/kuadrant/kuadrant-dns-operator/api/v1alpha1"

	"github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/controllers/conditions"
	"github.com/kuadrant/kuadrant-operator/controllers/events"
	"github.com/kuadrant/kuadrant-operator/pkg/reconcilers"
)

const (
	DNSPolicyFinalizer                                    = "kuadrant.io/dns-policy"
	DNSPoliciesBackRefAnnotation                          = "kuadrant.io/dnspolicies"
	DNSPolicyBackRefAnnotation                            = "kuadrant.io/dnspolicy"
	DNSPolicyAffected            conditions.ConditionType = "kuadrant.io/DNSPolicyAffected"
)

type DNSPolicyRefsConfig struct{}

func (c *DNSPolicyRefsConfig) PolicyRefsAnnotation() string {
	return DNSPoliciesBackRefAnnotation
}

// DNSPolicyReconciler reconciles a DNSPolicy object
type DNSPolicyReconciler struct {
	reconcilers.TargetRefReconciler
	dnsHelper dnsHelper
}

//+kubebuilder:rbac:groups=kuadrant.io,resources=dnspolicies,verbs=get;list;watch;update;patch;delete
//+kubebuilder:rbac:groups=kuadrant.io,resources=dnspolicies/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=kuadrant.io,resources=dnspolicies/finalizers,verbs=update
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways/finalizers,verbs=update

//+kubebuilder:rbac:groups=kuadrant.io,resources=dnshealthcheckprobes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=kuadrant.io,resources=dnshealthcheckprobes/status,verbs=get
//+kubebuilder:rbac:groups=kuadrant.io,resources=dnsrecords,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=kuadrant.io,resources=dnsrecords/status,verbs=get

//+kubebuilder:rbac:groups=kuadrant.io,resources=managedzones,verbs=get;list;watch

func (r *DNSPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Logger().WithValues("DNSPolicy", req.NamespacedName)
	log.Info("Reconciling DNSPolicy")
	ctx = crlog.IntoContext(ctx, log)

	previous := &v1alpha1.DNSPolicy{}
	if err := r.Client().Get(ctx, req.NamespacedName, previous); err != nil {
		log.Info("error getting dns policy", "error", err)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	dnsPolicy := previous.DeepCopy()
	log.V(3).Info("DNSPolicyReconciler Reconcile", "dnsPolicy", dnsPolicy)

	markedForDeletion := dnsPolicy.GetDeletionTimestamp() != nil

	targetNetworkObject, err := r.FetchValidTargetRef(ctx, dnsPolicy.GetTargetRef(), dnsPolicy.Namespace)
	if err != nil {
		if !markedForDeletion {
			if apierrors.IsNotFound(err) {
				log.V(3).Info("Network object not found. Cleaning up")
				delResErr := r.deleteResources(ctx, dnsPolicy, nil)
				if delResErr == nil {
					delResErr = err
				}
				return r.reconcileStatus(ctx, dnsPolicy, fmt.Errorf("%w : %w", conditions.ErrTargetNotFound, delResErr))
			}
			return ctrl.Result{}, err
		}
		targetNetworkObject = nil // we need the object set to nil when there's an error, otherwise deleting the resources (when marked for deletion) will panic
	}

	if markedForDeletion {
		log.V(3).Info("cleaning up dns policy")
		if controllerutil.ContainsFinalizer(dnsPolicy, DNSPolicyFinalizer) {
			if err := r.deleteResources(ctx, dnsPolicy, targetNetworkObject); err != nil {
				return ctrl.Result{}, err
			}
			if err := r.RemoveFinalizer(ctx, dnsPolicy, DNSPolicyFinalizer); err != nil {
				return ctrl.Result{}, err
			}
		}

		return ctrl.Result{}, nil
	}

	// add finalizer to the dnsPolicy
	if !controllerutil.ContainsFinalizer(dnsPolicy, DNSPolicyFinalizer) {
		if err := r.AddFinalizer(ctx, dnsPolicy, DNSPolicyFinalizer); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{Requeue: true}, err
		} else if apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}

	specErr := r.reconcileResources(ctx, dnsPolicy, targetNetworkObject)

	statusResult, statusErr := r.reconcileStatus(ctx, dnsPolicy, specErr)

	if specErr != nil {
		return ctrl.Result{}, specErr
	}

	return statusResult, statusErr
}

func (r *DNSPolicyReconciler) reconcileResources(ctx context.Context, dnsPolicy *v1alpha1.DNSPolicy, targetNetworkObject client.Object) error {
	gatewayCondition := conditions.BuildPolicyAffectedCondition(DNSPolicyAffected, dnsPolicy, targetNetworkObject, conditions.PolicyReasonAccepted, nil)

	// validate
	err := dnsPolicy.Validate()
	if err != nil {
		return err
	}

	dnsPolicy.Default()

	// reconcile based on gateway diffs
	gatewayDiffObj, err := r.ComputeGatewayDiffs(ctx, dnsPolicy, targetNetworkObject, &DNSPolicyRefsConfig{})
	if err != nil {
		return err
	}

	if err = r.reconcileDNSRecords(ctx, dnsPolicy, gatewayDiffObj); err != nil {
		gatewayCondition = conditions.BuildPolicyAffectedCondition(DNSPolicyAffected, dnsPolicy, targetNetworkObject, conditions.PolicyReasonInvalid, err)
		updateErr := r.updateGatewayCondition(ctx, gatewayCondition, gatewayDiffObj)
		return errors.Join(fmt.Errorf("reconcile DNSRecords error %w", err), updateErr)
	}

	if err = r.reconcileHealthCheckProbes(ctx, dnsPolicy, gatewayDiffObj); err != nil {
		gatewayCondition = conditions.BuildPolicyAffectedCondition(DNSPolicyAffected, dnsPolicy, targetNetworkObject, conditions.PolicyReasonInvalid, err)
		updateErr := r.updateGatewayCondition(ctx, gatewayCondition, gatewayDiffObj)
		return errors.Join(fmt.Errorf("reconcile HealthChecks error %w", err), updateErr)
	}

	// set direct back ref - i.e. claim the target network object as taken asap
	if err = r.ReconcileTargetBackReference(ctx, dnsPolicy, targetNetworkObject, DNSPolicyBackRefAnnotation); err != nil {
		gatewayCondition = conditions.BuildPolicyAffectedCondition(DNSPolicyAffected, dnsPolicy, targetNetworkObject, conditions.PolicyReasonConflicted, err)
		updateErr := r.updateGatewayCondition(ctx, gatewayCondition, gatewayDiffObj)
		return errors.Join(fmt.Errorf("reconcile TargetBackReference error %w", err), updateErr)
	}

	// set annotation of policies affecting the gateway
	if err := r.ReconcileGatewayPolicyReferences(ctx, dnsPolicy, gatewayDiffObj); err != nil {
		gatewayCondition = conditions.BuildPolicyAffectedCondition(DNSPolicyAffected, dnsPolicy, targetNetworkObject, conditions.PolicyReasonUnknown, err)
		updateErr := r.updateGatewayCondition(ctx, gatewayCondition, gatewayDiffObj)
		return errors.Join(fmt.Errorf("ReconcileGatewayPolicyReferences error %w", err), updateErr)
	}

	// set gateway policy affected condition status - should be the last step, only when all the reconciliation steps succeed
	updateErr := r.updateGatewayCondition(ctx, gatewayCondition, gatewayDiffObj)
	if updateErr != nil {
		return fmt.Errorf("failed to update gateway conditions %w ", updateErr)
	}

	return nil
}

func (r *DNSPolicyReconciler) deleteResources(ctx context.Context, dnsPolicy *v1alpha1.DNSPolicy, targetNetworkObject client.Object) error {
	// delete based on gateway diffs

	if err := r.deleteDNSRecords(ctx, dnsPolicy); err != nil {
		return err
	}

	if err := r.deleteHealthCheckProbes(ctx, dnsPolicy); err != nil {
		return err
	}

	// remove direct back ref
	if targetNetworkObject != nil {
		if err := r.DeleteTargetBackReference(ctx, targetNetworkObject, DNSPolicyBackRefAnnotation); err != nil {
			return err
		}
	}

	gatewayDiffObj, err := r.ComputeGatewayDiffs(ctx, dnsPolicy, targetNetworkObject, &DNSPolicyRefsConfig{})
	if err != nil {
		return err
	}

	// update annotation of policies affecting the gateway
	if err := r.ReconcileGatewayPolicyReferences(ctx, dnsPolicy, gatewayDiffObj); err != nil {
		return err
	}

	// remove gateway policy affected condition status
	return r.updateGatewayCondition(ctx, metav1.Condition{Type: string(DNSPolicyAffected)}, gatewayDiffObj)
}

func (r *DNSPolicyReconciler) reconcileStatus(ctx context.Context, dnsPolicy *v1alpha1.DNSPolicy, specErr error) (ctrl.Result, error) {
	newStatus := r.calculateStatus(dnsPolicy, specErr)

	if !equality.Semantic.DeepEqual(newStatus, dnsPolicy.Status) {
		dnsPolicy.Status = *newStatus
		updateErr := r.Client().Status().Update(ctx, dnsPolicy)
		if updateErr != nil {
			// Ignore conflicts, resource might just be outdated.
			if apierrors.IsConflict(updateErr) {
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, updateErr
		}
	}

	if errors.Is(specErr, conditions.ErrTargetNotFound) {
		return ctrl.Result{Requeue: true}, nil
	}

	return ctrl.Result{}, nil
}

func (r *DNSPolicyReconciler) calculateStatus(dnsPolicy *v1alpha1.DNSPolicy, specErr error) *v1alpha1.DNSPolicyStatus {
	newStatus := dnsPolicy.Status.DeepCopy()
	if specErr != nil {
		newStatus.ObservedGeneration = dnsPolicy.Generation
	}
	readyCond := r.readyCondition(string(dnsPolicy.Spec.TargetRef.Kind), specErr)
	meta.SetStatusCondition(&newStatus.Conditions, *readyCond)
	return newStatus
}

func (r *DNSPolicyReconciler) readyCondition(targetNetworkObjectectKind string, specErr error) *metav1.Condition {
	cond := &metav1.Condition{
		Type:    string(conditions.ConditionTypeReady),
		Status:  metav1.ConditionTrue,
		Reason:  fmt.Sprintf("%sDNSEnabled", targetNetworkObjectectKind),
		Message: fmt.Sprintf("%s is DNS Enabled", targetNetworkObjectectKind),
	}

	if specErr != nil {
		cond.Status = metav1.ConditionFalse
		cond.Message = specErr.Error()
		cond.Reason = "ReconciliationError"

		if errors.Is(specErr, conditions.ErrTargetNotFound) {
			cond.Reason = string(conditions.PolicyReasonTargetNotFound)
		}
	}

	return cond
}

func (r *DNSPolicyReconciler) updateGatewayCondition(ctx context.Context, condition metav1.Condition, gatewayDiff *reconcilers.GatewayDiff) error {

	// update condition if needed
	for _, gw := range append(gatewayDiff.GatewaysWithValidPolicyRef, gatewayDiff.GatewaysMissingPolicyRef...) {
		previous := gw.DeepCopy()
		meta.SetStatusCondition(&gw.Status.Conditions, condition)
		if !reflect.DeepEqual(previous.Status.Conditions, gw.Status.Conditions) {
			if err := r.Client().Status().Update(ctx, gw.Gateway); err != nil {
				return err
			}
		}
	}

	// remove condition from gateway that is no longer referenced
	for _, gw := range gatewayDiff.GatewaysWithInvalidPolicyRef {
		previous := gw.DeepCopy()
		meta.RemoveStatusCondition(&gw.Status.Conditions, condition.Type)
		if !reflect.DeepEqual(previous.Status.Conditions, gw.Status.Conditions) {
			if err := r.Client().Status().Update(ctx, gw.Gateway); err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *DNSPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	gatewayEventMapper := events.NewGatewayEventMapper(r.Logger(), &DNSPolicyRefsConfig{}, "dnspolicy")
	probeEventMapper := events.NewProbeEventMapper(r.Logger(), DNSPolicyBackRefAnnotation, "dnspolicy")
	r.dnsHelper = dnsHelper{Client: r.Client()}
	ctrlr := ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.DNSPolicy{}).
		Watches(
			&gatewayapiv1.Gateway{},
			handler.EnqueueRequestsFromMapFunc(gatewayEventMapper.MapToPolicy),
		).
		Watches(
			&kuadrantdnsv1alpha1.DNSHealthCheckProbe{},
			handler.EnqueueRequestsFromMapFunc(probeEventMapper.MapToPolicy),
		)
	return ctrlr.Complete(r)
}
