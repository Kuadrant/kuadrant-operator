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

	egv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gwapiv1a2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	kuadrantenvoygateway "github.com/kuadrant/kuadrant-operator/pkg/envoygateway"
	"github.com/kuadrant/kuadrant-operator/pkg/kuadranttools"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/mappers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/reconcilers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools/wasm"
)

// RateLimitingEnvoyPatchPolicyReconciler reconciles an EnvoyPatchPolicy object for rate limiting
// https://gateway.envoyproxy.io/latest/api/extension_types/#envoypatchpolicy
type RateLimitingEnvoyPatchPolicyReconciler struct {
	*reconcilers.BaseReconciler
}

//+kubebuilder:rbac:groups=gateway.envoyproxy.io,resources=envoypatchpolicies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=kuadrant.io,resources=ratelimitpolicies,verbs=get;list;watch;update;patch

// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *RateLimitingEnvoyPatchPolicyReconciler) Reconcile(eventCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger().WithValues("Gateway", req.NamespacedName)
	logger.Info("Reconciling rate limiting EnvoyPatchPolicy")
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

	kObj, err := kuadranttools.KuadrantFromGateway(ctx, r.Client(), gw)
	if err != nil {
		logger.Info("failed to read kuadrant instance")
		return ctrl.Result{}, err
	}

	if kObj == nil {
		logger.Info("kuadrant instance not found, maybe not the gateway is not assigned to kuadrant")
		return ctrl.Result{}, nil
	}

	desired, err := r.desiredEnvoyPatchPolicy(ctx, gw, kObj)
	if err != nil {
		return ctrl.Result{}, err
	}

	// TODO Mutator!! -> support upgrade
	err = r.ReconcileResource(ctx, &egv1alpha1.EnvoyPatchPolicy{}, desired, reconcilers.CreateOnlyMutator)
	if err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("Rate limiting envoypatchpolicy reconciled successfully")
	return ctrl.Result{}, nil
}

func (r *RateLimitingEnvoyPatchPolicyReconciler) desiredEnvoyPatchPolicy(ctx context.Context, gw *gatewayapiv1.Gateway, kObj *kuadrantv1beta1.Kuadrant) (*egv1alpha1.EnvoyPatchPolicy, error) {
	baseLogger, err := logr.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	pathPolicy := &egv1alpha1.EnvoyPatchPolicy{
		TypeMeta: metav1.TypeMeta{
			Kind:       "EnvoyPatchPolicy",
			APIVersion: egv1alpha1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      kuadrantenvoygateway.RateLimitEnvoyPatchPolicyName(gw),
			Namespace: gw.Namespace,
		},
		Spec: egv1alpha1.EnvoyPatchPolicySpec{
			TargetRef: gwapiv1a2.PolicyTargetReference{
				Group:     gatewayapiv1.GroupName,
				Kind:      "Gateway",
				Name:      gatewayapiv1.ObjectName(gw.Name),
				Namespace: ptr.To(gatewayapiv1.Namespace(gw.Namespace)),
			},
			Type:        egv1alpha1.JSONPatchEnvoyPatchType,
			JSONPatches: nil,
		},
	}

	logger := baseLogger.WithValues("envoypatchpolicy", client.ObjectKeyFromObject(pathPolicy))

	//
	// Limitador Service Cluster patch
	//
	limitador, err := kuadranttools.LimitadorLocation(ctx, r.Client(), kObj)
	if err != nil {
		return nil, err
	}
	pathPolicy.Spec.JSONPatches = append(pathPolicy.Spec.JSONPatches,
		kuadrantenvoygateway.LimitadorClusterPatch(
			limitador.Status.Service.Host,
			int(limitador.Status.Service.Ports.GRPC),
		),
	)

	//
	// Wasm filter patch
	//
	wasmConfig, err := wasm.ConfigFromGateway(ctx, r.Client(), gw)
	if err != nil {
		return nil, err
	}

	if wasmConfig == nil || len(wasmConfig.RateLimitPolicies) == 0 {
		logger.V(1).Info("wasmConfig is empty. EnvoyPatchPolicy will be deleted if it exists")
		utils.TagObjectToDelete(pathPolicy)
		return pathPolicy, nil
	}
	wasmConfigJSON, err := json.Marshal(wasmConfig)
	if err != nil {
		return nil, err
	}
	pathPolicy.Spec.JSONPatches = append(pathPolicy.Spec.JSONPatches, kuadrantenvoygateway.WasmFilterPatch(
		gw,
		"https://raw.githubusercontent.com/Kuadrant/wasm-shim/release-binaries/releases/kuadrant-ratelimit-wasm-v0.4.0-alpha.1",
		"b101508ddd5fd40eb2116204e6c768332a359c21feb2dbb348956459349e7d71",
		"raw_githubusercontent_com_443",
		string(wasmConfigJSON)))

	//
	// Wasm Binary Cluster patch
	//
	pathPolicy.Spec.JSONPatches = append(pathPolicy.Spec.JSONPatches,
		kuadrantenvoygateway.WasmBinarySourceClusterPatch(
			"raw_githubusercontent_com_443",
			"raw.githubusercontent.com",
			443,
		),
	)

	// controller reference
	if err := r.SetOwnerReference(gw, pathPolicy); err != nil {
		return nil, err
	}

	return pathPolicy, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RateLimitingEnvoyPatchPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	ok, err := kuadrantenvoygateway.IsEnvoyGatewayEnvoyPatchPolicyInstalled(mgr.GetRESTMapper())
	if err != nil {
		return err
	}
	if !ok {
		r.Logger().Info("EnvoyGateway EnvoyPatchPolicy controller disabled. API was not found")
		return nil
	}

	ok, err = kuadrantgatewayapi.IsGatewayAPIInstalled(mgr.GetRESTMapper())
	if err != nil {
		return err
	}
	if !ok {
		r.Logger().Info("EnvoyGateway EnvoyPatchPolicy controller disabled. GatewayAPI was not found")
		return nil
	}

	httpRouteToParentGatewaysEventMapper := mappers.NewHTTPRouteToParentGatewaysEventMapper(
		mappers.WithLogger(r.Logger().WithName("httpRouteToParentGatewaysEventMapper")),
	)

	rlpToParentGatewaysEventMapper := mappers.NewPolicyToParentGatewaysEventMapper(
		mappers.WithLogger(r.Logger().WithName("ratelimitpolicyToParentGatewaysEventMapper")),
		mappers.WithClient(r.Client()),
	)

	return ctrl.NewControllerManagedBy(mgr).
		// Rate limiting EnvoyGateway EnvoyPatchPolicy controller only cares about
		// Gateway API Gateway
		// Gateway API HTTPRoutes
		// Kuadrant RateLimitPolicies

		For(&gatewayapiv1.Gateway{}).
		Owns(&egv1alpha1.EnvoyPatchPolicy{}).
		Watches(
			&gatewayapiv1.HTTPRoute{},
			handler.EnqueueRequestsFromMapFunc(httpRouteToParentGatewaysEventMapper.Map),
		).
		Watches(
			&kuadrantv1beta2.RateLimitPolicy{},
			handler.EnqueueRequestsFromMapFunc(rlpToParentGatewaysEventMapper.Map),
		).
		Complete(r)
}
