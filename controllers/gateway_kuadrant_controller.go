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
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/mappers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/reconcilers"
)

// GatewayKuadrantReconciler is responsible of assiging gateways to a kuadrant instances
// Currently only one kuadrant instance is allowed per cluster
// This controller will annotate every gateway in the cluster
// with the namespace of the kuadrant instance
// TODO: After the RFC defined, we might want to get the gw to label/annotate from Kuadrant.Spec or manual labeling/annotation
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

	err := r.reconcileGatewayWithKuadrantMetadata(ctx, gw)

	if err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("Gateway kuadrant annotations reconciled successfully")
	return ctrl.Result{}, nil
}

func (r *GatewayKuadrantReconciler) reconcileGatewayWithKuadrantMetadata(ctx context.Context, gw *gatewayapiv1.Gateway) error {
	logger, err := logr.FromContext(ctx)
	if err != nil {
		return err
	}

	kuadrantList := &kuadrantv1beta1.KuadrantList{}
	if err := r.Client().List(ctx, kuadrantList); err != nil {
		return err
	}

	if len(kuadrantList.Items) > 1 {
		// multiple kuadrant instances? not supported
		keys := make([]string, len(kuadrantList.Items))
		for idx := range kuadrantList.Items {
			keys[idx] = client.ObjectKeyFromObject(&kuadrantList.Items[idx]).String()
		}
		logger.Info("Multiple kuadrant instances found", "num", len(kuadrantList.Items), "keys", strings.Join(keys[:], ","))
		return nil
	}

	if len(kuadrantList.Items) == 0 {
		logger.Info("Kuadrant instance not found in the cluster")
		return r.removeKuadrantNamespaceAnnotation(ctx, gw)
	}

	val, ok := gw.GetAnnotations()[kuadrant.KuadrantNamespaceAnnotation]
	if !ok || val != kuadrantList.Items[0].Namespace {
		// Either the annotation does not exist or
		// the namespace differs from the available Kuadrant CR, hence the gateway is updated.
		annotations := gw.GetAnnotations()
		if annotations == nil {
			annotations = map[string]string{}
		}
		annotations[kuadrant.KuadrantNamespaceAnnotation] = kuadrantList.Items[0].Namespace
		gw.SetAnnotations(annotations)
		logger.Info("annotate gateway with kuadrant namespace", "namespace", kuadrantList.Items[0].Namespace)
		return r.UpdateResource(ctx, gw)
	}

	return nil
}

func (r *GatewayKuadrantReconciler) removeKuadrantNamespaceAnnotation(ctx context.Context, gw *gatewayapiv1.Gateway) error {
	logger, err := logr.FromContext(ctx)
	if err != nil {
		return err
	}

	if _, ok := gw.GetAnnotations()[kuadrant.KuadrantNamespaceAnnotation]; ok {
		delete(gw.Annotations, kuadrant.KuadrantNamespaceAnnotation)
		logger.Info("remove gateway annotation with kuadrant namespace")
		return r.UpdateResource(ctx, gw)
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *GatewayKuadrantReconciler) SetupWithManager(mgr ctrl.Manager) error {
	ok, err := kuadrantgatewayapi.IsGatewayAPIInstalled(mgr.GetRESTMapper())
	if err != nil {
		return err
	}
	if !ok {
		r.Logger().Info("GatewayKuadrant controller disabled. GatewayAPI was not found")
		return nil
	}

	// maps any kuadrant event to gateway event
	// on any kuadrant event, one reconciliation request for every gateway in the cluster is created
	kuadrantToGatewayEventMapper := mappers.NewKuadrantToGatewayEventMapper(
		mappers.WithLogger(r.Logger().WithName("kuadrantToGatewayEventMapper")),
		mappers.WithClient(r.Client()),
	)

	return ctrl.NewControllerManagedBy(mgr).
		// Gateway Kuadrant controller only cares about the annotations
		For(&gatewayapiv1.Gateway{}, builder.WithPredicates(predicate.AnnotationChangedPredicate{})).
		// Watch for any kuadrant CR being created or deleted
		Watches(
			&kuadrantv1beta1.Kuadrant{},
			handler.EnqueueRequestsFromMapFunc(kuadrantToGatewayEventMapper.Map),
			builder.WithPredicates(predicate.Funcs{
				UpdateFunc: func(event.UpdateEvent) bool {
					// The reconciler only cares about creation/deletion events
					return false
				},
			}),
		).
		Complete(r)
}
