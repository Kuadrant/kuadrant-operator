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

	"github.com/go-logr/logr"
	"github.com/gogo/protobuf/types"
	"github.com/kuadrant/limitador-operator/api/v1alpha1"
	networkingv1alpha3 "istio.io/api/networking/v1alpha3"
	istio "istio.io/client-go/pkg/apis/networking/v1alpha3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	apimv1alpha1 "github.com/kuadrant/kuadrant-controller/apis/apim/v1alpha1"
	"github.com/kuadrant/kuadrant-controller/pkg/common"
	kuadrantistioutils "github.com/kuadrant/kuadrant-controller/pkg/istio"
	"github.com/kuadrant/kuadrant-controller/pkg/reconcilers"
)

const (
	EnvoysHTTPPortNumber            = 8080
	EnvoysHTTPConnectionManagerName = "envoy.filters.network.http_connection_manager"

	VSAnnotationRateLimitPolicy = "kuadrant.io/ratelimitpolicy"
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
//+kubebuilder:rbac:groups=networking.istio.io,resources=envoyfilters,verbs=get;list;watch;create;delete;update;patch
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

	var rlp apimv1alpha1.RateLimitPolicy
	if err := r.Client().Get(ctx, req.NamespacedName, &rlp); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("no RateLimitPolicy found")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "failed to get RateLimitPolicy")
		return ctrl.Result{}, err
	}

	if rlp.GetDeletionTimestamp() != nil && controllerutil.ContainsFinalizer(&rlp, patchesFinalizer) {
		logger.V(1).Info("Handling removal of ratelimitpolicy object")
		if err := r.finalizeEnvoyFilters(ctx, &rlp); err != nil {
			logger.Error(err, "failed to remove ownerRlp entry from filters patch")
			return ctrl.Result{}, err
		}
		if err := r.deleteRateLimits(ctx, &rlp); err != nil {
			logger.Error(err, "failed to delete RateLimt objects")
			return ctrl.Result{}, err
		}
		controllerutil.RemoveFinalizer(&rlp, patchesFinalizer)
		if err := r.UpdateResource(ctx, &rlp); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Ignore deleted resources, this can happen when foregroundDeletion is enabled
	// https://kubernetes.io/docs/concepts/workloads/controllers/garbage-collection/#foreground-cascading-deletion
	if rlp.GetDeletionTimestamp() != nil {
		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(&rlp, patchesFinalizer) {
		controllerutil.AddFinalizer(&rlp, patchesFinalizer)
		if err := r.UpdateResource(ctx, &rlp); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{Requeue: true}, err
		}
	}

	if err := r.reconcileLimits(ctx, &rlp); err != nil {
		return ctrl.Result{}, err
	}

	// Operation specific annotations must be removed if they were present
	updateRequired := false
	// check for delete operation for virtualservice
	if vsName, present := rlp.Annotations[KuadrantDeleteVSAnnotation]; present {
		vsNamespacedName := client.ObjectKey{
			Namespace: rlp.Namespace, // VirtualService lookup is limited to RLP's namespace
			Name:      vsName,
		}
		vsKey := vsNamespacedName.String()

		var vs istio.VirtualService
		// TODO(eastizle): if VirtualService has been deleted,
		// the Get operation returns NotFound and annotation is not deleted
		if err := r.Client().Get(ctx, vsNamespacedName, &vs); err != nil {
			if apierrors.IsNotFound(err) {
				logger.Info("no VirtualService found", "lookup name", vsNamespacedName.String())
				return ctrl.Result{}, nil
			}
			logger.Error(err, "failed to get VirtualService")
			return ctrl.Result{}, err
		}

		if err := r.detachFromNetwork(ctx, vs.Spec.Gateways, vsKey, &rlp); err != nil {
			logger.Error(err, "failed to detach RateLimitPolicy from VirtualService")
			return ctrl.Result{}, err
		}

		if err := r.detachVSFromStatus(ctx, &vs, &rlp); err != nil {
			return ctrl.Result{}, err
		}

		delete(rlp.Annotations, KuadrantDeleteVSAnnotation)
		updateRequired = true
	}

	// check for add operation for virtualservice
	if vsName, present := rlp.Annotations[KuadrantAddVSAnnotation]; present {
		vsNamespacedName := client.ObjectKey{
			Namespace: rlp.Namespace,
			Name:      vsName,
		}
		vsKey := vsNamespacedName.String()

		var vs istio.VirtualService
		if err := r.Client().Get(ctx, vsNamespacedName, &vs); err != nil {
			if apierrors.IsNotFound(err) {
				logger.Info("no VirtualService found", "lookup name", vsNamespacedName.String())
				return ctrl.Result{}, nil
			}
			logger.Error(err, "failed to get VirutalService")
			return ctrl.Result{}, err
		}

		if err := r.attachToNetwork(ctx, vs.Spec.Gateways, vs.Spec.Hosts, vsKey, &rlp); err != nil {
			logger.Error(err, "failed to attach RateLimitPolicy to VirtualService")
			return ctrl.Result{}, err
		}

		if err := r.attachVSToStatus(ctx, &vs, &rlp); err != nil {
			return ctrl.Result{}, err
		}

		delete(rlp.Annotations, KuadrantAddVSAnnotation)
		updateRequired = true
	}

	if updateRequired {
		if err := r.Client().Update(ctx, &rlp); err != nil {
			logger.Error(err, "failed to remove operation specific annotations from RateLimitPolicy")
			return ctrl.Result{}, err
		}
		logger.Info("successfully removed operation specific annotations from RateLimitPolicy")
	}

	// TODO(rahulanand16nov): do the same as above for HTTPRoute
	logger.Info("successfully reconciled RateLimitPolicy")
	return ctrl.Result{}, nil
}

func (r *RateLimitPolicyReconciler) detachFromNetwork(ctx context.Context, gateways []string, owner string, rlp *apimv1alpha1.RateLimitPolicy) error {
	logger := logr.FromContext(ctx)
	ownerKey := common.NamespacedNameToObjectKey(owner, rlp.Namespace)
	logger.Info("Detaching RateLimitPolicy from a network")

	for _, gw := range gateways {
		gwKey := common.NamespacedNameToObjectKey(gw, rlp.Namespace)

		// fetch the filters patch
		filtersPatchKey := client.ObjectKey{Namespace: gwKey.Namespace, Name: rlFiltersPatchName(gwKey.Name)}
		filtersPatch := &istio.EnvoyFilter{}
		// TODO(eastizle): if not found, do not return error. It has already been deleted.
		if err := r.Client().Get(ctx, filtersPatchKey, filtersPatch); err != nil {
			logger.Error(err, "failed to get ratelimit filters patch")
			return err
		}

		// remove the parentRef entry on filters patch
		if err := r.removeParentRefEntry(ctx, filtersPatch, owner); err != nil {
			logger.Error(err, "failed to remove parentRef entry on the ratelimit filters patch")
			return err
		}
		logger.Info("successfully deleted/updated ratelimit filters patch")

		// fetch the ratelimits patch
		ratelimitsPatchKey := client.ObjectKey{Namespace: gwKey.Namespace, Name: ratelimitsPatchName(gwKey.Name, ownerKey)}
		ratelimitsPatch := &istio.EnvoyFilter{}
		// TODO(eastizle): if not found, do not return error. It has already been deleted.
		if err := r.Client().Get(ctx, ratelimitsPatchKey, ratelimitsPatch); err != nil {
			logger.Error(err, "failed to get ratelimits patch")
			return err
		}

		// remove the parentRef entry on ratelimits patch
		if err := r.removeParentRefEntry(ctx, ratelimitsPatch, owner); err != nil {
			logger.Error(err, "failed to remove parentRef entry on the ratelimits patch")
			return err
		}
		logger.Info("successfully deleted/updated ratelimit filters patch")
	}

	logger.Info("successfully detached RateLimitPolicy from specified gateways and hosts")
	return nil
}

func (r *RateLimitPolicyReconciler) attachToNetwork(ctx context.Context, gateways, hosts []string, owner string, rlp *apimv1alpha1.RateLimitPolicy) error {
	logger := logr.FromContext(ctx)
	ownerKey := common.NamespacedNameToObjectKey(owner, rlp.Namespace)
	logger.Info("Attaching RateLimitPolicy to a network")

	for _, gw := range gateways {
		gwKey := common.NamespacedNameToObjectKey(gw, rlp.Namespace)
		gwLabels := gatewayLabels(ctx, r.Client(), gwKey)

		// create/update ratelimit filters patch
		// fetch already existing filters patch or create a new one
		filtersPatchKey := client.ObjectKey{Namespace: gwKey.Namespace, Name: rlFiltersPatchName(gwKey.Name)}
		filtersPatch := &istio.EnvoyFilter{}
		if err := r.Client().Get(ctx, filtersPatchKey, filtersPatch); err != nil {
			if !apierrors.IsNotFound(err) {
				logger.Error(err, "failed to get ratelimit filters patch")
				return err
			}
			filtersPatch, err = ratelimitFiltersPatch(gwKey, gwLabels)
			if err != nil {
				logger.Error(err, "failed to form ratelimit filters patch")
				return err
			}
		}

		if err := r.addParentRefEntry(ctx, filtersPatch, owner); err != nil {
			logger.Error(err, "failed to add ownerRLP entry to the ratelimit filters patch")
			return err
		}
		logger.Info("successfully created/updated ratelimit filters patch", "gateway", gwKey.String())

		// create/update ratelimits patch
		ratelimitsPatchKey := client.ObjectKey{Namespace: gwKey.Namespace, Name: ratelimitsPatchName(gwKey.Name, ownerKey)}
		ratelimitsEnvoyFilter := &istio.EnvoyFilter{}
		if err := r.Client().Get(ctx, ratelimitsPatchKey, ratelimitsEnvoyFilter); err != nil {
			if !apierrors.IsNotFound(err) {
				logger.Error(err, "failed to get ratelimits patch")
				return err
			}
			ratelimitsEnvoyFilter = desiredRatelimitsEnvoyFilter(rlp, hosts, gwKey, ownerKey, gwLabels)
		}

		// Note(rahulanand16nov): ratelimits patch don't require parentRef because they are unique per VirtualService
		if err := r.ReconcileResource(ctx, &istio.EnvoyFilter{}, ratelimitsEnvoyFilter, alwaysUpdateEnvoyPatches); err != nil {
			logger.Error(err, "failed to reconcile ratelimits patch")
			return err
		}
		logger.Info("successfully created/updated ratelimits patch", "gateway", gwKey.String())
	}
	logger.Info("successfully attached RateLimitPolicy to specified gateways and hosts")
	return nil
}

// rateLimitInitialPatch returns EnvoyFilter resource that patches-in
// - Add Preauth RateLimit Filter as the first http filter
// - Add PostAuth RateLimit Filter as the last http filter
// - Change cluster name pointing to Limitador in kuadrant-system namespace (temp)
// Note: please make sure this patch is only created once per gateway.
func ratelimitFiltersPatch(gwKey client.ObjectKey, gwLabels map[string]string) (*istio.EnvoyFilter, error) {
	patchUnstructured := map[string]interface{}{
		"operation": "INSERT_FIRST", // preauth should be the first filter in the chain
		"value": map[string]interface{}{
			"name": "envoy.filters.http.preauth.ratelimit",
			"typed_config": map[string]interface{}{
				"@type":             "type.googleapis.com/envoy.extensions.filters.http.ratelimit.v3.RateLimit",
				"domain":            "preauth",
				"stage":             kuadrantistioutils.PreAuthStage,
				"failure_mode_deny": true,
				// If not specified, returns success immediately (can be useful for us)
				"rate_limit_service": map[string]interface{}{
					"transport_api_version": "V3",
					"grpc_service": map[string]interface{}{
						"timeout": "3s",
						"envoy_grpc": map[string]string{
							"cluster_name": kuadrantistioutils.PatchedLimitadorClusterName,
						},
					},
				},
			},
		},
	}

	patchRaw, _ := json.Marshal(patchUnstructured)
	prePatch := networkingv1alpha3.EnvoyFilter_Patch{}
	if err := prePatch.UnmarshalJSON(patchRaw); err != nil {
		return nil, err
	}

	postPatch := prePatch.DeepCopy()
	postPatch.Value.Fields["name"] = &types.Value{
		Kind: &types.Value_StringValue{
			StringValue: "envoy.filters.http.postauth.ratelimit",
		},
	}

	// update domain for postauth filter
	postPatch.Value.Fields["typed_config"].GetStructValue().Fields["domain"] = &types.Value{
		Kind: &types.Value_StringValue{
			StringValue: "postauth",
		},
	}
	// update stage for postauth filter
	postPatch.Value.Fields["typed_config"].GetStructValue().Fields["stage"] = &types.Value{
		Kind: &types.Value_NumberValue{
			NumberValue: float64(kuadrantistioutils.PostAuthStage),
		},
	}
	// update operation for postauth filter
	postPatch.Operation = networkingv1alpha3.EnvoyFilter_Patch_INSERT_BEFORE

	preAuthFilterPatch := &networkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch{
		ApplyTo: networkingv1alpha3.EnvoyFilter_HTTP_FILTER,
		Match: &networkingv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch{
			Context: networkingv1alpha3.EnvoyFilter_GATEWAY,
			ObjectTypes: &networkingv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
				Listener: &networkingv1alpha3.EnvoyFilter_ListenerMatch{
					PortNumber: EnvoysHTTPPortNumber, // Kuadrant-gateway listens on this port by default
					FilterChain: &networkingv1alpha3.EnvoyFilter_ListenerMatch_FilterChainMatch{
						Filter: &networkingv1alpha3.EnvoyFilter_ListenerMatch_FilterMatch{
							Name: EnvoysHTTPConnectionManagerName,
						},
					},
				},
			},
		},
		Patch: &prePatch,
	}

	postAuthFilterPatch := preAuthFilterPatch.DeepCopy()
	postAuthFilterPatch.Patch = postPatch

	// postauth filter should be injected just before the router filter
	postAuthFilterPatch.Match.ObjectTypes = &networkingv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
		Listener: &networkingv1alpha3.EnvoyFilter_ListenerMatch{
			PortNumber: EnvoysHTTPPortNumber,
			FilterChain: &networkingv1alpha3.EnvoyFilter_ListenerMatch_FilterChainMatch{
				Filter: &networkingv1alpha3.EnvoyFilter_ListenerMatch_FilterMatch{
					Name: EnvoysHTTPConnectionManagerName,
					SubFilter: &networkingv1alpha3.EnvoyFilter_ListenerMatch_SubFilterMatch{
						Name: "envoy.filters.http.router",
					},
				},
			},
		},
	}

	// Eventually, this should be dropped since it's a temp-fix for Kuadrant/limitador#53
	clusterPatch := kuadrantistioutils.LimitadorClusterEnvoyPatch()

	factory := kuadrantistioutils.EnvoyFilterFactory{
		ObjectName: rlFiltersPatchName(gwKey.Name),
		Namespace:  gwKey.Namespace,
		Patches: []*networkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch{
			preAuthFilterPatch,
			postAuthFilterPatch,
			clusterPatch,
		},
		Labels: gwLabels,
	}

	return factory.EnvoyFilter(), nil
}

func desiredRatelimitsEnvoyFilter(rlp *apimv1alpha1.RateLimitPolicy, vHostNames []string, gwKey, networkingKey client.ObjectKey, gwLabels map[string]string) *istio.EnvoyFilter {
	patches := make([]*networkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch, 0)

	// route-level patches
	for _, host := range vHostNames {
		// TODO(eguzki): The VirtualHost name is generated by envoy from the
		// Virtualservice domain + gateway port information
		// Instead of harcoding, it should be read from the Gateway object.
		vHostName := host + ":80" // Istio names virtual host in this format: host + port
		vhPatch := virtualHostRateLimitsPatch(vHostName, rlp.Spec.RateLimits)
		patches = append(patches, vhPatch)

		for _, route := range rlp.Spec.Routes {
			routePatch := routeRateLimitsPatch(vHostName, route.Name, route.RateLimits)
			patches = append(patches, routePatch)
		}
	}

	factory := kuadrantistioutils.EnvoyFilterFactory{
		ObjectName: ratelimitsPatchName(gwKey.Name, networkingKey),
		Namespace:  gwKey.Namespace,
		Patches:    patches,
		Labels:     gwLabels,
	}

	return factory.EnvoyFilter()
}

func routeRateLimitsPatch(vHostName string, routeName string, rateLimits []*apimv1alpha1.RateLimit) *networkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch {
	// A patch example would look like this:
	//
	//applyTo: HTTP_ROUTE
	//match:
	//  context: GATEWAY
	//  routeConfiguration:
	//    vhost:
	//	    name: api.animaltoys.127.0.0.1.nip.io:80
	//	    route:
	//	      name: getToys
	//patch:
	//  operation: MERGE
	//  value:
	//    route:
	//      rate_limits:
	//	      - stage: 0
	//		    actions:
	//            - request_headers:
	//                header_name: ":path"
	//                descriptor_key: "req.path"
	//            - request_headers:
	//                header_name: ":method"
	//                descriptor_key: "req.method"

	patchUnstructured := map[string]interface{}{
		"operation": "MERGE",
		"value": map[string]interface{}{
			"route": map[string]interface{}{
				"rate_limits": kuadrantistioutils.EnvoyFilterRatelimitsUnstructured(rateLimits),
			},
		},
	}

	patchRaw, _ := json.Marshal(patchUnstructured)
	patch := &networkingv1alpha3.EnvoyFilter_Patch{}
	if err := patch.UnmarshalJSON(patchRaw); err != nil {
		//TODO(eguzki): handle error
		panic(err)
	}

	return &networkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch{
		ApplyTo: networkingv1alpha3.EnvoyFilter_HTTP_ROUTE,
		Match: &networkingv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch{
			Context: networkingv1alpha3.EnvoyFilter_GATEWAY,
			ObjectTypes: &networkingv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
				RouteConfiguration: &networkingv1alpha3.EnvoyFilter_RouteConfigurationMatch{
					Vhost: &networkingv1alpha3.EnvoyFilter_RouteConfigurationMatch_VirtualHostMatch{
						Name: vHostName,
						Route: &networkingv1alpha3.EnvoyFilter_RouteConfigurationMatch_RouteMatch{
							Name: routeName,
						},
					},
				},
			},
		},
		Patch: patch,
	}
}

func virtualHostRateLimitsPatch(vHostName string, rateLimits []*apimv1alpha1.RateLimit) *networkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch {
	// A patch example would look like this:
	//
	// applyTo: VIRTUAL_HOST
	// match:
	//   context: GATEWAY
	//   routeConfiguration:
	//     vhost:
	// 	    name: api.animaltoys.127.0.0.1.nip.io:80
	// 	    route:
	// 	      name: getToys
	// patch:
	//   operation: MERGE
	//   value:
	//     rate_limits:
	// 	    - stage: 0
	// 	      actions:
	//           - request_headers:
	//               header_name: ":path"
	//               descriptor_key: "req.path"
	//           - request_headers:
	//               header_name: ":method"
	//               descriptor_key: "req.method"
	//     typed_per_filter_config:
	//       envoy.filters.http.ratelimit:
	//         @type: "type.googleapis.com/envoy.extensions.filters.http.ratelimit.v3.RateLimitPerRoute"
	//         vh_rate_limits: INCLUDE
	patchUnstructured := map[string]interface{}{
		"operation": "MERGE",
		"value": map[string]interface{}{
			"rate_limits": kuadrantistioutils.EnvoyFilterRatelimitsUnstructured(rateLimits),
			"typed_per_filter_config": map[string]interface{}{
				// Note the following name is different from what we have given to our pre/post-auth
				// ratelimit filters. It's because you refer to the type of filter and not the name field
				// of the filter. This infers it's configured for both filters in our case.
				"envoy.filters.http.ratelimit": map[string]interface{}{
					"@type":          "type.googleapis.com/envoy.extensions.filters.http.ratelimit.v3.RateLimitPerRoute",
					"vh_rate_limits": "INCLUDE",
				},
			},
		},
	}

	patchRaw, _ := json.Marshal(patchUnstructured)
	patch := &networkingv1alpha3.EnvoyFilter_Patch{}
	if err := patch.UnmarshalJSON(patchRaw); err != nil {
		//TODO(eguzki): handle error
		panic(err)
	}

	return &networkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch{
		ApplyTo: networkingv1alpha3.EnvoyFilter_VIRTUAL_HOST,
		Match: &networkingv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch{
			Context: networkingv1alpha3.EnvoyFilter_GATEWAY,
			ObjectTypes: &networkingv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
				RouteConfiguration: &networkingv1alpha3.EnvoyFilter_RouteConfigurationMatch{
					Vhost: &networkingv1alpha3.EnvoyFilter_RouteConfigurationMatch_VirtualHostMatch{
						Name: vHostName,
					},
				},
			},
		},
		Patch: patch,
	}
}

func (r *RateLimitPolicyReconciler) reconcileLimits(ctx context.Context, rlp *apimv1alpha1.RateLimitPolicy) error {
	logger := logr.FromContext(ctx)
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

func (r *RateLimitPolicyReconciler) attachVSToStatus(ctx context.Context, vs *istio.VirtualService, rlp *apimv1alpha1.RateLimitPolicy) error {
	if updated := rlp.Status.AddVirtualService(vs); updated {
		logger := logr.FromContext(ctx)
		err := r.Client().Status().Update(ctx, rlp)
		logger.V(1).Info("adding VS to status", "virtualservice", vs.Name, "error", err)
		return err
	}
	return nil
}

func (r *RateLimitPolicyReconciler) detachVSFromStatus(ctx context.Context, vs *istio.VirtualService, rlp *apimv1alpha1.RateLimitPolicy) error {
	if updated := rlp.Status.DeleteVirtualService(vs); updated {
		logger := logr.FromContext(ctx)
		err := r.Client().Status().Update(ctx, rlp)
		logger.V(1).Info("deleting VS from status", "virtualservice", vs.Name, "error", err)
		return err
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RateLimitPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&apimv1alpha1.RateLimitPolicy{}).
		Complete(r)
}
