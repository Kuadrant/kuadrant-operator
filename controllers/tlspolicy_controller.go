/*
Copyright 2023.

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

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	crlog "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/mappers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/reconcilers"
)

const TLSPolicyFinalizer = "kuadrant.io/tls-policy"

// TLSPolicyReconciler reconciles a TLSPolicy object
type TLSPolicyReconciler struct {
	*reconcilers.BaseReconciler
	TargetRefReconciler reconcilers.TargetRefReconciler
	RestMapper          meta.RESTMapper
}

//+kubebuilder:rbac:groups=kuadrant.io,resources=tlspolicies,verbs=get;list;watch;update;patch;delete
//+kubebuilder:rbac:groups=kuadrant.io,resources=tlspolicies/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=kuadrant.io,resources=tlspolicies/finalizers,verbs=update
//+kubebuilder:rbac:groups="cert-manager.io",resources=issuers,verbs=get;list;watch;
//+kubebuilder:rbac:groups="cert-manager.io",resources=clusterissuers,verbs=get;list;watch;
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups="cert-manager.io",resources=certificates,verbs=get;list;watch;create;update;patch;delete

func (r *TLSPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Logger().WithValues("TLSPolicy", req.NamespacedName)
	log.Info("Reconciling TLSPolicy")
	ctx = crlog.IntoContext(ctx, log)

	previous := &v1alpha1.TLSPolicy{}
	if err := r.Client().Get(ctx, req.NamespacedName, previous); err != nil {
		if err := client.IgnoreNotFound(err); err == nil {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	tlsPolicy := previous.DeepCopy()
	log.V(3).Info("TLSPolicyReconciler Reconcile", "tlsPolicy", tlsPolicy, "tlsPolicy.Spec", tlsPolicy.Spec)

	markedForDeletion := tlsPolicy.GetDeletionTimestamp() != nil

	targetReferenceObject, err := reconcilers.FetchTargetRefObject(ctx, r.Client(), tlsPolicy.GetTargetRef(), tlsPolicy.Namespace, tlsPolicy.TargetProgrammedGatewaysOnly())
	log.V(3).Info("TLSPolicyReconciler targetReferenceObject", "targetReferenceObject", targetReferenceObject)
	if err != nil {
		if !markedForDeletion {
			if apierrors.IsNotFound(err) {
				log.V(3).Info("Network object not found. Cleaning up")
				delResErr := r.deleteResources(ctx, tlsPolicy, nil)
				if delResErr == nil {
					delResErr = err
				}
				return ctrl.Result{}, delResErr
			}
			return ctrl.Result{}, err
		}
		targetReferenceObject = nil // we need the object set to nil when there's an error, otherwise deleting the resources (when marked for deletion) will panic
	}

	if markedForDeletion {
		log.V(3).Info("cleaning up tls policy")
		if controllerutil.ContainsFinalizer(tlsPolicy, TLSPolicyFinalizer) {
			if err := r.deleteResources(ctx, tlsPolicy, targetReferenceObject); err != nil {
				return ctrl.Result{}, err
			}
			if err := r.RemoveFinalizer(ctx, tlsPolicy, TLSPolicyFinalizer); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// add finalizer to the tlsPolicy
	if !controllerutil.ContainsFinalizer(tlsPolicy, TLSPolicyFinalizer) {
		if err := r.AddFinalizer(ctx, tlsPolicy, TLSPolicyFinalizer); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
	}
	specErr := r.reconcileResources(ctx, tlsPolicy, targetReferenceObject)

	return ctrl.Result{}, specErr
}

func (r *TLSPolicyReconciler) reconcileResources(ctx context.Context, tlsPolicy *v1alpha1.TLSPolicy, targetNetworkObject client.Object) error {
	err := validateIssuer(ctx, r.Client(), tlsPolicy)
	if err != nil {
		return err
	}

	// reconcile based on gateway diffs
	gatewayDiffObj, err := reconcilers.ComputeGatewayDiffs(ctx, r.Client(), tlsPolicy, targetNetworkObject)
	if err != nil {
		return err
	}

	if err = r.reconcileCertificates(ctx, tlsPolicy, gatewayDiffObj); err != nil {
		return fmt.Errorf("reconcile Certificates error %w", err)
	}

	// set direct back ref - i.e. claim the target network object as taken asap
	if err = r.TargetRefReconciler.ReconcileTargetBackReference(ctx, tlsPolicy, targetNetworkObject, tlsPolicy.DirectReferenceAnnotationName()); err != nil {
		return fmt.Errorf("reconcile TargetBackReference error %w", err)
	}

	// set annotation of policies affecting the gateway
	if err = r.TargetRefReconciler.ReconcileGatewayPolicyReferences(ctx, tlsPolicy, gatewayDiffObj); err != nil {
		return fmt.Errorf("ReconcileGatewayPolicyReferences error %w", err)
	}

	return nil
}

func (r *TLSPolicyReconciler) deleteResources(ctx context.Context, tlsPolicy *v1alpha1.TLSPolicy, targetNetworkObject client.Object) error {
	// delete based on gateway diffs
	gatewayDiffObj, err := reconcilers.ComputeGatewayDiffs(ctx, r.Client(), tlsPolicy, targetNetworkObject)
	if err != nil {
		return err
	}

	if err := r.deleteCertificates(ctx, tlsPolicy); err != nil {
		return err
	}

	// remove direct back ref
	if targetNetworkObject != nil {
		if err := r.TargetRefReconciler.DeleteTargetBackReference(ctx, targetNetworkObject, tlsPolicy.DirectReferenceAnnotationName()); err != nil {
			return err
		}
	}

	// update annotation of policies affecting the gateway
	return r.TargetRefReconciler.ReconcileGatewayPolicyReferences(ctx, tlsPolicy, gatewayDiffObj)
}

// SetupWithManager sets up the controller with the Manager.
func (r *TLSPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	ok, err := kuadrantgatewayapi.IsGatewayAPIInstalled(mgr.GetRESTMapper())
	if err != nil {
		return err
	}
	if !ok {
		r.Logger().Info("TLSPolicy controller disabled. GatewayAPI was not found")
		return nil
	}

	ok, err = kuadrantgatewayapi.IsCertManagerInstalled(mgr.GetRESTMapper(), r.Logger())
	if err != nil {
		return err
	}
	if !ok {
		r.Logger().Info("TLSPolicy controller disabled. CertManager was not found")
		return nil
	}

	gatewayEventMapper := mappers.NewGatewayEventMapper(
		v1alpha1.NewTLSPolicyType(),
		mappers.WithLogger(r.Logger().WithName("gateway.mapper")),
		mappers.WithClient(mgr.GetClient()),
	)

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.TLSPolicy{}).
		Owns(&certmanagerv1.Certificate{}).
		Watches(&gatewayapiv1.Gateway{}, handler.EnqueueRequestsFromMapFunc(gatewayEventMapper.Map)).
		Complete(r)
}
