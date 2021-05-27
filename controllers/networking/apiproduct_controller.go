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

package networking

import (
	"context"
	"encoding/json"

	"github.com/kuadrant/kuadrant-controller/pkg/authproviders"

	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/kuadrant/kuadrant-controller/pkg/ingressproviders"
	"k8s.io/apimachinery/pkg/api/errors"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	networkingv1beta1 "github.com/kuadrant/kuadrant-controller/apis/networking/v1beta1"
)

// APIProductReconciler reconciles a APIProduct object
type APIProductReconciler struct {
	client.Client
	Log             logr.Logger
	Scheme          *runtime.Scheme
	IngressProvider ingressproviders.IngressProvider
	AuthProvider    authproviders.AuthProvider
}

const (
	finalizerName = "kuadrant.io/apiproduct"
)

//+kubebuilder:rbac:groups=networking.kuadrant.io,resources=apiproducts;apis,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.kuadrant.io,resources=apiproducts/status;apis/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=networking.kuadrant.io,resources=apiproducts/finalizers;apis/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *APIProductReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("apiproduct", req.NamespacedName)
	log.Info("Reconciling APIProduct")

	apip := networkingv1beta1.APIProduct{}
	err := r.Client.Get(ctx, req.NamespacedName, &apip)
	if err != nil && errors.IsNotFound(err) {
		log.Info("APIProduct Object has been deleted.")
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
		return ctrl.Result{}, nil
	}

	// APIProduct has been marked for deletion
	if apip.GetDeletionTimestamp() != nil && controllerutil.ContainsFinalizer(&apip, finalizerName) {
		// cleanup the Ingress objects.
		err = r.IngressProvider.Delete(ctx, apip)
		if err != nil {
			return ctrl.Result{}, err
		}

		// cleanup the authorization objects.
		err = r.AuthProvider.Delete(ctx, apip)
		if err != nil {
			return ctrl.Result{}, err
		}

		//Remove finalizer and update the object.
		controllerutil.RemoveFinalizer(&apip, finalizerName)
		err := r.Client.Update(ctx, &apip)
		log.Info("Removing finalizer", "error", err)
		return ctrl.Result{Requeue: true}, err
	}

	if !controllerutil.ContainsFinalizer(&apip, finalizerName) {
		controllerutil.AddFinalizer(&apip, finalizerName)
		err := r.Update(ctx, &apip)
		log.Info("Adding finalizer", "error", err)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	// Create or Update the Ingress using the provider
	result, err := r.createOrUpdateIngress(ctx, apip)
	if err != nil {
		return result, err
	}

	// Create or Update the Authorization objects using the provider
	result, err = r.createOrUpdateAuth(ctx, apip)
	if err != nil {
		return result, err
	}

	// Check if the provider objects are set to Ready.
	ingressOK, err := r.IngressProvider.Status(ctx, apip)
	if err != nil {
		return ctrl.Result{}, err
	}
	authOK, err := r.AuthProvider.Status(ctx, apip)
	if err != nil {
		return ctrl.Result{}, err
	}

	//Mark the object as ready if both provider are OK.
	if ingressOK && authOK {
		apip.Status.Ready = true
		apip.Status.ObservedGen = apip.GetGeneration()
		err := r.Client.Status().Update(ctx, &apip)
		if err != nil {
			return ctrl.Result{}, err
		}
	} else {
		// If theres some issue with the ingresses or the authorization objects, mark the object as not ready.
		apip.Status.Ready = false
		apip.Status.ObservedGen = apip.GetGeneration()
		err := r.Client.Status().Update(ctx, &apip)
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

func (r *APIProductReconciler) createOrUpdateIngress(ctx context.Context, apip networkingv1beta1.APIProduct) (ctrl.Result, error) {
	err := r.IngressProvider.Create(ctx, apip)

	if err != nil && errors.IsAlreadyExists(err) {
		err = r.IngressProvider.Update(ctx, apip)
		if err != nil {
			return ctrl.Result{}, err
		}
	} else if err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *APIProductReconciler) createOrUpdateAuth(ctx context.Context, apip networkingv1beta1.APIProduct) (ctrl.Result, error) {
	err := r.AuthProvider.Create(ctx, apip)

	if err != nil && errors.IsAlreadyExists(err) {
		err = r.AuthProvider.Update(ctx, apip)
		if err != nil {
			return ctrl.Result{}, err
		}
	} else if err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}
