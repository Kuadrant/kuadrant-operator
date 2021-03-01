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

	"github.com/kuadrant/kuadrant-controller/pkg/ingressproviders"
	"k8s.io/apimachinery/pkg/api/errors"

	"github.com/go-logr/logr"
	networkingv1beta1 "github.com/kuadrant/kuadrant-controller/apis/networking/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// APIReconciler reconciles a API object
type APIReconciler struct {
	client.Client
	Log             logr.Logger
	Scheme          *runtime.Scheme
	IngressProvider ingressproviders.IngressProvider
}

// +kubebuilder:rbac:groups=networking.kuadrant.io,resources=apis,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.kuadrant.io,resources=apis/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=networking.kuadrant.io,resources=apis/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *APIReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("api", req.NamespacedName)

	api := networkingv1beta1.API{}
	err := r.Get(ctx, req.NamespacedName, &api)
	if err != nil && errors.IsNotFound(err) {
		log.Info("API Object has been deleted.")
		// TODO: Now the deletion is handled by the Owner reference, but a proper way should be to use finalizers.
		return ctrl.Result{}, nil
	} else if err != nil {
		return ctrl.Result{}, err
	}

	// If the status is ready, and the generation matches, we can ignore the object.
	//TODO: use the provider validation and verify its status is Ready TOO
	if api.Status.Ready && api.GetGeneration() == api.Status.ObservedGeneration {
		return ctrl.Result{}, nil
	}

	err = r.IngressProvider.Create(ctx, api)
	if err != nil && errors.IsAlreadyExists(err) {
		err = r.IngressProvider.Update(ctx, api)
		if err != nil {
			return ctrl.Result{}, err
		}
	} else if err != nil {
		return ctrl.Result{}, err
	}

	// If we are here, set the status to Ready for now.
	// TODO: Get the status provider, and if ready, set it to ready
	_, _ = r.IngressProvider.Status(api)
	api.Status.Ready = true
	api.Status.ObservedGeneration = api.GetGeneration()
	r.Client.Status().Update(ctx, &api)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *APIReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&networkingv1beta1.API{}).
		Complete(r)
}
