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
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	crlog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantdnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"

	"github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/mappers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/reconcilers"
)

const DNSPolicyFinalizer = "kuadrant.io/dns-policy"

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
	// validate
	err := dnsPolicy.Validate()
	if err != nil {
		return err
	}

	// reconcile based on gateway diffs
	gatewayDiffObj, err := reconcilers.ComputeGatewayDiffs(ctx, r.Client(), dnsPolicy, targetNetworkObject)
	if err != nil {
		return err
	}

	if err = r.reconcileDNSRecords(ctx, dnsPolicy, gatewayDiffObj); err != nil {
		return fmt.Errorf("reconcile DNSRecords error %w", err)
	}

	// set direct back ref - i.e. claim the target network object as taken asap
	if err = r.TargetRefReconciler.ReconcileTargetBackReference(ctx, dnsPolicy, targetNetworkObject, dnsPolicy.DirectReferenceAnnotationName()); err != nil {
		return fmt.Errorf("reconcile TargetBackReference error %w", err)
	}

	// set annotation of policies affecting the gateway
	if err := r.TargetRefReconciler.ReconcileGatewayPolicyReferences(ctx, dnsPolicy, gatewayDiffObj); err != nil {
		return fmt.Errorf("ReconcileGatewayPolicyReferences error %w", err)
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
	return r.TargetRefReconciler.ReconcileGatewayPolicyReferences(ctx, dnsPolicy, gatewayDiffObj)
}

func (r *DNSPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	ok, err := kuadrantgatewayapi.IsGatewayAPIInstalled(mgr.GetRESTMapper())
	if err != nil {
		return err
	}
	if !ok {
		r.Logger().Info("DNSPolicy controller disabled. GatewayAPI was not found")
		return nil
	}

	gatewayEventMapper := mappers.NewGatewayEventMapper(mappers.WithLogger(r.Logger().WithName("gatewayEventMapper")), mappers.WithClient(mgr.GetClient()))

	r.dnsHelper = dnsHelper{Client: r.Client()}
	ctrlr := ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.DNSPolicy{}).
		Owns(&kuadrantdnsv1alpha1.DNSRecord{}).
		Watches(
			&gatewayapiv1.Gateway{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, object client.Object) []reconcile.Request {
				return gatewayEventMapper.MapToPolicy(ctx, object, &v1alpha1.DNSPolicy{})
			}),
		)
	return ctrlr.Complete(r)
}
