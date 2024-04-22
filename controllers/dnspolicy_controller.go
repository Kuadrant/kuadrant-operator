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
	"reflect"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	crlog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantdnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"

	"github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/mappers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/reconcilers"
)

const (
	DNSPolicyFinalizer        = "kuadrant.io/dns-policy"
	DNSPolicyAffected  string = "kuadrant.io/DNSPolicyAffected"
)

type DNSPolicyRefsConfig struct{}

// DNSPolicyReconciler reconciles a DNSPolicy object
type DNSPolicyReconciler struct {
	*reconcilers.BaseReconciler
	TargetRefReconciler reconcilers.TargetRefReconciler
	dnsHelper           dnsHelper
}

//+kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch
//+kubebuilder:rbac:groups=kuadrant.io,resources=dnspolicies,verbs=get;list;watch;update;patch;delete
//+kubebuilder:rbac:groups=kuadrant.io,resources=dnspolicies/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=kuadrant.io,resources=dnspolicies/finalizers,verbs=update
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways/finalizers,verbs=update

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

	targetNetworkObject, err := reconcilers.FetchTargetRefObject(ctx, r.Client(), dnsPolicy.GetTargetRef(), dnsPolicy.Namespace)
	if err != nil {
		if !markedForDeletion {
			if apierrors.IsNotFound(err) {
				log.V(3).Info("Network object not found. Cleaning up")
				delResErr := r.deleteResources(ctx, dnsPolicy, nil)
				if delResErr == nil {
					delResErr = err
				}
				return r.reconcileStatus(ctx, dnsPolicy, kuadrant.NewErrTargetNotFound(dnsPolicy.Kind(), dnsPolicy.GetTargetRef(), delResErr))
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
	gatewayCondition := BuildPolicyAffectedCondition(DNSPolicyAffected, dnsPolicy, targetNetworkObject, gatewayapiv1alpha2.PolicyReasonAccepted, nil)

	// validate
	err := dnsPolicy.Validate()
	if err != nil {
		return err
	}

	dnsPolicy.Default()

	// reconcile based on gateway diffs
	gatewayDiffObj, err := reconcilers.ComputeGatewayDiffs(ctx, r.Client(), dnsPolicy, targetNetworkObject)
	if err != nil {
		return err
	}

	if err = r.reconcileDNSRecords(ctx, dnsPolicy, gatewayDiffObj); err != nil {
		gatewayCondition = BuildPolicyAffectedCondition(DNSPolicyAffected, dnsPolicy, targetNetworkObject, gatewayapiv1alpha2.PolicyReasonInvalid, err)
		updateErr := r.updateGatewayCondition(ctx, gatewayCondition, gatewayDiffObj)
		return errors.Join(fmt.Errorf("reconcile DNSRecords error %w", err), updateErr)
	}

	// set direct back ref - i.e. claim the target network object as taken asap
	if err = r.TargetRefReconciler.ReconcileTargetBackReference(ctx, dnsPolicy, targetNetworkObject, dnsPolicy.DirectReferenceAnnotationName()); err != nil {
		gatewayCondition = BuildPolicyAffectedCondition(DNSPolicyAffected, dnsPolicy, targetNetworkObject, gatewayapiv1alpha2.PolicyReasonConflicted, err)
		updateErr := r.updateGatewayCondition(ctx, gatewayCondition, gatewayDiffObj)
		return errors.Join(fmt.Errorf("reconcile TargetBackReference error %w", err), updateErr)
	}

	// set annotation of policies affecting the gateway
	if err := r.TargetRefReconciler.ReconcileGatewayPolicyReferences(ctx, dnsPolicy, gatewayDiffObj); err != nil {
		gatewayCondition = BuildPolicyAffectedCondition(DNSPolicyAffected, dnsPolicy, targetNetworkObject, gatewayapiv1alpha2.PolicyConditionReason(PolicyReasonUnknown), err)
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

	// remove direct back ref
	if targetNetworkObject != nil {
		if err := r.TargetRefReconciler.DeleteTargetBackReference(ctx, targetNetworkObject, dnsPolicy.DirectReferenceAnnotationName()); err != nil {
			return err
		}
	}

	gatewayDiffObj, err := reconcilers.ComputeGatewayDiffs(ctx, r.Client(), dnsPolicy, targetNetworkObject)
	if err != nil {
		return err
	}

	// update annotation of policies affecting the gateway
	if err := r.TargetRefReconciler.ReconcileGatewayPolicyReferences(ctx, dnsPolicy, gatewayDiffObj); err != nil {
		return err
	}

	// remove gateway policy affected condition status
	return r.updateGatewayCondition(ctx, metav1.Condition{Type: DNSPolicyAffected}, gatewayDiffObj)
}

func (r *DNSPolicyReconciler) updateGatewayCondition(ctx context.Context, condition metav1.Condition, gatewayDiff *reconcilers.GatewayDiffs) error {
	// update condition if needed
	gatewayDiffs := append(gatewayDiff.GatewaysWithValidPolicyRef, gatewayDiff.GatewaysMissingPolicyRef...)
	for i, gw := range gatewayDiffs {
		previous := gw.DeepCopy()
		meta.SetStatusCondition(&gatewayDiffs[i].Status.Conditions, condition)
		if !reflect.DeepEqual(previous.Status.Conditions, gw.Status.Conditions) {
			if err := r.Client().Status().Update(ctx, gw.Gateway); err != nil {
				return err
			}
		}
	}

	// remove condition from gateway that is no longer referenced
	gatewayDiffs = gatewayDiff.GatewaysWithInvalidPolicyRef
	for i, gw := range gatewayDiff.GatewaysWithInvalidPolicyRef {
		previous := gw.DeepCopy()
		meta.RemoveStatusCondition(&gatewayDiffs[i].Status.Conditions, condition.Type)
		if !reflect.DeepEqual(previous.Status.Conditions, gw.Status.Conditions) {
			if err := r.Client().Status().Update(ctx, gw.Gateway); err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *DNSPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	gatewayEventMapper := mappers.NewGatewayEventMapper(mappers.WithLogger(r.Logger().WithName("gatewayEventMapper")))

	r.dnsHelper = dnsHelper{Client: r.Client()}
	ctrlr := ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.DNSPolicy{}).
		Owns(&kuadrantdnsv1alpha1.DNSRecord{}).
		Watches(
			&gatewayapiv1.Gateway{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, object client.Object) []reconcile.Request {
				return gatewayEventMapper.MapToPolicy(object, &v1alpha1.DNSPolicy{})
			}),
		)
	return ctrlr.Complete(r)
}
