/*
Copyright 2021.

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
	authorinov1beta1 "github.com/kuadrant/authorino-operator/api/v1beta1"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/reconcilers"
	"github.com/kuadrant/kuadrant-operator/pkg/log"
)

const (
	kuadrantFinalizer = "kuadrant.io/finalizer"
)

// KuadrantReconciler reconciles a Kuadrant object
type KuadrantReconciler struct {
	*reconcilers.BaseReconciler
	RestMapper meta.RESTMapper
}

//+kubebuilder:rbac:groups=kuadrant.io,resources=kuadrants,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=kuadrant.io,resources=kuadrants/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=kuadrant.io,resources=kuadrants/finalizers,verbs=update
//+kubebuilder:rbac:groups=limitador.kuadrant.io,resources=limitadors,verbs=get;list;watch;create;update;delete;patch

//+kubebuilder:rbac:groups=core,resources=serviceaccounts;configmaps;services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=coordination.k8s.io,resources=configmaps;leases,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch
//+kubebuilder:rbac:groups="",resources=leases,verbs=get;list;watch;create;update;patch;delete

//+kubebuilder:rbac:groups=operator.authorino.kuadrant.io,resources=authorinos,verbs=get;list;watch;create;update;delete;patch
//+kubebuilder:rbac:groups=install.istio.io,resources=istiooperators,verbs=get;list;watch;create;update;patch
//+kubebuilder:rbac:groups=operator.istio.io,resources=istios,verbs=get;list;watch;create;update;patch
//+kubebuilder:rbac:groups=maistra.io,resources=servicemeshcontrolplanes,verbs=get;list;watch;update;use;patch
//+kubebuilder:rbac:groups=maistra.io,resources=servicemeshmembers,verbs=get;list;watch;create;update;delete;patch

// Common permissions required by policy controllers
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways/finalizers,verbs=update
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes/status,verbs=get;update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *KuadrantReconciler) Reconcile(eventCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.Log.WithValues("kuadrant", req.NamespacedName)
	logger.Info("Reconciling")
	ctx := logr.NewContext(eventCtx, logger)

	kObj := &kuadrantv1beta1.Kuadrant{}
	if err := r.Client().Get(ctx, req.NamespacedName, kObj); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("no kuadrant object found")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "failed to get kuadrant object")
		return ctrl.Result{}, err
	}

	if logger.V(1).Enabled() {
		jsonData, err := json.MarshalIndent(kObj, "", "  ")
		if err != nil {
			return ctrl.Result{}, err
		}
		logger.V(1).Info(string(jsonData))
	}

	if kObj.GetDeletionTimestamp() != nil && controllerutil.ContainsFinalizer(kObj, kuadrantFinalizer) {
		logger.V(1).Info("Handling removal of kuadrant object")

		logger.Info("removing finalizer")
		controllerutil.RemoveFinalizer(kObj, kuadrantFinalizer)
		if err := r.Client().Update(ctx, kObj); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Ignore deleted resources, this can happen when foregroundDeletion is enabled
	// https://kubernetes.io/docs/concepts/workloads/controllers/garbage-collection/#foreground-cascading-deletion
	if kObj.GetDeletionTimestamp() != nil {
		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(kObj, kuadrantFinalizer) {
		controllerutil.AddFinalizer(kObj, kuadrantFinalizer)
		if err := r.Client().Update(ctx, kObj); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{Requeue: true}, err
		}
	}

	statusResult, err := r.reconcileStatus(ctx, kObj, nil)
	if err != nil {
		return ctrl.Result{}, err
	}

	if statusResult.Requeue {
		logger.V(1).Info("Reconciling status not finished. Requeueing.")
		return statusResult, nil
	}

	logger.Info("successfully reconciled")
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KuadrantReconciler) SetupWithManager(mgr ctrl.Manager) error {
	ok, err := kuadrantgatewayapi.IsGatewayAPIInstalled(mgr.GetRESTMapper())
	if err != nil {
		return err
	}
	if !ok {
		r.Logger().Info("Kuadrant controller disabled. GatewayAPI was not found")
		return nil
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&kuadrantv1beta1.Kuadrant{}).
		Owns(&limitadorv1alpha1.Limitador{}).
		Owns(&authorinov1beta1.Authorino{}).
		Complete(r)
}
