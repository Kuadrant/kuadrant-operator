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
	"strings"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/reconcilers"
)

// GatewayKuadrantReconciler reconciles Gateway object with kuadrant metadata
type GatewayKuadrantReconciler struct {
	*reconcilers.BaseReconciler
}

//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=kuadrant.io,resources=kuadrants,verbs=get;list

// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *GatewayKuadrantReconciler) Reconcile(eventCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger().WithValues("Gateway", req.NamespacedName)
	logger.Info("Reconciling Kuadrant annotations")
	ctx := logr.NewContext(eventCtx, logger)

	gw := &gatewayapiv1.Gateway{}
	if err := r.Client().Get(ctx, req.NamespacedName, gw); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("no gateway found")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "failed to get gateway")
		return ctrl.Result{}, err
	}

	if logger.V(1).Enabled() {
		jsonData, err := json.MarshalIndent(gw, "", "  ")
		if err != nil {
			return ctrl.Result{}, err
		}
		logger.V(1).Info(string(jsonData))
	}

	err := r.reconcileGatewayKuadrantMetadata(ctx, gw)

	if err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("Gateway kuadrant annotations reconciled successfully")
	return ctrl.Result{}, nil
}

func (r *GatewayKuadrantReconciler) reconcileGatewayKuadrantMetadata(ctx context.Context, gw *gatewayapiv1.Gateway) error {
	updated, err := r.reconcileKuadrantNamespaceAnnotation(ctx, gw)
	if err != nil {
		return err
	}

	if updated {
		if err := r.Client().Update(ctx, gw); err != nil {
			return err
		}
	}

	return nil
}

func (r *GatewayKuadrantReconciler) reconcileKuadrantNamespaceAnnotation(ctx context.Context, gw *gatewayapiv1.Gateway) (bool, error) {
	logger, err := logr.FromContext(ctx)
	if err != nil {
		return false, err
	}

	if common.IsKuadrantManaged(gw) {
		return false, nil
	}

	kuadrantList := &kuadrantv1beta1.KuadrantList{}
	if err := r.Client().List(ctx, kuadrantList); err != nil {
		return false, err
	}
	if len(kuadrantList.Items) == 0 {
		// Kuadrant was not found
		logger.Info("Kuadrant instance not found in the cluster")
		return false, nil
	}

	if len(kuadrantList.Items) > 1 {
		// multiple kuadrant instances? not supported
		keys := make([]string, len(kuadrantList.Items))
		for idx := range kuadrantList.Items {
			keys[idx] = client.ObjectKeyFromObject(&kuadrantList.Items[idx]).String()
		}
		logger.Info("Multiple kuadrant instances found", "num", len(kuadrantList.Items), "keys", strings.Join(keys[:], ","))
		return false, nil
	}

	common.AnnotateObject(gw, kuadrantList.Items[0].Namespace)

	return true, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *GatewayKuadrantReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// Gateway Kuadrant controller only cares about the annotations
		For(&gatewayapiv1.Gateway{}, builder.WithPredicates(predicate.AnnotationChangedPredicate{})).
		Complete(r)
}
