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
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
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
	"github.com/kuadrant/kuadrant-controller/pkg/reconcilers"
	"github.com/kuadrant/kuadrant-controller/pkg/rlptools"
)

// RateLimitPolicyReconciler reconciles a RateLimitPolicy object
type RateLimitPolicyReconciler struct {
	*reconcilers.BaseReconciler
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=apim.kuadrant.io,resources=ratelimitpolicies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apim.kuadrant.io,resources=ratelimitpolicies/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=apim.kuadrant.io,resources=ratelimitpolicies/finalizers,verbs=update
//+kubebuilder:rbac:groups=limitador.kuadrant.io,resources=limitadors,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.istio.io,resources=envoyfilters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=extensions.istio.io,resources=wasmplugins,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=create;get;list;watch;update;patch

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

	if logger.V(1).Enabled() {
		jsonData, err := json.MarshalIndent(rlp, "", "  ")
		if err != nil {
			return ctrl.Result{}, err
		}
		logger.V(1).Info(string(jsonData))
	}

	if rlp.GetDeletionTimestamp() != nil && controllerutil.ContainsFinalizer(rlp, rateLimitPolicyFinalizer) {
		if err := r.finalizeRLP(ctx, rlp); err != nil {
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

	// Ensure only one RLP is targeting the Gateway/HTTPRoute
	err = r.reconcileDirectBackReference(ctx, rlp)
	if err != nil {
		return ctrl.Result{}, err
	}

	err = r.reconcileGatewayDiffs(ctx, rlp)
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *RateLimitPolicyReconciler) reconcileGatewayDiffs(ctx context.Context, rlp *apimv1alpha1.RateLimitPolicy) error {
	// Reconcile based on gateway diffs:
	// * Limits
	// * WASM Plugin configuration object
	// * EnvoyFilter
	// * Gateway rate limit policy annotations (last)
	logger, _ := logr.FromContext(ctx)

	gatewayDiffObj, err := r.computeGatewayDiffs(ctx, rlp)
	if err != nil {
		return err
	}
	if gatewayDiffObj == nil {
		logger.V(1).Info("gatewayDiffObj is nil")
		return nil
	}

	if err := r.reconcileLimits(ctx, rlp, gatewayDiffObj); err != nil {
		return err
	}

	if err := r.reconcileRateLimitingClusterEnvoyFilter(ctx, rlp, gatewayDiffObj); err != nil {
		return err
	}

	if err := r.reconcileWASMPluginConf(ctx, rlp, gatewayDiffObj); err != nil {
		return err
	}

	// should be the last step, only when all the reconciliation steps succeed
	if err := r.reconcileGatewayRLPReferences(ctx, rlp, gatewayDiffObj); err != nil {
		return err
	}

	return nil
}

func (r *RateLimitPolicyReconciler) rlpGatewayKeys(ctx context.Context, rlp *apimv1alpha1.RateLimitPolicy) ([]client.ObjectKey, error) {
	gwKeys := make([]client.ObjectKey, 0)
	if rlp.IsForHTTPRoute() {
		httpRoute, err := r.fetchHTTPRoute(ctx, rlp)
		if err != nil {
			return nil, err
		}

		gwKeys = r.gatewayRefListFromHTTPRoute(httpRoute)
	} else if rlp.IsForGateway() {
		gwKey := client.ObjectKey{Name: string(rlp.Spec.TargetRef.Name), Namespace: rlp.GetNamespace()}
		if rlp.Spec.TargetRef.Namespace != nil {
			gwKey.Namespace = string(*rlp.Spec.TargetRef.Namespace)
		}
		gwKeys = []client.ObjectKey{gwKey}
	}

	return gwKeys, nil
}

type gatewayDiff struct {
	NewGateways  []rlptools.GatewayWrapper
	SameGateways []rlptools.GatewayWrapper
	LeftGateways []rlptools.GatewayWrapper
}

// Returns:
// * list of gateways to which the RLP applies for the first time
// * list of gateways to which the RLP no longer apply
// * list of gateways to which the RLP still applies
func (r *RateLimitPolicyReconciler) computeGatewayDiffs(ctx context.Context, rlp *apimv1alpha1.RateLimitPolicy) (*gatewayDiff, error) {
	logger, _ := logr.FromContext(ctx)

	rlpGwKeys, err := r.rlpGatewayKeys(ctx, rlp)
	if err != nil {
		return nil, err
	}

	// TODO(rahulanand16nov): maybe think about optimizing it with a label later
	allGwList := &gatewayapiv1alpha2.GatewayList{}
	err = r.Client().List(ctx, allGwList)
	if err != nil {
		return nil, err
	}

	gwDiff := &gatewayDiff{
		NewGateways:  rlptools.NewGateways(allGwList, client.ObjectKeyFromObject(rlp), rlpGwKeys),
		SameGateways: rlptools.SameGateways(allGwList, client.ObjectKeyFromObject(rlp), rlpGwKeys),
		LeftGateways: rlptools.LeftGateways(allGwList, client.ObjectKeyFromObject(rlp), rlpGwKeys),
	}

	logger.V(1).Info("computeGatewayDiffs",
		"#new-gw", len(gwDiff.NewGateways),
		"#same-gw", len(gwDiff.SameGateways),
		"#left-gw", len(gwDiff.LeftGateways))

	return gwDiff, nil
}

func (r *RateLimitPolicyReconciler) reconcileDirectBackReference(ctx context.Context, rlp *apimv1alpha1.RateLimitPolicy) error {
	logger, _ := logr.FromContext(ctx)
	var netObj client.Object
	var err error

	if rlp.IsForGateway() {
		netObj, err = r.fetchGateway(ctx, rlp)
		if err != nil {
			// The object should also exist
			return err
		}
	} else if rlp.IsForHTTPRoute() {
		netObj, err = r.fetchHTTPRoute(ctx, rlp)
		if err != nil {
			// The object should also exist
			return err
		}
	} else {
		logger.Info("reconcileDirectBackReference: rlp targeting unknown network resource")
		return nil
	}

	netObjKey := client.ObjectKeyFromObject(netObj)
	netObjType := netObj.GetObjectKind().GroupVersionKind()

	// Reconcile the back reference:
	objAnnotations := netObj.GetAnnotations()
	if objAnnotations == nil {
		objAnnotations = map[string]string{}
	}

	rlpKey := client.ObjectKeyFromObject(rlp)
	val, ok := objAnnotations[common.RateLimitPolicyBackRefAnnotation]
	if ok {
		if val != rlpKey.String() {
			return fmt.Errorf("the %s target %s is already referenced by ratelimitpolicy %s",
				netObjType, netObjKey, rlpKey.String())
		}
	} else {
		objAnnotations[common.RateLimitPolicyBackRefAnnotation] = rlpKey.String()
		netObj.SetAnnotations(objAnnotations)
		err := r.UpdateResource(ctx, netObj)
		logger.V(1).Info("reconcileDirectBackReference: update network resource",
			"type", netObjType, "name", netObjKey, "err", err)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *RateLimitPolicyReconciler) reconcileGatewayRLPReferences(ctx context.Context, rlp *apimv1alpha1.RateLimitPolicy, gwDiffObj *gatewayDiff) error {
	logger, _ := logr.FromContext(ctx)

	for _, leftGateway := range gwDiffObj.LeftGateways {
		if leftGateway.DeleteRLP(client.ObjectKeyFromObject(rlp)) {
			err := r.UpdateResource(ctx, leftGateway.Gateway)
			logger.V(1).Info("reconcileGatewayRLPReferences: update gateway", "left gateway key", leftGateway.Key(), "err", err)
			if err != nil {
				return err
			}
		}
	}

	for _, newGateway := range gwDiffObj.NewGateways {
		if newGateway.AddRLP(client.ObjectKeyFromObject(rlp)) {
			err := r.UpdateResource(ctx, newGateway.Gateway)
			logger.V(1).Info("reconcileGatewayRLPReferences: update gateway", "new gateway key", newGateway.Key(), "err", err)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *RateLimitPolicyReconciler) fetchHTTPRoute(ctx context.Context, rlp *apimv1alpha1.RateLimitPolicy) (*gatewayapiv1alpha2.HTTPRoute, error) {
	logger, _ := logr.FromContext(ctx)

	key := rlp.TargetKey()

	httpRoute := &gatewayapiv1alpha2.HTTPRoute{}
	err := r.Client().Get(ctx, key, httpRoute)
	logger.V(1).Info("fetchHTTPRoute", "httpRoute", key, "err", err)
	if err != nil {
		return nil, err
	}

	return httpRoute, nil
}

func (r *RateLimitPolicyReconciler) fetchGateway(ctx context.Context, rlp *apimv1alpha1.RateLimitPolicy) (*gatewayapiv1alpha2.Gateway, error) {
	logger, _ := logr.FromContext(ctx)

	key := rlp.TargetKey()

	gw := &gatewayapiv1alpha2.Gateway{}
	err := r.Client().Get(ctx, key, gw)
	logger.V(1).Info("fetchGateway", "gateway", key, "err", err)
	if err != nil {
		return nil, err
	}

	return gw, nil
}

func (r *RateLimitPolicyReconciler) gatewayRefListFromHTTPRoute(httpRoute *gatewayapiv1alpha2.HTTPRoute) []client.ObjectKey {
	gwKeys := make([]client.ObjectKey, 0)
	for _, parentRef := range httpRoute.Spec.CommonRouteSpec.ParentRefs {
		gwKey := client.ObjectKey{Name: string(parentRef.Name), Namespace: httpRoute.Namespace}
		if parentRef.Namespace != nil {
			gwKey.Namespace = string(*parentRef.Namespace)
		}
		gwKeys = append(gwKeys, gwKey)
	}

	return gwKeys
}

func (r *RateLimitPolicyReconciler) validateHTTPRoute(ctx context.Context, rlp *apimv1alpha1.RateLimitPolicy) error {
	if !rlp.IsForHTTPRoute() {
		return nil
	}

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
	httpRouteEventMapper := &HTTPRouteEventMapper{
		Logger: r.Logger().WithName("httpRouteEventMapper"),
	}
	gatewayEventMapper := &GatewayEventMapper{
		Logger: r.Logger().WithName("gatewayEventMapper"),
	}
	gatewayRateLimtPolicyEventMapper := &GatewayRateLimitPolicyEventMapper{
		Logger: r.Logger().WithName("gatewayRateLimitPolicyEventMapper"),
		Client: r.Client(),
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&apimv1alpha1.RateLimitPolicy{}).
		Watches(
			&source.Kind{Type: &gatewayapiv1alpha2.HTTPRoute{}},
			handler.EnqueueRequestsFromMapFunc(httpRouteEventMapper.MapToRateLimitPolicy),
		).
		// Currently the purpose is to generate events when rlp references change in gateways
		// so the status of the rlps targeting a route can be keep in sync
		Watches(
			&source.Kind{Type: &gatewayapiv1alpha2.Gateway{}},
			handler.EnqueueRequestsFromMapFunc(gatewayEventMapper.MapToRateLimitPolicy),
		).
		// When gateway level RLP changes, notify route level RLP's
		Watches(
			&source.Kind{Type: &apimv1alpha1.RateLimitPolicy{}},
			handler.EnqueueRequestsFromMapFunc(gatewayRateLimtPolicyEventMapper.MapRouteRateLimitPolicy),
		).
		Complete(r)
}
