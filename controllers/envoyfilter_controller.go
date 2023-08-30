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
	"fmt"

	"github.com/go-logr/logr"
	istioapinetworkingv1alpha3 "istio.io/api/networking/v1alpha3"
	istioclientnetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/kuadrant/kuadrant-operator/pkg/common"
	kuadrantistioutils "github.com/kuadrant/kuadrant-operator/pkg/istio"
	"github.com/kuadrant/kuadrant-operator/pkg/reconcilers"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
)

// EnvoyFilterReconciler reconciles a EnvoyFilter object
type EnvoyFilterReconciler struct {
	*reconcilers.BaseReconciler
}

//+kubebuilder:rbac:groups=networking.istio.io,resources=envoyfilters,verbs=get;list;watch;create;update;patch;delete

// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *EnvoyFilterReconciler) Reconcile(eventCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger().WithValues("Gateway", req.NamespacedName)
	logger.Info("Reconciling EnvoyFilter")
	ctx := logr.NewContext(eventCtx, logger)

	gw := &gatewayapiv1beta1.Gateway{}
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

	err := r.reconcileRateLimitingClusterEnvoyFilter(ctx, gw)

	if err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("EnvoyFilter reconciled successfully")
	return ctrl.Result{}, nil
}

func (r *EnvoyFilterReconciler) reconcileRateLimitingClusterEnvoyFilter(ctx context.Context, gw *gatewayapiv1beta1.Gateway) error {
	desired, err := r.desiredRateLimitingClusterEnvoyFilter(ctx, gw)
	if err != nil {
		return err
	}

	err = r.ReconcileResource(ctx, &istioclientnetworkingv1alpha3.EnvoyFilter{}, desired, kuadrantistioutils.AlwaysUpdateEnvoyFilter)
	if err != nil {
		return err
	}

	return nil
}

func (r *EnvoyFilterReconciler) desiredRateLimitingClusterEnvoyFilter(ctx context.Context, gw *gatewayapiv1beta1.Gateway) (*istioclientnetworkingv1alpha3.EnvoyFilter, error) {
	logger, err := logr.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	ef := &istioclientnetworkingv1alpha3.EnvoyFilter{
		TypeMeta: metav1.TypeMeta{
			Kind:       "EnvoyFilter",
			APIVersion: "networking.istio.io/v1alpha3",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("kuadrant-ratelimiting-cluster-%s", gw.Name),
			Namespace: gw.Namespace,
		},
		Spec: istioapinetworkingv1alpha3.EnvoyFilter{
			WorkloadSelector: &istioapinetworkingv1alpha3.WorkloadSelector{
				Labels: common.IstioWorkloadSelectorFromGateway(ctx, r.Client(), gw).MatchLabels,
			},
			ConfigPatches: nil,
		},
	}

	gateway := common.GatewayWrapper{Gateway: gw, PolicyRefsConfig: &common.KuadrantRateLimitPolicyRefsConfig{}}
	rlpRefs := gateway.PolicyRefs()
	logger.V(1).Info("desiredRateLimitingClusterEnvoyFilter", "rlpRefs", rlpRefs)

	if len(rlpRefs) < 1 {
		common.TagObjectToDelete(ef)
		return ef, nil
	}

	kuadrantNamespace, err := common.GetKuadrantNamespace(gw)
	if err != nil {
		return nil, err
	}

	limitadorKey := client.ObjectKey{Name: common.LimitadorName, Namespace: kuadrantNamespace}
	limitador := &limitadorv1alpha1.Limitador{}
	err = r.Client().Get(ctx, limitadorKey, limitador)
	logger.V(1).Info("desiredRateLimitingClusterEnvoyFilter", "get limitador", limitadorKey, "err", err)
	if err != nil {
		return nil, err
	}

	if !meta.IsStatusConditionTrue(limitador.Status.Conditions, "Ready") {
		return nil, fmt.Errorf("limitador Status not ready")
	}

	configPatches, err := kuadrantistioutils.LimitadorClusterPatch(limitador.Status.Service.Host, int(limitador.Status.Service.Ports.GRPC))
	if err != nil {
		return nil, err
	}
	ef.Spec.ConfigPatches = configPatches

	// controller reference
	if err := r.SetOwnerReference(gw, ef); err != nil {
		return nil, err
	}

	return ef, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *EnvoyFilterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gatewayapiv1beta1.Gateway{}).
		Owns(&istioclientnetworkingv1alpha3.EnvoyFilter{}).
		Complete(r)
}
