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

package apim

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/go-logr/logr"
	apimv1alpha1 "github.com/kuadrant/kuadrant-controller/apis/apim/v1alpha1"
	"github.com/kuadrant/kuadrant-controller/pkg/reconcilers"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

const APIFinalizerName = "kuadrant.io/envoyfilters"

// APIReconciler reconciles a API object
type APIReconciler struct {
	*reconcilers.BaseReconciler
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=apim.kuadrant.io,resources=apis,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apim.kuadrant.io,resources=apis/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=apim.kuadrant.io,resources=apis/finalizers,verbs=update

func (r *APIReconciler) Reconcile(eventCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger().WithValues("API", req.NamespacedName)
	logger.Info("Reconciling API object")
	ctx := logr.NewContext(eventCtx, logger)

	var api apimv1alpha1.API
	if err := r.Client().Get(ctx, req.NamespacedName, &api); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("no API resource found.")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "failed to get API object")
		return ctrl.Result{}, err
	}

	if api.GetDeletionTimestamp() != nil && controllerutil.ContainsFinalizer(&api, APIFinalizerName) {
		controllerutil.RemoveFinalizer(&api, APIFinalizerName)
		if err := r.BaseReconciler.UpdateResource(ctx, &api); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{Requeue: true}, err
		}
		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(&api, APIFinalizerName) {
		controllerutil.AddFinalizer(&api, APIFinalizerName)
		if err := r.BaseReconciler.UpdateResource(ctx, &api); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{Requeue: true}, err
		}
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *APIReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&apimv1alpha1.API{}).
		Complete(r)
}
