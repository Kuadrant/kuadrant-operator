package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"

	"github.com/go-logr/logr"
	"github.com/gogo/protobuf/types"
	routev1 "github.com/openshift/api/route/v1"
	istioapinetworkingv1alpha3 "istio.io/api/networking/v1alpha3"
	securityv1beta1 "istio.io/api/security/v1beta1"
	istionetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	istiosecurityv1beta1 "istio.io/client-go/pkg/apis/security/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	apimv1alpha1 "github.com/kuadrant/kuadrant-controller/apis/apim/v1alpha1"
	"github.com/kuadrant/kuadrant-controller/pkg/common"
	kuadrantistio "github.com/kuadrant/kuadrant-controller/pkg/istio"
	"github.com/kuadrant/kuadrant-controller/pkg/log"
	"github.com/kuadrant/kuadrant-controller/pkg/mappers"
	"github.com/kuadrant/kuadrant-controller/pkg/reconcilers"
)

const (
	routeFinalizerName = "kuadrant.io/route"
)

// +kubebuilder:rbac:groups=route.openshift.io,namespace=placeholder,resources=routes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=route.openshift.io,namespace=placeholder,resources=routes/custom-host,verbs=create
// +kubebuilder:rbac:groups=route.openshift.io,namespace=placeholder,resources=routes/status,verbs=get

// RouteReconciler reconciles Openshift Route object
type RouteReconciler struct {
	*reconcilers.BaseReconciler
	Scheme *runtime.Scheme
}

func (r *RouteReconciler) Reconcile(eventCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger().WithValues("Route", req.NamespacedName)
	ctx := logr.NewContext(eventCtx, logger)

	route := &routev1.Route{}
	if err := r.Client().Get(ctx, req.NamespacedName, route); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		logger.Error(err, "failed to get Route")
		return ctrl.Result{}, err
	}

	if logger.V(1).Enabled() {
		jsonData, err := json.MarshalIndent(route, "", "  ")
		if err != nil {
			return ctrl.Result{}, err
		}
		logger.V(1).Info(string(jsonData))
	}

	// Route has been marked for deletion
	if route.GetDeletionTimestamp() != nil && controllerutil.ContainsFinalizer(route, routeFinalizerName) {
		err := r.deleteAuthPolicy(ctx, route)
		if err != nil {
			return ctrl.Result{}, err
		}

		err = r.deleteRateLimitFilterEnvoyFilter(ctx, route)
		if err != nil {
			return ctrl.Result{}, err
		}

		err = r.deleteRateLimitDescriptorsEnvoyFilter(ctx, route)
		if err != nil {
			return ctrl.Result{}, err
		}

		//Remove finalizer and update the object.
		controllerutil.RemoveFinalizer(route, routeFinalizerName)
		err = r.UpdateResource(ctx, route)
		logger.Info("Removing finalizer", "error", err)
		return ctrl.Result{Requeue: true}, err
	}

	// Ignore deleted resources, this can happen when foregroundDeletion is enabled
	// https://kubernetes.io/docs/concepts/workloads/controllers/garbage-collection/#foreground-cascading-deletion
	if route.GetDeletionTimestamp() != nil {
		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(route, routeFinalizerName) {
		controllerutil.AddFinalizer(route, routeFinalizerName)
		err := r.UpdateResource(ctx, route)
		logger.Info("Adding finalizer", "error", err)
		return ctrl.Result{Requeue: true}, err
	}

	routeLabels := route.GetLabels()
	if kuadrantEnabled, ok := routeLabels[mappers.KuadrantManagedLabel]; !ok || kuadrantEnabled != "true" {
		// this route used to be kuadrant protected, not anymore

		err := r.deleteAuthPolicy(ctx, route)
		if err != nil {
			return ctrl.Result{}, err
		}

		err = r.deleteRateLimitFilterEnvoyFilter(ctx, route)
		if err != nil {
			return ctrl.Result{}, err
		}

		err = r.deleteRateLimitDescriptorsEnvoyFilter(ctx, route)
		if err != nil {
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil
	}

	desiredAuthPolicy := r.desiredAuthorizationPolicy(route)
	err := r.ReconcileResource(ctx, &istiosecurityv1beta1.AuthorizationPolicy{}, desiredAuthPolicy, basicAuthPolicyMutator)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Limitador rate limits are managed by the ratelimitpolicy controller

	desiredRLFilterEF, err := r.desiredRateLimitFilterEnvoyFilter(route)
	if err != nil {
		return ctrl.Result{}, err
	}

	err = r.ReconcileResource(ctx, &istionetworkingv1alpha3.EnvoyFilter{}, desiredRLFilterEF, basicEnvoyFilterMutator)
	if err != nil {
		return ctrl.Result{}, err
	}

	desiredDescriptorsEF, err := r.desiredDescriptorsEnvoyFilter(ctx, route)
	if err != nil {
		return ctrl.Result{}, err
	}

	err = r.ReconcileResource(ctx, &istionetworkingv1alpha3.EnvoyFilter{}, desiredDescriptorsEF, basicEnvoyFilterMutator)
	if err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("successfully reconciled")
	return ctrl.Result{}, nil
}

func kuadrantRoutePredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			// Lets filter for only Routes that have the kuadrant label and are enabled.
			if val, ok := e.Object.GetLabels()[mappers.KuadrantManagedLabel]; ok {
				enabled, _ := strconv.ParseBool(val)
				return enabled
			}
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			// In case the update object had the kuadrant label set to true, we need to reconcile it.
			if val, ok := e.ObjectOld.GetLabels()[mappers.KuadrantManagedLabel]; ok {
				enabled, _ := strconv.ParseBool(val)
				return enabled
			}
			// In case that route gets update by adding the label, and set to true, we need to reconcile it.
			if val, ok := e.ObjectNew.GetLabels()[mappers.KuadrantManagedLabel]; ok {
				enabled, _ := strconv.ParseBool(val)
				return enabled
			}

			return false
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// If the object had the Kuadrant label, we need to handle its deletion
			_, ok := e.Object.GetLabels()[mappers.KuadrantManagedLabel]
			return ok
		},
	}
}

func desiredRateLimitFilterEnvoyFilterName() string {
	return "kuadrant-ratelimit-http-filter"
}

func desiredRateLimitDescriptorsEnvoyFilterName(route *routev1.Route) string {
	return fmt.Sprintf("route-%s", route.Name)
}

func (r *RouteReconciler) desiredAuthorizationPolicy(route *routev1.Route) *istiosecurityv1beta1.AuthorizationPolicy {
	authPolicy := &istiosecurityv1beta1.AuthorizationPolicy{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AuthorizationPolicy",
			APIVersion: "security.istio.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      route.Name,
			Namespace: route.Namespace,
		},
	}

	annotations := route.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	providerName, ok := annotations[mappers.KuadrantAuthProviderAnnotation]

	if !ok {
		common.TagObjectToDelete(authPolicy)
		return authPolicy
	}

	authPolicy.Spec = securityv1beta1.AuthorizationPolicy{
		Rules: []*securityv1beta1.Rule{
			{
				To: []*securityv1beta1.Rule_To{
					{
						Operation: &securityv1beta1.Operation{
							Hosts: []string{route.Spec.Host},
						},
					},
				},
			},
		},
		Action: securityv1beta1.AuthorizationPolicy_CUSTOM,
		ActionDetail: &securityv1beta1.AuthorizationPolicy_Provider{
			Provider: &securityv1beta1.AuthorizationPolicy_ExtensionProvider{
				Name: providerName,
			},
		},
	}

	return authPolicy
}

func (r *RouteReconciler) deleteAuthPolicy(ctx context.Context, route *routev1.Route) error {
	authPolicy := &istiosecurityv1beta1.AuthorizationPolicy{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AuthorizationPolicy",
			APIVersion: "security.istio.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      route.Name,
			Namespace: route.Namespace,
		},
	}

	if err := r.DeleteResource(ctx, authPolicy); client.IgnoreNotFound(err) != nil {
		return err
	}

	return nil
}

func (r *RouteReconciler) deleteRateLimitFilterEnvoyFilter(ctx context.Context, route *routev1.Route) error {
	ef := &istionetworkingv1alpha3.EnvoyFilter{
		TypeMeta: metav1.TypeMeta{
			Kind:       "EnvoyFilter",
			APIVersion: "networking.istio.io/v1alpha3",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      desiredRateLimitFilterEnvoyFilterName(),
			Namespace: route.Namespace,
		},
	}

	if err := r.DeleteResource(ctx, ef); client.IgnoreNotFound(err) != nil {
		return err
	}

	return nil
}

func (r *RouteReconciler) deleteRateLimitDescriptorsEnvoyFilter(ctx context.Context, route *routev1.Route) error {
	ef := &istionetworkingv1alpha3.EnvoyFilter{
		TypeMeta: metav1.TypeMeta{
			Kind:       "EnvoyFilter",
			APIVersion: "networking.istio.io/v1alpha3",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      desiredRateLimitDescriptorsEnvoyFilterName(route),
			Namespace: route.Namespace,
		},
	}

	if err := r.DeleteResource(ctx, ef); client.IgnoreNotFound(err) != nil {
		return err
	}

	return nil
}

func basicAuthPolicyMutator(existingObj, desiredObj client.Object) (bool, error) {
	existing, ok := existingObj.(*istiosecurityv1beta1.AuthorizationPolicy)
	if !ok {
		return false, fmt.Errorf("%T is not a *istiosecurityv1beta1.AuthorizationPolicy", existingObj)
	}
	desired, ok := desiredObj.(*istiosecurityv1beta1.AuthorizationPolicy)
	if !ok {
		return false, fmt.Errorf("%T is not a *istiosecurityv1beta1.AuthorizationPolicy", desiredObj)
	}

	updated := false
	if !reflect.DeepEqual(existing.Spec, desired.Spec) {
		existing.Spec = desired.Spec
		updated = true
	}

	tmpAnnotations := existing.GetAnnotations()
	tmpUpdated := common.MergeMapStringString(&tmpAnnotations, desired.GetAnnotations())
	if tmpUpdated {
		existing.SetAnnotations(tmpAnnotations)
		updated = true
	}

	tmpLabels := existing.GetLabels()
	tmpUpdated = common.MergeMapStringString(&tmpLabels, desired.GetLabels())
	if tmpUpdated {
		existing.SetLabels(tmpLabels)
		updated = true
	}

	return updated, nil
}

func basicEnvoyFilterMutator(existingObj, desiredObj client.Object) (bool, error) {
	existing, ok := existingObj.(*istionetworkingv1alpha3.EnvoyFilter)
	if !ok {
		return false, fmt.Errorf("%T is not a *istionetworkingv1alpha3.EnvoyFilter", existingObj)
	}
	desired, ok := desiredObj.(*istionetworkingv1alpha3.EnvoyFilter)
	if !ok {
		return false, fmt.Errorf("%T is not a *istionetworkingv1alpha3.EnvoyFilter", desiredObj)
	}

	updated := false
	if !reflect.DeepEqual(existing.Spec, desired.Spec) {
		existing.Spec = desired.Spec
		updated = true
	}

	tmpAnnotations := existing.GetAnnotations()
	tmpUpdated := common.MergeMapStringString(&tmpAnnotations, desired.GetAnnotations())
	if tmpUpdated {
		existing.SetAnnotations(tmpAnnotations)
		updated = true
	}

	tmpLabels := existing.GetLabels()
	tmpUpdated = common.MergeMapStringString(&tmpLabels, desired.GetLabels())
	if tmpUpdated {
		existing.SetLabels(tmpLabels)
		updated = true
	}

	return updated, nil
}

func (r *RouteReconciler) desiredRateLimitFilterEnvoyFilter(route *routev1.Route) (*istionetworkingv1alpha3.EnvoyFilter, error) {
	ef := &istionetworkingv1alpha3.EnvoyFilter{
		TypeMeta: metav1.TypeMeta{
			Kind:       "EnvoyFilter",
			APIVersion: "networking.istio.io/v1alpha3",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      desiredRateLimitFilterEnvoyFilterName(),
			Namespace: route.Namespace,
		},
	}

	annotations := route.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	_, ok := annotations[mappers.KuadrantRateLimitPolicyAnnotation]

	if !ok {
		// TODO(eastizle): implement references and remove when all references are gone
		common.TagObjectToDelete(ef)
		return ef, nil
	}

	patchUnstructured := map[string]interface{}{
		"operation": "INSERT_FIRST", // preauth should be the first filter in the chain
		"value": map[string]interface{}{
			"name": "envoy.filters.http.preauth.ratelimit",
			"typed_config": map[string]interface{}{
				"@type":             "type.googleapis.com/envoy.extensions.filters.http.ratelimit.v3.RateLimit",
				"domain":            "preauth",
				"stage":             kuadrantistio.PreAuthStage,
				"failure_mode_deny": true,
				// If not specified, returns success immediately (can be useful for us)
				"rate_limit_service": map[string]interface{}{
					"transport_api_version": "V3",
					"grpc_service": map[string]interface{}{
						"timeout": "3s",
						"envoy_grpc": map[string]string{
							"cluster_name": kuadrantistio.PatchedLimitadorClusterName,
						},
					},
				},
			},
		},
	}

	patchRaw, _ := json.Marshal(patchUnstructured)
	prePatch := istioapinetworkingv1alpha3.EnvoyFilter_Patch{}
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
			NumberValue: float64(kuadrantistio.PostAuthStage),
		},
	}
	// update operation for postauth filter
	postPatch.Operation = istioapinetworkingv1alpha3.EnvoyFilter_Patch_INSERT_BEFORE

	preAuthFilterPatch := &istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch{
		ApplyTo: istioapinetworkingv1alpha3.EnvoyFilter_HTTP_FILTER,
		Match: &istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch{
			Context: istioapinetworkingv1alpha3.EnvoyFilter_SIDECAR_INBOUND,
			ObjectTypes: &istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
				Listener: &istioapinetworkingv1alpha3.EnvoyFilter_ListenerMatch{
					FilterChain: &istioapinetworkingv1alpha3.EnvoyFilter_ListenerMatch_FilterChainMatch{
						Filter: &istioapinetworkingv1alpha3.EnvoyFilter_ListenerMatch_FilterMatch{
							Name: "envoy.filters.network.http_connection_manager",
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
	postAuthFilterPatch.Match.ObjectTypes = &istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
		Listener: &istioapinetworkingv1alpha3.EnvoyFilter_ListenerMatch{
			FilterChain: &istioapinetworkingv1alpha3.EnvoyFilter_ListenerMatch_FilterChainMatch{
				Filter: &istioapinetworkingv1alpha3.EnvoyFilter_ListenerMatch_FilterMatch{
					Name: "envoy.filters.network.http_connection_manager",
					SubFilter: &istioapinetworkingv1alpha3.EnvoyFilter_ListenerMatch_SubFilterMatch{
						Name: "envoy.filters.http.router",
					},
				},
			},
		},
	}

	// Eventually, this should be dropped since it's a temp-fix for Kuadrant/limitador#53
	clusterPatch := kuadrantistio.LimitadorClusterEnvoyPatch()

	ef.Spec = istioapinetworkingv1alpha3.EnvoyFilter{
		ConfigPatches: []*istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch{
			preAuthFilterPatch,
			postAuthFilterPatch,
			clusterPatch,
		},
	}

	return ef, nil
}

func (r *RouteReconciler) desiredDescriptorsEnvoyFilter(ctx context.Context, route *routev1.Route) (*istionetworkingv1alpha3.EnvoyFilter, error) {
	ef := &istionetworkingv1alpha3.EnvoyFilter{
		TypeMeta: metav1.TypeMeta{
			Kind:       "EnvoyFilter",
			APIVersion: "networking.istio.io/v1alpha3",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      desiredRateLimitDescriptorsEnvoyFilterName(route),
			Namespace: route.Namespace,
		},
	}

	annotations := route.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	rlpName, ok := annotations[mappers.KuadrantRateLimitPolicyAnnotation]

	if !ok {
		common.TagObjectToDelete(ef)
		return ef, nil
	}

	rlpKey := client.ObjectKey{Name: rlpName, Namespace: route.Namespace}
	rlp := &apimv1alpha1.RateLimitPolicy{}
	if err := r.Client().Get(ctx, rlpKey, rlp); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	patches := make([]*istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch, 0)

	// A patch example would look like this:
	//
	//- applyTo: VIRTUAL_HOST
	//  match:
	//    context: SIDECAR_INBOUND
	//    routeConfiguration:
	//      vhost:
	//        name: inbound|http|80
	//  patch:
	//    operation: MERGE
	//    value:
	//      rate_limits:
	//        - actions:
	//            - generic_key:
	//                descriptor_key: vhaction
	//                descriptor_value: "yes"
	//          stage: 0
	patchUnstructured := map[string]interface{}{
		"operation": "MERGE",
		"value": map[string]interface{}{
			"rate_limits": kuadrantistio.EnvoyFilterRatelimitsUnstructured(rlp.Spec.RateLimits),
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
	patch := &istioapinetworkingv1alpha3.EnvoyFilter_Patch{}
	if err := patch.UnmarshalJSON(patchRaw); err != nil {
		//TODO(eguzki): handle error
		panic(err)
	}

	vhPatch := &istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch{
		ApplyTo: istioapinetworkingv1alpha3.EnvoyFilter_VIRTUAL_HOST,
		Match: &istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch{
			Context: istioapinetworkingv1alpha3.EnvoyFilter_SIDECAR_INBOUND,
			ObjectTypes: &istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch_RouteConfiguration{
				RouteConfiguration: &istioapinetworkingv1alpha3.EnvoyFilter_RouteConfigurationMatch{
					Vhost: &istioapinetworkingv1alpha3.EnvoyFilter_RouteConfigurationMatch_VirtualHostMatch{
						// TODO(eastizle): hardcoded. If the API k8s service does not expose API in port 80, it will not work
						Name: "inbound|http|80",
					},
				},
			},
		},
		Patch: patch,
	}

	patches = append(patches, vhPatch)

	ef.Spec = istioapinetworkingv1alpha3.EnvoyFilter{
		ConfigPatches: patches,
	}

	return ef, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RouteReconciler) SetupWithManager(mgr ctrl.Manager) error {
	rateLimitPolicyToRouteEventMapper := &mappers.RateLimitPolicyToRouteEventMapper{
		K8sClient: r.Client(),
		Logger:    r.Logger().WithName("rateLimitPolicyToRouteHandler"),
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&routev1.Route{}, builder.WithPredicates(kuadrantRoutePredicate())).
		Watches(
			&source.Kind{Type: &apimv1alpha1.RateLimitPolicy{}},
			handler.EnqueueRequestsFromMapFunc(rateLimitPolicyToRouteEventMapper.Map),
		).
		WithLogger(log.Log). // use base logger, the manager will add prefixes for watched sources
		Complete(r)
}
