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
	"errors"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	"github.com/kuadrant/limitador-operator/api/v1alpha1"
	istioextensionv1alpha3 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	apimv1alpha1 "github.com/kuadrant/kuadrant-controller/apis/apim/v1alpha1"
	"github.com/kuadrant/kuadrant-controller/pkg/common"
	kuadrantistioutils "github.com/kuadrant/kuadrant-controller/pkg/istio"
	"github.com/kuadrant/kuadrant-controller/pkg/reconcilers"
)

// RateLimitPolicyReconciler reconciles a RateLimitPolicy object
type RateLimitPolicyReconciler struct {
	*reconcilers.BaseReconciler
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=apim.kuadrant.io,resources=ratelimitpolicies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apim.kuadrant.io,resources=ratelimitpolicies/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=apim.kuadrant.io,resources=ratelimitpolicies/finalizers,verbs=update
//+kubebuilder:rbac:groups=networking.istio.io,resources=gateways,verbs=get;list;watch
//+kubebuilder:rbac:groups=extensions.istio.io,resources=wasmplugins,verbs=get;list;watch;create;delete;update;patch
//+kubebuilder:rbac:groups=limitador.kuadrant.io,resources=ratelimits,verbs=get;list;watch;create;update;delete;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the RateLimitPolicy object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *RateLimitPolicyReconciler) Reconcile(eventCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger().WithValues("RateLimitPolicy", req.NamespacedName)
	logger.Info("Reconciling RateLimitPolicy")
	ctx := logr.NewContext(eventCtx, logger)

	rlp := &apimv1alpha1.RateLimitPolicy{}
	if err := r.Client().Get(ctx, req.NamespacedName, rlp); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("no RateLimitPolicy found")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "failed to get RateLimitPolicy")
		return ctrl.Result{}, err
	}

	if rlp.GetDeletionTimestamp() != nil && controllerutil.ContainsFinalizer(rlp, rateLimitPolicyFinalizer) {
		logger.V(1).Info("Handling removal of ratelimitpolicy object")
		if err := r.finalizeWASMPlugins(ctx, rlp); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.deleteRateLimits(ctx, rlp); err != nil {
			return ctrl.Result{}, err
		}

		if err := r.deleteNetworkResourceBackReference(ctx, rlp); err != nil {
			return ctrl.Result{}, err
		}

		logger.Info("removing finalizer")
		controllerutil.RemoveFinalizer(rlp, rateLimitPolicyFinalizer)
		if err := r.UpdateResource(ctx, rlp); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Ignore deleted resources, this can happen when foregroundDeletion is enabled
	// https://kubernetes.io/docs/concepts/workloads/controllers/garbage-collection/#foreground-cascading-deletion
	if rlp.GetDeletionTimestamp() != nil {
		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(rlp, rateLimitPolicyFinalizer) {
		controllerutil.AddFinalizer(rlp, rateLimitPolicyFinalizer)
		if err := r.UpdateResource(ctx, rlp); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{Requeue: true}, err
		}
	}

	specResult, specErr := r.reconcileSpec(ctx, rlp)
	if specErr == nil && specResult.Requeue {
		logger.V(1).Info("Reconciling spec not finished. Requeueing.")
		return specResult, nil
	}

	statusResult, statusErr := r.reconcileStatus(ctx, rlp, specErr)

	if specErr != nil {
		return ctrl.Result{}, specErr
	}

	if statusErr != nil {
		return ctrl.Result{}, statusErr
	}

	if statusResult.Requeue {
		logger.V(1).Info("Reconciling status not finished. Requeueing.")
		return statusResult, nil
	}

	logger.Info("successfully reconciled RateLimitPolicy")
	return ctrl.Result{}, nil
}

func (r *RateLimitPolicyReconciler) reconcileSpec(ctx context.Context, rlp *apimv1alpha1.RateLimitPolicy) (ctrl.Result, error) {
	err := rlp.Validate()
	if err != nil {
		return ctrl.Result{}, err
	}

	err = r.validateHTTPRoute(ctx, rlp)
	if err != nil {
		return ctrl.Result{}, err
	}

	err = r.reconcileNetworkResourceBackReference(ctx, rlp)
	if err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileLimits(ctx, rlp); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileWASMPlugins(ctx, rlp); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.cleanUpOrphanWASMPlugins(ctx, rlp); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *RateLimitPolicyReconciler) reconcileLimits(ctx context.Context, rlp *apimv1alpha1.RateLimitPolicy) error {
	logger, _ := logr.FromContext(ctx)
	rlpKey := client.ObjectKeyFromObject(rlp)

	// create the RateLimit resource
	for i, rlSpec := range rlp.Spec.Limits {
		ratelimitfactory := common.RateLimitFactory{
			Key: client.ObjectKey{
				Name: limitadorRatelimitsName(rlpKey, i+1),
				// Currently, Limitador Operator (v0.2.0) will configure limitador services with
				// RateLimit CRs created in the same namespace.
				Namespace: common.KuadrantNamespace,
			},
			Conditions: rlSpec.Conditions,
			MaxValue:   rlSpec.MaxValue,
			Namespace:  rlSpec.Namespace,
			Variables:  rlSpec.Variables,
			Seconds:    rlSpec.Seconds,
		}

		ratelimit := ratelimitfactory.RateLimit()
		err := r.ReconcileResource(ctx, &v1alpha1.RateLimit{}, ratelimit, alwaysUpdateRateLimit)
		if err != nil && !apierrors.IsAlreadyExists(err) {
			logger.Error(err, "ReconcileResource failed to create/update RateLimit resource")
			return err
		}
	}
	logger.Info("successfully created/updated RateLimit resources")
	return nil
}

func (r *RateLimitPolicyReconciler) reconcileNetworkResourceBackReference(ctx context.Context, rlp *apimv1alpha1.RateLimitPolicy) error {
	logger, _ := logr.FromContext(ctx)
	httpRoute, err := r.fetchHTTPRoute(ctx, rlp)
	if err != nil {
		// The object should also exist
		return err
	}

	// Reconcile the back reference:
	httpRouteAnnotations := httpRoute.GetAnnotations()
	if httpRouteAnnotations == nil {
		httpRouteAnnotations = map[string]string{}
	}

	rlpKey := client.ObjectKeyFromObject(rlp)
	val, ok := httpRouteAnnotations[common.RateLimitPolicyBackRefAnnotation]
	if ok {
		if val != rlpKey.String() {
			return fmt.Errorf("the target HTTPRoute {%s} is already referenced by ratelimitpolicy %s", client.ObjectKeyFromObject(httpRoute), rlpKey.String())
		}
	} else {
		httpRouteAnnotations[common.RateLimitPolicyBackRefAnnotation] = rlpKey.String()
		httpRoute.SetAnnotations(httpRouteAnnotations)
		err := r.UpdateResource(ctx, httpRoute)
		logger.V(1).Info("reconcileNetworkResourceBackReference: update HTTPRoute", "httpRoute", client.ObjectKeyFromObject(httpRoute), "err", err)
		if err != nil {
			return err
		}
	}

	return nil
}

// Finds gateways with WASMPLugins with rate limit configuration from the current RLP
// Delete RL conf from the current RLP from gateways not referenced by the current RLP
// Cleans up RL conf when:
// - HTTPRoute updates parentRefs (gateways)
// - RLP updates targetRef to another HTTPRoute
func (r *RateLimitPolicyReconciler) cleanUpOrphanWASMPlugins(ctx context.Context, rlp *apimv1alpha1.RateLimitPolicy) error {
	logger, _ := logr.FromContext(ctx)

	currentGatewayRefs, err := r.gatewayRefList(ctx, rlp)
	if err != nil {
		return err
	}

	// TODO(rahulanand16nov): maybe think about optimizing it with a label later
	gwList := &gatewayapiv1alpha2.GatewayList{}
	err = r.Client().List(ctx, gwList)
	if err != nil {
		return err
	}

	gwKeyList := make([]client.ObjectKey, 0)
	for idx := range gwList.Items {
		gwKeyList = append(gwKeyList, client.ObjectKeyFromObject(&gwList.Items[idx]))
	}

	notReferencedGatewayKeys := common.ObjectKeyListDifference(gwKeyList, currentGatewayRefs)

	RateLimitStages := []apimv1alpha1.RateLimitStage{apimv1alpha1.RateLimitStagePREAUTH, apimv1alpha1.RateLimitStagePOSTAUTH}
	for _, gwKey := range notReferencedGatewayKeys {
		for _, stage := range RateLimitStages {
			wasmKey := kuadrantistioutils.WASMPluginKey(gwKey, stage)

			wasmplugin := &istioextensionv1alpha3.WasmPlugin{}
			err = r.Client().Get(ctx, wasmKey, wasmplugin)
			logger.V(1).Info("cleanUpOrphanWASMPlugins: get WasmPlugin", "wasmplugin", wasmKey, "err", err)
			if apierrors.IsNotFound(err) {
				logger.V(1).Info("cleanUpOrphanWASMPlugins: wasmplugin not found. Nothing to do", "wasmplugin", wasmKey)
				continue
			}
			if err != nil {
				return err
			}
			err = r.finalizeSingleWASMPlugins(ctx, rlp, wasmplugin)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *RateLimitPolicyReconciler) reconcileWASMPlugins(ctx context.Context, rlp *apimv1alpha1.RateLimitPolicy) error {
	logger, _ := logr.FromContext(ctx)

	httpRoute, err := r.fetchHTTPRoute(ctx, rlp)
	if err != nil {
		// The object should also exist
		return err
	}

	currentGatewayRefs, err := r.gatewayRefList(ctx, rlp)
	if err != nil {
		return err
	}

	for idx := range currentGatewayRefs {
		gwKey := currentGatewayRefs[idx]
		gateway := &gatewayapiv1alpha2.Gateway{}
		err := r.Client().Get(ctx, gwKey, gateway)
		logger.V(1).Info("reconcileWASMPlugins: get Gateway", "gateway", gwKey, "err", err)
		if err != nil {
			// gateway needs to exist
			return err
		}

		// Reconcile two WasmPlugins per gateway
		// Gateway API Gateway resource labels will be copied to the deployment in the automated deployment
		// For the manual deployment, the Gateway resource labels must match deployment/pod labels or WASMPlugins selector will not match
		// https://istio.io/latest/docs/tasks/traffic-management/ingress/gateway-api/#automated-deployment
		wps, err := kuadrantistioutils.WasmPlugins(rlp, gwKey, gateway.GetLabels(), httpRoute.Spec.Hostnames)
		if err != nil {
			return err
		}

		for _, wp := range wps {
			err = r.ReconcileResource(ctx, &istioextensionv1alpha3.WasmPlugin{}, wp, kuadrantistioutils.WASMPluginMutator)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *RateLimitPolicyReconciler) fetchHTTPRoute(ctx context.Context, rlp *apimv1alpha1.RateLimitPolicy) (*gatewayapiv1alpha2.HTTPRoute, error) {
	logger, _ := logr.FromContext(ctx)

	tmpNS := rlp.Namespace
	if rlp.Spec.TargetRef.Namespace != nil {
		tmpNS = string(*rlp.Spec.TargetRef.Namespace)
	}

	key := client.ObjectKey{
		Name:      string(rlp.Spec.TargetRef.Name),
		Namespace: tmpNS,
	}

	httpRoute := &gatewayapiv1alpha2.HTTPRoute{}
	err := r.Client().Get(ctx, key, httpRoute)
	logger.V(1).Info("fetchHTTPRoute", "httpRoute", key, "err", err)
	if err != nil {
		return nil, err
	}

	return httpRoute, nil
}

func (r *RateLimitPolicyReconciler) gatewayRefList(ctx context.Context, rlp *apimv1alpha1.RateLimitPolicy) ([]client.ObjectKey, error) {
	httpRoute, err := r.fetchHTTPRoute(ctx, rlp)
	if err != nil {
		// The object should also exist
		return nil, err
	}

	gwKeys := make([]client.ObjectKey, 0)
	for _, parentRef := range httpRoute.Spec.CommonRouteSpec.ParentRefs {
		gwKey := client.ObjectKey{Name: string(parentRef.Name), Namespace: httpRoute.Namespace}
		if parentRef.Namespace != nil {
			gwKey.Namespace = string(*parentRef.Namespace)
		}
		gwKeys = append(gwKeys, gwKey)
	}

	return gwKeys, nil
}

func (r *RateLimitPolicyReconciler) validateHTTPRoute(ctx context.Context, rlp *apimv1alpha1.RateLimitPolicy) error {
	httpRoute, err := r.fetchHTTPRoute(ctx, rlp)
	if err != nil {
		// The object should exist
		return err
	}

	// Check HTTProute parents (gateways) in the status object
	// if any of the current parent gateways reports not "Admitted", return error
	for _, parentRef := range httpRoute.Spec.CommonRouteSpec.ParentRefs {
		routeParentStatus := func(pRef gatewayapiv1alpha2.ParentRef) *gatewayapiv1alpha2.RouteParentStatus {
			for idx := range httpRoute.Status.RouteStatus.Parents {
				if reflect.DeepEqual(pRef, httpRoute.Status.RouteStatus.Parents[idx].ParentRef) {
					return &httpRoute.Status.RouteStatus.Parents[idx]
				}
			}

			return nil
		}(parentRef)

		if routeParentStatus == nil {
			continue
		}

		if meta.IsStatusConditionFalse(routeParentStatus.Conditions, "Accepted") {
			return errors.New("httproute not accepted")
		}
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RateLimitPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	HTTPRouteEventMapper := &HTTPRouteEventMapper{
		Logger: r.Logger().WithName("httpRouteHandler"),
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&apimv1alpha1.RateLimitPolicy{}).
		Watches(
			&source.Kind{Type: &gatewayapiv1alpha2.HTTPRoute{}},
			handler.EnqueueRequestsFromMapFunc(HTTPRouteEventMapper.MapToRateLimitPolicy),
		).
		Complete(r)
}
