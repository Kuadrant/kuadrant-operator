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

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"

	networkingv1beta1 "github.com/kuadrant/kuadrant-controller/apis/networking/v1beta1"
	"github.com/kuadrant/kuadrant-controller/pkg/authproviders"
	"github.com/kuadrant/kuadrant-controller/pkg/ingressproviders"
	"github.com/kuadrant/kuadrant-controller/pkg/log"
	"github.com/kuadrant/kuadrant-controller/pkg/ratelimitproviders"
	"github.com/kuadrant/kuadrant-controller/pkg/reconcilers"
)

const (
	finalizerName = "kuadrant.io/apiproduct"
)

// APIProductReconciler reconciles a APIProduct object
type APIProductReconciler struct {
	*reconcilers.BaseReconciler
	IngressProvider   ingressproviders.IngressProvider
	AuthProvider      authproviders.AuthProvider
	RateLimitProvider ratelimitproviders.RateLimitProvider
}

//+kubebuilder:rbac:groups=networking.kuadrant.io,resources=apiproducts;apis,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.kuadrant.io,resources=apiproducts/status;apis/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=networking.kuadrant.io,resources=apiproducts/finalizers;apis/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *APIProductReconciler) Reconcile(eventCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger().WithValues("apiproduct", req.NamespacedName)
	logger.Info("Reconciling APIProduct")
	ctx := logr.NewContext(eventCtx, logger)

	apip := &networkingv1beta1.APIProduct{}
	err := r.Client().Get(ctx, req.NamespacedName, apip)
	if err != nil && apierrors.IsNotFound(err) {
		logger.Info("resource not found. Ignoring since object must have been deleted")
		return ctrl.Result{}, nil
	} else if err != nil {
		return ctrl.Result{}, err
	}

	if logger.V(1).Enabled() {
		jsonData, err := json.MarshalIndent(apip, "", "  ")
		if err != nil {
			return ctrl.Result{}, err
		}
		logger.V(1).Info(string(jsonData))
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

		err = r.RateLimitProvider.Delete(ctx, apip)
		if err != nil {
			return ctrl.Result{}, err
		}

		//Remove finalizer and update the object.
		controllerutil.RemoveFinalizer(apip, finalizerName)
		err = r.UpdateResource(ctx, apip)
		logger.Info("Removing finalizer", "error", err)
		return ctrl.Result{Requeue: true}, err
	}

	// Ignore deleted resources, this can happen when foregroundDeletion is enabled
	// https://kubernetes.io/docs/concepts/workloads/controllers/garbage-collection/#foreground-cascading-deletion
	if apip.GetDeletionTimestamp() != nil {
		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(apip, finalizerName) {
		controllerutil.AddFinalizer(apip, finalizerName)
		err := r.UpdateResource(ctx, apip)
		logger.Info("Adding finalizer", "error", err)
		return ctrl.Result{Requeue: true}, err
	}

	specResult, specErr := r.reconcileSpec(ctx, apip)
	logger.Info("spec reconcile done", "result", specResult, "error", specErr)
	if specErr == nil && specResult.Requeue {
		logger.Info("Reconciling not finished. Requeueing.")
		return specResult, nil
	}

	// reconcile status regardless specErr
	statusResult, statusErr := r.reconcileStatus(ctx, apip)
	logger.Info("status reconcile done", "result", statusResult, "error", statusErr)
	if statusErr != nil {
		// Ignore conflicts, resource might just be outdated.
		if apierrors.IsConflict(statusErr) {
			logger.Info("Failed to update status: resource might just be outdated")
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, statusErr
	}

	if specErr != nil {
		// Ignore conflicts, resource might just be outdated.
		if apierrors.IsConflict(specErr) {
			logger.Info("Resource update conflict error. Requeuing...")
			return ctrl.Result{Requeue: true}, nil
		}

		if apierrors.IsInvalid(specErr) {
			// On Validation error, no need to retry as spec is not valid and needs to be changed
			logger.Info("ERROR", "spec validation error", specErr)
			r.EventRecorder().Eventf(apip, corev1.EventTypeWarning, "Invalid APIProduct Spec", "%v", specErr)
			return ctrl.Result{}, nil
		}

		r.EventRecorder().Eventf(apip, corev1.EventTypeWarning, "ReconcileError", "%v", specErr)
		return ctrl.Result{}, specErr
	}

	if statusResult.Requeue {
		logger.Info("Reconciling not finished. Requeueing.")
		return statusResult, nil
	}

	return ctrl.Result{}, nil
}

func (r *APIProductReconciler) reconcileSpec(ctx context.Context, apip *networkingv1beta1.APIProduct) (ctrl.Result, error) {
	err := r.validateSpec(ctx, apip)
	if err != nil {
		return ctrl.Result{}, err
	}

	res, err := r.setDefaults(ctx, apip)
	if err != nil || res.Requeue {
		return res, err
	}

	res, err = r.IngressProvider.Reconcile(ctx, apip)
	if err != nil || res.Requeue {
		return res, err
	}

	res, err = r.AuthProvider.Reconcile(ctx, apip)
	if err != nil || res.Requeue {
		return res, err
	}

	res, err = r.RateLimitProvider.Reconcile(ctx, apip)
	if err != nil || res.Requeue {
		return res, err
	}

	return ctrl.Result{}, nil
}

func (r *APIProductReconciler) setDefaults(ctx context.Context, apip *networkingv1beta1.APIProduct) (ctrl.Result, error) {
	changed, err := r.reconcileAPIProductLabels(ctx, apip)
	if err != nil {
		return ctrl.Result{}, err
	}

	if changed {
		err = r.UpdateResource(ctx, apip)
	}

	return ctrl.Result{Requeue: changed}, err
}

func (r *APIProductReconciler) reconcileAPIProductLabels(ctx context.Context, apip *networkingv1beta1.APIProduct) (bool, error) {
	apiUIDs, err := r.getAPIUIDs(ctx, apip)
	if err != nil {
		return false, err
	}

	return replaceAPILabels(apip, apiUIDs), nil
}

func (r *APIProductReconciler) getAPIUIDs(ctx context.Context, apip *networkingv1beta1.APIProduct) ([]string, error) {
	logger := logr.FromContext(ctx)

	uids := []string{}
	for _, apiSel := range apip.Spec.APIs {
		api := &networkingv1beta1.API{}
		err := r.Client().Get(ctx, apiSel.APINamespacedName(), api)
		logger.V(1).Info("get API", "objectKey", apiSel.APINamespacedName(), "error", err)
		if err != nil {
			return nil, err
		}
		uids = append(uids, string(api.GetUID()))
	}
	return uids, nil
}

func (r *APIProductReconciler) validateSpec(ctx context.Context, apip *networkingv1beta1.APIProduct) error {
	logger := logr.FromContext(ctx)

	err := apip.Validate()
	logger.V(1).Info("validate SPEC", "error", err)
	if err != nil {
		return err
	}

	for _, apiSel := range apip.Spec.APIs {
		// Check API exist
		api := &networkingv1beta1.API{}
		err := r.Client().Get(ctx, apiSel.APINamespacedName(), api)
		logger.V(1).Info("get API", "objectKey", apiSel.APINamespacedName(), "error", err)
		if err != nil {
			return err
		}

		// Check destination service exist
		service := &corev1.Service{}
		err = r.Client().Get(ctx, api.Spec.Destination.NamespacedName(), service)
		logger.V(1).Info("get service", "objectKey", api.Spec.Destination.NamespacedName(), "error", err)
		if err != nil {
			return err
		}
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *APIProductReconciler) SetupWithManager(mgr ctrl.Manager) error {
	apiEventMapper := &APIProductAPIEventMapper{
		K8sClient: r.Client(),
		Logger:    r.Logger().WithName("apiHandler"),
	}
	return ctrl.NewControllerManagedBy(mgr).
		Watches(
			&source.Kind{Type: &networkingv1beta1.API{}},
			handler.EnqueueRequestsFromMapFunc(apiEventMapper.Map),
		).
		For(&networkingv1beta1.APIProduct{}).
		WithLogger(log.Log). // use base logger, the manager will add prefixes for watched sources
		Complete(r)
}
