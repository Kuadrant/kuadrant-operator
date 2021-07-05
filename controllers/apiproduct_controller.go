/*
Copyright 2021 Red Hat, Inc.

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
	"encoding/json"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	networkingv1beta1 "github.com/kuadrant/kuadrant-controller/apis/networking/v1beta1"
	"github.com/kuadrant/kuadrant-controller/pkg/authproviders"
	"github.com/kuadrant/kuadrant-controller/pkg/ingressproviders"
	"github.com/kuadrant/kuadrant-controller/pkg/reconcilers"
)

const (
	finalizerName = "kuadrant.io/apiproduct"
)

// APIProductReconciler reconciles a APIProduct object
type APIProductReconciler struct {
	*reconcilers.BaseReconciler
	IngressProvider ingressproviders.IngressProvider
	AuthProvider    authproviders.AuthProvider
}

//+kubebuilder:rbac:groups=networking.kuadrant.io,resources=apiproducts;apis,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.kuadrant.io,resources=apiproducts/status;apis/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=networking.kuadrant.io,resources=apiproducts/finalizers;apis/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *APIProductReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Logger().WithValues("apiproduct", req.NamespacedName)
	log.Info("Reconciling APIProduct")

	apip := &networkingv1beta1.APIProduct{}
	err := r.Client().Get(ctx, req.NamespacedName, apip)
	if err != nil && errors.IsNotFound(err) {
		log.Info("resource not found. Ignoring since object must have been deleted")
		return ctrl.Result{}, nil
	} else if err != nil {
		return ctrl.Result{}, err
	}

	if log.V(1).Enabled() {
		jsonData, err := json.MarshalIndent(apip, "", "  ")
		if err != nil {
			return ctrl.Result{}, err
		}
		log.V(1).Info(string(jsonData))
	}

	if apip.Status.Ready && apip.Status.ObservedGen == apip.GetGeneration() {
		log.Info("nothing to be done")
		return ctrl.Result{}, nil
	}

	// APIProduct has been marked for deletion
	if apip.GetDeletionTimestamp() != nil && controllerutil.ContainsFinalizer(apip, finalizerName) {
		err := r.IngressProvider.Delete(ctx, apip)
		if err != nil {
			return ctrl.Result{}, err
		}

		err = r.AuthProvider.Delete(ctx, apip)
		if err != nil {
			return ctrl.Result{}, err
		}

		//Remove finalizer and update the object.
		controllerutil.RemoveFinalizer(apip, finalizerName)
		err = r.UpdateResource(ctx, apip)
		log.Info("Removing finalizer", "error", err)
		return ctrl.Result{Requeue: true}, err
	}

	if !controllerutil.ContainsFinalizer(apip, finalizerName) {
		controllerutil.AddFinalizer(apip, finalizerName)
		err := r.UpdateResource(ctx, apip)
		log.Info("Adding finalizer", "error", err)
		return ctrl.Result{Requeue: true}, err
	}

	result, err := r.reconcileSpec(ctx, apip)
	log.Info("spec reconcile done", "result", result, "error", err)
	if err != nil {
		// Ignore conflicts, resource might just be outdated.
		if errors.IsConflict(err) {
			log.Info("Resource update conflict error. Requeuing...")
			return ctrl.Result{Requeue: true}, nil
		}
		r.EventRecorder().Eventf(apip, corev1.EventTypeWarning, "ReconcileError", "%v", err)
		return ctrl.Result{}, err
	}

	if result.Requeue {
		log.Info("Reconciling not finished. Requeueing.")
		return result, nil
	}

	result, err = r.reconcileStatus(ctx, apip)
	log.Info("status reconcile done", "result", result, "error", err)

	if err != nil {
		// Ignore conflicts, resource might just be outdated.
		if errors.IsConflict(err) {
			log.Info("Resource update conflict error. Requeuing...")
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}

	if result.Requeue {
		log.Info("Reconciling not finished. Requeueing.")
		return result, nil
	}

	return ctrl.Result{}, nil
}

func (r *APIProductReconciler) reconcileSpec(ctx context.Context, apip *networkingv1beta1.APIProduct) (ctrl.Result, error) {
	res, err := r.IngressProvider.Reconcile(ctx, apip)
	if err != nil || res.Requeue {
		return res, err
	}

	res, err = r.AuthProvider.Reconcile(ctx, apip)
	if err != nil || res.Requeue {
		return res, err
	}

	return ctrl.Result{}, nil
}

func (r *APIProductReconciler) reconcileStatus(ctx context.Context, apip *networkingv1beta1.APIProduct) (ctrl.Result, error) {
	ingressOK, err := r.IngressProvider.Status(ctx, apip)
	if err != nil {
		return ctrl.Result{}, err
	}

	authOK, err := r.IngressProvider.Status(ctx, apip)
	if err != nil {
		return ctrl.Result{}, err
	}

	//Mark the object as ready if both provider are OK.
	if ingressOK && authOK {
		apip.Status.Ready = true
		apip.Status.ObservedGen = apip.GetGeneration()
		err := r.UpdateResourceStatus(ctx, apip)
		if err != nil {
			return ctrl.Result{}, err
		}
	} else {
		apip.Status.Ready = false
		apip.Status.ObservedGen = apip.GetGeneration()
		err := r.UpdateResourceStatus(ctx, apip)
		if err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *APIProductReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&networkingv1beta1.APIProduct{}).
		Complete(r)
}
